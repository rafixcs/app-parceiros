package http

var notificationMessages = map[string]string{
	"notification_not_found":           "Notificação não encontrada.",
	"push_unavailable":                 "As notificações no navegador não estão disponíveis neste servidor.",
	"invalid_push_subscription":        "Este navegador enviou uma inscrição de notificação inválida.",
	"too_many_push_subscriptions":      "Você já ativou as notificações em navegadores demais. Desative em algum deles.",
	"invalid_notification_preferences": "Informe se quer receber e-mails.",
}
