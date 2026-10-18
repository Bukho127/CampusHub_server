package media

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
)

type BlobConfig struct {
	AccountName   string
	AccountKey    string `json:"-"`
	ContainerName string
	ServiceURL    string
}

type AzureStorage struct {
	client    *azblob.Client
	container string
}

var accountPattern = regexp.MustCompile(`^[a-z0-9]{3,24}$`)
var containerPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

// NewAzureStorage configures a client without creating resources or contacting Azure.
// The container must already exist with public access disabled.
func NewAzureStorage(settings BlobConfig, options *azblob.ClientOptions) (*AzureStorage, error) {
	if !accountPattern.MatchString(settings.AccountName) ||
		!containerPattern.MatchString(settings.ContainerName) ||
		strings.Contains(settings.ContainerName, "--") {
		return nil, errors.New("set valid AZURE_STORAGE_ACCOUNT_NAME and AZURE_STORAGE_CONTAINER_NAME")
	}

	endpoint := settings.ServiceURL
	host := settings.AccountName + ".blob.core.windows.net"
	if endpoint == "" {
		endpoint = "https://" + host
	}

	parsed, err := url.Parse(endpoint)
	if err != nil ||
		parsed.Scheme != "https" ||
		parsed.Host != host ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		(parsed.Path != "" &&
			parsed.Path != "/" &&
			parsed.Path != "/"+settings.ContainerName) {
		return nil, errors.New("AZURE_STORAGE_BLOB_URL must be the HTTPS Azure account URL (or its configured container URL), without a SAS query")
	}
	parsed.Path, parsed.RawPath = "/", ""

	credential, err := azblob.NewSharedKeyCredential(settings.AccountName, settings.AccountKey)
	if err != nil || settings.AccountKey == "" {
		return nil, errors.New("set a valid AZURE_STORAGE_ACCOUNT_KEY")
	}

	client, err := azblob.NewClientWithSharedKeyCredential(parsed.String(), credential, options)
	if err != nil {
		return nil, errors.New("could not configure Azure Blob Storage")
	}

	return &AzureStorage{
		client:    client,
		container: settings.ContainerName,
	}, nil
}

func (s *AzureStorage) Put(ctx context.Context, key string, file *os.File, contentType string) error {
	cache := "private, no-store"
	star := azcore.ETag("*")
	_, err := s.client.UploadFile(
		ctx,
		s.container,
		key,
		file,
		&azblob.UploadFileOptions{
			Concurrency: 1,
			HTTPHeaders: &blob.HTTPHeaders{
				BlobContentType:  &contentType,
				BlobCacheControl: &cache,
			},
			AccessConditions: &blob.AccessConditions{
				ModifiedAccessConditions: &blob.ModifiedAccessConditions{
					IfNoneMatch: &star,
				},
			},
		},
	)
	return err
}

func (s *AzureStorage) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	response, err := s.client.DownloadStream(ctx, s.container, key, nil)
	if bloberror.HasCode(err, bloberror.BlobNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return response.NewRetryReader(ctx, nil), nil
}

func (s *AzureStorage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteBlob(ctx, s.container, key, nil)
	if bloberror.HasCode(err, bloberror.BlobNotFound) {
		return nil
	}
	return err
}
