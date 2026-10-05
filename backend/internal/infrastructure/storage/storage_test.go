package storage_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/storage"
)

const s3Config = `{"identities": [{"name": "test",
  "credentials": [{"accessKey": "test", "secretKey": "test-secret"}],
  "actions": ["Admin", "Read", "List", "Tagging", "Write"]}]}`

var (
	s3Once sync.Once
	s3URL  string
	s3Err  error
)

// endpoint returns a test S3: TEST_S3_ENDPOINT (with the keys test and
// test-secret) or a SeaweedFS with testcontainers.
func endpoint(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("TEST_S3_ENDPOINT"); v != "" {
		return v
	}
	if testing.Short() {
		t.Skip("integration test with S3 (run without -short)")
	}
	s3Once.Do(func() {
		ctx := context.Background()
		c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image: "chrislusf/seaweedfs:4.48",
				Cmd: []string{"server", "-dir=/data", "-s3", "-s3.port=8333", "-s3.config=/etc/seaweedfs/s3.json",
					"-master.volumeSizeLimitMB=64", "-volume.max=8"},
				ExposedPorts: []string{"8333/tcp"},
				Files: []testcontainers.ContainerFile{{
					Reader: strings.NewReader(s3Config), ContainerFilePath: "/etc/seaweedfs/s3.json", FileMode: 0o644,
				}},
				WaitingFor: wait.ForListeningPort("8333/tcp").WithStartupTimeout(90 * time.Second),
			},
			Started: true,
		})
		if err != nil {
			s3Err = err
			return
		}
		host, err := c.Host(ctx)
		if err != nil {
			s3Err = err
			return
		}
		port, err := c.MappedPort(ctx, "8333/tcp")
		if err != nil {
			s3Err = err
			return
		}
		s3URL = fmt.Sprintf("http://%s:%s", host, port.Port())
	})
	if s3Err != nil {
		t.Fatalf("starting SeaweedFS: %v", s3Err)
	}
	return s3URL
}

func newS3(t *testing.T) *storage.S3 {
	t.Helper()
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	s, err := storage.NewS3(storage.Config{
		Endpoint: endpoint(t), Bucket: fmt.Sprintf("test-%x", b), AccessKey: "test", SecretKey: "test-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	// SeaweedFS takes a few seconds to accept writes after starting.
	ctx := context.Background()
	deadline := time.Now().Add(60 * time.Second)
	for {
		err = s.EnsureBucket(ctx)
		if err == nil {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("creating bucket: %v", err)
		}
		time.Sleep(time.Second)
	}
}

func uploadPart(t *testing.T, url string, data []byte) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("PUT part: %d %s", res.StatusCode, body)
	}
	return res.Header.Get("ETag")
}

// TestUploadMultipart walks the browser path: parts sent through presigned
// URLs, completion, signed download and cleanup.
func TestUploadMultipart(t *testing.T) {
	s := newS3(t)
	ctx := context.Background()
	key := "videos/ws/v1/original"

	id, err := s.StartMultipart(ctx, key, "video/mp4")
	if err != nil {
		t.Fatal(err)
	}
	p1 := bytes.Repeat([]byte("a"), 5<<20) // S3 requires 5 MB parts, except the last one
	p2 := []byte("end of video")
	var parts []storage.Part
	for i, data := range [][]byte{p1, p2} {
		url, err := s.SignPart(ctx, key, id, i+1, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, storage.Part{Number: i + 1, ETag: uploadPart(t, url, data)})
	}
	listed, err := s.Parts(ctx, key, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].Size != int64(len(p1)) || listed[1].Number != 2 {
		t.Fatalf("parts: %+v", listed)
	}
	if err := s.CompleteMultipart(ctx, key, id, parts); err != nil {
		t.Fatal(err)
	}
	info, err := s.Info(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != int64(len(p1)+len(p2)) {
		t.Fatalf("size %d", info.Size)
	}

	url, err := s.SignGet(ctx, key, time.Minute, "Meu vídeo.mp4")
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 200 || !bytes.HasSuffix(body, p2) {
		t.Fatalf("download: %d, %d bytes", res.StatusCode, len(body))
	}
	if cd := res.Header.Get("Content-Disposition"); !strings.Contains(cd, "filename*=UTF-8''Meu%20v%C3%ADdeo.mp4") {
		t.Fatalf("Content-Disposition %q", cd)
	}

	file := filepath.Join(t.TempDir(), "thumb.jpg")
	if err := os.WriteFile(file, []byte("jpg"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.UploadFile(ctx, "videos/ws/v1/thumb.jpg", file, "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePrefix(ctx, "videos/ws/v1/"); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{key, "videos/ws/v1/thumb.jpg"} {
		if _, err := s.Info(ctx, k); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("%s after delete: %v", k, err)
		}
	}
}

func TestAbortMultipart(t *testing.T) {
	s := newS3(t)
	ctx := context.Background()
	id, err := s.StartMultipart(ctx, "videos/x/original", "video/mp4")
	if err != nil {
		t.Fatal(err)
	}
	url, err := s.SignPart(ctx, "videos/x/original", id, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	uploadPart(t, url, []byte("part"))
	if err := s.AbortMultipart(ctx, "videos/x/original", id); err != nil {
		t.Fatal(err)
	}
	// Aborting again (an upload that no longer exists) is not an error.
	if err := s.AbortMultipart(ctx, "videos/x/original", id); err != nil {
		t.Fatalf("abort again: %v", err)
	}
	if _, err := s.Info(ctx, "videos/x/original"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("object after abort: %v", err)
	}
}

func TestAttachment(t *testing.T) {
	got := storage.Attachment(`Ação "nova"; final.mp4`)
	want := `attachment; filename="A__o _nova_; final.mp4"; filename*=UTF-8''A%C3%A7%C3%A3o%20%22nova%22%3B%20final.mp4`
	if got != want {
		t.Fatalf("Attachment:\n got %s\nwant %s", got, want)
	}
}
