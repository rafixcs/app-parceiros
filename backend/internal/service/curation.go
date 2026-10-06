package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// curationFormerMember is the name shown on the dashboard for an importer who
// left the group (customer text, pt-BR).
const curationFormerMember = "Ex-membro"

// curationProducts reads the catalog (service.ProductService).
type curationProducts interface {
	Products(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]domain.Product, error)
}

// curationCollections is the collection of each member
// (service.CollectionService): curation only changes it through here.
type curationCollections interface {
	ResolveProduct(ctx context.Context, productID *uuid.UUID, link string) (domain.Product, error)
	ItemsOfProducts(ctx context.Context, a domain.Actor, productIDs []uuid.UUID) (map[uuid.UUID]domain.Item, error)
	Import(ctx context.Context, a domain.Actor, items []domain.ImportedItem, collectionName string) (domain.ImportResult, error)
}

// curationAccounts reads the group and the plan (service.AccountService).
type curationAccounts interface {
	Members(ctx context.Context, m domain.Member) ([]domain.MemberDetail, error)
	Workspace(ctx context.Context, m domain.Member) (domain.WorkspaceView, error)
	PlanLimit(ctx context.Context, m domain.Member, key string) (int64, error)
}

// curationMedia are the videos of lists and products (service.MediaService).
type curationMedia interface {
	ForTargets(ctx context.Context, m domain.Member, target domain.VideoTarget, ids []uuid.UUID) (map[uuid.UUID][]domain.VideoView, error)
	LinkList(ctx context.Context, m domain.Member, id, listID uuid.UUID) error
	UnlinkList(ctx context.Context, m domain.Member, id, listID uuid.UUID) error
	UnlinkTarget(ctx context.Context, m domain.Member, target domain.VideoTarget, targetID uuid.UUID) error
}

// CurationService handles the lists a mentor builds for the group: the
// products, a comment per product, the publication (which notifies the
// members) and the import, in which the affiliate takes the list, all of it
// or part, into their own collection and gets the links with their own
// credential.
//
// Drafts only show to owner and mentors. The affiliate's collection belongs to
// the collections module, and curation changes it only through it.
type CurationService struct {
	repo        domain.CurationRepository
	tx          domain.Transactor
	products    curationProducts
	collections curationCollections
	accounts    curationAccounts
	notifier    domain.Notifier
	media       curationMedia
	log         *slog.Logger
}

func NewCurationService(repo domain.CurationRepository, tx domain.Transactor, products curationProducts,
	collections curationCollections, accounts curationAccounts, notifier domain.Notifier, media curationMedia,
	log *slog.Logger,
) *CurationService {
	return &CurationService{
		repo: repo, tx: tx, products: products, collections: collections, accounts: accounts,
		notifier: notifier, media: media, log: log,
	}
}

func requireCurator(m domain.Member) error {
	if m.WorkspaceKind != domain.WorkspaceMentorship {
		return domain.ErrListsMentorshipOnly
	}
	if !m.Role.Manages() {
		return domain.ErrListManagersOnly
	}
	return nil
}

func curationNotFound(err, notFound error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return notFound
	}
	return err
}

// Lists returns the lists of the workspace: the drafts first (only for owner
// and mentors), then the published ones, the newest first.
func (s *CurationService) Lists(ctx context.Context, m domain.Member) ([]domain.CuratedList, error) {
	manager := m.Role.Manages()
	ls, err := s.repo.Lists(ctx, m.Actor(), manager)
	if err != nil {
		return nil, err
	}
	out := make([]domain.CuratedList, len(ls))
	for i, l := range ls {
		out[i] = curatedListFor(l, manager)
	}
	return out, nil
}

// curatedListFor hides the import counts from who does not manage the group.
func curatedListFor(l domain.CuratedList, manager bool) domain.CuratedList {
	if !manager {
		l.Importers = nil
	}
	return l
}

