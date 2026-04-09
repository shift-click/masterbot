package providers

import "errors"

var errNotImplemented = errors.New("provider fetch is not implemented")

// Frankfurter is a stub for future forex provider.
type Frankfurter struct{}

