package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

// Argon2id hashes passwords with argon2id, in the PHC string format
// ($argon2id$v=19$m=...,t=...,p=...$salt$hash), so the parameters can grow
// later without invalidating stored hashes.
type Argon2id struct {
	Memory  uint32 // KiB
	Time    uint32
	Threads uint8
}

var _ domain.PasswordHasher = Argon2id{}

// DefaultArgon2id follows the OWASP recommendation (19 MiB, 2 passes).
var DefaultArgon2id = Argon2id{Memory: 19 * 1024, Time: 2, Threads: 1}

const (
	saltLen = 16
	keyLen  = 32
)

func (a Argon2id) Hash(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, a.Time, a.Memory, a.Threads, keyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, a.Memory, a.Time, a.Threads, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

var errBadHash = errors.New("malformed argon2id hash")

func (Argon2id) Verify(hash, password string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errBadHash
	}
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, errBadHash
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return false, errBadHash
	}
	want, err := enc.DecodeString(parts[5])
	if err != nil {
		return false, errBadHash
	}
	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
