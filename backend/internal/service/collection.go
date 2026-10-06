package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

const (
	DefaultItemsPerPage   = 30
	MaxItemsPerPage       = 100
	maxItemTitle          = 200
	maxItemDescription    = 2000
	maxItemNotes          = 5000
	maxItemTags           = 20
	maxItemTag            = 30
	maxAffiliateLink      = 500
	maxItemQuery          = 100
	maxCollectionName     = 60
	maxCollectionsPerItem = 100
	maxPastedLink         = 2000
	mentorTipPrefix       = "Dica do mentor: "
)

// collectionProducts is what the collections need from the catalog
// (ProductService).
type collectionProducts interface {
	Product(ctx context.Context, id uuid.UUID) (domain.Product, error)
	Products(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]domain.Product, error)
	ProductByItem(ctx context.Context, source domain.Source, itemID int64) (domain.Product, error)
	Import(ctx context.Context, source domain.Source, o domain.Offer) (domain.Product, error)
	Catalog() domain.Catalog
}

// CollectionService keeps the products each user saved to promote (items),
// their affiliate links per channel and the collections (folders) they
// organize them in. The collection belongs to the user inside each workspace:
// not even the mentor sees it.
type CollectionService struct {
	repo     domain.CollectionRepository
	tx       domain.Transactor
	products collectionProducts
	// affiliator generates the links with each user's credential; nil
	// behaves as if nobody had one connected.
	affiliator domain.Affiliator
	// links reads pasted product links; nil refuses pasted links.
	links domain.LinkParser
	// queue enqueues generate_affiliate_link; nil in the worker, which does
	// not enqueue.
	queue domain.AffiliateLinkQueue
	log   *slog.Logger
}

func NewCollectionService(repo domain.CollectionRepository, tx domain.Transactor, products collectionProducts,
	affiliator domain.Affiliator, links domain.LinkParser, queue domain.AffiliateLinkQueue, log *slog.Logger,
) *CollectionService {
	return &CollectionService{
		repo: repo, tx: tx, products: products, affiliator: affiliator, links: links, queue: queue, log: log,
	}
}

// Save keeps a product in the collection: by its catalog id or by its pasted
// link. When it was already saved, it returns the existing item and
// created=false.
func (s *CollectionService) Save(ctx context.Context, a domain.Actor, productID *uuid.UUID, link string) (domain.Item, bool, error) {
	p, err := s.ResolveProduct(ctx, productID, link)
	if err != nil {
		return domain.Item{}, false, err
	}
	status, err := s.initialLinkStatus(ctx, a.UserID)
	if err != nil {
		return domain.Item{}, false, err
	}
	row, created, err := s.repo.CreateItem(ctx, a, domain.NewSavedItem{
		ProductID: p.ID, Title: truncateRunes(p.Name, maxItemTitle), LinkStatus: status,
	})
	if err != nil {
		return domain.Item{}, false, err
	}
	if created && status == domain.LinkGenerating {
		row = s.enqueue(ctx, a, row)
	}
	it, err := s.assembleOne(ctx, a, row)
	return it, created, err
}

// ResolveProduct finds the product by its catalog id or by the pasted link of
// the source (exactly one of them). A product not collected yet is fetched
// with the app credential and imported. Curation uses the same path to build
// its lists.
func (s *CollectionService) ResolveProduct(ctx context.Context, productID *uuid.UUID, link string) (domain.Product, error) {
	link = strings.TrimSpace(link)
	if (productID == nil) == (link == "") {
		return domain.Product{}, domain.ErrProductOrLinkRequired
	}
	if productID == nil {
		return s.productOfLink(ctx, link)
	}
	return s.products.Product(ctx, *productID)
}

