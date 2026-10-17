# Campus Hub Server

A Go HTTP API backed by PostgreSQL. The user API manages profiles and stores password hashes. Login, authorization, and authenticated password changes are separate features.

## Run locally

Run these commands from the repository root, the directory containing this README and go.mod.

1. Copy .env.example to .env if you do not already have a configured .env.
2. Set the DB_* variables to your existing PostgreSQL credentials and database.
3. For a new database, apply both migrations once in order:

~~~sh
psql -h localhost -p 5432 -U postgres -d campus_hub -v ON_ERROR_STOP=1 -1 -f migrations/000001_create_users.up.sql -f migrations/000002_add_password_hash.up.sql
~~~

Use your configured host, port, username, and database if they differ. psql prompts for the database password. The first migration creates a new users table and refuses to overwrite an existing table.

If the users table already exists from migration 000001, apply only the new password migration:

~~~sh
psql -h localhost -p 5432 -U postgres -d campus_hub -v ON_ERROR_STOP=1 -1 -f migrations/000002_add_password_hash.up.sql
~~~

Existing profiles retain their data and have an unset (NULL) password hash. No default password is assigned. An authenticated password setup/reset flow will be needed before those profiles can log in.

4. Start the API:

~~~sh
go run ./cmd/api
~~~

The local server defaults to http://127.0.0.1:8080. HOST and PORT override the HTTP address. Database queries have a five-second request deadline, and the server shuts down on Ctrl+C.

The endpoints are unauthenticated profile CRUD. Local configuration and Compose bind the published API port to loopback.

## User endpoints

| Method | Path | Result |
| --- | --- | --- |
| POST | /api/v1/users | Create a profile; 201 and a Location header |
| GET | /api/v1/users?limit=20&offset=0 | List profiles ordered by ID; 200 |
| GET | /api/v1/users/{id} | Get a profile; 200 |
| PUT | /api/v1/users/{id} | Replace name and email; 200 |
| DELETE | /api/v1/users/{id} | Delete a profile; 204 |
| GET | /healthz | Process liveness; 200 |

IDs are positive integers. POST requires Content-Type: application/json and all three fields:

~~~json
{
  "name": "Bukho Mabhula",
  "email": "bukho@example.com",
  "password": "a long example passphrase"
}
~~~

Names are trimmed and contain 1–100 characters. Emails are trimmed, lowercased, validated, limited to 254 bytes, and unique. PUT requires name and email and replaces those profile fields. It rejects password fields and preserves the existing password hash. Unknown JSON fields, multiple JSON values, and request bodies larger than 16 KiB are rejected.

Passwords must contain 15–128 Unicode characters. They are preserved exactly, including whitespace. The service hashes passwords with Argon2id (19 MiB memory, two iterations, one lane), a random 16-byte salt, and a 32-byte derived key, following the [OWASP password storage baseline](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html). The encoded hash includes its algorithm, parameters, and salt. Only the hash is written to PostgreSQL; responses never include passwords or hashes. The internal password package also provides constant-time hash verification for a future login flow.

Single-user responses contain id, name, email, created_at, and updated_at. Lists contain users, limit, and offset; an empty list contains an empty array. The default limit is 20, the maximum is 100, and offset must be between 0 and 2147483647.

Errors contain an error message. Invalid input returns 400, a missing user 404, a duplicate email 409, an oversized body 413, an unsupported content type 415, and a database request timeout 504. Unexpected failures return a generic 500 response and are logged by the server.

Example using Git Bash:

~~~sh
curl -i http://127.0.0.1:8080/api/v1/users \
  -H 'Content-Type: application/json' \
  -d '{"name":"Bukho Mabhula","email":"bukho@example.com","password":"a long example passphrase"}'
~~~

## Docker Compose

~~~sh
docker compose up --build
~~~

Compose supplies the DB_* settings expected by Go, using db as the internal database hostname. The API is published at 127.0.0.1:8080; PostgreSQL is published at 127.0.0.1:5433 to avoid conflicting with a local PostgreSQL installation.

POSTGRES_USER, POSTGRES_PASSWORD, and POSTGRES_DB configure the Compose database and its API credentials. They default to campushub, local_dev_password, and campushub for local development. POSTGRES_PORT overrides the published database port, and PORT overrides the published API port. The container always connects to db:5432.

Both up migrations run automatically when PostgreSQL initializes a fresh data volume. For an existing Compose database, apply only migrations that have not already run. If the users table already exists, run only the second command:

~~~sh
docker compose exec db sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -1 -f /docker-entrypoint-initdb.d/000001_create_users.sql'
docker compose exec db sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -1 -f /docker-entrypoint-initdb.d/000002_add_password_hash.sql'
~~~

Existing volumes retain their original PostgreSQL credentials. Changing environment variables does not change credentials already stored in PostgreSQL.

## Tests

All tests live in test/.

~~~sh
go test ./...
go vet ./...
go build ./...
~~~

PostgreSQL integration tests are opt-in locally. Using the project's .env, in Git Bash:

~~~sh
RUN_DATABASE_TESTS=1 go test ./test -run TestUsersPostgres -count=1 -v
~~~

In PowerShell:

~~~powershell
$env:RUN_DATABASE_TESTS = '1'
go test ./test -run TestUsersPostgres -count=1 -v
Remove-Item Env:RUN_DATABASE_TESTS
~~~

Alternatively, set TEST_DATABASE_URL to a dedicated test database. Integration tests create an isolated schema inside a transaction and always roll it back. They exercise the real migrations, HTTP routes, CRUD queries, pagination, duplicate-email handling, missing-user responses, salted hash storage, password privacy, and preservation of existing profiles during migration. The test database role must have permission to create schemas.

CI provides PostgreSQL and runs these integration tests together with the remaining tests, race detection, coverage, vet, formatting, compilation, and a Docker image build. Running race detection locally also requires CGO and a supported C compiler:

~~~sh
go test -race ./...
~~~
