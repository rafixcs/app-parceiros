package http

import (
	"context"
	"log/slog"
	nethttp "net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

// MediaService is what the video handler needs (service.MediaService).
type MediaService interface {
	Videos(ctx context.Context, m domain.Member, productID *uuid.UUID, onlyMine bool) ([]domain.VideoView, error)
	Get(ctx context.Context, m domain.Member, id uuid.UUID) (domain.VideoView, error)
	PasteLink(ctx context.Context, m domain.Member, link string, productID *uuid.UUID) (domain.VideoView, bool, error)
	StartUpload(ctx context.Context, m domain.Member, n domain.NewUpload) (domain.StartedUpload, error)
	SignPart(ctx context.Context, m domain.Member, id uuid.UUID, number int) (string, error)
	Parts(ctx context.Context, m domain.Member, id uuid.UUID) ([]domain.UploadPart, error)
	CompleteUpload(ctx context.Context, m domain.Member, id uuid.UUID, parts []domain.UploadPart) (domain.VideoView, error)
	Delete(ctx context.Context, m domain.Member, id uuid.UUID) error
	Update(ctx context.Context, m domain.Member, id uuid.UUID, title *string, shared *bool) (domain.VideoView, error)
	Download(ctx context.Context, m domain.Member, id uuid.UUID) (string, error)
	LinkProduct(ctx context.Context, m domain.Member, id, productID uuid.UUID) (domain.VideoView, error)
	UnlinkProduct(ctx context.Context, m domain.Member, id, productID uuid.UUID) (domain.VideoView, error)
	Quota(ctx context.Context, m domain.Member) (domain.VideoQuota, error)
}

// MediaHandler serves the video library.
type MediaHandler struct {
	svc MediaService
	log *slog.Logger
}

func NewMediaHandler(svc MediaService, log *slog.Logger) *MediaHandler {
	return &MediaHandler{svc: svc, log: log}
}

const (
	maxMediaBody = 32 << 10
	// maxPartsBody fits up to 10 thousand parts.
	maxPartsBody = 1 << 20
)

// Routes registers the videos under /v1/workspaces/{workspaceId}.
func (h *MediaHandler) Routes() Routes {
	return Routes{Workspace: func(r chi.Router) {
		r.Get("/videos", h.list)
		r.Get("/videos/quota", h.quota)
		r.Post("/videos/embed", h.paste)
		r.Post("/videos/uploads", h.startUpload)
		r.Get("/videos/{videoId}", h.get)
		r.Patch("/videos/{videoId}", h.update)
		r.Delete("/videos/{videoId}", h.delete)
		r.Get("/videos/{videoId}/parts", h.parts)
		r.Post("/videos/{videoId}/parts", h.signPart)
		r.Post("/videos/{videoId}/complete", h.complete)
		r.Get("/videos/{videoId}/download", h.download)
		r.Put("/videos/{videoId}/products/{productId}", h.linkProduct)
		r.Delete("/videos/{videoId}/products/{productId}", h.unlinkProduct)
	}}
}

func (h *MediaHandler) list(w nethttp.ResponseWriter, r *nethttp.Request) {
	var productID *uuid.UUID
	if v := r.URL.Query().Get("product_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			WriteError(w, r, h.log, domain.ErrProductNotFound)
			return
		}
		productID = &id
	}
	vs, err := h.svc.Videos(r.Context(), currentMember(r), productID, r.URL.Query().Get("mine") == "true")
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, mapSlice(vs, videoResponseOf))
}

func (h *MediaHandler) quota(w nethttp.ResponseWriter, r *nethttp.Request) {
	q, err := h.svc.Quota(r.Context(), currentMember(r))
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, videoQuotaResponse{UsedBytes: q.UsedBytes, LimitBytes: q.LimitBytes})
}

func (h *MediaHandler) paste(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req pasteVideoRequest
	if !decodeBody(w, r, maxMediaBody, &req) {
		return
	}
	v, created, err := h.svc.PasteLink(r.Context(), currentMember(r), req.URL, req.ProductID)
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	status := nethttp.StatusOK
	if created {
		status = nethttp.StatusCreated
	}
	httputil.JSON(w, status, videoResponseOf(v))
}

