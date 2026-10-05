package midia

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/queue"
	"github.com/rafixcs/app-parceiros/backend/internal/midia/midiadb"
)

// JobArgs são os argumentos de um job do River.
type JobArgs = river.JobArgs

// ProcessarVideoArgs gera a prévia em 720p e a miniatura de um upload
// concluído e lê a duração e as dimensões.
type ProcessarVideoArgs struct {
	VideoID     uuid.UUID `json:"video_id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	DonoID      uuid.UUID `json:"dono_id"`
}

func (ProcessarVideoArgs) Kind() string { return "processar_video" }

func (ProcessarVideoArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: queue.QueueMedia, MaxAttempts: 3,
		UniqueOpts: river.UniqueOpts{ByArgs: true},
	}
}

// RevalidarEmbedArgs confere no oEmbed se um vídeo de referência ainda existe.
// Cada embed é revalidado a cada 7 dias: o job agenda a próxima rodada.
type RevalidarEmbedArgs struct {
	VideoID     uuid.UUID `json:"video_id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	DonoID      uuid.UUID `json:"dono_id"`
}

func (RevalidarEmbedArgs) Kind() string { return "revalidar_embed" }

func (RevalidarEmbedArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		// Sem UniqueOpts: o River exige o estado running nelas, e o job agenda
		// a próxima rodada de si mesmo. Cada vídeo só agenda uma vez, ao ser criado.
		Queue: queue.QueueDefault, MaxAttempts: 5,
	}
}

