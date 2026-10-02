package shopee

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// decimal guarda um número JSON (texto ou número) sem passar por float, para
// converter dinheiro e taxas em inteiros sem erro de arredondamento.
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

var errDecimal = errors.New("número decimal inválido")

// escalar devolve o valor × 10^casas, arredondando a casa seguinte (meio para
// cima). "129.9" com 2 casas vira 12990; "0.125" com 4 casas vira 1250.
// Vazio vale 0.
func (d decimal) escalar(casas int) (int64, error) {
	s := string(d)
	if s == "" {
		return 0, nil
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	inteiro, frac, _ := strings.Cut(s, ".")
	if inteiro == "" {
		inteiro = "0"
	}
	if !soDigitos(inteiro) || !soDigitos(frac) || strings.ContainsAny(s, "eE") {
		return 0, errDecimal
	}
	arredonda := false
	if len(frac) > casas {
		arredonda = frac[casas] >= '5'
		frac = frac[:casas]
	}
	frac += strings.Repeat("0", casas-len(frac))
	v, err := strconv.ParseInt(inteiro+frac, 10, 64)
	if err != nil {
		return 0, errDecimal
	}
	if arredonda {
		v++
	}
	if neg {
		v = -v
	}
	return v, nil
}

func soDigitos(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