// Create creates an empty draft, within the plan's limit of lists.
func (s *CurationService) Create(ctx context.Context, m domain.Member, title, description string) (domain.CuratedListDetail, error) {
	if err := requireCurator(m); err != nil {
		return domain.CuratedListDetail{}, err
	}
	t, err := checkListTitle(title)
	if err != nil {
		return domain.CuratedListDetail{}, err
	}
	d, err := checkListText(description, domain.MaxListDescription, domain.ErrInvalidListDesc)
	if err != nil {
		return domain.CuratedListDetail{}, err
	}
	limit, err := s.accounts.PlanLimit(ctx, m, domain.LimitLists)
	if err != nil {
		return domain.CuratedListDetail{}, err
	}
	var id uuid.UUID
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		n, err := s.repo.CountLists(ctx, m.Actor())
		if err != nil {
			return err
		}
		if n >= limit {
			return domain.ErrListLimit
		}
		id, err = s.repo.CreateList(ctx, m.Actor(), t, d)
		return err
	})
	if err != nil {
		return domain.CuratedListDetail{}, err
	}
	return s.Get(ctx, m, id)
}

// Get returns the list with its products. Each product already saved in the
// viewer's collection comes with their affiliate link.
func (s *CurationService) Get(ctx context.Context, m domain.Member, id uuid.UUID) (domain.CuratedListDetail, error) {
	manager := m.Role.Manages()
	a := m.Actor()
	var out domain.CuratedListDetail
	var items []domain.CuratedListItem
	importers := map[uuid.UUID]int64{}
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		l, err := s.repo.List(ctx, a, id, manager)
		if err != nil {
			return curationNotFound(err, domain.ErrListNotFound)
		}
		out.CuratedList = curatedListFor(l, manager)
		if items, err = s.repo.ListItems(ctx, a, id); err != nil {
			return err
		}
		if manager {
			importers, err = s.repo.ImportersByProduct(ctx, a, id)
		}
		return err
	})
	if err != nil {
		return domain.CuratedListDetail{}, err
	}

	ids := make([]uuid.UUID, len(items))
	for i, it := range items {
		ids[i] = it.ProductID
	}
	products, err := s.products.Products(ctx, ids)
	if err != nil {
		return domain.CuratedListDetail{}, err
	}
	mine, err := s.collections.ItemsOfProducts(ctx, a, ids)
	if err != nil {
		return domain.CuratedListDetail{}, err
	}
	productVideos, err := s.media.ForTargets(ctx, m, domain.TargetProduct, ids)
	if err != nil {
		return domain.CuratedListDetail{}, err
	}
	listVideos, err := s.media.ForTargets(ctx, m, domain.TargetList, []uuid.UUID{id})
	if err != nil {
		return domain.CuratedListDetail{}, err
	}
	out.Videos = nonNilVideos(listVideos[id])
	out.Items = make([]domain.ListItemView, 0, len(items))
	for _, it := range items {
		p, ok := products[it.ProductID]
		if !ok {
			return domain.CuratedListDetail{}, fmt.Errorf("product %s of list %s is missing from the catalog", it.ProductID, id)
		}
		v := domain.ListItemView{
			Product: p, Comment: it.Comment, Position: it.Position, Videos: nonNilVideos(productVideos[it.ProductID]),
		}
		if manager {
			n := importers[it.ProductID]
			v.Importers = &n
		}
		if my, ok := mine[it.ProductID]; ok {
			v.MyItem = &my
		}
		out.Items = append(out.Items, v)
	}
	return out, nil
}

func nonNilVideos(vs []domain.VideoView) []domain.VideoView {
	if vs == nil {
		return []domain.VideoView{}
	}
	return vs
}

// Update changes title and description (nil does not change).
func (s *CurationService) Update(ctx context.Context, m domain.Member, id uuid.UUID, title, description *string) (domain.CuratedListDetail, error) {
	if err := requireCurator(m); err != nil {
		return domain.CuratedListDetail{}, err
	}
	if title != nil {
		t, err := checkListTitle(*title)
		if err != nil {
			return domain.CuratedListDetail{}, err
		}
		title = &t
	}
	if description != nil {
		d, err := checkListText(*description, domain.MaxListDescription, domain.ErrInvalidListDesc)
		if err != nil {
			return domain.CuratedListDetail{}, err
		}
		description = &d
	}
	if err := s.repo.UpdateList(ctx, m.Actor(), id, title, description); err != nil {
		return domain.CuratedListDetail{}, curationNotFound(err, domain.ErrListNotFound)
	}
	return s.Get(ctx, m, id)
}

