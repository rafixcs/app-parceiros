// Package assinaturas cuida do que o workspace paga: o período de teste, o
// plano (avulso por workspace, mentoria por assento), o checkout no gateway de
// cobrança e os avisos de pagamento que liberam ou suspendem o acesso.
//
// Quem paga é o dono do workspace: o afiliado avulso paga o próprio plano e o
// mentor paga pela turma, um assento por afiliado.
//
// O gateway fica atrás da interface Gateway (Asaas por padrão, com um mock
// para o ambiente local), e só este módulo fala com ele. O acesso de cada
// workspace é uma data em contas (`acesso_ate`): cada pagamento confirmado a
// estende e, passada a data, o workspace se suspende sozinho, sem depender de
// job nem de um webhook que pode se perder.
package assinaturas

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/assinaturas/assinaturasdb"
	"github.com/rafixcs/app-parceiros/backend/internal/contas"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/notificacoes"
)

const (
	// Teste é o período de avaliação de um workspace novo (a data fica em
	// contas; aqui ele só aparece na tela).
	Teste = 7 * 24 * time.Hour
	// Tolerancia é o tempo de acesso além do ciclo pago, para o pagamento
	// seguinte ter folga para cair.
	Tolerancia = 3 * 24 * time.Hour
	// Ciclo é a duração de um ciclo pago.
	Ciclo = 30 * 24 * time.Hour
	// PrimeiroVencimento é o prazo da primeira cobrança do checkout.
	PrimeiroVencimento = 3 * 24 * time.Hour

	chavePrecoAvulso  = "preco_centavos"
	chavePrecoAssento = "preco_assento_centavos"
	chaveAssentosMax  = "assentos"
	chaveAssentosTest = "assentos_teste"

	tipoNotificacao = "cobranca"
)

type Status string

const (
	// StatusSemAssinatura é o workspace que nunca assinou (está no teste ou já
	// suspenso).
	StatusSemAssinatura Status = "sem_assinatura"
	StatusAguardando    Status = "aguardando"
	StatusAtiva         Status = "ativa"
	StatusAtrasada      Status = "atrasada"
	StatusCancelada     Status = "cancelada"
)

// Assinatura é o que a tela de assinatura mostra.
type Assinatura struct {
	Plano string `json:"plano"`
	// Situacao do workspace: teste, ativo ou suspenso.
	Situacao contas.Situacao `json:"situacao"`
	// AcessoAte é até quando o workspace pode ser usado.
	AcessoAte time.Time `json:"acesso_ate"`
	Status    Status    `json:"status"`
	// Provedor do gateway, vazio sem assinatura.
	Provedor string `json:"provedor"`
	// PrecoCentavos é o preço mensal do plano: do workspace no avulso, de cada
	// assento na mentoria.
	PrecoCentavos int64 `json:"preco_centavos"`
	// Assentos contratados (0 sem assinatura); EmUso são os afiliados e os
	// convites pendentes; Maximo é o teto do plano.
	Assentos       int64 `json:"assentos"`
	AssentosEmUso  int64 `json:"assentos_em_uso"`
	AssentosMaximo int64 `json:"assentos_maximo"`
	// ValorCentavos é o total mensal da assinatura (0 sem assinatura).
	ValorCentavos int64 `json:"valor_centavos"`
	// ProximoCiclo é o vencimento da cobrança em aberto (ou da próxima).
	ProximoCiclo *string `json:"proximo_ciclo"`
	// URLPagamento é a fatura em aberto no gateway (PIX, boleto ou cartão).
	URLPagamento *string `json:"url_pagamento"`
	// Simulavel diz que o gateway é o mock do ambiente local e a tela pode
	// oferecer o botão de simular o pagamento.
	Simulavel bool `json:"simulavel"`
}

// Erro é um erro de negócio com o status HTTP e o código que a API devolve.
type Erro struct {
	Status   int
	Codigo   string
	Mensagem string
}

func (e *Erro) Error() string { return e.Codigo }

