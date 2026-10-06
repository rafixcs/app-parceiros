package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
)

func TestFirstAccessCreatesPersonalWorkspace(t *testing.T) {
	a := newTestApp(t)

	u1 := a.me("ana")
	u2 := a.me("ana")
	if u1.ID != u2.ID || u1.Email != "ana@dev.local" {
		t.Fatalf("user changed between accesses: %+v %+v", u1, u2)
	}

	ws := a.workspaces("ana")
	if len(ws) != 1 || ws[0].Kind != "personal" || ws[0].Role != "owner" || ws[0].Plan != "solo" {
		t.Fatalf("workspaces = %+v, want one personal with role owner", ws)
	}
}

func TestSignedOut(t *testing.T) {
	a := newTestApp(t)
	a.mustFail("", http.MethodGet, "/v1/me", nil, http.StatusUnauthorized, "unauthenticated")
}

// Definition of done of M1: the mentor invites and the affiliate joins.
func TestMentorInvitesAffiliateJoins(t *testing.T) {
	a := newTestApp(t)

	ws := a.mentorship("mentor", "Turma de outubro")
	if ws.Kind != "mentorship" || ws.Role != "owner" || ws.Plan != "mentorship" {
		t.Fatalf("mentorship created = %+v", ws)
	}

	inv := a.invite("mentor", ws.ID, nil)
	if inv.Token == "" || inv.URL != "https://app.test/convite/"+inv.Token {
		t.Fatalf("invite without link: %+v", inv)
	}
	if d := time.Until(inv.ExpiresAt); d < 6*24*time.Hour || d > 8*24*time.Hour {
		t.Fatalf("default validity = %v, want ~7 days", d)
	}

	// Whoever gets the link sees the invite before signing in.
	var pub publicInviteJSON
	a.must("", http.MethodGet, "/v1/invites/"+inv.Token, nil, &pub, http.StatusOK)
	if pub.WorkspaceName != "Turma de outubro" || pub.Status != "valid" {
		t.Fatalf("public invite = %+v", pub)
	}

	var joined workspaceJSON
	a.must("bia", http.MethodPost, "/v1/invites/"+inv.Token+"/accept", nil, &joined, http.StatusOK)
	if joined.ID != ws.ID || joined.Role != "affiliate" {
		t.Fatalf("accept = %+v", joined)
	}

	// The affiliate sees the personal workspace and the mentorship.
	hers := a.workspaces("bia")
	if len(hers) != 2 || hers[1].ID != ws.ID || hers[1].Role != "affiliate" {
		t.Fatalf("affiliate's workspaces = %+v", hers)
	}

	var members []memberJSON
	a.must("mentor", http.MethodGet, wsPath(ws.ID, "/members"), nil, &members, http.StatusOK)
	if len(members) != 2 || members[0].Role != "owner" || members[1].Email != "bia@dev.local" {
		t.Fatalf("members = %+v", members)
	}

	// A used invite says so, for whoever tries again.
	a.must("", http.MethodGet, "/v1/invites/"+inv.Token, nil, &pub, http.StatusOK)
	if pub.Status != "used" {
		t.Fatalf("status after accepting = %q", pub.Status)
	}
	a.mustFail("caio", http.MethodPost, "/v1/invites/"+inv.Token+"/accept", nil, http.StatusGone, "invite_used")
}

func TestAffiliateDoesNotManageGroup(t *testing.T) {
	a := newTestApp(t)
	ws := a.mentorship("mentor", "Turma")
	a.join("mentor", "bia", ws.ID)

	a.mustFail("bia", http.MethodGet, wsPath(ws.ID, "/members"), nil, http.StatusForbidden, "forbidden")
	a.mustFail("bia", http.MethodPost, wsPath(ws.ID, "/invites"), map[string]any{}, http.StatusForbidden, "forbidden")
	a.mustFail("bia", http.MethodGet, wsPath(ws.ID, "/invites"), nil, http.StatusForbidden, "forbidden")
	a.mustFail("bia", http.MethodPatch, wsPath(ws.ID, ""), map[string]any{"name": "Minha"}, http.StatusForbidden, "forbidden")

	var seen workspaceJSON
	a.must("bia", http.MethodGet, wsPath(ws.ID, ""), nil, &seen, http.StatusOK)
	if seen.Name != "Turma" || seen.Role != "affiliate" {
		t.Fatalf("workspace seen by the affiliate = %+v", seen)
	}
}

