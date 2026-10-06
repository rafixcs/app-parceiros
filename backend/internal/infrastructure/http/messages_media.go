package http

var mediaMessages = map[string]string{
	"video_not_found":           "Vídeo não encontrado.",
	"invalid_video_link":        "Cole o link de um vídeo do YouTube ou do TikTok.",
	"video_unavailable":         "Esse vídeo não existe, é privado ou não permite ser incorporado.",
	"platform_unavailable":      "Não conseguimos falar com o YouTube ou o TikTok agora. Tente de novo em instantes.",
	"usage_rights_required":     "Confirme que você tem direito de uso do vídeo.",
	"invalid_video_format":      "Envie um vídeo MP4, MOV ou WebM.",
	"invalid_video_size":        "Informe o tamanho do arquivo.",
	"video_too_large":           "O vídeo pode ter até 1 GB.",
	"video_quota_exceeded":      "O espaço para vídeos do plano acabou. Apague vídeos para enviar outros.",
	"uploads_unavailable":       "O envio de vídeos não está disponível agora.",
	"upload_closed":             "Esse envio já terminou ou foi cancelado.",
	"upload_incomplete":         "O envio não chegou inteiro. Envie o vídeo de novo.",
	"upload_parts_required":     "Informe as partes enviadas.",
	"invalid_part_number":       "A parte deve ser de 1 a 10000.",
	"video_not_ready":           "O vídeo ainda está sendo processado.",
	"video_owner_only":          "Só quem enviou o vídeo pode alterá-lo.",
	"video_share_managers_only": "Só o dono e os mentores de uma mentoria compartilham vídeos com a turma.",
	"video_title_too_long":      "O título pode ter até 200 caracteres.",
	"embed_not_downloadable":    "Vídeos de referência não podem ser baixados; abra-os na plataforma.",
}
