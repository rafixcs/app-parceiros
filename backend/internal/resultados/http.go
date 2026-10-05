package resultados

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/curadoria"
	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Modulo registra o painel sob /v1/workspaces/{workspaceId} e a
// sincronização, que é do usuário, sob /v1/eu.
func (h *Handler) Modulo() contas.Modulo {
	return contas.Modulo{
		Autenticadas: func(r chi.Router) {
			r.Get("/v1/eu/resultados/sincronizacao", h.sincronizacao)
			r.Post("/v1/eu/resultados/sincronizar", h.sincronizar)
		},
		DoWorkspace: func(r chi.Router) {
			r.Get("/resultados", h.meus)
			r.Get("/resultados/turma", h.turma)
		},
		// Retirar (ou dar) o consentimento vale mesmo com o workspace suspenso.
		Livres: func(r chi.Router) {
			r.Put("/resultados/consentimento", h.consentimento)
		},
	}
}

func (h *Handler) periodo(r *http.Request) (Periodo, error) {
	q := r.URL.Query()
	return NovoPeriodo(q.Get("de"), q.Get("ate"), h.svc.agora())
}

func (h *Handler) meus(w http.ResponseWriter, r *http.Request) {
	p, err := h.periodo(r)
	if err != nil {
		h.erro(w, r, err)
		return
	}
	m, _ := contas.MembroDoContexto(r.Context())
	out, err := h.svc.Meus(r.Context(), m, p)
	h.responder(w, r, http.StatusOK, out, err)
}

func (h *Handler) turma(w http.ResponseWriter, r *http.Request) {
	p, err := h.periodo(r)
	if err != nil {
		h.erro(w, r, err)
		return
	}
	m, _ := contas.MembroDoContexto(r.Context())
	out, err := h.svc.Turma(r.Context(), m, p)
	h.responder(w, r, http.StatusOK, out, err)
}

type dadosConsentimento struct {
	Consente *bool `json:"consente"`
}

func (h *Handler) consentimento(w http.ResponseWriter, r *http.Request) {
	var in dadosConsentimento
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || in.Consente == nil {
		httputil.Error(w, http.StatusBadRequest, "json_invalido", "Envie {\"consente\": true} ou {\"consente\": false}.")
		return
	}
	m, _ := contas.MembroDoContexto(r.Context())
	err := h.svc.DefinirConsentimento(r.Context(), m, *in.Consente)
	h.responder(w, r, http.StatusOK, dadosConsentimento{Consente: in.Consente}, err)
}

func (h *Handler) sincronizacao(w http.ResponseWriter, r *http.Request) {
	u, _ := contas.UsuarioDoContexto(r.Context())
	out, err := h.svc.Sincronizacao(r.Context(), u.ID)
	h.responder(w, r, http.StatusOK, out, err)
}

func (h *Handler) sincronizar(w http.ResponseWriter, r *http.Request) {
	u, _ := contas.UsuarioDoContexto(r.Context())
	out, err := h.svc.PedirSincronizacao(r.Context(), u.ID)
	h.responder(w, r, http.StatusAccepted, out, err)
}

func (h *Handler) responder(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		h.erro(w, r, err)
		return
	}
	httputil.JSON(w, status, v)
}

// erro devolve os erros de negócio dos resultados, de contas (consentimento)
// e da curadoria (listas do painel da turma).
func (h *Handler) erro(w http.ResponseWriter, r *http.Request, err error) {
	var e *Erro
	if errors.As(err, &e) {
		httputil.Error(w, e.Status, e.Codigo, e.Mensagem)
		return
	}
	var ec *contas.Erro
	if errors.As(err, &ec) {
		httputil.Error(w, ec.Status, ec.Codigo, ec.Mensagem)
		return
	}
	var ecur *curadoria.Erro
	if errors.As(err, &ecur) {
		httputil.Error(w, ecur.Status, ecur.Codigo, ecur.Mensagem)
		return
	}
	h.log.ErrorContext(r.Context(), "erro interno nos resultados", "err", err)
	httputil.Error(w, http.StatusInternalServerError, "erro_interno", "Algo deu errado. Tente de novo em instantes.")
}
