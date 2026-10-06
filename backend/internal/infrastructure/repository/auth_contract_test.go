package repository_test

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

// runAuthContract exercises an AuthRepository through the internal provider.
// Every implementation runs the same contract.
func runAuthContract(t *testing.T, repo domain.AuthRepository) {
	ctx := context.Background()
	box := &mailbox{verify: map[string]string{}, reset: map[string]string{}}
	svc, err := service.NewInternalAuth(repo, auth.Argon2id{Memory: 1024, Time: 1, Threads: 1}, box,
		service.AuthLimits{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	sess, err := svc.Register(ctx, service.RegisterInput{Name: " Ana ", Email: " Ana@Example.com ", Password: "secret-123"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := svc.Authenticate(ctx, sess.Token)
	if err != nil {
		t.Fatal(err)
	}
	if id.Provider != domain.AuthProviderInternal || id.Email != "ana@example.com" || id.Name != "Ana" || id.EmailVerified {
		t.Fatalf("identity after sign-up: %+v", id)
	}

	if _, err := svc.Register(ctx, service.RegisterInput{Name: "Ana", Email: "ana@example.com", Password: "secret-123"}); !errors.Is(err, domain.ErrEmailTaken) {
		t.Fatalf("duplicate email: %v", err)
	}
	if _, err := svc.Login(ctx, "ana@example.com", "wrong-pass", ""); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := svc.Login(ctx, "nobody@example.com", "secret-123", ""); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("unknown email: %v", err)
	}

	// Email verification, once.
	token := box.verify["ana@example.com"]
	if token == "" {
		t.Fatal("no verification email")
	}
	if err := svc.VerifyEmail(ctx, token); err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyEmail(ctx, token); !errors.Is(err, domain.ErrInvalidAuthToken) {
		t.Fatalf("token reused: %v", err)
	}
	if id, _ := svc.Authenticate(ctx, sess.Token); !id.EmailVerified {
		t.Fatal("email still unverified")
	}

	// Sign-in, sign-out.
	second, err := svc.Login(ctx, "ANA@example.com", "secret-123", "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Logout(ctx, second.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, second.Token); !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("session after logout: %v", err)
	}
	if err := svc.Logout(ctx, second.Token); err != nil {
		t.Fatalf("logout twice: %v", err)
	}

	// Password reset ends every session.
	if err := svc.RequestPasswordReset(ctx, "nobody@example.com"); err != nil {
		t.Fatalf("reset for unknown email: %v", err)
	}
	if err := svc.RequestPasswordReset(ctx, "ana@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ResetPassword(ctx, box.reset["ana@example.com"], "short"); !errors.Is(err, domain.ErrWeakPassword) {
		t.Fatalf("weak password: %v", err)
	}
	if err := svc.ResetPassword(ctx, box.reset["ana@example.com"], "new-secret-456"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, sess.Token); !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("session after reset: %v", err)
	}
	if _, err := svc.Login(ctx, "ana@example.com", "secret-123", ""); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("old password after reset: %v", err)
	}
	if _, err := svc.Login(ctx, "ana@example.com", "new-secret-456", ""); err != nil {
		t.Fatalf("new password: %v", err)
	}
	if _, err := svc.Authenticate(ctx, "garbage"); !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("garbage token: %v", err)
	}
}

func TestInMemoryAuth(t *testing.T) {
	runAuthContract(t, repository.NewInMemoryAuth())
}
