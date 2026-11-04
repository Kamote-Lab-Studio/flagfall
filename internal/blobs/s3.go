package blobs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/Kamote-Lab-Studio/flagfall/internal/config"
)

// RedirectTTL is how long a signed download link works.
const RedirectTTL = 5 * time.Minute

// FromConfig opens the store the config asks for: S3 when a bucket is set,
// else DataDir/files. Temporary files go to DataDir/files either way.
func FromConfig(cfg config.Config) (*Store, error) {
	local := LocalDir(cfg.DataDir)
	if cfg.S3.Bucket == "" {
		return Local(local), nil
	}
	return S3(cfg.S3, local, nil)
}

// S3 stores blobs in a bucket. transport is for tests; nil means the default.
func S3(c config.S3Config, spool string, transport http.RoundTripper) (*Store, error) {
	endpoint, secure, err := c.EndpointHost()
	if err != nil {
		return nil, err
	}
	var creds *credentials.Credentials
	if c.AccessKey != "" {
		creds = credentials.NewStaticV4(c.AccessKey, c.SecretKey, "")
	} else {
		// AWS_ACCESS_KEY_ID and friends, ~/.aws/credentials, then an
		// instance or task role.
		creds = credentials.NewChainCredentials([]credentials.Provider{
			&credentials.EnvAWS{}, &credentials.FileAWSCredentials{}, &credentials.IAM{},
		})
	}
	opts := &minio.Options{Creds: creds, Secure: secure, Region: c.Region, Transport: transport}
	if c.PathStyle {
		opts.BucketLookup = minio.BucketLookupPath
	}
	cl, err := minio.New(endpoint, opts)
	if err != nil {
		return nil, fmt.Errorf("S3 storage: %w", err)
	}
	b := &s3Backend{cl: cl, bucket: c.Bucket, prefix: c.KeyPrefix()}
	scheme := "https"
	if !secure {
		scheme = "http"
	}
	s := &Store{b: b, spool: spool, Kind: "s3", Location: scheme + "://" + endpoint + "/" + c.Bucket + "/" + b.prefix}
	if c.Redirect {
		s.Redirect = b.signedURL
	}
	return s, nil
}

type s3Backend struct {
	cl     *minio.Client
	bucket string
	prefix string
}

func (b *s3Backend) put(ctx context.Context, key string, r io.Reader, size int64) error {
	_, err := b.cl.PutObject(ctx, b.bucket, b.prefix+key, r, size, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
		// Content-addressed: the bytes under a key never change.
		CacheControl: "private, max-age=31536000, immutable",
	})
	return wrapS3(err)
}

func (b *s3Backend) open(ctx context.Context, key string) (io.ReadSeekCloser, error) {
	obj, err := b.cl.GetObject(ctx, b.bucket, b.prefix+key, minio.GetObjectOptions{})
	if err != nil {
		return nil, wrapS3(err)
	}
	// GetObject is lazy; Stat makes a missing key fail here, not mid-download.
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		if isNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, wrapS3(err)
	}
	return obj, nil
}

func (b *s3Backend) exists(ctx context.Context, key string) (bool, error) {
	_, err := b.cl.StatObject(ctx, b.bucket, b.prefix+key, minio.StatObjectOptions{})
	switch {
	case err == nil:
		return true, nil
	case isNotFound(err):
		if minio.ToErrorResponse(err).Code == "NoSuchBucket" {
			return false, fmt.Errorf("S3 storage: bucket %q doesn't exist", b.bucket)
		}
		return false, nil
	}
	return false, wrapS3(err)
}

func (b *s3Backend) signedURL(ctx context.Context, key, filename string) (string, error) {
	q := url.Values{}
	q.Set("response-content-disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename))
	q.Set("response-content-type", "application/octet-stream")
	u, err := b.cl.PresignedGetObject(ctx, b.bucket, b.prefix+key, RedirectTTL, q)
	if err != nil {
		return "", wrapS3(err)
	}
	return u.String(), nil
}

func isNotFound(err error) bool {
	r := minio.ToErrorResponse(err)
	return r.StatusCode == http.StatusNotFound || r.Code == "NoSuchKey" || r.Code == "NoSuchBucket"
}

func wrapS3(err error) error {
	if err == nil {
		return nil
	}
	r := minio.ToErrorResponse(err)
	switch {
	case r.Code == "AccessDenied" || r.StatusCode == http.StatusForbidden:
		return fmt.Errorf("S3 storage: access denied (check the access key and the bucket policy): %w", err)
	case r.Code == "NoSuchBucket":
		return fmt.Errorf("S3 storage: the bucket doesn't exist: %w", err)
	}
	return fmt.Errorf("S3 storage: %w", err)
}

func (b *s3Backend) check(ctx context.Context) error {
	ok, err := b.cl.BucketExists(ctx, b.bucket)
	if err != nil {
		return wrapS3(err)
	}
	if !ok {
		return fmt.Errorf("S3 storage: bucket %q doesn't exist", b.bucket)
	}
	// Also proves the key may read objects, not just list buckets.
	_, err = b.exists(ctx, "0000000000000000000000000000000000000000000000000000000000000000")
	return err
}