// Delete deletes the list. The items the affiliates imported stay in their
// collections, and the attached videos stay in the library of who sent them.
func (s *CurationService) Delete(ctx context.Context, m domain.Member, id uuid.UUID) error {
	if err := requireCurator(m); err != nil {
		return err
	}
	if err := s.repo.DeleteList(ctx, m.Actor(), id); err != nil {
		return curationNotFound(err, domain.ErrListNotFound)
	}
	if err := s.media.UnlinkTarget(ctx, m, domain.TargetList, id); err != nil {
		s.log.ErrorContext(ctx, "deleted list kept video links", "list_id", id, "err", err)
	}
	return nil
}

// AttachVideo attaches a video of the editor's library to the list. The
// video becomes shared with the group.
func (s *CurationService) AttachVideo(ctx context.Context, m domain.Member, id, videoID uuid.UUID) (domain.CuratedListDetail, error) {
	if err := s.edit(ctx, m, id, func(context.Context) error { return nil }); err != nil {
		return domain.CuratedListDetail{}, err
	}
	if err := s.media.LinkList(ctx, m, videoID, id); err != nil {
		return domain.CuratedListDetail{}, err
	}
	return s.Get(ctx, m, id)
}

// DetachVideo removes the video from the list; it stays in the library of
// who sent it.
func (s *CurationService) DetachVideo(ctx context.Context, m domain.Member, id, videoID uuid.UUID) (domain.CuratedListDetail, error) {
	if err := s.edit(ctx, m, id, func(context.Context) error { return nil }); err != nil {
		return domain.CuratedListDetail{}, err
	}
	err := s.media.UnlinkList(ctx, m, videoID, id)
	if errors.Is(err, domain.ErrVideoNotFound) {
		return domain.CuratedListDetail{}, domain.ErrVideoNotInList
	}
	if err != nil {
		return domain.CuratedListDetail{}, err
	}
	return s.Get(ctx, m, id)
}

// edit locks the list and runs fn, checking that it exists. Only owner and
// mentors.
func (s *CurationService) edit(ctx context.Context, m domain.Member, id uuid.UUID, fn func(ctx context.Context) error) error {
	if err := requireCurator(m); err != nil {
		return err
	}
	a := m.Actor()
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.repo.LockList(ctx, a, id); err != nil {
			return curationNotFound(err, domain.ErrListNotFound)
		}
		if err := fn(ctx); err != nil {
			return err
		}
		return s.repo.TouchList(ctx, a, id)
	})
}

// AddProduct puts a product at the end of the list: from the radar
// (productID) or pasted by its Shopee link, as in the collection.
func (s *CurationService) AddProduct(ctx context.Context, m domain.Member, id uuid.UUID, productID *uuid.UUID, link, comment string) (domain.CuratedListDetail, error) {
	if err := requireCurator(m); err != nil {
		return domain.CuratedListDetail{}, err
	}
	c, err := checkListText(comment, domain.MaxListComment, domain.ErrInvalidListComment)
	if err != nil {
		return domain.CuratedListDetail{}, err
	}
	p, err := s.collections.ResolveProduct(ctx, productID, link)
	if err != nil {
		return domain.CuratedListDetail{}, err
	}
	err = s.edit(ctx, m, id, func(ctx context.Context) error {
		items, err := s.repo.ListItems(ctx, m.Actor(), id)
		if err != nil {
			return err
		}
		if len(items) >= domain.MaxListItems {
			return domain.ErrListFull
		}
		added, err := s.repo.AddListItem(ctx, m.Actor(), id, p.ID, c)
		if err == nil && !added {
			return domain.ErrAlreadyInList
		}
		return err
	})
	if err != nil {
		return domain.CuratedListDetail{}, err
	}
	return s.Get(ctx, m, id)
}

// Comment replaces the mentor's comment on a product of the list.
func (s *CurationService) Comment(ctx context.Context, m domain.Member, id, productID uuid.UUID, comment string) (domain.CuratedListDetail, error) {
	c, err := checkListText(comment, domain.MaxListComment, domain.ErrInvalidListComment)
	if err != nil {
		return domain.CuratedListDetail{}, err
	}
	err = s.edit(ctx, m, id, func(ctx context.Context) error {
		return curationNotFound(s.repo.CommentListItem(ctx, m.Actor(), id, productID, c), domain.ErrListItemNotFound)
	})
	if err != nil {
		return domain.CuratedListDetail{}, err
	}
	return s.Get(ctx, m, id)
}

