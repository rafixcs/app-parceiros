package shopee

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
	"github.com/rafixcs/app-parceiros/backend/internal/fontes/shopee/shopeedb"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/crypto"
	"github.com/rafixcs/app-parceiros/backend/internal/platform/postgres"
)

// Status da conexão do usuário com a Shopee.
type Status string

const (
	StatusDesconectado Status = "desconectado"
	StatusConectado    Status = "conectado"
	StatusInvalido     Status = "invalido"
	StatusExpirado     Status = "expirado"
)

// Conexao é o que o usuário vê da sua credencial. Nunca inclui o Secret.
type Conexao struct {
	Status       Status     `json:"status"`
	AppID        *string    `json:"app_id"`
	VerificadoEm *time.Time `json:"verificado_em"`
}

// Validador faz a chamada de teste com uma credencial (o *Cliente).
type Validador interface {
	Validar(ctx context.Context, cred Credencial) error
}

// Erro é um erro de negócio com o status HTTP e o código que a API devolve.
type Erro struct {
	Status   int
	Codigo   string
	Mensagem string
}

func (e *Erro) Error() string { return e.Codigo }

var (
	ErrCredencialInvalida = &Erro{http.StatusUnprocessableEntity, "credencial_invalida", "A Shopee recusou o AppID ou o Secret. Confira os dados no painel de afiliados e tente de novo."}
	ErrAcessoNegado       = &Erro{http.StatusUnprocessableEntity, "credencial_invalida", "A Shopee negou acesso a esta conta de afiliado. Confira se o acesso à Open API está aprovado."}
	ErrLimiteShopee       = &Erro{http.StatusTooManyRequests, "shopee_limite", "A Shopee está limitando as chamadas agora. Tente de novo em alguns instantes."}
	ErrShopeeIndisponivel = &Erro{http.StatusBadGateway, "shopee_indisponivel", "Não conseguimos falar com a Shopee agora. Tente de novo em instantes."}
	ErrSemCredencial      = fontes.ErrSemCredencial
)

var reAppID = regexp.MustCompile(`^[0-9]{1,20}$`)

const tamanhoSecretMax = 256

// Credenciais guarda e valida a credencial da Open API de cada usuário.
type Credenciais struct {
	pool      *pgxpool.Pool
	cofre     *crypto.Cofre
	validador Validador
	agora     func() time.Time
}

func NovasCredenciais(pool *pgxpool.Pool, cofre *crypto.Cofre, v Validador) *Credenciais {
	return &Credenciais{pool: pool, cofre: cofre, validador: v, agora: time.Now}
}

func (s *Credenciais) tx(ctx context.Context, usuarioID uuid.UUID, fn func(*shopeedb.Queries) error) error {
	return postgres.InTx(ctx, s.pool, postgres.Escopo{UsuarioID: usuarioID.String()}, func(tx pgx.Tx) error {
		return fn(shopeedb.New(tx))
	})
}

// aad amarra o envelope ao usuário: um secret copiado para outra linha não abre.
func aad(usuarioID uuid.UUID) []byte { return []byte("credenciais_shopee:" + usuarioID.String()) }

