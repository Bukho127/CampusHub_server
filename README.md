# Campus Hub Server

Go HTTP API backed by PostgreSQL. The account model follows the previous Node schema using snake_case JSON and SQL columns. JWT login and owner/admin authorization are implemented. Email delivery, verification processing, password resets, and moderation endpoints are separate features.

## Local setup

Run commands from the repository root, alongside go.mod.

Copy .env.example to .env if no configured .env exists, then set the DB_* variables for your PostgreSQL database and JWT_SECRET to a randomly generated secret of at least 32 bytes. For example, openssl rand -hex 32 generates a suitable value. Preserve your configured secret across restarts. Use # for .env comments. Apply only migrations that have not already run, in numeric order.

For a fresh database:

~~~sh
psql -h localhost -p 5432 -U postgres -d campus_hub -v ON_ERROR_STOP=1 -1 \
  -f migrations/000001_create_users.up.sql \
  -f migrations/000002_add_password_hash.up.sql \
  -f migrations/000003_expand_user_schema.up.sql
~~~

If migrations 000001 and 000002 were already applied:

~~~sh
psql -h localhost -p 5432 -U postgres -d campus_hub -v ON_ERROR_STOP=1 -1 -f migrations/000003_expand_user_schema.up.sql
~~~

Use your configured connection settings if different. psql prompts for the database password. SQL files do not maintain a migration history table.

Migration 000002 leaves existing profiles with an unset password hash. Migration 000003 renames name to display_name, preserving its values. Legacy first_name and last_name remain empty, and identity_type is unset, until genuine information is supplied. No passwords, names, or identities are invented for old profiles.

Start the API:

~~~sh
go run ./cmd/api
~~~

The server defaults to http://127.0.0.1:8080; HOST and PORT override the address. Requests have a five-second database deadline and Ctrl+C shuts down the server.

Local configuration and Compose publish the API on loopback. Registration, login, and health checks are public. The remaining user endpoints require Bearer authentication and enforce the permissions below.

## Authentication and authorization