// RemoveProduct takes a product out of the list.
func (s *CurationService) RemoveProduct(ctx context.Context, m domain.Member, id, productID uuid.UUID) (domain.CuratedListDetail, error) {
	err := s.edit(ctx, m, id, func(ctx context.Context) error {
		return curationNotFound(s.repo.RemoveListItem(ctx, m.Actor(), id, productID), domain.ErrListItemNotFound)
	})
	if err != nil {
		return domain.CuratedListDetail{}, err
	}
	return s.Get(ctx, m, id)
}

// Reorder takes every product of the list, in the new order.
func (s *CurationService) Reorder(ctx context.Context, m domain.Member, id uuid.UUID, productIDs []uuid.UUID) (domain.CuratedListDetail, error) {
	err := s.edit(ctx, m, id, func(ctx context.Context) error {
		items, err := s.repo.ListItems(ctx, m.Actor(), id)
		if err != nil {
			return err
		}
		if !sameProducts(items, productIDs) {
			return domain.ErrInvalidListOrder
		}
		return s.repo.ReorderListItems(ctx, m.Actor(), id, productIDs)
	})
	if err != nil {
		return domain.CuratedListDetail{}, err
	}
	return s.Get(ctx, m, id)
}

// Publish shows the list to the group and notifies each member (except who
// published) in the inbox, by email and by push. Publishing again does not
// notify again.
func (s *CurationService) Publish(ctx context.Context, m domain.Member, id uuid.UUID) (domain.CuratedListDetail, error) {
	var published bool
	err := s.edit(ctx, m, id, func(ctx context.Context) error {
		items, err := s.repo.ListItems(ctx, m.Actor(), id)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return domain.ErrListEmpty
		}
		published, err = s.repo.PublishList(ctx, m.Actor(), id)
		return err
	})
	if err != nil {
		return domain.CuratedListDetail{}, err
	}
	l, err := s.Get(ctx, m, id)
	if err != nil {
		return domain.CuratedListDetail{}, err
	}
	if published {
		// The list is already published; if the notice fails, the group still
		// sees it in the app.
		if err := s.notifyGroup(ctx, m, l); err != nil {
			s.log.ErrorContext(ctx, "could not notify the group of the published list", "list_id", id, "err", err)
		}
	}
	return l, nil
}

