// Package notificacoes entrega avisos aos usuários: a caixa de entrada do
// app (por workspace), o e-mail e o Web Push do navegador.
//
// Outros módulos chamam Notificar, que enfileira um job entregar_notificacao
// por destinatário. O worker grava a notificação com o escopo do
// destinatário e manda o e-mail e o push, sem repetir o que já foi enviado
// quando o job é refeito.
package notificacoes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/notificacoes/notificacoesdb"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/postgres"
)

// Dono identifica a caixa de entrada: um usuário dentro de um workspace.
type Dono struct {
	WorkspaceID uuid.UUID
	UsuarioID   uuid.UUID
}

func (d Dono) escopo() postgres.Escopo {
	return postgres.Escopo{UsuarioID: d.UsuarioID.String(), WorkspaceID: d.WorkspaceID.String()}
}

func escopoUsuario(id uuid.UUID) postgres.Escopo {
	return postgres.Escopo{UsuarioID: id.String()}
}

type Notificacao struct {
	ID       uuid.UUID  `json:"id"`
	Tipo     string     `json:"tipo"`
	Titulo   string     `json:"titulo"`
	Corpo    string     `json:"corpo"`
	URL      string     `json:"url"`
	CriadoEm time.Time  `json:"criado_em"`
	LidaEm   *time.Time `json:"lida_em"`
}

type Caixa struct {
	Notificacoes []Notificacao `json:"notificacoes"`
	NaoLidas     int64         `json:"nao_lidas"`
}

// Nova é uma notificação para vários usuários de um workspace. Chave a torna
// idempotente por usuário (ex.: "lista:<id>"); URL é um caminho do app
// (ex.: "/w/<id>/listas/<id>").
type Nova struct {
	WorkspaceID uuid.UUID
	UsuarioIDs  []uuid.UUID
	Tipo        string
	Chave       string
	Titulo      string
	Corpo       string
	URL         string
}

// Preferencias do usuário, valem em todos os workspaces.
type Preferencias struct {
	Email bool `json:"email"`
	// PushChavePublica é a chave VAPID para o navegador se inscrever; nula
	// quando o servidor não tem Web Push configurado.
	PushChavePublica *string `json:"push_chave_publica"`
	// Inscricoes conta os navegadores inscritos no push.
	Inscricoes int64 `json:"inscricoes"`
}

// Inscricao é a PushSubscription do navegador (PushSubscription.toJSON()).
type Inscricao struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// Erro é um erro de negócio com o status HTTP e o código que a API devolve.
type Erro struct {
	Status   int
	Codigo   string
	Mensagem string
}

func (e *Erro) Error() string { return e.Codigo }

var (
	ErrNotificacaoNaoEncontrada = &Erro{http.StatusNotFound, "notificacao_nao_encontrada", "Notificação não encontrada."}
	ErrPushIndisponivel         = &Erro{http.StatusServiceUnavailable, "push_indisponivel", "As notificações no navegador não estão disponíveis neste servidor."}
	ErrInscricaoInvalida        = &Erro{http.StatusUnprocessableEntity, "inscricao_invalida", "Este navegador enviou uma inscrição de notificação inválida."}
	ErrInscricoesDemais         = &Erro{http.StatusConflict, "inscricoes_demais", "Você já ativou as notificações em navegadores demais. Desative em algum deles."}
)

const (
	tamanhoCaixa   = 50
	maxInscricoes  = 20
	maxTitulo      = 200
	maxCorpo       = 1000
	timeoutConvite = 10 * time.Second
)

// Fila enfileira o job entregar_notificacao (em produção, o River).
type Fila interface {
	Enfileirar(ctx context.Context, args ...EntregarArgs) error
}

// Contatos dá nome e e-mail do destinatário (implementado por contas.Service).
type Contatos interface {
	Contato(ctx context.Context, usuarioID uuid.UUID) (contas.Contato, error)
}

type Service struct {
	pool      *pgxpool.Pool
	fila      Fila
	contatos  Contatos
	remetente Remetente // nil: sem e-mail
	push      Push      // nil: sem Web Push
	appURL    string
	log       *slog.Logger
}

// NewService monta o serviço. remetente e push podem ser nil (o canal fica
// desligado); fila pode ser nil no worker, que não enfileira.
func NewService(pool *pgxpool.Pool, fila Fila, contatos Contatos, remetente Remetente, push Push, appURL string, log *slog.Logger) *Service {
	return &Service{
		pool: pool, fila: fila, contatos: contatos, remetente: remetente, push: push,
		appURL: strings.TrimRight(appURL, "/"), log: log,
	}
}

func (s *Service) tx(ctx context.Context, e postgres.Escopo, fn func(*notificacoesdb.Queries) error) error {
	return postgres.InTx(ctx, s.pool, e, func(tx pgx.Tx) error { return fn(notificacoesdb.New(tx)) })
}

