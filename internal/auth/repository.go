package auth

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Database interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type PostgresRepository struct {
	db Database
}

func NewPostgresRepository(db Database) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// CredentialsByEmail is the only account lookup that reads password hashes.
func (r *PostgresRepository) CredentialsByEmail(ctx context.Context, email string) (Credentials, error) {
	var credentials Credentials
	err := r.db.QueryRow(ctx, `
		SELECT id, role, status, token_version, COALESCE(password_hash, '')
		FROM users WHERE email = $1`, email).Scan(
		&credentials.ID,
		&credentials.Role,
		&credentials.Status,
		&credentials.TokenVersion,
		&credentials.PasswordHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrAccountNotFound
	}
	return credentials, err
}

func (r *PostgresRepository) AccountByID(ctx context.Context, id int64) (Account, error) {
	var account Account
	err := r.db.QueryRow(ctx, "SELECT id, role, status, token_version FROM users WHERE id = $1", id).Scan(
		&account.ID,
		&account.Role,
		&account.Status,
		&account.TokenVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrAccountNotFound
	}
	return account, err
}

func (r *PostgresRepository) Revoke(ctx context.Context, id int64) error {
	result, err := r.db.Exec(ctx, "UPDATE users SET token_version = token_version + 1 WHERE id = $1 AND status = 'active'", id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrUnauthorized
	}
	return nil
}
