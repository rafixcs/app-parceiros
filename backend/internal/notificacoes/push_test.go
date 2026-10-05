package notificacoes

import (
	"strings"
	"testing"
)

func TestEndpointPermitido(t *testing.T) {
	for endpoint, quer := range map[string]bool{
		"https://fcm.googleapis.com/fcm/send/abc":                 true,
		"https://updates.push.services.mozilla.com/wpush/v2/x":    true,
		"https://wns2-bl2p.notify.windows.com/w/?token=x":         true,
		"https://web.push.apple.com/QGx":                          true,
		"http://fcm.googleapis.com/fcm/send/abc":                  false,
		"https://fcm.googleapis.com:8443/x":                       false,
		"https://user@fcm.googleapis.com/x":                       false,
		"https://evil-fcm.googleapis.com.exemplo.com/x":           false,
		"https://169.254.169.254/latest/meta-data":                false,
		"https://localhost/x":                                     false,
		"https://notfcm.googleapis.com.evil/x":                    false,
		"https://fcm.googleapis.com/" + strings.Repeat("a", 1000): false,
	} {
		if got := endpointPermitido(endpoint); got != quer {
			t.Errorf("%s: %v, quer %v", endpoint, got, quer)
		}
	}
}
