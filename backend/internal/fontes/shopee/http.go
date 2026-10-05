package shopee

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/pkg/httputil"
)

// Handler expõe a conexão do usuário com a Shopee em /v1/eu/shopee.
type Handler struct {
	svc *Credenciais
	log *slog.Logger
}

func NewHandler(svc *Credenciais, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

func (h *Handler) Modulo() contas.Modulo {
	return contas.Modulo{Autenticadas: func(r chi.Router) {
		r.Get("/v1/eu/shopee", h.ver)
		r.Put("/v1/eu/shopee", h.conectar)
		r.Delete("/v1/eu/shopee", h.desconectar)
	}}
}

func (h *Handler) ver(w http.ResponseWriter, r *http.Request) {
	u, _ := contas.UsuarioDoContexto(r.Context())
	c, err := h.svc.Ver(r.Context(), u.ID)
	if err != nil {
		h.erro(w, r, err)
		return
	}
	httputil.JSON(w, http.StatusOK, c)
}

type conexaoEntrada struct {
	AppID  string `json:"app_id"`
	Secret string `json:"secret"`
}

func (h *Handler) conectar(w http.ResponseWriter, r *http.Request) {
	var in conexaoEntrada
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "json_invalido", "O corpo da requisição não é um JSON válido.")
		return
	}
	u, _ := contas.UsuarioDoContexto(r.Context())
	c, err := h.svc.Conectar(r.Context(), u.ID, in.AppID, in.Secret)
	if err != nil {
		h.erro(w, r, err)
		return
	}
	httputil.JSON(w, http.StatusOK, c)
}

func (h *Handler) desconectar(w http.ResponseWriter, r *http.Request) {
	u, _ := contas.UsuarioDoContexto(r.Context())
	if err := h.svc.Desconectar(r.Context(), u.ID); err != nil {
		h.erro(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// erro responde com o erro de negócio ou um 500 genérico. O log leva só o
// erro, que nunca contém o Secret (veja Credencial.String e Cliente.chamar).
func (h *Handler) erro(w http.ResponseWriter, r *http.Request, err error) {
	var e *Erro
	if errors.As(err, &e) {
		if e == ErrShopeeIndisponivel {
			h.log.WarnContext(r.Context(), "shopee indisponível ao validar credencial", "err", err)
		}
		httputil.Error(w, e.Status, e.Codigo, e.Mensagem)
		return
	}
	h.log.ErrorContext(r.Context(), "erro interno na conexão shopee", "err", err)
	httputil.Error(w, http.StatusInternalServerError, "erro_interno", "Algo deu errado. Tente de novo em instantes.")
}
