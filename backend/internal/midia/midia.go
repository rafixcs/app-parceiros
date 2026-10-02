// Package midia cuida da biblioteca de vídeos: referências de outros
// criadores por embed (só metadados do oEmbed oficial, o vídeo toca no player
// da plataforma) e vídeos próprios enviados direto ao bucket por URL
// pré-assinada, com miniatura e prévia geradas pelo ffmpeg no worker.
//
// O vídeo é do dono, dentro do workspace. Dono e mentor podem compartilhar os
// seus com a turma. Os vínculos ligam vídeos a produtos do catálogo e a listas
// da curadoria; a curadoria cria os vínculos de lista por este pacote.
package midia

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/midia/midiadb"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/postgres"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/storage"
	"github.com/rafixcs/app-parceiros/backend/internal/produtos"
)

type Tipo string

const (
	TipoEmbed  Tipo = "embed"
	TipoUpload Tipo = "upload"
)

type Status string

const (
	StatusEnviando     Status = "enviando"
	StatusProcessando  Status = "processando"
	StatusPronto       Status = "pronto"
	StatusFalhou       Status = "falhou"
	StatusIndisponivel Status = "indisponivel"
)

// Alvo é aquilo a que um vídeo se liga.
type Alvo string

const (
	AlvoProduto Alvo = "produto"
	AlvoLista   Alvo = "lista"
)

type Video struct {
	ID         uuid.UUID `json:"id"`
	Tipo       Tipo      `json:"tipo"`
	Plataforma string    `json:"plataforma"`
	Status     Status    `json:"status"`
	Titulo     string    `json:"titulo"`
	Autor      string    `json:"autor"`
	// URL é a página do vídeo na plataforma (embed).
	URL *string `json:"url"`
	// PlayerURL é o player oficial da plataforma, para o iframe (embed).
	PlayerURL *string `json:"player_url"`
	// ThumbURL é a miniatura: da plataforma (embed) ou gerada pelo worker
	// (upload, URL assinada).
	ThumbURL *string `json:"thumb_url"`
	// PreviewURL é a prévia em 720p do upload pronto (URL assinada).
	PreviewURL    *string     `json:"preview_url"`
	DuracaoS      *int32      `json:"duracao_s"`
	Largura       *int32      `json:"largura"`
	Altura        *int32      `json:"altura"`
	TamanhoBytes  int64       `json:"tamanho_bytes"`
	NomeArquivo   *string     `json:"nome_arquivo"`
	Compartilhado bool        `json:"compartilhado"`
	Meu           bool        `json:"meu"`
	ProdutoIDs    []uuid.UUID `json:"produto_ids"`
	ListaIDs      []uuid.UUID `json:"lista_ids"`
	CriadoEm      time.Time   `json:"criado_em"`
}

// UploadIniciado é a resposta do início de um upload: o vídeo (enviando) e o
// upload multipart no bucket, que o navegador preenche parte a parte.
type UploadIniciado struct {
	Video    Video  `json:"video"`
	UploadID string `json:"upload_id"`
	Chave    string `json:"chave"`
}

type NovoUpload struct {
	Nome        string
	ContentType string
	Tamanho     int64
	DireitoUso  bool
	ProdutoID   *uuid.UUID
}

type Cota struct {
	UsadosBytes int64 `json:"usados_bytes"`
	LimiteBytes int64 `json:"limite_bytes"`
}

// Erro é um erro de negócio com o status HTTP e o código que a API devolve.
type Erro struct {
	Status   int
	Codigo   string
	Mensagem string
}

func (e *Erro) Error() string { return e.Codigo }

