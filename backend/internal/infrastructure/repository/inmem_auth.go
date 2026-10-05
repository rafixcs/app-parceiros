package repository

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// InMemoryAuth is the domain.AuthRepository in memory, for service tests.
type InMemoryAuth struct {
	mu       sync.Mutex
	accounts map[uuid.UUID]domain.AuthAccount
	sessions map[string]domain.AuthSession
	tokens   map[string]inMemoryToken
}

type inMemoryToken struct {
	domain.AuthToken
	used bool
}

var _ domain.AuthRepository = (*InMemoryAuth)(nil)

func NewInMemoryAuth() *InMemoryAuth {
	return &InMemoryAuth{
		accounts: map[uuid.UUID]domain.AuthAccount{},
		sessions: map[string]domain.AuthSession{},
		tokens:   map[string]inMemoryToken{},
	}
}

func (r *InMemoryAuth) CreateAccount(_ context.Context, a domain.AuthAccount) (domain.AuthAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, other := range r.accounts {
		if other.Email == a.Email {
			return domain.AuthAccount{}, domain.ErrEmailTaken
		}
	}
	a.ID = uuid.New()
	a.CreatedAt = time.Now()
	r.accounts[a.ID] = a
	return a, nil
}

func (r *InMemoryAuth) AccountByEmail(_ context.Context, email string) (domain.AuthAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.accounts {
		if a.Email == email {
			return a, nil
		}
	}
	return domain.AuthAccount{}, domain.ErrNotFound
}

func (r *InMemoryAuth) CreateSession(_ context.Context, s domain.AuthSession) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[string(s.TokenHash)] = s
	return nil
}

func (r *InMemoryAuth) SessionByHash(_ context.Context, hash []byte) (domain.AuthSession, domain.AuthAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[string(hash)]
	if !ok {
		return domain.AuthSession{}, domain.AuthAccount{}, domain.ErrNotFound
	}
	return s, r.accounts[s.AccountID], nil
}

func (r *InMemoryAuth) ExtendSession(_ context.Context, hash []byte, lastSeen, expires time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[string(hash)]
	if !ok {
		return domain.ErrNotFound
	}
	s.LastSeenAt, s.ExpiresAt = lastSeen, expires
	r.sessions[string(hash)] = s
	return nil
}

func (r *InMemoryAuth) DeleteSession(_ context.Context, hash []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sessions[string(hash)]; !ok {
		return domain.ErrNotFound
	}
	delete(r.sessions, string(hash))
	return nil
}

func (r *InMemoryAuth) CreateToken(_ context.Context, t domain.AuthToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tokens[string(t.TokenHash)] = inMemoryToken{AuthToken: t}
	return nil
}

func (r *InMemoryAuth) VerifyEmail(_ context.Context, hash []byte, now time.Time) (domain.AuthAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, err := r.useToken(hash, domain.TokenVerifyEmail, now)
	if err != nil {
		return domain.AuthAccount{}, err
	}
	if a.EmailVerifiedAt == nil {
		a.EmailVerifiedAt = &now
	}
	r.accounts[a.ID] = a
	return a, nil
}

func (r *InMemoryAuth) ResetPassword(_ context.Context, hash []byte, passwordHash string, now time.Time) (domain.AuthAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, err := r.useToken(hash, domain.TokenResetPassword, now)
	if err != nil {
		return domain.AuthAccount{}, err
	}
	a.PasswordHash = passwordHash
	if a.EmailVerifiedAt == nil {
		a.EmailVerifiedAt = &now
	}
	r.accounts[a.ID] = a
	for k, s := range r.sessions {
		if s.AccountID == a.ID {
			delete(r.sessions, k)
		}
	}
	return a, nil
}

func (r *InMemoryAuth) useToken(hash []byte, purpose domain.AuthTokenPurpose, now time.Time) (domain.AuthAccount, error) {
	t, ok := r.tokens[string(hash)]
	if !ok || t.used || t.Purpose != purpose || !now.Before(t.ExpiresAt) {
		return domain.AuthAccount{}, domain.ErrNotFound
	}
	t.used = true
	r.tokens[string(hash)] = t
	return r.accounts[t.AccountID], nil
}
