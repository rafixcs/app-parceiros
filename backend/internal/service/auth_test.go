package service_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/auth"
	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/repository"
	"github.com/rafixcs/app-parceiros/backend/internal/service"
)

// mailbox keeps the tokens the internal provider would send by email.
type mailbox struct {
	mu     sync.Mutex
	verify map[string]string
	reset  map[string]string
}

func (m *mailbox) SendEmailVerification(_ context.Context, to, _, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.verify[to] = token
	return nil
}

func (m *mailbox) SendPasswordReset(_ context.Context, to, _, token string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reset[to] = token
	return nil
}

// budget allows n attempts per key.
type budget struct {
	mu   sync.Mutex
	n    int
	used map[string]int
}

func (b *budget) Allow(_ context.Context, key string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.used[key]++
	return b.used[key] <= b.n, nil
}

var fastHasher = auth.Argon2id{Memory: 1024, Time: 1, Threads: 1}

func newInternalAuth(t *testing.T, repo domain.AuthRepository, limits service.AuthLimits) (*service.InternalAuth, *mailbox) {
	t.Helper()
	box := &mailbox{verify: map[string]string{}, reset: map[string]string{}}
	svc, err := service.NewInternalAuth(repo, fastHasher, box, limits, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return svc, box
}

func TestInternalAuthValidation(t *testing.T) {
	svc, _ := newInternalAuth(t, repository.NewInMemoryAuth(), service.AuthLimits{})
	ctx := context.Background()
	for name, tc := range map[string]struct {
		in   service.RegisterInput
		want error
	}{
		"bad email":      {service.RegisterInput{Name: "Ana", Email: "ana", Password: "secret-123"}, domain.ErrInvalidEmail},
		"display name":   {service.RegisterInput{Name: "Ana", Email: "Ana <ana@example.com>", Password: "secret-123"}, domain.ErrInvalidEmail},
		"empty name":     {service.RegisterInput{Name: "  ", Email: "ana@example.com", Password: "secret-123"}, domain.ErrInvalidName},
		"short password": {service.RegisterInput{Name: "Ana", Email: "ana@example.com", Password: "1234567"}, domain.ErrWeakPassword},
	} {
		if _, err := svc.Register(ctx, tc.in); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
}

func TestInternalAuthRateLimit(t *testing.T) {
	ctx := context.Background()
	limits := service.AuthLimits{Account: &budget{n: 3, used: map[string]int{}}, IP: &budget{n: 100, used: map[string]int{}}}
	svc, _ := newInternalAuth(t, repository.NewInMemoryAuth(), limits)
	if _, err := svc.Register(ctx, service.RegisterInput{Name: "Ana", Email: "ana@example.com", Password: "secret-123"}); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := svc.Login(ctx, "ana@example.com", "wrong-pass", "10.0.0.1"); !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("attempt: %v", err)
		}
	}
	if _, err := svc.Login(ctx, "ana@example.com", "secret-123", "10.0.0.1"); !errors.Is(err, domain.ErrTooManyAttempts) {
		t.Fatalf("fourth attempt: %v", err)
	}
}
