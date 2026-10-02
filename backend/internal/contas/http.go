package contas

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/platform/auth"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/httpserver"
)

type (
	usuarioKey struct{}
	membroKey  struct{}
)

// UsuarioDoContexto devolve o usuário autenticado (rotas sob Autenticado).
func UsuarioDoContexto(ctx context.Context) (Usuario, bool) {
	u, ok := ctx.Value(usuarioKey{}).(Usuario)
	return u, ok
}

// MembroDoContexto devolve a participação no workspace da rota (rotas sob
// ExigirMembro).
func MembroDoContexto(ctx context.Context) (Membro, bool) {
	m, ok := ctx.Value(membroKey{}).(Membro)
	return m, ok
}

// Handler expõe as rotas HTTP do módulo.
type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Autenticado resolve o usuário da identidade (criando-o no primeiro acesso)
// e o guarda no contexto. Deve rodar depois de auth.Middleware.
func (h *Handler) Autenticado(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := auth.DoContexto(r.Context())
		if !ok {
			httpserver.JSONErro(w, http.StatusUnauthorized, "nao_autenticado", "Faça login para continuar.")
			return
		}
		u, err := h.svc.Entrar(r.Context(), id)
		if err != nil {
			h.erro(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), usuarioKey{}, u)))
	})
}

// ExigirMembro confere que o usuário participa do workspace {workspaceId} da
// rota e guarda a participação no contexto. Quem não participa recebe 404.
func (h *Handler) ExigirMembro(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := UsuarioDoContexto(r.Context())
		wsID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
		if err != nil {
			h.erro(w, r, ErrWorkspaceNaoEncontrado)
			return
		}
		m, err := h.svc.Membro(r.Context(), u.ID, wsID)
		if err != nil {
			h.erro(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), membroKey{}, m)))
	})
}

// Rotas registra as rotas de contas em r. As rotas autenticadas usam v.
func (h *Handler) Rotas(r chi.Router, v auth.Verificador) {
	r.Get("/v1/convites/{token}", h.verConvite)

	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(v), h.Autenticado)

		r.Get("/v1/eu", h.eu)
		r.Get("/v1/workspaces", h.listarWorkspaces)
		r.Post("/v1/workspaces", h.criarMentoria)
		r.Post("/v1/convites/{token}/aceitar", h.aceitarConvite)

		r.Route("/v1/workspaces/{workspaceId}", func(r chi.Router) {
			r.Use(h.ExigirMembro)
			r.Get("/", h.verWorkspace)
			r.Patch("/", h.atualizarWorkspace)
			r.Get("/membros", h.listarMembros)
			r.Delete("/membros/{usuarioId}", h.removerMembro)
			r.Get("/convites", h.listarConvites)
			r.Post("/convites", h.criarConvite)
			r.Delete("/convites/{conviteId}", h.revogarConvite)
		})
	})
}

func (h *Handler) eu(w http.ResponseWriter, r *http.Request) {
	u, _ := UsuarioDoContexto(r.Context())
	httpserver.JSON(w, http.StatusOK, u)
}

func (h *Handler) listarWorkspaces(w http.ResponseWriter, r *http.Request) {
	u, _ := UsuarioDoContexto(r.Context())
	ws, err := h.svc.Workspaces(r.Context(), u.ID)
	if err != nil {
		h.erro(w, r, err)
		return
	}
	httpserver.JSON(w, http.StatusOK, ws)
}

type novaMentoria struct {
	Nome    string  `json:"nome"`
	FotoURL *string `json:"foto_url"`
}

func (h *Handler) criarMentoria(w http.ResponseWriter, r *http.Request) {
	var in novaMentoria
	if !h.ler(w, r, &in) {
		return
	}
	u, _ := UsuarioDoContexto(r.Context())
	ws, err := h.svc.CriarMentoria(r.Context(), u.ID, in.Nome, in.FotoURL)
	if err != nil {
		h.erro(w, r, err)
		return
	}
	httpserver.JSON(w, http.StatusCreated, ws)
}

