package assinaturas

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/httpserver"
)

type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Modulo registra a assinatura sob /v1/workspaces/{workspaceId}, fora do
// bloqueio por falta de pagamento (é por ela que o workspace volta), e o
// webhook do gateway sem autenticação, que ele confere pelo segredo do
// provedor.
func (h *Handler) Modulo() contas.Modulo {
	return contas.Modulo{
		Publicas: func(r chi.Router) {
			r.Post("/v1/webhooks/cobranca", h.webhook)
		},
		Livres: func(r chi.Router) {
			r.Get("/assinatura", h.ver)
			r.Post("/assinatura", h.assinar)
			r.Patch("/assinatura", h.mudarAssentos)
			r.Delete("/assinatura", h.cancelar)
			r.Post("/assinatura/simular-pagamento", h.simular)
		},
	}
}

func membro(r *http.Request) contas.Membro {
	m, _ := contas.MembroDoContexto(r.Context())
	return m
}

func (h *Handler) ver(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Ver(r.Context(), membro(r))
	h.responder(w, r, http.StatusOK, out, err)
}

type novaAssinatura struct {
	Assentos int64  `json:"assentos"`
	CPFCNPJ  string `json:"cpf_cnpj"`
}

func (h *Handler) assinar(w http.ResponseWriter, r *http.Request) {
	var in novaAssinatura
	if !h.ler(w, r, &in) {
		return
	}
	out, err := h.svc.Assinar(r.Context(), membro(r), Novo(in))
	h.responder(w, r, http.StatusCreated, out, err)
}

type mudancaAssentos struct {
	Assentos int64 `json:"assentos"`
}

func (h *Handler) mudarAssentos(w http.ResponseWriter, r *http.Request) {
	var in mudancaAssentos
	if !h.ler(w, r, &in) {
		return
	}
	out, err := h.svc.MudarAssentos(r.Context(), membro(r), in.Assentos)
	h.responder(w, r, http.StatusOK, out, err)
}

func (h *Handler) cancelar(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Cancelar(r.Context(), membro(r))
	h.responder(w, r, http.StatusOK, out, err)
}

func (h *Handler) simular(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.SimularPagamento(r.Context(), membro(r))
	h.responder(w, r, http.StatusOK, out, err)
}

// webhook recebe o aviso do gateway. A resposta é sempre curta e sem detalhes:
// quem chama aqui não está autenticado.
func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	err := h.svc.Webhook(r.Context(), r)
	switch {
	case err == nil, errors.Is(err, ErrEventoIgnorado):
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, ErrWebhookInvalido):
		httpserver.JSONErro(w, http.StatusUnauthorized, "nao_autenticado", "Aviso de cobrança não reconhecido.")
	case errors.Is(err, ErrAssinaturaDesconhecida):
		// Nada a fazer, e o gateway não deve ficar reenviando.
		h.log.WarnContext(r.Context(), "aviso de cobrança de assinatura desconhecida")
		w.WriteHeader(http.StatusNoContent)
	default:
		h.log.ErrorContext(r.Context(), "falha ao processar aviso de cobrança", "err", err)
		httpserver.JSONErro(w, http.StatusInternalServerError, "erro_interno", "Não foi possível processar o aviso agora.")
	}
}

const maxCorpo = 4 << 10

func (h *Handler) ler(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCorpo))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httpserver.JSONErro(w, http.StatusBadRequest, "json_invalido", "O corpo da requisição não é um JSON válido.")
		return false
	}
	return true
}

func (h *Handler) responder(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		h.erro(w, r, err)
		return
	}
	httpserver.JSON(w, status, v)
}

// erro devolve os erros de negócio da assinatura e os de contas (assentos).
func (h *Handler) erro(w http.ResponseWriter, r *http.Request, err error) {
	var e *Erro
	if errors.As(err, &e) {
		httpserver.JSONErro(w, e.Status, e.Codigo, e.Mensagem)
		return
	}
	var ec *contas.Erro
	if errors.As(err, &ec) {
		httpserver.JSONErro(w, ec.Status, ec.Codigo, ec.Mensagem)
		return
	}
	h.log.ErrorContext(r.Context(), "erro interno na assinatura", "err", err)
	httpserver.JSONErro(w, http.StatusInternalServerError, "erro_interno", "Algo deu errado. Tente de novo em instantes.")
}
