package test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Bukho127/CampusHub_server/internal/auth"
	"github.com/Bukho127/CampusHub_server/internal/media"
	"github.com/Bukho127/CampusHub_server/internal/server"
	"github.com/Bukho127/CampusHub_server/internal/users"
)

type mediaRepository struct {
	files       map[string]media.File
	createError error
}

func (r *mediaRepository) Create(ctx context.Context, f media.File) (media.File, error) {
	if r.createError != nil {
		return media.File{}, r.createError
	}
	f.CreatedAt = time.Now().UTC()
	r.files[f.ID] = f
	return f, nil
}
func (r *mediaRepository) Get(ctx context.Context, id string) (media.File, error) {
	f, ok := r.files[id]
	if !ok {
		return media.File{}, media.ErrNotFound
	}
	return f, nil
}
func (r *mediaRepository) List(ctx context.Context, owner int64, limit, offset int) ([]media.File, error) {
	files := make([]media.File, 0)
	for _, f := range r.files {
		if f.OwnerID == owner && f.DeletedAt == nil {
			files = append(files, f)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ID < files[j].ID })
	if offset >= len(files) {
		return nil, nil
	}
	files = files[offset:]
	if len(files) > limit {
		files = files[:limit]
	}
	return files, nil
}
func (r *mediaRepository) MarkDeleted(ctx context.Context, id string) error {
	f, ok := r.files[id]
	if !ok {
		return media.ErrNotFound
	}
	now := time.Now()
	f.DeletedAt = &now
	r.files[id] = f
	return nil
}

type mediaStorage struct {
	objects     map[string][]byte
	putError    error
	deleteError error
}

func (s *mediaStorage) Put(ctx context.Context, key string, file *os.File, kind string) error {
	data, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	s.objects[key] = data // Simulate an ambiguous failed response after a committed upload.
	return s.putError
}
func (s *mediaStorage) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	data, ok := s.objects[key]
	if !ok {
		return nil, media.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
func (s *mediaStorage) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.deleteError != nil {
		return s.deleteError
	}
	delete(s.objects, key)
	return nil
}

type mediaProcessor func(context.Context, string, string, string) error

func (p mediaProcessor) Optimize(ctx context.Context, input, output, kind string) error {
	return p(ctx, input, output, kind)
}

func imageFixture(t *testing.T, compressed bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: 180,
				G: uint8(y),
				B: 50,
				A: uint8(80 + x),
			})
		}
	}
	var data bytes.Buffer
	level := png.NoCompression
	if compressed {
		level = png.BestCompression
	}
	if err := (&png.Encoder{CompressionLevel: level}).Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func mediaFixture(t *testing.T, threshold int64) (*media.Service, *mediaRepository, *mediaStorage) {
	t.Helper()
	r := &mediaRepository{files: make(map[string]media.File)}
	s := &mediaStorage{objects: make(map[string][]byte)}
	p := mediaProcessor(func(ctx context.Context, input, output, kind string) error {
		return os.WriteFile(output, imageFixture(t, true), 0600)
	})
	return media.NewService(r, s, p, threshold), r, s
}

