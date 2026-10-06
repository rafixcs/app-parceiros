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

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

const (
	// MaxReportWindow is the longest range conversionReport accepts.
	MaxReportWindow = 90 * 24 * time.Hour
	// ReportLimit is the most conversions per page of conversionReport.
	ReportLimit = 500
	// maxReportPages guards the job against an endless loop if Shopee always
	// answers hasNextPage.
	maxReportPages = 400
)

// ConversionFilter asks for one page of conversionReport. After the first,
// pass the returned ScrollID: it expires in ~30 s, so the pages are asked in
// sequence, with no pause between them.
type ConversionFilter struct {
	From, To time.Time
	ScrollID string
	Limit    int
}

type ConversionPage struct {
	Conversions []domain.Conversion
	ScrollID    string
	HasNext     bool
}

// Conversions queries one page of conversionReport: one item per order line,
// purchased in [From, To).
func (c *Client) Conversions(ctx context.Context, cred Credential, f ConversionFilter) (ConversionPage, error) {
	if !f.To.After(f.From) || f.To.Sub(f.From) > MaxReportWindow {
		return ConversionPage{}, fmt.Errorf("the report window must be at most %d days", int(MaxReportWindow.Hours()/24))
	}
	if f.Limit < 1 || f.Limit > ReportLimit {
		f.Limit = ReportLimit
	}
	args := []string{
		"purchaseTimeStart:" + strconv.FormatInt(f.From.Unix(), 10),
		"purchaseTimeEnd:" + strconv.FormatInt(f.To.Unix()-1, 10),
		"limit:" + strconv.Itoa(f.Limit),
	}
	if f.ScrollID != "" {
		s, err := json.Marshal(f.ScrollID)
		if err != nil {
			return ConversionPage{}, err
		}
		args = append(args, "scrollId:"+string(s))
	}
	query := "{conversionReport(" + strings.Join(args, ",") + "){nodes{" + conversionFields + "} pageInfo{limit hasNextPage scrollId}}}"
	raw, err := c.call(ctx, cred, query)
	if err != nil {
		return ConversionPage{}, err
	}
	var resp struct {
		Data struct {
			ConversionReport struct {
				Nodes    []conversion `json:"nodes"`
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					ScrollID    string `json:"scrollId"`
				} `json:"pageInfo"`
			} `json:"conversionReport"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ConversionPage{}, fmt.Errorf("%w: invalid conversionReport answer: %v", domain.ErrSourceUnavailable, err)
	}
	out := ConversionPage{
		ScrollID: resp.Data.ConversionReport.PageInfo.ScrollID,
		HasNext:  resp.Data.ConversionReport.PageInfo.HasNextPage,
	}
	for _, n := range resp.Data.ConversionReport.Nodes {
		cs, err := n.normalize()
		if err != nil {
			return ConversionPage{}, fmt.Errorf("%w: conversion %s: %v", domain.ErrSourceUnavailable, n.ConversionID, err)
		}
		out.Conversions = append(out.Conversions, cs...)
	}
	return out, nil
}

const conversionFields = "purchaseTime clickTime conversionId utmContent orders{orderId orderStatus items{itemId itemName shopName modelId itemPrice qty itemTotalCommission}}"

type conversion struct {
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

func (n conversion) normalize() ([]domain.Conversion, error) {
	id, err := n.ConversionID.scale(0)
	if err != nil || id <= 0 {
		return nil, errors.New("invalid conversionId")
	}
	purchased, err := n.PurchaseTime.scale(0)
	if err != nil || purchased <= 0 {
		return nil, errors.New("invalid purchaseTime")
	}
	var clicked *time.Time
	if ts, err := n.ClickTime.scale(0); err == nil && ts > 0 {
		t := time.Unix(ts, 0).UTC()
		clicked = &t
	}
	var out []domain.Conversion
	for _, o := range n.Orders {
		order := string(o.OrderID)
		if order == "" {
			return nil, errors.New("empty orderId")
		}
		for _, it := range o.Items {
			c := domain.Conversion{
				ConversionID: id,
				OrderID:      order,
				ItemName:     strings.TrimSpace(it.ItemName),
				ShopName:     strings.TrimSpace(it.ShopName),
				Status:       orderStatus(o.OrderStatus),
				SubID:        strings.TrimSpace(n.UtmContent),
				PurchasedAt:  time.Unix(purchased, 0).UTC(),
				ClickedAt:    clicked,
			}
			if c.ItemID, err = it.ItemID.scale(0); err != nil || c.ItemID <= 0 {
				return nil, errors.New("invalid itemId")
			}
			if c.ModelID, err = it.ModelID.scale(0); err != nil {
				return nil, fmt.Errorf("modelId: %w", err)
			}
			if c.PriceCents, err = it.ItemPrice.scale(2); err != nil {
				return nil, fmt.Errorf("itemPrice: %w", err)
			}
			if c.CommissionCents, err = it.ItemTotalCommission.scale(2); err != nil {
				return nil, fmt.Errorf("itemTotalCommission: %w", err)
			}
			qty, err := it.Qty.scale(0)
			if err != nil || qty < 0 || qty > 1<<20 {
				return nil, errors.New("invalid qty")
			}
			c.Quantity = int32(qty)
			out = append(out, c)
		}
	}
	return out, nil
}

func orderStatus(s string) domain.OrderStatus {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "UNPAID":
		return domain.OrderUnpaid
	case "COMPLETED":
		return domain.OrderCompleted
	case "CANCELLED", "CANCELED":
		return domain.OrderCancelled
	default:
		return domain.OrderPending
	}
}

// Report reads the conversions with the credential of each user
// (domain.ConversionReport).
type Report struct {
	Credentials UserCredentials
	Client      *Client
	// Limit of conversions per page; zero is ReportLimit.
	Limit int
}

var _ domain.ConversionReport = Report{}

// Conversions pages through conversionReport in sequence, with no pause,
// because the scrollId expires in ~30 s. When Shopee refuses the credential,
// it marks the connection invalid or expired.
func (r Report) Conversions(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]domain.Conversion, error) {
	cred, err := r.Credentials.UserCredential(ctx, userID)
	if err != nil {
		return nil, err
	}
	var out []domain.Conversion
	f := ConversionFilter{From: from, To: to, Limit: r.Limit}
	for range maxReportPages {
		p, err := r.Client.Conversions(ctx, cred, f)
		if err != nil {
			return nil, recordFailure(ctx, r.Credentials, userID, err)
		}
		out = append(out, p.Conversions...)
		if !p.HasNext || p.ScrollID == "" {
			return out, nil
		}
		f.ScrollID = p.ScrollID
	}
	return nil, fmt.Errorf("%w: conversionReport with too many pages", domain.ErrSourceUnavailable)
}
