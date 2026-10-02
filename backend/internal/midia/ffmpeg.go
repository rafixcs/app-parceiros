package midia

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
)

// Processado é o resultado do processamento de um upload: a prévia em 720p,
// a miniatura e os dados do vídeo.
type Processado struct {
	DuracaoS int32
	Largura  int32 // da prévia, já na orientação de exibição
	Altura   int32
	Previa   string // caminho local do MP4
	Thumb    string // caminho local do JPEG
}

// Processador gera prévia e miniatura de um vídeo (ffmpeg em produção).
type Processador interface {
	Processar(ctx context.Context, entrada, dir string) (Processado, error)
}

// ErrNaoEhVideo indica um arquivo que o ffmpeg não lê como vídeo.
var ErrNaoEhVideo = errors.New("o arquivo não é um vídeo legível")

// FFmpeg processa com os binários ffmpeg e ffprobe (vazios usam o PATH). A
// entrada pode ser um caminho local ou uma URL (o worker lê do bucket por URL
// pré-assinada, sem baixar o original).
type FFmpeg struct {
	FFmpeg  string
	FFprobe string
}

type sondagem struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		Width     int32  `json:"width"`
		Height    int32  `json:"height"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func (f FFmpeg) bin(nome, padrao string) string {
	if nome != "" {
		return nome
	}
	return padrao
}

func (f FFmpeg) sondar(ctx context.Context, entrada string) (sondagem, error) {
	var s sondagem
	out, err := rodar(ctx, f.bin(f.FFprobe, "ffprobe"), "-v", "error", "-print_format", "json",
		"-show_entries", "format=duration:stream=codec_type,width,height", entrada)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(out, &s); err != nil {
		return s, fmt.Errorf("ffprobe: saída inválida: %w", err)
	}
	return s, nil
}

func (s sondagem) video() (int32, int32, bool) {
	for _, st := range s.Streams {
		if st.CodecType == "video" && st.Width > 0 && st.Height > 0 {
			return st.Width, st.Height, true
		}
	}
	return 0, 0, false
}

func (f FFmpeg) Processar(ctx context.Context, entrada, dir string) (Processado, error) {
	s, err := f.sondar(ctx, entrada)
	if err != nil {
		var saida *exec.ExitError
		if errors.As(err, &saida) {
			return Processado{}, fmt.Errorf("%w: %w", ErrNaoEhVideo, err)
		}
		return Processado{}, err
	}
	if _, _, ok := s.video(); !ok {
		return Processado{}, ErrNaoEhVideo
	}
	dur, _ := strconv.ParseFloat(s.Format.Duration, 64)

	out := Processado{
		DuracaoS: int32(math.Round(dur)),
		Previa:   filepath.Join(dir, "preview.mp4"),
		Thumb:    filepath.Join(dir, "thumb.jpg"),
	}
	// Prévia leve em H.264: o lado menor fica com até 720 px (720p em pé ou
	// deitado). O ffmpeg já aplica a rotação gravada pelo celular.
	escala := "scale='if(gte(iw,ih),-2,trunc(min(720,iw)/2)*2)':'if(gte(iw,ih),trunc(min(720,ih)/2)*2,-2)'"
	if _, err := rodar(ctx, f.bin(f.FFmpeg, "ffmpeg"), "-nostdin", "-v", "error", "-y", "-i", entrada,
		"-map", "0:v:0", "-map", "0:a:0?", "-vf", escala,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "26", "-pix_fmt", "yuv420p", "-threads", "2",
		"-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart", out.Previa); err != nil {
		return Processado{}, err
	}
	// Miniatura a partir da prévia (já girada e menor), em 1 s ou na metade.
	em := math.Min(1, dur/2)
	if _, err := rodar(ctx, f.bin(f.FFmpeg, "ffmpeg"), "-nostdin", "-v", "error", "-y",
		"-ss", strconv.FormatFloat(em, 'f', 2, 64), "-i", out.Previa,
		"-frames:v", "1", "-vf", "scale='if(gte(iw,ih),min(640,iw),-2)':'if(gte(iw,ih),-2,min(640,ih))'",
		"-q:v", "4", out.Thumb); err != nil {
		return Processado{}, err
	}
	p, err := f.sondar(ctx, out.Previa)
	if err != nil {
		return Processado{}, err
	}
	out.Largura, out.Altura, _ = p.video()
	return out, nil
}

var urlNoErro = regexp.MustCompile(`https?://\S+`)

func rodar(ctx context.Context, nome string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, nome, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		// A entrada pode ser uma URL assinada: ela não vai para o erro.
		msg := urlNoErro.ReplaceAllString(strings.TrimSpace(stderr.String()), "<url>")
		if len(msg) > 500 {
			msg = msg[:500]
		}
		return nil, fmt.Errorf("%s: %w: %s", filepath.Base(nome), err, msg)
	}
	return stdout.Bytes(), nil
}