var (
	ErrSemPermissao    = &Erro{http.StatusForbidden, "sem_permissao", "Só o dono do workspace cuida da assinatura."}
	ErrSoGestor        = &Erro{http.StatusForbidden, "sem_permissao", "Só o dono e os mentores veem a assinatura."}
	ErrJaAssinada      = &Erro{http.StatusConflict, "ja_assinada", "Este workspace já tem uma assinatura. Cancele a atual antes de contratar outra."}
	ErrSemAssinatura   = &Erro{http.StatusNotFound, "sem_assinatura", "Este workspace ainda não tem assinatura."}
	ErrCPFCNPJ         = &Erro{http.StatusUnprocessableEntity, "dados_invalidos", "Informe um CPF ou CNPJ válido."}
	ErrAssentosMinimo  = &Erro{http.StatusUnprocessableEntity, "dados_invalidos", "A mentoria precisa de pelo menos um assento."}
	ErrSemPreco        = &Erro{http.StatusConflict, "sem_preco", "O preço deste plano não está configurado. Fale com o suporte."}
	ErrSemSimulacao    = &Erro{http.StatusNotFound, "nao_encontrado", "Simular pagamento só existe no ambiente local."}
	ErrGatewayIndispon = &Erro{http.StatusBadGateway, "cobranca_indisponivel", "Não conseguimos falar com o sistema de cobrança agora. Tente de novo em instantes."}

	// ErrWebhookInvalido é um aviso que não veio do gateway (ou veio quebrado).
	ErrWebhookInvalido = errors.New("webhook de cobrança inválido")
	// ErrEventoIgnorado é um aviso que não muda nada na assinatura.
	ErrEventoIgnorado = errors.New("evento de cobrança ignorado")
	// ErrAssinaturaDesconhecida é um aviso de uma assinatura que não é nossa.
	ErrAssinaturaDesconhecida = errors.New("assinatura não encontrada para o evento")
)

// Contas é o que o módulo usa de contas: o workspace, os limites do plano e o
// acesso (acesso_ate e assentos contratados).
type Contas interface {
	Workspace(ctx context.Context, m contas.Membro) (contas.Workspace, error)
	Limite(ctx context.Context, m contas.Membro, chave string) (int64, error)
	AssentosEmUso(ctx context.Context, m contas.Membro) (int64, error)
	ValidarAssentos(ctx context.Context, m contas.Membro, n int64) error
	DefinirAssentos(ctx context.Context, m contas.Membro, n int32) error
	LiberarAcesso(ctx context.Context, workspaceID uuid.UUID, ate time.Time, assentos *int32) error
	BloquearAcesso(ctx context.Context, workspaceID uuid.UUID) error
	Contato(ctx context.Context, usuarioID uuid.UUID) (contas.Contato, error)
}

// Notificador avisa o dono sobre a cobrança (notificacoes.Service). Nil não avisa.
type Notificador interface {
	Notificar(ctx context.Context, n notificacoes.Nova) error
}

type Service struct {
	pool        *pgxpool.Pool
	gw          Gateway
	contas      Contas
	notificador Notificador
	log         *slog.Logger
	agora       func() time.Time
}

// NewService monta o serviço. notificador pode ser nil.
func NewService(pool *pgxpool.Pool, gw Gateway, c Contas, n Notificador, log *slog.Logger) *Service {
	return &Service{pool: pool, gw: gw, contas: c, notificador: n, log: log, agora: time.Now}
}

func (s *Service) tx(ctx context.Context, e database.Scope, fn func(*assinaturasdb.Queries) error) error {
	return database.InTx(ctx, s.pool, e, func(tx pgx.Tx) error { return fn(assinaturasdb.New(tx)) })
}

func escopoDe(m contas.Membro) database.Scope {
	return database.Scope{UserID: m.UsuarioID.String(), WorkspaceID: m.WorkspaceID.String()}
}

