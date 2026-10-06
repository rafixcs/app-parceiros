package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

var shopeeAppIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)

const shopeeMaxSecret = 256

// ShopeeCredentialService keeps and validates the Shopee Open API credential
// of each user. The Secret is stored sealed (envelope encryption) and never
// leaves this service except to call Shopee (UserCredential).
type ShopeeCredentialService struct {
	repo      domain.ShopeeCredentialRepository
	box       domain.SecretBox
	validator domain.ShopeeCredentialValidator
	now       func() time.Time
}

func NewShopeeCredentialService(repo domain.ShopeeCredentialRepository, box domain.SecretBox,
	validator domain.ShopeeCredentialValidator,
) *ShopeeCredentialService {
	return &ShopeeCredentialService{repo: repo, box: box, validator: validator, now: time.Now}
}

// shopeeAAD binds the envelope to the user: a secret copied to another row
// does not open.
func shopeeAAD(userID uuid.UUID) []byte { return []byte("shopee_credentials:" + userID.String()) }

// View returns the state of the user's connection.
func (s *ShopeeCredentialService) View(ctx context.Context, userID uuid.UUID) (domain.ShopeeConnection, error) {
	c, err := s.repo.ShopeeCredential(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ShopeeConnection{Status: domain.ShopeeDisconnected}, nil
	}
	if err != nil {
		return domain.ShopeeConnection{}, err
	}
	return shopeeConnectionOf(c), nil
}

// Connect validates the credential with a test call to Shopee and stores the
// sealed Secret. It replaces the previous credential.
func (s *ShopeeCredentialService) Connect(ctx context.Context, userID uuid.UUID, appID, secret string) (domain.ShopeeConnection, error) {
	appID = strings.TrimSpace(appID)
	secret = strings.TrimSpace(secret)
	if !shopeeAppIDPattern.MatchString(appID) {
		return domain.ShopeeConnection{}, domain.ErrInvalidAppID
	}
	if secret == "" || len(secret) > shopeeMaxSecret {
		return domain.ShopeeConnection{}, domain.ErrInvalidSecret
	}

	if err := s.validator.Validate(ctx, domain.ShopeeCredential{AppID: appID, Secret: secret}); err != nil {
		return domain.ShopeeConnection{}, shopeeValidationError(err)
	}

	sealed, err := s.box.Seal(ctx, []byte(secret), shopeeAAD(userID))
	if err != nil {
		return domain.ShopeeConnection{}, fmt.Errorf("sealing the Shopee credential: %w", err)
	}
	c, err := s.repo.SaveShopeeCredential(ctx, domain.StoredShopeeCredential{
		UserID: userID, AppID: appID, Secret: sealed, Status: domain.ShopeeConnected, VerifiedAt: s.now(),
	})
	if err != nil {
		return domain.ShopeeConnection{}, err
	}
	return shopeeConnectionOf(c), nil
}

// Disconnect deletes the credential. It is not an error when there is none.
func (s *ShopeeCredentialService) Disconnect(ctx context.Context, userID uuid.UUID) error {
	return s.repo.DeleteShopeeCredential(ctx, userID)
}

// UserCredential decrypts the user's credential for a call to Shopee (e.g.
// to generate an affiliate link). It returns domain.ErrNoCredential when
// there is no connected one. Do not keep or log the result.
func (s *ShopeeCredentialService) UserCredential(ctx context.Context, userID uuid.UUID) (domain.ShopeeCredential, error) {
	c, err := s.repo.ShopeeCredential(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ShopeeCredential{}, domain.ErrNoCredential
	}
	if err != nil {
		return domain.ShopeeCredential{}, err
	}
	if c.Status != domain.ShopeeConnected {
		return domain.ShopeeCredential{}, domain.ErrNoCredential
	}
	secret, err := s.box.Open(ctx, c.Secret, shopeeAAD(userID))
	if err != nil {
		return domain.ShopeeCredential{}, fmt.Errorf("opening the Shopee credential: %w", err)
	}
	return domain.ShopeeCredential{AppID: c.AppID, Secret: string(secret)}, nil
}

// RecordFailure marks the credential invalid or expired when a call with it
// was refused by Shopee. Other errors change nothing.
func (s *ShopeeCredentialService) RecordFailure(ctx context.Context, userID uuid.UUID, err error) error {
	var status domain.ShopeeStatus
	switch {
	case errors.Is(err, domain.ErrSourceInvalidCredential):
		status = domain.ShopeeInvalid
	case errors.Is(err, domain.ErrSourceAccessDenied):
		status = domain.ShopeeExpired
	default:
		return nil
	}
	return s.repo.SetShopeeCredentialStatus(ctx, userID, status)
}

// ConnectedUsers lists the users with a connected credential, to schedule
// the daily conversion sync. Worker only.
func (s *ShopeeCredentialService) ConnectedUsers(ctx context.Context) ([]uuid.UUID, error) {
	return s.repo.ConnectedShopeeUsers(ctx)
}

// shopeeValidationError turns the outcome of the test call into what the
// customer sees. Anything unexpected counts as Shopee unavailable.
func shopeeValidationError(err error) error {
	mapped := domain.ShopeeErrorOf(err)
	var de *domain.Error
	if errors.As(mapped, &de) {
		return mapped
	}
	return errors.Join(domain.ErrShopeeUnavailable, err)
}

func shopeeConnectionOf(c domain.StoredShopeeCredential) domain.ShopeeConnection {
	app := shopeeMaskAppID(c.AppID)
	v := c.VerifiedAt
	return domain.ShopeeConnection{Status: c.Status, AppID: &app, VerifiedAt: &v}
}

// shopeeMaskAppID shows only the last 4 digits of the AppID.
func shopeeMaskAppID(appID string) string {
	if len(appID) <= 4 {
		return "••••"
	}
	return "••••" + appID[len(appID)-4:]
}
