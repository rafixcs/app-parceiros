package server

import (
	"bytes"
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/repository"
)

const curationPushEndpoint = "https://fcm.googleapis.com/fcm/send/abc123"

// curationEnv is the collections environment (fake source, affiliator and
// link queue) with email and Web Push fakes.
type curationEnv struct {
	*collEnv
	mailer *fakeMailer
	push   *fakePush
}

func newCurationEnv(t *testing.T) *curationEnv {
	t.Helper()
	e := &curationEnv{
		collEnv: &collEnv{
			queue:      &collQueue{},
			affiliator: &collAffiliator{connected: map[uuid.UUID]bool{}},
			offers:     collOffers(),
		},
		mailer: &fakeMailer{},
		push:   &fakePush{expired: map[string]bool{}},
	}
	e.testApp = newTestApp(t, withNotificationChannels(e.mailer, e.push), func(in *infra) {
		in.affiliator = e.affiliator
		in.linkParser = collLinkParser{}
		in.linkQueue = e.queue
		in.catalog = collCatalog{offers: e.offers}
	})
	if err := repository.NewPostgresProduct(e.pool).Record(context.Background(), domain.SourceShopee, time.Now(), e.offers[:collInCatalog]); err != nil {
		t.Fatal(err)
	}
	return e
}

// group creates the mentorship of owner with the affiliates invited.
func (e *curationEnv) group(owner string, affiliates ...string) uuid.UUID {
	e.t.Helper()
	ws := e.mentorship(owner, "Turma do "+owner).ID
	for _, sub := range affiliates {
		e.join(owner, sub, ws)
	}
	return ws
}

func (e *curationEnv) emailsTo(addr string) []domain.Email {
	e.mailer.mu.Lock()
	defer e.mailer.mu.Unlock()
	var out []domain.Email
	for _, m := range e.mailer.sent {
		if m.To == addr {
			out = append(out, m)
		}
	}
	return out
}

// JSON of the curation routes, as clients see it.

