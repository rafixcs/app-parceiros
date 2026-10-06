package http

// Generic error codes, not tied to a domain error.
const (
	CodeUnauthenticated = "unauthenticated"
	CodeInternal        = "internal_error"
	CodeInvalidJSON     = "invalid_json"
	CodeInvalidRequest  = "invalid_request"
)

// messages maps the stable error codes to the text shown to customers, in
// pt-BR. Every domain error code must have an entry (see messages_test.go).
// Each module keeps its own map, in messages_<module>.go.
var messages = mergeMessages(
	genericMessages,
	accountMessages,
	catalogMessages,
	notificationMessages,
	billingMessages,
	shopeeMessages,
	collectionMessages,
	mediaMessages,
	resultMessages,
	curationMessages,
)

func mergeMessages(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for code, text := range m {
			if _, dup := out[code]; dup {
				panic("duplicate message code " + code)
			}
			out[code] = text
		}
	}
	return out
}

var genericMessages = map[string]string{
	CodeUnauthenticated: "Faça login para continuar.",
	CodeInternal:        "Algo deu errado. Tente de novo em instantes.",
	CodeInvalidJSON:     "Não entendemos os dados enviados.",
	CodeInvalidRequest:  "Confira os dados enviados.",

	// Internal identity provider.
	"invalid_credentials": "E-mail ou senha incorretos.",
	"email_taken":         "Já existe uma conta com este e-mail. Entre ou recupere a senha.",
	"invalid_email":       "Informe um e-mail válido.",
	"invalid_name":        "Informe um nome de até 80 caracteres.",
	"weak_password":       "A senha precisa ter de 8 a 128 caracteres.",
	"invalid_auth_token":  "Este link expirou ou já foi usado. Peça um novo.",
	"too_many_attempts":   "Muitas tentativas. Espere alguns minutos e tente de novo.",
}

var accountMessages = map[string]string{
	"workspace_not_found":       "Workspace não encontrado.",
	"member_not_found":          "Membro não encontrado.",
	"invite_not_found":          "Convite não encontrado.",
	"forbidden":                 "Você não tem permissão para esta ação.",
	"mentorship_only":           "Só é possível convidar afiliados para um workspace de mentoria.",
	"consent_mentorship_only":   "O consentimento vale só em workspaces de mentoria.",
	"owner_cannot_leave":        "O dono não pode sair nem ser removido do workspace.",
	"no_seats":                  "Todos os assentos do plano estão ocupados.",
	"seats_in_use":              "A turma e os convites pendentes ocupam mais assentos do que isso. Remova afiliados ou cancele convites antes.",
	"seats_above_plan":          "Essa quantidade de assentos passa do máximo do plano.",
	"workspace_suspended":       "Este workspace está suspenso por falta de pagamento. Fale com o dono do workspace.",
	"workspace_suspended_owner": "Este workspace está suspenso por falta de pagamento. Regularize a assinatura para voltar a usá-lo.",
	"already_member":            "Você já participa deste workspace.",
	"invite_expired":            "Este convite expirou. Peça um novo ao seu mentor.",
	"invite_used":               "Este convite já foi usado. Peça um novo ao seu mentor.",
	"invite_revoked":            "Este convite foi cancelado. Peça um novo ao seu mentor.",
	"invite_other_email":        "Este convite foi enviado para outro e-mail. Entre com a conta que recebeu o convite.",
	"invalid_workspace_name":    "O nome deve ter entre 1 e 80 caracteres.",
	"invalid_photo_url":         "A foto deve ser uma URL https válida.",
	"invalid_invite_email":      "E-mail inválido.",
	"invalid_invite_validity":   "A validade do convite deve ficar entre 1 hora e 30 dias.",
}

// Message returns the pt-BR text of an error code.
func Message(code string) string {
	if m, ok := messages[code]; ok {
		return m
	}
	return messages[CodeInternal]
}
