package http

import (
	"time"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

type consentRequest struct {
	SharesResults *bool `json:"shares_results"`
}

type consentResponse struct {
	SharesResults bool `json:"shares_results"`
}

// syncResponse is the state of the user's sync. FinishedAt and Conversions
// are those of the last successful one; Error is the pt-BR text of
// ErrorCode, when the last one failed.
type syncResponse struct {
	Status      string     `json:"status"`
	RequestedAt *time.Time `json:"requested_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	Conversions int32      `json:"conversions"`
	ErrorCode   *string    `json:"error_code"`
	Error       *string    `json:"error"`
}

func syncResponseOf(s domain.ConversionSync) syncResponse {
	out := syncResponse{
		Status: string(s.Status), RequestedAt: s.RequestedAt, FinishedAt: s.FinishedAt,
		Conversions: s.Conversions, ErrorCode: s.ErrorCode,
	}
	if s.ErrorCode != nil {
		msg := Message(*s.ErrorCode)
		out.Error = &msg
	}
	return out
}

type periodResponse struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type totalsResponse struct {
	Orders                   int64 `json:"orders"`
	Cancelled                int64 `json:"cancelled"`
	Items                    int64 `json:"items"`
	SalesCents               int64 `json:"sales_cents"`
	EstimatedCommissionCents int64 `json:"estimated_commission_cents"`
	ValidatedCommissionCents int64 `json:"validated_commission_cents"`
}

type dayResponse struct {
	Day                      string `json:"day"`
	Orders                   int64  `json:"orders"`
	EstimatedCommissionCents int64  `json:"estimated_commission_cents"`
	ValidatedCommissionCents int64  `json:"validated_commission_cents"`
}

type productResultResponse struct {
	ItemID                   int64      `json:"item_id"`
	ProductID                *uuid.UUID `json:"product_id"`
	Name                     string     `json:"name"`
	ShopName                 string     `json:"shop_name"`
	ImageURL                 *string    `json:"image_url"`
	Orders                   int64      `json:"orders"`
	Items                    int64      `json:"items"`
	SalesCents               int64      `json:"sales_cents"`
	EstimatedCommissionCents int64      `json:"estimated_commission_cents"`
	ValidatedCommissionCents int64      `json:"validated_commission_cents"`
}

type channelResponse struct {
	// Channel is empty when the sale came from a link made outside the app.
	Channel                  string `json:"channel"`
	Orders                   int64  `json:"orders"`
	EstimatedCommissionCents int64  `json:"estimated_commission_cents"`
	ValidatedCommissionCents int64  `json:"validated_commission_cents"`
}

type myResultsResponse struct {
	Period periodResponse `json:"period"`
	Sync   syncResponse   `json:"sync"`
	// SharesResults is null outside mentorships.
	SharesResults *bool                   `json:"shares_results"`
	Totals        totalsResponse          `json:"totals"`
	ByDay         []dayResponse           `json:"by_day"`
	ByProduct     []productResultResponse `json:"by_product"`
	ByChannel     []channelResponse       `json:"by_channel"`
}

type listResultResponse struct {
	ID                       uuid.UUID `json:"id"`
	Title                    string    `json:"title"`
	PublishedAt              time.Time `json:"published_at"`
	Importers                int       `json:"importers"`
	Orders                   int64     `json:"orders"`
	SalesCents               int64     `json:"sales_cents"`
	EstimatedCommissionCents int64     `json:"estimated_commission_cents"`
	ValidatedCommissionCents int64     `json:"validated_commission_cents"`
}

type groupResultsResponse struct {
	Period     periodResponse          `json:"period"`
	Affiliates int                     `json:"affiliates"`
	Sharing    int                     `json:"sharing"`
	Active     int64                   `json:"active"`
	Totals     totalsResponse          `json:"totals"`
	ByDay      []dayResponse           `json:"by_day"`
	ByList     []listResultResponse    `json:"by_list"`
	ByProduct  []productResultResponse `json:"by_product"`
}

func periodResponseOf(p domain.Period) periodResponse { return periodResponse{From: p.From, To: p.To} }

func totalsResponseOf(t domain.ResultTotals) totalsResponse {
	return totalsResponse{
		Orders: t.Orders, Cancelled: t.Cancelled, Items: t.Items, SalesCents: t.SalesCents,
		EstimatedCommissionCents: t.EstimatedCommissionCents, ValidatedCommissionCents: t.ValidatedCommissionCents,
	}
}

func dayResponseOf(d domain.DayResult) dayResponse {
	return dayResponse{
		Day: d.Day, Orders: d.Orders,
		EstimatedCommissionCents: d.EstimatedCommissionCents, ValidatedCommissionCents: d.ValidatedCommissionCents,
	}
}

func productResultResponseOf(p domain.ProductResult) productResultResponse {
	return productResultResponse{
		ItemID: p.ItemID, ProductID: p.ProductID, Name: p.Name, ShopName: p.ShopName, ImageURL: p.ImageURL,
		Orders: p.Orders, Items: p.Items, SalesCents: p.SalesCents,
		EstimatedCommissionCents: p.EstimatedCommissionCents, ValidatedCommissionCents: p.ValidatedCommissionCents,
	}
}

func channelResponseOf(c domain.ChannelResult) channelResponse {
	return channelResponse{
		Channel: string(c.Channel), Orders: c.Orders,
		EstimatedCommissionCents: c.EstimatedCommissionCents, ValidatedCommissionCents: c.ValidatedCommissionCents,
	}
}

func listResultResponseOf(l domain.ListResult) listResultResponse {
	return listResultResponse{
		ID: l.ID, Title: l.Title, PublishedAt: l.PublishedAt, Importers: l.Importers, Orders: l.Orders,
		SalesCents: l.SalesCents, EstimatedCommissionCents: l.EstimatedCommissionCents,
		ValidatedCommissionCents: l.ValidatedCommissionCents,
	}
}

func myResultsResponseOf(m domain.MyResults) myResultsResponse {
	return myResultsResponse{
		Period: periodResponseOf(m.Period), Sync: syncResponseOf(m.Sync), SharesResults: m.SharesResults,
		Totals: totalsResponseOf(m.Totals), ByDay: mapSlice(m.ByDay, dayResponseOf),
		ByProduct: mapSlice(m.ByProduct, productResultResponseOf), ByChannel: mapSlice(m.ByChannel, channelResponseOf),
	}
}

func groupResultsResponseOf(g domain.GroupResults) groupResultsResponse {
	return groupResultsResponse{
		Period: periodResponseOf(g.Period), Affiliates: g.Affiliates, Sharing: g.Sharing, Active: g.Active,
		Totals: totalsResponseOf(g.Totals), ByDay: mapSlice(g.ByDay, dayResponseOf),
		ByList: mapSlice(g.ByList, listResultResponseOf), ByProduct: mapSlice(g.ByProduct, productResultResponseOf),
	}
}
