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
  -f migrations/000003_expand_user_schema.up.sql \
  -f migrations/000004_create_media.up.sql
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

The server defaults to http://127.0.0.1:8080; HOST and PORT override the address. Requests have a five-second deadline; media requests allow 45 seconds for uploads, processing, and blob transfer. Ctrl+C shuts down the server.

Local configuration and Compose publish the API on loopback. Registration, login, and health checks are public. The remaining user endpoints require Bearer authentication and enforce the permissions below.

## Authentication and authorization

JWT_SECRET stays on the server. It signs access tokens with HS256 and validates their signatures. Tokens require the campus-hub issuer, campus-hub-api audience, an access token purpose, user ID, token version, token ID, issued/not-before timestamps, and expiration. They last 15 minutes with a five-second clock tolerance. Validation uses a fixed algorithm allowlist, following the [Go JWT library guidance](https://golang-jwt.github.io/jwt/usage/parse/).

Register an account (no Bearer token required):

~~~sh
curl -i http://127.0.0.1:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"first_name":"Bukho","last_name":"Mabhula","display_name":"Bukho Mabhula","email":"bukho@example.com","password":"a long example passphrase","identity_type":"student"}'
~~~

Registration returns 201 with access_token, token_type (Bearer), expires_in (900 seconds), user_id, and the public profile under user. The Location header points to the new profile. Use the returned token immediately on protected requests. POST /api/v1/users also creates an account with the same validation and Argon2id password hashing, and returns only the public profile.

Existing accounts can log in separately:

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
| POST /api/v1/auth/register | Public registration; role always defaults to user |
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

## Media API

Media uses private Azure Blob Storage for the files and PostgreSQL for ownership, dimensions, sizes, and blob identifiers. Static PNG, JPG, and JPEG are accepted. Animated PNG, GIF, SVG, video, and audio are not supported in this version. Every endpoint requires a Bearer token; the owner or an admin can read/download/delete an individual image. Listing always returns the caller's own images.

| Method | Path | Result |
| --- | --- | --- |
| POST | /api/v1/media | Upload exactly one multipart file field named file; 201 and Location |
| GET | /api/v1/media?limit=20&offset=0 | List caller's images, newest first; 200 |
| GET | /api/v1/media/{id} | Image metadata and relative download URLs; 200 |
| GET | /api/v1/media/{id}/content | Stream the smaller optimized copy, or the original when optimization is unnecessary; 200 |
| GET | /api/v1/media/{id}/original | Download the unchanged original; 200 |
| DELETE | /api/v1/media/{id} | Hide the metadata and remove both blobs; 204 |

The content_url and original_url require the same Authorization header. A plain browser img tag cannot supply a Bearer header; fetch the content with authorization and create a temporary object URL in the frontend. URLs never contain account keys or SAS credentials. Public listing/marketplace sharing and CDN access require a separate access policy.

### Image size and compression policy

There is no universal industry file size. [Shopify's general image uploader](https://help.shopify.com/en/manual/online-store/images/theme-images) permits 20 MB and 20 megapixels. [Its product image guidance](https://help.shopify.com/en/manual/products/product-media/product-media-types) recommends 2048 x 2048 for square product images, with a different product-specific megapixel ceiling. [Google Photos storage saver](https://support.google.com/photos/answer/6220791) resizes images above 16 megapixels. These are platform policies, not guarantees about a suitable byte size for every image.

Campus Hub accepts at most 20 MiB (20,971,520 bytes) and 20 megapixels (width x height). This is an application policy inspired by those limits; MiB differs from decimal MB. The server checks the extension and decodes the image to validate the contents, dimensions, and corruption. Browser-provided MIME types do not establish trust.

Images larger than MEDIA_COMPRESSION_THRESHOLD_BYTES (default 1 MiB) are processed with FFmpeg. This threshold is our configurable optimization trigger, not an industry standard or promised output size:

- Dimensions are preserved; there is no downscaling, upscaling, or cropping. Orientation correction may swap width and height.
- PNG uses lossless compression level 9 and retains transparency.
- JPEG uses FFmpeg quality scale 2 with 4:4:4 chroma sampling. JPEG recompression is lossy even at high quality; it cannot guarantee pixel-identical output.
- A candidate is used only when it is valid, preserves resolution, and is smaller than the original. No repeated quality reduction tries to force a fixed kilobyte target.
- The unchanged original is retained in the private container for recovery or future variants. An optimized upload therefore uses storage for both copies. Unoptimized images use one blob.

FFmpeg runs without a shell, using server-generated temporary paths, a 20-second processing timeout, one codec thread, and a file/pipe protocol allowlist. Two uploads can be processed concurrently. Temporary files are removed after the request. Deletion first hides the database row and retains a tombstone; retrying DELETE can finish failed blob cleanup. Failed uploads attempt to remove any blobs already written. Blob storage and PostgreSQL are separate systems; persistent cleanup failures require an operational reconciliation job, which is not implemented here.

### Enable media when Azure is ready

Existing Azure placeholders can remain in .env. MEDIA_ENABLED defaults to false: authenticated media requests return 503 while the rest of the API works normally. There is no local-storage fallback and no fake upload success.

Once you have created an Azure storage account and a container with public access disabled:

1. Replace AZURE_STORAGE_ACCOUNT_NAME, AZURE_STORAGE_ACCOUNT_KEY, AZURE_STORAGE_CONTAINER_NAME, and AZURE_STORAGE_BLOB_URL in .env. The URL is the HTTPS account root (or that configured container's URL), without a SAS query string. These settings use Azure public-cloud endpoints.
2. Install FFmpeg and place it on PATH, or set FFMPEG_PATH to its executable. Docker includes FFmpeg. Shared-key credentials are supported for your current configuration; managed identity can replace them in a future deployment.
3. Apply migration 000004 if the first three have already run:

~~~sh
psql -h localhost -p 5432 -U postgres -d campus_hub -v ON_ERROR_STOP=1 -1 -f migrations/000004_create_media.up.sql
~~~

4. Set MEDIA_ENABLED=true and restart the API. Client construction does not provision resources or contact Azure; the first upload will report a storage failure if the account/container/credentials are not ready.

~~~sh
curl -i http://127.0.0.1:8080/api/v1/media \
  -H 'Authorization: Bearer TOKEN_FROM_REGISTRATION_OR_LOGIN' \
  -F 'file=@/path/to/photo.jpg'
~~~

Do not send an owner_id: the API takes ownership from the authenticated account. Metadata includes id, owner_id, file_name, content_type, original_size, size, width, height, optimized, content_url, original_url, and created_at. The API does not automatically assign the uploaded image as a user's avatar.

## Docker Compose

~~~sh
docker compose up --build
~~~

Compose supplies DB_* settings with db as the internal database host and requires JWT_SECRET from .env or the process environment. The API is published at 127.0.0.1:8080; PostgreSQL at 127.0.0.1:5433 to avoid a local PostgreSQL port conflict.

POSTGRES_USER, POSTGRES_PASSWORD, and POSTGRES_DB configure Compose credentials. Local defaults are campushub, local_dev_password, and campushub. POSTGRES_PORT overrides the published database port and PORT the published API port. The API container connects to db:5432.

All four up migrations run automatically when PostgreSQL initializes a fresh volume. For an existing volume, apply only missing migrations. If the first two were applied, run:

~~~sh
docker compose exec db sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -1 -f /docker-entrypoint-initdb.d/000003_expand_user_schema.sql'
docker compose exec db sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -1 -f /docker-entrypoint-initdb.d/000004_create_media.sql'
~~~

Existing volumes retain their original database credentials. Editing environment variables does not change credentials already stored in PostgreSQL.

## Valkey

Compose includes the Valkey project's image, valkey/valkey:9.1.2-alpine, for queue and temporary application state. Set a separate VALKEY_PASSWORD in the ignored .env file (openssl rand -hex 32 generates one). Do not reuse JWT_SECRET.

Start just Valkey:

~~~sh
docker compose up -d --wait valkey
docker compose exec valkey valkey-cli ping
~~~

The response should be PONG. VALKEYCLI_AUTH inside the container supplies the password to valkey-cli, including the health check. The [Valkey CLI documentation](https://valkey.io/topics/cli/) recommends this environment variable instead of putting passwords in CLI arguments. The local development server password is configured through Compose; Docker administrators can inspect container configuration.

| Connection | Address |
| --- | --- |
| Go API running directly on your machine | 127.0.0.1:6380 |
| API or worker container on this Compose network | valkey:6379 |

VALKEY_PORT changes only the published host port, which defaults to 6380 to avoid conflicts with another Redis/Valkey instance on 6379. The host binding is restricted to 127.0.0.1, and the service requires authentication. This is a single local-development node. The Go API does not yet contain a Valkey client, job producers, or workers; creating the container prepares that infrastructure.

Configuration lives in docker/valkey/valkey.conf:

- A named valkey_data volume mounted at /data keeps files across container recreation. Removing the volume removes that data.
- AOF is enabled with appendfsync everysec, alongside periodic RDB snapshots, following the [Valkey persistence guidance](https://valkey.io/topics/persistence/). A disaster can still lose roughly the most recent second of writes; a volume is not an off-host backup.
- maxmemory is 256 MiB, with a 768 MiB container limit to leave room for persistence buffers and fork overhead.
- noeviction rejects memory-growing writes when full instead of evicting queue keys. Producers must handle that error. The [Valkey eviction documentation](https://valkey.io/topics/lru-cache/) explains the policy and recommends separate instances when mixing an evictable cache with persistent data. Future product caches should use a separate instance when they need LRU/LFU eviction.
- A health check verifies authenticated PING, restart is unless-stopped, and shutdown allows 30 seconds.

Suggested Campus Hub uses, based on Valkey's documented data structures:

| Use case | Mechanism | Application behavior still needed |
| --- | --- | --- |
| Email delivery, notification jobs, and future background image processing | [Streams and consumer groups](https://valkey.io/topics/streams-intro/) | Enqueue references/IDs, process with workers, acknowledge completed jobs, reclaim abandoned pending jobs, and define retry limits |
| A simple first-in, first-out queue | [Lists](https://valkey.io/topics/lists/) | Define acknowledgement and recovery behavior for worker failures |
| Live presence or notifications to connected clients | [Pub/Sub](https://valkey.io/topics/pubsub/) | Disconnected subscribers miss messages; use Streams for work that must be retained |
| Temporary carts or short-lived lookup data | [Hashes](https://valkey.io/topics/hashes/) and [key expiry](https://valkey.io/commands/expire/) | Apply TTLs and bound memory use; retain authoritative orders/inventory in PostgreSQL |

Keep image bytes in Azure Blob Storage and put media IDs in jobs. Worker processing should be idempotent because jobs can be delivered more than once. Acknowledging a stream entry does not remove it: define a retention/trimming policy that preserves entries still needed by workers.

Inspect the service without printing credentials:

~~~sh
docker compose ps valkey
docker compose logs --tail=50 valkey
docker compose exec valkey valkey-cli INFO persistence
~~~

## Published container images

The CD workflow publishes two separate images to GitHub Container Registry on pushes to main and on manual workflow_dispatch runs:

| Image | Contents |
| --- | --- |
| ghcr.io/bukho127/campushub_server | Go API and FFmpeg, built from Dockerfile |
| ghcr.io/bukho127/campushub_server-valkey | Valkey 9.1.2 Alpine with docker/valkey/valkey.conf bundled into the image |

Both receive latest and sha-<short-commit> tags. The namespace/repository is derived from GITHUB_REPOSITORY in lowercase, so forks publish under their own names. Each image has a separate build cache. CI and CD use the named targets in the single root Dockerfile:

```sh
docker build --target api -t campushub-api .
docker build --target valkey -t campushub-valkey .
```

The API is the default target, so `docker build .` also builds the API. Compose explicitly selects the `api` target for its API service.

Publication follows [GitHub's container publishing guidance](https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images), using GITHUB_TOKEN with packages: write. VALKEY_PASSWORD and .env are not included in the image or required for building it. Supply authentication at runtime as the local Compose service does. The image retains the upstream entrypoint and /data volume behavior.

Local Compose uses the upstream Valkey image with our configuration mounted into it. The published Valkey image bundles that same configuration for deployment. Publishing an image does not start or update deployed containers, and the workflow will run after these changes are committed and pushed to main (or invoked manually).

## Go code style

Follow [Effective Go's formatting guidance](https://go.dev/doc/effective_go#formatting) and the [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments). Use gofmt for indentation and alignment. Keep larger struct literals and nested option lists on multiple lines, group related operations with blank lines, and wrap conditions when it helps readability. Go does not impose a fixed line-length limit.

Format source and tests from the repository root:

~~~sh
gofmt -w cmd internal test
~~~

.editorconfig supplies Go whitespace settings for editors that support it. CI already checks gofmt output and rejects unformatted Go files.

## Tests

All tests live in test/.

~~~sh
go test ./...
go vet ./...
go build ./...
~~~

PostgreSQL integration tests opt in locally. Using the project's .env in Git Bash:

~~~sh
RUN_DATABASE_TESTS=1 go test ./test -run 'Test(Users|Media)Postgres' -count=1 -v
~~~

PowerShell:

~~~powershell
$env:RUN_DATABASE_TESTS = '1'
go test ./test -run 'Test(Users|Media)Postgres' -count=1 -v
Remove-Item Env:RUN_DATABASE_TESTS
~~~

Alternatively, set TEST_DATABASE_URL. The role needs permission to create schemas. Integration tests use an isolated schema inside a transaction and always roll it back. They cover migration and rollback, legacy data, CRUD, pagination, private-field storage, salted hashes, account defaults, database constraints, verification invalidation, soft deletion, and real JWT login/authorization/revocation. Unit tests additionally cover invalid signatures, forbidden algorithms, expired/malformed claims, ownership, admin permissions, and dotenv secret-redaction errors. Tests use a separate test signing key.

CI supplies PostgreSQL and runs integration tests, race detection, coverage, vet, formatting, compilation, and a Docker image build.

Media tests cover authenticated uploads, private downloads, ownership, format/size validation, optimization selection, original retention, failed-upload cleanup, deletion retries, Azure SDK request signing with a fake transport, and the media migration/rollback. No real Azure account is needed for tests. The real FFmpeg test runs when FFmpeg is installed; CI installs it and requires that test. Set FFMPEG_PATH locally if necessary, then run go test ./test -run TestFFmpegImageProcessing -v.

Local race detection requires CGO and a supported C compiler:

~~~sh
go test -race ./...
~~~