// LimparUploadArgs descarta um upload que não terminou no prazo (24 h): as
// partes no bucket, o vídeo e a reserva na cota.
type LimparUploadArgs struct {
	VideoID     uuid.UUID `json:"video_id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	DonoID      uuid.UUID `json:"dono_id"`
}

func (LimparUploadArgs) Kind() string { return "limpar_upload" }

func (LimparUploadArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: queue.QueueMedia, MaxAttempts: 10,
	}
}

// FilaRiver enfileira no River. No worker, o cliente só existe depois de
// registrados os workers; ele é atribuído antes de a fila começar a rodar.
type FilaRiver struct {
	Client *river.Client[pgx.Tx]
}

func (f *FilaRiver) Enfileirar(ctx context.Context, js ...Job) error {
	params := make([]river.InsertManyParams, len(js))
	for i, j := range js {
		params[i] = river.InsertManyParams{Args: j.Args}
		if !j.Em.IsZero() {
			params[i].InsertOpts = &river.InsertOpts{ScheduledAt: j.Em}
		}
	}
	_, err := f.Client.InsertMany(ctx, params)
	return err
}

type ProcessarVideoWorker struct {
	river.WorkerDefaults[ProcessarVideoArgs]
	Svc *Service
	Log *slog.Logger
}

func (w *ProcessarVideoWorker) Timeout(*river.Job[ProcessarVideoArgs]) time.Duration {
	return 30 * time.Minute
}

func (w *ProcessarVideoWorker) Work(ctx context.Context, job *river.Job[ProcessarVideoArgs]) error {
	d := Dono{WorkspaceID: job.Args.WorkspaceID, UsuarioID: job.Args.DonoID}
	log := w.Log.With("job", job.Kind, "video_id", job.Args.VideoID)
	err := w.Svc.Processar(ctx, d, job.Args.VideoID)
	switch {
	case errors.Is(err, ErrNaoEhVideo):
		log.Info("arquivo enviado não é um vídeo legível", "err", err)
		w.Svc.marcar(ctx, d, job.Args.VideoID, StatusFalhou)
		return river.JobCancel(err)
	case err != nil && job.Attempt >= job.MaxAttempts:
		log.Error("não foi possível processar o vídeo; desistindo", "err", err)
		w.Svc.marcar(ctx, d, job.Args.VideoID, StatusFalhou)
	}
	return err
}

type RevalidarEmbedWorker struct {
	river.WorkerDefaults[RevalidarEmbedArgs]
	Svc *Service
}

func (w *RevalidarEmbedWorker) Work(ctx context.Context, job *river.Job[RevalidarEmbedArgs]) error {
	return w.Svc.Revalidar(ctx, Dono{WorkspaceID: job.Args.WorkspaceID, UsuarioID: job.Args.DonoID}, job.Args.VideoID)
}

type LimparUploadWorker struct {
	river.WorkerDefaults[LimparUploadArgs]
	Svc *Service
}

func (w *LimparUploadWorker) Work(ctx context.Context, job *river.Job[LimparUploadArgs]) error {
	return w.Svc.LimparUpload(ctx, Dono{WorkspaceID: job.Args.WorkspaceID, UsuarioID: job.Args.DonoID}, job.Args.VideoID)
}

// donoVideo lê o vídeo pelo escopo do dono. Sem vídeo, devolve ok=false.
func (s *Service) donoVideo(ctx context.Context, d Dono, id uuid.UUID) (midiadb.Video, bool, error) {
	row, err := s.video(ctx, d, id)
	if errors.Is(err, ErrVideoNaoEncontrado) {
		return row, false, nil
	}
	return row, err == nil && row.DonoID == d.UsuarioID, err
}

// Processar gera prévia e miniatura de um upload concluído e grava duração e
// dimensões. O ffmpeg lê o original direto do bucket por URL assinada.
func (s *Service) Processar(ctx context.Context, d Dono, id uuid.UUID) error {
	row, ok, err := s.donoVideo(ctx, d, id)
	if err != nil || !ok || Status(row.Status) != StatusProcessando {
		return err // apagado ou já processado
	}
	entrada, err := s.objetos.InternalURL(ctx, chaveOriginal(row), time.Hour)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "video-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	p, err := s.processador.Processar(ctx, entrada, dir)
	if err != nil {
		return err
	}
	if err := s.objetos.UploadFile(ctx, chavePrevia(row), p.Previa, "video/mp4"); err != nil {
		return err
	}
	if err := s.objetos.UploadFile(ctx, chaveThumb(row), p.Thumb, "image/jpeg"); err != nil {
		return err
	}
	return s.tx(ctx, d, func(q *midiadb.Queries) error {
		return q.Processado(ctx, midiadb.ProcessadoParams{
			ID: id, WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID, DuracaoS: &p.DuracaoS, Largura: &p.Largura, Altura: &p.Altura,
		})
	})
}

// Revalidar confere o embed no oEmbed (sem cache): marca indisponível se o
// vídeo sumiu ou ficou privado, atualiza título e miniatura se ainda existe,
// e agenda a próxima revalidação.
func (s *Service) Revalidar(ctx context.Context, d Dono, id uuid.UUID) error {
	row, ok, err := s.donoVideo(ctx, d, id)
	if err != nil || !ok || row.Tipo != midiadb.VideoTipoEmbed || row.Url == nil {
		return err
	}
	ref, err := s.oembed.Resolver(ctx, *row.Url, false)
	p := midiadb.RevalidadoParams{ID: id, WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID, Status: midiadb.VideoStatusPronto}
	switch {
	case errors.Is(err, errIndisponivel), errors.Is(err, errLinkVideo):
		p.Status = midiadb.VideoStatusIndisponivel
	case err != nil:
		return err // a plataforma não respondeu: o River tenta de novo
	default:
		p.Titulo, p.Autor, p.ThumbUrl = &ref.Titulo, &ref.Autor, opcional(ref.ThumbURL)
	}
	if err := s.tx(ctx, d, func(q *midiadb.Queries) error { return q.Revalidado(ctx, p) }); err != nil {
		return err
	}
	return s.fila.Enfileirar(ctx, Job{Args: RevalidarEmbedArgs{VideoID: id, WorkspaceID: d.WorkspaceID, DonoID: d.UsuarioID}, Em: time.Now().Add(intervaloRevalidar)})
}

// LimparUpload descarta o upload se ele ainda não terminou.
func (s *Service) LimparUpload(ctx context.Context, d Dono, id uuid.UUID) error {
	row, ok, err := s.donoVideo(ctx, d, id)
	if err != nil || !ok || Status(row.Status) != StatusEnviando {
		return err
	}
	err = s.apagar(ctx, d, id)
	if errors.Is(err, ErrVideoNaoEncontrado) {
		return nil
	}
	return err
}
