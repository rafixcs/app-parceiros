package repository

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/database/dbgen"
)

// PostgresAccount is the domain.AccountRepository on Postgres.
type PostgresAccount struct {
	pool *pgxpool.Pool
}

var _ domain.AccountRepository = (*PostgresAccount)(nil)

func NewPostgresAccount(pool *pgxpool.Pool) *PostgresAccount { return &PostgresAccount{pool: pool} }

func (r *PostgresAccount) UserByIdentity(ctx context.Context, provider, subject string) (domain.User, error) {
	var u dbgen.User
	s := database.Scope{AuthProvider: provider, AuthSubject: subject}
	err := run(ctx, r.pool, s, func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		u, err = q.UserByIdentity(ctx, dbgen.UserByIdentityParams{AuthProvider: provider, AuthSubject: subject})
		return err
	})
	return userOf(u), notFound(err)
}

func (r *PostgresAccount) SaveUser(ctx context.Context, nu domain.NewUser) (domain.User, bool, error) {
	var u dbgen.User
	var created bool
	s := database.Scope{AuthProvider: nu.AuthProvider, AuthSubject: nu.AuthSubject}
	err := run(ctx, r.pool, s, func(q *dbgen.Queries, tx pgx.Tx) error {
		row, err := q.UpsertUser(ctx, dbgen.UpsertUserParams{
			AuthProvider:  nu.AuthProvider,
			AuthSubject:   nu.AuthSubject,
			Name:          nu.Name,
			Email:         nu.Email,
			EmailVerified: nu.EmailVerified,
		})
		if err != nil {
			return err
		}
		created = row.Created
		u, err = q.UserByIdentity(ctx, dbgen.UserByIdentityParams{AuthProvider: nu.AuthProvider, AuthSubject: nu.AuthSubject})
		return err
	})
	return userOf(u), created, err
}

func (r *PostgresAccount) UserByID(ctx context.Context, a domain.Actor, id uuid.UUID) (domain.User, error) {
	var u dbgen.User
	err := run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		u, err = q.UserByID(ctx, id)
		return err
	})
	return userOf(u), notFound(err)
}

func (r *PostgresAccount) CreateWorkspace(ctx context.Context, nw domain.NewWorkspace) (domain.Workspace, error) {
	var w dbgen.Workspace
	err := run(ctx, r.pool, userScope(nw.OwnerID), func(q *dbgen.Queries, tx pgx.Tx) error {
		var err error
		w, err = q.CreateWorkspace(ctx, dbgen.CreateWorkspaceParams{
			Kind:     dbgen.WorkspaceKind(nw.Kind),
			Name:     nw.Name,
			PhotoURL: nw.PhotoURL,
			OwnerID:  nw.OwnerID,
			Plan:     nw.Plan,
		})
		if err != nil {
			return err
		}
		if err := database.SetScope(ctx, tx, scopeOf(domain.Actor{UserID: nw.OwnerID, WorkspaceID: w.ID})); err != nil {
			return err
		}
		return q.InsertMember(ctx, dbgen.InsertMemberParams{
			WorkspaceID: w.ID, UserID: nw.OwnerID, Role: dbgen.MemberRoleOwner,
		})
	})
	return workspaceOf(w), err
}

func (r *PostgresAccount) UserWorkspaces(ctx context.Context, userID uuid.UUID) ([]domain.Membership, error) {
	var out []domain.Membership
	err := run(ctx, r.pool, userScope(userID), func(q *dbgen.Queries, _ pgx.Tx) error {
		rows, err := q.UserWorkspaces(ctx, userID)
		if err != nil {
			return err
		}
		out = make([]domain.Membership, 0, len(rows))
		for _, r := range rows {
			out = append(out, domain.Membership{
				Workspace: workspaceOf(dbgen.Workspace{
					ID: r.ID, Kind: r.Kind, Name: r.Name, PhotoURL: r.PhotoURL, OwnerID: r.OwnerID, Plan: r.Plan,
					AccessUntil: r.AccessUntil, PaidAt: r.PaidAt, Seats: r.Seats, CreatedAt: r.CreatedAt,
				}),
				Role: domain.Role(r.Role),
			})
		}
		return nil
	})
	return out, err
}

func (r *PostgresAccount) IsStudent(ctx context.Context, userID uuid.UUID) (bool, error) {
	var student bool
	err := run(ctx, r.pool, userScope(userID), func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		student, err = q.IsStudentOfActiveMentorship(ctx, userID)
		return err
	})
	return student, err
}

