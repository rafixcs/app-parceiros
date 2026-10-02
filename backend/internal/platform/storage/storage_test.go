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

	"github.com/rafixcs/app-parceiros/backend/internal/platform/storage"
)

const s3Config = `{"identities": [{"name": "teste",
  "credentials": [{"accessKey": "teste", "secretKey": "teste-segredo"}],
  "actions": ["Admin", "Read", "List", "Tagging", "Write"]}]}`

var (
	s3Once sync.Once
	s3URL  string
	s3Err  error
)

// endpoint devolve um S3 de teste: TEST_S3_ENDPOINT (com as chaves teste e
// teste-segredo) ou um SeaweedFS com testcontainers.
func endpoint(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("TEST_S3_ENDPOINT"); v != "" {
		return v
	}
	if testing.Short() {
		t.Skip("teste de integração com S3 (rode sem -short)")
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
		porta, err := c.MappedPort(ctx, "8333/tcp")
		if err != nil {
			s3Err = err
			return
		}
		s3URL = fmt.Sprintf("http://%s:%s", host, porta.Port())
	})
	if s3Err != nil {
		t.Fatalf("subindo SeaweedFS: %v", s3Err)
	}
	return s3URL
}

func novoS3(t *testing.T) *storage.S3 {
	t.Helper()
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	s, err := storage.NovoS3(storage.Config{
		Endpoint: endpoint(t), Bucket: fmt.Sprintf("teste-%x", b), AccessKey: "teste", SecretKey: "teste-segredo",
	})
	if err != nil {
		t.Fatal(err)
	}
	// O SeaweedFS demora alguns segundos para aceitar escritas depois de subir.
	ctx := context.Background()
	prazo := time.Now().Add(60 * time.Second)
	for {
		err = s.GarantirBucket(ctx)
		if err == nil {
			return s
		}
		if time.Now().After(prazo) {
			t.Fatalf("criando bucket: %v", err)
		}
		time.Sleep(time.Second)
	}
}

func enviarParte(t *testing.T, url string, dados []byte) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(dados))
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		corpo, _ := io.ReadAll(res.Body)
		t.Fatalf("PUT da parte: %d %s", res.StatusCode, corpo)
	}
	return res.Header.Get("ETag")
}

// TestUploadMultipart percorre o caminho do navegador: partes enviadas por
// URL pré-assinada, conclusão, download assinado e limpeza.
func TestUploadMultipart(t *testing.T) {
	s := novoS3(t)
	ctx := context.Background()
	chave := "videos/ws/v1/original"

	id, err := s.IniciarMultipart(ctx, chave, "video/mp4")
	if err != nil {
		t.Fatal(err)
	}
	p1 := bytes.Repeat([]byte("a"), 5<<20) // o S3 exige 5 MB nas partes, menos na última
	p2 := []byte("fim do vídeo")
	var partes []storage.Parte
	for i, dados := range [][]byte{p1, p2} {
		url, err := s.AssinarParte(ctx, chave, id, i+1, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		partes = append(partes, storage.Parte{Numero: i + 1, ETag: enviarParte(t, url, dados)})
	}
	listadas, err := s.Partes(ctx, chave, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(listadas) != 2 || listadas[0].Tamanho != int64(len(p1)) || listadas[1].Numero != 2 {
		t.Fatalf("partes: %+v", listadas)
	}
	if err := s.ConcluirMultipart(ctx, chave, id, partes); err != nil {
		t.Fatal(err)
	}
	info, err := s.Info(ctx, chave)
	if err != nil {
		t.Fatal(err)
	}
	if info.Tamanho != int64(len(p1)+len(p2)) {
		t.Fatalf("tamanho %d", info.Tamanho)
	}

	url, err := s.AssinarGet(ctx, chave, time.Minute, "Meu vídeo.mp4")
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	corpo, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 200 || !bytes.HasSuffix(corpo, p2) {
		t.Fatalf("download: %d, %d bytes", res.StatusCode, len(corpo))
	}
	if cd := res.Header.Get("Content-Disposition"); !strings.Contains(cd, "filename*=UTF-8''Meu%20v%C3%ADdeo.mp4") {
		t.Fatalf("Content-Disposition %q", cd)
	}

	arq := filepath.Join(t.TempDir(), "thumb.jpg")
	if err := os.WriteFile(arq, []byte("jpg"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.EnviarArquivo(ctx, "videos/ws/v1/thumb.jpg", arq, "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if err := s.ApagarPrefixo(ctx, "videos/ws/v1/"); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{chave, "videos/ws/v1/thumb.jpg"} {
		if _, err := s.Info(ctx, k); !errors.Is(err, storage.ErrNaoEncontrado) {
			t.Fatalf("%s depois de apagar: %v", k, err)
		}
	}
}

func TestAbortarMultipart(t *testing.T) {
	s := novoS3(t)
	ctx := context.Background()
	id, err := s.IniciarMultipart(ctx, "videos/x/original", "video/mp4")
	if err != nil {
		t.Fatal(err)
	}
	url, err := s.AssinarParte(ctx, "videos/x/original", id, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	enviarParte(t, url, []byte("parte"))
	if err := s.AbortarMultipart(ctx, "videos/x/original", id); err != nil {
		t.Fatal(err)
	}
	// Abortar de novo (upload que já não existe) não é erro.
	if err := s.AbortarMultipart(ctx, "videos/x/original", id); err != nil {
		t.Fatalf("abortar de novo: %v", err)
	}
	if _, err := s.Info(ctx, "videos/x/original"); !errors.Is(err, storage.ErrNaoEncontrado) {
		t.Fatalf("objeto depois de abortar: %v", err)
	}
}

func TestAnexo(t *testing.T) {
	got := storage.Anexo(`Ação "nova"; final.mp4`)
	want := `attachment; filename="A__o _nova_; final.mp4"; filename*=UTF-8''A%C3%A7%C3%A3o%20%22nova%22%3B%20final.mp4`
	if got != want {
		t.Fatalf("Anexo:\n got %s\nwant %s", got, want)
	}
}
