package midia

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"
)

const (
	PlataformaYouTube = "youtube"
	PlataformaTikTok  = "tiktok"
	PlataformaUpload  = "upload"
)

// Referencia é o que o oEmbed oficial diz de um vídeo de outro criador. Só
// metadados: o vídeo em si toca no player da plataforma.
type Referencia struct {
	Plataforma string `json:"plataforma"`
	EmbedID    string `json:"embed_id"`
	URL        string `json:"url"`
	Titulo     string `json:"titulo"`
	Autor      string `json:"autor"`
	ThumbURL   string `json:"thumb_url"`
}

// Erros do oEmbed. O serviço os traduz para os erros da API.
var (
	errLinkVideo      = errors.New("link de vídeo não reconhecido")
	errIndisponivel   = errors.New("vídeo indisponível na plataforma")
	errPlataformaFora = errors.New("plataforma não respondeu")
)

// Cache guarda as respostas do oEmbed (Redis em produção).
type Cache interface {
	Ler(ctx context.Context, chave string) (string, bool)
	Gravar(ctx context.Context, chave, valor string, validade time.Duration)
}

// CacheRedis é o Cache no Redis. Falhas do Redis só fazem perder o cache.
type CacheRedis struct{ R *redis.Client }

func (c CacheRedis) Ler(ctx context.Context, chave string) (string, bool) {
	v, err := c.R.Get(ctx, chave).Result()
	return v, err == nil
}

func (c CacheRedis) Gravar(ctx context.Context, chave, valor string, validade time.Duration) {
	_ = c.R.Set(ctx, chave, valor, validade).Err()
}

// OEmbed resolve links do YouTube e do TikTok pelos endpoints oficiais.
type OEmbed struct {
	HTTP       *http.Client
	YouTubeURL string // padrão https://www.youtube.com/oembed
	TikTokURL  string // padrão https://www.tiktok.com/oembed
	Cache      Cache  // opcional
}

const (
	validadeCache  = 24 * time.Hour
	maxRespostaOEm = 64 << 10
	maxTextoVideo  = 200
)

var (
	idYouTube = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	idTikTok  = regexp.MustCompile(`^[0-9]{8,25}$`)
)

// identificar reconhece a plataforma e, quando possível, o id do vídeo, e
// devolve a URL canônica para o oEmbed. Links curtos do TikTok só têm o id
// depois do oEmbed.
func identificar(link string) (plataforma, id, canonica string, err error) {
	link = strings.TrimSpace(link)
	if link == "" || len(link) > 2000 {
		return "", "", "", errLinkVideo
	}
	if !strings.Contains(link, "://") {
		link = "https://" + link
	}
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", "", "", errLinkVideo
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	partes := strings.Split(strings.Trim(u.Path, "/"), "/")

	switch host {
	case "youtube.com", "m.youtube.com":
		switch {
		case len(partes) == 1 && partes[0] == "watch":
			id = u.Query().Get("v")
		case len(partes) == 2 && (partes[0] == "shorts" || partes[0] == "embed" || partes[0] == "live" || partes[0] == "v"):
			id = partes[1]
		}
		if !idYouTube.MatchString(id) {
			return "", "", "", errLinkVideo
		}
		return PlataformaYouTube, id, "https://www.youtube.com/watch?v=" + id, nil
	case "youtu.be":
		if len(partes) != 1 || !idYouTube.MatchString(partes[0]) {
			return "", "", "", errLinkVideo
		}
		return PlataformaYouTube, partes[0], "https://www.youtube.com/watch?v=" + partes[0], nil
	case "tiktok.com", "m.tiktok.com":
		if len(partes) == 3 && strings.HasPrefix(partes[0], "@") && len(partes[0]) > 1 && partes[1] == "video" && idTikTok.MatchString(partes[2]) {
			return PlataformaTikTok, partes[2], "https://www.tiktok.com/" + partes[0] + "/video/" + partes[2], nil
		}
		if len(partes) == 2 && partes[0] == "t" && partes[1] != "" {
			return PlataformaTikTok, "", "https://www.tiktok.com/t/" + partes[1] + "/", nil
		}
	case "vm.tiktok.com", "vt.tiktok.com":
		if len(partes) == 1 && partes[0] != "" {
			return PlataformaTikTok, "", "https://" + host + "/" + partes[0] + "/", nil
		}
	}
	return "", "", "", errLinkVideo
}

