package contas

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/contas/contasdb"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/auth"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/postgres"
)

const (
	nomePessoal       = "Pessoal"
	planoAvulso       = "avulso"
	planoMentoria     = "mentoria"
	limiteAssentos    = "assentos"
	limiteTeste       = "assentos_teste"
	ValidadePadrao    = 7 * 24 * time.Hour
	ValidadeMaxima    = 30 * 24 * time.Hour
	tamanhoNomeMax    = 80
	tamanhoFotoURL    = 2048
	bytesTokenConvite = 32
)

type Service struct {
	pool   *pgxpool.Pool
	perfil func(context.Context, auth.Identidade) (auth.Perfil, error)
	appURL string
	// enviarConvite manda o e-mail do convite (módulo notificacoes). Nil não envia.
	enviarConvite func(context.Context, EnvioConvite) error
}

// EnvioConvite é o que o e-mail de um convite precisa.
type EnvioConvite struct {
	Email         string
	WorkspaceNome string
	URL           string
	ExpiraEm      time.Time
}

// EnviarConvitesCom liga o envio do e-mail de convite. Sem ele, a API só
// devolve o link.
func (s *Service) EnviarConvitesCom(fn func(context.Context, EnvioConvite) error) {
	s.enviarConvite = fn
}

// Contato devolve nome e e-mail de um usuário, para as notificações.
func (s *Service) Contato(ctx context.Context, usuarioID uuid.UUID) (Contato, error) {
	var u contasdb.Usuario
	err := s.tx(ctx, postgres.Escopo{UsuarioID: usuarioID.String()}, func(q *contasdb.Queries, _ pgx.Tx) error {
		var err error
		u, err = q.UsuarioPorID(ctx, usuarioID)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Contato{}, ErrMembroNaoEncontrado
	}
	return Contato{Nome: u.Nome, Email: u.Email, EmailVerificado: u.EmailVerificado}, err
}

// NewService cria o serviço. appURL é a base dos links de convite
// (ex.: https://app.exemplo.com.br). O worker, que só lê contatos, passa v nil.
func NewService(pool *pgxpool.Pool, v auth.Verificador, appURL string) *Service {
	s := &Service{pool: pool, appURL: strings.TrimRight(appURL, "/")}
	if v != nil {
		s.perfil = v.Perfil
	}
	return s
}

func (s *Service) tx(ctx context.Context, e postgres.Escopo, fn func(*contasdb.Queries, pgx.Tx) error) error {
	return postgres.InTx(ctx, s.pool, e, func(tx pgx.Tx) error {
		return fn(contasdb.New(tx), tx)
	})
}

// Entrar devolve o usuário da identidade. No primeiro acesso, cria o usuário
// e o seu workspace pessoal.
func (s *Service) Entrar(ctx context.Context, id auth.Identidade) (Usuario, error) {
	var u contasdb.Usuario
	err := s.tx(ctx, postgres.Escopo{ZitadelSub: id.Sub}, func(q *contasdb.Queries, _ pgx.Tx) error {
		var err error
		u, err = q.UsuarioPorSub(ctx, id.Sub)
		return err
	})
	if err == nil {
		return usuarioDe(u), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Usuario{}, err
	}

	perfil, err := s.perfil(ctx, id)
	if err != nil {
		return Usuario{}, fmt.Errorf("buscando perfil no provedor: %w", err)
	}
	nome := strings.TrimSpace(perfil.Nome)
	if nome == "" {
		nome, _, _ = strings.Cut(perfil.Email, "@")
	}

	err = s.tx(ctx, postgres.Escopo{ZitadelSub: id.Sub}, func(q *contasdb.Queries, tx pgx.Tx) error {
		r, err := q.UpsertUsuario(ctx, contasdb.UpsertUsuarioParams{
			ZitadelSub:      id.Sub,
			Nome:            nome,
			Email:           strings.ToLower(strings.TrimSpace(perfil.Email)),
			EmailVerificado: perfil.EmailVerificado,
		})
		if err != nil {
			return err
		}
		if r.Criado {
			if err := postgres.SetEscopo(ctx, tx, postgres.Escopo{UsuarioID: r.ID.String(), ZitadelSub: id.Sub}); err != nil {
				return err
			}
			if _, err := criarWorkspace(ctx, q, tx, r.ID, TipoPessoal, nomePessoal, nil); err != nil {
				return err
			}
		}
		u, err = q.UsuarioPorID(ctx, r.ID)
		return err
	})
	if err != nil {
		return Usuario{}, err
	}
	return usuarioDe(u), nil
}

// criarWorkspace insere o workspace e o dono como membro. O escopo da
// transação precisa ter o usuário dono.
func criarWorkspace(ctx context.Context, q *contasdb.Queries, tx pgx.Tx, dono uuid.UUID, tipo TipoWorkspace, nome string, foto *string) (contasdb.Workspace, error) {
	plano := planoAvulso
	if tipo == TipoMentoria {
		plano = planoMentoria
	}
	w, err := q.CriarWorkspace(ctx, contasdb.CriarWorkspaceParams{
		Tipo:    contasdb.WorkspaceTipo(tipo),
		Nome:    nome,
		FotoUrl: foto,
		DonoID:  dono,
		Plano:   plano,
	})
	if err != nil {
		return w, err
	}
	if err := postgres.SetEscopo(ctx, tx, postgres.Escopo{UsuarioID: dono.String(), WorkspaceID: w.ID.String()}); err != nil {
		return w, err
	}
	err = q.InserirMembro(ctx, contasdb.InserirMembroParams{
		WorkspaceID: w.ID, UsuarioID: dono, Papel: contasdb.MembroPapelDono,
	})
	return w, err
}

// Workspaces lista os workspaces de que o usuário participa (seletor do topo).
func (s *Service) Workspaces(ctx context.Context, usuarioID uuid.UUID) ([]Workspace, error) {
	var out []Workspace
	err := s.tx(ctx, postgres.Escopo{UsuarioID: usuarioID.String()}, func(q *contasdb.Queries, _ pgx.Tx) error {
		rows, err := q.WorkspacesDoUsuario(ctx, usuarioID)
		if err != nil {
			return err
		}
		aluno, err := q.AlunoDeMentoriaAtiva(ctx, usuarioID)
		if err != nil {
			return err
		}
		out = make([]Workspace, 0, len(rows))
		for _, r := range rows {
			out = append(out, workspaceDe(contasdb.Workspace{
				ID: r.ID, Tipo: r.Tipo, Nome: r.Nome, FotoUrl: r.FotoUrl, DonoID: r.DonoID,
				Plano: r.Plano, CriadoEm: r.CriadoEm, AcessoAte: r.AcessoAte, PagoEm: r.PagoEm, Assentos: r.Assentos,
			}, Papel(r.Papel), aluno))
		}
		return nil
	})
	return out, err
}

// CriarMentoria cria um workspace de mentoria com o usuário como dono.
func (s *Service) CriarMentoria(ctx context.Context, usuarioID uuid.UUID, nome string, foto *string) (Workspace, error) {
	nome, err := validarNome(nome)
	if err != nil {
		return Workspace{}, err
	}
	if foto, err = validarFoto(foto); err != nil {
		return Workspace{}, err
	}
	var w contasdb.Workspace
	err = s.tx(ctx, postgres.Escopo{UsuarioID: usuarioID.String()}, func(q *contasdb.Queries, tx pgx.Tx) error {
		w, err = criarWorkspace(ctx, q, tx, usuarioID, TipoMentoria, nome, foto)
		return err
	})
	if err != nil {
		return Workspace{}, err
	}
	return workspaceDe(w, PapelDono, false), nil
}

// alunoSe diz se o workspace é o pessoal de um aluno de mentoria em dia (que
// não é cobrado). Só consulta no workspace pessoal.
func alunoSe(ctx context.Context, q *contasdb.Queries, w contasdb.Workspace, usuarioID uuid.UUID) (bool, error) {
	if TipoWorkspace(w.Tipo) != TipoPessoal {
		return false, nil
	}
	return q.AlunoDeMentoriaAtiva(ctx, usuarioID)
}

// Membro devolve a participação do usuário no workspace, ou
// ErrWorkspaceNaoEncontrado se ele não participa (sem revelar se o workspace
// existe).
func (s *Service) Membro(ctx context.Context, usuarioID, workspaceID uuid.UUID) (Membro, error) {
	var m Membro
	err := s.tx(ctx, postgres.Escopo{UsuarioID: usuarioID.String()}, func(q *contasdb.Queries, _ pgx.Tx) error {
		mb, err := q.Membro(ctx, contasdb.MembroParams{WorkspaceID: workspaceID, UsuarioID: usuarioID})
		if err != nil {
			return err
		}
		w, err := q.Workspace(ctx, workspaceID)
		if err != nil {
			return err
		}
		aluno, err := alunoSe(ctx, q, w, usuarioID)
		if err != nil {
			return err
		}
		m = Membro{
			WorkspaceID: workspaceID, UsuarioID: usuarioID, Papel: Papel(mb.Papel),
			TipoWorkspace: TipoWorkspace(w.Tipo), ConsenteResultados: mb.ConsenteResultados,
			Suspenso: situacaoDe(w, time.Now(), aluno) == SituacaoSuspenso,
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Membro{}, ErrWorkspaceNaoEncontrado
	}
	return m, err
}

func escopoDe(m Membro) postgres.Escopo {
	return postgres.Escopo{UsuarioID: m.UsuarioID.String(), WorkspaceID: m.WorkspaceID.String()}
}

func (s *Service) Workspace(ctx context.Context, m Membro) (Workspace, error) {
	var w contasdb.Workspace
	var aluno bool
	err := s.tx(ctx, escopoDe(m), func(q *contasdb.Queries, _ pgx.Tx) error {
		var err error
		if w, err = q.Workspace(ctx, m.WorkspaceID); err != nil {
			return err
		}
		aluno, err = alunoSe(ctx, q, w, m.UsuarioID)
		return err
	})
	if err != nil {
		return Workspace{}, err
	}
	return workspaceDe(w, m.Papel, aluno), nil
}

// AtualizarWorkspace muda nome e foto. Só o dono pode. Foto vazia remove a foto.
func (s *Service) AtualizarWorkspace(ctx context.Context, m Membro, nome, foto *string) (Workspace, error) {
	if m.Papel != PapelDono {
		return Workspace{}, ErrSemPermissao
	}
	p := contasdb.AtualizarWorkspaceParams{ID: m.WorkspaceID}
	if nome != nil {
		n, err := validarNome(*nome)
		if err != nil {
			return Workspace{}, err
		}
		p.Nome = &n
	}
	if foto != nil {
		if strings.TrimSpace(*foto) == "" {
			p.LimparFoto = true
		} else {
			f, err := validarFoto(foto)
			if err != nil {
				return Workspace{}, err
			}
			p.FotoUrl = f
		}
	}
	var w contasdb.Workspace
	var aluno bool
	err := s.tx(ctx, escopoDe(m), func(q *contasdb.Queries, _ pgx.Tx) error {
		var err error
		if w, err = q.AtualizarWorkspace(ctx, p); err != nil {
			return err
		}
		aluno, err = alunoSe(ctx, q, w, m.UsuarioID)
		return err
	})
	if err != nil {
		return Workspace{}, err
	}
	return workspaceDe(w, m.Papel, aluno), nil
}

// Membros lista a turma. Só dono e mentor veem.
func (s *Service) Membros(ctx context.Context, m Membro) ([]MembroDetalhe, error) {
	if !m.Papel.Gestor() {
		return nil, ErrSemPermissao
	}
	var out []MembroDetalhe
	err := s.tx(ctx, escopoDe(m), func(q *contasdb.Queries, _ pgx.Tx) error {
		rows, err := q.MembrosDoWorkspace(ctx, m.WorkspaceID)
		if err != nil {
			return err
		}
		out = make([]MembroDetalhe, 0, len(rows))
		for _, r := range rows {
			out = append(out, MembroDetalhe{
				UsuarioID: r.UsuarioID, Nome: r.Nome, Email: r.Email, Papel: Papel(r.Papel), EntrouEm: r.EntrouEm,
				ConsenteResultados: r.ConsenteResultados,
			})
		}
		return nil
	})
	return out, err
}

// DefinirConsentimento grava se o usuário autoriza dono e mentores a ver os
// seus resultados agregados no workspace. Só existe em mentorias; vale a
// partir da próxima consulta do painel.
func (s *Service) DefinirConsentimento(ctx context.Context, m Membro, consente bool) error {
	if m.TipoWorkspace != TipoMentoria {
		return ErrConsentimentoSoMentoria
	}
	return s.tx(ctx, escopoDe(m), func(q *contasdb.Queries, _ pgx.Tx) error {
		n, err := q.DefinirConsentimento(ctx, contasdb.DefinirConsentimentoParams{
			Consente: consente, WorkspaceID: m.WorkspaceID, UsuarioID: m.UsuarioID,
		})
		if err == nil && n == 0 {
			return ErrMembroNaoEncontrado
		}
		return err
	})
}

// RemoverMembro tira alguém do workspace. Qualquer membro pode sair (alvo =
// ele mesmo), exceto o dono. Dono e mentor removem afiliados; só o dono remove
// mentores. Os dados pessoais do afiliado (coleção, credencial) não são
// apagados: continuam dele.
func (s *Service) RemoverMembro(ctx context.Context, m Membro, alvo uuid.UUID) error {
	return s.tx(ctx, escopoDe(m), func(q *contasdb.Queries, _ pgx.Tx) error {
		t, err := q.Membro(ctx, contasdb.MembroParams{WorkspaceID: m.WorkspaceID, UsuarioID: alvo})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMembroNaoEncontrado
		}
		if err != nil {
			return err
		}
		switch {
		case t.Papel == contasdb.MembroPapelDono:
			return ErrDonoNaoSai
		case alvo == m.UsuarioID:
		case m.Papel == PapelDono:
		case m.Papel == PapelMentor && t.Papel == contasdb.MembroPapelAfiliado:
		default:
			return ErrSemPermissao
		}
		_, err = q.RemoverMembro(ctx, contasdb.RemoverMembroParams{WorkspaceID: m.WorkspaceID, UsuarioID: alvo})
		return err
	})
}

// CriarConvite gera um convite de uso único para um afiliado. Com e-mail, só
// quem entrar com aquele e-mail (verificado) pode aceitar, e o link também vai
// por e-mail quando o envio está ligado (EnviarConvitesCom).
func (s *Service) CriarConvite(ctx context.Context, m Membro, email *string, validade time.Duration) (Convite, error) {
	if !m.Papel.Gestor() {
		return Convite{}, ErrSemPermissao
	}
	if m.TipoWorkspace != TipoMentoria {
		return Convite{}, ErrSoMentoria
	}
	if validade == 0 {
		validade = ValidadePadrao
	}
	if validade < time.Hour || validade > ValidadeMaxima {
		return Convite{}, erroValidacao("A validade do convite deve ficar entre 1 hora e 30 dias.")
	}
	if email != nil {
		e, err := validarEmail(*email)
		if err != nil {
			return Convite{}, err
		}
		email = e
	}

	token, hash, err := novoToken()
	if err != nil {
		return Convite{}, err
	}

	var c contasdb.Convite
	var w contasdb.Workspace
	err = s.tx(ctx, escopoDe(m), func(q *contasdb.Queries, _ pgx.Tx) error {
		var err error
		w, err = q.TravarWorkspace(ctx, m.WorkspaceID)
		if err != nil {
			return err
		}
		limite, err := assentosDe(ctx, q, w)
		if err != nil {
			return err
		}
		emUso, err := assentosEmUso(ctx, q, m.WorkspaceID)
		if err != nil {
			return err
		}
		if emUso >= limite {
			return ErrSemAssentos
		}
		c, err = q.CriarConvite(ctx, contasdb.CriarConviteParams{
			WorkspaceID: m.WorkspaceID,
			Email:       email,
			TokenHash:   hash,
			ExpiraEm:    time.Now().Add(validade),
			CriadoPor:   m.UsuarioID,
		})
		return err
	})
	if err != nil {
		return Convite{}, err
	}
	out := conviteDe(c)
	out.Token = token
	out.URL = s.appURL + "/convite/" + token
	if email != nil && s.enviarConvite != nil {
		// O convite já vale: se o e-mail falhar, o mentor ainda tem o link.
		enviado := s.enviarConvite(ctx, EnvioConvite{Email: *email, WorkspaceNome: w.Nome, URL: out.URL, ExpiraEm: c.ExpiraEm}) == nil
		out.EmailEnviado = &enviado
	}
	return out, nil
}

// Limite devolve um limite do plano do workspace (ex.: "listas"). Sem valor
// na tabela, o limite é zero.
func (s *Service) Limite(ctx context.Context, m Membro, chave string) (int64, error) {
	var v int64
	err := s.tx(ctx, escopoDe(m), func(q *contasdb.Queries, _ pgx.Tx) error {
		w, err := q.Workspace(ctx, m.WorkspaceID)
		if err != nil {
			return err
		}
		v, err = q.Limite(ctx, contasdb.LimiteParams{Plano: w.Plano, Chave: chave})
		if errors.Is(err, pgx.ErrNoRows) {
			v, err = 0, nil
		}
		return err
	})
	return v, err
}

func limiteDoPlano(ctx context.Context, q *contasdb.Queries, plano, chave string) (int64, error) {
	v, err := q.Limite(ctx, contasdb.LimiteParams{Plano: plano, Chave: chave})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return v, err
}

// assentosDe devolve os assentos do workspace: os contratados ou, sem
// pagamento, o limite de teste do plano.
func assentosDe(ctx context.Context, q *contasdb.Queries, w contasdb.Workspace) (int64, error) {
	if w.Assentos != nil {
		return int64(*w.Assentos), nil
	}
	return limiteDoPlano(ctx, q, w.Plano, limiteTeste)
}

// assentosEmUso conta os afiliados e os convites pendentes, que já reservam
// um assento.
func assentosEmUso(ctx context.Context, q *contasdb.Queries, workspaceID uuid.UUID) (int64, error) {
	afiliados, err := q.ContarAfiliados(ctx, workspaceID)
	if err != nil {
		return 0, err
	}
	pendentes, err := q.ContarConvitesPendentes(ctx, workspaceID)
	return afiliados + pendentes, err
}

// AssentosEmUso conta os afiliados e os convites pendentes do workspace.
func (s *Service) AssentosEmUso(ctx context.Context, m Membro) (int64, error) {
	var n int64
	err := s.tx(ctx, escopoDe(m), func(q *contasdb.Queries, _ pgx.Tx) error {
		var err error
		n, err = assentosEmUso(ctx, q, m.WorkspaceID)
		return err
	})
	return n, err
}

// DefinirAssentos muda os assentos contratados da mentoria (módulo
// assinaturas). Não deixa ficar abaixo dos em uso nem acima do máximo do
// plano. Só o dono pode.
func (s *Service) DefinirAssentos(ctx context.Context, m Membro, n int32) error {
	if m.Papel != PapelDono {
		return ErrSemPermissao
	}
	return s.tx(ctx, escopoDe(m), func(q *contasdb.Queries, _ pgx.Tx) error {
		w, err := q.TravarWorkspace(ctx, m.WorkspaceID)
		if err != nil {
			return err
		}
		if err := validarAssentos(ctx, q, w, int64(n)); err != nil {
			return err
		}
		return q.DefinirAssentos(ctx, contasdb.DefinirAssentosParams{ID: m.WorkspaceID, Assentos: &n})
	})
}

// ValidarAssentos confere se a mentoria pode contratar n assentos: não menos
// que os em uso nem mais que o máximo do plano.
func (s *Service) ValidarAssentos(ctx context.Context, m Membro, n int64) error {
	return s.tx(ctx, escopoDe(m), func(q *contasdb.Queries, _ pgx.Tx) error {
		w, err := q.Workspace(ctx, m.WorkspaceID)
		if err != nil {
			return err
		}
		return validarAssentos(ctx, q, w, n)
	})
}

func validarAssentos(ctx context.Context, q *contasdb.Queries, w contasdb.Workspace, n int64) error {
	maximo, err := limiteDoPlano(ctx, q, w.Plano, limiteAssentos)
	if err != nil {
		return err
	}
	if n > maximo {
		return ErrAssentosAcimaDoPlano
	}
	emUso, err := assentosEmUso(ctx, q, w.ID)
	if err != nil {
		return err
	}
	if n < emUso {
		return ErrAssentosEmUso
	}
	return nil
}

// LiberarAcesso registra um pagamento: estende o acesso do workspace até
// `ate` (nunca encurta) e, se informado, fixa os assentos contratados. Chamado
// pelo webhook de cobrança, que não tem usuário; é idempotente.
func (s *Service) LiberarAcesso(ctx context.Context, workspaceID uuid.UUID, ate time.Time, assentos *int32) error {
	return s.tx(ctx, postgres.Escopo{WorkspaceID: workspaceID.String()}, func(q *contasdb.Queries, _ pgx.Tx) error {
		_, err := q.LiberarAcesso(ctx, contasdb.LiberarAcessoParams{ID: workspaceID, Ate: ate, Assentos: assentos})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrWorkspaceNaoEncontrado
		}
		return err
	})
}

// BloquearAcesso suspende o workspace agora (estorno ou contestação de um
// pagamento). É idempotente.
func (s *Service) BloquearAcesso(ctx context.Context, workspaceID uuid.UUID) error {
	return s.tx(ctx, postgres.Escopo{WorkspaceID: workspaceID.String()}, func(q *contasdb.Queries, _ pgx.Tx) error {
		_, err := q.BloquearAcesso(ctx, workspaceID)
		return err
	})
}

// Convites lista os convites pendentes (sem o token).
func (s *Service) Convites(ctx context.Context, m Membro) ([]Convite, error) {
	if !m.Papel.Gestor() {
		return nil, ErrSemPermissao
	}
	var out []Convite
	err := s.tx(ctx, escopoDe(m), func(q *contasdb.Queries, _ pgx.Tx) error {
		rows, err := q.ConvitesPendentes(ctx, m.WorkspaceID)
		if err != nil {
			return err
		}
		out = make([]Convite, 0, len(rows))
		for _, r := range rows {
			out = append(out, conviteDe(r))
		}
		return nil
	})
	return out, err
}

// RevogarConvite cancela um convite pendente.
func (s *Service) RevogarConvite(ctx context.Context, m Membro, id uuid.UUID) error {
	if !m.Papel.Gestor() {
		return ErrSemPermissao
	}
	return s.tx(ctx, escopoDe(m), func(q *contasdb.Queries, _ pgx.Tx) error {
		n, err := q.RevogarConvite(ctx, contasdb.RevogarConviteParams{ID: id, WorkspaceID: m.WorkspaceID})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrConviteNaoEncontrado
		}
		return nil
	})
}

