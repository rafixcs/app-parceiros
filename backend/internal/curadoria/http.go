package curadoria

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/colecoes"
	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/midia"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/httpserver"
)

type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Modulo registra as rotas sob /v1/workspaces/{workspaceId}.
func (h *Handler) Modulo() contas.Modulo {
	return contas.Modulo{DoWorkspace: func(r chi.Router) {
		r.Get("/listas", h.listas)
		r.Post("/listas", h.criar)
		r.Get("/listas/{listaId}", h.ver)
		r.Patch("/listas/{listaId}", h.atualizar)
		r.Delete("/listas/{listaId}", h.apagar)
		r.Post("/listas/{listaId}/itens", h.adicionar)
		r.Put("/listas/{listaId}/ordem", h.ordenar)
		r.Patch("/listas/{listaId}/itens/{produtoId}", h.comentar)
		r.Delete("/listas/{listaId}/itens/{produtoId}", h.remover)
		r.Post("/listas/{listaId}/publicar", h.publicar)
		r.Post("/listas/{listaId}/importar", h.importar)
		r.Get("/listas/{listaId}/painel", h.painel)
		r.Put("/listas/{listaId}/videos/{videoId}", h.anexarVideo)
		r.Delete("/listas/{listaId}/videos/{videoId}", h.tirarVideo)
	}}
}

func membro(r *http.Request) contas.Membro {
	m, _ := contas.MembroDoContexto(r.Context())
	return m
}

func (h *Handler) listas(w http.ResponseWriter, r *http.Request) {
	ls, err := h.svc.Listas(r.Context(), membro(r))
	h.responder(w, r, http.StatusOK, ls, err)
}

type dadosLista struct {
	Titulo    *string `json:"titulo"`
	Descricao *string `json:"descricao"`
}

func (h *Handler) criar(w http.ResponseWriter, r *http.Request) {
	var in dadosLista
	if !h.ler(w, r, &in) {
		return
	}
	var t, d string
	if in.Titulo != nil {
		t = *in.Titulo
	}
	if in.Descricao != nil {
		d = *in.Descricao
	}
	l, err := h.svc.Criar(r.Context(), membro(r), t, d)
	h.responder(w, r, http.StatusCreated, l, err)
}

func (h *Handler) ver(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "listaId", ErrListaNaoEncontrada)
	if !ok {
		return
	}
	l, err := h.svc.Ver(r.Context(), membro(r), id)
	h.responder(w, r, http.StatusOK, l, err)
}

func (h *Handler) atualizar(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "listaId", ErrListaNaoEncontrada)
	if !ok {
		return
	}
	var in dadosLista
	if !h.ler(w, r, &in) {
		return
	}
	l, err := h.svc.Atualizar(r.Context(), membro(r), id, in.Titulo, in.Descricao)
	h.responder(w, r, http.StatusOK, l, err)
}

func (h *Handler) apagar(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "listaId", ErrListaNaoEncontrada)
	if !ok {
		return
	}
	h.responder(w, r, http.StatusNoContent, nil, h.svc.Apagar(r.Context(), membro(r), id))
}

func (h *Handler) adicionar(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "listaId", ErrListaNaoEncontrada)
	if !ok {
		return
	}
	var in struct {
		ProdutoID  *uuid.UUID `json:"produto_id"`
		URL        string     `json:"url"`
		Comentario string     `json:"comentario"`
	}
	if !h.ler(w, r, &in) {
		return
	}
	l, err := h.svc.AdicionarProduto(r.Context(), membro(r), id, in.ProdutoID, in.URL, in.Comentario)
	h.responder(w, r, http.StatusCreated, l, err)
}

func (h *Handler) ordenar(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "listaId", ErrListaNaoEncontrada)
	if !ok {
		return
	}
	var in struct {
		ProdutoIDs []uuid.UUID `json:"produto_ids"`
	}
	if !h.ler(w, r, &in) {
		return
	}
	l, err := h.svc.Ordenar(r.Context(), membro(r), id, in.ProdutoIDs)
	h.responder(w, r, http.StatusOK, l, err)
}