func mediaRouter(t *testing.T, uploads *media.Service) http.Handler {
	t.Helper()
	accounts := &authRepositoryStub{accountFn: func(ctx context.Context, id int64) (auth.Account, error) {
		role := "user"
		if id == 3 {
			role = "admin"
		}
		return auth.Account{
			ID:     id,
			Role:   role,
			Status: "active",
		}, nil
	}}
	authentication, err := auth.NewService(accounts, testTokens)
	if err != nil {
		t.Fatal(err)
	}
	return server.NewRouter(
		users.NewService(&userRepositoryStub{t: t}),
		authentication,
		uploads,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}

func mediaToken(t *testing.T, id int64) string {
	t.Helper()
	response, err := testTokens.Issue(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	return response.AccessToken
}

type uploadFile struct {
	field, name string
	data        []byte
}

func mediaUpload(t *testing.T, router http.Handler, files []uploadFile, fields map[string]string, token string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, f := range files {
		part, err := writer.CreateFormFile(f.field, f.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(f.data); err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/api/v1/media", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestMediaAPI(t *testing.T) {
	uploads, _, storage := mediaFixture(t, 0)
	router := mediaRouter(t, uploads)
	token := mediaToken(t, 1)
	data := imageFixture(t, true)
	response := mediaUpload(t, router, []uploadFile{{"file", "photo.png", data}}, nil, token)
	if response.Code != 201 {
		t.Fatalf("upload: %d %s", response.Code, response.Body)
	}
	var f media.File
	if err := json.Unmarshal(response.Body.Bytes(), &f); err != nil {
		t.Fatal(err)
	}
	if f.OwnerID != 1 ||
		f.Width != 32 ||
		f.Height != 24 ||
		f.Size != int64(len(data)) ||
		f.Optimized ||
		f.ContentURL == "" ||
		f.OriginalURL == "" {
		t.Fatal("invalid media metadata")
	}
	if strings.Contains(response.Body.String(), "blob_key") ||
		strings.Contains(response.Body.String(), "blob.core.windows.net") {
		t.Fatal("private storage details exposed")
	}
	path := "/api/v1/media/" + f.ID
	if response.Header().Get("Location") != path {
		t.Fatal("missing media Location header")
	}
	for _, target := range []string{path, f.ContentURL, f.OriginalURL} {
		if got := authRequest(router, "GET", target, "", mediaToken(t, 2)); got.Code != 404 {
			t.Fatal("another user accessed private media")
		}
		if got := authRequest(router, "GET", target, "", mediaToken(t, 3)); got.Code != 200 {
			t.Fatal("admin could not access media")
		}
	}
	for _, target := range []string{f.ContentURL, f.OriginalURL} {
		got := authRequest(router, "GET", target, "", token)
		if got.Code != 200 ||
			!bytes.Equal(got.Body.Bytes(), data) ||
			got.Header().Get("Content-Type") != "image/png" ||
			got.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("download changed the image or its headers")
		}
		if got := authRequest(router, "HEAD", target, "", token); got.Code != 200 || got.Body.Len() != 0 {
			t.Fatal("HEAD returned media bytes")
		}
	}
	var page media.Page
	if err := json.Unmarshal(authRequest(router, "GET", "/api/v1/media?limit=1&offset=0", "", token).Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Files) != 1 || page.Files[0].ID != f.ID {
		t.Fatal("owner list omitted uploaded image")
	}
	for _, id := range []int64{2, 3} {
		got := authRequest(router, "GET", "/api/v1/media", "", mediaToken(t, id))
		if !strings.Contains(got.Body.String(), `"files":[]`) {
			t.Fatal("list exposed another owner's files")
		}
	}
	if got := authRequest(router, "DELETE", path, "", mediaToken(t, 2)); got.Code != 404 {
		t.Fatal("another user deleted media")
	}
	for range 2 {
		if got := authRequest(router, "DELETE", path, "", token); got.Code != 204 {
			t.Fatal("delete or retry failed")
		}
	}
	if len(storage.objects) != 0 {
		t.Fatal("delete left blobs behind")
	}
	if got := authRequest(router, "GET", path, "", token); got.Code != 404 {
		t.Fatal("deleted media remained visible")
	}
}

func TestMediaUploadValidation(t *testing.T) {
	data := imageFixture(t, true)
	oversizedDimensions := bytes.Clone(data)
	binary.BigEndian.PutUint32(oversizedDimensions[16:20], 10000)
	binary.BigEndian.PutUint32(oversizedDimensions[20:24], 10000)
	binary.BigEndian.PutUint32(oversizedDimensions[29:33], crc32.ChecksumIEEE(oversizedDimensions[12:29]))
	animationChunk := make([]byte, 20)
	binary.BigEndian.PutUint32(animationChunk[:4], 8)
	copy(animationChunk[4:8], "acTL")
	binary.BigEndian.PutUint32(animationChunk[8:12], 2)
	binary.BigEndian.PutUint32(animationChunk[16:20], crc32.ChecksumIEEE(animationChunk[4:16]))
	animated := append(bytes.Clone(data[:33]), animationChunk...)
	animated = append(animated, data[33:]...)
	for _, tc := range []struct {
		name   string
		files  []uploadFile
		fields map[string]string
		status int
	}{
		{"empty", nil, nil, 400},
		{"wrong field", []uploadFile{{"avatar", "x.png", data}}, nil, 400},
		{"two files", []uploadFile{{"file", "x.png", data}, {"file", "y.png", data}}, nil, 400},
		{"owner spoof", []uploadFile{{"file", "x.png", data}}, map[string]string{"owner_id": "3"}, 400},
		{"SVG", []uploadFile{{"file", "x.svg", []byte("<svg></svg>")}}, nil, 415},
		{"GIF", []uploadFile{{"file", "x.gif", []byte("GIF89a")}}, nil, 415},
		{"spoofed PNG", []uploadFile{{"file", "x.png", []byte("<script>alert(1)</script>")}}, nil, 400},
		{"extension mismatch", []uploadFile{{"file", "x.jpg", data}}, nil, 415},
		{"truncated", []uploadFile{{"file", "x.png", data[:len(data)/2]}}, nil, 400},
		{"pixel limit", []uploadFile{{"file", "x.png", oversizedDimensions}}, nil, 400},
		{"animated PNG", []uploadFile{{"file", "x.png", animated}}, nil, 400},
		{"too large", []uploadFile{{"file", "x.png", make([]byte, media.MaxUploadBytes+1)}}, nil, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uploads, repo, storage := mediaFixture(t, 0)
			response := mediaUpload(t, mediaRouter(t, uploads), tc.files, tc.fields, mediaToken(t, 1))
			if response.Code != tc.status {
				t.Fatalf("got %d, want %d: %s", response.Code, tc.status, response.Body)
			}
			if len(repo.files) != 0 || len(storage.objects) != 0 {
				t.Fatal("invalid upload persisted")
			}
		})
	}
	uploads, _, _ := mediaFixture(t, 0)
	router := mediaRouter(t, uploads)
	if got := mediaUpload(t, router, []uploadFile{{"file", "x.png", data}}, nil, ""); got.Code != 401 {
		t.Fatal("anonymous upload accepted")
	}
	if got := authRequest(router, "POST", "/api/v1/media", "{}", mediaToken(t, 1)); got.Code != 415 {
		t.Fatal("JSON upload accepted")
	}
	if got := authRequest(mediaRouter(t, nil), "POST", "/api/v1/media", "", mediaToken(t, 1)); got.Code != 503 {
		t.Fatal("placeholder storage did not fail closed")
	}
	for _, query := range []string{
		"limit=0",
		"limit=101",
		"offset=-1",
		"offset=2147483648",
		"limit=1&limit=2",
	} {
		if got := authRequest(router, "GET", "/api/v1/media?"+query, "", mediaToken(t, 1)); got.Code != 400 {
			t.Fatal("invalid pagination accepted")
		}
	}
}

func TestMediaOptimizationAndCleanup(t *testing.T) {
	account := auth.Account{
		ID:     1,
		Role:   "user",
		Status: "active",
	}
	source, compressed := imageFixture(t, false), imageFixture(t, true)
	uploads, repo, storage := mediaFixture(t, 1)
	f, err := uploads.Upload(context.Background(), account, "photo.png", bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	if !f.Optimized ||
		f.Size != int64(len(compressed)) ||
		f.OriginalSize != int64(len(source)) ||
		f.Width != 32 ||
		f.Height != 24 {
		t.Fatal("optimization metadata is incorrect")
	}
	if !bytes.Equal(storage.objects[f.BlobKey], compressed) ||
		!bytes.Equal(storage.objects[f.OriginalBlobKey], source) {
		t.Fatal("optimized copy or original missing")
	}
	storage.deleteError = errors.New("temporary blob failure")
	if err := uploads.Delete(context.Background(), account, f.ID); err == nil {
		t.Fatal("blob deletion failure was hidden")
	}
	if _, err := uploads.Get(context.Background(), account, f.ID); !errors.Is(err, media.ErrNotFound) {
		t.Fatal("failed deletion left media accessible")
	}
	storage.deleteError = nil
	if err := uploads.Delete(context.Background(), account, f.ID); err != nil || len(storage.objects) != 0 {
		t.Fatal("deletion could not be retried")
	}
	for _, failStorage := range []bool{false, true} {
		repo.createError, storage.putError = nil, nil
		if failStorage {
			storage.putError = errors.New("upload failed after commit")
		} else {
			repo.createError = errors.New("database insert failed")
		}
		if _, err := uploads.Upload(context.Background(), account, "photo.png", bytes.NewReader(source)); err == nil {
			t.Fatal("failed upload reported success")
		}
		if len(storage.objects) != 0 {
			t.Fatal("failed upload left orphaned blobs")
		}
	}
	// A larger candidate must not replace a smaller original.
	processor := mediaProcessor(func(ctx context.Context, input, output, kind string) error { return os.WriteFile(output, source, 0600) })
	uploads = media.NewService(
		&mediaRepository{files: make(map[string]media.File)},
		&mediaStorage{objects: make(map[string][]byte)},
		processor,
		1,
	)
	f, err = uploads.Upload(context.Background(), account, "photo.png", bytes.NewReader(compressed))
	if err != nil || f.Optimized || f.Size != int64(len(compressed)) {
		t.Fatal("larger optimized candidate replaced the original")
	}
}

func TestFFmpegImageProcessing(t *testing.T) {
	processor, err := media.NewFFmpeg(os.Getenv("FFMPEG_PATH"))
	if err != nil {
		if os.Getenv("REQUIRE_FFMPEG_TESTS") == "1" {
			t.Fatal("FFmpeg is required for this test run")
		}
		t.Skip("install FFmpeg or set FFMPEG_PATH to run real encoder tests")
	}
	for _, kind := range []string{"image/png", "image/jpeg"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			input, output := filepath.Join(dir, "input"), filepath.Join(dir, "output")
			data := imageFixture(t, false)
			if kind == "image/jpeg" {
				img, _, err := image.Decode(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				var encoded bytes.Buffer
				if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 95}); err != nil {
					t.Fatal(err)
				}
				data = encoded.Bytes()
			}
			if err := os.WriteFile(input, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := processor.Optimize(context.Background(), input, output, kind); err != nil {
				t.Fatal(err)
			}
			result, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			before, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			after, _, err := image.Decode(bytes.NewReader(result))
			if err != nil {
				t.Fatal(err)
			}
			if after.Bounds() != before.Bounds() {
				t.Fatal("compression resized the image")
			}
			if kind == "image/png" {
				for y := 0; y < 24; y++ {
					for x := 0; x < 32; x++ {
						if color.NRGBA64Model.Convert(before.At(x, y)) != color.NRGBA64Model.Convert(after.At(x, y)) {
							t.Fatal("PNG compression changed pixels or transparency")
						}
					}
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := processor.Optimize(ctx, input, output, kind); !errors.Is(err, context.Canceled) {
				t.Fatal("encoder ignored cancellation")
			}
		})
	}
}
