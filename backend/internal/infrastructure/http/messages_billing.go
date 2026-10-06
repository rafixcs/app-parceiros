package http

var billingMessages = map[string]string{
	"billing_owner_only":      "Só o dono do workspace cuida da assinatura.",
	"billing_managers_only":   "Só o dono e os mentores veem a assinatura.",
	"already_subscribed":      "Este workspace já tem uma assinatura. Cancele a atual antes de contratar outra.",
	"no_subscription":         "Este workspace ainda não tem assinatura.",
	"invalid_tax_id":          "Informe um CPF ou CNPJ válido.",
	"minimum_seats":           "A mentoria precisa de pelo menos um assento.",
	"no_price":                "O preço deste plano não está configurado. Fale com o suporte.",
	"simulation_unavailable":  "Simular pagamento só existe no ambiente local.",
	"billing_unavailable":     "Não conseguimos falar com o sistema de cobrança agora. Tente de novo em instantes.",
	"invalid_billing_webhook": "Aviso de cobrança não reconhecido.",
}
