package gorpc

import (
	"fmt"
	"io"
)

var (
	// ErrClosed ...
	ErrClosed = fmt.Errorf("closed: [%w]", io.ErrClosedPipe)
)