// productOfLink finds the product of the link in the catalog or, when it was
// not collected yet, fetches it from the source and imports it.
func (s *CollectionService) productOfLink(ctx context.Context, link string) (domain.Product, error) {
	if utf8.RuneCountInString(link) > maxPastedLink {
		return domain.Product{}, domain.ErrInvalidProductLink
	}
	if s.links == nil {
		return domain.Product{}, domain.ErrImportUnavailable
	}
	pl, err := s.links.ParseProductLink(link)
	switch {
	case errors.Is(err, domain.ErrShortLink):
		return domain.Product{}, domain.ErrShortLink
	case err != nil:
		return domain.Product{}, domain.ErrInvalidProductLink
	}
	p, err := s.products.ProductByItem(ctx, pl.Source, pl.ItemID)
	if !errors.Is(err, domain.ErrProductNotFound) {
		return p, err
	}
	catalog := s.products.Catalog()
	if catalog == nil || catalog.Source() != pl.Source {
		return domain.Product{}, domain.ErrImportUnavailable
	}
	o, err := catalog.OfferByItem(ctx, pl.ItemID)
	switch {
	case errors.Is(err, domain.ErrSourceNotFound):
		return domain.Product{}, domain.ErrProductNotFound
	case errors.Is(err, domain.ErrSourceLimit):
		return domain.Product{}, domain.ErrLinkRateLimited
	case err != nil:
		s.log.WarnContext(ctx, "source failed to find a pasted product", "item_id", pl.ItemID, "err", err)
		return domain.Product{}, domain.ErrProductLookupFailed
	}
	return s.products.Import(ctx, pl.Source, o)
}

// Import saves several products at once (from a curated list). The new ones
// carry the mentor's comment in the notes and enter the
// generate_affiliate_link queue with the user's own credential. With
// collectionName, all of them (new and already saved) go to the collection
// with that name, created when missing.
func (s *CollectionService) Import(ctx context.Context, a domain.Actor, items []domain.ImportedItem, collectionName string) (domain.ImportResult, error) {
	var name string
	if collectionName != "" {
		var err error
		if name, err = collectionNameOf(truncateRunes(collectionName, maxCollectionName)); err != nil {
			return domain.ImportResult{}, err
		}
	}
	ids := make([]uuid.UUID, len(items))
	for i, it := range items {
		ids[i] = it.ProductID
	}
	prods, err := s.products.Products(ctx, ids)
	if err != nil {
		return domain.ImportResult{}, err
	}
	for _, id := range ids {
		if _, ok := prods[id]; !ok {
			return domain.ImportResult{}, domain.ErrProductNotFound
		}
	}
	status, err := s.initialLinkStatus(ctx, a.UserID)
	if err != nil {
		return domain.ImportResult{}, err
	}

	var out domain.ImportResult
	var created []uuid.UUID
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		out, created = domain.ImportResult{LinkStatus: status}, nil
		var collectionID uuid.UUID
		if name != "" {
			id, err := s.repo.CollectionByName(ctx, a, name)
			if errors.Is(err, domain.ErrNotFound) {
				id, err = s.repo.CreateCollection(ctx, a, name)
			}
			if err != nil {
				return err
			}
			collectionID = id
			out.CollectionID = &id
		}
		for _, it := range items {
			p := prods[it.ProductID]
			notes := strings.TrimSpace(it.Comment)
			if notes != "" {
				notes = truncateRunes(mentorTipPrefix+notes, maxItemNotes)
			}
			row, isNew, err := s.repo.CreateItem(ctx, a, domain.NewSavedItem{
				ProductID: p.ID, Title: truncateRunes(p.Name, maxItemTitle), Notes: notes, LinkStatus: status,
			})
			if err != nil {
				return err
			}
			if isNew {
				out.Created++
				created = append(created, row.ID)
			} else {
				out.AlreadySaved++
			}
			if name != "" {
				if err := s.repo.AddToCollection(ctx, a, collectionID, row.ID); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return domain.ImportResult{}, err
	}
	if status == domain.LinkGenerating && len(created) > 0 {
		if err := s.enqueueMany(ctx, a, created); err != nil {
			s.log.ErrorContext(ctx, "could not enqueue generate_affiliate_link for the import", "items", len(created), "err", err)
			for _, id := range created {
				if errMark := s.repo.MarkLinkStatus(ctx, a, id, domain.LinkFailed); errMark != nil {
					return out, errors.Join(err, errMark)
				}
			}
			out.LinkStatus = domain.LinkFailed
		}
	}
	return out, nil
}

// ItemsOfProducts returns the items the user already saved of these
// products, by product id. Curation uses it to show the affiliate's link in a
// list.
func (s *CollectionService) ItemsOfProducts(ctx context.Context, a domain.Actor, productIDs []uuid.UUID) (map[uuid.UUID]domain.Item, error) {
	rows, err := s.repo.ItemsByProducts(ctx, a, productIDs)
	if err != nil {
		return nil, err
	}
	items, err := s.assemble(ctx, a, rows)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]domain.Item, len(items))
	for _, it := range items {
		out[it.ProductID] = it
	}
	return out, nil
}

