package notificacoes

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

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

// Modulo registra as preferências e o push em /v1/eu e a caixa de entrada
// sob /v1/workspaces/{workspaceId}.
func (h *Handler) Modulo() contas.Modulo {
	return contas.Modulo{
		Autenticadas: func(r chi.Router) {
			r.Get("/v1/eu/notificacoes", h.preferencias)
			r.Put("/v1/eu/notificacoes", h.definirPreferencias)
			r.Post("/v1/eu/push", h.inscrever)
			r.Delete("/v1/eu/push", h.desinscrever)
		},
		DoWorkspace: func(r chi.Router) {
			r.Get("/notificacoes", h.caixa)
			r.Post("/notificacoes/lidas", h.marcarTodas)
			r.Post("/notificacoes/{notificacaoId}/lida", h.marcarLida)
		},
	}
}

func usuario(r *http.Request) uuid.UUID {
	u, _ := contas.UsuarioDoContexto(r.Context())
	return u.ID
}

func dono(r *http.Request) Dono {
	m, _ := contas.MembroDoContexto(r.Context())
	return Dono{WorkspaceID: m.WorkspaceID, UsuarioID: m.UsuarioID}
}

func (h *Handler) preferencias(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.Preferencias(r.Context(), usuario(r))
	h.responder(w, r, http.StatusOK, p, err)
}

func (h *Handler) definirPreferencias(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email *bool `json:"email"`
	}
	if !h.ler(w, r, &in) {
		return
	}
	if in.Email == nil {
		httputil.Error(w, http.StatusUnprocessableEntity, "dados_invalidos", "Informe se quer receber e-mails.")
		return
	}
	p, err := h.svc.DefinirEmail(r.Context(), usuario(r), *in.Email)
	h.responder(w, r, http.StatusOK, p, err)
}

func (h *Handler) inscrever(w http.ResponseWriter, r *http.Request) {
	var in Inscricao
	if !h.ler(w, r, &in) {
		return
	}
	h.responder(w, r, http.StatusNoContent, nil, h.svc.Inscrever(r.Context(), usuario(r), in))
}

func (h *Handler) desinscrever(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Endpoint string `json:"endpoint"`
	}
	if !h.ler(w, r, &in) {
		return
	}
	h.responder(w, r, http.StatusNoContent, nil, h.svc.Desinscrever(r.Context(), usuario(r), in.Endpoint))
}

func (h *Handler) caixa(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.Caixa(r.Context(), dono(r))
	h.responder(w, r, http.StatusOK, c, err)
}

func (h *Handler) marcarTodas(w http.ResponseWriter, r *http.Request) {
	h.responder(w, r, http.StatusNoContent, nil, h.svc.MarcarTodasLidas(r.Context(), dono(r)))
}

func (h *Handler) marcarLida(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "notificacaoId"))
	if err != nil {
		h.erro(w, r, ErrNotificacaoNaoEncontrada)
		return
	}
	h.responder(w, r, http.StatusNoContent, nil, h.svc.MarcarLida(r.Context(), dono(r), id))
}

const maxCorpoReq = 8 << 10

func (h *Handler) ler(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCorpoReq))
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
	h.log.ErrorContext(r.Context(), "erro interno em notificações", "err", err)
	httputil.Error(w, http.StatusInternalServerError, "erro_interno", "Algo deu errado. Tente de novo em instantes.")
}
