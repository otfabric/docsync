// Package errors is a module package with the name of a standard one: a Go
// block that calls errors.New means the standard library.
package errors

// Wrap returns err.
func Wrap(err error) error { return err }