JWT_SECRET stays on the server. It signs access tokens with HS256 and validates their signatures. Tokens require the campus-hub issuer, campus-hub-api audience, an access token purpose, user ID, token version, token ID, issued/not-before timestamps, and expiration. They last 15 minutes with a five-second clock tolerance. Validation uses a fixed algorithm allowlist, following the [Go JWT library guidance](https://golang-jwt.github.io/jwt/usage/parse/).

Log in with the account's existing password:

~~~sh
curl -i http://127.0.0.1:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"bukho@example.com","password":"a long example passphrase"}'
~~~

The response contains access_token, token_type (Bearer), expires_in (900 seconds), and user_id. Send the returned token on protected requests:

~~~sh
curl http://127.0.0.1:8080/api/v1/users/1 \
  -H 'Authorization: Bearer TOKEN_FROM_LOGIN'
~~~

Use the user_id returned by login in the URL. The server accepts tokens from the Authorization header and checks current account status, role, and token_version in PostgreSQL on every protected request. Roles supplied in JWT claims do not grant permissions. Banned/deleted accounts, nonexistent users, and mismatched token versions are rejected.

| Endpoint | Permission |
| --- | --- |
| POST /api/v1/users | Public registration; role always defaults to user |
| POST /api/v1/auth/login | Public; active account with a valid password |
| GET /healthz | Public |
| GET /api/v1/users | Admin |
| GET /api/v1/users/{id} | Owner or admin |
| PUT /api/v1/users/{id} | Owner or admin |
| DELETE /api/v1/users/{id} | Owner or admin |
| POST /api/v1/auth/logout | Authenticated; revokes all issued tokens for the caller |

Logout increments token_version. Email changes and deletion also invalidate existing tokens. Database role changes apply to the next request. Administration and role provisioning use trusted database/admin tooling; public registration cannot create an admin. Unverified email alone does not block login while verification workflows are pending. Legacy accounts without a password hash cannot log in.

Missing, invalid, expired, or revoked tokens return 401 with a Bearer challenge. Insufficient permissions return 403. Authentication database failures return 503 and fail closed. Login failures use the same invalid-email-or-password response for unknown emails, incorrect passwords, banned/deleted users, and unset password hashes. Tokens and credentials are excluded from logs, and authentication responses are not cached.

There is no refresh-token endpoint; log in again after expiration. Rotating JWT_SECRET invalidates all previously signed access tokens.

## User API

| Method | Path | Result |
| --- | --- | --- |
| POST | /api/v1/users | Create; 201 and a Location header |
| GET | /api/v1/users?limit=20&offset=0 | List non-deleted profiles ordered by ID; 200 |
| GET | /api/v1/users/{id} | Get a non-deleted profile; 200 |
| PUT | /api/v1/users/{id} | Replace editable profile fields; 200 |
| DELETE | /api/v1/users/{id} | Soft-delete; 204 |
| GET | /healthz | Process liveness; 200 |

IDs are positive integers. POST requires Content-Type: application/json and:

~~~json
{
  "first_name": "Bukho",
  "last_name": "Mabhula",
  "display_name": "Bukho Mabhula",
  "email": "bukho@example.com",
  "password": "a long example passphrase",
  "identity_type": "student"
}
~~~

Creation also accepts institution_id, student_number, campus_email, location, and avatar.

First, last, and display names are trimmed and require 1-100 characters each. Identity type is student, faculty, resident, or vendor. Institution ID must be positive when supplied. Email and campus email are trimmed, lowercased, validated, and limited to 254 bytes. Account email is unique, including accounts retained by soft deletion. Student number is trimmed and limited to 128 characters, location to 255, and avatar to 2048. Avatar may be a path or URL.

PUT requires first_name, last_name, display_name, and email. It also accepts location and avatar; omitted optional values are cleared. It excludes password, identity type, institution membership, and private campus identity fields.

Changing account email resets its verification status, clears obsolete verification codes and reset tokens, and increments token_version. Editing a profile with the same email preserves verification. Password hashes and campus verification remain unchanged.

The server controls role, status, ban information, all verification states, ratings, review counts, token versions, and verification/reset tokens. Client-supplied values for these fields are rejected. A vendor identity starts with role=user and unverified vendor status.

DELETE sets status=deleted and deleted_at, increments token_version, and clears outstanding verification/reset tokens. Deleted rows and their email uniqueness reservations remain, but normal API get/list/update/delete operations exclude them.

List responses contain users, limit, and offset. An empty list is an array. Default limit is 20, maximum is 100, and offset is between 0 and 2147483647.

Errors contain an error message: invalid input 400, missing/deleted user 404, duplicate email 409, oversized body 413, unsupported content type 415, and database request timeout 504. Unexpected failures return a generic 500 and are logged. Unknown JSON fields, multiple JSON values, and bodies larger than 16 KiB are rejected.

Example using Git Bash:

~~~sh
curl -i http://127.0.0.1:8080/api/v1/users \
  -H 'Content-Type: application/json' \
  -d '{"first_name":"Bukho","last_name":"Mabhula","display_name":"Bukho Mabhula","email":"bukho@example.com","password":"a long example passphrase","identity_type":"student"}'
~~~

## Legacy schema mapping

| Area | Stored fields and behavior |
| --- | --- |
| Identity | first_name, last_name, display_name, institution_id, student_number, identity_type |
| Permissions | role: user/vendor/admin; status: active/banned/deleted |
| Email | email_verification_status: unverified/pending/verified; email_verified is generated from that status |
| Vendor | vendor_verification_status: unverified/pending/verified |
| Campus email | email, verification status, verified timestamp, token hash, expiry |
| Moderation | ban_reason, banned_until, deleted_at |
| Reviews | nullable rating between 0 and 5; review_count defaults to 0 |
| Credentials | password hash, verification-code hash/expiry/attempts, password-reset hash/expiry, token_version |
| Metadata | location, avatar, created_at, updated_at |

Both resetTokenHash and passwordResetTokenHash map to password_reset_token_hash. resetTokenExpires and passwordResetExpiresAt map to password_reset_expires_at. Email verification has one source of truth: email_verified is a PostgreSQL generated column based on email_verification_status.

Database constraints enforce enum values, non-negative counters, and rating bounds. Role defaults to user, status to active, verification statuses to unverified, and counters to zero. Passwords are required for new API-created users; legacy profiles may have an unset hash pending authenticated setup.

Passwords, hashes, student numbers, campus email, verification/reset tokens and expiries, verification attempts, and token_version are excluded from JSON responses. Public profile queries do not select these private fields.

institution_id is currently nullable PostgreSQL bigint metadata. The institution table, foreign key, and existence validation will be added with the institution module. Importing MongoDB documents requires a separate ObjectId-to-PostgreSQL-ID mapping.

## Password storage

Passwords require 15-128 Unicode characters, preserved exactly including whitespace. The service uses Argon2id (19 MiB memory, two iterations, one lane), a random 16-byte salt, and a 32-byte derived key, following the [OWASP baseline](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html).

Only the encoded hash reaches storage. Login uses constant-time password verification. Password changes and setup for legacy profiles still require dedicated authenticated flows; the general profile endpoint rejects passwords.

## Docker Compose

~~~sh
docker compose up --build
~~~

Compose supplies DB_* settings with db as the internal database host and requires JWT_SECRET from .env or the process environment. The API is published at 127.0.0.1:8080; PostgreSQL at 127.0.0.1:5433 to avoid a local PostgreSQL port conflict.

POSTGRES_USER, POSTGRES_PASSWORD, and POSTGRES_DB configure Compose credentials. Local defaults are campushub, local_dev_password, and campushub. POSTGRES_PORT overrides the published database port and PORT the published API port. The API container connects to db:5432.

All three up migrations run automatically when PostgreSQL initializes a fresh volume. For an existing volume, apply only missing migrations. If the first two were applied, run:

~~~sh
docker compose exec db sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -1 -f /docker-entrypoint-initdb.d/000003_expand_user_schema.sql'
~~~

Existing volumes retain their original database credentials. Editing environment variables does not change credentials already stored in PostgreSQL.

## Tests

All tests live in test/.

~~~sh
go test ./...
go vet ./...
go build ./...
~~~

PostgreSQL integration tests opt in locally. Using the project's .env in Git Bash:

~~~sh
RUN_DATABASE_TESTS=1 go test ./test -run TestUsersPostgres -count=1 -v
~~~

PowerShell:

~~~powershell
$env:RUN_DATABASE_TESTS = '1'
go test ./test -run TestUsersPostgres -count=1 -v
Remove-Item Env:RUN_DATABASE_TESTS
~~~

Alternatively, set TEST_DATABASE_URL. The role needs permission to create schemas. Integration tests use an isolated schema inside a transaction and always roll it back. They cover migration and rollback, legacy data, CRUD, pagination, private-field storage, salted hashes, account defaults, database constraints, verification invalidation, soft deletion, and real JWT login/authorization/revocation. Unit tests additionally cover invalid signatures, forbidden algorithms, expired/malformed claims, ownership, admin permissions, and dotenv secret-redaction errors. Tests use a separate test signing key.

CI supplies PostgreSQL and runs integration tests, race detection, coverage, vet, formatting, compilation, and a Docker image build. Local race detection requires CGO and a supported C compiler:

~~~sh
go test -race ./...
~~~