func (s *CurationService) notifyGroup(ctx context.Context, m domain.Member, l domain.CuratedListDetail) error {
	members, err := s.accounts.Members(ctx, m)
	if err != nil {
		return err
	}
	ws, err := s.accounts.Workspace(ctx, m)
	if err != nil {
		return err
	}
	var ids []uuid.UUID
	for _, mb := range members {
		if mb.UserID != m.UserID {
			ids = append(ids, mb.UserID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	body := fmt.Sprintf("%s publicou %s para você divulgar.", ws.Name, curationPlural(len(l.Items), "produto", "produtos"))
	if d := strings.TrimSpace(l.Description); d != "" {
		body += " " + d
	}
	return s.notifier.Notify(ctx, domain.Notice{
		WorkspaceID: m.WorkspaceID, UserIDs: ids, Kind: domain.NoticeListPublished,
		Key: "list:" + l.ID.String(), Title: "Nova lista: " + l.Title, Body: body,
		URL: "/w/" + m.WorkspaceID.String() + "/listas/" + l.ID.String(),
	})
}

// Import takes products of a published list into the importer's collection
// (all of them when productIDs is empty). With intoCollection, the items also
// go to a collection named after the list. The links come out with the
// user's own credential.
func (s *CurationService) Import(ctx context.Context, m domain.Member, id uuid.UUID, productIDs []uuid.UUID, intoCollection bool) (domain.ImportResult, error) {
	l, err := s.Get(ctx, m, id)
	if err != nil {
		return domain.ImportResult{}, err
	}
	if l.PublishedAt == nil {
		return domain.ImportResult{}, domain.ErrListNotPublished
	}
	chosen := map[uuid.UUID]bool{}
	for _, p := range productIDs {
		chosen[p] = true
	}
	var items []domain.ImportedItem
	for _, it := range l.Items {
		if len(productIDs) == 0 || chosen[it.Product.ID] {
			items = append(items, domain.ImportedItem{ProductID: it.Product.ID, Comment: it.Comment})
			delete(chosen, it.Product.ID)
		}
	}
	if len(chosen) > 0 {
		return domain.ImportResult{}, domain.ErrProductNotInList
	}
	if len(items) == 0 {
		return domain.ImportResult{}, domain.ErrListEmpty
	}
	name := ""
	if intoCollection {
		name = l.Title
	}
	res, err := s.collections.Import(ctx, m.Actor(), items, name)
	if err != nil {
		return res, err
	}
	ids := make([]uuid.UUID, len(items))
	for i, it := range items {
		ids[i] = it.ProductID
	}
	return res, s.repo.RecordImports(ctx, m.Actor(), id, ids)
}

// Dashboard shows the mentor who of the group imported the list.
func (s *CurationService) Dashboard(ctx context.Context, m domain.Member, id uuid.UUID) (domain.ListDashboard, error) {
	if err := requireCurator(m); err != nil {
		return domain.ListDashboard{}, err
	}
	var rows []domain.ListImporter
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if _, err := s.repo.List(ctx, m.Actor(), id, true); err != nil {
			return curationNotFound(err, domain.ErrListNotFound)
		}
		var err error
		rows, err = s.repo.Importers(ctx, m.Actor(), id)
		return err
	})
	if err != nil {
		return domain.ListDashboard{}, err
	}
	members, err := s.accounts.Members(ctx, m)
	if err != nil {
		return domain.ListDashboard{}, err
	}
	names := map[uuid.UUID]string{}
	out := domain.ListDashboard{Importers: []domain.ListImporter{}}
	for _, mb := range members {
		names[mb.UserID] = mb.Name
		if mb.Role == domain.RoleAffiliate {
			out.Affiliates++
		}
	}
	for _, r := range rows {
		name, ok := names[r.UserID]
		if !ok {
			name = curationFormerMember
		}
		r.Name = name
		out.Importers = append(out.Importers, r)
	}
	return out, nil
}

// Imports returns the published lists of the workspace, the newest first,
// with the imports of each. Only owner and mentors see it; the group's
// results dashboard uses it to add up per list.
func (s *CurationService) Imports(ctx context.Context, m domain.Member) ([]domain.ImportedList, error) {
	if err := requireCurator(m); err != nil {
		return nil, err
	}
	var lists []domain.CuratedList
	var imports []domain.PublishedListImport
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if lists, err = s.repo.Lists(ctx, m.Actor(), false); err != nil {
			return err
		}
		imports, err = s.repo.PublishedImports(ctx, m.Actor())
		return err
	})
	if err != nil {
		return nil, err
	}
	byList := map[uuid.UUID][]domain.ListImport{}
	for _, i := range imports {
		byList[i.ListID] = append(byList[i.ListID], i.ListImport)
	}
	out := make([]domain.ImportedList, 0, len(lists))
	for _, l := range lists {
		out = append(out, domain.ImportedList{ID: l.ID, Title: l.Title, PublishedAt: l.PublishedAt, Imports: byList[l.ID]})
	}
	return out, nil
}

// sameProducts says whether ids are exactly the products of the list, each
// once.
func sameProducts(items []domain.CuratedListItem, ids []uuid.UUID) bool {
	if len(items) != len(ids) {
		return false
	}
	a := make([]string, len(ids))
	b := make([]string, len(items))
	for i := range ids {
		a[i], b[i] = ids[i].String(), items[i].ProductID.String()
	}
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b) // b has no repeats: product_id is a key of the list
}

func checkListTitle(t string) (string, error) {
	t = strings.Join(strings.Fields(t), " ")
	if t == "" || utf8.RuneCountInString(t) > domain.MaxListTitle {
		return "", domain.ErrInvalidListTitle
	}
	return t, nil
}

func checkListText(v string, maxLen int, invalid error) (string, error) {
	v = strings.TrimSpace(v)
	if utf8.RuneCountInString(v) > maxLen {
		return "", invalid
	}
	return v, nil
}

func curationPlural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