// VerConvite mostra o workspace e a situação do convite para quem tem o link.
// Não exige login.
func (s *Service) VerConvite(ctx context.Context, token string) (ConvitePublico, error) {
	hash, ok := hashToken(token)
	if !ok {
		return ConvitePublico{}, ErrConviteNaoEncontrado
	}
	var out ConvitePublico
	err := s.tx(ctx, postgres.Escopo{ConviteHash: hex.EncodeToString(hash)}, func(q *contasdb.Queries, _ pgx.Tx) error {
		c, err := q.ConvitePorHash(ctx, hash)
		if err != nil {
			return err
		}
		w, err := q.Workspace(ctx, c.WorkspaceID)
		if err != nil {
			return err
		}
		out = ConvitePublico{WorkspaceNome: w.Nome, WorkspaceFotoURL: w.FotoUrl, Status: statusDe(c), ExpiraEm: c.ExpiraEm}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ConvitePublico{}, ErrConviteNaoEncontrado
	}
	return out, err
}

// AceitarConvite torna o usuário afiliado do workspace do convite.
func (s *Service) AceitarConvite(ctx context.Context, usuarioID uuid.UUID, token string) (Workspace, error) {
	hash, ok := hashToken(token)
	if !ok {
		return Workspace{}, ErrConviteNaoEncontrado
	}
	escopo := postgres.Escopo{UsuarioID: usuarioID.String(), ConviteHash: hex.EncodeToString(hash)}
	var w contasdb.Workspace
	err := s.tx(ctx, escopo, func(q *contasdb.Queries, tx pgx.Tx) error {
		c, err := q.ConvitePorHash(ctx, hash)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrConviteNaoEncontrado
		}
		if err != nil {
			return err
		}
		// A partir daqui a transação opera no workspace do convite.
		escopo.WorkspaceID = c.WorkspaceID.String()
		if err := postgres.SetEscopo(ctx, tx, escopo); err != nil {
			return err
		}
		if c, err = q.TravarConvitePorHash(ctx, hash); err != nil {
			return err
		}
		switch statusDe(c) {
		case ConviteRevogado:
			return ErrConviteRevogado
		case ConviteUsado:
			return ErrConviteUsado
		case ConviteExpirado:
			return ErrConviteExpirado
		}

		if _, err := q.Membro(ctx, contasdb.MembroParams{WorkspaceID: c.WorkspaceID, UsuarioID: usuarioID}); err == nil {
			return ErrJaMembro
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		if c.Email != nil {
			u, err := q.UsuarioPorID(ctx, usuarioID)
			if err != nil {
				return err
			}
			if !u.EmailVerificado || !strings.EqualFold(u.Email, *c.Email) {
				return ErrConviteOutroEmail
			}
		}

		w, err = q.TravarWorkspace(ctx, c.WorkspaceID)
		if err != nil {
			return err
		}
		limite, err := assentosDe(ctx, q, w)
		if err != nil {
			return err
		}
		afiliados, err := q.ContarAfiliados(ctx, c.WorkspaceID)
		if err != nil {
			return err
		}
		if afiliados >= limite {
			return ErrSemAssentos
		}

		if err := q.InserirMembro(ctx, contasdb.InserirMembroParams{
			WorkspaceID: c.WorkspaceID, UsuarioID: usuarioID, Papel: contasdb.MembroPapelAfiliado,
		}); err != nil {
			return err
		}
		return q.MarcarConviteUsado(ctx, contasdb.MarcarConviteUsadoParams{ID: c.ID, UsadoPor: &usuarioID})
	})
	if err != nil {
		return Workspace{}, err
	}
	return workspaceDe(w, PapelAfiliado, false), nil
}

func statusDe(c contasdb.Convite) StatusConvite {
	switch {
	case c.RevogadoEm != nil:
		return ConviteRevogado
	case c.UsadoPor != nil:
		return ConviteUsado
	case !time.Now().Before(c.ExpiraEm):
		return ConviteExpirado
	default:
		return ConviteValido
	}
}

func novoToken() (string, []byte, error) {
	b := make([]byte, bytesTokenConvite)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(token))
	return token, h[:], nil
}

