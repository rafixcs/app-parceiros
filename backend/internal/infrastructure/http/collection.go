package http

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	nethttp "net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

// CollectionService is what the collections handler needs
// (service.CollectionService).
type CollectionService interface {
	Save(ctx context.Context, a domain.Actor, productID *uuid.UUID, link string) (domain.Item, bool, error)
	List(ctx context.Context, a domain.Actor, f domain.ItemFilter) (domain.ItemPage, error)
	Get(ctx context.Context, a domain.Actor, id uuid.UUID) (domain.Item, error)
	Update(ctx context.Context, a domain.Actor, id uuid.UUID, u domain.ItemUpdate) (domain.Item, error)
	GenerateLink(ctx context.Context, a domain.Actor, id uuid.UUID) (domain.Item, error)
	GeneratePending(ctx context.Context, a domain.Actor) (int, error)
	Remove(ctx context.Context, a domain.Actor, id uuid.UUID) error
	SavedProducts(ctx context.Context, a domain.Actor) ([]uuid.UUID, error)
	SetCollections(ctx context.Context, a domain.Actor, id uuid.UUID, collectionIDs []uuid.UUID) (domain.Item, error)
	Collections(ctx context.Context, a domain.Actor) ([]domain.Collection, error)
	CreateCollection(ctx context.Context, a domain.Actor, name string) (domain.Collection, error)
	RenameCollection(ctx context.Context, a domain.Actor, id uuid.UUID, name string) (domain.Collection, error)
	DeleteCollection(ctx context.Context, a domain.Actor, id uuid.UUID) error
}

// CollectionHandler serves the user's saved items and collections.
type CollectionHandler struct {
	svc CollectionService
	log *slog.Logger
}

func NewCollectionHandler(svc CollectionService, log *slog.Logger) *CollectionHandler {
	return &CollectionHandler{svc: svc, log: log}
}

const maxCollectionBody = 16 << 10

// Routes registers the collections under /v1/workspaces/{workspaceId}.
func (h *CollectionHandler) Routes() Routes {
	return Routes{Workspace: func(r chi.Router) {
		r.Get("/items", h.list)
		r.Post("/items", h.save)
		r.Get("/items/products", h.savedProducts)
		r.Post("/items/pending-links", h.generatePending)
		r.Get("/items/{itemId}", h.get)
		r.Patch("/items/{itemId}", h.update)
		r.Delete("/items/{itemId}", h.remove)
		r.Post("/items/{itemId}/link", h.generateLink)
		r.Put("/items/{itemId}/collections", h.setCollections)
		r.Get("/collections", h.collections)
		r.Post("/collections", h.createCollection)
		r.Patch("/collections/{collectionId}", h.renameCollection)
		r.Delete("/collections/{collectionId}", h.deleteCollection)
	}}
}

func collectionActor(r *nethttp.Request) domain.Actor { return currentMember(r).Actor() }

func (h *CollectionHandler) list(w nethttp.ResponseWriter, r *nethttp.Request) {
	q := r.URL.Query()
	f := domain.ItemFilter{Query: q.Get("q"), Status: domain.ItemStatus(q.Get("status")), Tag: q.Get("tag")}
	ok := true
	if v := q.Get("collection"); v != "" {
		id, err := uuid.Parse(v)
		ok = err == nil
		f.CollectionID = &id
	}
	for name, dst := range map[string]*int{"page": &f.Page, "per_page": &f.PerPage} {
		if v := q.Get(name); v != "" && ok {
			var err error
			*dst, err = strconv.Atoi(v)
			ok = err == nil
		}
	}
	if !ok {
		invalidRequest(w)
		return
	}
	p, err := h.svc.List(r.Context(), collectionActor(r), f)
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, itemPageResponse{
		Items: mapSlice(p.Items, itemResponseOf), Total: p.Total, Page: p.Page, PerPage: p.PerPage,
	})
}

func (h *CollectionHandler) save(w nethttp.ResponseWriter, r *nethttp.Request) {
	var in saveItemRequest
	if !decodeBody(w, r, maxCollectionBody, &in) {
		return
	}
	it, created, err := h.svc.Save(r.Context(), collectionActor(r), in.ProductID, in.URL)
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	status := nethttp.StatusOK
	if created {
		status = nethttp.StatusCreated
	}
	httputil.JSON(w, status, itemResponseOf(it))
}

