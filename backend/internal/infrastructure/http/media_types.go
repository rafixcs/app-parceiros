package http

import (
	"time"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

type videoResponse struct {
	ID       uuid.UUID `json:"id"`
	Kind     string    `json:"kind"`
	Platform string    `json:"platform"`
	Status   string    `json:"status"`
	Title    string    `json:"title"`
	Author   string    `json:"author"`
	// URL is the page of the video at the platform (embed).
	URL     *string `json:"url"`
	EmbedID *string `json:"embed_id"`
	// PlayerURL is the official player of the platform, for the iframe (embed).
	PlayerURL *string `json:"player_url"`
	// ThumbnailURL comes from the platform (embed) or is made by the worker
	// (ready upload, signed URL).
	ThumbnailURL *string `json:"thumbnail_url"`
	// PreviewURL is the 720p preview of a ready upload (signed URL).
	PreviewURL  *string     `json:"preview_url"`
	DurationS   *int32      `json:"duration_s"`
	Width       *int32      `json:"width"`
	Height      *int32      `json:"height"`
	SizeBytes   int64       `json:"size_bytes"`
	FileName    *string     `json:"file_name"`
	ContentType *string     `json:"content_type"`
	Shared      bool        `json:"shared"`
	Mine        bool        `json:"mine"`
	ProductIDs  []uuid.UUID `json:"product_ids"`
	ListIDs     []uuid.UUID `json:"list_ids"`
	CreatedAt   time.Time   `json:"created_at"`
}

// videoResponseOf is the JSON of a video, also used by the curated lists.
func videoResponseOf(v domain.VideoView) videoResponse {
	products, lists := v.ProductIDs, v.ListIDs
	if products == nil {
		products = []uuid.UUID{}
	}
	if lists == nil {
		lists = []uuid.UUID{}
	}
	return videoResponse{
		ID: v.ID, Kind: string(v.Kind), Platform: v.Platform, Status: string(v.Status), Title: v.Title, Author: v.Author,
		URL: v.URL, EmbedID: v.EmbedID, PlayerURL: v.PlayerURL, ThumbnailURL: v.ThumbnailURL, PreviewURL: v.PreviewURL,
		DurationS: v.DurationS, Width: v.Width, Height: v.Height, SizeBytes: v.SizeBytes, FileName: v.FileName,
		ContentType: v.ContentType, Shared: v.Shared, Mine: v.Mine, ProductIDs: products, ListIDs: lists,
		CreatedAt: v.CreatedAt,
	}
}

type pasteVideoRequest struct {
	URL       string     `json:"url"`
	ProductID *uuid.UUID `json:"product_id"`
}

type startUploadRequest struct {
	FileName    string     `json:"file_name"`
	ContentType string     `json:"content_type"`
	SizeBytes   int64      `json:"size_bytes"`
	UsageRights bool       `json:"usage_rights"`
	ProductID   *uuid.UUID `json:"product_id"`
}

type startedUploadResponse struct {
	Video    videoResponse `json:"video"`
	UploadID string        `json:"upload_id"`
	Key      string        `json:"key"`
}

type updateVideoRequest struct {
	Title  *string `json:"title"`
	Shared *bool   `json:"shared"`
}

type uploadPartJSON struct {
	Number int    `json:"number"`
	ETag   string `json:"etag"`
	Size   int64  `json:"size,omitempty"`
}

type signPartRequest struct {
	Number int `json:"number"`
}

type completeUploadRequest struct {
	Parts []uploadPartJSON `json:"parts"`
}

type signedURLResponse struct {
	URL string `json:"url"`
}

type videoQuotaResponse struct {
	UsedBytes  int64 `json:"used_bytes"`
	LimitBytes int64 `json:"limit_bytes"`
}
