package push

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/rafixcs/app-parceiros/backend/internal/domain"
)

func TestPayload(t *testing.T) {
	id := uuid.New()
	var got map[string]string
	if err := json.Unmarshal(Payload(domain.PushMessage{ID: id, Title: "Oi", Body: "Nova lista", URL: "/w/1"}), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"id": id.String(), "title": "Oi", "body": "Nova lista", "url": "/w/1"}
	if len(got) != len(want) {
		t.Fatalf("payload %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("payload %v, want %v", got, want)
		}
	}
}

// An endpoint outside the known push services is never called: it is
// reported as expired so the worker deletes it.
func TestSendRefusesUnknownHosts(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))
	defer srv.Close()
	public, private, err := GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	w := WebPush{PublicKeyB64: public, PrivateKeyB64: private, Subject: "a@example.com", HTTP: srv.Client()}
	if w.PublicKey() != public {
		t.Fatal("public key")
	}
	sub := domain.PushSubscription{Endpoint: srv.URL + "/x", P256dh: "k", Auth: "a"}
	if err := w.Send(context.Background(), sub, domain.PushMessage{Title: "x"}); !errors.Is(err, domain.ErrPushSubscriptionExpired) {
		t.Fatalf("err = %v, want expired", err)
	}
	if calls.Load() != 0 {
		t.Fatal("called an endpoint outside the push services")
	}
}
