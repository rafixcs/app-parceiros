package domain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ShopeeStatus is the state of a user's connection to the Shopee affiliate
// Open API.
type ShopeeStatus string

const (
	ShopeeDisconnected ShopeeStatus = "disconnected"
	ShopeeConnected    ShopeeStatus = "connected"
	// ShopeeInvalid: Shopee refused the credential (signature).
	ShopeeInvalid ShopeeStatus = "invalid"
	// ShopeeExpired: Shopee denied access (blocked account, revoked
	// permission).
	ShopeeExpired ShopeeStatus = "expired"
)

// ShopeeCredential is the AppID and Secret of an affiliate account. The
// Secret never goes to a log, an error or an answer: String and GoString hide
// it.
type ShopeeCredential struct {
	AppID  string
	Secret string
}

func (c ShopeeCredential) String() string {
	return "ShopeeCredential{AppID:" + c.AppID + ", Secret:[hidden]}"
}

func (c ShopeeCredential) GoString() string { return c.String() }

// ShopeeConnection is what the user sees of their credential. It never holds
// the Secret, and the AppID comes masked ("••••1234").
type ShopeeConnection struct {
	Status     ShopeeStatus
	AppID      *string
	VerifiedAt *time.Time
}

// SealedSecret is a secret under envelope encryption: sealed with a data key
// (DEK), and the DEK sealed with the master key KEKID.
type SealedSecret struct {
	Ciphertext   []byte
	EncryptedDEK []byte
	KEKID        string
}

// SecretBox encrypts customer secrets. aad binds the envelope to its owner:
// the same aad must be given to open it.
type SecretBox interface {
	Seal(ctx context.Context, secret, aad []byte) (SealedSecret, error)
	Open(ctx context.Context, s SealedSecret, aad []byte) ([]byte, error)
}

// StoredShopeeCredential is a credential as kept in the database.
type StoredShopeeCredential struct {
	UserID     uuid.UUID
	AppID      string
	Secret     SealedSecret
	Status     ShopeeStatus
	VerifiedAt time.Time
}

// ShopeeCredentialRepository keeps the credential of each user. Every call
// but ConnectedShopeeUsers runs as the user, who sees only their own row.
type ShopeeCredentialRepository interface {
	// ShopeeCredential returns ErrNotFound when the user has none.
	ShopeeCredential(ctx context.Context, userID uuid.UUID) (StoredShopeeCredential, error)
	// SaveShopeeCredential replaces the user's credential, as connected.
	SaveShopeeCredential(ctx context.Context, c StoredShopeeCredential) (StoredShopeeCredential, error)
	DeleteShopeeCredential(ctx context.Context, userID uuid.UUID) error
	SetShopeeCredentialStatus(ctx context.Context, userID uuid.UUID, status ShopeeStatus) error
	// ConnectedShopeeUsers lists the users with a connected credential. Only
	// the worker uses it (owner role), and it returns only ids.
	ConnectedShopeeUsers(ctx context.Context) ([]uuid.UUID, error)
}

// ShopeeCredentialValidator makes a test call with a credential. It returns
// nil when Shopee accepts it, or one of the ErrSource* errors.
type ShopeeCredentialValidator interface {
	Validate(ctx context.Context, cred ShopeeCredential) error
}

var (
	ErrInvalidShopeeCredential = NewError(KindInvalid, "invalid_shopee_credential")
	ErrShopeeAccessDenied      = NewError(KindInvalid, "shopee_access_denied")
	ErrShopeeRateLimited       = NewError(KindTooManyRequests, "shopee_rate_limited")
	ErrShopeeUnavailable       = NewError(KindUpstream, "shopee_unavailable")
	ErrInvalidAppID            = NewError(KindInvalid, "invalid_app_id")
	ErrInvalidSecret           = NewError(KindInvalid, "invalid_secret")
)

// ShopeeErrorOf turns an error of the source into the business error the
// customer sees: refused credential, access denied, rate limit or Shopee
// unavailable (keeping the cause, for the log). Other errors pass through.
func ShopeeErrorOf(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrSourceInvalidCredential):
		return ErrInvalidShopeeCredential
	case errors.Is(err, ErrSourceAccessDenied):
		return ErrShopeeAccessDenied
	case errors.Is(err, ErrSourceLimit):
		return ErrShopeeRateLimited
	case errors.Is(err, ErrSourceUnavailable):
		return errors.Join(ErrShopeeUnavailable, err)
	default:
		return err
	}
}
