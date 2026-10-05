package colecoes

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Modulo registra as rotas sob /v1/workspaces/{workspaceId}.
func (h *Handler) Modulo() contas.Modulo {
	return contas.Modulo{DoWorkspace: func(r chi.Router) {
		r.Get("/itens", h.listar)
		r.Post("/itens", h.salvar)
		r.Get("/itens/produtos", h.produtosSalvos)
		r.Post("/itens/links-pendentes", h.gerarPendentes)
		r.Get("/itens/{itemId}", h.ver)
		r.Patch("/itens/{itemId}", h.atualizar)
		r.Delete("/itens/{itemId}", h.remover)
		r.Post("/itens/{itemId}/link", h.gerarLink)
		r.Put("/itens/{itemId}/colecoes", h.definirColecoes)
		r.Get("/colecoes", h.colecoes)
		r.Post("/colecoes", h.criarColecao)
		r.Patch("/colecoes/{colecaoId}", h.renomearColecao)
		r.Delete("/colecoes/{colecaoId}", h.apagarColecao)
	}}
}

func dono(r *http.Request) Dono {
	m, _ := contas.MembroDoContexto(r.Context())
	return Dono{WorkspaceID: m.WorkspaceID, UsuarioID: m.UsuarioID}
}

func (h *Handler) listar(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := Filtro{Busca: q.Get("q"), Status: Status(q.Get("status")), Tag: q.Get("tag")}
	var err error
	if v := q.Get("colecao"); v != "" {
		id, e := uuid.Parse(v)
		if e != nil {
			err = erroValidacao("Parâmetro colecao inválido.")
		}
		f.ColecaoID = &id
	}
	for nome, dst := range map[string]*int{"pagina": &f.Pagina, "por_pagina": &f.PorPagina} {
		if v := q.Get(nome); v != "" && err == nil {
			if *dst, err = strconv.Atoi(v); err != nil {
				err = erroValidacao("Parâmetro " + nome + " inválido.")
			}
		}
	}
	if err != nil {
		h.erro(w, r, err)
		return
	}
	p, err := h.svc.Listar(r.Context(), dono(r), f)
	h.responder(w, r, http.StatusOK, p, err)
}

type novoItem struct {
	ProdutoID *uuid.UUID `json:"produto_id"`
	URL       string     `json:"url"`
}

func (h *Handler) salvar(w http.ResponseWriter, r *http.Request) {
	var in novoItem
	if !h.ler(w, r, &in) {
		return
	}
	it, criado, err := h.svc.Salvar(r.Context(), dono(r), in.ProdutoID, in.URL)
	status := http.StatusOK
	if criado {
		status = http.StatusCreated
	}
	h.responder(w, r, status, it, err)
}

func (h *Handler) produtosSalvos(w http.ResponseWriter, r *http.Request) {
	ids, err := h.svc.ProdutosSalvos(r.Context(), dono(r))
	h.responder(w, r, http.StatusOK, ids, err)
}

func (h *Handler) gerarPendentes(w http.ResponseWriter, r *http.Request) {
	n, err := h.svc.GerarPendentes(r.Context(), dono(r))
	h.responder(w, r, http.StatusAccepted, map[string]int{"enfileirados": n}, err)
}

func (h *Handler) ver(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "itemId", ErrItemNaoEncontrado)
	if !ok {
		return
	}
	it, err := h.svc.Ver(r.Context(), dono(r), id)
	h.responder(w, r, http.StatusOK, it, err)
}

type edicaoItem struct {
	Titulo       *string         `json:"titulo"`
	Descricao    *string         `json:"descricao"`
	Notas        *string         `json:"notas"`
	Tags         *[]string       `json:"tags"`
	Status       *Status         `json:"status"`
	LinkAfiliado json.RawMessage `json:"link_afiliado"`
}

