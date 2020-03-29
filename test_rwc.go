package gorpc

import (
	"bytes"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

type blockReadWriteCloser struct {
	sync.Mutex
	cond      *sync.Cond
	b         bytes.Buffer
	readTimer *time.Timer
	err       error
	closed    bool
}

// NewBlockReadWriteCloser ...
func NewBlockReadWriteCloser() io.ReadWriteCloser {
	return newBlockReadWriteCloser()
}

func newBlockReadWriteCloser() *blockReadWriteCloser {
	s := &blockReadWriteCloser{}
	s.cond = sync.NewCond(&s.Mutex)
	return s
}

func (p *blockReadWriteCloser) Read(b []byte) (n int, err error) {
	p.Lock()
	defer p.Unlock()
	if p.b.Len() > 0 {
		n, err = p.b.Read(b)
		return
	}
	if p.closed {
		err = io.EOF
		return
	}
	p.cond.Wait()
	err = p.err
	p.err = nil
	return
}

func (p *blockReadWriteCloser) Write(b []byte) (n int, err error) {
	p.Lock()
	defer p.Unlock()
	if p.closed {
		err = io.ErrClosedPipe
		return
	}
	n, err = p.b.Write(b)
	p.cond.Signal()
	return
}

func (p *blockReadWriteCloser) Close() error {
	p.Lock()
	defer p.Unlock()
	p.closed = true
	p.cond.Broadcast()
	return nil
}

var (
	// ErrTimeout ...
	ErrTimeout = errors.New("timeout")
)

func (p *blockReadWriteCloser) SetReadDeadline(t time.Time) (err error) {
	if t.IsZero() && p.readTimer != nil {
		p.readTimer.Stop()
		p.readTimer = nil
		return
	}
	now := time.Now()
	sub := t.Sub(now)
	if sub <= 0 {
		return
	}
	if p.readTimer != nil {
		p.readTimer.Reset(sub)
		return
	}
	p.readTimer = time.AfterFunc(sub, func() {
		p.Lock()
		defer p.Unlock()
		p.err = ErrTimeout
		p.cond.Broadcast()
	})
	return
}

// testConn ...
type testConn struct {
	r *blockReadWriteCloser
	w *blockReadWriteCloser
}

var _ net.Conn = (*testConn)(nil)

// NewTestConn ...
func NewTestConn() (local, remote net.Conn) {
	l := newTestReadWriteClose()
	local = l
	remote = l.Remote()
	return
}

func newTestReadWriteClose() *testConn {
	s := &testConn{
		r: newBlockReadWriteCloser(),
		w: newBlockReadWriteCloser(),
	}
	return s
}

func (p *testConn) Read(b []byte) (n int, err error) {
	return p.r.Read(b)
}

func (p *testConn) Write(b []byte) (n int, err error) {
	return p.w.Write(b)
}

// Close ...
func (p *testConn) Close() error {
	p.r.Close()
	p.w.Close()
	return nil
}

func (p *testConn) Remote() *testConn {
	return &testConn{
		r: p.w,
		w: p.r,
	}
}

func (p *testConn) LocalAddr() net.Addr {
	return nil
}

func (p *testConn) RemoteAddr() net.Addr {
	return nil
}

func (p *testConn) SetDeadline(t time.Time) error {
	return nil
}

func (p *testConn) SetReadDeadline(t time.Time) error {
	return p.r.SetReadDeadline(t)
}

func (p *testConn) SetWriteDeadline(t time.Time) error {
	return nil
}
