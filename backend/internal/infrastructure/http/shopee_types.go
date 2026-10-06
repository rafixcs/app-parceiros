package http

import (
	"time"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

type connectShopeeRequest struct {
	AppID  string `json:"app_id"`
	Secret string `json:"secret"`
}

// shopeeConnectionResponse never holds the Secret; the AppID comes masked.
type shopeeConnectionResponse struct {
	Status     string     `json:"status"`
	AppID      *string    `json:"app_id"`
	VerifiedAt *time.Time `json:"verified_at"`
}

func shopeeConnectionResponseOf(c domain.ShopeeConnection) shopeeConnectionResponse {
	return shopeeConnectionResponse{Status: string(c.Status), AppID: c.AppID, VerifiedAt: c.VerifiedAt}
}
