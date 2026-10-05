package logs

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNomesDoCloudLogging(t *testing.T) {
	var buf bytes.Buffer
	New(&buf).Warn("cuidado", "level", "campo do usuário")

	var linha map[string]any
	if err := json.Unmarshal(buf.Bytes(), &linha); err != nil {
		t.Fatal(err)
	}
	if linha["severity"] != "WARNING" || linha["message"] != "cuidado" {
		t.Fatalf("linha = %v", linha)
	}
	// Um atributo do chamador com o mesmo nome não é renomeado.
	if linha["level"] != "campo do usuário" {
		t.Fatalf("atributo do chamador mudou: %v", linha)
	}
}
