package http

var collectionMessages = map[string]string{
	"item_not_found":            "Produto salvo não encontrado.",
	"collection_not_found":      "Coleção não encontrada.",
	"collection_exists":         "Você já tem uma coleção com esse nome.",
	"no_credential":             "Conecte a sua conta de afiliado da Shopee para gerar os links.",
	"import_unavailable":        "Por enquanto só dá para salvar produtos do radar.",
	"link_rate_limited":         "A Shopee está limitando as chamadas agora. Tente de novo em alguns instantes.",
	"product_lookup_failed":     "Não conseguimos falar com a Shopee agora. Tente de novo em instantes.",
	"product_or_link_required":  "Informe o produto do radar ou cole o link da Shopee.",
	"invalid_item_title":        "O título pode ter até 200 caracteres.",
	"invalid_item_description":  "A descrição pode ter até 2000 caracteres.",
	"invalid_item_notes":        "As notas podem ter até 5000 caracteres.",
	"invalid_tag":               "Cada tag pode ter até 30 caracteres.",
	"too_many_tags":             "Use no máximo 20 tags.",
	"invalid_item_status":       "Status inválido.",
	"invalid_affiliate_link":    "O link de afiliado deve ser uma URL https, como https://s.shopee.com.br/abc123.",
	"invalid_collection_name":   "O nome da coleção deve ter de 1 a 60 caracteres.",
	"too_many_item_collections": "Coleções demais para um item.",
	"invalid_item_page":         "Paginação inválida: a página começa em 1, com 1 a 100 itens por página.",
}
