package media

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Repository interface {
	Create(context.Context, File) (File, error)
	Get(context.Context, string) (File, error)
	List(context.Context, int64, int, int) ([]File, error)
	MarkDeleted(context.Context, string) error
}

type Database interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresRepository struct{ db Database }

func NewPostgresRepository(db Database) *PostgresRepository { return &PostgresRepository{db: db} }

const columns = `id, owner_id, file_name, content_type, size, original_size,
    width, height, optimized, blob_key, original_blob_key, created_at, deleted_at`

func (r *PostgresRepository) Create(ctx context.Context, f File) (File, error) {
	return scanFile(r.db.QueryRow(ctx, `INSERT INTO media
		(id, owner_id, file_name, content_type, size, original_size, width, height, optimized, blob_key, original_blob_key)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+columns,
		f.ID, f.OwnerID, f.FileName, f.ContentType, f.Size, f.OriginalSize,
		f.Width, f.Height, f.Optimized, f.BlobKey, f.OriginalBlobKey))
}

// Get includes tombstones so failed blob deletions can be retried by the owner.
func (r *PostgresRepository) Get(ctx context.Context, id string) (File, error) {
	return scanFile(r.db.QueryRow(ctx, "SELECT "+columns+" FROM media WHERE id = $1", id))
}

func (r *PostgresRepository) List(ctx context.Context, ownerID int64, limit, offset int) ([]File, error) {
	rows, err := r.db.Query(ctx, "SELECT "+columns+" FROM media WHERE owner_id = $1 AND deleted_at IS NULL ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3", ownerID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := make([]File, 0)
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (r *PostgresRepository) MarkDeleted(ctx context.Context, id string) error {
	result, err := r.db.Exec(ctx, "UPDATE media SET deleted_at = COALESCE(deleted_at, CURRENT_TIMESTAMP) WHERE id = $1", id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanFile(row pgx.Row) (File, error) {
	var f File
	err := row.Scan(&f.ID, &f.OwnerID, &f.FileName, &f.ContentType, &f.Size, &f.OriginalSize,
		&f.Width, &f.Height, &f.Optimized, &f.BlobKey, &f.OriginalBlobKey, &f.CreatedAt, &f.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return File{}, ErrNotFound
	}
	f.setURLs()
	return f, err
}