// List returns a page of the user's items, the newest first.
func (s *CollectionService) List(ctx context.Context, a domain.Actor, f domain.ItemFilter) (domain.ItemPage, error) {
	if f.Page == 0 {
		f.Page = 1
	}
	if f.PerPage == 0 {
		f.PerPage = DefaultItemsPerPage
	}
	if f.Page < 1 || f.PerPage < 1 || f.PerPage > MaxItemsPerPage {
		return domain.ItemPage{}, domain.ErrInvalidItemPage
	}
	q := domain.ItemQuery{
		CollectionID: f.CollectionID, Tag: strings.TrimSpace(f.Tag),
		Limit: f.PerPage, Offset: (f.Page - 1) * f.PerPage,
	}
	if search := strings.TrimSpace(f.Query); search != "" {
		if utf8.RuneCountInString(search) > maxItemQuery {
			return domain.ItemPage{}, domain.ErrQueryTooLong
		}
		q.Query = escapeLike(search)
	}
	if f.Status != "" {
		if !f.Status.Valid() {
			return domain.ItemPage{}, domain.ErrInvalidItemStatus
		}
		q.Status = f.Status
	}
	rows, total, err := s.repo.ListItems(ctx, a, q)
	if err != nil {
		return domain.ItemPage{}, err
	}
	items, err := s.assemble(ctx, a, rows)
	if err != nil {
		return domain.ItemPage{}, err
	}
	return domain.ItemPage{Items: items, Total: total, Page: f.Page, PerPage: f.PerPage}, nil
}

// Get returns one item.
func (s *CollectionService) Get(ctx context.Context, a domain.Actor, id uuid.UUID) (domain.Item, error) {
	row, err := s.item(ctx, a, id)
	if err != nil {
		return domain.Item{}, err
	}
	return s.assembleOne(ctx, a, row)
}

func (s *CollectionService) item(ctx context.Context, a domain.Actor, id uuid.UUID) (domain.SavedItem, error) {
	row, err := s.repo.Item(ctx, a, id)
	if errors.Is(err, domain.ErrNotFound) {
		return row, domain.ErrItemNotFound
	}
	return row, err
}

// Update applies the change. Going back to the automatic link enqueues
// generate_affiliate_link again.
func (s *CollectionService) Update(ctx context.Context, a domain.Actor, id uuid.UUID, u domain.ItemUpdate) (domain.Item, error) {
	var f domain.ItemFields
	var err error
	if f.Title, err = checkItemText(u.Title, maxItemTitle, domain.ErrInvalidItemTitle); err != nil {
		return domain.Item{}, err
	}
	if f.Description, err = checkItemText(u.Description, maxItemDescription, domain.ErrInvalidItemDesc); err != nil {
		return domain.Item{}, err
	}
	if f.Notes, err = checkItemText(u.Notes, maxItemNotes, domain.ErrInvalidItemNotes); err != nil {
		return domain.Item{}, err
	}
	if u.Tags != nil {
		if f.Tags, err = normalizeItemTags(*u.Tags); err != nil {
			return domain.Item{}, err
		}
	}
	if u.Status != nil {
		if !u.Status.Valid() {
			return domain.Item{}, domain.ErrInvalidItemStatus
		}
		f.Status = u.Status
	}
	var manual string
	var auto domain.LinkStatus
	if u.ChangeLink {
		if u.Link != nil {
			if manual, err = checkAffiliateLink(*u.Link); err != nil {
				return domain.Item{}, err
			}
		} else if auto, err = s.initialLinkStatus(ctx, a.UserID); err != nil {
			return domain.Item{}, err
		}
	}

	var row domain.SavedItem
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if row, err = s.repo.UpdateItem(ctx, a, id, f); err != nil {
			return err
		}
		switch {
		case !u.ChangeLink:
		case u.Link != nil:
			row, err = s.repo.SetManualLink(ctx, a, id, manual)
		default:
			row, err = s.repo.ResetAutoLink(ctx, a, id, auto)
		}
		return err
	})
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Item{}, domain.ErrItemNotFound
	}
	if err != nil {
		return domain.Item{}, err
	}
	if auto == domain.LinkGenerating {
		row = s.enqueue(ctx, a, row)
	}
	return s.assembleOne(ctx, a, row)
}

