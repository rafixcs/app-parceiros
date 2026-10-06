package auth

import (
	"strings"
	"testing"
)

func TestArgon2id(t *testing.T) {
	h := Argon2id{Memory: 1024, Time: 1, Threads: 1}
	hash, err := h.Hash("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=1024,t=1,p=1$") {
		t.Fatalf("hash %q", hash)
	}
	if ok, err := h.Verify(hash, "correct horse"); err != nil || !ok {
		t.Fatalf("right password: %v %v", ok, err)
	}
	if ok, _ := h.Verify(hash, "wrong horse"); ok {
		t.Fatal("wrong password accepted")
	}
	// Hashes made with other parameters still verify.
	if ok, _ := DefaultArgon2id.Verify(hash, "correct horse"); !ok {
		t.Fatal("hash with older parameters rejected")
	}
	if other, _ := h.Hash("correct horse"); other == hash {
		t.Fatal("same salt twice")
	}
	if _, err := h.Verify("plain", "x"); err == nil {
		t.Fatal("malformed hash accepted")
	}
}