// Ver devolve a assinatura e os números do plano. Dono e mentor veem.
func (s *Service) Ver(ctx context.Context, m contas.Membro) (Assinatura, error) {
	if !m.Papel.Gestor() {
		return Assinatura{}, ErrSoGestor
	}
	w, err := s.contas.Workspace(ctx, m)
	if err != nil {
		return Assinatura{}, err
	}
	out := Assinatura{
		Plano:     w.Plano,
		Situacao:  w.Status,
		AcessoAte: w.AcessoAte,
		Status:    StatusSemAssinatura,
		Simulavel: s.simulavel(),
	}
	if out.PrecoCentavos, err = s.preco(ctx, m, w); err != nil {
		return Assinatura{}, err
	}
	if w.Tipo == contas.TipoMentoria {
		if out.AssentosEmUso, err = s.contas.AssentosEmUso(ctx, m); err != nil {
			return Assinatura{}, err
		}
		if out.AssentosMaximo, err = s.contas.Limite(ctx, m, chaveAssentosMax); err != nil {
			return Assinatura{}, err
		}
	} else {
		out.AssentosMaximo = 1
	}
	if w.Assentos != nil {
		out.Assentos = int64(*w.Assentos)
	}

	a, err := s.assinatura(ctx, escopoDe(m), m.WorkspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return Assinatura{}, err
	}
	out.Status = Status(a.Status)
	out.Provedor = a.Provedor
	out.ValorCentavos = a.ValorCentavos
	if a.CanceladaEm == nil {
		out.Assentos = int64(a.Assentos)
	}
	if a.ProximoCiclo.Valid {
		d := a.ProximoCiclo.Time.Format(time.DateOnly)
		out.ProximoCiclo = &d
	}
	out.URLPagamento = a.UrlPagamento
	return out, nil
}

func (s *Service) assinatura(ctx context.Context, e database.Scope, workspaceID uuid.UUID) (assinaturasdb.Assinatura, error) {
	var a assinaturasdb.Assinatura
	err := s.tx(ctx, e, func(q *assinaturasdb.Queries) error {
		var err error
		a, err = q.Assinatura(ctx, workspaceID)
		return err
	})
	return a, err
}

// preco é o preço mensal do plano: do workspace no avulso, de cada assento na
// mentoria.
func (s *Service) preco(ctx context.Context, m contas.Membro, w contas.Workspace) (int64, error) {
	chave := chavePrecoAvulso
	if w.Tipo == contas.TipoMentoria {
		chave = chavePrecoAssento
	}
	return s.contas.Limite(ctx, m, chave)
}

// Novo é o pedido de checkout.
type Novo struct {
	// Assentos pedidos na mentoria. No avulso o campo é ignorado.
	Assentos int64
	// CPFCNPJ de quem paga, exigido pelo gateway.
	CPFCNPJ string
}

