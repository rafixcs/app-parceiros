// Package storage keeps objects in an S3-compatible bucket: Cloudflare R2 or
// GCS in production and SeaweedFS locally.
package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// Storage keeps objects by key.
type Storage interface {
	Put(ctx context.Context, key string, data []byte, contentType string) error
}

type Config struct {
	Endpoint string // e.g. http://seaweedfs:8333 or https://<account>.r2.cloudflarestorage.com
	// PublicEndpoint signs the URLs the browser uses (upload and download)
	// when it cannot reach Endpoint (locally, the cluster S3 answers on
	// http://localhost:8333). Empty means Endpoint.
	PublicEndpoint string
	Bucket         string
	AccessKey      string
	SecretKey      string
	Region         string
}

// S3 is the Storage in an S3-compatible bucket.
type S3 struct {
	cli    *minio.Client
	pub    *minio.Client // only signs URLs for the browser
	bucket string
}

func NewS3(c Config) (*S3, error) {
	cli, err := newClient(c, c.Endpoint, "S3_ENDPOINT")
	if err != nil {
		return nil, err
	}
	pub := cli
	if c.PublicEndpoint != "" {
		if pub, err = newClient(c, c.PublicEndpoint, "S3_PUBLIC_ENDPOINT"); err != nil {
			return nil, err
		}
	}
	return &S3{cli: cli, pub: pub, bucket: c.Bucket}, nil
}

func newClient(c Config, endpoint, name string) (*minio.Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid %s %q", name, endpoint)
	}
	region := c.Region
	if region == "" {
		region = "auto"
	}
	// With a fixed region, signing a URL makes no network call.
	return minio.New(u.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(c.AccessKey, c.SecretKey, ""),
		Secure: u.Scheme == "https",
		Region: region,
	})
}

// EnsureBucket creates the bucket when it does not exist. Useful locally; in
// production the infrastructure creates the bucket.
func (s *S3) EnsureBucket(ctx context.Context) error {
	ok, err := s.cli.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("checking bucket %s: %w", s.bucket, err)
	}
	if ok {
		return nil
	}
	return s.cli.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{})
}

func (s *S3) Put(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := s.cli.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("storing %s: %w", key, err)
	}
	return nil
}

// ErrNotFound means the object (or the multipart upload) does not exist.
var ErrNotFound = fmt.Errorf("object %w", domain.ErrNotFound)