type listJSON struct {
	ID           uuid.UUID  `json:"id"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	PublishedAt  *time.Time `json:"published_at"`
	Products     int64      `json:"products"`
	Importers    *int64     `json:"importers"`
	ImportedByMe bool       `json:"imported_by_me"`
}

type listMyItemJSON struct {
	ID            uuid.UUID      `json:"id"`
	AffiliateLink *string        `json:"affiliate_link"`
	LinkStatus    string         `json:"link_status"`
	Links         []collLinkJSON `json:"links"`
}

type listItemJSON struct {
	Product   collProductJSON `json:"product"`
	Comment   string          `json:"comment"`
	Position  int32           `json:"position"`
	Importers *int64          `json:"importers"`
	MyItem    *listMyItemJSON `json:"my_item"`
	Videos    []videoJSON     `json:"videos"`
}

type listDetailJSON struct {
	listJSON
	Items  []listItemJSON `json:"items"`
	Videos []videoJSON    `json:"videos"`
}

type listImportJSON struct {
	Created      int        `json:"created"`
	AlreadySaved int        `json:"already_saved"`
	CollectionID *uuid.UUID `json:"collection_id"`
	LinkStatus   string     `json:"link_status"`
}

type listDashboardJSON struct {
	Affiliates int `json:"affiliates"`
	Importers  []struct {
		UserID   uuid.UUID `json:"user_id"`
		Name     string    `json:"name"`
		Products int64     `json:"products"`
	} `json:"importers"`
}

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

// TestListReachesAffiliate is the criterion of M4: the mentor's list reaches
// the affiliate, who imports it and gets their own link.
func TestListReachesAffiliate(t *testing.T) {
	a := newCurationEnv(t)
	wsID := a.group("mentor", "ana", "beto")
	base := wsPath(wsID, "")
	a.connect("ana")
	a.must("ana", http.MethodPost, "/v1/me/push", pushSubscription(curationPushEndpoint), nil, http.StatusNoContent)

	// The mentor builds the list: two products of the radar and one pasted by link.
	var l listDetailJSON
	a.must("mentor", http.MethodPost, base+"/lists", map[string]any{"title": "  Achados da   semana ", "description": "Para o fim de semana"}, &l, http.StatusCreated)
	if l.Title != "Achados da semana" || l.PublishedAt != nil || len(l.Items) != 0 || l.Importers == nil || *l.Importers != 0 {
		t.Fatalf("created list: %+v", l)
	}
	list := base + "/lists/" + l.ID.String()
	a.must("mentor", http.MethodPost, list+"/items", map[string]any{"product_id": a.productID(0), "comment": "Vende muito no reels"}, &l, http.StatusCreated)
	a.must("mentor", http.MethodPost, list+"/items", map[string]any{"product_id": a.productID(1)}, &l, http.StatusCreated)
	outside := a.offers[collInCatalog+2]
	a.must("mentor", http.MethodPost, list+"/items", map[string]any{"url": "https://shopee.com.br/x-i." + itoa64(outside.ShopID) + "." + itoa64(outside.ItemID)}, &l, http.StatusCreated)
	a.mustFail("mentor", http.MethodPost, list+"/items", map[string]any{"product_id": a.productID(1)}, http.StatusConflict, "already_in_list")
	if len(l.Items) != 3 || l.Items[0].Comment != "Vende muito no reels" || l.Items[2].Product.Name != outside.Name || l.Products != 3 {
		t.Fatalf("items: %+v", l.Items)
	}

	// Reorder (the third first) and change a comment.
	order := []uuid.UUID{l.Items[2].Product.ID, l.Items[0].Product.ID, l.Items[1].Product.ID}
	a.must("mentor", http.MethodPut, list+"/order", map[string]any{"product_ids": order}, &l, http.StatusOK)
	if l.Items[0].Product.ID != order[0] || l.Items[2].Product.ID != order[2] {
		t.Fatalf("order: %+v", l.Items)
	}
	a.mustFail("mentor", http.MethodPut, list+"/order", map[string]any{"product_ids": order[:2]}, http.StatusUnprocessableEntity, "invalid_list_order")
	a.mustFail("mentor", http.MethodPut, list+"/order", map[string]any{"product_ids": []uuid.UUID{order[0], order[0], order[1]}}, http.StatusUnprocessableEntity, "invalid_list_order")
	a.must("mentor", http.MethodPatch, list+"/items/"+order[2].String(), map[string]any{"comment": "Frete grátis"}, &l, http.StatusOK)
	if l.Items[2].Comment != "Frete grátis" {
		t.Fatalf("comment: %+v", l.Items[2])
	}

	// Draft: the affiliates do not see it yet.
	var ls []listJSON
	a.must("ana", http.MethodGet, base+"/lists", nil, &ls, http.StatusOK)
	if len(ls) != 0 {
		t.Fatalf("ana sees the draft: %+v", ls)
	}
	a.mustFail("ana", http.MethodGet, list, nil, http.StatusNotFound, "list_not_found")
	a.mustFail("ana", http.MethodPost, list+"/import", map[string]any{}, http.StatusNotFound, "list_not_found")

	// Publishing notifies ana and beto, not the mentor.
	a.must("mentor", http.MethodPost, list+"/publish", nil, &l, http.StatusOK)
	if l.PublishedAt == nil {
		t.Fatalf("not published: %+v", l.listJSON)
	}
	notices := a.notices.take()
	if len(notices) != 2 {
		t.Fatalf("notices: %+v", notices)
	}
	for _, d := range notices {
		if err := a.svcs.notifications.Deliver(context.Background(), d); err != nil {
			t.Fatal(err)
		}
	}
	listURL := "/w/" + wsID.String() + "/listas/" + l.ID.String()
	for _, sub := range []string{"ana", "beto"} {
		in := a.inbox(sub, wsID)
		if in.Unread != 1 || in.Notifications[0].Title != "Nova lista: Achados da semana" || in.Notifications[0].Kind != "list_published" ||
			in.Notifications[0].URL != listURL || !strings.Contains(in.Notifications[0].Body, "3 produtos") ||
			!strings.Contains(in.Notifications[0].Body, "Turma do mentor publicou") {
			t.Fatalf("inbox of %s: %+v", sub, in)
		}
		emails := a.emailsTo(sub + "@dev.local")
		if len(emails) != 1 || !strings.Contains(emails[0].Text, "https://app.test"+listURL) {
			t.Fatalf("email of %s: %+v", sub, emails)
		}
	}
	if len(a.emailsTo("mentor@dev.local")) != 0 {
		t.Fatal("the mentor got the notice of their own list")
	}
	if len(a.push.sent) != 1 || a.push.sent[0] != curationPushEndpoint || a.push.msgs[0].Title != "Nova lista: Achados da semana" {
		t.Fatalf("push: %+v %+v", a.push.sent, a.push.msgs)
	}

	// Delivering again (retry) does not resend email nor push; publishing again does not notify.
	for _, d := range notices {
		if err := a.svcs.notifications.Deliver(context.Background(), d); err != nil {
			t.Fatal(err)
		}
	}
	if len(a.emailsTo("ana@dev.local")) != 1 || len(a.push.sent) != 1 {
		t.Fatal("the repeated delivery resent")
	}
	a.must("mentor", http.MethodPost, list+"/publish", nil, nil, http.StatusOK)
	if n := len(a.notices.take()); n != 0 {
		t.Fatalf("publishing again notified %d", n)
	}

	// Ana imports two products: they enter the collection with the mentor's
	// tip and a collection named after the list, and the link comes out with
	// her credential.
	a.must("ana", http.MethodGet, base+"/lists", nil, &ls, http.StatusOK)
	if len(ls) != 1 || ls[0].Importers != nil || ls[0].ImportedByMe || ls[0].Products != 3 {
		t.Fatalf("lists of ana: %+v", ls)
	}
	var res listImportJSON
	a.must("ana", http.MethodPost, list+"/import", map[string]any{"product_ids": []uuid.UUID{order[1], order[2]}}, &res, http.StatusOK)
	if res.Created != 2 || res.AlreadySaved != 0 || res.CollectionID == nil || res.LinkStatus != "generating" {
		t.Fatalf("import: %+v", res)
	}
	if a.process() != 2 {
		t.Fatal("the links of the import were not generated")
	}
	var lv listDetailJSON
	a.must("ana", http.MethodGet, list, nil, &lv, http.StatusOK)
	if !lv.ImportedByMe || lv.Items[0].MyItem != nil || lv.Items[0].Importers != nil || lv.Importers != nil {
		t.Fatalf("list seen by ana: %+v", lv)
	}
	for _, it := range lv.Items[1:] {
		if it.MyItem == nil || it.MyItem.LinkStatus != "ready" || it.MyItem.AffiliateLink == nil ||
			!strings.HasPrefix(*it.MyItem.AffiliateLink, "https://s.shopee.com.br/") || len(it.MyItem.Links) != len(domain.Channels) {
			t.Fatalf("ana's link on product %s: %+v", it.Product.Name, it.MyItem)
		}
	}
	var item collItemJSON
	a.must("ana", http.MethodGet, base+"/items/"+lv.Items[1].MyItem.ID.String(), nil, &item, http.StatusOK)
	if item.Notes != "Dica do mentor: Vende muito no reels" || len(item.CollectionIDs) != 1 || item.CollectionIDs[0] != *res.CollectionID {
		t.Fatalf("imported item: %+v", item)
	}
	var cs []collCollectionJSON
	a.must("ana", http.MethodGet, base+"/collections", nil, &cs, http.StatusOK)
	if len(cs) != 1 || cs[0].Name != "Achados da semana" || cs[0].Items != 2 {
		t.Fatalf("collections of ana: %+v", cs)
	}

	// Importing everything later: only what is missing is new, and the collection is the same.
	a.must("ana", http.MethodPost, list+"/import", map[string]any{}, &res, http.StatusOK)
	if res.Created != 1 || res.AlreadySaved != 2 || a.process() != 1 {
		t.Fatalf("import all: %+v", res)
	}
	a.must("ana", http.MethodGet, base+"/collections", nil, &cs, http.StatusOK)
	if len(cs) != 1 || cs[0].Items != 3 {
		t.Fatalf("collections after importing all: %+v", cs)
	}
	a.mustFail("ana", http.MethodPost, list+"/import", map[string]any{"product_ids": []uuid.UUID{a.productID(5)}}, http.StatusUnprocessableEntity, "product_not_in_list")

	// Beto, without Shopee connected, imports without a collection: link pending.
	a.must("beto", http.MethodPost, list+"/import", map[string]any{"collection": false}, &res, http.StatusOK)
	if res.Created != 3 || res.CollectionID != nil || res.LinkStatus != "pending" || a.process() != 0 {
		t.Fatalf("beto's import: %+v", res)
	}

	// The mentor sees who imported.
	var d listDashboardJSON
	a.must("mentor", http.MethodGet, list+"/dashboard", nil, &d, http.StatusOK)
	if d.Affiliates != 2 || len(d.Importers) != 2 || d.Importers[0].Name != "beto" || d.Importers[0].Products != 3 {
		t.Fatalf("dashboard: %+v", d)
	}
	a.must("mentor", http.MethodGet, list, nil, &lv, http.StatusOK)
	if *lv.Importers != 2 || *lv.Items[0].Importers != 2 {
		t.Fatalf("list seen by the mentor: %+v", lv)
	}
	a.mustFail("ana", http.MethodGet, list+"/dashboard", nil, http.StatusForbidden, "list_managers_only")

	// The imports the results dashboard adds up per list.
	mentor, err := a.svcs.accounts.Member(context.Background(), a.me("mentor").ID, wsID)
	if err != nil {
		t.Fatal(err)
	}
	imps, err := a.svcs.curation.Imports(context.Background(), mentor)
	if err != nil {
		t.Fatal(err)
	}
	if len(imps) != 1 || imps[0].ID != l.ID || imps[0].Title != "Achados da semana" || imps[0].PublishedAt == nil || len(imps[0].Imports) != 6 {
		t.Fatalf("imports: %+v", imps)
	}
	anaMember, err := a.svcs.accounts.Member(context.Background(), a.me("ana").ID, wsID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.svcs.curation.Imports(context.Background(), anaMember); err != domain.ErrListManagersOnly {
		t.Fatalf("affiliate read the imports: %v", err)
	}

	// Deleting the list does not touch the collection of who imported.
	a.must("mentor", http.MethodDelete, list, nil, nil, http.StatusNoContent)
	a.must("ana", http.MethodGet, base+"/collections", nil, &cs, http.StatusOK)
	if len(cs) != 1 || cs[0].Items != 3 {
		t.Fatalf("collection after deleting the list: %+v", cs)
	}
	a.mustFail("mentor", http.MethodGet, list, nil, http.StatusNotFound, "list_not_found")
}

func TestCurationPermissionsAndValidation(t *testing.T) {
	a := newCurationEnv(t)
	wsID := a.group("mentor", "ana")
	base := wsPath(wsID, "")

	// Affiliates do not build lists; the personal workspace has no curation.
	a.mustFail("ana", http.MethodPost, base+"/lists", map[string]any{"title": "Minha"}, http.StatusForbidden, "list_managers_only")
	a.mustFail("mentor", http.MethodPost, wsPath(a.personal("mentor").ID, "/lists"), map[string]any{"title": "x"}, http.StatusConflict, "lists_mentorship_only")

	a.mustFail("mentor", http.MethodPost, base+"/lists", map[string]any{"title": " "}, http.StatusUnprocessableEntity, "invalid_list_title")
	a.mustFail("mentor", http.MethodPost, base+"/lists", map[string]any{"title": strings.Repeat("a", 121)}, http.StatusUnprocessableEntity, "invalid_list_title")
	a.mustFail("mentor", http.MethodPost, base+"/lists", map[string]any{"title": "x", "description": strings.Repeat("a", 2001)}, http.StatusUnprocessableEntity, "invalid_list_description")
	a.mustFail("mentor", http.MethodPost, base+"/lists", map[string]any{"title": "x", "other": 1}, http.StatusBadRequest, "invalid_json")

	var l listDetailJSON
	a.must("mentor", http.MethodPost, base+"/lists", map[string]any{"title": "Natal"}, &l, http.StatusCreated)
	list := base + "/lists/" + l.ID.String()
	a.mustFail("mentor", http.MethodPost, list+"/publish", nil, http.StatusConflict, "list_empty")
	a.mustFail("mentor", http.MethodPost, list+"/import", map[string]any{}, http.StatusConflict, "list_not_published")
	a.mustFail("mentor", http.MethodPost, list+"/items", map[string]any{"product_id": uuid.New()}, http.StatusNotFound, "product_not_found")
	a.mustFail("mentor", http.MethodPost, list+"/items", map[string]any{"url": "https://s.shopee.com.br/abc"}, http.StatusUnprocessableEntity, "short_link")
	a.mustFail("mentor", http.MethodPost, list+"/items", map[string]any{}, http.StatusUnprocessableEntity, "product_or_link_required")
	a.mustFail("mentor", http.MethodPost, list+"/items", map[string]any{"product_id": a.productID(0), "comment": strings.Repeat("a", 1001)}, http.StatusUnprocessableEntity, "invalid_list_comment")
	a.mustFail("mentor", http.MethodPatch, list+"/items/"+a.productID(0).String(), map[string]any{"comment": "x"}, http.StatusNotFound, "list_item_not_found")
	a.mustFail("mentor", http.MethodPatch, list+"/items/"+a.productID(0).String(), map[string]any{}, http.StatusUnprocessableEntity, "list_comment_required")
	a.mustFail("mentor", http.MethodDelete, list+"/items/"+a.productID(0).String(), nil, http.StatusNotFound, "list_item_not_found")
	a.mustFail("mentor", http.MethodDelete, list+"/items/not-a-uuid", nil, http.StatusNotFound, "list_item_not_found")
	a.mustFail("mentor", http.MethodGet, base+"/lists/"+uuid.NewString(), nil, http.StatusNotFound, "list_not_found")
	a.mustFail("mentor", http.MethodGet, base+"/lists/not-a-uuid", nil, http.StatusNotFound, "list_not_found")
	a.mustFail("mentor", http.MethodPost, base+"/lists/"+uuid.NewString()+"/items", map[string]any{"product_id": a.productID(0)}, http.StatusNotFound, "list_not_found")
	a.mustFail("mentor", http.MethodGet, base+"/lists/"+uuid.NewString()+"/dashboard", nil, http.StatusNotFound, "list_not_found")

	a.must("mentor", http.MethodPost, list+"/items", map[string]any{"product_id": a.productID(0)}, &l, http.StatusCreated)
	a.must("mentor", http.MethodPost, list+"/items", map[string]any{"product_id": a.productID(1)}, &l, http.StatusCreated)
	a.must("mentor", http.MethodDelete, list+"/items/"+a.productID(0).String(), nil, &l, http.StatusOK)
	if len(l.Items) != 1 || l.Items[0].Product.ID != a.productID(1) {
		t.Fatalf("after removing: %+v", l.Items)
	}
	a.must("mentor", http.MethodPatch, list, map[string]any{"description": "Presentes"}, &l, http.StatusOK)
	if l.Title != "Natal" || l.Description != "Presentes" {
		t.Fatalf("update: %+v", l.listJSON)
	}
	a.mustFail("mentor", http.MethodPatch, list, map[string]any{"title": ""}, http.StatusUnprocessableEntity, "invalid_list_title")
	a.must("mentor", http.MethodPost, list+"/publish", nil, &l, http.StatusOK)

	// The affiliate only reads and imports.
	a.must("ana", http.MethodGet, list, nil, &l, http.StatusOK)
	for _, c := range []struct{ method, path string }{
		{http.MethodPatch, list},
		{http.MethodDelete, list},
		{http.MethodPost, list + "/items"},
		{http.MethodPost, list + "/publish"},
		{http.MethodPut, list + "/order"},
		{http.MethodPatch, list + "/items/" + a.productID(1).String()},
		{http.MethodDelete, list + "/items/" + a.productID(1).String()},
		{http.MethodGet, list + "/dashboard"},
		{http.MethodPut, list + "/videos/" + uuid.NewString()},
		{http.MethodDelete, list + "/videos/" + uuid.NewString()},
	} {
		var body any = map[string]any{}
		switch {
		case c.method == http.MethodDelete || c.method == http.MethodGet || strings.HasSuffix(c.path, "/publish") || strings.Contains(c.path, "/videos/"):
			body = nil
		case c.method == http.MethodPatch && strings.Contains(c.path, "/items/"):
			body = map[string]any{"comment": "x"}
		}
		a.mustFail("ana", c.method, c.path, body, http.StatusForbidden, "list_managers_only")
	}

	// The plan's limit of lists.
	a.admin("UPDATE plan_limits SET value = 1 WHERE plan = 'mentorship' AND key = 'lists'")
	a.mustFail("mentor", http.MethodPost, base+"/lists", map[string]any{"title": "Outra"}, http.StatusConflict, "list_limit")
}

func TestListFull(t *testing.T) {
	a := newCurationEnv(t)
	wsID := a.group("mentor")
	offers := make([]domain.Offer, domain.MaxListItems+1)
	for i := range offers {
		item := int64(90000 + i)
		offers[i] = domain.Offer{ItemID: item, ShopID: 9, ShopName: "Loja", Name: "Item " + strconv.Itoa(i),
			URL: "https://shopee.com.br/product/9/" + itoa64(item), MinPriceCents: 100, MaxPriceCents: 100, CommissionBP: 100}
	}
	if err := repository.NewPostgresProduct(a.pool).Record(context.Background(), domain.SourceShopee, time.Now(), offers); err != nil {
		t.Fatal(err)
	}
	var l listDetailJSON
	a.must("mentor", http.MethodPost, wsPath(wsID, "/lists"), map[string]any{"title": "Cheia"}, &l, http.StatusCreated)
	list := wsPath(wsID, "/lists/"+l.ID.String())
	for _, o := range offers[:domain.MaxListItems] {
		p, err := a.svcs.products.ProductByItem(context.Background(), domain.SourceShopee, o.ItemID)
		if err != nil {
			t.Fatal(err)
		}
		a.must("mentor", http.MethodPost, list+"/items", map[string]any{"product_id": p.ID}, nil, http.StatusCreated)
	}
	last, err := a.svcs.products.ProductByItem(context.Background(), domain.SourceShopee, offers[domain.MaxListItems].ItemID)
	if err != nil {
		t.Fatal(err)
	}
	a.mustFail("mentor", http.MethodPost, list+"/items", map[string]any{"product_id": last.ID}, http.StatusConflict, "list_full")
}

// TestCurationLeak checks that lists, items and imports do not leak between
// workspaces nor between members, through the API and in the database (RLS).
func TestCurationLeak(t *testing.T) {
	a := newCurationEnv(t)
	ctx := context.Background()
	wsID := a.group("mentor", "ana", "beto")
	other := a.group("rival", "carla")
	base := wsPath(wsID, "")

	var draft, published listDetailJSON
	a.must("mentor", http.MethodPost, base+"/lists", map[string]any{"title": "Rascunho"}, &draft, http.StatusCreated)
	a.must("mentor", http.MethodPost, base+"/lists/"+draft.ID.String()+"/items", map[string]any{"product_id": a.productID(0)}, nil, http.StatusCreated)
	a.must("mentor", http.MethodPost, base+"/lists", map[string]any{"title": "Publicada"}, &published, http.StatusCreated)
	a.must("mentor", http.MethodPost, base+"/lists/"+published.ID.String()+"/items", map[string]any{"product_id": a.productID(1)}, nil, http.StatusCreated)
	a.must("mentor", http.MethodPost, base+"/lists/"+published.ID.String()+"/publish", nil, nil, http.StatusOK)
	a.deliverNotifications()
	a.must("ana", http.MethodPost, base+"/lists/"+published.ID.String()+"/import", map[string]any{}, nil, http.StatusOK)

	// The mentor sees the draft first, then the published one.
	var ls []listJSON
	a.must("mentor", http.MethodGet, base+"/lists", nil, &ls, http.StatusOK)
	if len(ls) != 2 || ls[0].ID != draft.ID || ls[1].ID != published.ID {
		t.Fatalf("lists of the mentor: %+v", ls)
	}
	a.must("beto", http.MethodGet, base+"/lists", nil, &ls, http.StatusOK)
	if len(ls) != 1 || ls[0].ID != published.ID {
		t.Fatalf("lists of beto: %+v", ls)
	}
	a.mustFail("beto", http.MethodGet, base+"/lists/"+draft.ID.String(), nil, http.StatusNotFound, "list_not_found")

	// Another workspace sees nothing, not even with the right list id.
	otherBase := wsPath(other, "")
	a.must("rival", http.MethodGet, otherBase+"/lists", nil, &ls, http.StatusOK)
	if len(ls) != 0 {
		t.Fatalf("rival sees lists of another mentorship: %+v", ls)
	}
	a.mustFail("rival", http.MethodGet, otherBase+"/lists/"+published.ID.String(), nil, http.StatusNotFound, "list_not_found")
	a.mustFail("rival", http.MethodPatch, otherBase+"/lists/"+published.ID.String(), map[string]any{"title": "x"}, http.StatusNotFound, "list_not_found")
	a.mustFail("rival", http.MethodDelete, otherBase+"/lists/"+draft.ID.String(), nil, http.StatusNotFound, "list_not_found")
	a.mustFail("rival", http.MethodPost, otherBase+"/lists/"+draft.ID.String()+"/items", map[string]any{"product_id": a.productID(2)}, http.StatusNotFound, "list_not_found")
	a.mustFail("rival", http.MethodPost, otherBase+"/lists/"+published.ID.String()+"/publish", nil, http.StatusNotFound, "list_not_found")
	a.mustFail("rival", http.MethodGet, otherBase+"/lists/"+published.ID.String()+"/dashboard", nil, http.StatusNotFound, "list_not_found")
	a.mustFail("carla", http.MethodPost, otherBase+"/lists/"+published.ID.String()+"/import", map[string]any{}, http.StatusNotFound, "list_not_found")
	a.mustFail("rival", http.MethodGet, base+"/lists", nil, http.StatusNotFound, "workspace_not_found")
	a.mustFail("rival", http.MethodGet, base+"/lists/"+published.ID.String(), nil, http.StatusNotFound, "workspace_not_found")
	if in := a.inbox("carla", other); len(in.Notifications) != 0 {
		t.Fatalf("carla got a notice of another mentorship: %+v", in)
	}
	// The mentor in their personal workspace does not see the mentorship lists.
	a.mustFail("mentor", http.MethodGet, wsPath(a.personal("mentor").ID, "/lists/"+published.ID.String()), nil, http.StatusNotFound, "list_not_found")

	count := func(s database.Scope, table string) int {
		t.Helper()
		var n int
		err := database.InTx(ctx, a.pool, s, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	scope := func(sub string, ws uuid.UUID) database.Scope {
		return database.Scope{UserID: a.me(sub).ID.String(), WorkspaceID: ws.String()}
	}
	for _, c := range []struct {
		name  string
		s     database.Scope
		table string
		want  int
	}{
		{"mentor sees draft and published", scope("mentor", wsID), "curated_lists", 2},
		{"mentor sees the items of both", scope("mentor", wsID), "curated_list_items", 2},
		{"affiliate only sees the published", scope("beto", wsID), "curated_lists", 1},
		{"affiliate only sees items of the published", scope("beto", wsID), "curated_list_items", 1},
		{"other workspace sees no lists", scope("rival", other), "curated_lists", 0},
		{"other workspace sees no items", scope("rival", other), "curated_list_items", 0},
		{"other workspace sees no imports", scope("rival", other), "list_imports", 0},
		{"manager of another workspace in the wrong workspace", scope("rival", wsID), "curated_lists", 1},
		{"no workspace sees no lists", database.Scope{UserID: a.me("mentor").ID.String()}, "curated_lists", 0},
		{"no scope sees no imports", database.Scope{}, "list_imports", 0},
		{"mentor sees the group's imports", scope("mentor", wsID), "list_imports", 1},
		{"ana sees her import", scope("ana", wsID), "list_imports", 1},
		{"beto does not see ana's import", scope("beto", wsID), "list_imports", 0},
	} {
		if n := count(c.s, c.table); n != c.want {
			t.Errorf("%s: %d rows in %s, want %d", c.name, n, c.table, c.want)
		}
	}

	// Writing with the application role: an affiliate creates no list nor
	// imports on behalf of another, and nobody imports a draft.
	write := func(s database.Scope, sql string, args ...any) error {
		return database.InTx(ctx, a.pool, s, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, sql, args...)
			return err
		})
	}
	ana := a.me("ana").ID
	for name, err := range map[string]error{
		"affiliate creates a list": write(scope("ana", wsID),
			"INSERT INTO curated_lists (workspace_id, author_id, title) VALUES ($1, $2, 'x')", wsID, ana),
		"affiliate adds an item": write(scope("ana", wsID),
			"INSERT INTO curated_list_items (list_id, workspace_id, product_id, position) VALUES ($1, $2, $3, 9)", published.ID, wsID, a.productID(3)),
		"import on behalf of ana": write(scope("beto", wsID),
			"INSERT INTO list_imports (list_id, workspace_id, user_id, product_id) VALUES ($1, $2, $3, $4)", published.ID, wsID, ana, a.productID(1)),
		"import of a draft": write(scope("ana", wsID),
			"INSERT INTO list_imports (list_id, workspace_id, user_id, product_id) VALUES ($1, $2, $3, $4)", draft.ID, wsID, ana, a.productID(0)),
		"list in another workspace": write(scope("rival", other),
			"INSERT INTO curated_lists (workspace_id, author_id, title) VALUES ($1, $2, 'x')", wsID, a.me("rival").ID),
	} {
		if !isRLSViolation(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Affiliates and other workspaces neither edit nor delete lists (the
	// policies hide the rows).
	for _, s := range []database.Scope{scope("ana", wsID), scope("rival", other)} {
		for _, sql := range []string{
			"UPDATE curated_lists SET title = 'invaded'",
			"UPDATE curated_list_items SET comment = 'invaded'",
			"DELETE FROM curated_list_items",
			"DELETE FROM curated_lists",
		} {
			if err := write(s, sql); err != nil {
				t.Fatal(err)
			}
		}
	}
	if n := count(scope("mentor", wsID), "curated_lists"); n != 2 {
		t.Fatalf("lists were deleted: %d left", n)
	}
	var l listDetailJSON
	a.must("mentor", http.MethodGet, base+"/lists/"+published.ID.String(), nil, &l, http.StatusOK)
	if l.Title != "Publicada" || len(l.Items) != 1 || l.Items[0].Comment != "" {
		t.Fatalf("the list was changed: %+v", l)
	}
}

// TestListVideos: the mentor attaches videos to a list (they become shared),
// and the group sees them with the list and on its products.
func TestListVideos(t *testing.T) {
	e := newMediaEnv(t, mediaProcessor{})
	wsID := e.mentorship("mentor", "Turma").ID
	e.join("mentor", "ana", wsID)
	ws := wsPath(wsID, "")
	product := e.products[2]

	var ref videoJSON
	e.must("mentor", http.MethodPost, ws+"/videos/embed", map[string]any{"url": mediaYouTubeOK, "product_id": product}, &ref, http.StatusCreated)
	own := e.upload("mentor", ws, "aula.mp4", bytes.Repeat([]byte("m"), 40), &product)
	e.run("process_video")
	e.must("mentor", http.MethodPatch, ws+"/videos/"+own.ID.String(), map[string]any{"shared": true}, &own, http.StatusOK)
	var hers videoJSON
	e.must("ana", http.MethodPost, ws+"/videos/embed", map[string]any{"url": mediaYouTubeOK2}, &hers, http.StatusCreated)

	// The mentor attaches the reference to a list: it becomes shared.
	var l listDetailJSON
	e.must("mentor", http.MethodPost, ws+"/lists", map[string]any{"title": "Achados"}, &l, http.StatusCreated)
	list := ws + "/lists/" + l.ID.String()
	e.must("mentor", http.MethodPost, list+"/items", map[string]any{"product_id": product, "comment": "Veja os vídeos"}, &l, http.StatusCreated)
	e.must("mentor", http.MethodPut, list+"/videos/"+ref.ID.String(), nil, &l, http.StatusOK)
	if !slices.Equal(videoIDs(l.Videos), []uuid.UUID{ref.ID}) || !l.Videos[0].Shared ||
		!slices.Equal(videoIDs(l.Items[0].Videos), []uuid.UUID{ref.ID, own.ID}) {
		t.Fatalf("list with videos: %+v / %+v", l.Videos, l.Items[0].Videos)
	}
	e.mustFail("ana", http.MethodPut, list+"/videos/"+hers.ID.String(), nil, http.StatusForbidden, "list_managers_only")
	e.mustFail("mentor", http.MethodPut, list+"/videos/"+hers.ID.String(), nil, http.StatusNotFound, "video_not_found")
	e.mustFail("mentor", http.MethodPut, list+"/videos/not-a-uuid", nil, http.StatusNotFound, "video_not_found")
	e.mustFail("mentor", http.MethodPut, ws+"/lists/"+uuid.NewString()+"/videos/"+ref.ID.String(), nil, http.StatusNotFound, "list_not_found")

	// Published, the affiliate sees the videos of the list and of the product.
	e.must("mentor", http.MethodPost, list+"/publish", nil, &l, http.StatusOK)
	e.must("ana", http.MethodGet, list, nil, &l, http.StatusOK)
	if !slices.Equal(videoIDs(l.Videos), []uuid.UUID{ref.ID}) || !slices.Equal(videoIDs(l.Items[0].Videos), []uuid.UUID{ref.ID, own.ID}) {
		t.Fatalf("list for the affiliate: %+v / %+v", l.Videos, l.Items[0].Videos)
	}

	// Removing from the list; deleting the list takes the links, not the videos.
	e.must("mentor", http.MethodDelete, list+"/videos/"+ref.ID.String(), nil, &l, http.StatusOK)
	if len(l.Videos) != 0 {
		t.Fatalf("videos after removing: %+v", l.Videos)
	}
	e.mustFail("mentor", http.MethodDelete, list+"/videos/"+ref.ID.String(), nil, http.StatusNotFound, "video_not_in_list")
	e.must("mentor", http.MethodPut, list+"/videos/"+own.ID.String(), nil, &l, http.StatusOK)
	e.must("mentor", http.MethodGet, ws+"/videos/"+own.ID.String(), nil, &own, http.StatusOK)
	if !slices.Equal(own.ListIDs, []uuid.UUID{l.ID}) {
		t.Fatalf("own video in the list: %+v", own)
	}
	e.must("mentor", http.MethodDelete, list, nil, nil, http.StatusNoContent)
	e.must("mentor", http.MethodGet, ws+"/videos/"+own.ID.String(), nil, &own, http.StatusOK)
	if len(own.ListIDs) != 0 || len(own.ProductIDs) != 1 {
		t.Fatalf("links after deleting the list: %+v", own)
	}
}
