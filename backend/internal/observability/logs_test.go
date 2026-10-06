package observability

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCloudLoggingFieldNames(t *testing.T) {
	var buf bytes.Buffer
	NewLogger(&buf).Warn("careful", "level", "caller field")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatal(err)
	}
	if line["severity"] != "WARNING" || line["message"] != "careful" {
		t.Fatalf("line = %v", line)
	}
	// A caller attribute with the same name is not renamed.
	if line["level"] != "caller field" {
		t.Fatalf("caller attribute changed: %v", line)
	}
}