// GenerateLink drops the current link (manual or automatic) and generates
// it again.
func (s *CollectionService) GenerateLink(ctx context.Context, a domain.Actor, id uuid.UUID) (domain.Item, error) {
	return s.Update(ctx, a, id, domain.ItemUpdate{ChangeLink: true})
}

// GeneratePending enqueues the automatic links pending or failed, and
// returns how many.
func (s *CollectionService) GeneratePending(ctx context.Context, a domain.Actor) (int, error) {
	ok, err := s.connected(ctx, a.UserID)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, domain.ErrLinkNoCredential
	}
	ids, err := s.repo.ClaimPendingLinks(ctx, a)
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	if err := s.enqueueMany(ctx, a, ids); err != nil {
		for _, id := range ids {
			if errMark := s.repo.MarkLinkStatus(ctx, a, id, domain.LinkFailed); errMark != nil {
				return 0, errors.Join(err, errMark)
			}
		}
		return 0, err
	}
	return len(ids), nil
}

// Remove deletes the item, its links and its place in the collections.
func (s *CollectionService) Remove(ctx context.Context, a domain.Actor, id uuid.UUID) error {
	err := s.repo.DeleteItem(ctx, a, id)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrItemNotFound
	}
	return err
}

// SavedProducts lists the product ids already saved, for the radar to mark.
func (s *CollectionService) SavedProducts(ctx context.Context, a domain.Actor) ([]uuid.UUID, error) {
	ids, err := s.repo.SavedProductIDs(ctx, a)
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return ids, err
}

// SetCollections replaces the collections of the item.
func (s *CollectionService) SetCollections(ctx context.Context, a domain.Actor, id uuid.UUID, collectionIDs []uuid.UUID) (domain.Item, error) {
	ids := slices.Clone(collectionIDs)
	slices.SortFunc(ids, func(x, y uuid.UUID) int { return strings.Compare(x.String(), y.String()) })
	ids = slices.Compact(ids)
	if len(ids) > maxCollectionsPerItem {
		return domain.Item{}, domain.ErrTooManyCollections
	}
	if ids == nil {
		ids = []uuid.UUID{}
	}
	var row domain.SavedItem
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if row, err = s.item(ctx, a, id); err != nil {
			return err
		}
		n, err := s.repo.CountCollections(ctx, a, ids)
		if err != nil {
			return err
		}
		if n != int64(len(ids)) {
			return domain.ErrCollectionNotFound
		}
		return s.repo.SetItemCollections(ctx, a, id, ids)
	})
	if err != nil {
		return domain.Item{}, err
	}
	return s.assembleOne(ctx, a, row)
}

// Collections lists the user's collections, in alphabetical order.
func (s *CollectionService) Collections(ctx context.Context, a domain.Actor) ([]domain.Collection, error) {
	cs, err := s.repo.Collections(ctx, a)
	if cs == nil {
		cs = []domain.Collection{}
	}
	return cs, err
}

// CreateCollection creates an empty collection.
func (s *CollectionService) CreateCollection(ctx context.Context, a domain.Actor, name string) (domain.Collection, error) {
	name, err := collectionNameOf(name)
	if err != nil {
		return domain.Collection{}, err
	}
	var c domain.Collection
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		id, err := s.repo.CreateCollection(ctx, a, name)
		if err != nil {
			return err
		}
		c, err = s.repo.Collection(ctx, a, id)
		return err
	})
	return c, err
}

