// Package oembed resolves YouTube and TikTok links through their official
// oEmbed endpoints (domain.EmbedResolver). Only metadata is read: the video is
// never downloaded, it plays in the platform's player.
package oembed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// Cache keeps the oEmbed answers (Redis in production).
type Cache interface {
	Get(ctx context.Context, key string) (string, bool)
	Set(ctx context.Context, key, value string, ttl time.Duration)
}

// RedisCache is the Cache on Redis. Redis failures only lose the cache.
type RedisCache struct{ R *redis.Client }

func (c RedisCache) Get(ctx context.Context, key string) (string, bool) {
	v, err := c.R.Get(ctx, key).Result()
	return v, err == nil
}

func (c RedisCache) Set(ctx context.Context, key, value string, ttl time.Duration) {
	_ = c.R.Set(ctx, key, value, ttl).Err()
}

// Client resolves links at the official endpoints.
type Client struct {
	HTTP       *http.Client
	YouTubeURL string // default https://www.youtube.com/oembed
	TikTokURL  string // default https://www.tiktok.com/oembed
	Cache      Cache  // optional
}

var _ domain.EmbedResolver = (*Client)(nil)

const (
	cacheTTL       = 24 * time.Hour
	maxAnswerBytes = 64 << 10
	defaultYouTube = "https://www.youtube.com/oembed"
	defaultTikTok  = "https://www.tiktok.com/oembed"
	cacheKeyPrefix = "oembed:"
	maxLinkLength  = 2000
	defaultTimeout = 8 * time.Second
)

var (
	youtubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	tiktokID  = regexp.MustCompile(`^[0-9]{8,25}$`)
)

// identify recognizes the platform and, when possible, the id of the video,
// and returns the canonical URL for the oEmbed. TikTok short links only have
// the id after the oEmbed.
func identify(link string) (platform, id, canonical string, err error) {
	link = strings.TrimSpace(link)
	if link == "" || len(link) > maxLinkLength {
		return "", "", "", domain.ErrInvalidVideoLink
	}
	if !strings.Contains(link, "://") {
		link = "https://" + link
	}
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", "", "", domain.ErrInvalidVideoLink
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")

	switch host {
	case "youtube.com", "m.youtube.com":
		switch {
		case len(parts) == 1 && parts[0] == "watch":
			id = u.Query().Get("v")
		case len(parts) == 2 && (parts[0] == "shorts" || parts[0] == "embed" || parts[0] == "live" || parts[0] == "v"):
			id = parts[1]
		}
		if !youtubeID.MatchString(id) {
			return "", "", "", domain.ErrInvalidVideoLink
		}
		return domain.PlatformYouTube, id, "https://www.youtube.com/watch?v=" + id, nil
	case "youtu.be":
		if len(parts) != 1 || !youtubeID.MatchString(parts[0]) {
			return "", "", "", domain.ErrInvalidVideoLink
		}
		return domain.PlatformYouTube, parts[0], "https://www.youtube.com/watch?v=" + parts[0], nil
	case "tiktok.com", "m.tiktok.com":
		if len(parts) == 3 && strings.HasPrefix(parts[0], "@") && len(parts[0]) > 1 && parts[1] == "video" && tiktokID.MatchString(parts[2]) {
			return domain.PlatformTikTok, parts[2], "https://www.tiktok.com/" + parts[0] + "/video/" + parts[2], nil
		}
		if len(parts) == 2 && parts[0] == "t" && parts[1] != "" {
			return domain.PlatformTikTok, "", "https://www.tiktok.com/t/" + parts[1] + "/", nil
		}
	case "vm.tiktok.com", "vt.tiktok.com":
		if len(parts) == 1 && parts[0] != "" {
			return domain.PlatformTikTok, "", "https://" + host + "/" + parts[0] + "/", nil
		}
	}
	return "", "", "", domain.ErrInvalidVideoLink
}

// Resolve fetches the metadata of the video at the official oEmbed. With
// useCache, good answers stay a day in the cache.
func (c *Client) Resolve(ctx context.Context, link string, useCache bool) (domain.EmbedRef, error) {
	platform, id, canonical, err := identify(link)
	if err != nil {
		return domain.EmbedRef{}, err
	}
	sum := sha256.Sum256([]byte(canonical))
	key := cacheKeyPrefix + hex.EncodeToString(sum[:16])
	if useCache && c.Cache != nil {
		if v, ok := c.Cache.Get(ctx, key); ok {
			var r domain.EmbedRef
			if json.Unmarshal([]byte(v), &r) == nil {
				return r, nil
			}
		}
	}

	endpoint := c.YouTubeURL
	if endpoint == "" {
		endpoint = defaultYouTube
	}
	if platform == domain.PlatformTikTok {
		endpoint = c.TikTokURL
		if endpoint == "" {
			endpoint = defaultTikTok
		}
	}
	q := url.Values{"url": {canonical}, "format": {"json"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return domain.EmbedRef{}, err
	}
	req.Header.Set("Accept", "application/json")
	cli := c.HTTP
	if cli == nil {
		cli = &http.Client{Timeout: defaultTimeout}
	}
	res, err := cli.Do(req)
	if err != nil {
		return domain.EmbedRef{}, fmt.Errorf("%w: %w", domain.ErrPlatformUnavailable, err)
	}
	defer func() { _ = res.Body.Close() }()
	switch {
	case res.StatusCode == http.StatusOK:
	case res.StatusCode >= 400 && res.StatusCode < 500 && res.StatusCode != http.StatusTooManyRequests:
		// Private, removed, missing or not allowed to be embedded.
		return domain.EmbedRef{}, fmt.Errorf("%w: %s answered %d", domain.ErrVideoUnavailable, platform, res.StatusCode)
	default:
		return domain.EmbedRef{}, fmt.Errorf("%w: %s answered %d", domain.ErrPlatformUnavailable, platform, res.StatusCode)
	}

	var body struct {
		Title          string `json:"title"`
		AuthorName     string `json:"author_name"`
		ThumbnailURL   string `json:"thumbnail_url"`
		EmbedProductID string `json:"embed_product_id"` // TikTok: id of the video
		AuthorUniqueID string `json:"author_unique_id"` // TikTok: @ of the author
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxAnswerBytes)).Decode(&body); err != nil {
		return domain.EmbedRef{}, fmt.Errorf("%w: invalid answer: %w", domain.ErrPlatformUnavailable, err)
	}
	r := domain.EmbedRef{
		Platform: platform, EmbedID: id, URL: canonical,
		Title: truncate(body.Title, domain.MaxVideoText), Author: truncate(body.AuthorName, domain.MaxVideoText),
	}
	if t, err := url.Parse(body.ThumbnailURL); err == nil && t.Scheme == "https" && t.Host != "" {
		r.ThumbnailURL = body.ThumbnailURL
	}
	if platform == domain.PlatformTikTok {
		if tiktokID.MatchString(body.EmbedProductID) {
			r.EmbedID = body.EmbedProductID
		}
		if r.EmbedID == "" {
			return domain.EmbedRef{}, domain.ErrInvalidVideoLink
		}
		if id == "" && body.AuthorUniqueID != "" && !strings.ContainsAny(body.AuthorUniqueID, "/?#") {
			r.URL = "https://www.tiktok.com/@" + body.AuthorUniqueID + "/video/" + r.EmbedID
		}
	}
	if c.Cache != nil {
		if b, err := json.Marshal(r); err == nil {
			c.Cache.Set(ctx, key, string(b), cacheTTL)
		}
	}
	return r, nil
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}
