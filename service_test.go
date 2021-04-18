package gorpc

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFuncToSyncService(t *testing.T) {
	f1 := func(i int, j *int) error {
		*j = i + 1
		return nil
	}
	svc, err := FuncToService(f1)
	if !assert.NoError(t, err) {
		return
	}
	i := svc.GetArg()
	*i.(*int) = 10
	ss := svc.(SyncService)
	j, err := ss.Do()
	if !assert.Equal(t, 11, *j.(*int), err) {
		return
	}
}

func TestService(t *testing.T) {
	c, s := NewTestConn()
	defer c.Close()
	client := NewClientWithCodec(newTestClientCodec(c, json.NewDecoder(c), json.NewEncoder(c)))
	handlers := NewHandlers()
	server := NewServerWithCodec(handlers, newTestServerCodec(s, json.NewDecoder(s), json.NewEncoder(s)))

	var err error
	err = handlers.Register("sync_incr", new(testSyncService))
	assert.NoError(t, err)
	err = handlers.Register("async_incr", new(testAsyncService))
	assert.NoError(t, err)

	go server.Serve()
	var rsp int
	err = client.Call("sync_incr", 10, &rsp)
	assert.Equal(t, 11, rsp, err)
	err = client.Call("async_incr", 10, &rsp)
	assert.Equal(t, 20, rsp, err)
}

func TestServiceWithContext(t *testing.T) {
	c, s := NewTestConn()
	defer c.Close()
	client := NewClientWithCodec(newTestClientCodec(c, json.NewDecoder(c), json.NewEncoder(c)))
	handlers := NewHandlers()
	server := NewServerWithCodec(handlers, newTestServerCodec(s, json.NewDecoder(s), json.NewEncoder(s)))
	ctx := 0
	server.SetContextHandler(ServerContextFunc(func(Header) interface{} {
		return &ctx
	}))
	_ = handlers.Register("incr", new(testServiceWithContext))
	_ = handlers.Register("count", new(testServiceWithContext))
	go server.Serve()
	var rsp int
	var err error
	for i := 1; i <= 3; i++ {
		err = client.Call("incr", 1, nil)
		assert.NoError(t, err)
	}
	err = client.Call("count", 0, &rsp)
	assert.Equal(t, 3, rsp, err)
}

type testSyncService struct {
	req int
}

var _ SyncService = (*testSyncService)(nil)

func (p *testSyncService) Clone() Service {
	return &testSyncService{}
}

func (p *testSyncService) GetArg() interface{} {
	return &p.req
}

func (p *testSyncService) Do() (interface{}, error) {
	return p.req + 1, nil
}

type testAsyncService struct {
	req int
}

var _ AsyncService = (*testAsyncService)(nil)

func (p *testAsyncService) Clone() Service {
	return &testAsyncService{}
}

func (p *testAsyncService) GetArg() interface{} {
	return &p.req
}

func (p *testAsyncService) Do(w ResponseWriter) {
	defer w.Free()
	w.Reply(p.req * 2)
}

type testServiceWithContext struct {
	ctx    *int
	header Header
	testSyncService
}

func (p *testServiceWithContext) Clone() Service {
	return new(testServiceWithContext)
}

func (p *testServiceWithContext) WithContext(ctx interface{}) {
	p.ctx = ctx.(*int)
}

func (p *testServiceWithContext) WithHeader(h Header) {
	p.header = h
}

func (p *testServiceWithContext) Do() (interface{}, error) {
	if p.header.Method() == "incr" {
		*p.ctx += p.req
		return *p.ctx, nil
	}
	if p.header.Method() == "count" {
		return *p.ctx, nil
	}
	return nil, os.ErrNotExist
}
