-- Existing profiles keep NULL until a password is set through a future authenticated flow.
ALTER TABLE users ADD COLUMN password_hash text
    CONSTRAINT users_password_hash_check
    CHECK (password_hash IS NULL OR password_hash LIKE '$argon2id$%');