// RenameCollection changes the name of the collection.
func (s *CollectionService) RenameCollection(ctx context.Context, a domain.Actor, id uuid.UUID, name string) (domain.Collection, error) {
	name, err := collectionNameOf(name)
	if err != nil {
		return domain.Collection{}, err
	}
	var c domain.Collection
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.repo.RenameCollection(ctx, a, id, name); err != nil {
			return err
		}
		var err error
		c, err = s.repo.Collection(ctx, a, id)
		return err
	})
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Collection{}, domain.ErrCollectionNotFound
	}
	return c, err
}

// DeleteCollection deletes the collection; its items stay saved.
func (s *CollectionService) DeleteCollection(ctx context.Context, a domain.Actor, id uuid.UUID) error {
	err := s.repo.DeleteCollection(ctx, a, id)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrCollectionNotFound
	}
	return err
}

// GenerateAffiliateLinks is the work of the generate_affiliate_link job: one
// link per channel, with the credential of the item's owner. An item removed
// or switched to a manual link needs nothing. Without a valid credential the
// link stays pending. On the last attempt a failure marks the link as failed.
// It returns domain.ErrSourceLimit for the caller to try later, and
// domain.ErrProductNotFound when the product left the catalog (no retry
// helps).
func (s *CollectionService) GenerateAffiliateLinks(ctx context.Context, a domain.Actor, itemID uuid.UUID, lastAttempt bool) error {
	row, err := s.repo.Item(ctx, a, itemID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.LinkOrigin == domain.LinkManual {
		return nil
	}
	prods, err := s.products.Products(ctx, []uuid.UUID{row.ProductID})
	if err != nil {
		return err
	}
	p, ok := prods[row.ProductID]
	if !ok {
		return fmt.Errorf("product %s of item %s: %w", row.ProductID, row.ID, domain.ErrProductNotFound)
	}
	log := s.log.With("item_id", itemID)

	links := make([]domain.ChannelLink, 0, len(domain.Channels))
	var main string
	for _, c := range domain.Channels {
		subIDs := domain.ChannelSubIDs(c, a.WorkspaceID)
		var link string
		if s.affiliator == nil {
			err = domain.ErrNoCredential
		} else {
			link, err = s.affiliator.GenerateLink(ctx, a.UserID, p.URL, subIDs)
		}
		switch {
		case errors.Is(err, domain.ErrNoCredential),
			errors.Is(err, domain.ErrSourceInvalidCredential),
			errors.Is(err, domain.ErrSourceAccessDenied):
			// Stays pending until the user connects (or reconnects) Shopee.
			log.Info("no valid credential; the link stays pending", "err", err)
			return s.repo.MarkLinkStatus(ctx, a, row.ID, domain.LinkPending)
		case errors.Is(err, domain.ErrSourceLimit):
			return err
		case err != nil:
			if lastAttempt {
				log.Error("the source did not generate the link; giving up", "err", err)
				if errMark := s.repo.MarkLinkStatus(ctx, a, row.ID, domain.LinkFailed); errMark != nil {
					return errors.Join(err, errMark)
				}
			}
			return err
		}
		links = append(links, domain.ChannelLink{Channel: c, SubID: domain.JoinSubIDs(subIDs), URL: link})
		if c == domain.ChannelOther {
			main = link
		}
	}
	return s.repo.CompleteAutoLink(ctx, a, row.ID, links, main)
}

// connected says whether the user has a credential to generate links.
func (s *CollectionService) connected(ctx context.Context, userID uuid.UUID) (bool, error) {
	if s.affiliator == nil {
		return false, nil
	}
	return s.affiliator.Connected(ctx, userID)
}

// initialLinkStatus of the automatic link: generating with a credential,
// pending without.
func (s *CollectionService) initialLinkStatus(ctx context.Context, userID uuid.UUID) (domain.LinkStatus, error) {
	ok, err := s.connected(ctx, userID)
	if err != nil {
		return "", err
	}
	if ok {
		return domain.LinkGenerating, nil
	}
	return domain.LinkPending, nil
}

func (s *CollectionService) enqueueMany(ctx context.Context, a domain.Actor, ids []uuid.UUID) error {
	if s.queue == nil {
		return errors.New("no affiliate link queue")
	}
	jobs := make([]domain.AffiliateLinkJob, len(ids))
	for i, id := range ids {
		jobs[i] = domain.AffiliateLinkJob{ItemID: id, WorkspaceID: a.WorkspaceID, UserID: a.UserID}
	}
	return s.queue.EnqueueAffiliateLinks(ctx, jobs...)
}

// enqueue schedules generate_affiliate_link. If the queue fails, the item is
// marked failed, for the user to ask again.
func (s *CollectionService) enqueue(ctx context.Context, a domain.Actor, row domain.SavedItem) domain.SavedItem {
	err := s.enqueueMany(ctx, a, []uuid.UUID{row.ID})
	if err == nil {
		return row
	}
	s.log.ErrorContext(ctx, "could not enqueue generate_affiliate_link", "item_id", row.ID, "err", err)
	if errMark := s.repo.MarkLinkStatus(ctx, a, row.ID, domain.LinkFailed); errMark != nil {
		s.log.ErrorContext(ctx, "could not mark the link as failed", "item_id", row.ID, "err", errMark)
		return row
	}
	row.LinkStatus = domain.LinkFailed
	return row
}

func (s *CollectionService) assembleOne(ctx context.Context, a domain.Actor, row domain.SavedItem) (domain.Item, error) {
	items, err := s.assemble(ctx, a, []domain.SavedItem{row})
	if err != nil {
		return domain.Item{}, err
	}
	return items[0], nil
}

// assemble joins to each item its product (from the catalog), its links per
// channel and its collections.
func (s *CollectionService) assemble(ctx context.Context, a domain.Actor, rows []domain.SavedItem) ([]domain.Item, error) {
	out := make([]domain.Item, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, len(rows))
	productIDs := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
		productIDs[i] = r.ProductID
	}
	prods, err := s.products.Products(ctx, productIDs)
	if err != nil {
		return nil, err
	}
	links, err := s.repo.ItemLinks(ctx, a, ids)
	if err != nil {
		return nil, err
	}
	collections, err := s.repo.ItemCollections(ctx, a, ids)
	if err != nil {
		return nil, err
	}
	for i, r := range rows {
		p, ok := prods[r.ProductID]
		if !ok {
			return nil, fmt.Errorf("product %s of item %s left the catalog", r.ProductID, r.ID)
		}
		it := domain.Item{SavedItem: r, Product: p, Links: links[r.ID], CollectionIDs: collections[r.ID]}
		if it.Tags == nil {
			it.Tags = []string{}
		}
		if it.Links == nil {
			it.Links = []domain.ChannelLink{}
		}
		if it.CollectionIDs == nil {
			it.CollectionIDs = []uuid.UUID{}
		}
		out[i] = it
	}
	return out, nil
}