func (h *Handler) verWorkspace(w http.ResponseWriter, r *http.Request) {
	m, _ := MembroDoContexto(r.Context())
	ws, err := h.svc.Workspace(r.Context(), m)
	if err != nil {
		h.erro(w, r, err)
		return
	}
	httpserver.JSON(w, http.StatusOK, ws)
}

type atualizacaoWorkspace struct {
	Nome    *string `json:"nome"`
	FotoURL *string `json:"foto_url"`
}

func (h *Handler) atualizarWorkspace(w http.ResponseWriter, r *http.Request) {
	var in atualizacaoWorkspace
	if !h.ler(w, r, &in) {
		return
	}
	m, _ := MembroDoContexto(r.Context())
	ws, err := h.svc.AtualizarWorkspace(r.Context(), m, in.Nome, in.FotoURL)
	if err != nil {
		h.erro(w, r, err)
		return
	}
	httpserver.JSON(w, http.StatusOK, ws)
}

func (h *Handler) listarMembros(w http.ResponseWriter, r *http.Request) {
	m, _ := MembroDoContexto(r.Context())
	ms, err := h.svc.Membros(r.Context(), m)
	if err != nil {
		h.erro(w, r, err)
		return
	}
	httpserver.JSON(w, http.StatusOK, ms)
}

func (h *Handler) removerMembro(w http.ResponseWriter, r *http.Request) {
	alvo, err := uuid.Parse(chi.URLParam(r, "usuarioId"))
	if err != nil {
		h.erro(w, r, ErrMembroNaoEncontrado)
		return
	}
	m, _ := MembroDoContexto(r.Context())
	if err := h.svc.RemoverMembro(r.Context(), m, alvo); err != nil {
		h.erro(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listarConvites(w http.ResponseWriter, r *http.Request) {
	m, _ := MembroDoContexto(r.Context())
	cs, err := h.svc.Convites(r.Context(), m)
	if err != nil {
		h.erro(w, r, err)
		return
	}
	httpserver.JSON(w, http.StatusOK, cs)
}

type novoConvite struct {
	Email         *string `json:"email"`
	ValidadeHoras int     `json:"validade_horas"`
}

func (h *Handler) criarConvite(w http.ResponseWriter, r *http.Request) {
	var in novoConvite
	if !h.ler(w, r, &in) {
		return
	}
	m, _ := MembroDoContexto(r.Context())
	c, err := h.svc.CriarConvite(r.Context(), m, in.Email, time.Duration(in.ValidadeHoras)*time.Hour)
	if err != nil {
		h.erro(w, r, err)
		return
	}
	httpserver.JSON(w, http.StatusCreated, c)
}

func (h *Handler) revogarConvite(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "conviteId"))
	if err != nil {
		h.erro(w, r, ErrConviteNaoEncontrado)
		return
	}
	m, _ := MembroDoContexto(r.Context())
	if err := h.svc.RevogarConvite(r.Context(), m, id); err != nil {
		h.erro(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) verConvite(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.VerConvite(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		h.erro(w, r, err)
		return
	}
	httpserver.JSON(w, http.StatusOK, c)
}

func (h *Handler) aceitarConvite(w http.ResponseWriter, r *http.Request) {
	u, _ := UsuarioDoContexto(r.Context())
	ws, err := h.svc.AceitarConvite(r.Context(), u.ID, chi.URLParam(r, "token"))
	if err != nil {
		h.erro(w, r, err)
		return
	}
	httpserver.JSON(w, http.StatusOK, ws)
}

const maxCorpo = 64 << 10

func (h *Handler) ler(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCorpo))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httpserver.JSONErro(w, http.StatusBadRequest, "json_invalido", "O corpo da requisição não é um JSON válido.")
		return false
	}
	return true
}

func (h *Handler) erro(w http.ResponseWriter, r *http.Request, err error) {
	var e *Erro
	if errors.As(err, &e) {
		httpserver.JSONErro(w, e.Status, e.Codigo, e.Mensagem)
		return
	}
	h.log.ErrorContext(r.Context(), "erro interno em contas", "err", err)
	httpserver.JSONErro(w, http.StatusInternalServerError, "erro_interno", "Algo deu errado. Tente de novo em instantes.")
}
