package shopee

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// decimal keeps a JSON number (text or number) without going through float,
// to turn money and rates into integers with no rounding error.
type decimal string

func (d *decimal) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		*d = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*d = decimal(strings.TrimSpace(s))
		return nil
	}
	*d = decimal(b)
	return nil
}

var errDecimal = errors.New("invalid decimal number")

// scale returns the value × 10^places, rounding on the next digit (half up).
// "129.9" with 2 places is 12990; "0.125" with 4 places is 1250. Empty is 0.
func (d decimal) scale(places int) (int64, error) {
	s := string(d)
	if s == "" {
		return 0, nil
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	if !digitsOnly(whole) || !digitsOnly(frac) || strings.ContainsAny(s, "eE") {
		return 0, errDecimal
	}
	roundUp := false
	if len(frac) > places {
		roundUp = frac[places] >= '5'
		frac = frac[:places]
	}
	frac += strings.Repeat("0", places-len(frac))
	v, err := strconv.ParseInt(whole+frac, 10, 64)
	if err != nil {
		return 0, errDecimal
	}
	if roundUp {
		v++
	}
	if neg {
		v = -v
	}
	return v, nil
}

func digitsOnly(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