// Assinar contrata o plano e devolve a assinatura com a fatura em aberto. Só o
// dono assina. O acesso só é liberado quando o pagamento é confirmado (pelo
// webhook do gateway).
func (s *Service) Assinar(ctx context.Context, m contas.Membro, n Novo) (Assinatura, error) {
	if m.Papel != contas.PapelDono {
		return Assinatura{}, ErrSemPermissao
	}
	doc, err := CPFCNPJ(n.CPFCNPJ)
	if err != nil {
		return Assinatura{}, err
	}
	w, err := s.contas.Workspace(ctx, m)
	if err != nil {
		return Assinatura{}, err
	}
	assentos, valor, err := s.valor(ctx, m, w, n.Assentos)
	if err != nil {
		return Assinatura{}, err
	}
	// Uma assinatura em andamento não é substituída: cancelar primeiro evita
	// duas cobranças no gateway.
	if a, err := s.assinatura(ctx, escopoDe(m), m.WorkspaceID); err == nil && a.CanceladaEm == nil {
		return Assinatura{}, ErrJaAssinada
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Assinatura{}, err
	}

	contato, err := s.contas.Contato(ctx, m.UsuarioID)
	if err != nil {
		return Assinatura{}, err
	}
	vencimento := s.agora().Add(PrimeiroVencimento)
	ext, err := s.gw.Criar(ctx, NovaAssinatura{
		WorkspaceID:   m.WorkspaceID,
		Descricao:     descricao(w, assentos),
		ValorCentavos: valor,
		Vencimento:    vencimento,
		Nome:          contato.Nome,
		Email:         contato.Email,
		CPFCNPJ:       doc,
	})
	if err != nil {
		s.log.ErrorContext(ctx, "falha ao criar assinatura no gateway", "provedor", s.gw.Nome(), "err", err)
		return Assinatura{}, ErrGatewayIndispon
	}
	if ext.ProximoCiclo.IsZero() {
		ext.ProximoCiclo = vencimento
	}

	err = s.tx(ctx, escopoDe(m), func(q *assinaturasdb.Queries) error {
		_, err := q.SalvarAssinatura(ctx, assinaturasdb.SalvarAssinaturaParams{
			WorkspaceID:      m.WorkspaceID,
			Provedor:         s.gw.Nome(),
			ClienteExternoID: ext.ClienteID,
			ExternoID:        ext.ID,
			Assentos:         int32(assentos),
			ValorCentavos:    valor,
			ProximoCiclo:     pgtype.Date{Time: ext.ProximoCiclo, Valid: true},
			UrlPagamento:     texto(ext.URLPagamento),
			CriadoPor:        m.UsuarioID,
		})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Outra requisição assinou no meio do caminho: desfaz a daqui.
		if err := s.gw.Cancelar(ctx, ext.ID); err != nil {
			s.log.ErrorContext(ctx, "assinatura duplicada no gateway, cancelamento falhou",
				"provedor", s.gw.Nome(), "externo_id", ext.ID, "err", err)
		}
		return Assinatura{}, ErrJaAssinada
	}
	if err != nil {
		return Assinatura{}, err
	}
	return s.Ver(ctx, m)
}

// MudarAssentos ajusta os assentos da mentoria. Diminuir vale na hora (e nunca
// abaixo dos assentos em uso); aumentar vale quando o próximo pagamento for
// confirmado, com o novo valor já na cobrança em aberto.
func (s *Service) MudarAssentos(ctx context.Context, m contas.Membro, assentos int64) (Assinatura, error) {
	if m.Papel != contas.PapelDono {
		return Assinatura{}, ErrSemPermissao
	}
	w, err := s.contas.Workspace(ctx, m)
	if err != nil {
		return Assinatura{}, err
	}
	n, valor, err := s.valor(ctx, m, w, assentos)
	if err != nil {
		return Assinatura{}, err
	}
	a, err := s.assinatura(ctx, escopoDe(m), m.WorkspaceID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && a.CanceladaEm != nil) {
		return Assinatura{}, ErrSemAssinatura
	}
	if err != nil {
		return Assinatura{}, err
	}
	if err := s.gw.MudarValor(ctx, a.ExternoID, valor); err != nil {
		s.log.ErrorContext(ctx, "falha ao mudar o valor no gateway", "provedor", s.gw.Nome(), "err", err)
		return Assinatura{}, ErrGatewayIndispon
	}
	err = s.tx(ctx, escopoDe(m), func(q *assinaturasdb.Queries) error {
		_, err := q.AtualizarPlano(ctx, assinaturasdb.AtualizarPlanoParams{
			WorkspaceID: m.WorkspaceID, Assentos: int32(n), ValorCentavos: valor,
		})
		return err
	})
	if err != nil {
		return Assinatura{}, err
	}
	// Menos assentos valem na hora; mais assentos, só com o pagamento.
	if w.Assentos != nil && n < int64(*w.Assentos) {
		if err := s.contas.DefinirAssentos(ctx, m, int32(n)); err != nil {
			return Assinatura{}, err
		}
	}
	return s.Ver(ctx, m)
}

// Cancelar encerra a assinatura no gateway. O acesso continua até o fim do
// período já pago.
func (s *Service) Cancelar(ctx context.Context, m contas.Membro) (Assinatura, error) {
	if m.Papel != contas.PapelDono {
		return Assinatura{}, ErrSemPermissao
	}
	a, err := s.assinatura(ctx, escopoDe(m), m.WorkspaceID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && a.CanceladaEm != nil) {
		return Assinatura{}, ErrSemAssinatura
	}
	if err != nil {
		return Assinatura{}, err
	}
	if err := s.gw.Cancelar(ctx, a.ExternoID); err != nil {
		s.log.ErrorContext(ctx, "falha ao cancelar no gateway", "provedor", s.gw.Nome(), "err", err)
		return Assinatura{}, ErrGatewayIndispon
	}
	err = s.tx(ctx, escopoDe(m), func(q *assinaturasdb.Queries) error {
		_, err := q.CancelarAssinatura(ctx, m.WorkspaceID)
		return err
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Assinatura{}, err
	}
	return s.Ver(ctx, m)
}

// valor confere os assentos pedidos e devolve quantos e quanto por mês.
func (s *Service) valor(ctx context.Context, m contas.Membro, w contas.Workspace, assentos int64) (int64, int64, error) {
	preco, err := s.preco(ctx, m, w)
	if err != nil {
		return 0, 0, err
	}
	if preco <= 0 {
		return 0, 0, ErrSemPreco
	}
	if w.Tipo != contas.TipoMentoria {
		return 1, preco, nil
	}
	if assentos < 1 {
		return 0, 0, ErrAssentosMinimo
	}
	if err := s.contas.ValidarAssentos(ctx, m, assentos); err != nil {
		return 0, 0, err
	}
	return assentos, assentos * preco, nil
}

func descricao(w contas.Workspace, assentos int64) string {
	if w.Tipo == contas.TipoMentoria {
		return fmt.Sprintf("App Parceiros · Mentoria %s · %d assentos", w.Nome, assentos)
	}
	return "App Parceiros · Plano avulso"
}

// Webhook interpreta e processa um aviso do gateway.
func (s *Service) Webhook(ctx context.Context, r *http.Request) error {
	e, err := s.gw.Evento(r)
	if err != nil {
		return err
	}
	return s.Processar(ctx, e)
}

// Processar aplica um evento de cobrança: libera ou suspende o acesso e
// guarda a situação da assinatura. Reenvios do mesmo evento não fazem nada.
func (s *Service) Processar(ctx context.Context, e Evento) error {
	// O aviso não tem usuário nem workspace: a assinatura é achada pelo id
	// externo (app.assinatura_externa).
	var a assinaturasdb.Assinatura
	err := s.tx(ctx, database.Scope{ExternalSubscriptionID: e.AssinaturaExterna}, func(q *assinaturasdb.Queries) error {
		var err error
		a, err = q.AssinaturaPorExterno(ctx, assinaturasdb.AssinaturaPorExternoParams{
			Provedor: e.Provedor, ExternoID: e.AssinaturaExterna,
		})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAssinaturaDesconhecida
	}
	if err != nil {
		return err
	}

	escopo := database.Scope{WorkspaceID: a.WorkspaceID.String(), ExternalSubscriptionID: e.AssinaturaExterna}
	var novo bool
	err = s.tx(ctx, escopo, func(q *assinaturasdb.Queries) error {
		n, err := q.RegistrarEvento(ctx, assinaturasdb.RegistrarEventoParams{
			Provedor: e.Provedor, EventoID: e.ID, WorkspaceID: a.WorkspaceID, Tipo: string(e.Tipo),
		})
		novo = n > 0
		return err
	})
	if err != nil || !novo {
		return err
	}

	status := a.Status
	var proximo *time.Time
	var url *string
	switch e.Tipo {
	case EventoPago:
		status = assinaturasdb.AssinaturaStatusAtiva
		// O ciclo pago vai do vencimento ao vencimento seguinte; a tolerância
		// dá folga para o próximo pagamento cair.
		vencimento := e.Vencimento
		if vencimento.IsZero() {
			vencimento = s.agora()
		}
		fim := vencimento.Add(Ciclo)
		proximo = &fim
		assentos := a.Assentos
		if err := s.contas.LiberarAcesso(ctx, a.WorkspaceID, fim.Add(Tolerancia), &assentos); err != nil {
			return err
		}
		vazio := ""
		url = &vazio
	case EventoAtrasado:
		// O acesso cai pela data, não por este aviso.
		status = assinaturasdb.AssinaturaStatusAtrasada
		if e.URLPagamento != "" {
			url = &e.URLPagamento
		}
	case EventoEstornado:
		status = assinaturasdb.AssinaturaStatusAtrasada
		if err := s.contas.BloquearAcesso(ctx, a.WorkspaceID); err != nil {
			return err
		}
	case EventoCancelado:
		status = assinaturasdb.AssinaturaStatusCancelada
	case EventoCobranca:
		if e.URLPagamento != "" {
			url = &e.URLPagamento
		}
		if !e.Vencimento.IsZero() {
			v := e.Vencimento
			proximo = &v
		}
	}

	err = s.tx(ctx, escopo, func(q *assinaturasdb.Queries) error {
		p := assinaturasdb.AtualizarCobrancaParams{WorkspaceID: a.WorkspaceID, Status: status}
		if proximo != nil {
			p.ProximoCiclo = pgtype.Date{Time: *proximo, Valid: true}
		}
		if url != nil {
			p.UrlPagamento = url
		}
		_, err := q.AtualizarCobranca(ctx, p)
		return err
	})
	if err != nil {
		return err
	}
	s.avisar(ctx, a, e)
	return nil
}

// avisar manda ao dono a notificação da cobrança. Uma falha aqui não desfaz o
// evento: o pagamento já foi aplicado.
func (s *Service) avisar(ctx context.Context, a assinaturasdb.Assinatura, e Evento) {
	if s.notificador == nil {
		return
	}
	var titulo, corpo string
	switch e.Tipo {
	case EventoPago:
		titulo = "Pagamento confirmado"
		corpo = "A assinatura do seu workspace está ativa."
	case EventoAtrasado:
		titulo = "Pagamento em atraso"
		corpo = "A cobrança da assinatura venceu. Pague para o workspace não ser suspenso."
	case EventoEstornado:
		titulo = "Pagamento estornado"
		corpo = "O pagamento da assinatura foi estornado e o workspace está suspenso."
	default:
		return
	}
	n := notificacoes.Nova{
		WorkspaceID: a.WorkspaceID, UsuarioIDs: []uuid.UUID{a.CriadoPor}, Tipo: tipoNotificacao,
		Chave: "cobranca:" + e.ID, Titulo: titulo, Corpo: corpo,
		URL: "/w/" + a.WorkspaceID.String() + "/assinatura",
	}
	if err := s.notificador.Notificar(ctx, n); err != nil {
		s.log.ErrorContext(ctx, "falha ao avisar sobre a cobrança", "err", err)
	}
}

// SimularPagamento confirma o pagamento em aberto sem gateway de verdade. Só
// existe com o gateway mock do ambiente local.
func (s *Service) SimularPagamento(ctx context.Context, m contas.Membro) (Assinatura, error) {
	if !s.simulavel() {
		return Assinatura{}, ErrSemSimulacao
	}
	if m.Papel != contas.PapelDono {
		return Assinatura{}, ErrSemPermissao
	}
	a, err := s.assinatura(ctx, escopoDe(m), m.WorkspaceID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && a.CanceladaEm != nil) {
		return Assinatura{}, ErrSemAssinatura
	}
	if err != nil {
		return Assinatura{}, err
	}
	vencimento := s.agora()
	if a.ProximoCiclo.Valid {
		vencimento = a.ProximoCiclo.Time
	}
	err = s.Processar(ctx, Evento{
		ID: "mock_" + uuid.NewString(), Provedor: s.gw.Nome(), Tipo: EventoPago,
		AssinaturaExterna: a.ExternoID, Vencimento: vencimento,
	})
	if err != nil {
		return Assinatura{}, err
	}
	return s.Ver(ctx, m)
}

func (s *Service) simulavel() bool {
	_, ok := s.gw.(*Mock)
	return ok
}

func texto(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
