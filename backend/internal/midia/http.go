package midia

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/httpserver"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/storage"
)

type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Modulo registra as rotas sob /v1/workspaces/{workspaceId}.
func (h *Handler) Modulo() contas.Modulo {
	return contas.Modulo{DoWorkspace: func(r chi.Router) {
		r.Get("/videos", h.listar)
		r.Get("/videos/cota", h.cota)
		r.Post("/videos/embed", h.colar)
		r.Post("/videos/uploads", h.iniciarUpload)
		r.Get("/videos/{videoId}", h.ver)
		r.Patch("/videos/{videoId}", h.editar)
		r.Delete("/videos/{videoId}", h.apagar)
		r.Get("/videos/{videoId}/partes", h.partes)
		r.Post("/videos/{videoId}/partes", h.assinarParte)
		r.Post("/videos/{videoId}/concluir", h.concluir)
		r.Get("/videos/{videoId}/download", h.download)
		r.Put("/videos/{videoId}/produtos/{produtoId}", h.vincular)
		r.Delete("/videos/{videoId}/produtos/{produtoId}", h.desvincular)
	}}
}

func membro(r *http.Request) contas.Membro {
	m, _ := contas.MembroDoContexto(r.Context())
	return m
}

func (h *Handler) listar(w http.ResponseWriter, r *http.Request) {
	var produtoID *uuid.UUID
	if v := r.URL.Query().Get("produto_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			h.erro(w, r, ErrProdutoNaoEncontrado)
			return
		}
		produtoID = &id
	}
	vs, err := h.svc.Videos(r.Context(), membro(r), produtoID, r.URL.Query().Get("meus") == "true")
	h.responder(w, r, http.StatusOK, vs, err)
}

func (h *Handler) cota(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.Cota(r.Context(), membro(r))
	h.responder(w, r, http.StatusOK, c, err)
}

func (h *Handler) colar(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL       string     `json:"url"`
		ProdutoID *uuid.UUID `json:"produto_id"`
	}
	if !h.ler(w, r, &in, maxCorpo) {
		return
	}
	v, criado, err := h.svc.ColarLink(r.Context(), membro(r), in.URL, in.ProdutoID)
	status := http.StatusOK
	if criado {
		status = http.StatusCreated
	}
	h.responder(w, r, status, v, err)
}

func (h *Handler) iniciarUpload(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Nome        string     `json:"nome"`
		ContentType string     `json:"content_type"`
		Tamanho     int64      `json:"tamanho"`
		DireitoUso  bool       `json:"direito_uso"`
		ProdutoID   *uuid.UUID `json:"produto_id"`
	}
	if !h.ler(w, r, &in, maxCorpo) {
		return
	}
	u, err := h.svc.IniciarUpload(r.Context(), membro(r), NovoUpload{
		Nome: in.Nome, ContentType: in.ContentType, Tamanho: in.Tamanho, DireitoUso: in.DireitoUso, ProdutoID: in.ProdutoID,
	})
	h.responder(w, r, http.StatusCreated, u, err)
}

func (h *Handler) ver(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "videoId", ErrVideoNaoEncontrado)
	if !ok {
		return
	}
	v, err := h.svc.Ver(r.Context(), membro(r), id)
	h.responder(w, r, http.StatusOK, v, err)
}

func (h *Handler) editar(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "videoId", ErrVideoNaoEncontrado)
	if !ok {
		return
	}
	var in struct {
		Titulo        *string `json:"titulo"`
		Compartilhado *bool   `json:"compartilhado"`
	}
	if !h.ler(w, r, &in, maxCorpo) {
		return
	}
	v, err := h.svc.Editar(r.Context(), membro(r), id, in.Titulo, in.Compartilhado)
	h.responder(w, r, http.StatusOK, v, err)
}

func (h *Handler) apagar(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "videoId", ErrVideoNaoEncontrado)
	if !ok {
		return
	}
	h.responder(w, r, http.StatusNoContent, nil, h.svc.Apagar(r.Context(), membro(r), id))
}