// hashToken devolve o hash de um token com formato válido.
func hashToken(token string) ([]byte, bool) {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(b) != bytesTokenConvite {
		return nil, false
	}
	h := sha256.Sum256([]byte(token))
	return h[:], true
}

func validarNome(nome string) (string, error) {
	nome = strings.TrimSpace(nome)
	if nome == "" || utf8.RuneCountInString(nome) > tamanhoNomeMax {
		return "", erroValidacao(fmt.Sprintf("O nome deve ter entre 1 e %d caracteres.", tamanhoNomeMax))
	}
	return nome, nil
}

func validarFoto(foto *string) (*string, error) {
	if foto == nil {
		return nil, nil
	}
	f := strings.TrimSpace(*foto)
	if f == "" {
		return nil, nil
	}
	u, err := url.Parse(f)
	if err != nil || u.Scheme != "https" || u.Host == "" || len(f) > tamanhoFotoURL {
		return nil, erroValidacao("A foto deve ser uma URL https válida.")
	}
	return &f, nil
}

func validarEmail(email string) (*string, error) {
	e := strings.ToLower(strings.TrimSpace(email))
	if e == "" {
		return nil, nil
	}
	a, err := mail.ParseAddress(e)
	if err != nil || a.Address != e {
		return nil, erroValidacao("E-mail inválido.")
	}
	return &e, nil
}

func usuarioDe(u contasdb.Usuario) Usuario {
	return Usuario{ID: u.ID, Nome: u.Nome, Email: u.Email, CriadoEm: u.CriadoEm}
}
