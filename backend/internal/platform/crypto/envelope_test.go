package crypto_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/rafixcs/app-parceiros/backend/internal/platform/crypto"
)

func novaKEK(t *testing.T, id string) *crypto.KEKLocal {
	t.Helper()
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	k, err := crypto.NovaKEKLocal(id, base64.StdEncoding.EncodeToString(b))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestEnvelope(t *testing.T) {
	ctx := context.Background()
	cofre := crypto.NovoCofre(novaKEK(t, "local-1"))
	segredo := []byte("segredo-muito-secreto")

	e, err := cofre.Cifrar(ctx, segredo, []byte("usuario-a"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(e.Cifrado, segredo) || bytes.Contains(e.DEKCifrada, segredo) {
		t.Fatal("segredo em claro no envelope")
	}
	if e.KEKID != "local-1" {
		t.Fatalf("kek id %q", e.KEKID)
	}

	got, err := cofre.Decifrar(ctx, e, []byte("usuario-a"))
	if err != nil || !bytes.Equal(got, segredo) {
		t.Fatalf("decifrar: %q %v", got, err)
	}

	t.Run("outro dono não abre", func(t *testing.T) {
		if _, err := cofre.Decifrar(ctx, e, []byte("usuario-b")); !errors.Is(err, crypto.ErrDecifrar) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("outra KEK não abre", func(t *testing.T) {
		outro := crypto.NovoCofre(novaKEK(t, "local-1"))
		if _, err := outro.Decifrar(ctx, e, []byte("usuario-a")); !errors.Is(err, crypto.ErrDecifrar) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("adulterado não abre", func(t *testing.T) {
		x := e
		x.Cifrado = append([]byte(nil), e.Cifrado...)
		x.Cifrado[len(x.Cifrado)-1] ^= 1
		if _, err := cofre.Decifrar(ctx, x, []byte("usuario-a")); !errors.Is(err, crypto.ErrDecifrar) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("DEK por segredo", func(t *testing.T) {
		e2, _ := cofre.Cifrar(ctx, segredo, []byte("usuario-a"))
		if bytes.Equal(e.DEKCifrada, e2.DEKCifrada) || bytes.Equal(e.Cifrado, e2.Cifrado) {
			t.Fatal("dois envelopes iguais")
		}
	})
}

func TestKEKLocalInvalida(t *testing.T) {
	if _, err := crypto.NovaKEKLocal("x", "curta"); err == nil {
		t.Fatal("aceitou chave inválida")
	}
}
