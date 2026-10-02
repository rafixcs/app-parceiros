package tendencias

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/httpserver"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/postgres"
)

type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Modulo registra o radar sob /v1/workspaces/{workspaceId}.
func (h *Handler) Modulo() contas.Modulo {
	return contas.Modulo{DoWorkspace: func(r chi.Router) {
		r.Get("/radar", h.radar)
		r.Get("/radar/categorias", h.categorias)
		r.Get("/radar/produtos/{produtoId}", h.produto)
	}}
}

func escopo(r *http.Request) postgres.Escopo {
	m, _ := contas.MembroDoContexto(r.Context())
	return postgres.Escopo{UsuarioID: m.UsuarioID.String(), WorkspaceID: m.WorkspaceID.String()}
}

func (h *Handler) radar(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var f Filtro
	var err error
	ler := func(nome string, fn func(string) error) {
		if v := q.Get(nome); v != "" && err == nil {
			if fn(v) != nil {
				err = &ErrFiltro{"Parâmetro " + nome + " inválido."}
			}
		}
	}
	ler("categoria", func(v string) error { return int64Em(v, &f.Categoria) })
	ler("preco_min", func(v string) error { return int64Em(v, &f.PrecoMin) })
	ler("preco_max", func(v string) error { return int64Em(v, &f.PrecoMax) })
	ler("comissao_min", func(v string) error {
		n, e := strconv.ParseInt(v, 10, 32)
		c := int32(n)
		f.ComissaoMin = &c
		return e
	})
	ler("nota_min", func(v string) error {
		n, e := strconv.ParseFloat(v, 64)
		f.NotaMin = &n
		return e
	})
	ler("pagina", func(v string) (e error) { f.Pagina, e = strconv.Atoi(v); return })
	ler("por_pagina", func(v string) (e error) { f.PorPagina, e = strconv.Atoi(v); return })
	f.Busca = q.Get("q")
	f.Ordem = Ordem(q.Get("ordem"))
	if err != nil {
		h.erro(w, r, err)
		return
	}
	p, err := h.svc.Radar(r.Context(), escopo(r), f)
	if err != nil {
		h.erro(w, r, err)
		return
	}
	httpserver.JSON(w, http.StatusOK, p)
}

func (h *Handler) categorias(w http.ResponseWriter, r *http.Request) {
	cs, err := h.svc.Categorias(r.Context(), escopo(r))
	if err != nil {
		h.erro(w, r, err)
		return
	}
	httpserver.JSON(w, http.StatusOK, cs)
}

func (h *Handler) produto(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "produtoId"))
	if err != nil {
		h.erro(w, r, ErrProdutoNaoEncontrado)
		return
	}
	dias := 0
	if v := r.URL.Query().Get("dias"); v != "" {
		if dias, err = strconv.Atoi(v); err != nil {
			h.erro(w, r, &ErrFiltro{"Parâmetro dias inválido."})
			return
		}
	}
	d, err := h.svc.Produto(r.Context(), escopo(r), id, dias, time.Now())
	if err != nil {
		h.erro(w, r, err)
		return
	}
	httpserver.JSON(w, http.StatusOK, d)
}

func int64Em(v string, dst **int64) error {
	n, err := strconv.ParseInt(v, 10, 64)
	*dst = &n
	return err
}

func (h *Handler) erro(w http.ResponseWriter, r *http.Request, err error) {
	var ef *ErrFiltro
	switch {
	case errors.As(err, &ef):
		httpserver.JSONErro(w, http.StatusUnprocessableEntity, "dados_invalidos", ef.Mensagem)
	case errors.Is(err, ErrProdutoNaoEncontrado):
		httpserver.JSONErro(w, http.StatusNotFound, "produto_nao_encontrado", "Produto não encontrado.")
	default:
		h.log.ErrorContext(r.Context(), "erro interno no radar", "err", err)
		httpserver.JSONErro(w, http.StatusInternalServerError, "erro_interno", "Algo deu errado. Tente de novo em instantes.")
	}
}
