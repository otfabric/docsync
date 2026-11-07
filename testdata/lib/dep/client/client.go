// Package client is a dependency's package with the name of a local one.
package client

// HandshakeError is the dependency's error.
type HandshakeError struct{}

func (HandshakeError) Error() string { return "handshake" }