func (h *CollectionHandler) savedProducts(w nethttp.ResponseWriter, r *nethttp.Request) {
	ids, err := h.svc.SavedProducts(r.Context(), collectionActor(r))
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, ids)
}

func (h *CollectionHandler) generatePending(w nethttp.ResponseWriter, r *nethttp.Request) {
	n, err := h.svc.GeneratePending(r.Context(), collectionActor(r))
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusAccepted, pendingLinksResponse{Enqueued: n})
}

func (h *CollectionHandler) get(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.pathID(w, r, "itemId", domain.ErrItemNotFound)
	if !ok {
		return
	}
	it, err := h.svc.Get(r.Context(), collectionActor(r), id)
	h.item(w, r, nethttp.StatusOK, it, err)
}

func (h *CollectionHandler) update(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.pathID(w, r, "itemId", domain.ErrItemNotFound)
	if !ok {
		return
	}
	var in updateItemRequest
	if !decodeBody(w, r, maxCollectionBody, &in) {
		return
	}
	u := domain.ItemUpdate{Title: in.Title, Description: in.Description, Notes: in.Notes, Tags: in.Tags, Status: in.Status}
	if in.AffiliateLink != nil {
		u.ChangeLink = true
		if !bytes.Equal(in.AffiliateLink, []byte("null")) {
			var link string
			if err := json.Unmarshal(in.AffiliateLink, &link); err != nil {
				WriteError(w, r, h.log, domain.ErrInvalidAffiliateLink)
				return
			}
			u.Link = &link
		}
	}
	it, err := h.svc.Update(r.Context(), collectionActor(r), id, u)
	h.item(w, r, nethttp.StatusOK, it, err)
}

func (h *CollectionHandler) remove(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.pathID(w, r, "itemId", domain.ErrItemNotFound)
	if !ok {
		return
	}
	if err := h.svc.Remove(r.Context(), collectionActor(r), id); err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

func (h *CollectionHandler) generateLink(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.pathID(w, r, "itemId", domain.ErrItemNotFound)
	if !ok {
		return
	}
	it, err := h.svc.GenerateLink(r.Context(), collectionActor(r), id)
	h.item(w, r, nethttp.StatusAccepted, it, err)
}

func (h *CollectionHandler) setCollections(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.pathID(w, r, "itemId", domain.ErrItemNotFound)
	if !ok {
		return
	}
	var in setCollectionsRequest
	if !decodeBody(w, r, maxCollectionBody, &in) {
		return
	}
	it, err := h.svc.SetCollections(r.Context(), collectionActor(r), id, in.CollectionIDs)
	h.item(w, r, nethttp.StatusOK, it, err)
}

func (h *CollectionHandler) collections(w nethttp.ResponseWriter, r *nethttp.Request) {
	cs, err := h.svc.Collections(r.Context(), collectionActor(r))
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, mapSlice(cs, collectionResponseOf))
}

func (h *CollectionHandler) createCollection(w nethttp.ResponseWriter, r *nethttp.Request) {
	var in collectionNameRequest
	if !decodeBody(w, r, maxCollectionBody, &in) {
		return
	}
	c, err := h.svc.CreateCollection(r.Context(), collectionActor(r), in.Name)
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusCreated, collectionResponseOf(c))
}

func (h *CollectionHandler) renameCollection(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.pathID(w, r, "collectionId", domain.ErrCollectionNotFound)
	if !ok {
		return
	}
	var in collectionNameRequest
	if !decodeBody(w, r, maxCollectionBody, &in) {
		return
	}
	c, err := h.svc.RenameCollection(r.Context(), collectionActor(r), id, in.Name)
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, nethttp.StatusOK, collectionResponseOf(c))
}

func (h *CollectionHandler) deleteCollection(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, ok := h.pathID(w, r, "collectionId", domain.ErrCollectionNotFound)
	if !ok {
		return
	}
	if err := h.svc.DeleteCollection(r.Context(), collectionActor(r), id); err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

// pathID reads a uuid of the path; a malformed one is not found.
func (h *CollectionHandler) pathID(w nethttp.ResponseWriter, r *nethttp.Request, param string, notFound error) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, param))
	if err != nil {
		WriteError(w, r, h.log, notFound)
		return uuid.Nil, false
	}
	return id, true
}

func (h *CollectionHandler) item(w nethttp.ResponseWriter, r *nethttp.Request, status int, it domain.Item, err error) {
	if err != nil {
		WriteError(w, r, h.log, err)
		return
	}
	httputil.JSON(w, status, itemResponseOf(it))
}
