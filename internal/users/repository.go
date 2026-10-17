package users

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Database is the query interface implemented by a pgx pool or transaction.
type Database interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresRepository struct {
	db Database
}

func NewPostgresRepository(db Database) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// Profile queries never select private identity data, credential hashes, or tokens.
const publicColumns = `
	id, first_name, last_name, display_name, email, email_verified,
	institution_id, identity_type, role, status, COALESCE(ban_reason, ''), banned_until, deleted_at,
	email_verification_status, vendor_verification_status,
	campus_email_verification_status, campus_email_verified_at, rating, review_count,
	COALESCE(location, ''), COALESCE(avatar, ''), created_at, updated_at`

func (r *PostgresRepository) Create(ctx context.Context, input CreateParams) (User, error) {
	return scanUser(r.db.QueryRow(ctx, `
		INSERT INTO users (
			first_name, last_name, display_name, email, password_hash, identity_type,
			institution_id, student_number, campus_email, location, avatar
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, ''), NULLIF($11, ''))
		RETURNING `+publicColumns,
		input.Profile.FirstName, input.Profile.LastName, input.Profile.DisplayName, input.Profile.Email,
		input.PasswordHash, input.IdentityType, input.InstitutionID, input.StudentNumber,
		input.CampusEmail, input.Profile.Location, input.Profile.Avatar))
}

func (r *PostgresRepository) Get(ctx context.Context, id int64) (User, error) {
	return scanUser(r.db.QueryRow(ctx, "SELECT "+publicColumns+" FROM users WHERE id = $1 AND status <> 'deleted'", id))
}

func (r *PostgresRepository) List(ctx context.Context, limit, offset int) ([]User, error) {
	rows, err := r.db.Query(ctx, "SELECT "+publicColumns+" FROM users WHERE status <> 'deleted' ORDER BY id LIMIT $1 OFFSET $2", limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	profiles := make([]User, 0)
	for rows.Next() {
		profile, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

func (r *PostgresRepository) Update(ctx context.Context, id int64, input ProfileInput) (User, error) {
	return scanUser(r.db.QueryRow(ctx, `
		UPDATE users SET first_name = $2, last_name = $3, display_name = $4,
			email = $5, location = NULLIF($6, ''), avatar = NULLIF($7, ''), updated_at = CURRENT_TIMESTAMP,
			email_verification_status = CASE WHEN email <> $5 THEN 'unverified' ELSE email_verification_status END,
			verify_code_hash = CASE WHEN email <> $5 THEN NULL ELSE verify_code_hash END,
			verify_code_expires = CASE WHEN email <> $5 THEN NULL ELSE verify_code_expires END,
			verify_attempts = CASE WHEN email <> $5 THEN 0 ELSE verify_attempts END,
			password_reset_token_hash = CASE WHEN email <> $5 THEN NULL ELSE password_reset_token_hash END,
			password_reset_expires_at = CASE WHEN email <> $5 THEN NULL ELSE password_reset_expires_at END,
			token_version = CASE WHEN email <> $5 THEN token_version + 1 ELSE token_version END
		WHERE id = $1 AND status <> 'deleted' RETURNING `+publicColumns,
		id, input.FirstName, input.LastName, input.DisplayName, input.Email, input.Location, input.Avatar))
}

func (r *PostgresRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.Exec(ctx, `
		UPDATE users SET status = 'deleted', deleted_at = CURRENT_TIMESTAMP,
			updated_at = CURRENT_TIMESTAMP, token_version = token_version + 1,
			verify_code_hash = NULL, verify_code_expires = NULL,
			password_reset_token_hash = NULL, password_reset_expires_at = NULL,
			campus_email_verification_token_hash = NULL, campus_email_verification_expires_at = NULL
		WHERE id = $1 AND status <> 'deleted'`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanUser(row pgx.Row) (User, error) {
	var profile User
	err := row.Scan(
		&profile.ID, &profile.FirstName, &profile.LastName, &profile.DisplayName, &profile.Email, &profile.EmailVerified,
		&profile.InstitutionID, &profile.IdentityType, &profile.Role, &profile.Status, &profile.BanReason, &profile.BannedUntil, &profile.DeletedAt,
		&profile.EmailVerificationStatus, &profile.VendorVerificationStatus,
		&profile.CampusEmailVerificationStatus, &profile.CampusEmailVerifiedAt, &profile.Rating, &profile.ReviewCount,
		&profile.Location, &profile.Avatar, &profile.CreatedAt, &profile.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" && postgresError.ConstraintName == "users_email_key" {
		return User{}, ErrEmailExists
	}
	return profile, err
}
