package shopee

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

func TestNormalizeConversion(t *testing.T) {
	raw := `{"purchaseTime":1759600000,"clickTime":"1759590000","conversionId":"8812345","utmContent":"tiktok-w0123456789ab---",
		"orders":[{"orderId":"250930ABCD","orderStatus":"COMPLETED","items":[
			{"itemId":22000084811,"itemName":" Organizador ","shopName":"Casa","modelId":"0","itemPrice":"34.9","qty":2,"itemTotalCommission":"6.98"},
			{"itemId":"22000104100","itemName":"Luminária","shopName":"Lumi","modelId":77,"itemPrice":39.9,"qty":"1","itemTotalCommission":"0"}]},
		{"orderId":"250930EFGH","orderStatus":"CANCELLED","items":[
			{"itemId":22000117380,"itemName":"Lençol","shopName":"Sono","modelId":0,"itemPrice":"99.90","qty":1,"itemTotalCommission":"0"}]}]}`
	var n conversion
	if err := json.Unmarshal([]byte(raw), &n); err != nil {
		t.Fatal(err)
	}
	cs, err := n.normalize()
	if err != nil {
		t.Fatal(err)
	}
	clicked := time.Unix(1759590000, 0).UTC()
	want := domain.Conversion{
		ConversionID: 8812345, OrderID: "250930ABCD", ItemID: 22000084811, ItemName: "Organizador", ShopName: "Casa",
		Quantity: 2, PriceCents: 3490, CommissionCents: 698, Status: domain.OrderCompleted,
		SubID: "tiktok-w0123456789ab---", PurchasedAt: time.Unix(1759600000, 0).UTC(), ClickedAt: &clicked,
	}
	if len(cs) != 3 || !reflect.DeepEqual(cs[0], want) {
		t.Fatalf("conversions: %+v", cs)
	}
	if cs[1].ModelID != 77 || cs[1].PriceCents != 3990 || cs[2].Status != domain.OrderCancelled || cs[2].OrderID != "250930EFGH" {
		t.Fatalf("other items: %+v %+v", cs[1], cs[2])
	}
}

func TestPagedConversions(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	m := &Mock{Now: func() time.Time { return now }}
	c := NewMock(m, Config{})
	cred := Credential{AppID: "123", Secret: "x"}
	if _, err := c.GenerateLink(context.Background(), cred, "https://shopee.com.br/product/300000000/22000084811", []string{"instagram", "w0123456789ab"}); err != nil {
		t.Fatal(err)
	}
	read := func(limit int) []domain.Conversion {
		f := ConversionFilter{From: now.Add(-60 * 24 * time.Hour), To: now, Limit: limit}
		var out []domain.Conversion
		for {
			p, err := c.Conversions(context.Background(), cred, f)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, p.Conversions...)
			if !p.HasNext {
				return out
			}
			f.ScrollID = p.ScrollID
		}
	}
	all, few := read(ReportLimit), read(4)
	if len(all) < 20 || !reflect.DeepEqual(all, few) {
		t.Fatalf("pages: %d with one page, %d with many", len(all), len(few))
	}
	tagged := 0
	for _, cv := range all {
		if cv.PurchasedAt.Before(now.Add(-60*24*time.Hour)) || !cv.PurchasedAt.Before(now) {
			t.Fatalf("outside the window: %v", cv.PurchasedAt)
		}
		if cv.SubID == "instagram-w0123456789ab" {
			tagged++
		}
	}
	if tagged == 0 {
		t.Fatal("no sale through the generated link")
	}
	if _, err := c.Conversions(context.Background(), cred, ConversionFilter{From: now.Add(-91 * 24 * time.Hour), To: now}); err == nil {
		t.Fatal("accepted a 91-day window")
	}
}
