// Package domain holds the entities, the business rules that need no
// infrastructure, and the interfaces (repositories, providers) that the
// services depend on. It imports nothing else from the project.
package domain

import "errors"

// ErrNotFound is returned by repositories when a record does not exist.
var ErrNotFound = errors.New("not found")

// ErrorKind classifies a business error. The HTTP layer maps each kind to a
// status code.
type ErrorKind int

const (
	KindInvalid ErrorKind = iota + 1
	KindUnauthenticated
	KindForbidden
	KindNotFound
	KindConflict
	KindGone
	KindPaymentRequired
	KindTooManyRequests
	// KindUnavailable: a feature the server has not configured.
	KindUnavailable
	// KindUpstream: an external provider (Shopee, the payment gateway) failed.
	KindUpstream
)

// Error is a business error with a stable code. It carries no user-facing
// text: the delivery layer translates Code into the customer's language.
type Error struct {
	Kind ErrorKind
	Code string
}

func (e *Error) Error() string { return e.Code }

var registry []*Error

// NewError builds a business error and registers it, so the delivery layer
// can check that every code has a message.
func NewError(kind ErrorKind, code string) *Error {
	e := &Error{Kind: kind, Code: code}
	registry = append(registry, e)
	return e
}

// Errors returns every business error declared with NewError.
func Errors() []*Error { return append([]*Error(nil), registry...) }