func (h *Handler) comentar(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "listaId", ErrListaNaoEncontrada)
	if !ok {
		return
	}
	produtoID, ok := h.id(w, r, "produtoId", ErrItemNaoEncontrado)
	if !ok {
		return
	}
	var in struct {
		Comentario *string `json:"comentario"`
	}
	if !h.ler(w, r, &in) {
		return
	}
	if in.Comentario == nil {
		h.erro(w, r, erroValidacao("Informe o comentário."))
		return
	}
	l, err := h.svc.Comentar(r.Context(), membro(r), id, produtoID, *in.Comentario)
	h.responder(w, r, http.StatusOK, l, err)
}

func (h *Handler) remover(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "listaId", ErrListaNaoEncontrada)
	if !ok {
		return
	}
	produtoID, ok := h.id(w, r, "produtoId", ErrItemNaoEncontrado)
	if !ok {
		return
	}
	l, err := h.svc.RemoverProduto(r.Context(), membro(r), id, produtoID)
	h.responder(w, r, http.StatusOK, l, err)
}

func (h *Handler) publicar(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "listaId", ErrListaNaoEncontrada)
	if !ok {
		return
	}
	l, err := h.svc.Publicar(r.Context(), membro(r), id)
	h.responder(w, r, http.StatusOK, l, err)
}

func (h *Handler) importar(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "listaId", ErrListaNaoEncontrada)
	if !ok {
		return
	}
	var in struct {
		ProdutoIDs []uuid.UUID `json:"produto_ids"`
		Colecao    *bool       `json:"colecao"`
	}
	if !h.ler(w, r, &in) {
		return
	}
	colecao := in.Colecao == nil || *in.Colecao
	res, err := h.svc.Importar(r.Context(), membro(r), id, in.ProdutoIDs, colecao)
	h.responder(w, r, http.StatusOK, res, err)
}

func (h *Handler) painel(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "listaId", ErrListaNaoEncontrada)
	if !ok {
		return
	}
	p, err := h.svc.Painel(r.Context(), membro(r), id)
	h.responder(w, r, http.StatusOK, p, err)
}

func (h *Handler) anexarVideo(w http.ResponseWriter, r *http.Request) {
	h.video(w, r, h.svc.AnexarVideo)
}

func (h *Handler) tirarVideo(w http.ResponseWriter, r *http.Request) {
	h.video(w, r, h.svc.TirarVideo)
}

func (h *Handler) video(w http.ResponseWriter, r *http.Request, fn func(context.Context, contas.Membro, uuid.UUID, uuid.UUID) (ListaDetalhe, error)) {
	id, ok := h.id(w, r, "listaId", ErrListaNaoEncontrada)
	if !ok {
		return
	}
	videoID, ok := h.id(w, r, "videoId", midia.ErrVideoNaoEncontrado)
	if !ok {
		return
	}
	l, err := fn(r.Context(), membro(r), id, videoID)
	h.responder(w, r, http.StatusOK, l, err)
}

func (h *Handler) id(w http.ResponseWriter, r *http.Request, param string, naoEncontrado error) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, param))
	if err != nil {
		h.erro(w, r, naoEncontrado)
		return uuid.Nil, false
	}
	return id, true
}

const maxCorpoReq = 32 << 10

func (h *Handler) ler(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCorpoReq))
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

// erro devolve os erros de negócio da curadoria, os da coleção (produto
// colado por link, importação) e os da mídia (vídeos anexados).
func (h *Handler) erro(w http.ResponseWriter, r *http.Request, err error) {
	var e *Erro
	if errors.As(err, &e) {
		httpserver.JSONErro(w, e.Status, e.Codigo, e.Mensagem)
		return
	}
	var ec *colecoes.Erro
	if errors.As(err, &ec) {
		httpserver.JSONErro(w, ec.Status, ec.Codigo, ec.Mensagem)
		return
	}
	var em *midia.Erro
	if errors.As(err, &em) {
		httpserver.JSONErro(w, em.Status, em.Codigo, em.Mensagem)
		return
	}
	h.log.ErrorContext(r.Context(), "erro interno na curadoria", "err", err)
	httpserver.JSONErro(w, http.StatusInternalServerError, "erro_interno", "Algo deu errado. Tente de novo em instantes.")
}
