-- Preserve the legacy display name; do not invent given/family names or identity.
ALTER TABLE users RENAME COLUMN name TO display_name;

ALTER TABLE users
    ADD COLUMN first_name text NOT NULL DEFAULT '' CHECK (char_length(first_name) <= 100),
    ADD COLUMN last_name text NOT NULL DEFAULT '' CHECK (char_length(last_name) <= 100),
    ADD COLUMN institution_id bigint CHECK (institution_id > 0),
    ADD COLUMN student_number text CHECK (char_length(student_number) <= 128),
    ADD COLUMN identity_type text CHECK (identity_type IN ('student', 'faculty', 'resident', 'vendor')),
    ADD COLUMN role text NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'vendor', 'admin')),
    ADD COLUMN status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'banned', 'deleted')),
    ADD COLUMN ban_reason text CHECK (char_length(ban_reason) <= 1000),
    ADD COLUMN banned_until timestamptz,
    ADD COLUMN deleted_at timestamptz,
    ADD COLUMN email_verification_status text NOT NULL DEFAULT 'unverified'
        CHECK (email_verification_status IN ('unverified', 'pending', 'verified')),
    ADD COLUMN email_verified boolean GENERATED ALWAYS AS (email_verification_status = 'verified') STORED,
    ADD COLUMN vendor_verification_status text NOT NULL DEFAULT 'unverified'
        CHECK (vendor_verification_status IN ('unverified', 'pending', 'verified')),
    ADD COLUMN verify_code_hash text,
    ADD COLUMN verify_code_expires timestamptz,
    ADD COLUMN verify_attempts bigint NOT NULL DEFAULT 0 CHECK (verify_attempts >= 0),
    ADD COLUMN password_reset_token_hash text,
    ADD COLUMN password_reset_expires_at timestamptz,
    ADD COLUMN token_version bigint NOT NULL DEFAULT 0 CHECK (token_version >= 0),
    ADD COLUMN rating double precision CHECK (rating BETWEEN 0 AND 5),
    ADD COLUMN review_count bigint NOT NULL DEFAULT 0 CHECK (review_count >= 0),
    ADD COLUMN campus_email text CHECK (char_length(campus_email) <= 254 AND campus_email = lower(btrim(campus_email))),
    ADD COLUMN campus_email_verification_status text NOT NULL DEFAULT 'unverified'
        CHECK (campus_email_verification_status IN ('unverified', 'pending', 'verified')),
    ADD COLUMN campus_email_verified_at timestamptz,
    ADD COLUMN campus_email_verification_token_hash text,
    ADD COLUMN campus_email_verification_expires_at timestamptz,
    ADD COLUMN location text CHECK (char_length(location) <= 255),
    ADD COLUMN avatar text CHECK (char_length(avatar) <= 2048);

-- A foreign key for institution_id will be added with the institution schema.
-- Both legacy reset-token pairs map to password_reset_token_hash/password_reset_expires_at.
