package http

// Generic error codes. They keep the values of the current API contract,
// which moves to English in a later phase.
const (
	CodeUnauthenticated = "nao_autenticado"
	CodeInternal        = "erro_interno"
	CodeInvalidJSON     = "json_invalido"
)

// messages maps the stable error codes to the text shown to customers, in
// pt-BR. Every domain error code must have an entry (see messages_test.go).
var messages = map[string]string{
	CodeUnauthenticated: "Faça login para continuar.",
	CodeInternal:        "Algo deu errado. Tente de novo em instantes.",
	CodeInvalidJSON:     "Não entendemos os dados enviados.",

	// Internal identity provider.
	"invalid_credentials": "E-mail ou senha incorretos.",
	"email_taken":         "Já existe uma conta com este e-mail. Entre ou recupere a senha.",
	"invalid_email":       "Informe um e-mail válido.",
	"invalid_name":        "Informe um nome de até 80 caracteres.",
	"weak_password":       "A senha precisa ter de 8 a 128 caracteres.",
	"invalid_auth_token":  "Este link expirou ou já foi usado. Peça um novo.",
	"too_many_attempts":   "Muitas tentativas. Espere alguns minutos e tente de novo.",
}

// Message returns the pt-BR text of an error code.
func Message(code string) string {
	if m, ok := messages[code]; ok {
		return m
	}
	return messages[CodeInternal]
}
