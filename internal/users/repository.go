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

// PostgresRepository stores user profiles in PostgreSQL.
type PostgresRepository struct {
	db Database
}

func NewPostgresRepository(db Database) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) Create(ctx context.Context, input CreateParams) (User, error) {
	return scanUser(r.db.QueryRow(ctx, `
		INSERT INTO users (name, email, password_hash) VALUES ($1, $2, $3)
		RETURNING id, name, email, created_at, updated_at`, input.Name, input.Email, input.PasswordHash))
}

func (r *PostgresRepository) Get(ctx context.Context, id int64) (User, error) {
	return scanUser(r.db.QueryRow(ctx, `
		SELECT id, name, email, created_at, updated_at FROM users WHERE id = $1`, id))
}

func (r *PostgresRepository) List(ctx context.Context, limit, offset int) ([]User, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, name, email, created_at, updated_at FROM users
		ORDER BY id LIMIT $1 OFFSET $2`, limit, offset)
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
		UPDATE users SET name = $2, email = $3, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 RETURNING id, name, email, created_at, updated_at`, id, input.Name, input.Email))
}

func (r *PostgresRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
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
	err := row.Scan(&profile.ID, &profile.Name, &profile.Email, &profile.CreatedAt, &profile.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" && postgresError.ConstraintName == "users_email_key" {
		return User{}, ErrEmailExists
	}
	return profile, err
}
