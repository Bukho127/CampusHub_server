package media

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Bukho127/CampusHub_server/internal/auth"
)

// Storage keeps media in a private object store, never in a public file tree.
type Storage interface {
	Put(context.Context, string, *os.File, string) error
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}

type Service struct {
	repository Repository
	storage    Storage
	processor  Processor
	threshold  int64
	slots      chan struct{}
}

func NewService(repository Repository, storage Storage, processor Processor, threshold int64) *Service {
	if threshold <= 0 {
		threshold = DefaultCompressionThreshold
	}
	return &Service{
		repository: repository,
		storage:    storage,
		processor:  processor,
		threshold:  threshold,
		slots:      make(chan struct{}, 2),
	}
}

func (s *Service) Available() bool {
	return s != nil && s.repository != nil && s.storage != nil && s.processor != nil
}

func (s *Service) Upload(ctx context.Context, account auth.Account, name string, input io.Reader) (File, error) {
	if !s.Available() {
		return File{}, ErrUnavailable
	}
	if account.ID <= 0 {
		return File{}, auth.ErrUnauthorized
	}

	if !utf8.ValidString(name) ||
		len(name) < 1 ||
		len(name) > 255 ||
		strings.ContainsAny(name, `/\:`) ||
		strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return File{}, ErrInvalidName
	}

	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
		return File{}, ErrUnsupportedType
	}

	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return File{}, ErrBusy
	}

	if err := ctx.Err(); err != nil {
		return File{}, err
	}

	dir, err := os.MkdirTemp("", "campushub-media-")
	if err != nil {
		return File{}, err
	}

	defer os.RemoveAll(dir)
	sourcePath := filepath.Join(dir, "original"+ext)
	source, err := os.OpenFile(sourcePath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return File{}, err
	}

	defer source.Close()
	size, err := io.Copy(source, io.LimitReader(&contextReader{
		ctx:    ctx,
		source: input,
	}, MaxUploadBytes+1))
	if err != nil {
		return File{}, err
	}

	if size > MaxUploadBytes {
		return File{}, ErrTooLarge
	}

	contentType, width, height, err := inspectImage(source)
	if err != nil {
		return File{}, err
	}

	if (ext == ".png") != (contentType == "image/png") {
		return File{}, ErrUnsupportedType
	}

	f := File{
		OwnerID:      account.ID,
		FileName:     name,
		ContentType:  contentType,
		Size:         size,
		OriginalSize: size,
		Width:        width,
		Height:       height,
	}

	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return File{}, err
	}

	f.ID = hex.EncodeToString(nonce)
	prefix := "media/" + strconv.FormatInt(account.ID, 10) + "/" + f.ID + "/"
	f.OriginalBlobKey = prefix + "original" + ext
	f.BlobKey = f.OriginalBlobKey
	display := source
	if size > s.threshold {
		outputPath := filepath.Join(dir, "display"+ext)
		if err := s.processor.Optimize(ctx, sourcePath, outputPath, contentType); err != nil {
			return File{}, err
		}

		candidate, err := os.Open(outputPath)
		if err != nil {
			return File{}, err
		}
		defer candidate.Close()
		info, err := candidate.Stat()
		if err != nil {
			return File{}, err
		}
		if info.Size() < 1 || info.Size() > MaxUploadBytes {
			return File{}, ErrProcessing
		}
		kind, w, h, err := inspectImage(candidate)
		if err != nil || kind != contentType || !((w == width && h == height) || (w == height && h == width)) {
			return File{}, ErrProcessing
		}
		if info.Size() < size {
			display = candidate
			f.Size, f.Width, f.Height, f.Optimized = info.Size(), w, h, true
			f.BlobKey = prefix + "display" + ext
		}
	}

	// The original remains private and available for lossless recovery/reprocessing.
	keys := []string{f.OriginalBlobKey}
	if f.Optimized {
		keys = append(keys, f.BlobKey)
	}
	if err := s.put(ctx, f.OriginalBlobKey, source, contentType); err != nil {
		return File{}, s.cleanup(keys, err)
	}
	if f.Optimized {
		if err := s.put(ctx, f.BlobKey, display, contentType); err != nil {
			return File{}, s.cleanup(keys, err)
		}
	}
	created, err := s.repository.Create(ctx, f)
	if err != nil {
		return File{}, s.cleanup(keys, err)
	}

	created.setURLs()
	return created, nil
}