// Notificar enfileira a entrega para cada destinatário.
func (s *Service) Notificar(ctx context.Context, n Nova) error {
	if len(n.UsuarioIDs) == 0 {
		return nil
	}
	args := make([]EntregarArgs, len(n.UsuarioIDs))
	for i, u := range n.UsuarioIDs {
		args[i] = EntregarArgs{
			WorkspaceID: n.WorkspaceID, UsuarioID: u, Tipo: n.Tipo, Chave: n.Chave,
			Titulo: cortar(n.Titulo, maxTitulo), Corpo: cortar(n.Corpo, maxCorpo), URL: n.URL,
		}
	}
	return s.fila.Enfileirar(ctx, args...)
}

// Caixa devolve as notificações mais recentes do usuário no workspace.
func (s *Service) Caixa(ctx context.Context, d Dono) (Caixa, error) {
	out := Caixa{Notificacoes: []Notificacao{}}
	err := s.tx(ctx, d.escopo(), func(q *notificacoesdb.Queries) error {
		rows, err := q.Caixa(ctx, notificacoesdb.CaixaParams{WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID, Limite: tamanhoCaixa})
		if err != nil {
			return err
		}
		for _, r := range rows {
			out.Notificacoes = append(out.Notificacoes, notificacaoDe(r))
		}
		out.NaoLidas, err = q.ContarNaoLidas(ctx, notificacoesdb.ContarNaoLidasParams{WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID})
		return err
	})
	return out, err
}

// MarcarLida marca uma notificação como lida.
func (s *Service) MarcarLida(ctx context.Context, d Dono, id uuid.UUID) error {
	return s.tx(ctx, d.escopo(), func(q *notificacoesdb.Queries) error {
		n, err := q.MarcarLida(ctx, notificacoesdb.MarcarLidaParams{ID: id, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID})
		if err == nil && n == 0 {
			return ErrNotificacaoNaoEncontrada
		}
		return err
	})
}

// MarcarTodasLidas marca todas as notificações do workspace como lidas.
func (s *Service) MarcarTodasLidas(ctx context.Context, d Dono) error {
	return s.tx(ctx, d.escopo(), func(q *notificacoesdb.Queries) error {
		return q.MarcarTodasLidas(ctx, notificacoesdb.MarcarTodasLidasParams{WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID})
	})
}