// checkItemText trims a text field and checks its length.
func checkItemText(v *string, maxLen int, invalid error) (*string, error) {
	if v == nil {
		return nil, nil
	}
	t := strings.TrimSpace(*v)
	if utf8.RuneCountInString(t) > maxLen {
		return nil, invalid
	}
	return &t, nil
}

// normalizeItemTags collapses spaces, joins repeated tags (ignoring case) and
// limits count and length.
func normalizeItemTags(tags []string) ([]string, error) {
	out := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, t := range tags {
		t = strings.Join(strings.Fields(t), " ")
		if t == "" {
			continue
		}
		if utf8.RuneCountInString(t) > maxItemTag {
			return nil, domain.ErrInvalidTag
		}
		key := strings.ToLower(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	if len(out) > maxItemTags {
		return nil, domain.ErrTooManyTags
	}
	return out, nil
}

func checkAffiliateLink(v string) (string, error) {
	v = strings.TrimSpace(v)
	u, err := url.Parse(v)
	if err != nil || u.Scheme != "https" || u.Host == "" || len(v) > maxAffiliateLink {
		return "", domain.ErrInvalidAffiliateLink
	}
	return v, nil
}

func collectionNameOf(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || utf8.RuneCountInString(name) > maxCollectionName {
		return "", domain.ErrInvalidCollectionName
	}
	return name, nil
}

func truncateRunes(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	return string([]rune(s)[:maxLen])
}
