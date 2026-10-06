package queue_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbtest"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/queue"
)

// TestEnqueueAffiliateLinks enqueues on a real River: the same item does not
// enter twice while its job is in the queue.
func TestEnqueueAffiliateLinks(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.New(t)
	if err := queue.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	client, err := queue.NewInsertClient(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	q := &queue.River{Client: client}
	a := domain.AffiliateLinkJob{ItemID: uuid.New(), WorkspaceID: uuid.New(), UserID: uuid.New()}
	b := domain.AffiliateLinkJob{ItemID: uuid.New(), WorkspaceID: a.WorkspaceID, UserID: a.UserID}
	if err := q.EnqueueAffiliateLinks(ctx, a, b); err != nil {
		t.Fatal(err)
	}
	if err := q.EnqueueAffiliateLinks(ctx, a); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM river_job WHERE kind = 'generate_affiliate_link' AND queue = 'shopee'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("%d jobs in the queue, want 2", n)
	}
}
