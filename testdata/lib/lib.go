// Package lib is a docsync test fixture.
package lib

import (
	"context"
	"errors"
)

// ErrClosed is documented.
var ErrClosed = errors.New("closed")

// ErrMissing is not in ERRORS.md.
var ErrMissing = errors.New("missing")

// Mode is an enum type: its values need no entries of their own.
type Mode int

const (
	ModeA Mode = iota
	ModeB
)

// MaxSize is untyped and undocumented.
const MaxSize = 10

// Config configures a Client.
type Config struct {
	Addr    string
	Timeout int
	Inner
}

// Inner is embedded in Config.
type Inner struct{ Retries int }

// Client is the client.
type Client struct{}

// New returns a client.
func New(conf Config) (*Client, error) { return &Client{}, nil }

// Read reads.
func (c *Client) Read(ctx context.Context, addr uint16) ([]byte, error) { return nil, nil }

// Undocumented is not in API.md.
func (c *Client) Undocumented() {}

// String needs no entry.
func (c *Client) String() string { return "" }

// OpError is an error type.
type OpError struct{ Op string }

func (e *OpError) Error() string { return e.Op }
