CREATE TABLE media (
    id text PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    owner_id bigint NOT NULL REFERENCES users(id),
    file_name text NOT NULL CHECK (octet_length(file_name) BETWEEN 1 AND 255),
    content_type text NOT NULL CHECK (content_type IN ('image/png', 'image/jpeg')),
    size bigint NOT NULL CHECK (size BETWEEN 1 AND 20971520),
    original_size bigint NOT NULL CHECK (original_size BETWEEN 1 AND 20971520 AND size <= original_size),
    width integer NOT NULL CHECK (width > 0),
    height integer NOT NULL CHECK (height > 0 AND width::bigint * height <= 20000000),
    optimized boolean NOT NULL,
    blob_key text NOT NULL UNIQUE,
    original_blob_key text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamptz,
    CHECK (optimized = (blob_key <> original_blob_key))
);

CREATE INDEX media_owner_created_idx ON media (owner_id, created_at DESC, id DESC) WHERE deleted_at IS NULL;