func (s *Service) put(ctx context.Context, key string, file *os.File, contentType string) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return s.storage.Put(ctx, key, file, contentType)
}

func (s *Service) cleanup(keys []string, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, key := range keys {
		if err := s.storage.Delete(ctx, key); err != nil {
			cause = errors.Join(cause, fmt.Errorf("clean up media object: %w", err))
		}
	}
	return cause
}

func inspectImage(file *os.File) (string, int, int, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", 0, 0, err
	}
	config, format, err := image.DecodeConfig(file)
	if err != nil ||
		(format != "jpeg" && format != "png") ||
		config.Width <= 0 ||
		config.Height <= 0 ||
		int64(config.Width)*int64(config.Height) > MaxPixels {
		return "", 0, 0, ErrInvalidImage
	}

	if format == "png" {
		// APNG must not silently become a still image through -frames:v 1.
		if _, err := file.Seek(8, io.SeekStart); err != nil {
			return "", 0, 0, err
		}
		var chunk [8]byte
		for {
			if _, err := io.ReadFull(file, chunk[:]); err != nil {
				return "", 0, 0, ErrInvalidImage
			}
			length := binary.BigEndian.Uint32(chunk[:4])
			if string(chunk[4:]) == "acTL" || length > MaxUploadBytes {
				return "", 0, 0, ErrInvalidImage
			}
			if string(chunk[4:]) == "IEND" {
				break
			}
			if _, err := file.Seek(int64(length)+4, io.SeekCurrent); err != nil {
				return "", 0, 0, ErrInvalidImage
			}
		}
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", 0, 0, err
	}

	// Decode the complete image, not just a spoofable header or MIME type.
	decoded, actual, err := image.Decode(file)
	if err != nil ||
		actual != format ||
		decoded.Bounds().Dx() != config.Width ||
		decoded.Bounds().Dy() != config.Height {
		return "", 0, 0, ErrInvalidImage
	}
	if format == "jpeg" {
		return "image/jpeg", config.Width, config.Height, nil
	}
	return "image/png", config.Width, config.Height, nil
}

func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'f' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func (s *Service) owned(ctx context.Context, account auth.Account, id string) (File, error) {
	if !s.Available() {
		return File{}, ErrUnavailable
	}
	if !validID(id) {
		return File{}, ErrNotFound
	}
	f, err := s.repository.Get(ctx, id)
	if err != nil {
		return File{}, err
	}
	if account.ID <= 0 || (f.OwnerID != account.ID && account.Role != "admin") {
		return File{}, ErrNotFound
	}
	f.setURLs()
	return f, nil
}

func (s *Service) Get(ctx context.Context, account auth.Account, id string) (File, error) {
	f, err := s.owned(ctx, account, id)
	if err != nil {
		return File{}, err
	}
	if f.DeletedAt != nil {
		return File{}, ErrNotFound
	}
	return f, nil
}

func (s *Service) List(ctx context.Context, account auth.Account, limit, offset int) (Page, error) {
	if !s.Available() {
		return Page{}, ErrUnavailable
	}
	files, err := s.repository.List(ctx, account.ID, limit, offset)
	if err != nil {
		return Page{}, err
	}
	if files == nil {
		files = []File{}
	}
	for i := range files {
		files[i].setURLs()
	}
	return Page{
		Files:  files,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func (s *Service) Open(ctx context.Context, account auth.Account, id string, original bool) (File, io.ReadCloser, error) {
	f, err := s.Get(ctx, account, id)
	if err != nil {
		return File{}, nil, err
	}
	key := f.BlobKey
	if original {
		key = f.OriginalBlobKey
	}
	stream, err := s.storage.Open(ctx, key)
	return f, stream, err
}

func (s *Service) Delete(ctx context.Context, account auth.Account, id string) error {
	f, err := s.owned(ctx, account, id)
	if err != nil {
		return err
	}

	// Hide the record first; retain a tombstone so storage failures can be retried.
	if err := s.repository.MarkDeleted(ctx, id); err != nil {
		return err
	}

	if err := s.storage.Delete(ctx, f.OriginalBlobKey); err != nil {
		return err
	}

	if f.BlobKey != f.OriginalBlobKey {
		return s.storage.Delete(ctx, f.BlobKey)
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	source io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(p)
}
