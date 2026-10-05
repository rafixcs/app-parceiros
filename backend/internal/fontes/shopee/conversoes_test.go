package shopee

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/rafixcs/app-parceiros/backend/internal/fontes"
)

func TestNormalizarConversao(t *testing.T) {
	bruto := `{"purchaseTime":1759600000,"clickTime":"1759590000","conversionId":"8812345","utmContent":"tiktok-w0123456789ab---",
		"orders":[{"orderId":"250930ABCD","orderStatus":"COMPLETED","items":[
			{"itemId":22000084811,"itemName":" Organizador ","shopName":"Casa","modelId":"0","itemPrice":"34.9","qty":2,"itemTotalCommission":"6.98"},
			{"itemId":"22000104100","itemName":"Luminária","shopName":"Lumi","modelId":77,"itemPrice":39.9,"qty":"1","itemTotalCommission":"0"}]},
		{"orderId":"250930EFGH","orderStatus":"CANCELLED","items":[
			{"itemId":22000117380,"itemName":"Lençol","shopName":"Sono","modelId":0,"itemPrice":"99.90","qty":1,"itemTotalCommission":"0"}]}]}`
	var n conversao
	if err := json.Unmarshal([]byte(bruto), &n); err != nil {
		t.Fatal(err)
	}
	cs, err := n.normalizar()
	if err != nil {
		t.Fatal(err)
	}
	clique := time.Unix(1759590000, 0).UTC()
	quer := fontes.Conversao{
		ConversaoID: 8812345, PedidoID: "250930ABCD", ItemID: 22000084811, ItemNome: "Organizador", LojaNome: "Casa",
		Quantidade: 2, PrecoCentavos: 3490, ComissaoCentavos: 698, Status: fontes.PedidoConcluido,
		SubID: "tiktok-w0123456789ab---", CompradoEm: time.Unix(1759600000, 0).UTC(), ClicadoEm: &clique,
	}
	if len(cs) != 3 || !reflect.DeepEqual(cs[0], quer) {
		t.Fatalf("conversões: %+v", cs)
	}
	if cs[1].ModeloID != 77 || cs[1].PrecoCentavos != 3990 || cs[2].Status != fontes.PedidoCancelado || cs[2].PedidoID != "250930EFGH" {
		t.Fatalf("demais itens: %+v %+v", cs[1], cs[2])
	}
}

func TestConversoesPaginadas(t *testing.T) {
	agora := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	m := &Mock{Agora: func() time.Time { return agora }}
	c := NovoMock(m, Config{})
	cred := Credencial{AppID: "123", Secret: "x"}
	if _, err := c.GerarLink(context.Background(), cred, "https://shopee.com.br/product/300000000/22000084811", []string{"instagram", "w0123456789ab"}); err != nil {
		t.Fatal(err)
	}
	ler := func(limite int) []fontes.Conversao {
		f := FiltroConversoes{De: agora.Add(-60 * 24 * time.Hour), Ate: agora, Limite: limite}
		var out []fontes.Conversao
		for {
			p, err := c.Conversoes(context.Background(), cred, f)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, p.Conversoes...)
			if !p.TemProxima {
				return out
			}
			f.ScrollID = p.ScrollID
		}
	}
	todas, poucas := ler(LimiteRelatorio), ler(4)
	if len(todas) < 20 || !reflect.DeepEqual(todas, poucas) {
		t.Fatalf("páginas: %d com uma página, %d com várias", len(todas), len(poucas))
	}
	marcadas := 0
	for _, cv := range todas {
		if cv.CompradoEm.Before(agora.Add(-60*24*time.Hour)) || !cv.CompradoEm.Before(agora) {
			t.Fatalf("fora da janela: %v", cv.CompradoEm)
		}
		if cv.SubID == "instagram-w0123456789ab" {
			marcadas++
		}
	}
	if marcadas == 0 {
		t.Fatal("nenhuma venda pelo link gerado")
	}
	if _, err := c.Conversoes(context.Background(), cred, FiltroConversoes{De: agora.Add(-91 * 24 * time.Hour), Ate: agora}); err == nil {
		t.Fatal("aceitou janela de 91 dias")
	}
}