// Ver devolve a situação da conexão do usuário.
func (s *Credenciais) Ver(ctx context.Context, usuarioID uuid.UUID) (Conexao, error) {
	var c shopeedb.CredenciaisShopee
	err := s.tx(ctx, usuarioID, func(q *shopeedb.Queries) error {
		var err error
		c, err = q.CredencialDoUsuario(ctx, usuarioID)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Conexao{Status: StatusDesconectado}, nil
	}
	if err != nil {
		return Conexao{}, err
	}
	return conexaoDe(c), nil
}

// Conectar valida a credencial com uma chamada de teste à Shopee e guarda o
// Secret cifrado. Substitui a credencial anterior.
func (s *Credenciais) Conectar(ctx context.Context, usuarioID uuid.UUID, appID, secret string) (Conexao, error) {
	appID = strings.TrimSpace(appID)
	secret = strings.TrimSpace(secret)
	if !reAppID.MatchString(appID) {
		return Conexao{}, &Erro{http.StatusUnprocessableEntity, "dados_invalidos", "O AppID tem só números. Copie-o do painel de afiliados da Shopee."}
	}
	if secret == "" || len(secret) > tamanhoSecretMax {
		return Conexao{}, &Erro{http.StatusUnprocessableEntity, "dados_invalidos", "Informe o Secret da Open API."}
	}

	cred := Credencial{AppID: appID, Secret: secret}
	if err := s.validador.Validar(ctx, cred); err != nil {
		return Conexao{}, traduzir(err)
	}

	env, err := s.cofre.Cifrar(ctx, []byte(secret), aad(usuarioID))
	if err != nil {
		return Conexao{}, fmt.Errorf("cifrando credencial: %w", err)
	}
	var c shopeedb.CredenciaisShopee
	err = s.tx(ctx, usuarioID, func(q *shopeedb.Queries) error {
		c, err = q.SalvarCredencial(ctx, shopeedb.SalvarCredencialParams{
			UsuarioID: usuarioID, AppID: appID, SecretCifrado: env.Cifrado, DekCifrada: env.DEKCifrada,
			KekID: env.KEKID, VerificadoEm: s.agora(),
		})
		return err
	})
	if err != nil {
		return Conexao{}, err
	}
	return conexaoDe(c), nil
}

// Desconectar apaga a credencial. Não é erro se não houver.
func (s *Credenciais) Desconectar(ctx context.Context, usuarioID uuid.UUID) error {
	return s.tx(ctx, usuarioID, func(q *shopeedb.Queries) error {
		return q.ApagarCredencial(ctx, usuarioID)
	})
}

// DoUsuario decifra a credencial do usuário para uma chamada à Shopee (ex.:
// gerar o link de afiliado). Devolve ErrSemCredencial se não houver uma
// conectada. Não guarde nem registre o resultado.
func (s *Credenciais) DoUsuario(ctx context.Context, usuarioID uuid.UUID) (Credencial, error) {
	var c shopeedb.CredenciaisShopee
	err := s.tx(ctx, usuarioID, func(q *shopeedb.Queries) error {
		var err error
		c, err = q.CredencialDoUsuario(ctx, usuarioID)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Credencial{}, ErrSemCredencial
	}
	if err != nil {
		return Credencial{}, err
	}
	if Status(c.Status) != StatusConectado {
		return Credencial{}, ErrSemCredencial
	}
	secret, err := s.cofre.Decifrar(ctx, crypto.Envelope{Cifrado: c.SecretCifrado, DEKCifrada: c.DekCifrada, KEKID: c.KekID}, aad(usuarioID))
	if err != nil {
		return Credencial{}, err
	}
	return Credencial{AppID: c.AppID, Secret: string(secret)}, nil
}

// RegistrarFalha marca a credencial como inválida ou expirada quando uma
// chamada com ela é recusada pela Shopee. Outros erros não mudam nada.
func (s *Credenciais) RegistrarFalha(ctx context.Context, usuarioID uuid.UUID, err error) error {
	var st shopeedb.CredencialStatus
	switch {
	case errors.Is(err, fontes.ErrCredencialInvalida):
		st = shopeedb.CredencialStatusInvalido
	case errors.Is(err, fontes.ErrAcessoNegado):
		st = shopeedb.CredencialStatusExpirado
	default:
		return nil
	}
	return s.tx(ctx, usuarioID, func(q *shopeedb.Queries) error {
		_, err := q.MarcarStatusCredencial(ctx, shopeedb.MarcarStatusCredencialParams{UsuarioID: usuarioID, Status: st})
		return err
	})
}

func traduzir(err error) error {
	switch {
	case errors.Is(err, fontes.ErrCredencialInvalida):
		return ErrCredencialInvalida
	case errors.Is(err, fontes.ErrAcessoNegado):
		return ErrAcessoNegado
	case errors.Is(err, fontes.ErrLimite):
		return ErrLimiteShopee
	default:
		return fmt.Errorf("%w: %v", ErrShopeeIndisponivel, err)
	}
}

func conexaoDe(c shopeedb.CredenciaisShopee) Conexao {
	app := mascarar(c.AppID)
	v := c.VerificadoEm
	return Conexao{Status: Status(c.Status), AppID: &app, VerificadoEm: &v}
}

// mascarar mostra só os 4 últimos dígitos do AppID.
func mascarar(appID string) string {
	if len(appID) <= 4 {
		return "••••"
	}
	return "••••" + appID[len(appID)-4:]
}

// Conectados lista os usuários com credencial conectada, para agendar a
// sincronização diária de conversões. Uso exclusivo do worker: lê com o papel
// dono das tabelas e devolve só os IDs.
func (s *Credenciais) Conectados(ctx context.Context) ([]uuid.UUID, error) {
	return shopeedb.New(s.pool).UsuariosConectados(ctx)
}