// Resolver busca os metadados do vídeo no oEmbed oficial. Com usarCache, as
// respostas boas ficam um dia no cache.
func (o *OEmbed) Resolver(ctx context.Context, link string, usarCache bool) (Referencia, error) {
	plataforma, id, canonica, err := identificar(link)
	if err != nil {
		return Referencia{}, err
	}
	soma := sha256.Sum256([]byte(canonica))
	chave := "oembed:" + hex.EncodeToString(soma[:16])
	if usarCache && o.Cache != nil {
		if v, ok := o.Cache.Ler(ctx, chave); ok {
			var r Referencia
			if json.Unmarshal([]byte(v), &r) == nil {
				return r, nil
			}
		}
	}

	endpoint := o.YouTubeURL
	if endpoint == "" {
		endpoint = "https://www.youtube.com/oembed"
	}
	if plataforma == PlataformaTikTok {
		endpoint = o.TikTokURL
		if endpoint == "" {
			endpoint = "https://www.tiktok.com/oembed"
		}
	}
	q := url.Values{"url": {canonica}, "format": {"json"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return Referencia{}, err
	}
	req.Header.Set("Accept", "application/json")
	cli := o.HTTP
	if cli == nil {
		cli = &http.Client{Timeout: 8 * time.Second}
	}
	res, err := cli.Do(req)
	if err != nil {
		return Referencia{}, fmt.Errorf("%w: %w", errPlataformaFora, err)
	}
	defer func() { _ = res.Body.Close() }()
	switch {
	case res.StatusCode == http.StatusOK:
	case res.StatusCode >= 400 && res.StatusCode < 500 && res.StatusCode != http.StatusTooManyRequests:
		// Privado, removido, inexistente ou sem permissão de incorporar.
		return Referencia{}, fmt.Errorf("%w: %s respondeu %d", errIndisponivel, plataforma, res.StatusCode)
	default:
		return Referencia{}, fmt.Errorf("%w: %s respondeu %d", errPlataformaFora, plataforma, res.StatusCode)
	}

	var corpo struct {
		Title          string `json:"title"`
		AuthorName     string `json:"author_name"`
		ThumbnailURL   string `json:"thumbnail_url"`
		EmbedProductID string `json:"embed_product_id"` // TikTok: id do vídeo
		AuthorUniqueID string `json:"author_unique_id"` // TikTok: @ do autor
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxRespostaOEm)).Decode(&corpo); err != nil {
		return Referencia{}, fmt.Errorf("%w: resposta inválida: %w", errPlataformaFora, err)
	}
	r := Referencia{
		Plataforma: plataforma, EmbedID: id, URL: canonica,
		Titulo: cortar(corpo.Title, maxTextoVideo), Autor: cortar(corpo.AuthorName, maxTextoVideo),
	}
	if t, err := url.Parse(corpo.ThumbnailURL); err == nil && t.Scheme == "https" && t.Host != "" {
		r.ThumbURL = corpo.ThumbnailURL
	}
	if plataforma == PlataformaTikTok {
		if idTikTok.MatchString(corpo.EmbedProductID) {
			r.EmbedID = corpo.EmbedProductID
		}
		if r.EmbedID == "" {
			return Referencia{}, errLinkVideo
		}
		if id == "" && corpo.AuthorUniqueID != "" && !strings.ContainsAny(corpo.AuthorUniqueID, "/?#") {
			r.URL = "https://www.tiktok.com/@" + corpo.AuthorUniqueID + "/video/" + r.EmbedID
		}
	}
	if o.Cache != nil {
		if b, err := json.Marshal(r); err == nil {
			o.Cache.Gravar(ctx, chave, string(b), validadeCache)
		}
	}
	return r, nil
}

// PlayerURL é o endereço do player oficial da plataforma, para o iframe.
func PlayerURL(plataforma, embedID string) string {
	switch plataforma {
	case PlataformaYouTube:
		return "https://www.youtube-nocookie.com/embed/" + embedID
	case PlataformaTikTok:
		return "https://www.tiktok.com/player/v1/" + embedID
	}
	return ""
}

func cortar(s string, max int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}