var (
	ErrVideoNaoEncontrado   = &Erro{http.StatusNotFound, "video_nao_encontrado", "Vídeo não encontrado."}
	ErrProdutoNaoEncontrado = &Erro{http.StatusNotFound, "produto_nao_encontrado", "Produto não encontrado."}
	ErrLinkVideo            = &Erro{http.StatusUnprocessableEntity, "link_video_invalido", "Cole o link de um vídeo do YouTube ou do TikTok."}
	ErrVideoIndisponivel    = &Erro{http.StatusUnprocessableEntity, "video_indisponivel", "Esse vídeo não existe, é privado ou não permite ser incorporado."}
	ErrPlataformaFora       = &Erro{http.StatusBadGateway, "plataforma_indisponivel", "Não conseguimos falar com o YouTube ou o TikTok agora. Tente de novo em instantes."}
	ErrSemDireitoUso        = &Erro{http.StatusUnprocessableEntity, "direito_uso_obrigatorio", "Confirme que você tem direito de uso do vídeo."}
	ErrFormato              = &Erro{http.StatusUnprocessableEntity, "formato_invalido", "Envie um vídeo MP4, MOV ou WebM."}
	ErrArquivoGrande        = &Erro{http.StatusUnprocessableEntity, "arquivo_grande", fmt.Sprintf("O vídeo pode ter até %d GB.", MaxBytes>>30)}
	ErrCota                 = &Erro{http.StatusConflict, "cota_videos", "O espaço para vídeos do plano acabou. Apague vídeos para enviar outros."}
	ErrUploadsIndisponiveis = &Erro{http.StatusServiceUnavailable, "uploads_indisponiveis", "O envio de vídeos não está disponível agora."}
	ErrUploadEncerrado      = &Erro{http.StatusConflict, "upload_encerrado", "Esse envio já terminou ou foi cancelado."}
	ErrUploadIncompleto     = &Erro{http.StatusUnprocessableEntity, "upload_incompleto", "O envio não chegou inteiro. Envie o vídeo de novo."}
	ErrVideoNaoPronto       = &Erro{http.StatusConflict, "video_nao_pronto", "O vídeo ainda está sendo processado."}
	ErrSoDono               = &Erro{http.StatusForbidden, "sem_permissao", "Só quem enviou o vídeo pode alterá-lo."}
	ErrSoGestorCompartilha  = &Erro{http.StatusForbidden, "sem_permissao", "Só o dono e os mentores de uma mentoria compartilham vídeos com a turma."}
	ErrSemPartes            = &Erro{http.StatusUnprocessableEntity, "dados_invalidos", "Informe as partes enviadas."}
)

const (
	// MaxBytes é o tamanho máximo de um vídeo enviado.
	MaxBytes = 1 << 30
	// maxPartes segue o limite do S3 para uploads multipart.
	maxPartes          = 10000
	limiteCotaChave    = "video_bytes"
	validadeParte      = time.Hour
	validadeLeitura    = 6 * time.Hour
	validadeDownload   = 5 * time.Minute
	prazoUpload        = 24 * time.Hour
	intervaloRevalidar = 7 * 24 * time.Hour
)

// Formatos aceitos no upload, pelo Content-Type que o navegador informa.
var formatos = map[string]string{
	"video/mp4":       ".mp4",
	"video/quicktime": ".mov",
	"video/webm":      ".webm",
}

// Objetos é o bucket dos vídeos (storage.S3).
type Objetos interface {
	IniciarMultipart(ctx context.Context, chave, contentType string) (string, error)
	AssinarParte(ctx context.Context, chave, uploadID string, numero int, validade time.Duration) (string, error)
	Partes(ctx context.Context, chave, uploadID string) ([]storage.Parte, error)
	ConcluirMultipart(ctx context.Context, chave, uploadID string, partes []storage.Parte) error
	AbortarMultipart(ctx context.Context, chave, uploadID string) error
	Info(ctx context.Context, chave string) (storage.Objeto, error)
	AssinarGet(ctx context.Context, chave string, validade time.Duration, baixarComo string) (string, error)
	URLInterna(ctx context.Context, chave string, validade time.Duration) (string, error)
	EnviarArquivo(ctx context.Context, chave, caminho, contentType string) error
	ApagarPrefixo(ctx context.Context, prefixo string) error
}

// Limites lê os limites do plano (implementado por contas.Service).
type Limites interface {
	Limite(ctx context.Context, m contas.Membro, chave string) (int64, error)
}

// Job é um job a enfileirar, opcionalmente agendado.
type Job struct {
	Args JobArgs
	Em   time.Time
}

// Fila enfileira os jobs do módulo (em produção, o River).
type Fila interface {
	Enfileirar(ctx context.Context, jobs ...Job) error
}

// Dono identifica a biblioteca de um usuário num workspace.
type Dono struct {
	WorkspaceID uuid.UUID
	UsuarioID   uuid.UUID
}

func (d Dono) escopo() postgres.Escopo {
	return postgres.Escopo{UsuarioID: d.UsuarioID.String(), WorkspaceID: d.WorkspaceID.String()}
}

func donoDe(m contas.Membro) Dono { return Dono{WorkspaceID: m.WorkspaceID, UsuarioID: m.UsuarioID} }

type Service struct {
	pool        *pgxpool.Pool
	produtos    *produtos.Service
	oembed      *OEmbed
	objetos     Objetos // nil sem bucket: só embeds
	processador Processador
	limites     Limites
	fila        Fila
	log         *slog.Logger
}