func translate(err error) error {
	switch minio.ToErrorResponse(err).Code {
	case "NoSuchKey", "NoSuchUpload":
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	return err
}

// Part is one part of a multipart upload.
type Part = domain.UploadPart

// Object describes a stored object.
type Object = domain.ObjectInfo

// S3 is the bucket of the videos.
var _ domain.ObjectStore = (*S3)(nil)

// StartMultipart opens a multipart upload and returns its uploadId. The parts
// go straight from the browser to the bucket through URLs from SignPart.
func (s *S3) StartMultipart(ctx context.Context, key, contentType string) (string, error) {
	id, err := minio.Core{Client: s.cli}.NewMultipartUpload(ctx, s.bucket, key, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return "", fmt.Errorf("starting upload of %s: %w", key, err)
	}
	return id, nil
}

// SignPart returns the presigned URL (PUT) of one part of the upload.
func (s *S3) SignPart(ctx context.Context, key, uploadID string, number int, ttl time.Duration) (string, error) {
	q := url.Values{"partNumber": {strconv.Itoa(number)}, "uploadId": {uploadID}}
	u, err := s.pub.Presign(ctx, http.MethodPut, s.bucket, key, ttl, q)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// Parts lists the parts already uploaded.
func (s *S3) Parts(ctx context.Context, key, uploadID string) ([]Part, error) {
	core := minio.Core{Client: s.cli}
	var out []Part
	marker := 0
	for {
		r, err := core.ListObjectParts(ctx, s.bucket, key, uploadID, marker, 1000)
		if err != nil {
			return nil, translate(err)
		}
		for _, p := range r.ObjectParts {
			out = append(out, Part{Number: p.PartNumber, ETag: p.ETag, Size: p.Size})
		}
		if !r.IsTruncated {
			return out, nil
		}
		marker = r.NextPartNumberMarker
	}
}

// CompleteMultipart joins the parts into the final object.
func (s *S3) CompleteMultipart(ctx context.Context, key, uploadID string, parts []Part) error {
	cp := make([]minio.CompletePart, len(parts))
	for i, p := range parts {
		cp[i] = minio.CompletePart{PartNumber: p.Number, ETag: p.ETag}
	}
	_, err := minio.Core{Client: s.cli}.CompleteMultipartUpload(ctx, s.bucket, key, uploadID, cp, minio.PutObjectOptions{})
	return translate(err)
}

// AbortMultipart discards an upload and its parts. An upload that no longer
// exists is not an error.
func (s *S3) AbortMultipart(ctx context.Context, key, uploadID string) error {
	err := translate(minio.Core{Client: s.cli}.AbortMultipartUpload(ctx, s.bucket, key, uploadID))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// Info returns the size and type of an object.
func (s *S3) Info(ctx context.Context, key string) (Object, error) {
	st, err := s.cli.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return Object{}, translate(err)
	}
	return Object{Size: st.Size, ContentType: st.ContentType}, nil
}

// SignGet returns a presigned URL for the browser to read the object. With
// downloadAs, the response asks the browser to save it under that name.
func (s *S3) SignGet(ctx context.Context, key string, ttl time.Duration, downloadAs string) (string, error) {
	q := url.Values{}
	if downloadAs != "" {
		q.Set("response-content-disposition", Attachment(downloadAs))
	}
	u, err := s.pub.PresignedGetObject(ctx, s.bucket, key, ttl, q)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// InternalURL returns a presigned read URL on the internal endpoint, for
// processes inside the cluster (the worker's ffmpeg).
func (s *S3) InternalURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	u, err := s.cli.PresignedGetObject(ctx, s.bucket, key, ttl, nil)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// UploadFile stores a local file.
func (s *S3) UploadFile(ctx context.Context, key, path, contentType string) error {
	if _, err := s.cli.FPutObject(ctx, s.bucket, key, path, minio.PutObjectOptions{ContentType: contentType}); err != nil {
		return fmt.Errorf("storing %s: %w", key, err)
	}
	return nil
}

// DeletePrefix deletes every object whose key starts with prefix.
func (s *S3) DeletePrefix(ctx context.Context, prefix string) error {
	objs := make(chan minio.ObjectInfo)
	errs := make(chan error, 1)
	go func() {
		defer close(objs)
		for o := range s.cli.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
			if o.Err != nil {
				errs <- o.Err
				return
			}
			objs <- o
		}
	}()
	for e := range s.cli.RemoveObjects(ctx, s.bucket, objs, minio.RemoveObjectsOptions{}) {
		if e.Err != nil {
			return fmt.Errorf("deleting %s: %w", e.ObjectName, e.Err)
		}
	}
	select {
	case err := <-errs:
		return fmt.Errorf("listing %s: %w", prefix, err)
	default:
		return nil
	}
}

// Attachment builds the download Content-Disposition with the file name, also
// in UTF-8 (RFC 6266) for names with accents.
func Attachment(name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	var b strings.Builder
	for _, c := range []byte(name) {
		if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || strings.IndexByte("!#$&+-.^_`|~", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii, b.String())
}

// Memory keeps objects in a map. Meant for tests.
type Memory struct {
	mu      sync.Mutex
	Objects map[string][]byte
}

func (m *Memory) Put(_ context.Context, key string, data []byte, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Objects == nil {
		m.Objects = map[string][]byte{}
	}
	m.Objects[key] = append([]byte(nil), data...)
	return nil
}

func (m *Memory) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.Objects))
	for k := range m.Objects {
		out = append(out, k)
	}
	return out
}

// Discard stores nothing. Used when no bucket is configured.
type Discard struct{}

func (Discard) Put(context.Context, string, []byte, string) error { return nil }
