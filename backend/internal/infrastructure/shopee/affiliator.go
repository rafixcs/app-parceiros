package shopee

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// UserCredentials is what the affiliator and the report need of the users'
// credentials (service.ShopeeCredentialService).
type UserCredentials interface {
	View(ctx context.Context, userID uuid.UUID) (domain.ShopeeConnection, error)
	// UserCredential decrypts the user's connected credential, or returns
	// domain.ErrNoCredential.
	UserCredential(ctx context.Context, userID uuid.UUID) (Credential, error)
	// RecordFailure marks the credential invalid or expired when Shopee
	// refused it.
	RecordFailure(ctx context.Context, userID uuid.UUID, err error) error
}

// Affiliator generates affiliate links with the credential of each user
// (domain.Affiliator).
type Affiliator struct {
	Credentials UserCredentials
	Client      *Client
}

var _ domain.Affiliator = Affiliator{}

func (a Affiliator) Connected(ctx context.Context, userID uuid.UUID) (bool, error) {
	c, err := a.Credentials.View(ctx, userID)
	if err != nil {
		return false, err
	}
	return c.Status == domain.ShopeeConnected, nil
}

// GenerateLink calls generateShortLink with the user's credential. When
// Shopee refuses the credential, it marks the connection invalid or expired,
// and returns domain.ErrSourceInvalidCredential or ErrSourceAccessDenied.
func (a Affiliator) GenerateLink(ctx context.Context, userID uuid.UUID, origin string, subIDs []string) (string, error) {
	cred, err := a.Credentials.UserCredential(ctx, userID)
	if err != nil {
		return "", err
	}
	link, err := a.Client.GenerateLink(ctx, cred, origin, subIDs)
	if err != nil {
		return "", recordFailure(ctx, a.Credentials, userID, err)
	}
	return link, nil
}

// recordFailure marks the credential when Shopee refused it, and returns err
// (joined with the failure to record, if any).
func recordFailure(ctx context.Context, creds UserCredentials, userID uuid.UUID, err error) error {
	if errors.Is(err, domain.ErrSourceInvalidCredential) || errors.Is(err, domain.ErrSourceAccessDenied) {
		if errRec := creds.RecordFailure(ctx, userID, err); errRec != nil {
			return errors.Join(err, errRec)
		}
	}
	return err
}