func (r *PostgresAccount) Membership(ctx context.Context, userID, workspaceID uuid.UUID) (domain.Membership, error) {
	var out domain.Membership
	err := run(ctx, r.pool, userScope(userID), func(q *dbgen.Queries, _ pgx.Tx) error {
		m, err := q.MemberByUser(ctx, dbgen.MemberByUserParams{WorkspaceID: workspaceID, UserID: userID})
		if err != nil {
			return err
		}
		w, err := q.WorkspaceByID(ctx, workspaceID)
		if err != nil {
			return err
		}
		out = domain.Membership{Workspace: workspaceOf(w), Role: domain.Role(m.Role), SharesResults: m.SharesResults}
		return nil
	})
	return out, notFound(err)
}

func (r *PostgresAccount) Workspace(ctx context.Context, a domain.Actor) (domain.Workspace, error) {
	var w dbgen.Workspace
	err := run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		w, err = q.WorkspaceByID(ctx, a.WorkspaceID)
		return err
	})
	return workspaceOf(w), notFound(err)
}

func (r *PostgresAccount) LockWorkspace(ctx context.Context, a domain.Actor) (domain.Workspace, error) {
	var w dbgen.Workspace
	err := run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		w, err = q.LockWorkspace(ctx, a.WorkspaceID)
		return err
	})
	return workspaceOf(w), notFound(err)
}

func (r *PostgresAccount) UpdateWorkspace(ctx context.Context, a domain.Actor, u domain.WorkspaceUpdate) (domain.Workspace, error) {
	var w dbgen.Workspace
	err := run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		w, err = q.UpdateWorkspace(ctx, dbgen.UpdateWorkspaceParams{
			ID: a.WorkspaceID, Name: u.Name, PhotoURL: u.PhotoURL, ClearPhoto: u.ClearPhoto,
		})
		return err
	})
	return workspaceOf(w), notFound(err)
}

func (r *PostgresAccount) GrantAccess(ctx context.Context, workspaceID uuid.UUID, until time.Time, seats *int32) error {
	return run(ctx, r.pool, workspaceScope(workspaceID), func(q *dbgen.Queries, _ pgx.Tx) error {
		_, err := q.GrantAccess(ctx, dbgen.GrantAccessParams{ID: workspaceID, Until: until, Seats: seats})
		return notFound(err)
	})
}

func (r *PostgresAccount) RevokeAccess(ctx context.Context, workspaceID uuid.UUID) error {
	return run(ctx, r.pool, workspaceScope(workspaceID), func(q *dbgen.Queries, _ pgx.Tx) error {
		_, err := q.RevokeAccess(ctx, workspaceID)
		return err
	})
}

func (r *PostgresAccount) SetSeats(ctx context.Context, a domain.Actor, seats int32) error {
	return run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		return q.SetSeats(ctx, dbgen.SetSeatsParams{ID: a.WorkspaceID, Seats: &seats})
	})
}

// PlanLimit reads a limit of the plan. A limit missing from the table is zero.
func (r *PostgresAccount) PlanLimit(ctx context.Context, plan, key string) (int64, error) {
	var v int64
	err := run(ctx, r.pool, database.Scope{}, func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		v, err = q.PlanLimit(ctx, dbgen.PlanLimitParams{Plan: plan, Key: key})
		if errors.Is(err, pgx.ErrNoRows) {
			v, err = 0, nil
		}
		return err
	})
	return v, err
}

func (r *PostgresAccount) Members(ctx context.Context, a domain.Actor) ([]domain.MemberDetail, error) {
	var out []domain.MemberDetail
	err := run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		rows, err := q.WorkspaceMembers(ctx, a.WorkspaceID)
		if err != nil {
			return err
		}
		out = make([]domain.MemberDetail, 0, len(rows))
		for _, r := range rows {
			out = append(out, domain.MemberDetail{
				UserID: r.UserID, Name: r.Name, Email: r.Email, Role: domain.Role(r.Role),
				JoinedAt: r.JoinedAt, SharesResults: r.SharesResults,
			})
		}
		return nil
	})
	return out, err
}

func (r *PostgresAccount) MemberRole(ctx context.Context, a domain.Actor, userID uuid.UUID) (domain.Role, error) {
	var m dbgen.Member
	err := run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		m, err = q.MemberByUser(ctx, dbgen.MemberByUserParams{WorkspaceID: a.WorkspaceID, UserID: userID})
		return err
	})
	return domain.Role(m.Role), notFound(err)
}

func (r *PostgresAccount) AddMember(ctx context.Context, a domain.Actor, userID uuid.UUID, role domain.Role) error {
	return run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		return q.InsertMember(ctx, dbgen.InsertMemberParams{
			WorkspaceID: a.WorkspaceID, UserID: userID, Role: dbgen.MemberRole(role),
		})
	})
}

