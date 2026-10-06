-- Accounts: users, workspaces, members, invites and plan limits.

-- name: UserByIdentity :one
SELECT * FROM users WHERE auth_provider = $1 AND auth_subject = $2;

-- name: UpsertUser :one
-- Creates the user on first access or updates name and email. `created` says
-- whether the row was just inserted.
INSERT INTO users (auth_provider, auth_subject, name, email, email_verified)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (auth_provider, auth_subject) DO UPDATE
    SET name = EXCLUDED.name,
        email = EXCLUDED.email,
        email_verified = EXCLUDED.email_verified
RETURNING id, (xmax = 0)::boolean AS created;

-- name: UserByID :one
SELECT * FROM users WHERE id = $1;

-- name: CreateWorkspace :one
INSERT INTO workspaces (kind, name, photo_url, owner_id, plan)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: WorkspaceByID :one
SELECT * FROM workspaces WHERE id = $1;

-- name: LockWorkspace :one
-- Locks the workspace row to serialize the seat count.
SELECT * FROM workspaces WHERE id = $1 FOR UPDATE;

-- name: UpdateWorkspace :one
UPDATE workspaces
SET name = coalesce(sqlc.narg(name), name),
    photo_url = CASE WHEN sqlc.arg(clear_photo)::boolean THEN NULL
                     ELSE coalesce(sqlc.narg(photo_url), photo_url) END
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: UserWorkspaces :many
SELECT w.*, m.role
FROM members m
JOIN workspaces w ON w.id = m.workspace_id
WHERE m.user_id = $1
ORDER BY w.kind, w.created_at;

-- name: InsertMember :exec
INSERT INTO members (workspace_id, user_id, role)
VALUES ($1, $2, $3);

-- name: MemberByUser :one
SELECT * FROM members WHERE workspace_id = $1 AND user_id = $2;

-- name: WorkspaceMembers :many
SELECT m.user_id, u.name, u.email, m.role, m.joined_at, m.shares_results
FROM members m
JOIN users u ON u.id = m.user_id
WHERE m.workspace_id = $1
ORDER BY m.role, m.joined_at;

-- name: DeleteMember :execrows
DELETE FROM members WHERE workspace_id = $1 AND user_id = $2;

-- name: CountAffiliates :one
SELECT count(*) FROM members WHERE workspace_id = $1 AND role = 'affiliate';

-- name: CountPendingInvites :one
SELECT count(*) FROM invites
WHERE workspace_id = $1
  AND used_by IS NULL
  AND revoked_at IS NULL
  AND expires_at > now();

-- name: PlanLimit :one
SELECT value FROM plan_limits WHERE plan = $1 AND key = $2;

-- name: CreateInvite :one
INSERT INTO invites (workspace_id, email, token_hash, expires_at, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: PendingInvites :many
SELECT * FROM invites
WHERE workspace_id = $1
  AND used_by IS NULL
  AND revoked_at IS NULL
  AND expires_at > now()
ORDER BY created_at DESC;

-- name: InviteByHash :one
SELECT * FROM invites WHERE token_hash = $1;

-- name: LockInviteByHash :one
SELECT * FROM invites WHERE token_hash = $1 FOR UPDATE;

-- name: MarkInviteUsed :exec
UPDATE invites SET used_by = $2, used_at = now() WHERE id = $1;

-- name: RevokeInvite :execrows
UPDATE invites SET revoked_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND used_by IS NULL
  AND revoked_at IS NULL;

-- name: SetSharesResults :execrows
UPDATE members SET shares_results = @shares_results
WHERE workspace_id = @workspace_id AND user_id = @user_id;

-- name: GrantAccess :one
-- Extends the access until `until` (never shortens it) and records the
-- payment. Null seats keep the ones bought.
UPDATE workspaces
SET access_until = greatest(access_until, @until::timestamptz),
    paid_at = now(),
    seats = coalesce(sqlc.narg(seats), seats)
WHERE id = @id
RETURNING *;

-- name: RevokeAccess :execrows
UPDATE workspaces SET access_until = least(access_until, now()) WHERE id = $1;

-- name: SetSeats :exec
UPDATE workspaces SET seats = $2 WHERE id = $1;

-- name: IsStudentOfActiveMentorship :one
-- Whether the user is an affiliate of a mentorship with access up to date.
-- While they are, their personal workspace is not charged.
SELECT EXISTS (
    SELECT 1
    FROM members m
    JOIN workspaces w ON w.id = m.workspace_id
    WHERE m.user_id = $1
      AND m.role = 'affiliate'
      AND w.kind = 'mentorship'
      AND w.access_until > now()
)::boolean AS student;
