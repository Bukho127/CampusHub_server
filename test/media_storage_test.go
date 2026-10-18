package test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Bukho127/CampusHub_server/internal/config"
	"github.com/Bukho127/CampusHub_server/internal/media"
)

type blobTransport func(*http.Request) (*http.Response, error)

func (f blobTransport) Do(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestAzureMediaStorage(t *testing.T) {
	objects := make(map[string][]byte)
	transport := blobTransport(func(r *http.Request) (*http.Response, error) {
		// The SDK writes some lower-case keys directly. A real HTTP transport
		// serializes them case-insensitively; normalize them in this fake too.
		headers := make(http.Header)
		for key, values := range r.Header {
			headers[http.CanonicalHeaderKey(key)] = values
		}
		r.Header = headers
		if r.URL.Scheme != "https" ||
			r.URL.Host != "campusmedia.blob.core.windows.net" ||
			!strings.HasPrefix(r.URL.Path, "/private-media/") {
			t.Fatal("unexpected blob endpoint or duplicate container path")
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "SharedKey campusmedia:") {
			t.Fatal("blob request was not signed")
		}
		response := &http.Response{
			Request:    r,
			Header:     make(http.Header),
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("")),
		}
		response.Header.Set("ETag", `"test-etag"`)
		response.Header.Set("Last-Modified", "Sun, 18 Oct 2026 00:00:00 GMT")
		missing := func() {
			response.StatusCode = 404
			response.Header.Set("x-ms-error-code", "BlobNotFound")
			response.Body = io.NopCloser(strings.NewReader(`<Error><Code>BlobNotFound</Code></Error>`))
		}
		switch r.Method {
		case http.MethodPut:
			if r.Header.Get("If-None-Match") != "*" ||
				r.Header.Get("x-ms-blob-content-type") != "image/png" ||
				r.Header.Get("x-ms-blob-cache-control") != "private, no-store" {
				t.Fatal("missing blob overwrite guard or content headers")
			}
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			objects[r.URL.Path] = data
			response.StatusCode = 201
		case http.MethodGet:
			data, ok := objects[r.URL.Path]
			if !ok {
				missing()
				break
			}
			response.ContentLength = int64(len(data))
			response.Header.Set("Content-Length", strconv.Itoa(len(data)))
			response.Body = io.NopCloser(bytes.NewReader(data))
		case http.MethodDelete:
			if _, ok := objects[r.URL.Path]; !ok {
				missing()
				break
			}
			delete(objects, r.URL.Path)
			response.StatusCode = 202
		default:
			t.Fatalf("unexpected blob method %s", r.Method)
		}
		return response, nil
	})
	storage, err := media.NewAzureStorage(media.BlobConfig{
		AccountName:   "campusmedia",
		AccountKey:    base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 64)),
		ContainerName: "private-media",
		ServiceURL:    "https://campusmedia.blob.core.windows.net/private-media",
	}, &azblob.ClientOptions{ClientOptions: azcore.ClientOptions{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "image.png")
	data := imageFixture(t, true)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	ctx := context.Background()
	key := "media/1/test.png"
	if err := storage.Put(ctx, key, input, "image/png"); err != nil {
		t.Fatal(err)
	}
	stream, err := storage.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(stream)
	stream.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("blob download changed bytes")
	}
	for range 2 {
		if err := storage.Delete(ctx, key); err != nil {
			t.Fatal("blob deletion was not idempotent")
		}
	}
	if _, err := storage.Open(ctx, key); !errors.Is(err, media.ErrNotFound) {
		t.Fatal("missing blob did not become media not found")
	}
}

func TestMediaConfiguration(t *testing.T) {
	databaseEnvironment(t)
	t.Setenv("AZURE_STORAGE_ACCOUNT_KEY", "private-test-sentinel")
	settings, err := config.Load()
	if err != nil ||
		settings.Media.Enabled ||
		settings.Media.CompressionThreshold != media.DefaultCompressionThreshold {
		t.Fatal("media must remain disabled with placeholders")
	}
	for _, value := range []string{"0", "-1", "20971521", "not-a-number"} {
		t.Setenv("MEDIA_COMPRESSION_THRESHOLD_BYTES", value)
		if _, err := config.Load(); err == nil || strings.Contains(err.Error(), "private-test-sentinel") {
			t.Fatal("invalid media threshold accepted or secret exposed")
		}
	}
	t.Setenv("MEDIA_COMPRESSION_THRESHOLD_BYTES", "2097152")
	t.Setenv("MEDIA_ENABLED", "true")
	settings, err = config.Load()
	if err != nil || !settings.Media.Enabled || settings.Media.CompressionThreshold != 2<<20 {
		t.Fatal("media environment was not loaded")
	}
	t.Setenv("MEDIA_ENABLED", "invalid")
	if _, err := config.Load(); err == nil {
		t.Fatal("invalid media enable flag accepted")
	}
	for _, endpoint := range []string{"http://campusmedia.blob.core.windows.net", "https://untrusted.example", "https://campusmedia.blob.core.windows.net/?sig=private-test-sentinel"} {
		_, err := media.NewAzureStorage(
			media.BlobConfig{
				AccountName:   "campusmedia",
				ContainerName: "private-media",
				AccountKey:    "private-test-sentinel",
				ServiceURL:    endpoint,
			},
			nil,
		)
		if err == nil || strings.Contains(err.Error(), "private-test-sentinel") {
			t.Fatal("invalid endpoint accepted or secret exposed")
		}
	}
}
