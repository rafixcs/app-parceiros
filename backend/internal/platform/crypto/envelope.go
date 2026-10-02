// Package crypto cifra segredos de usuário com envelope encryption.
//
// Cada segredo ganha uma chave de dados (DEK) própria, AES-256-GCM. A DEK é
// cifrada pela chave mestra (KEK) e guardada ao lado do segredo cifrado. A KEK
// nunca sai do provedor: localmente é uma chave da configuração e, em
// produção, uma chave do KMS da cloud (a escolher; veja docs/stack.md). Trocar
// o provedor não muda o formato guardado no banco.
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

// KEK cifra e decifra chaves de dados. O contexto (aad) amarra a DEK ao dono
// do segredo: uma DEK copiada para outro registro não abre.
type KEK interface {
	ID() string
	Cifrar(ctx context.Context, dek, aad []byte) ([]byte, error)
	Decifrar(ctx context.Context, dekCifrada, aad []byte) ([]byte, error)
}

// Envelope é um segredo cifrado pronto para guardar.
type Envelope struct {
	Cifrado    []byte // nonce || ciphertext, com a DEK
	DEKCifrada []byte // DEK cifrada pela KEK
	KEKID      string
}

var ErrDecifrar = errors.New("não foi possível decifrar o segredo")

// Cofre cifra e decifra segredos com uma KEK.
type Cofre struct {
	kek KEK
}

func NovoCofre(kek KEK) *Cofre { return &Cofre{kek: kek} }

// Cifrar cifra o segredo. aad identifica o dono (ex.: o id do usuário) e
// precisa ser o mesmo ao decifrar.
func (c *Cofre) Cifrar(ctx context.Context, segredo, aad []byte) (Envelope, error) {
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return Envelope{}, err
	}
	cifrado, err := selar(dek, segredo, aad)
	if err != nil {
		return Envelope{}, err
	}
	dekCifrada, err := c.kek.Cifrar(ctx, dek, aad)
	if err != nil {
		return Envelope{}, fmt.Errorf("cifrando a chave de dados: %w", err)
	}
	return Envelope{Cifrado: cifrado, DEKCifrada: dekCifrada, KEKID: c.kek.ID()}, nil
}

// Decifrar abre um envelope. Erros não carregam nada do conteúdo.
func (c *Cofre) Decifrar(ctx context.Context, e Envelope, aad []byte) ([]byte, error) {
	if e.KEKID != c.kek.ID() {
		return nil, fmt.Errorf("%w: chave mestra %q desconhecida", ErrDecifrar, e.KEKID)
	}
	dek, err := c.kek.Decifrar(ctx, e.DEKCifrada, aad)
	if err != nil {
		return nil, ErrDecifrar
	}
	out, err := abrir(dek, e.Cifrado, aad)
	if err != nil {
		return nil, ErrDecifrar
	}
	return out, nil
}

// KEKLocal é uma chave mestra AES-256 vinda da configuração (CRYPTO_KEK).
// Serve para o ambiente local e para os testes; em produção use o KMS.
type KEKLocal struct {
	id    string
	chave []byte
}

// NovaKEKLocal recebe a chave em base64 (32 bytes).
func NovaKEKLocal(id, chaveBase64 string) (*KEKLocal, error) {
	k, err := base64.StdEncoding.DecodeString(chaveBase64)
	if err != nil || len(k) != 32 {
		return nil, errors.New("CRYPTO_KEK deve ser uma chave de 32 bytes em base64")
	}
	if id == "" {
		return nil, errors.New("CRYPTO_KEK_ID é obrigatório")
	}
	return &KEKLocal{id: id, chave: k}, nil
}

func (k *KEKLocal) ID() string { return k.id }

func (k *KEKLocal) Cifrar(_ context.Context, dek, aad []byte) ([]byte, error) {
	return selar(k.chave, dek, aad)
}

func (k *KEKLocal) Decifrar(_ context.Context, dekCifrada, aad []byte) ([]byte, error) {
	return abrir(k.chave, dekCifrada, aad)
}

func selar(chave, texto, aad []byte) ([]byte, error) {
	gcm, err := novoGCM(chave)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, texto, aad), nil
}

func abrir(chave, selado, aad []byte) ([]byte, error) {
	gcm, err := novoGCM(chave)
	if err != nil {
		return nil, err
	}
	if len(selado) < gcm.NonceSize() {
		return nil, ErrDecifrar
	}
	nonce, cifrado := selado[:gcm.NonceSize()], selado[gcm.NonceSize():]
	return gcm.Open(nil, nonce, cifrado, aad)
}

func novoGCM(chave []byte) (cipher.AEAD, error) {
	bloco, err := aes.NewCipher(chave)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(bloco)
}