func (r *PostgresAccount) RemoveMember(ctx context.Context, a domain.Actor, userID uuid.UUID) error {
	return run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		n, err := q.DeleteMember(ctx, dbgen.DeleteMemberParams{WorkspaceID: a.WorkspaceID, UserID: userID})
		if err == nil && n == 0 {
			return domain.ErrNotFound
		}
		return err
	})
}

func (r *PostgresAccount) SetSharesResults(ctx context.Context, a domain.Actor, shares bool) error {
	return run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		n, err := q.SetSharesResults(ctx, dbgen.SetSharesResultsParams{
			SharesResults: shares, WorkspaceID: a.WorkspaceID, UserID: a.UserID,
		})
		if err == nil && n == 0 {
			return domain.ErrNotFound
		}
		return err
	})
}

func (r *PostgresAccount) CountAffiliates(ctx context.Context, a domain.Actor) (int64, error) {
	var n int64
	err := run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		n, err = q.CountAffiliates(ctx, a.WorkspaceID)
		return err
	})
	return n, err
}

func (r *PostgresAccount) CreateInvite(ctx context.Context, a domain.Actor, ni domain.NewInvite) (domain.Invite, error) {
	var i dbgen.Invite
	err := run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		i, err = q.CreateInvite(ctx, dbgen.CreateInviteParams{
			WorkspaceID: a.WorkspaceID, Email: ni.Email, TokenHash: ni.TokenHash,
			ExpiresAt: ni.ExpiresAt, CreatedBy: ni.CreatedBy,
		})
		return err
	})
	return inviteOf(i), err
}

func (r *PostgresAccount) PendingInvites(ctx context.Context, a domain.Actor) ([]domain.Invite, error) {
	var out []domain.Invite
	err := run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		rows, err := q.PendingInvites(ctx, a.WorkspaceID)
		if err != nil {
			return err
		}
		out = make([]domain.Invite, 0, len(rows))
		for _, r := range rows {
			out = append(out, inviteOf(r))
		}
		return nil
	})
	return out, err
}

func (r *PostgresAccount) CountPendingInvites(ctx context.Context, a domain.Actor) (int64, error) {
	var n int64
	err := run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		n, err = q.CountPendingInvites(ctx, a.WorkspaceID)
		return err
	})
	return n, err
}

func (r *PostgresAccount) RevokeInvite(ctx context.Context, a domain.Actor, id uuid.UUID) error {
	return run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		n, err := q.RevokeInvite(ctx, dbgen.RevokeInviteParams{ID: id, WorkspaceID: a.WorkspaceID})
		if err == nil && n == 0 {
			return domain.ErrNotFound
		}
		return err
	})
}

func (r *PostgresAccount) InviteByHash(ctx context.Context, a domain.Actor, hash []byte, lock bool) (domain.Invite, error) {
	s := scopeOf(a)
	s.InviteHash = hex.EncodeToString(hash)
	var i dbgen.Invite
	err := run(ctx, r.pool, s, func(q *dbgen.Queries, _ pgx.Tx) error {
		var err error
		if lock {
			i, err = q.LockInviteByHash(ctx, hash)
		} else {
			i, err = q.InviteByHash(ctx, hash)
		}
		return err
	})
	return inviteOf(i), notFound(err)
}

func (r *PostgresAccount) MarkInviteUsed(ctx context.Context, a domain.Actor, inviteID, userID uuid.UUID) error {
	return run(ctx, r.pool, scopeOf(a), func(q *dbgen.Queries, _ pgx.Tx) error {
		return q.MarkInviteUsed(ctx, dbgen.MarkInviteUsedParams{ID: inviteID, UsedBy: &userID})
	})
}

func userOf(u dbgen.User) domain.User {
	return domain.User{ID: u.ID, Name: u.Name, Email: u.Email, EmailVerified: u.EmailVerified, CreatedAt: u.CreatedAt}
}

func workspaceOf(w dbgen.Workspace) domain.Workspace {
	return domain.Workspace{
		ID: w.ID, Kind: domain.WorkspaceKind(w.Kind), Name: w.Name, PhotoURL: w.PhotoURL, OwnerID: w.OwnerID,
		Plan: w.Plan, AccessUntil: w.AccessUntil, PaidAt: w.PaidAt, Seats: w.Seats, CreatedAt: w.CreatedAt,
	}
}

func inviteOf(i dbgen.Invite) domain.Invite {
	return domain.Invite{
		ID: i.ID, WorkspaceID: i.WorkspaceID, Email: i.Email, ExpiresAt: i.ExpiresAt,
		UsedBy: i.UsedBy, RevokedAt: i.RevokedAt, CreatedAt: i.CreatedAt,
	}
}
