package gorpc

import (
	"fmt"
	"io"
)

var (
	ErrClosed  = fmt.Errorf("%w closed", io.ErrClosedPipe)
	ErrNoSpace = fmt.Errorf("gorpc: client no space")
)
