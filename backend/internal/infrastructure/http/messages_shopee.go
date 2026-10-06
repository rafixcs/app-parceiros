package http

var shopeeMessages = map[string]string{
	"invalid_shopee_credential": "A Shopee recusou o AppID ou o Secret. Confira os dados no painel de afiliados e tente de novo.",
	"shopee_access_denied":      "A Shopee negou acesso a esta conta de afiliado. Confira se o acesso à Open API está aprovado.",
	"shopee_rate_limited":       "A Shopee está limitando as chamadas agora. Tente de novo em alguns instantes.",
	"shopee_unavailable":        "Não conseguimos falar com a Shopee agora. Tente de novo em instantes.",
	"invalid_app_id":            "O AppID tem só números. Copie-o do painel de afiliados da Shopee.",
	"invalid_secret":            "Informe o Secret da Open API.",
}