func TestInvalidInvites(t *testing.T) {
	a := newTestApp(t)
	ws := a.mentorship("mentor", "Turma")

	t.Run("expired", func(t *testing.T) {
		inv := a.invite("mentor", ws.ID, nil)
		a.admin("UPDATE invites SET expires_at = now() - interval '1 minute' WHERE id = $1", inv.ID)
		var pub publicInviteJSON
		a.must("", http.MethodGet, "/v1/invites/"+inv.Token, nil, &pub, http.StatusOK)
		if pub.Status != "expired" {
			t.Fatalf("status = %q", pub.Status)
		}
		a.mustFail("bia", http.MethodPost, "/v1/invites/"+inv.Token+"/accept", nil, http.StatusGone, "invite_expired")
	})

	t.Run("revoked", func(t *testing.T) {
		inv := a.invite("mentor", ws.ID, nil)
		a.must("mentor", http.MethodDelete, wsPath(ws.ID, "/invites/"+inv.ID.String()), nil, nil, http.StatusNoContent)
		a.mustFail("bia", http.MethodPost, "/v1/invites/"+inv.Token+"/accept", nil, http.StatusGone, "invite_revoked")
		a.mustFail("mentor", http.MethodDelete, wsPath(ws.ID, "/invites/"+inv.ID.String()), nil, http.StatusNotFound, "invite_not_found")
	})

	t.Run("unknown token", func(t *testing.T) {
		fake := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		a.mustFail("", http.MethodGet, "/v1/invites/"+fake, nil, http.StatusNotFound, "invite_not_found")
		a.mustFail("bia", http.MethodPost, "/v1/invites/junk/accept", nil, http.StatusNotFound, "invite_not_found")
	})

	t.Run("other email", func(t *testing.T) {
		inv := a.invite("mentor", ws.ID, map[string]any{"email": " Duda@Dev.Local "})
		if inv.Email == nil || *inv.Email != "duda@dev.local" {
			t.Fatalf("invite email = %v", inv.Email)
		}
		a.mustFail("caio", http.MethodPost, "/v1/invites/"+inv.Token+"/accept", nil, http.StatusForbidden, "invite_other_email")
		a.must("duda", http.MethodPost, "/v1/invites/"+inv.Token+"/accept", nil, nil, http.StatusOK)
	})

	t.Run("already a member", func(t *testing.T) {
		inv := a.invite("mentor", ws.ID, nil)
		a.mustFail("mentor", http.MethodPost, "/v1/invites/"+inv.Token+"/accept", nil, http.StatusConflict, "already_member")
	})

	t.Run("personal workspace", func(t *testing.T) {
		p := a.personal("mentor")
		a.mustFail("mentor", http.MethodPost, wsPath(p.ID, "/invites"), map[string]any{}, http.StatusConflict, "mentorship_only")
	})

	t.Run("validity out of bounds", func(t *testing.T) {
		a.mustFail("mentor", http.MethodPost, wsPath(ws.ID, "/invites"), map[string]any{"validity_hours": 24 * 31},
			http.StatusUnprocessableEntity, "invalid_invite_validity")
	})
}

func TestSeatLimit(t *testing.T) {
	a := newTestApp(t)
	// During the trial the trial limit of the plan applies.
	a.admin("UPDATE plan_limits SET value = 2 WHERE plan = 'mentorship' AND key = 'trial_seats'")
	ws := a.mentorship("mentor", "Turma")

	i1 := a.invite("mentor", ws.ID, nil)
	i2 := a.invite("mentor", ws.ID, nil)
	// Pending invites hold a seat.
	a.mustFail("mentor", http.MethodPost, wsPath(ws.ID, "/invites"), map[string]any{}, http.StatusConflict, "no_seats")

	a.must("bia", http.MethodPost, "/v1/invites/"+i1.Token+"/accept", nil, nil, http.StatusOK)
	a.mustFail("mentor", http.MethodPost, wsPath(ws.ID, "/invites"), map[string]any{}, http.StatusConflict, "no_seats")

	// With seats bought, they apply. If they drop after the invite was
	// created, accepting is blocked.
	a.admin("UPDATE workspaces SET seats = 1 WHERE id = $1", ws.ID)
	a.mustFail("caio", http.MethodPost, "/v1/invites/"+i2.Token+"/accept", nil, http.StatusConflict, "no_seats")
	a.admin("UPDATE workspaces SET seats = 3 WHERE id = $1", ws.ID)
	a.must("caio", http.MethodPost, "/v1/invites/"+i2.Token+"/accept", nil, nil, http.StatusOK)
}