type parteJSON struct {
	Numero  int    `json:"numero"`
	ETag    string `json:"etag"`
	Tamanho int64  `json:"tamanho,omitempty"`
}

func (h *Handler) partes(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "videoId", ErrVideoNaoEncontrado)
	if !ok {
		return
	}
	ps, err := h.svc.Partes(r.Context(), membro(r), id)
	out := make([]parteJSON, len(ps))
	for i, p := range ps {
		out[i] = parteJSON{Numero: p.Numero, ETag: p.ETag, Tamanho: p.Tamanho}
	}
	h.responder(w, r, http.StatusOK, out, err)
}

func (h *Handler) assinarParte(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "videoId", ErrVideoNaoEncontrado)
	if !ok {
		return
	}
	var in struct {
		Numero int `json:"numero"`
	}
	if !h.ler(w, r, &in, maxCorpo) {
		return
	}
	url, err := h.svc.AssinarParte(r.Context(), membro(r), id, in.Numero)
	h.responder(w, r, http.StatusOK, map[string]string{"url": url}, err)
}

func (h *Handler) concluir(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "videoId", ErrVideoNaoEncontrado)
	if !ok {
		return
	}
	var in struct {
		Partes []parteJSON `json:"partes"`
	}
	if !h.ler(w, r, &in, maxCorpoPartes) {
		return
	}
	ps := make([]storage.Parte, len(in.Partes))
	for i, p := range in.Partes {
		ps[i] = storage.Parte{Numero: p.Numero, ETag: p.ETag}
	}
	v, err := h.svc.ConcluirUpload(r.Context(), membro(r), id, ps)
	h.responder(w, r, http.StatusOK, v, err)
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "videoId", ErrVideoNaoEncontrado)
	if !ok {
		return
	}
	url, err := h.svc.Download(r.Context(), membro(r), id)
	h.responder(w, r, http.StatusOK, map[string]string{"url": url}, err)
}

func (h *Handler) vincular(w http.ResponseWriter, r *http.Request) {
	h.vinculo(w, r, h.svc.VincularProduto)
}

func (h *Handler) desvincular(w http.ResponseWriter, r *http.Request) {
	h.vinculo(w, r, h.svc.DesvincularProduto)
}

func (h *Handler) vinculo(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, m contas.Membro, id, produtoID uuid.UUID) (Video, error)) {
	id, ok := h.id(w, r, "videoId", ErrVideoNaoEncontrado)
	if !ok {
		return
	}
	produtoID, ok := h.id(w, r, "produtoId", ErrProdutoNaoEncontrado)
	if !ok {
		return
	}
	v, err := fn(r.Context(), membro(r), id, produtoID)
	h.responder(w, r, http.StatusOK, v, err)
}

func (h *Handler) id(w http.ResponseWriter, r *http.Request, param string, naoEncontrado error) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, param))
	if err != nil {
		h.erro(w, r, naoEncontrado)
		return uuid.Nil, false
	}
	return id, true
}

const (
	maxCorpo       = 32 << 10
	maxCorpoPartes = 1 << 20 // até 10 mil partes
)

func (h *Handler) ler(w http.ResponseWriter, r *http.Request, v any, max int64) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, max))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httpserver.JSONErro(w, http.StatusBadRequest, "json_invalido", "O corpo da requisição não é um JSON válido.")
		return false
	}
	return true
}

func (h *Handler) responder(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	switch {
	case err != nil:
		h.erro(w, r, err)
	case status == http.StatusNoContent:
		w.WriteHeader(status)
	default:
		httpserver.JSON(w, status, v)
	}
}

func (h *Handler) erro(w http.ResponseWriter, r *http.Request, err error) {
	var e *Erro
	if errors.As(err, &e) {
		httpserver.JSONErro(w, e.Status, e.Codigo, e.Mensagem)
		return
	}
	h.log.ErrorContext(r.Context(), "erro interno na mídia", "err", err)
	httpserver.JSONErro(w, http.StatusInternalServerError, "erro_interno", "Algo deu errado. Tente de novo em instantes.")
}
