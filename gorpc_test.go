package gorpc

import (
	"encoding/json"
	"io"
	"log"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

type testReq struct {
	Seq    int         `json:"seq"`
	Method string      `json:"method"`
	Data   interface{} `json:"data"`
}

type testRsp struct {
	Seq  int         `json:"seq"`
	Data interface{} `json:"data"`
}

// [:4] - id, end with 0x0
type testClientCodec struct {
	rwc io.ReadWriteCloser
	de  *json.Decoder
	en  *json.Encoder
	buf json.RawMessage
	seq uint32
}

func newTestClientCodec(rwc io.ReadWriteCloser, de *json.Decoder, en *json.Encoder) *testClientCodec {
	return &testClientCodec{
		rwc: rwc,
		de:  de,
		en:  en,
	}
}

func (p *testClientCodec) Close() error {
	return p.rwc.Close()
}

func (p *testClientCodec) GetSeq(head Header) (seq interface{}) {
	seq = atomic.AddUint32(&p.seq, 1)
	return
}

func (p *testClientCodec) WriteRequest(req Header, args interface{}) (err error) {
	var data testReq
	data.Seq = int(req.Seq().(uint32))
	data.Method = req.Method().(string)
	data.Data = args
	err = p.en.Encode(data)
	return
}

func (p *testClientCodec) ReadHeader(rsp Header) (err error) {
	p.buf = p.buf[:0]
	var data testRsp
	data.Data = &p.buf
	err = p.de.Decode(&data)
	if err != nil {
		return
	}
	rsp.SetSeq(uint32(data.Seq))
	rsp.SetContext(data.Data)
	return
}

// ReadResponseBody ...
func (p *testClientCodec) ReadResponseBody(rsp Header, reply interface{}) (err error) {
	if rsp.Err() != nil {
		err = rsp.Err()
		log.Println(err)
		return
	}
	err = json.Unmarshal(*rsp.Context().(*json.RawMessage), reply)
	return
}

type testServerCodec struct {
	rwc io.ReadWriteCloser
	de  *json.Decoder
	en  *json.Encoder
	buf json.RawMessage
}

func newTestServerCodec(rwc io.ReadWriteCloser, de *json.Decoder, en *json.Encoder) *testServerCodec {
	return &testServerCodec{
		rwc: rwc,
		de:  de,
		en:  en,
	}
}

func (p *testServerCodec) ReadHeader(req Header) (err error) {
	p.buf = p.buf[:0]
	var data testReq
	data.Data = &p.buf
	err = p.de.Decode(&data)
	if err != nil {
		return
	}
	req.SetSeq(uint32(data.Seq))
	req.SetMethod(data.Method)
	req.SetContext(data.Data)
	return
}

func (p *testServerCodec) ReadRequestBody(req Header, args interface{}) (err error) {
	err = json.Unmarshal(*req.Context().(*json.RawMessage), args)
	return
}

func (p *testServerCodec) WriteResponse(rsp Header, reply interface{}) (err error) {
	var data testRsp
	data.Seq = int(rsp.Seq().(uint32))
	data.Data = reply
	err = p.en.Encode(data)
	return
}

func testServerClient(t *testing.T, client Client) {
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			arg := int32(1)
			res := int32(0)
			err := client.Call("incr", arg, &res)
			assert.NoError(t, err)
		}(i)
	}
	wg.Add(1)
	arg := int32(1)
	res := int32(0)
	client.CallAsync("incr", arg, &res, func(error) {
		defer wg.Done()
	})
	wg.Wait()
	e := client.Call("count", 0, &res)
	assert.NoError(t, e)
	assert.Equal(t, int32(4), res)
}

