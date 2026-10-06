package http

import (
	"time"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

type listResponse struct {
	ID          uuid.UUID  `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	PublishedAt *time.Time `json:"published_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Products    int64      `json:"products"`
	// Importers only shows to owner and mentors.
	Importers    *int64 `json:"importers,omitempty"`
	ImportedByMe bool   `json:"imported_by_me"`
}

func listResponseOf(l domain.CuratedList) listResponse {
	return listResponse{
		ID: l.ID, Title: l.Title, Description: l.Description, PublishedAt: l.PublishedAt, CreatedAt: l.CreatedAt,
		UpdatedAt: l.UpdatedAt, Products: l.Products, Importers: l.Importers, ImportedByMe: l.ImportedByMe,
	}
}

// myItemResponse is the product already saved in the viewer's collection,
// with their links.
type myItemResponse struct {
	ID            uuid.UUID             `json:"id"`
	Title         string                `json:"title"`
	Description   string                `json:"description"`
	AffiliateLink *string               `json:"affiliate_link"`
	LinkOrigin    domain.LinkOrigin     `json:"link_origin"`
	LinkStatus    domain.LinkStatus     `json:"link_status"`
	Links         []channelLinkResponse `json:"links"`
}

type listItemResponse struct {
	Product  itemProductResponse `json:"product"`
	Comment  string              `json:"comment"`
	Position int32               `json:"position"`
	// Importers only shows to owner and mentors.
	Importers *int64          `json:"importers,omitempty"`
	MyItem    *myItemResponse `json:"my_item"`
	Videos    []videoResponse `json:"videos"`
}

type listDetailResponse struct {
	listResponse
	Items  []listItemResponse `json:"items"`
	Videos []videoResponse    `json:"videos"`
}

func listDetailResponseOf(l domain.CuratedListDetail) listDetailResponse {
	items := mapSlice(l.Items, func(it domain.ListItemView) listItemResponse {
		out := listItemResponse{
			Product: itemProductResponseOf(it.Product), Comment: it.Comment, Position: it.Position,
			Importers: it.Importers, Videos: mapSlice(it.Videos, videoResponseOf),
		}
		if my := it.MyItem; my != nil {
			out.MyItem = &myItemResponse{
				ID: my.ID, Title: my.Title, Description: my.Description, AffiliateLink: my.AffiliateLink,
				LinkOrigin: my.LinkOrigin, LinkStatus: my.LinkStatus,
				Links: mapSlice(my.Links, func(l domain.ChannelLink) channelLinkResponse {
					return channelLinkResponse{Channel: l.Channel, SubID: l.SubID, URL: l.URL}
				}),
			}
		}
		return out
	})
	return listDetailResponse{listResponse: listResponseOf(l.CuratedList), Items: items, Videos: mapSlice(l.Videos, videoResponseOf)}
}

type listImporterResponse struct {
	UserID         uuid.UUID `json:"user_id"`
	Name           string    `json:"name"`
	Products       int64     `json:"products"`
	LastImportedAt time.Time `json:"last_imported_at"`
}

type listDashboardResponse struct {
	Affiliates int                    `json:"affiliates"`
	Importers  []listImporterResponse `json:"importers"`
}

func listDashboardResponseOf(d domain.ListDashboard) listDashboardResponse {
	return listDashboardResponse{
		Affiliates: d.Affiliates,
		Importers: mapSlice(d.Importers, func(i domain.ListImporter) listImporterResponse {
			return listImporterResponse{UserID: i.UserID, Name: i.Name, Products: i.Products, LastImportedAt: i.LastImportedAt}
		}),
	}
}

type listImportResponse struct {
	Created      int               `json:"created"`
	AlreadySaved int               `json:"already_saved"`
	CollectionID *uuid.UUID        `json:"collection_id"`
	LinkStatus   domain.LinkStatus `json:"link_status"`
}

type listRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
}

type addListItemRequest struct {
	ProductID *uuid.UUID `json:"product_id"`
	URL       string     `json:"url"`
	Comment   string     `json:"comment"`
}

type reorderListRequest struct {
	ProductIDs []uuid.UUID `json:"product_ids"`
}

type commentListItemRequest struct {
	Comment *string `json:"comment"`
}

type importListRequest struct {
	ProductIDs []uuid.UUID `json:"product_ids"`
	// Collection (default true) also puts the items in a collection named
	// after the list.
	Collection *bool `json:"collection"`
}
