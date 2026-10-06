// Package httputil holds the JSON helpers shared by the HTTP handlers.
package httputil

import (
	"encoding/json"
	"net/http"
)

// JSON writes v as the JSON response body.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ErrorBody is the standard body of the API error responses. Code is stable
// and meant for programs; Message is ready to show to the user, in pt-BR.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error writes an error response in the standard format.
func Error(w http.ResponseWriter, status int, code, message string) {
	JSON(w, status, ErrorBody{Code: code, Message: message})
}

// DecodeJSON reads the request body into v, refusing unknown fields and
// bodies larger than maxBytes.
func DecodeJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