func TestServerClient(t *testing.T) {
	c, s := NewTestConn()
	defer c.Close()
	client := NewClientWithCodec(newTestClientCodec(c, json.NewDecoder(c), json.NewEncoder(c)))
	handlers := NewHandlers()
	server := NewServerWithCodec(handlers, newTestServerCodec(s, json.NewDecoder(s), json.NewEncoder(s)))
	count := int32(0)
	var err error
	err = handlers.Register("incr", func(i int32, res *int32) error {
		*res = atomic.AddInt32(&count, i)
		return nil
	})
	assert.NoError(t, err)
	err = handlers.Register("count", func(int32, res *int32) error {
		*res = atomic.LoadInt32(&count)
		return nil
	})
	assert.NoError(t, err)
	go server.Serve()
	testServerClient(t, client)
}

type testSessionCodec struct {
	*testClientCodec
	*testServerCodec
	buf json.RawMessage
}

func newTestSessionCodec(rwc io.ReadWriteCloser) *testSessionCodec {
	de := json.NewDecoder(rwc)
	en := json.NewEncoder(rwc)
	return &testSessionCodec{
		testClientCodec: newTestClientCodec(rwc, de, en),
		testServerCodec: newTestServerCodec(rwc, de, en),
	}
}

// ReadHeader ...
func (p *testSessionCodec) ReadHeader(head Header) (err error) {
	p.buf = p.buf[:0]
	var data testReq
	data.Data = &p.buf
	err = p.testServerCodec.de.Decode(&data)
	if err != nil {
		return
	}
	head.SetSeq(uint32(data.Seq))
	head.SetMethod(data.Method)
	head.SetContext(data.Data)
	if data.Method != "" {
		head.SetIsRequest(true)
	}
	return
}

func TestSession(t *testing.T) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	handlers := NewHandlers()
	err := handlers.Register("incr", func(i int32, res *int32, ctx *int32) error {
		// *res = ctx.Add(i)
		*res = atomic.AddInt32(ctx, i)
		return nil
	})
	assert.NoError(t, err)
	err = handlers.Register("count", func(i int32, resp ResponseWriter, ctx interface{}) error {
		defer resp.Free()
		// return resp.Reply(ctx.(*atomicInt).Load())
		return resp.Reply(atomic.LoadInt32(ctx.(*int32)))
	})
	assert.NoError(t, err)
	c, s := NewTestConn()
	defer c.Close()
	defer s.Close()
	// ccount := &atomicInt{}
	// scount := &atomicInt{}
	ccount := new(int32)
	scount := new(int32)
	clientCtx := reflect.ValueOf(ccount)
	serverCtx := reflect.ValueOf(scount)
	client := NewSessionWithCodec(newTestSessionCodec(c), handlers)
	client.SetContextHandler(ServerContextHandlerFunc(func(Header) reflect.Value {
		return clientCtx
	}))
	server := NewSessionWithCodec(newTestSessionCodec(s), handlers)
	server.SetContextHandler(ServerContextHandlerFunc(func(Header) reflect.Value {
		return serverCtx
	}))
	go server.Serve()
	go client.Serve()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		testServerClient(t, client)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		testServerClient(t, server)
	}()
	wg.Wait()
}

func TestServerFunction(t *testing.T) {
	c, s := NewTestConn()
	defer c.Close()
	client := NewClientWithCodec(newTestClientCodec(c, json.NewDecoder(c), json.NewEncoder(c)))
	handlers := NewHandlers()
	server := NewServerWithCodec(handlers, newTestServerCodec(s, json.NewDecoder(s), json.NewEncoder(s)))
	count := int32(0)
	var err error
	err = handlers.Register("incr", func(i int32, res *int32) error {
		*res = atomic.AddInt32(&count, i)
		return nil
	})
	assert.NoError(t, err)
	err = handlers.Register("count", func(int32, res *int32) error {
		*res = atomic.LoadInt32(&count)
		return nil
	})
	assert.NoError(t, err)
	go func() {
		for {
			f, e := server.ReadFunction()
			if e != nil {
				if e != io.EOF {
					t.Error(e)
				}
				return
			}
			f.Call()
			f.Free()
		}
	}()
	testServerClient(t, client)
}