func (h *MediaHandler) startUpload(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req startUploadRequest
	if !decodeBody(w, r, maxMediaBody, &req) {
		return
	}
	u, err := h.svc.StartUpload(r.Context(), currentMember(r), domain.NewUpload{
		FileName: req.FileName, ContentType: req.ContentType, SizeBytes: req.SizeBytes,
		UsageRights: req.UsageRights, ProductID: req.ProductID,
	})
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusCreated, startedUploadResponse{Video: videoResponseOf(u.Video), UploadID: u.UploadID, Key: u.Key})
}

func (h *MediaHandler) get(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.videoID(w, r)
	if !ok {
		return
	}
	v, err := h.svc.Get(r.Context(), currentMember(r), id)
	h.video(w, r, v, err)
}

func (h *MediaHandler) update(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.videoID(w, r)
	if !ok {
		return
	}
	var req updateVideoRequest
	if !decodeBody(w, r, maxMediaBody, &req) {
		return
	}
	v, err := h.svc.Update(r.Context(), currentMember(r), id, req.Title, req.Shared)
	h.video(w, r, v, err)
}

func (h *MediaHandler) delete(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.videoID(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), currentMember(r), id); err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

func (h *MediaHandler) parts(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.videoID(w, r)
	if !ok {
		return
	}
	ps, err := h.svc.Parts(r.Context(), currentMember(r), id)
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, mapSlice(ps, func(p domain.UploadPart) uploadPartJSON {
		return uploadPartJSON{Number: p.Number, ETag: p.ETag, Size: p.Size}
	}))
}

func (h *MediaHandler) signPart(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.videoID(w, r)
	if !ok {
		return
	}
	var req signPartRequest
	if !decodeBody(w, r, maxMediaBody, &req) {
		return
	}
	url, err := h.svc.SignPart(r.Context(), currentMember(r), id, req.Number)
	h.signedURL(w, r, url, err)
}

func (h *MediaHandler) complete(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.videoID(w, r)
	if !ok {
		return
	}
	var req completeUploadRequest
	if !decodeBody(w, r, maxPartsBody, &req) {
		return
	}
	ps := mapSlice(req.Parts, func(p uploadPartJSON) domain.UploadPart {
		return domain.UploadPart{Number: p.Number, ETag: p.ETag}
	})
	v, err := h.svc.CompleteUpload(r.Context(), currentMember(r), id, ps)
	h.video(w, r, v, err)
}

func (h *MediaHandler) download(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.videoID(w, r)
	if !ok {
		return
	}
	url, err := h.svc.Download(r.Context(), currentMember(r), id)
	h.signedURL(w, r, url, err)
}

func (h *MediaHandler) linkProduct(w nethttp.ResponseWriter, r *nethttp.Request) {
	h.productLink(w, r, h.svc.LinkProduct)
}

func (h *MediaHandler) unlinkProduct(w nethttp.ResponseWriter, r *nethttp.Request) {
	h.productLink(w, r, h.svc.UnlinkProduct)
}

func (h *MediaHandler) productLink(w nethttp.ResponseWriter, r *nethttp.Request,
	fn func(ctx context.Context, m domain.Member, id, productID uuid.UUID) (domain.VideoView, error),
) {
	id, ok := h.videoID(w, r)
	if !ok {
		return
	}
	productID, err := uuid.Parse(chi.URLParam(r, "productId"))
	if err != nil {
		WriteError(w, r, h.log, domain.ErrProductNotFound)
		return
	}
	v, err := fn(r.Context(), currentMember(r), id, productID)
	h.video(w, r, v, err)
}

func (h *MediaHandler) videoID(w nethttp.ResponseWriter, r *nethttp.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "videoId"))
	if err != nil {
		WriteError(w, r, h.log, domain.ErrVideoNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func (h *MediaHandler) video(w nethttp.ResponseWriter, r *nethttp.Request, v domain.VideoView, err error) {
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, videoResponseOf(v))
}

func (h *MediaHandler) signedURL(w nethttp.ResponseWriter, r *nethttp.Request, url string, err error) {
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, signedURLResponse{URL: url})
}
