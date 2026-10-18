package media

import (
	"errors"
	"time"
)

const (
	MaxUploadBytes              = 20 << 20
	MaxPixels                   = 20_000_000
	DefaultCompressionThreshold = 1 << 20
)

var (
	ErrUnavailable     = errors.New("media storage or image processing is not configured")
	ErrNotFound        = errors.New("media not found")
	ErrInvalidImage    = errors.New("file must be a valid static PNG or JPEG image of at most 20 megapixels")
	ErrUnsupportedType = errors.New("only .png, .jpg, and .jpeg images are supported")
	ErrTooLarge        = errors.New("image exceeds the 20 MiB upload limit")
	ErrInvalidName     = errors.New("file name must contain 1 to 255 bytes without path separators or control characters")
	ErrBusy            = errors.New("image processing is busy; try again shortly")
	ErrProcessing      = errors.New("image processing failed")
)

// File describes private media. Blob names and deletion state stay on the server.
type File struct {
	ID              string     `json:"id"`
	OwnerID         int64      `json:"owner_id"`
	FileName        string     `json:"file_name"`
	ContentType     string     `json:"content_type"`
	Size            int64      `json:"size"`
	OriginalSize    int64      `json:"original_size"`
	Width           int        `json:"width"`
	Height          int        `json:"height"`
	Optimized       bool       `json:"optimized"`
	ContentURL      string     `json:"content_url"`
	OriginalURL     string     `json:"original_url"`
	CreatedAt       time.Time  `json:"created_at"`
	BlobKey         string     `json:"-"`
	OriginalBlobKey string     `json:"-"`
	DeletedAt       *time.Time `json:"-"`
}

func (f *File) setURLs() {
	f.ContentURL = "/api/v1/media/" + f.ID + "/content"
	f.OriginalURL = "/api/v1/media/" + f.ID + "/original"
}

type Page struct {
	Files  []File `json:"files"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}
