package ffmpeg_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/ffmpeg"
)

func requireFFmpeg(t *testing.T) {
	t.Helper()
	for _, b := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(b); err != nil {
			t.Skipf("%s is not installed", b)
		}
	}
}

// TestProcess processes a real standing video: the preview keeps the
// orientation (shorter side up to 720 px) and the thumbnail is a JPEG.
func TestProcess(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	input := filepath.Join(dir, "standing.mp4")
	gen := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=duration=3:size=1080x1920:rate=10",
		"-f", "lavfi", "-i", "sine=duration=3", "-shortest", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", input)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generating the test video: %v %s", err, out)
	}

	p, err := ffmpeg.Processor{}.Process(context.Background(), input, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if p.DurationS != 3 || p.Width != 720 || p.Height != 1280 {
		t.Fatalf("processed: %+v", p)
	}
	for _, f := range []string{p.PreviewPath, p.ThumbnailPath} {
		if st, err := os.Stat(f); err != nil || st.Size() == 0 {
			t.Fatalf("%s: %v", f, err)
		}
	}
	jpg, _ := os.ReadFile(p.ThumbnailPath)
	if len(jpg) < 3 || jpg[0] != 0xFF || jpg[1] != 0xD8 {
		t.Fatal("the thumbnail is not a JPEG")
	}
}

func TestProcessRefusesNonVideo(t *testing.T) {
	requireFFmpeg(t)
	f := filepath.Join(t.TempDir(), "note.mp4")
	if err := os.WriteFile(f, []byte("this is not a video"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (ffmpeg.Processor{}).Process(context.Background(), f, t.TempDir()); !errors.Is(err, domain.ErrNotAVideo) {
		t.Fatalf("error %v, want ErrNotAVideo", err)
	}
}