func (h *Handler) atualizar(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "itemId", ErrItemNaoEncontrado)
	if !ok {
		return
	}
	var in edicaoItem
	if !h.ler(w, r, &in) {
		return
	}
	e := Edicao{Titulo: in.Titulo, Descricao: in.Descricao, Notas: in.Notas, Tags: in.Tags, Status: in.Status}
	// link_afiliado ausente não muda; null volta ao automático; texto é manual.
	if in.LinkAfiliado != nil {
		e.MudarLink = true
		if !bytes.Equal(in.LinkAfiliado, []byte("null")) {
			var link string
			if err := json.Unmarshal(in.LinkAfiliado, &link); err != nil {
				h.erro(w, r, erroValidacao("link_afiliado deve ser texto ou null."))
				return
			}
			e.Link = &link
		}
	}
	it, err := h.svc.Atualizar(r.Context(), dono(r), id, e)
	h.responder(w, r, http.StatusOK, it, err)
}

func (h *Handler) remover(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "itemId", ErrItemNaoEncontrado)
	if !ok {
		return
	}
	h.responder(w, r, http.StatusNoContent, nil, h.svc.Remover(r.Context(), dono(r), id))
}

func (h *Handler) gerarLink(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "itemId", ErrItemNaoEncontrado)
	if !ok {
		return
	}
	it, err := h.svc.GerarLink(r.Context(), dono(r), id)
	h.responder(w, r, http.StatusAccepted, it, err)
}

func (h *Handler) definirColecoes(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "itemId", ErrItemNaoEncontrado)
	if !ok {
		return
	}
	var in struct {
		ColecaoIDs []uuid.UUID `json:"colecao_ids"`
	}
	if !h.ler(w, r, &in) {
		return
	}
	it, err := h.svc.DefinirColecoes(r.Context(), dono(r), id, in.ColecaoIDs)
	h.responder(w, r, http.StatusOK, it, err)
}

func (h *Handler) colecoes(w http.ResponseWriter, r *http.Request) {
	cs, err := h.svc.Colecoes(r.Context(), dono(r))
	h.responder(w, r, http.StatusOK, cs, err)
}

type nomeEntrada struct {
	Nome string `json:"nome"`
}

func (h *Handler) criarColecao(w http.ResponseWriter, r *http.Request) {
	var in nomeEntrada
	if !h.ler(w, r, &in) {
		return
	}
	c, err := h.svc.CriarColecao(r.Context(), dono(r), in.Nome)
	h.responder(w, r, http.StatusCreated, c, err)
}

func (h *Handler) renomearColecao(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "colecaoId", ErrColecaoNaoEncontrada)
	if !ok {
		return
	}
	var in nomeEntrada
	if !h.ler(w, r, &in) {
		return
	}
	c, err := h.svc.RenomearColecao(r.Context(), dono(r), id, in.Nome)
	h.responder(w, r, http.StatusOK, c, err)
}

func (h *Handler) apagarColecao(w http.ResponseWriter, r *http.Request) {
	id, ok := h.id(w, r, "colecaoId", ErrColecaoNaoEncontrada)
	if !ok {
		return
	}
	h.responder(w, r, http.StatusNoContent, nil, h.svc.ApagarColecao(r.Context(), dono(r), id))
}

func (h *Handler) id(w http.ResponseWriter, r *http.Request, param string, naoEncontrado error) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, param))
	if err != nil {
		h.erro(w, r, naoEncontrado)
		return uuid.Nil, false
	}
	return id, true
}

const maxCorpo = 16 << 10

func (h *Handler) ler(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCorpo))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httputil.Error(w, http.StatusBadRequest, "json_invalido", "O corpo da requisição não é um JSON válido.")
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
		httputil.JSON(w, status, v)
	}
}

func (h *Handler) erro(w http.ResponseWriter, r *http.Request, err error) {
	var e *Erro
	if errors.As(err, &e) {
		httputil.Error(w, e.Status, e.Codigo, e.Mensagem)
		return
	}
	h.log.ErrorContext(r.Context(), "erro interno em coleções", "err", err)
	httputil.Error(w, http.StatusInternalServerError, "erro_interno", "Algo deu errado. Tente de novo em instantes.")
}
