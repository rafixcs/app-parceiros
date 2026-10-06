// Package ffmpeg processes uploaded videos with the ffmpeg and ffprobe
// binaries (domain.VideoProcessor): a light 720p preview, a JPEG thumbnail,
// the duration and the dimensions.
package ffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// Processor runs ffmpeg and ffprobe (empty names use the PATH). The input may
// be a local path or a URL: the worker reads the original from the bucket by
// a presigned URL, without downloading it first.
type Processor struct {
	FFmpeg  string
	FFprobe string
}

var _ domain.VideoProcessor = Processor{}

type probe struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		Width     int32  `json:"width"`
		Height    int32  `json:"height"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func (p Processor) bin(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

func (p Processor) probe(ctx context.Context, input string) (probe, error) {
	var pr probe
	out, err := run(ctx, p.bin(p.FFprobe, "ffprobe"), "-v", "error", "-print_format", "json",
		"-show_entries", "format=duration:stream=codec_type,width,height", input)
	if err != nil {
		return pr, err
	}
	if err := json.Unmarshal(out, &pr); err != nil {
		return pr, fmt.Errorf("ffprobe: invalid output: %w", err)
	}
	return pr, nil
}

func (pr probe) video() (int32, int32, bool) {
	for _, st := range pr.Streams {
		if st.CodecType == "video" && st.Width > 0 && st.Height > 0 {
			return st.Width, st.Height, true
		}
	}
	return 0, 0, false
}

// Process writes preview.mp4 and thumb.jpg in dir. It returns
// domain.ErrNotAVideo for a file ffmpeg cannot read as a video.
func (p Processor) Process(ctx context.Context, input, dir string) (domain.ProcessedVideo, error) {
	pr, err := p.probe(ctx, input)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return domain.ProcessedVideo{}, fmt.Errorf("%w: %w", domain.ErrNotAVideo, err)
		}
		return domain.ProcessedVideo{}, err
	}
	if _, _, ok := pr.video(); !ok {
		return domain.ProcessedVideo{}, domain.ErrNotAVideo
	}
	dur, _ := strconv.ParseFloat(pr.Format.Duration, 64)

	out := domain.ProcessedVideo{
		DurationS:     int32(math.Round(dur)),
		PreviewPath:   filepath.Join(dir, "preview.mp4"),
		ThumbnailPath: filepath.Join(dir, "thumb.jpg"),
	}
	// Light H.264 preview: the shorter side gets up to 720 px (720p standing
	// or lying). ffmpeg already applies the rotation recorded by the phone.
	scale := "scale='if(gte(iw,ih),-2,trunc(min(720,iw)/2)*2)':'if(gte(iw,ih),trunc(min(720,ih)/2)*2,-2)'"
	if _, err := run(ctx, p.bin(p.FFmpeg, "ffmpeg"), "-nostdin", "-v", "error", "-y", "-i", input,
		"-map", "0:v:0", "-map", "0:a:0?", "-vf", scale,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "26", "-pix_fmt", "yuv420p", "-threads", "2",
		"-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart", out.PreviewPath); err != nil {
		return domain.ProcessedVideo{}, err
	}
	// Thumbnail from the preview (already rotated and smaller), at 1 s or at
	// the middle.
	at := math.Min(1, dur/2)
	if _, err := run(ctx, p.bin(p.FFmpeg, "ffmpeg"), "-nostdin", "-v", "error", "-y",
		"-ss", strconv.FormatFloat(at, 'f', 2, 64), "-i", out.PreviewPath,
		"-frames:v", "1", "-vf", "scale='if(gte(iw,ih),min(640,iw),-2)':'if(gte(iw,ih),-2,min(640,ih))'",
		"-q:v", "4", out.ThumbnailPath); err != nil {
		return domain.ProcessedVideo{}, err
	}
	pp, err := p.probe(ctx, out.PreviewPath)
	if err != nil {
		return domain.ProcessedVideo{}, err
	}
	out.Width, out.Height, _ = pp.video()
	return out, nil
}

var urlInError = regexp.MustCompile(`https?://\S+`)

func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		// The input may be a signed URL: it does not go into the error.
		msg := urlInError.ReplaceAllString(strings.TrimSpace(stderr.String()), "<url>")
		if len(msg) > 500 {
			msg = msg[:500]
		}
		return nil, fmt.Errorf("%s: %w: %s", filepath.Base(name), err, msg)
	}
	return stdout.Bytes(), nil
}
