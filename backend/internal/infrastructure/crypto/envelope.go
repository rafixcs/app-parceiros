// Package crypto encrypts user secrets with envelope encryption.
//
// Each secret gets its own data key (DEK), AES-256-GCM. The DEK is encrypted by
// the master key (KEK) and stored next to the encrypted secret. The KEK never
// leaves its provider: locally it is a key from the configuration and, in
// production, a cloud KMS key (see docs/stack.md). Switching providers does not
// change the format stored in the database.
package crypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// KEK encrypts and decrypts data keys. The additional data (aad) binds the DEK
// to the owner of the secret: a DEK copied to another record does not open.
type KEK interface {
	ID() string
	Encrypt(ctx context.Context, dek, aad []byte) ([]byte, error)
	Decrypt(ctx context.Context, encryptedDEK, aad []byte) ([]byte, error)
}

// Envelope is an encrypted secret ready to be stored.
type Envelope struct {
	Ciphertext   []byte // nonce || ciphertext, sealed with the DEK
	EncryptedDEK []byte // DEK sealed with the KEK
	KEKID        string
}

var ErrDecrypt = errors.New("could not decrypt secret")

// Vault encrypts and decrypts secrets with a KEK.
type Vault struct {
	kek KEK
}

func NewVault(kek KEK) *Vault { return &Vault{kek: kek} }

// Encrypt seals the secret. aad identifies the owner (e.g. the user id) and
// must be the same when decrypting.
func (v *Vault) Encrypt(ctx context.Context, secret, aad []byte) (Envelope, error) {
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return Envelope{}, err
	}
	ciphertext, err := seal(dek, secret, aad)
	if err != nil {
		return Envelope{}, err
	}
	encryptedDEK, err := v.kek.Encrypt(ctx, dek, aad)
	if err != nil {
		return Envelope{}, fmt.Errorf("encrypting data key: %w", err)
	}
	return Envelope{Ciphertext: ciphertext, EncryptedDEK: encryptedDEK, KEKID: v.kek.ID()}, nil
}

// Decrypt opens an envelope. Errors never carry any of the content.
func (v *Vault) Decrypt(ctx context.Context, e Envelope, aad []byte) ([]byte, error) {
	if e.KEKID != v.kek.ID() {
		return nil, fmt.Errorf("%w: unknown master key %q", ErrDecrypt, e.KEKID)
	}
	dek, err := v.kek.Decrypt(ctx, e.EncryptedDEK, aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	out, err := open(dek, e.Ciphertext, aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return out, nil
}

// LocalKEK is an AES-256 master key from the configuration (CRYPTO_KEK). It is
// meant for local development and tests; production uses the KMS.
type LocalKEK struct {
	id  string
	key []byte
}

// NewLocalKEK takes the key in base64 (32 bytes).
func NewLocalKEK(id, keyBase64 string) (*LocalKEK, error) {
	k, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil || len(k) != 32 {
		return nil, errors.New("CRYPTO_KEK must be a 32-byte key in base64")
	}
	if id == "" {
		return nil, errors.New("CRYPTO_KEK_ID is required")
	}
	return &LocalKEK{id: id, key: k}, nil
}

func (k *LocalKEK) ID() string { return k.id }

func (k *LocalKEK) Encrypt(_ context.Context, dek, aad []byte) ([]byte, error) {
	return seal(k.key, dek, aad)
}

func (k *LocalKEK) Decrypt(_ context.Context, encryptedDEK, aad []byte) ([]byte, error) {
	return open(k.key, encryptedDEK, aad)
}

func seal(key, plaintext, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, aad), nil
}

func open(key, sealed, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, ErrDecrypt
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ciphertext, aad)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
