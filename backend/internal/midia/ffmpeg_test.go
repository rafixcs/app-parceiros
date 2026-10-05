package midia_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rafixcs/app-parceiros/backend/internal/midia"
)

func exigirFFmpeg(t *testing.T) {
	t.Helper()
	for _, b := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(b); err != nil {
			t.Skipf("%s não está instalado", b)
		}
	}
}

// TestFFmpeg processa um vídeo em pé de verdade: a prévia mantém a
// orientação (lado menor até 720 px) e a miniatura sai em JPEG.
func TestFFmpeg(t *testing.T) {
	exigirFFmpeg(t)
	dir := t.TempDir()
	entrada := filepath.Join(dir, "em-pe.mp4")
	gerar := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=duration=3:size=1080x1920:rate=10",
		"-f", "lavfi", "-i", "sine=duration=3", "-shortest", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", entrada)
	if out, err := gerar.CombinedOutput(); err != nil {
		t.Fatalf("gerando vídeo de teste: %v %s", err, out)
	}

	saida := t.TempDir()
	p, err := midia.FFmpeg{}.Processar(context.Background(), entrada, saida)
	if err != nil {
		t.Fatal(err)
	}
	if p.DuracaoS != 3 || p.Largura != 720 || p.Altura != 1280 {
		t.Fatalf("processado: %+v", p)
	}
	for _, f := range []string{p.Previa, p.Thumb} {
		if st, err := os.Stat(f); err != nil || st.Size() == 0 {
			t.Fatalf("%s: %v", f, err)
		}
	}
	jpg, _ := os.ReadFile(p.Thumb)
	if len(jpg) < 3 || jpg[0] != 0xFF || jpg[1] != 0xD8 {
		t.Fatal("a miniatura não é um JPEG")
	}
}

func TestFFmpegRecusaArquivoQueNaoEhVideo(t *testing.T) {
	exigirFFmpeg(t)
	arq := filepath.Join(t.TempDir(), "nota.mp4")
	if err := os.WriteFile(arq, []byte("isto não é um vídeo"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (midia.FFmpeg{}).Processar(context.Background(), arq, t.TempDir()); !errors.Is(err, midia.ErrNaoEhVideo) {
		t.Fatalf("erro %v, quer ErrNaoEhVideo", err)
	}
}
