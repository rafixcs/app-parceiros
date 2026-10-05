package http

import (
	"testing"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

func TestEveryDomainErrorHasMessage(t *testing.T) {
	for _, e := range domain.Errors() {
		if _, ok := messages[e.Code]; !ok {
			t.Errorf("error code %q has no pt-BR message", e.Code)
		}
		if _, ok := statusByKind[e.Kind]; !ok {
			t.Errorf("error code %q has a kind with no HTTP status", e.Code)
		}
	}
}
