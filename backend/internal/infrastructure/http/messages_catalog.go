package http

var catalogMessages = map[string]string{
	"product_not_found":      "Produto não encontrado.",
	"invalid_page":           "A página deve ficar entre 1 e 1000.",
	"invalid_per_page":       "Mostre de 1 a 50 produtos por página.",
	"negative_price":         "O preço não pode ser negativo.",
	"invalid_min_commission": "A comissão mínima deve ficar entre 0% e 100%.",
	"invalid_min_rating":     "A nota mínima deve ficar entre 0 e 5.",
	"invalid_sort":           "Ordenação desconhecida.",
	"query_too_long":         "A busca pode ter até 100 caracteres.",
	"invalid_history_days":   "O histórico pode cobrir de 1 a 90 dias.",
	"invalid_product_link":   "Cole o link de um produto da Shopee, como https://shopee.com.br/Nome-do-produto-i.123.456.",
	"short_link":             "Esse é um link curto. Abra-o no navegador e cole aqui o endereço completo da página do produto.",
}
