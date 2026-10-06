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

// CurationService is what the curated lists handler needs
// (service.CurationService).
type CurationService interface {
	Lists(ctx context.Context, m domain.Member) ([]domain.CuratedList, error)
	Create(ctx context.Context, m domain.Member, title, description string) (domain.CuratedListDetail, error)
	Get(ctx context.Context, m domain.Member, id uuid.UUID) (domain.CuratedListDetail, error)
	Update(ctx context.Context, m domain.Member, id uuid.UUID, title, description *string) (domain.CuratedListDetail, error)
	Delete(ctx context.Context, m domain.Member, id uuid.UUID) error
	AttachVideo(ctx context.Context, m domain.Member, id, videoID uuid.UUID) (domain.CuratedListDetail, error)
	DetachVideo(ctx context.Context, m domain.Member, id, videoID uuid.UUID) (domain.CuratedListDetail, error)
	AddProduct(ctx context.Context, m domain.Member, id uuid.UUID, productID *uuid.UUID, link, comment string) (domain.CuratedListDetail, error)
	Comment(ctx context.Context, m domain.Member, id, productID uuid.UUID, comment string) (domain.CuratedListDetail, error)
	RemoveProduct(ctx context.Context, m domain.Member, id, productID uuid.UUID) (domain.CuratedListDetail, error)
	Reorder(ctx context.Context, m domain.Member, id uuid.UUID, productIDs []uuid.UUID) (domain.CuratedListDetail, error)
	Publish(ctx context.Context, m domain.Member, id uuid.UUID) (domain.CuratedListDetail, error)
	Import(ctx context.Context, m domain.Member, id uuid.UUID, productIDs []uuid.UUID, intoCollection bool) (domain.ImportResult, error)
	Dashboard(ctx context.Context, m domain.Member, id uuid.UUID) (domain.ListDashboard, error)
}

// CurationHandler serves the curated lists of a mentorship.
type CurationHandler struct {
	svc CurationService
	log *slog.Logger
}

func NewCurationHandler(svc CurationService, log *slog.Logger) *CurationHandler {
	return &CurationHandler{svc: svc, log: log}
}

const maxCurationBody = 32 << 10

// Routes registers the lists under /v1/workspaces/{workspaceId}.
func (h *CurationHandler) Routes() Routes {
	return Routes{Workspace: func(r chi.Router) {
		r.Get("/lists", h.lists)
		r.Post("/lists", h.create)
		r.Get("/lists/{listId}", h.get)
		r.Patch("/lists/{listId}", h.update)
		r.Delete("/lists/{listId}", h.delete)
		r.Post("/lists/{listId}/items", h.addProduct)
		r.Put("/lists/{listId}/order", h.reorder)
		r.Patch("/lists/{listId}/items/{productId}", h.comment)
		r.Delete("/lists/{listId}/items/{productId}", h.removeProduct)
		r.Post("/lists/{listId}/publish", h.publish)
		r.Post("/lists/{listId}/import", h.importList)
		r.Get("/lists/{listId}/dashboard", h.dashboard)
		r.Put("/lists/{listId}/videos/{videoId}", h.attachVideo)
		r.Delete("/lists/{listId}/videos/{videoId}", h.detachVideo)
	}}
}

func (h *CurationHandler) lists(w nethttp.ResponseWriter, r *nethttp.Request) {
	ls, err := h.svc.Lists(r.Context(), currentMember(r))
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, mapSlice(ls, listResponseOf))
}

func (h *CurationHandler) create(w nethttp.ResponseWriter, r *nethttp.Request) {
	var in listRequest
	if !decodeBody(w, r, maxCurationBody, &in) {
		return
	}
	var title, description string
	if in.Title != nil {
		title = *in.Title
	}
	if in.Description != nil {
		description = *in.Description
	}
	l, err := h.svc.Create(r.Context(), currentMember(r), title, description)
	h.detail(w, r, nethttp.StatusCreated, l, err)
}

func (h *CurationHandler) get(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.listID(w, r)
	if !ok {
		return
	}
	l, err := h.svc.Get(r.Context(), currentMember(r), id)
	h.detail(w, r, nethttp.StatusOK, l, err)
}

func (h *CurationHandler) update(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.listID(w, r)
	if !ok {
		return
	}
	var in listRequest
	if !decodeBody(w, r, maxCurationBody, &in) {
		return
	}
	l, err := h.svc.Update(r.Context(), currentMember(r), id, in.Title, in.Description)
	h.detail(w, r, nethttp.StatusOK, l, err)
}

func (h *CurationHandler) delete(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.listID(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), currentMember(r), id); err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

