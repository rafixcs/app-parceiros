package shopee

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
)

const (
	// JanelaRelatorioMax é o maior intervalo que o conversionReport aceita.
	JanelaRelatorioMax = 90 * 24 * time.Hour
	// LimiteRelatorio é o máximo de conversões por página do conversionReport.
	LimiteRelatorio = 500
	// paginasRelatorioMax protege o job de um laço sem fim se a Shopee
	// devolver sempre hasNextPage.
	paginasRelatorioMax = 400
)

// FiltroConversoes pede uma página do conversionReport. Depois da primeira,
// passe o ScrollID devolvido: ele expira em ~30 s, então as páginas são
// pedidas em sequência, sem pausa entre elas.
type FiltroConversoes struct {
	De, Ate  time.Time
	ScrollID string
	Limite   int
}

type PaginaConversoes struct {
	Conversoes []fontes.Conversao
	ScrollID   string
	TemProxima bool
}

// Conversoes consulta uma página do conversionReport: um item por linha de
// pedido, com compra em [De, Ate).
func (c *Cliente) Conversoes(ctx context.Context, cred Credencial, f FiltroConversoes) (PaginaConversoes, error) {
	if !f.Ate.After(f.De) || f.Ate.Sub(f.De) > JanelaRelatorioMax {
		return PaginaConversoes{}, fmt.Errorf("janela do relatório deve ter até %d dias", int(JanelaRelatorioMax.Hours()/24))
	}
	if f.Limite < 1 || f.Limite > LimiteRelatorio {
		f.Limite = LimiteRelatorio
	}
	args := []string{
		"purchaseTimeStart:" + strconv.FormatInt(f.De.Unix(), 10),
		"purchaseTimeEnd:" + strconv.FormatInt(f.Ate.Unix()-1, 10),
		"limit:" + strconv.Itoa(f.Limite),
	}
	if f.ScrollID != "" {
		s, err := json.Marshal(f.ScrollID)
		if err != nil {
			return PaginaConversoes{}, err
		}
		args = append(args, "scrollId:"+string(s))
	}
	query := "{conversionReport(" + strings.Join(args, ",") + "){nodes{" + camposConversao + "} pageInfo{limit hasNextPage scrollId}}}"
	bruto, err := c.chamar(ctx, cred, query)
	if err != nil {
		return PaginaConversoes{}, err
	}
	var resp struct {
		Data struct {
			ConversionReport struct {
				Nodes    []conversao `json:"nodes"`
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					ScrollID    string `json:"scrollId"`
				} `json:"pageInfo"`
			} `json:"conversionReport"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bruto, &resp); err != nil {
		return PaginaConversoes{}, fmt.Errorf("%w: resposta do conversionReport inválida: %v", fontes.ErrIndisponivel, err)
	}
	out := PaginaConversoes{
		ScrollID:   resp.Data.ConversionReport.PageInfo.ScrollID,
		TemProxima: resp.Data.ConversionReport.PageInfo.HasNextPage,
	}
	for _, n := range resp.Data.ConversionReport.Nodes {
		cs, err := n.normalizar()
		if err != nil {
			return PaginaConversoes{}, fmt.Errorf("%w: conversão %s: %v", fontes.ErrIndisponivel, n.ConversionID, err)
		}
		out.Conversoes = append(out.Conversoes, cs...)
	}
	return out, nil
}

const camposConversao = "purchaseTime clickTime conversionId utmContent orders{orderId orderStatus items{itemId itemName shopName modelId itemPrice qty itemTotalCommission}}"

type conversao struct {
	PurchaseTime decimal `json:"purchaseTime"`
	ClickTime    decimal `json:"clickTime"`
	ConversionID decimal `json:"conversionId"`
	UtmContent   string  `json:"utmContent"`
	Orders       []struct {
		OrderID     decimal `json:"orderId"`
		OrderStatus string  `json:"orderStatus"`
		Items       []struct {
			ItemID              decimal `json:"itemId"`
			ItemName            string  `json:"itemName"`
			ShopName            string  `json:"shopName"`
			ModelID             decimal `json:"modelId"`
			ItemPrice           decimal `json:"itemPrice"`
			Qty                 decimal `json:"qty"`
			ItemTotalCommission decimal `json:"itemTotalCommission"`
		} `json:"items"`
	} `json:"orders"`
}

func (n conversao) normalizar() ([]fontes.Conversao, error) {
	id, err := n.ConversionID.escalar(0)
	if err != nil || id <= 0 {
		return nil, errors.New("conversionId inválido")
	}
	compra, err := n.PurchaseTime.escalar(0)
	if err != nil || compra <= 0 {
		return nil, errors.New("purchaseTime inválido")
	}
	var clique *time.Time
	if ts, err := n.ClickTime.escalar(0); err == nil && ts > 0 {
		t := time.Unix(ts, 0).UTC()
		clique = &t
	}
	var out []fontes.Conversao
	for _, o := range n.Orders {
		pedido := string(o.OrderID)
		if pedido == "" {
			return nil, errors.New("orderId vazio")
		}
		for _, it := range o.Items {
			c := fontes.Conversao{
				ConversaoID: id,
				PedidoID:    pedido,
				ItemNome:    strings.TrimSpace(it.ItemName),
				LojaNome:    strings.TrimSpace(it.ShopName),
				Status:      statusPedido(o.OrderStatus),
				SubID:       strings.TrimSpace(n.UtmContent),
				CompradoEm:  time.Unix(compra, 0).UTC(),
				ClicadoEm:   clique,
			}
			if c.ItemID, err = it.ItemID.escalar(0); err != nil || c.ItemID <= 0 {
				return nil, errors.New("itemId inválido")
			}
			if c.ModeloID, err = it.ModelID.escalar(0); err != nil {
				return nil, fmt.Errorf("modelId: %w", err)
			}
			if c.PrecoCentavos, err = it.ItemPrice.escalar(2); err != nil {
				return nil, fmt.Errorf("itemPrice: %w", err)
			}
			if c.ComissaoCentavos, err = it.ItemTotalCommission.escalar(2); err != nil {
				return nil, fmt.Errorf("itemTotalCommission: %w", err)
			}
			qtd, err := it.Qty.escalar(0)
			if err != nil || qtd < 0 || qtd > 1<<20 {
				return nil, errors.New("qty inválido")
			}
			c.Quantidade = int32(qtd)
			out = append(out, c)
		}
	}
	return out, nil
}

func statusPedido(s string) fontes.StatusPedido {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "UNPAID":
		return fontes.PedidoNaoPago
	case "COMPLETED":
		return fontes.PedidoConcluido
	case "CANCELLED", "CANCELED":
		return fontes.PedidoCancelado
	default:
		return fontes.PedidoPendente
	}
}

// Relatorio lê as conversões com a credencial de cada usuário
// (fontes.Relatorio).
type Relatorio struct {
	Credenciais *Credenciais
	Cliente     *Cliente
	// Limite de conversões por página; zero vale LimiteRelatorio.
	Limite int
}

var _ fontes.Relatorio = Relatorio{}

// Conversoes pagina o conversionReport em sequência, sem pausa, porque o
// scrollId expira em ~30 s. Se a Shopee recusar a credencial, marca a
// conexão como inválida ou expirada.
func (r Relatorio) Conversoes(ctx context.Context, usuarioID uuid.UUID, de, ate time.Time) ([]fontes.Conversao, error) {
	cred, err := r.Credenciais.DoUsuario(ctx, usuarioID)
	if err != nil {
		return nil, err
	}
	var out []fontes.Conversao
	f := FiltroConversoes{De: de, Ate: ate, Limite: r.Limite}
	for range paginasRelatorioMax {
		p, err := r.Cliente.Conversoes(ctx, cred, f)
		if errors.Is(err, fontes.ErrCredencialInvalida) || errors.Is(err, fontes.ErrAcessoNegado) {
			if errReg := r.Credenciais.RegistrarFalha(ctx, usuarioID, err); errReg != nil {
				return nil, errors.Join(err, errReg)
			}
		}
		if err != nil {
			return nil, err
		}
		out = append(out, p.Conversoes...)
		if !p.TemProxima || p.ScrollID == "" {
			return out, nil
		}
		f.ScrollID = p.ScrollID
	}
	return nil, fmt.Errorf("%w: conversionReport com páginas demais", fontes.ErrIndisponivel)
}