func TestSuspension(t *testing.T) {
	a := newTestApp(t)
	ws := a.mentorship("mentor", "Turma")
	if ws.Status != "trial" || time.Until(ws.AccessUntil) < 6*24*time.Hour || ws.Seats != nil {
		t.Fatalf("new mentorship: %+v", ws)
	}
	bia := a.join("mentor", "bia", ws.ID)

	// End of the trial without payment: suspended.
	a.admin("UPDATE workspaces SET access_until = now() - interval '1 minute' WHERE id = $1", ws.ID)
	var seen workspaceJSON
	a.must("mentor", http.MethodGet, wsPath(ws.ID, "/"), nil, &seen, http.StatusOK)
	if seen.Status != "suspended" {
		t.Fatalf("status = %q", seen.Status)
	}
	a.mustFail("mentor", http.MethodGet, wsPath(ws.ID, "/members"), nil, http.StatusPaymentRequired, "workspace_suspended_owner")
	a.mustFail("mentor", http.MethodPost, wsPath(ws.ID, "/invites"), map[string]any{}, http.StatusPaymentRequired, "workspace_suspended_owner")
	a.mustFail("bia", http.MethodPatch, wsPath(ws.ID, "/"), map[string]any{"name": "x"}, http.StatusPaymentRequired, "workspace_suspended")
	// The affiliate can still leave.
	a.must("bia", http.MethodDelete, wsPath(ws.ID, "/members/"+bia.ID.String()), nil, nil, http.StatusNoContent)

	// A payment reactivates, never shortening the access.
	ctx := context.Background()
	until := time.Now().Add(30 * 24 * time.Hour)
	seats := int32(10)
	if err := a.svcs.accounts.GrantAccess(ctx, ws.ID, until, &seats); err != nil {
		t.Fatal(err)
	}
	if err := a.svcs.accounts.GrantAccess(ctx, ws.ID, time.Now().Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	a.must("mentor", http.MethodGet, wsPath(ws.ID, "/"), nil, &seen, http.StatusOK)
	if seen.Status != "active" || seen.AccessUntil.Sub(until).Abs() > time.Second || seen.Seats == nil || *seen.Seats != 10 {
		t.Fatalf("after the payment: %+v", seen)
	}
	a.must("mentor", http.MethodGet, wsPath(ws.ID, "/members"), nil, &[]memberJSON{}, http.StatusOK)

	// A refund blocks right away.
	if err := a.svcs.accounts.RevokeAccess(ctx, ws.ID); err != nil {
		t.Fatal(err)
	}
	a.mustFail("mentor", http.MethodGet, wsPath(ws.ID, "/members"), nil, http.StatusPaymentRequired, "workspace_suspended_owner")
}

func TestSetSeats(t *testing.T) {
	a := newTestApp(t)
	a.admin("UPDATE plan_limits SET value = 4 WHERE plan = 'mentorship' AND key = 'seats'")
	ws := a.mentorship("mentor", "Turma")
	a.invite("mentor", ws.ID, nil)
	a.invite("mentor", ws.ID, nil)
	ctx := context.Background()
	svc := a.svcs.accounts
	m, err := svc.Member(ctx, a.me("mentor").ID, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := svc.SeatsInUse(ctx, m); err != nil || n != 2 {
		t.Fatalf("in use = %d, %v", n, err)
	}
	if err := svc.SetSeats(ctx, m, 1); !errors.Is(err, domain.ErrSeatsInUse) {
		t.Fatalf("below the ones in use: %v", err)
	}
	if err := svc.SetSeats(ctx, m, 5); !errors.Is(err, domain.ErrSeatsAbovePlan) {
		t.Fatalf("above the plan: %v", err)
	}
	if err := svc.SetSeats(ctx, m, 3); err != nil {
		t.Fatal(err)
	}
	w, err := svc.Workspace(ctx, m)
	if err != nil || w.Seats == nil || *w.Seats != 3 {
		t.Fatalf("seats = %v, %v", w.Seats, err)
	}
	m.Role = domain.RoleMentor
	if err := svc.SetSeats(ctx, m, 3); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("mentor: %v", err)
	}
}

func TestRemoveAndLeave(t *testing.T) {
	a := newTestApp(t)
	owner := a.me("mentor")
	ws := a.mentorship("mentor", "Turma")
	bia, caio := a.join("mentor", "bia", ws.ID), a.join("mentor", "caio", ws.ID)

	a.mustFail("bia", http.MethodDelete, wsPath(ws.ID, "/members/"+caio.ID.String()), nil, http.StatusForbidden, "forbidden")
	a.mustFail("mentor", http.MethodDelete, wsPath(ws.ID, "/members/"+owner.ID.String()), nil, http.StatusConflict, "owner_cannot_leave")

	// The affiliate leaves on her own and loses access.
	a.must("bia", http.MethodDelete, wsPath(ws.ID, "/members/"+bia.ID.String()), nil, nil, http.StatusNoContent)
	a.mustFail("bia", http.MethodGet, wsPath(ws.ID, ""), nil, http.StatusNotFound, "workspace_not_found")
	if hers := a.workspaces("bia"); len(hers) != 1 || hers[0].Kind != "personal" {
		t.Fatalf("after leaving, workspaces = %+v", hers)
	}

	// The mentor removes an affiliate.
	a.must("mentor", http.MethodDelete, wsPath(ws.ID, "/members/"+caio.ID.String()), nil, nil, http.StatusNoContent)
	a.mustFail("mentor", http.MethodDelete, wsPath(ws.ID, "/members/"+caio.ID.String()), nil, http.StatusNotFound, "member_not_found")
}

func TestUpdateWorkspace(t *testing.T) {
	a := newTestApp(t)
	ws := a.mentorship("mentor", "Turma")

	var got workspaceJSON
	a.must("mentor", http.MethodPatch, wsPath(ws.ID, ""), map[string]any{"name": "Turma VIP", "photo_url": "https://cdn.test/f.png"}, &got, http.StatusOK)
	if got.Name != "Turma VIP" || got.PhotoURL == nil || *got.PhotoURL != "https://cdn.test/f.png" {
		t.Fatalf("after the update = %+v", got)
	}
	a.must("mentor", http.MethodPatch, wsPath(ws.ID, ""), map[string]any{"photo_url": ""}, &got, http.StatusOK)
	if got.Name != "Turma VIP" || got.PhotoURL != nil {
		t.Fatalf("after removing the photo = %+v", got)
	}
	a.mustFail("mentor", http.MethodPatch, wsPath(ws.ID, ""), map[string]any{"photo_url": "javascript:alert(1)"}, http.StatusUnprocessableEntity, "invalid_photo_url")
	a.mustFail("mentor", http.MethodPost, "/v1/workspaces", map[string]any{"name": "  "}, http.StatusUnprocessableEntity, "invalid_workspace_name")
}

// Leak between workspaces, through the API: a non-member sees and changes
// nothing, and the workspace looks like it does not exist.
func TestWorkspaceLeakAPI(t *testing.T) {
	a := newTestApp(t)
	ws := a.mentorship("mentor", "Turma")
	inv := a.invite("mentor", ws.ID, nil)
	bia := a.join("mentor", "bia", ws.ID)
	a.me("intruder")

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, wsPath(ws.ID, "")},
		{http.MethodPatch, wsPath(ws.ID, "")},
		{http.MethodGet, wsPath(ws.ID, "/members")},
		{http.MethodDelete, wsPath(ws.ID, "/members/"+bia.ID.String())},
		{http.MethodGet, wsPath(ws.ID, "/invites")},
		{http.MethodPost, wsPath(ws.ID, "/invites")},
		{http.MethodDelete, wsPath(ws.ID, "/invites/"+inv.ID.String())},
		{http.MethodGet, wsPath(uuid.New(), "")},
	} {
		a.mustFail("intruder", tc.method, tc.path, map[string]any{}, http.StatusNotFound, "workspace_not_found")
	}

	if his := a.workspaces("intruder"); len(his) != 1 || his[0].Kind != "personal" {
		t.Fatalf("intruder sees other workspaces: %+v", his)
	}
}

