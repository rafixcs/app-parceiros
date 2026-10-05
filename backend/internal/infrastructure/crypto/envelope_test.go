package crypto_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/rafixcs/app-parceiros/backend/internal/infrastructure/crypto"
)

func newKEK(t *testing.T, id string) *crypto.LocalKEK {
	t.Helper()
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	k, err := crypto.NewLocalKEK(id, base64.StdEncoding.EncodeToString(b))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestEnvelope(t *testing.T) {
	ctx := context.Background()
	vault := crypto.NewVault(newKEK(t, "local-1"))
	secret := []byte("very-secret-value")

	e, err := vault.Encrypt(ctx, secret, []byte("user-a"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(e.Ciphertext, secret) || bytes.Contains(e.EncryptedDEK, secret) {
		t.Fatal("plaintext secret inside the envelope")
	}
	if e.KEKID != "local-1" {
		t.Fatalf("kek id %q", e.KEKID)
	}

	got, err := vault.Decrypt(ctx, e, []byte("user-a"))
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("decrypt: %q %v", got, err)
	}

	t.Run("another owner cannot open", func(t *testing.T) {
		if _, err := vault.Decrypt(ctx, e, []byte("user-b")); !errors.Is(err, crypto.ErrDecrypt) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("another KEK cannot open", func(t *testing.T) {
		other := crypto.NewVault(newKEK(t, "local-1"))
		if _, err := other.Decrypt(ctx, e, []byte("user-a")); !errors.Is(err, crypto.ErrDecrypt) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("tampered envelope does not open", func(t *testing.T) {
		x := e
		x.Ciphertext = append([]byte(nil), e.Ciphertext...)
		x.Ciphertext[len(x.Ciphertext)-1] ^= 1
		if _, err := vault.Decrypt(ctx, x, []byte("user-a")); !errors.Is(err, crypto.ErrDecrypt) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("one DEK per secret", func(t *testing.T) {
		e2, _ := vault.Encrypt(ctx, secret, []byte("user-a"))
		if bytes.Equal(e.EncryptedDEK, e2.EncryptedDEK) || bytes.Equal(e.Ciphertext, e2.Ciphertext) {
			t.Fatal("two identical envelopes")
		}
	})
}

func TestInvalidLocalKEK(t *testing.T) {
	if _, err := crypto.NewLocalKEK("x", "short"); err == nil {
		t.Fatal("accepted an invalid key")
	}
}
