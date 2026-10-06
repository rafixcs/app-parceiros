package oembed

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

func TestIdentify(t *testing.T) {
	cases := []struct {
		link, platform, id, canonical string
	}{
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=10s", domain.PlatformYouTube, "dQw4w9WgXcQ", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{"youtube.com/shorts/dQw4w9WgXcQ?feature=share", domain.PlatformYouTube, "dQw4w9WgXcQ", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{"https://m.youtube.com/watch?v=dQw4w9WgXcQ", domain.PlatformYouTube, "dQw4w9WgXcQ", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{"https://youtu.be/dQw4w9WgXcQ?si=abc", domain.PlatformYouTube, "dQw4w9WgXcQ", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{" https://www.tiktok.com/@loja.achados/video/7350000000000000001?lang=pt-BR ", domain.PlatformTikTok, "7350000000000000001", "https://www.tiktok.com/@loja.achados/video/7350000000000000001"},
		{"https://vm.tiktok.com/ZMabc123/", domain.PlatformTikTok, "", "https://vm.tiktok.com/ZMabc123/"},
		{"https://www.tiktok.com/t/ZTabc/", domain.PlatformTikTok, "", "https://www.tiktok.com/t/ZTabc/"},
	}
	for _, c := range cases {
		p, id, can, err := identify(c.link)
		if err != nil || p != c.platform || id != c.id || can != c.canonical {
			t.Errorf("identify(%q) = %q %q %q %v", c.link, p, id, can, err)
		}
	}
	for _, link := range []string{
		"", "https://www.youtube.com/watch?v=curto", "https://www.youtube.com/@canal", "https://vimeo.com/123",
		"https://shopee.com.br/video/123", "https://www.tiktok.com/@loja/photo/7350000000000000001",
		"ftp://youtube.com/watch?v=dQw4w9WgXcQ", "https://youtube.com.golpe.com/watch?v=dQw4w9WgXcQ",
	} {
		if _, _, _, err := identify(link); !errors.Is(err, domain.ErrInvalidVideoLink) {
			t.Errorf("identify(%q) accepted: %v", link, err)
		}
	}
}

type memCache struct {
	mu sync.Mutex
	m  map[string]string
}

func (c *memCache) Get(_ context.Context, key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[key]
	return v, ok
}

func (c *memCache) Set(_ context.Context, key, value string, _ time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = value
}

// TestResolve reads the official answers, keeps them in the cache and maps
// the platform failures to the domain errors.
func TestResolve(t *testing.T) {
	calls := 0
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		switch r.URL.Path {
		case "/youtube":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"title": "  Review  ", "author_name": "Canal", "thumbnail_url": "http://inseguro/thumb.jpg",
			})
		case "/tiktok":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"title": "Unboxing", "author_name": "Criadora", "author_unique_id": "criadora",
				"thumbnail_url": "https://p16.tiktokcdn.com/t.jpeg", "embed_product_id": "7350000000000000001",
			})
		}
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), YouTubeURL: srv.URL + "/youtube", TikTokURL: srv.URL + "/tiktok", Cache: &memCache{m: map[string]string{}}}
	ctx := context.Background()

	r, err := c.Resolve(ctx, "https://youtu.be/dQw4w9WgXcQ", true)
	if err != nil || r.Title != "Review" || r.Author != "Canal" || r.ThumbnailURL != "" || r.EmbedID != "dQw4w9WgXcQ" {
		t.Fatalf("youtube: %+v %v", r, err)
	}
	if _, err := c.Resolve(ctx, "https://www.youtube.com/watch?v=dQw4w9WgXcQ", true); err != nil || calls != 1 {
		t.Fatalf("cache: %d calls, %v", calls, err)
	}
	r, err = c.Resolve(ctx, "https://vm.tiktok.com/ZMabc123/", true)
	if err != nil || r.EmbedID != "7350000000000000001" || r.URL != "https://www.tiktok.com/@criadora/video/7350000000000000001" {
		t.Fatalf("tiktok: %+v %v", r, err)
	}

	status = http.StatusNotFound
	if _, err := c.Resolve(ctx, "https://youtu.be/bbbbbbbbbbb", true); !errors.Is(err, domain.ErrVideoUnavailable) {
		t.Fatalf("404: %v", err)
	}
	status = http.StatusTooManyRequests
	if _, err := c.Resolve(ctx, "https://youtu.be/bbbbbbbbbbb", true); !errors.Is(err, domain.ErrPlatformUnavailable) {
		t.Fatalf("429: %v", err)
	}
	// Without cache (revalidation) it asks the platform again.
	if _, err := c.Resolve(ctx, "https://youtu.be/dQw4w9WgXcQ", false); !errors.Is(err, domain.ErrPlatformUnavailable) {
		t.Fatalf("without cache: %v", err)
	}
}