// Leak between workspaces, in the database: even a query without a filter
// only returns rows of the transaction's scope, and writes to another
// workspace are blocked by RLS.
func TestWorkspaceLeakRLS(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	ws := a.mentorship("mentor", "Turma")
	a.invite("mentor", ws.ID, nil)
	intruder := a.me("intruder")
	scope := database.Scope{UserID: intruder.ID.String(), WorkspaceID: a.personal("intruder").ID.String()}

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

	for table, want := range map[string]int{"users": 1, "workspaces": 1, "members": 1, "invites": 0} {
		if n := count(scope, table); n != want {
			t.Errorf("%s visible to the intruder = %d, want %d", table, n, want)
		}
		if n := count(database.Scope{}, table); n != 0 {
			t.Errorf("%s visible without scope = %d, want 0", table, n)
		}
	}

	writes := map[string]func(pgx.Tx) error{
		"join as member": func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO members (workspace_id, user_id, role) VALUES ($1, $2, 'mentor')", ws.ID, intruder.ID)
			return err
		},
		"create invite": func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO invites (workspace_id, token_hash, expires_at, created_by) VALUES ($1, '\\x00', now() + interval '1 day', $2)", ws.ID, intruder.ID)
			return err
		},
		"create workspace for someone else": func(tx pgx.Tx) error {
			var owner uuid.UUID
			if err := a.pool.QueryRow(ctx, "SELECT owner_id FROM workspaces WHERE id = $1", ws.ID).Scan(&owner); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO workspaces (kind, name, owner_id, plan) VALUES ('mentorship', 'x', $1, 'mentorship')", owner)
			return err
		},
	}
	for name, write := range writes {
		err := database.InTx(ctx, a.pool, scope, write)
		if !isRLSViolation(err) {
			t.Errorf("%s: err = %v, want an RLS violation", name, err)
		}
	}

	// Updates and deletes in another workspace affect no row.
	err := database.InTx(ctx, a.pool, scope, func(tx pgx.Tx) error {
		for _, sql := range []string{
			"UPDATE workspaces SET name = 'taken' WHERE id = $1",
			"DELETE FROM members WHERE workspace_id = $1",
			"UPDATE invites SET revoked_at = now() WHERE workspace_id = $1",
		} {
			tag, err := tx.Exec(ctx, sql, ws.ID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 0 {
				return errors.New(sql + ": changed rows of another workspace")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func isRLSViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "row-level security")
}

// The personal workspace of a student of a mentorship that is up to date is
// not charged (provisional decision, docs/mvp.md §8).
func TestPersonalFreeForStudent(t *testing.T) {
	a := newTestApp(t)
	ws := a.mentorship("mentor", "Turma")
	bia := a.join("mentor", "bia", ws.ID)

	p := a.personal("bia")
	if p.Status != "free" {
		t.Fatalf("student's personal workspace = %q", p.Status)
	}
	// Even with the personal trial over, the student keeps using it.
	a.admin("UPDATE workspaces SET access_until = now() - interval '1 day' WHERE id = $1", p.ID)
	if got := a.call("bia", http.MethodGet, wsPath(p.ID, "/members"), nil, nil); got == http.StatusPaymentRequired {
		t.Fatal("student's personal workspace was suspended")
	}
	// The mentor gains nothing from it: their personal workspace is charged.
	for _, w := range a.workspaces("mentor") {
		if w.Status == "free" {
			t.Fatalf("mentor's workspace is free: %+v", w)
		}
	}

	// Mentorship suspended: the student's personal workspace depends on its
	// own access again.
	a.admin("UPDATE workspaces SET access_until = now() - interval '1 minute' WHERE id = $1", ws.ID)
	if p := a.personal("bia"); p.Status != "suspended" {
		t.Fatalf("with the mentorship suspended, personal = %q", p.Status)
	}
	// And so for whoever leaves the mentorship.
	a.admin("UPDATE workspaces SET access_until = now() + interval '1 day' WHERE id = $1", ws.ID)
	a.must("bia", http.MethodDelete, wsPath(ws.ID, "/members/"+bia.ID.String()), nil, nil, http.StatusNoContent)
	if p := a.personal("bia"); p.Status != "suspended" {
		t.Fatalf("outside the mentorship, personal = %q", p.Status)
	}
}