// Preferencias do usuário. Sem linha gravada, o e-mail fica ligado.
func (s *Service) Preferencias(ctx context.Context, usuarioID uuid.UUID) (Preferencias, error) {
	out := Preferencias{Email: true}
	if s.push != nil {
		k := s.push.ChavePublica()
		out.PushChavePublica = &k
	}
	err := s.tx(ctx, escopoUsuario(usuarioID), func(q *notificacoesdb.Queries) error {
		p, err := q.Preferencias(ctx, usuarioID)
		switch {
		case err == nil:
			out.Email = p.Email
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		out.Inscricoes, err = q.ContarInscricoes(ctx, usuarioID)
		return err
	})
	return out, err
}

// DefinirEmail liga ou desliga o e-mail das notificações.
func (s *Service) DefinirEmail(ctx context.Context, usuarioID uuid.UUID, email bool) (Preferencias, error) {
	if err := s.tx(ctx, escopoUsuario(usuarioID), func(q *notificacoesdb.Queries) error {
		_, err := q.SalvarPreferencias(ctx, notificacoesdb.SalvarPreferenciasParams{UsuarioID: usuarioID, Email: email})
		return err
	}); err != nil {
		return Preferencias{}, err
	}
	return s.Preferencias(ctx, usuarioID)
}

// Inscrever guarda a inscrição de Web Push do navegador.
func (s *Service) Inscrever(ctx context.Context, usuarioID uuid.UUID, in Inscricao) error {
	if s.push == nil {
		return ErrPushIndisponivel
	}
	if !endpointPermitido(in.Endpoint) || in.Keys.P256dh == "" || in.Keys.Auth == "" ||
		len(in.Keys.P256dh) > 200 || len(in.Keys.Auth) > 100 {
		return ErrInscricaoInvalida
	}
	return s.tx(ctx, escopoUsuario(usuarioID), func(q *notificacoesdb.Queries) error {
		n, err := q.ContarInscricoes(ctx, usuarioID)
		if err != nil {
			return err
		}
		if n >= maxInscricoes {
			// Reinscrever um navegador já inscrito continua valendo.
			apagadas, err := q.ApagarInscricao(ctx, notificacoesdb.ApagarInscricaoParams{UsuarioID: usuarioID, Endpoint: in.Endpoint})
			if err != nil {
				return err
			}
			if apagadas == 0 {
				return ErrInscricoesDemais
			}
		}
		return q.SalvarInscricao(ctx, notificacoesdb.SalvarInscricaoParams{
			UsuarioID: usuarioID, Endpoint: in.Endpoint, P256dh: in.Keys.P256dh, Auth: in.Keys.Auth,
		})
	})
}

// Desinscrever apaga a inscrição do navegador. Não existir não é erro.
func (s *Service) Desinscrever(ctx context.Context, usuarioID uuid.UUID, endpoint string) error {
	return s.tx(ctx, escopoUsuario(usuarioID), func(q *notificacoesdb.Queries) error {
		_, err := q.ApagarInscricao(ctx, notificacoesdb.ApagarInscricaoParams{UsuarioID: usuarioID, Endpoint: endpoint})
		return err
	})
}

// EnviarConvite manda o e-mail de um convite para a mentoria. É chamado na
// criação do convite (contas.Service.EnviarConvitesCom): o token não passa
// pela fila, que guardaria o link em texto no banco.
func (s *Service) EnviarConvite(ctx context.Context, c contas.EnvioConvite) error {
	if s.remetente == nil {
		return errors.New("e-mail desligado")
	}
	ctx, cancel := context.WithTimeout(ctx, timeoutConvite)
	defer cancel()
	err := s.remetente.Enviar(ctx, emailConvite(c))
	if err != nil {
		s.log.WarnContext(ctx, "não foi possível enviar o e-mail do convite", "err", err)
	}
	return err
}

// Entregar grava a notificação na caixa do destinatário e manda e-mail e
// push, cada um uma única vez.
func (s *Service) Entregar(ctx context.Context, a EntregarArgs) error {
	d := Dono{WorkspaceID: a.WorkspaceID, UsuarioID: a.UsuarioID}
	var n notificacoesdb.Notificacao
	if err := s.tx(ctx, d.escopo(), func(q *notificacoesdb.Queries) error {
		var err error
		n, err = q.CriarNotificacao(ctx, notificacoesdb.CriarNotificacaoParams{
			WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID, Tipo: a.Tipo, Chave: a.Chave,
			Titulo: a.Titulo, Corpo: a.Corpo, Url: a.URL,
		})
		return err
	}); err != nil {
		return err
	}

	if n.EmailEm == nil && s.remetente != nil {
		if err := s.entregarEmail(ctx, d, n); err != nil {
			return err
		}
	}
	if n.PushEm == nil && s.push != nil {
		if err := s.entregarPush(ctx, d, n); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) entregarEmail(ctx context.Context, d Dono, n notificacoesdb.Notificacao) error {
	prefs, err := s.Preferencias(ctx, d.UsuarioID)
	if err != nil {
		return err
	}
	if prefs.Email {
		c, err := s.contatos.Contato(ctx, d.UsuarioID)
		if err != nil {
			return err
		}
		// E-mail não verificado pode ser de outra pessoa: não mandamos.
		if c.EmailVerificado && c.Email != "" {
			if err := s.remetente.Enviar(ctx, s.emailNotificacao(c, n)); err != nil {
				return fmt.Errorf("enviando e-mail: %w", err)
			}
		}
	}
	return s.tx(ctx, d.escopo(), func(q *notificacoesdb.Queries) error {
		return q.MarcarEmail(ctx, notificacoesdb.MarcarEmailParams{ID: n.ID, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID})
	})
}

// entregarPush manda para cada navegador inscrito. Inscrições que o serviço
// de push deu como expiradas são apagadas; outras falhas só vão para o log,
// para não repetir o push nos navegadores que já receberam.
func (s *Service) entregarPush(ctx context.Context, d Dono, n notificacoesdb.Notificacao) error {
	var subs []notificacoesdb.PushInscricao
	if err := s.tx(ctx, escopoUsuario(d.UsuarioID), func(q *notificacoesdb.Queries) error {
		var err error
		subs, err = q.Inscricoes(ctx, d.UsuarioID)
		return err
	}); err != nil {
		return err
	}
	payload := mensagemPush(n)
	for _, sub := range subs {
		var in Inscricao
		in.Endpoint, in.Keys.P256dh, in.Keys.Auth = sub.Endpoint, sub.P256dh, sub.Auth
		err := s.push.Enviar(ctx, in, payload)
		switch {
		case errors.Is(err, ErrInscricaoExpirada):
			if err := s.Desinscrever(ctx, d.UsuarioID, sub.Endpoint); err != nil {
				return err
			}
		case err != nil:
			s.log.WarnContext(ctx, "push não entregue", "inscricao_id", sub.ID, "err", err)
		}
	}
	return s.tx(ctx, d.escopo(), func(q *notificacoesdb.Queries) error {
		return q.MarcarPush(ctx, notificacoesdb.MarcarPushParams{ID: n.ID, WorkspaceID: d.WorkspaceID, UsuarioID: d.UsuarioID})
	})
}

func notificacaoDe(r notificacoesdb.Notificacao) Notificacao {
	return Notificacao{ID: r.ID, Tipo: r.Tipo, Titulo: r.Titulo, Corpo: r.Corpo, URL: r.Url, CriadoEm: r.CriadoEm, LidaEm: r.LidaEm}
}

func cortar(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max-1]) + "…"
}
