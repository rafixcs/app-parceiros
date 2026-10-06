package crypto

import (
	"context"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

var _ domain.SecretBox = (*Vault)(nil)

// Seal is Encrypt for the services (domain.SecretBox).
func (v *Vault) Seal(ctx context.Context, secret, aad []byte) (domain.SealedSecret, error) {
	e, err := v.Encrypt(ctx, secret, aad)
	if err != nil {
		return domain.SealedSecret{}, err
	}
	return domain.SealedSecret{Ciphertext: e.Ciphertext, EncryptedDEK: e.EncryptedDEK, KEKID: e.KEKID}, nil
}

// Open is Decrypt for the services (domain.SecretBox).
func (v *Vault) Open(ctx context.Context, s domain.SealedSecret, aad []byte) ([]byte, error) {
	return v.Decrypt(ctx, Envelope{Ciphertext: s.Ciphertext, EncryptedDEK: s.EncryptedDEK, KEKID: s.KEKID}, aad)
}