func (h *CurationHandler) addProduct(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.listID(w, r)
	if !ok {
		return
	}
	var in addListItemRequest
	if !decodeBody(w, r, maxCurationBody, &in) {
		return
	}
	l, err := h.svc.AddProduct(r.Context(), currentMember(r), id, in.ProductID, in.URL, in.Comment)
	h.detail(w, r, nethttp.StatusCreated, l, err)
}

func (h *CurationHandler) reorder(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.listID(w, r)
	if !ok {
		return
	}
	var in reorderListRequest
	if !decodeBody(w, r, maxCurationBody, &in) {
		return
	}
	l, err := h.svc.Reorder(r.Context(), currentMember(r), id, in.ProductIDs)
	h.detail(w, r, nethttp.StatusOK, l, err)
}

func (h *CurationHandler) comment(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, productID, ok := h.listAndProduct(w, r)
	if !ok {
		return
	}
	var in commentListItemRequest
	if !decodeBody(w, r, maxCurationBody, &in) {
		return
	}
	if in.Comment == nil {
		WriteError(w, r, h.log, domain.ErrListCommentRequired)
		return
	}
	l, err := h.svc.Comment(r.Context(), currentMember(r), id, productID, *in.Comment)
	h.detail(w, r, nethttp.StatusOK, l, err)
}

func (h *CurationHandler) removeProduct(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, productID, ok := h.listAndProduct(w, r)
	if !ok {
		return
	}
	l, err := h.svc.RemoveProduct(r.Context(), currentMember(r), id, productID)
	h.detail(w, r, nethttp.StatusOK, l, err)
}

func (h *CurationHandler) publish(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.listID(w, r)
	if !ok {
		return
	}
	l, err := h.svc.Publish(r.Context(), currentMember(r), id)
	h.detail(w, r, nethttp.StatusOK, l, err)
}

func (h *CurationHandler) importList(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.listID(w, r)
	if !ok {
		return
	}
	var in importListRequest
	if !decodeBody(w, r, maxCurationBody, &in) {
		return
	}
	intoCollection := in.Collection == nil || *in.Collection
	res, err := h.svc.Import(r.Context(), currentMember(r), id, in.ProductIDs, intoCollection)
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, listImportResponse{
		Created: res.Created, AlreadySaved: res.AlreadySaved, CollectionID: res.CollectionID, LinkStatus: res.LinkStatus,
	})
}

func (h *CurationHandler) dashboard(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.listID(w, r)
	if !ok {
		return
	}
	d, err := h.svc.Dashboard(r.Context(), currentMember(r), id)
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, listDashboardResponseOf(d))
}

func (h *CurationHandler) attachVideo(w nethttp.ResponseWriter, r *nethttp.Request) {
	h.video(w, r, h.svc.AttachVideo)
}

func (h *CurationHandler) detachVideo(w nethttp.ResponseWriter, r *nethttp.Request) {
	h.video(w, r, h.svc.DetachVideo)
}

func (h *CurationHandler) video(w nethttp.ResponseWriter, r *nethttp.Request,
	fn func(context.Context, domain.Member, uuid.UUID, uuid.UUID) (domain.CuratedListDetail, error),
) {
	id, ok := h.listID(w, r)
	if !ok {
		return
	}
	videoID, ok := h.pathID(w, r, "videoId", domain.ErrVideoNotFound)
	if !ok {
		return
	}
	l, err := fn(r.Context(), currentMember(r), id, videoID)
	h.detail(w, r, nethttp.StatusOK, l, err)
}

func (h *CurationHandler) listID(w nethttp.ResponseWriter, r *nethttp.Request) (uuid.UUID, bool) {
	return h.pathID(w, r, "listId", domain.ErrListNotFound)
}

func (h *CurationHandler) listAndProduct(w nethttp.ResponseWriter, r *nethttp.Request) (uuid.UUID, uuid.UUID, bool) {
	id, ok := h.listID(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	productID, ok := h.pathID(w, r, "productId", domain.ErrListItemNotFound)
	return id, productID, ok
}

// pathID reads a uuid of the path; a malformed one is not found.
func (h *CurationHandler) pathID(w nethttp.ResponseWriter, r *nethttp.Request, param string, notFound error) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, param))
	if err != nil {
		WriteError(w, r, h.log, notFound)
		return uuid.Nil, false
	}
	return id, true
}

func (h *CurationHandler) detail(w nethttp.ResponseWriter, r *nethttp.Request, status int, l domain.CuratedListDetail, err error) {
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, status, listDetailResponseOf(l))
}
