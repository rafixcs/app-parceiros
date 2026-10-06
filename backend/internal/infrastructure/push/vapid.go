// Package push sends Web Push notifications, signed with VAPID and encrypted
// as RFC 8291 requires.
package push

import "github.com/SherClockHolmes/webpush-go"

// GenerateVAPIDKeys creates a new key pair (base64 url, without padding).
func GenerateVAPIDKeys() (public, private string, err error) {
	private, public, err = webpush.GenerateVAPIDKeys()
	return public, private, err
}
