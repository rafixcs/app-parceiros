package http

var curationMessages = map[string]string{
	"list_not_found":           "Lista não encontrada.",
	"list_item_not_found":      "Esse produto não está na lista.",
	"video_not_in_list":        "Esse vídeo não está na lista.",
	"list_managers_only":       "Só o dono e os mentores podem montar listas.",
	"lists_mentorship_only":    "Listas de curadoria são de workspaces de mentoria.",
	"list_limit":               "O plano chegou ao limite de listas. Apague listas antigas para criar outras.",
	"list_empty":               "Adicione ao menos um produto antes de publicar.",
	"already_in_list":          "Esse produto já está na lista.",
	"list_full":                "Uma lista pode ter até 100 produtos.",
	"list_not_published":       "Publique a lista antes de importar.",
	"invalid_list_title":       "O título deve ter de 1 a 120 caracteres.",
	"invalid_list_description": "A descrição pode ter até 2000 caracteres.",
	"invalid_list_comment":     "O comentário pode ter até 1000 caracteres.",
	"list_comment_required":    "Informe o comentário.",
	"invalid_list_order":       "Envie todos os produtos da lista, cada um uma vez, na nova ordem.",
	"product_not_in_list":      "Algum dos produtos escolhidos não está na lista.",
}
