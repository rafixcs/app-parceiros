// Package contas cuida de usuários, workspaces, membros (papéis) e convites.
//
// Outros módulos usam a interface pública deste pacote: o middleware
// ExigirMembro e as funções UsuarioDoContexto e MembroDoContexto. Eles não
// leem as tabelas de contas diretamente.
package contas

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/contas/contasdb"
)

type Papel string

const (
	PapelDono     Papel = "dono"
	PapelMentor   Papel = "mentor"
	PapelAfiliado Papel = "afiliado"
)

// Gestor diz se o papel pode gerir a turma (convidar, listar e remover membros).
// O dono de uma mentoria também é mentor.
func (p Papel) Gestor() bool { return p == PapelDono || p == PapelMentor }

type TipoWorkspace string

const (
	TipoPessoal  TipoWorkspace = "pessoal"
	TipoMentoria TipoWorkspace = "mentoria"
)

type Usuario struct {
	ID       uuid.UUID `json:"id"`
	Nome     string    `json:"nome"`
	Email    string    `json:"email"`
	CriadoEm time.Time `json:"criado_em"`
}

type Workspace struct {
	ID       uuid.UUID     `json:"id"`
	Tipo     TipoWorkspace `json:"tipo"`
	Nome     string        `json:"nome"`
	FotoURL  *string       `json:"foto_url"`
	Plano    string        `json:"plano"`
	Status   string        `json:"status"`
	Papel    Papel         `json:"papel"`
	CriadoEm time.Time     `json:"criado_em"`
}

// Membro é a participação do usuário autenticado no workspace da requisição.
type Membro struct {
	WorkspaceID   uuid.UUID
	UsuarioID     uuid.UUID
	Papel         Papel
	TipoWorkspace TipoWorkspace
}

type MembroDetalhe struct {
	UsuarioID uuid.UUID `json:"usuario_id"`
	Nome      string    `json:"nome"`
	Email     string    `json:"email"`
	Papel     Papel     `json:"papel"`
	EntrouEm  time.Time `json:"entrou_em"`
}

type Convite struct {
	ID       uuid.UUID `json:"id"`
	Email    *string   `json:"email"`
	ExpiraEm time.Time `json:"expira_em"`
	CriadoEm time.Time `json:"criado_em"`
	// Token e URL só aparecem na resposta da criação; o banco guarda o hash.
	Token string `json:"token,omitempty"`
	URL   string `json:"url,omitempty"`
	// EmailEnviado só aparece na criação de um convite com e-mail.
	EmailEnviado *bool `json:"email_enviado,omitempty"`
}

// Contato é o destino das notificações de um usuário.
type Contato struct {
	Nome            string
	Email           string
	EmailVerificado bool
}

type StatusConvite string

const (
	ConviteValido   StatusConvite = "valido"
	ConviteExpirado StatusConvite = "expirado"
	ConviteUsado    StatusConvite = "usado"
	ConviteRevogado StatusConvite = "revogado"
)

// ConvitePublico é o que quem tem o link vê antes de aceitar.
type ConvitePublico struct {
	WorkspaceNome    string        `json:"workspace_nome"`
	WorkspaceFotoURL *string       `json:"workspace_foto_url"`
	Status           StatusConvite `json:"status"`
	ExpiraEm         time.Time     `json:"expira_em"`
}

// Erro é um erro de negócio com o status HTTP e o código que a API devolve.
type Erro struct {
	Status   int
	Codigo   string
	Mensagem string
}

func (e *Erro) Error() string { return e.Codigo }

var (
	ErrWorkspaceNaoEncontrado = &Erro{http.StatusNotFound, "workspace_nao_encontrado", "Workspace não encontrado."}
	ErrMembroNaoEncontrado    = &Erro{http.StatusNotFound, "membro_nao_encontrado", "Membro não encontrado."}
	ErrConviteNaoEncontrado   = &Erro{http.StatusNotFound, "convite_nao_encontrado", "Convite não encontrado."}
	ErrSemPermissao           = &Erro{http.StatusForbidden, "sem_permissao", "Você não tem permissão para esta ação."}
	ErrSoMentoria             = &Erro{http.StatusConflict, "so_mentoria", "Só é possível convidar afiliados para um workspace de mentoria."}
	ErrDonoNaoSai             = &Erro{http.StatusConflict, "dono_nao_sai", "O dono não pode sair nem ser removido do workspace."}
	ErrSemAssentos            = &Erro{http.StatusConflict, "sem_assentos", "Todos os assentos do plano estão ocupados."}
	ErrJaMembro               = &Erro{http.StatusConflict, "ja_membro", "Você já participa deste workspace."}
	ErrConviteExpirado        = &Erro{http.StatusGone, "convite_expirado", "Este convite expirou. Peça um novo ao seu mentor."}
	ErrConviteUsado           = &Erro{http.StatusGone, "convite_usado", "Este convite já foi usado. Peça um novo ao seu mentor."}
	ErrConviteRevogado        = &Erro{http.StatusGone, "convite_revogado", "Este convite foi cancelado. Peça um novo ao seu mentor."}
	ErrConviteOutroEmail      = &Erro{http.StatusForbidden, "convite_outro_email", "Este convite foi enviado para outro e-mail. Entre com a conta que recebeu o convite."}
)

func erroValidacao(msg string) *Erro {
	return &Erro{http.StatusUnprocessableEntity, "dados_invalidos", msg}
}

func workspaceDe(w contasdb.Workspace, p Papel) Workspace {
	return Workspace{
		ID:       w.ID,
		Tipo:     TipoWorkspace(w.Tipo),
		Nome:     w.Nome,
		FotoURL:  w.FotoUrl,
		Plano:    w.Plano,
		Status:   w.Status,
		Papel:    p,
		CriadoEm: w.CriadoEm,
	}
}

func conviteDe(c contasdb.Convite) Convite {
	return Convite{ID: c.ID, Email: c.Email, ExpiraEm: c.ExpiraEm, CriadoEm: c.CriadoEm}
}