// NewService monta o serviço. objetos pode ser nil (sem bucket, só há
// referências por embed); processador só é usado no worker.
func NewService(pool *pgxpool.Pool, p *produtos.Service, o *OEmbed, objetos Objetos, proc Processador, l Limites, f Fila, log *slog.Logger) *Service {
	return &Service{pool: pool, produtos: p, oembed: o, objetos: objetos, processador: proc, limites: l, fila: f, log: log}
}

func (s *Service) tx(ctx context.Context, d Dono, fn func(*midiadb.Queries) error) error {
	return postgres.InTx(ctx, s.pool, d.escopo(), func(tx pgx.Tx) error { return fn(midiadb.New(tx)) })
}

func chaveOriginal(v midiadb.Video) string { return *v.StorageKey + "original" }
func chaveThumb(v midiadb.Video) string    { return *v.StorageKey + "thumb.jpg" }
func chavePrevia(v midiadb.Video) string   { return *v.StorageKey + "preview.mp4" }

// Videos lista os vídeos que o usuário vê no workspace: os dele e os
// compartilhados. Com produtoID, só os ligados ao produto.
func (s *Service) Videos(ctx context.Context, m contas.Membro, produtoID *uuid.UUID, soMeus bool) ([]Video, error) {
	d := donoDe(m)
	if produtoID != nil {
		por, err := s.DosAlvos(ctx, m, AlvoProduto, []uuid.UUID{*produtoID})
		if err != nil {
			return nil, err
		}
		out := por[*produtoID]
		if soMeus {
			out = filtrarMeus(out)
		}
		if out == nil {
			out = []Video{}
		}
		return out, nil
	}
	var rows []midiadb.Video
	err := s.tx(ctx, d, func(q *midiadb.Queries) error {
		var err error
		rows, err = q.Videos(ctx, midiadb.VideosParams{WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID, SoMeus: soMeus})
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.montar(ctx, d, rows)
}

func filtrarMeus(vs []Video) []Video {
	var out []Video
	for _, v := range vs {
		if v.Meu {
			out = append(out, v)
		}
	}
	return out
}

// DosAlvos devolve, por alvo, os vídeos ligados a ele que o usuário vê.
func (s *Service) DosAlvos(ctx context.Context, m contas.Membro, alvo Alvo, ids []uuid.UUID) (map[uuid.UUID][]Video, error) {
	d := donoDe(m)
	out := map[uuid.UUID][]Video{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []midiadb.VideosDosAlvosRow
	err := s.tx(ctx, d, func(q *midiadb.Queries) error {
		var err error
		rows, err = q.VideosDosAlvos(ctx, midiadb.VideosDosAlvosParams{WorkspaceID: d.WorkspaceID, AlvoTipo: midiadb.VideoAlvo(alvo), AlvoIds: ids})
		return err
	})
	if err != nil {
		return nil, err
	}
	videos := make([]midiadb.Video, 0, len(rows))
	vistos := map[uuid.UUID]bool{}
	for _, r := range rows {
		if !vistos[r.Video.ID] {
			vistos[r.Video.ID] = true
			videos = append(videos, r.Video)
		}
	}
	montados, err := s.montar(ctx, d, videos)
	if err != nil {
		return nil, err
	}
	porID := make(map[uuid.UUID]Video, len(montados))
	for _, v := range montados {
		porID[v.ID] = v
	}
	for _, r := range rows {
		out[r.AlvoID] = append(out[r.AlvoID], porID[r.Video.ID])
	}
	return out, nil
}

// Ver devolve um vídeo que o usuário vê.
func (s *Service) Ver(ctx context.Context, m contas.Membro, id uuid.UUID) (Video, error) {
	d := donoDe(m)
	row, err := s.video(ctx, d, id)
	if err != nil {
		return Video{}, err
	}
	vs, err := s.montar(ctx, d, []midiadb.Video{row})
	if err != nil {
		return Video{}, err
	}
	return vs[0], nil
}

func (s *Service) video(ctx context.Context, d Dono, id uuid.UUID) (midiadb.Video, error) {
	var row midiadb.Video
	err := s.tx(ctx, d, func(q *midiadb.Queries) error {
		var err error
		row, err = q.Video(ctx, midiadb.VideoParams{ID: id, WorkspaceID: d.WorkspaceID})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, ErrVideoNaoEncontrado
	}
	return row, err
}

// meu devolve o vídeo do próprio usuário, ou um erro: não encontrado se ele
// não o vê, só dono se o vídeo é de outra pessoa (compartilhado).
func (s *Service) meu(ctx context.Context, d Dono, id uuid.UUID) (midiadb.Video, error) {
	row, err := s.video(ctx, d, id)
	if err != nil {
		return row, err
	}
	if row.DonoID != d.UsuarioID {
		return row, ErrSoDono
	}
	return row, nil
}

// ColarLink resolve um link do YouTube ou do TikTok pelo oEmbed e guarda a
// referência (só metadados). Colar de novo o mesmo vídeo devolve o existente,
// com os dados atualizados. Com produtoID, o vídeo também se liga ao produto.
func (s *Service) ColarLink(ctx context.Context, m contas.Membro, link string, produtoID *uuid.UUID) (Video, bool, error) {
	d := donoDe(m)
	if produtoID != nil {
		if err := s.exigirProduto(ctx, d, *produtoID); err != nil {
			return Video{}, false, err
		}
	}
	ref, err := s.oembed.Resolver(ctx, link, true)
	switch {
	case errors.Is(err, errLinkVideo):
		return Video{}, false, ErrLinkVideo
	case errors.Is(err, errIndisponivel):
		return Video{}, false, ErrVideoIndisponivel
	case errors.Is(err, errPlataformaFora):
		s.log.WarnContext(ctx, "oEmbed não respondeu", "err", err)
		return Video{}, false, ErrPlataformaFora
	case err != nil:
		return Video{}, false, err
	}
	var row midiadb.CriarEmbedRow
	err = s.tx(ctx, d, func(q *midiadb.Queries) error {
		var err error
		row, err = q.CriarEmbed(ctx, midiadb.CriarEmbedParams{
			WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID, Plataforma: ref.Plataforma,
			Titulo: ref.Titulo, Autor: ref.Autor, Url: &ref.URL, EmbedID: &ref.EmbedID, ThumbUrl: opcional(ref.ThumbURL),
		})
		if err != nil {
			return err
		}
		if produtoID != nil {
			_, err = q.Vincular(ctx, midiadb.VincularParams{
				VideoID: row.ID, WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID, AlvoTipo: midiadb.VideoAlvoProduto, AlvoID: *produtoID,
			})
		}
		return err
	})
	if err != nil {
		return Video{}, false, err
	}
	if row.Criado {
		s.agendar(ctx, Job{Args: RevalidarEmbedArgs{VideoID: row.ID, WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID}, Em: time.Now().Add(intervaloRevalidar)})
	}
	v, err := s.Ver(ctx, m, row.ID)
	return v, row.Criado, err
}

// agendar enfileira jobs de manutenção. Se a fila falhar, o vídeo segue
// funcionando; só perde a revalidação ou a limpeza automática.
func (s *Service) agendar(ctx context.Context, jobs ...Job) {
	if s.fila == nil {
		return
	}
	if err := s.fila.Enfileirar(ctx, jobs...); err != nil {
		s.log.ErrorContext(ctx, "não foi possível agendar job de mídia", "err", err)
	}
}

// IniciarUpload valida o arquivo, reserva o espaço na cota do workspace e
// abre o upload multipart no bucket. O navegador envia as partes direto ao
// bucket, pelas URLs de AssinarParte. Um upload que não termina em 24 h é
// descartado pelo job limpar_upload.
func (s *Service) IniciarUpload(ctx context.Context, m contas.Membro, n NovoUpload) (UploadIniciado, error) {
	d := donoDe(m)
	if s.objetos == nil {
		return UploadIniciado{}, ErrUploadsIndisponiveis
	}
	if !n.DireitoUso {
		return UploadIniciado{}, ErrSemDireitoUso
	}
	ct := strings.ToLower(strings.TrimSpace(n.ContentType))
	if _, ok := formatos[ct]; !ok {
		return UploadIniciado{}, ErrFormato
	}
	if n.Tamanho <= 0 {
		return UploadIniciado{}, erroValidacao("Informe o tamanho do arquivo.")
	}
	if n.Tamanho > MaxBytes {
		return UploadIniciado{}, ErrArquivoGrande
	}
	nome := nomeArquivo(n.Nome)
	if n.ProdutoID != nil {
		if err := s.exigirProduto(ctx, d, *n.ProdutoID); err != nil {
			return UploadIniciado{}, err
		}
	}
	limite, err := s.limites.Limite(ctx, m, limiteCotaChave)
	if err != nil {
		return UploadIniciado{}, err
	}
	if n.Tamanho > limite {
		return UploadIniciado{}, ErrCota
	}

	id := uuid.New()
	prefixo := fmt.Sprintf("videos/%s/%s/", d.WorkspaceID, id)
	uploadID, err := s.objetos.IniciarMultipart(ctx, prefixo+"original", ct)
	if err != nil {
		return UploadIniciado{}, err
	}
	var row midiadb.Video
	err = s.tx(ctx, d, func(q *midiadb.Queries) error {
		uso, err := q.SomarUso(ctx, midiadb.SomarUsoParams{WorkspaceID: d.WorkspaceID, Delta: n.Tamanho})
		if err != nil {
			return err
		}
		if uso > limite {
			return ErrCota
		}
		row, err = q.CriarUpload(ctx, midiadb.CriarUploadParams{
			ID: id, WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID, Titulo: tituloDoArquivo(nome),
			StorageKey: &prefixo, UploadID: &uploadID, NomeArquivo: &nome, ContentType: &ct, TamanhoBytes: n.Tamanho,
		})
		if err != nil {
			return err
		}
		if n.ProdutoID != nil {
			_, err = q.Vincular(ctx, midiadb.VincularParams{
				VideoID: id, WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID, AlvoTipo: midiadb.VideoAlvoProduto, AlvoID: *n.ProdutoID,
			})
		}
		return err
	})
	if err != nil {
		if errAbort := s.objetos.AbortarMultipart(ctx, prefixo+"original", uploadID); errAbort != nil {
			s.log.ErrorContext(ctx, "upload aberto ficou sem vídeo", "chave", prefixo, "err", errAbort)
		}
		return UploadIniciado{}, err
	}
	s.agendar(ctx, Job{Args: LimparUploadArgs{VideoID: id, WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID}, Em: time.Now().Add(prazoUpload)})
	vs, err := s.montar(ctx, d, []midiadb.Video{row})
	if err != nil {
		return UploadIniciado{}, err
	}
	return UploadIniciado{Video: vs[0], UploadID: uploadID, Chave: prefixo + "original"}, nil
}

// enviando devolve o vídeo do usuário cujo upload ainda está aberto.
func (s *Service) enviando(ctx context.Context, d Dono, id uuid.UUID) (midiadb.Video, error) {
	if s.objetos == nil {
		return midiadb.Video{}, ErrUploadsIndisponiveis
	}
	row, err := s.meu(ctx, d, id)
	if err != nil {
		return row, err
	}
	if Status(row.Status) != StatusEnviando || row.UploadID == nil {
		return row, ErrUploadEncerrado
	}
	return row, nil
}

// AssinarParte devolve a URL para o navegador enviar uma parte do upload.
func (s *Service) AssinarParte(ctx context.Context, m contas.Membro, id uuid.UUID, numero int) (string, error) {
	if numero < 1 || numero > maxPartes {
		return "", erroValidacao(fmt.Sprintf("A parte deve ser de 1 a %d.", maxPartes))
	}
	row, err := s.enviando(ctx, donoDe(m), id)
	if err != nil {
		return "", err
	}
	return s.objetos.AssinarParte(ctx, chaveOriginal(row), *row.UploadID, numero, validadeParte)
}

// Partes lista as partes já enviadas, para retomar um upload.
func (s *Service) Partes(ctx context.Context, m contas.Membro, id uuid.UUID) ([]storage.Parte, error) {
	row, err := s.enviando(ctx, donoDe(m), id)
	if err != nil {
		return nil, err
	}
	ps, err := s.objetos.Partes(ctx, chaveOriginal(row), *row.UploadID)
	if errors.Is(err, storage.ErrNaoEncontrado) {
		return nil, ErrUploadEncerrado
	}
	return ps, err
}

// ConcluirUpload junta as partes, confere o tamanho com o que foi reservado
// na cota e põe o vídeo na fila do processar_video.
func (s *Service) ConcluirUpload(ctx context.Context, m contas.Membro, id uuid.UUID, partes []storage.Parte) (Video, error) {
	d := donoDe(m)
	if len(partes) == 0 || len(partes) > maxPartes {
		return Video{}, ErrSemPartes
	}
	row, err := s.enviando(ctx, d, id)
	if err != nil {
		return Video{}, err
	}
	chave := chaveOriginal(row)
	if err := s.objetos.ConcluirMultipart(ctx, chave, *row.UploadID, partes); err != nil {
		if errors.Is(err, storage.ErrNaoEncontrado) {
			return Video{}, ErrUploadEncerrado
		}
		// Partes faltando ou com ETag errado: o upload continua aberto.
		s.log.WarnContext(ctx, "não deu para concluir o upload", "video_id", id, "err", err)
		return Video{}, ErrUploadIncompleto
	}
	info, err := s.objetos.Info(ctx, chave)
	if err != nil {
		return Video{}, err
	}
	if info.Tamanho > row.TamanhoBytes {
		// Maior do que o reservado na cota: descarta.
		if err := s.apagar(ctx, d, id); err != nil {
			return Video{}, err
		}
		return Video{}, ErrUploadIncompleto
	}
	err = s.tx(ctx, d, func(q *midiadb.Queries) error {
		atual, err := q.TravarVideo(ctx, midiadb.TravarVideoParams{ID: id, WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVideoNaoEncontrado
		}
		if err != nil {
			return err
		}
		if Status(atual.Status) != StatusEnviando {
			return ErrUploadEncerrado
		}
		if _, err := q.SomarUso(ctx, midiadb.SomarUsoParams{WorkspaceID: d.WorkspaceID, Delta: info.Tamanho - atual.TamanhoBytes}); err != nil {
			return err
		}
		return q.ConcluirUpload(ctx, midiadb.ConcluirUploadParams{ID: id, WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID, TamanhoBytes: info.Tamanho})
	})
	if err != nil {
		return Video{}, err
	}
	if s.fila != nil {
		if err := s.fila.Enfileirar(ctx, Job{Args: ProcessarVideoArgs{VideoID: id, WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID}}); err != nil {
			s.log.ErrorContext(ctx, "não foi possível enfileirar o processar_video", "video_id", id, "err", err)
			s.marcar(ctx, d, id, StatusFalhou)
		}
	}
	return s.Ver(ctx, m, id)
}

func (s *Service) marcar(ctx context.Context, d Dono, id uuid.UUID, st Status) {
	err := s.tx(ctx, d, func(q *midiadb.Queries) error {
		return q.MarcarStatus(ctx, midiadb.MarcarStatusParams{ID: id, WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID, Status: midiadb.VideoStatus(st)})
	})
	if err != nil {
		s.log.ErrorContext(ctx, "não foi possível marcar o vídeo", "video_id", id, "status", st, "err", err)
	}
}

// Apagar apaga o vídeo, os arquivos no bucket (ou o upload em andamento) e os
// vínculos, e devolve o espaço à cota.
func (s *Service) Apagar(ctx context.Context, m contas.Membro, id uuid.UUID) error {
	d := donoDe(m)
	if _, err := s.meu(ctx, d, id); err != nil {
		return err
	}
	return s.apagar(ctx, d, id)
}

func (s *Service) apagar(ctx context.Context, d Dono, id uuid.UUID) error {
	return s.tx(ctx, d, func(q *midiadb.Queries) error {
		row, err := q.TravarVideo(ctx, midiadb.TravarVideoParams{ID: id, WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVideoNaoEncontrado
		}
		if err != nil {
			return err
		}
		if _, err := q.ApagarVideo(ctx, midiadb.ApagarVideoParams{ID: id, WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID}); err != nil {
			return err
		}
		if row.Tipo != midiadb.VideoTipoUpload {
			return nil
		}
		if _, err := q.SomarUso(ctx, midiadb.SomarUsoParams{WorkspaceID: d.WorkspaceID, Delta: -row.TamanhoBytes}); err != nil {
			return err
		}
		// Os arquivos saem antes do commit: se o bucket falhar, o vídeo fica.
		if s.objetos == nil {
			return ErrUploadsIndisponiveis
		}
		if row.UploadID != nil {
			if err := s.objetos.AbortarMultipart(ctx, chaveOriginal(row), *row.UploadID); err != nil {
				return err
			}
		}
		return s.objetos.ApagarPrefixo(ctx, *row.StorageKey)
	})
}

// Editar troca o título e o compartilhamento com a turma (nil não muda).
func (s *Service) Editar(ctx context.Context, m contas.Membro, id uuid.UUID, titulo *string, compartilhado *bool) (Video, error) {
	d := donoDe(m)
	if _, err := s.meu(ctx, d, id); err != nil {
		return Video{}, err
	}
	var t string
	if titulo != nil {
		t = strings.Join(strings.Fields(*titulo), " ")
		if utf8.RuneCountInString(t) > maxTextoVideo {
			return Video{}, erroValidacao(fmt.Sprintf("O título pode ter até %d caracteres.", maxTextoVideo))
		}
	}
	if compartilhado != nil && *compartilhado && !podeCompartilhar(m) {
		return Video{}, ErrSoGestorCompartilha
	}
	err := s.tx(ctx, d, func(q *midiadb.Queries) error {
		if titulo != nil {
			if _, err := q.Renomear(ctx, midiadb.RenomearParams{ID: id, WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID, Titulo: t}); err != nil {
				return err
			}
		}
		if compartilhado != nil {
			if _, err := q.Compartilhar(ctx, midiadb.CompartilharParams{ID: id, WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID, Compartilhado: *compartilhado}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Video{}, err
	}
	return s.Ver(ctx, m, id)
}

func podeCompartilhar(m contas.Membro) bool {
	return m.TipoWorkspace == contas.TipoMentoria && m.Papel.Gestor()
}

// Download devolve uma URL de curta duração para baixar o arquivo original
// de um vídeo enviado (o próprio ou um compartilhado com a turma).
func (s *Service) Download(ctx context.Context, m contas.Membro, id uuid.UUID) (string, error) {
	row, err := s.video(ctx, donoDe(m), id)
	if err != nil {
		return "", err
	}
	if row.Tipo != midiadb.VideoTipoUpload {
		return "", erroValidacao("Vídeos de referência não podem ser baixados; abra-os na plataforma.")
	}
	if st := Status(row.Status); st == StatusEnviando || st == StatusProcessando {
		return "", ErrVideoNaoPronto
	}
	if s.objetos == nil {
		return "", ErrUploadsIndisponiveis
	}
	nome := "video.mp4"
	if row.NomeArquivo != nil && *row.NomeArquivo != "" {
		nome = *row.NomeArquivo
	}
	return s.objetos.AssinarGet(ctx, chaveOriginal(row), validadeDownload, nome)
}

// VincularProduto liga um vídeo do usuário a um produto do catálogo.
func (s *Service) VincularProduto(ctx context.Context, m contas.Membro, id, produtoID uuid.UUID) (Video, error) {
	d := donoDe(m)
	if _, err := s.meu(ctx, d, id); err != nil {
		return Video{}, err
	}
	if err := s.exigirProduto(ctx, d, produtoID); err != nil {
		return Video{}, err
	}
	err := s.tx(ctx, d, func(q *midiadb.Queries) error {
		_, err := q.Vincular(ctx, midiadb.VincularParams{
			VideoID: id, WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID, AlvoTipo: midiadb.VideoAlvoProduto, AlvoID: produtoID,
		})
		return err
	})
	if err != nil {
		return Video{}, err
	}
	return s.Ver(ctx, m, id)
}

// DesvincularProduto tira o vídeo do produto.
func (s *Service) DesvincularProduto(ctx context.Context, m contas.Membro, id, produtoID uuid.UUID) (Video, error) {
	d := donoDe(m)
	if _, err := s.meu(ctx, d, id); err != nil {
		return Video{}, err
	}
	err := s.tx(ctx, d, func(q *midiadb.Queries) error {
		_, err := q.Desvincular(ctx, midiadb.DesvincularParams{
			WorkspaceID: d.WorkspaceID, VideoID: id, AlvoTipo: midiadb.VideoAlvoProduto, AlvoID: produtoID,
		})
		return err
	})
	if err != nil {
		return Video{}, err
	}
	return s.Ver(ctx, m, id)
}

// VincularLista anexa um vídeo do usuário a uma lista da curadoria e o
// compartilha com a turma. Quem chama (o módulo curadoria) confere a lista e
// o papel de quem anexa.
func (s *Service) VincularLista(ctx context.Context, m contas.Membro, id, listaID uuid.UUID) error {
	d := donoDe(m)
	if _, err := s.meu(ctx, d, id); err != nil {
		return err
	}
	if !podeCompartilhar(m) {
		return ErrSoGestorCompartilha
	}
	return s.tx(ctx, d, func(q *midiadb.Queries) error {
		if _, err := q.Compartilhar(ctx, midiadb.CompartilharParams{ID: id, WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID, Compartilhado: true}); err != nil {
			return err
		}
		_, err := q.Vincular(ctx, midiadb.VincularParams{
			VideoID: id, WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID, AlvoTipo: midiadb.VideoAlvoLista, AlvoID: listaID,
		})
		return err
	})
}

// DesvincularLista tira um vídeo da lista (de qualquer autor; a RLS só deixa
// dono e mentor). O vídeo continua compartilhado.
func (s *Service) DesvincularLista(ctx context.Context, m contas.Membro, id, listaID uuid.UUID) error {
	d := donoDe(m)
	var n int64
	err := s.tx(ctx, d, func(q *midiadb.Queries) error {
		var err error
		n, err = q.Desvincular(ctx, midiadb.DesvincularParams{WorkspaceID: d.WorkspaceID, VideoID: id, AlvoTipo: midiadb.VideoAlvoLista, AlvoID: listaID})
		return err
	})
	if err == nil && n == 0 {
		return ErrVideoNaoEncontrado
	}
	return err
}

// DesvincularAlvo tira todos os vídeos de um alvo apagado (uma lista).
func (s *Service) DesvincularAlvo(ctx context.Context, m contas.Membro, alvo Alvo, alvoID uuid.UUID) error {
	d := donoDe(m)
	return s.tx(ctx, d, func(q *midiadb.Queries) error {
		return q.DesvincularAlvo(ctx, midiadb.DesvincularAlvoParams{WorkspaceID: d.WorkspaceID, AlvoTipo: midiadb.VideoAlvo(alvo), AlvoID: alvoID})
	})
}

// Cota devolve o espaço de upload usado e o limite do plano.
func (s *Service) Cota(ctx context.Context, m contas.Membro) (Cota, error) {
	d := donoDe(m)
	limite, err := s.limites.Limite(ctx, m, limiteCotaChave)
	if err != nil {
		return Cota{}, err
	}
	out := Cota{LimiteBytes: limite}
	err = s.tx(ctx, d, func(q *midiadb.Queries) error {
		var err error
		out.UsadosBytes, err = q.Uso(ctx, d.WorkspaceID)
		return err
	})
	return out, err
}

func (s *Service) exigirProduto(ctx context.Context, d Dono, id uuid.UUID) error {
	ps, err := s.produtos.Varios(ctx, d.escopo(), []uuid.UUID{id})
	if err != nil {
		return err
	}
	if _, ok := ps[id]; !ok {
		return ErrProdutoNaoEncontrado
	}
	return nil
}

// montar junta os vínculos (só nos vídeos do próprio usuário) e assina as
// URLs de miniatura e prévia dos uploads prontos.
func (s *Service) montar(ctx context.Context, d Dono, rows []midiadb.Video) ([]Video, error) {
	out := make([]Video, len(rows))
	var meus []uuid.UUID
	for _, r := range rows {
		if r.DonoID == d.UsuarioID {
			meus = append(meus, r.ID)
		}
	}
	var vinculos []midiadb.VinculosDosVideosRow
	if len(meus) > 0 {
		err := s.tx(ctx, d, func(q *midiadb.Queries) error {
			var err error
			vinculos, err = q.VinculosDosVideos(ctx, midiadb.VinculosDosVideosParams{WorkspaceID: d.WorkspaceID, VideoIds: meus})
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	for i, r := range rows {
		v := Video{
			ID: r.ID, Tipo: Tipo(r.Tipo), Plataforma: r.Plataforma, Status: Status(r.Status), Titulo: r.Titulo, Autor: r.Autor,
			URL: r.Url, ThumbURL: r.ThumbUrl, DuracaoS: r.DuracaoS, Largura: r.Largura, Altura: r.Altura,
			TamanhoBytes: r.TamanhoBytes, NomeArquivo: r.NomeArquivo, Compartilhado: r.Compartilhado,
			Meu: r.DonoID == d.UsuarioID, ProdutoIDs: []uuid.UUID{}, ListaIDs: []uuid.UUID{}, CriadoEm: r.CriadoEm,
		}
		if r.Tipo == midiadb.VideoTipoEmbed && r.EmbedID != nil {
			v.PlayerURL = opcional(PlayerURL(r.Plataforma, *r.EmbedID))
		}
		if r.Tipo == midiadb.VideoTipoUpload && r.Status == midiadb.VideoStatusPronto && s.objetos != nil {
			thumb, err := s.objetos.AssinarGet(ctx, chaveThumb(r), validadeLeitura, "")
			if err != nil {
				return nil, err
			}
			previa, err := s.objetos.AssinarGet(ctx, chavePrevia(r), validadeLeitura, "")
			if err != nil {
				return nil, err
			}
			v.ThumbURL, v.PreviewURL = &thumb, &previa
		}
		for _, vv := range vinculos {
			if vv.VideoID != r.ID {
				continue
			}
			if vv.AlvoTipo == midiadb.VideoAlvoProduto {
				v.ProdutoIDs = append(v.ProdutoIDs, vv.AlvoID)
			} else {
				v.ListaIDs = append(v.ListaIDs, vv.AlvoID)
			}
		}
		out[i] = v
	}
	return out, nil
}

func erroValidacao(msg string) *Erro {
	return &Erro{http.StatusUnprocessableEntity, "dados_invalidos", msg}
}

func opcional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nomeArquivo limpa o nome que o navegador informa: sem pastas nem
// caracteres de controle, até 200 caracteres.
func nomeArquivo(nome string) string {
	nome = path.Base(strings.ReplaceAll(strings.TrimSpace(nome), "\\", "/"))
	nome = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, nome)
	if nome == "." || nome == "/" || nome == "" {
		nome = "video"
	}
	return cortar(nome, maxTextoVideo)
}

// tituloDoArquivo é o título inicial: o nome do arquivo sem a extensão.
func tituloDoArquivo(nome string) string {
	t := strings.TrimSuffix(nome, path.Ext(nome))
	t = strings.Join(strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(t)), " ")
	if t == "" {
		return "Vídeo"
	}
	return t
}
