# API

`lib.New` returns a `Client`; `Config` has `Addr`, `Timeout` and `Retries`. `Mode` is `ModeA` or `ModeB`.
`Client.Read` reads; `client.Dial` dials. `OpError` and `ErrClosed` are errors.

`lib.NewClient` does not exist, nor `Client.Write`, nor `client.Listen`.
`Config.Retries` is promoted; `LN.DO` is IEC 61850 notation; `Server.ServerStatus.State` is a chain.

```go
func New(conf Config) (*Client, error)
func (c *Client) Read(ctx, addr uint16) ([]byte, error)
func (c *Client) Write(b []byte) error
func Open(name string) error
type Config struct {
	Addr string
	Port int
}
```

```go
Read(ctx context.Context, addr uint16) ([]byte, error)
Flush() error
```

```go
c, err := lib.New(lib.Config{Addr: "x", Port: 1})
if err != nil {
	return err
}
_ = c.Read(ctx, 1)
```

```go
lib.Config{
	Addr: ...,
	Timeout: 5,
}
```

```go
this is not go
```

```go
package main

import (
	"fmt"

	"example.com/lib"
)

func main() {
	c, _ := lib.New(lib.Config{})
	fmt.Println(c.Undocumented)
	fmt.Println(lib.Missing)
}
```

```go
type promClient struct{ n int }

func (p promClient) OnRead() {}

srv, err := lib.New(lib.Config{Addr: ...})
_, _ = srv, err
```

`errors.Wrap` returns its error. Sentinels are declared with the standard
library:

```go
var ErrXxx = errors.New("example")
```

A listing of names is checked for the names:

```go
var (
	ErrNope // not declared anywhere
)
```
