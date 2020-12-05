package gorpc

import (
	"fmt"
	"os"
	"reflect"
	"sync"
)

// ServerCodec ...
type ServerCodec interface {
	ReadHeader(Header) error
	ReadRequestBody(header Header, args interface{}) error
	WriteResponse(header Header, reply interface{}) error
}

type ServerContextHandler interface {
	GetServerContext(Header) reflect.Value
}

type ServerContextHandlerFunc func(Header) reflect.Value

func (p ServerContextHandlerFunc) GetServerContext(h Header) reflect.Value {
	return p(h)
}

// Server ...
type Server interface {
	Serve()
	ServeRequest() error
	ReadFunction() (ServerFunction, error)
	SetContextHandler(ServerContextHandler)
}

type ResponseWriter interface {
	Header
	Free()
	Reply(interface{}) error
}

type ServerFunction interface {
	Call()
	Free()
}

// serverFunction ...
type serverFunction struct {
	server   *server
	funcType *funcType
	argv     reflect.Value
	replyv   reflect.Value
	rsp      *header
}

// Call ...
func (p *serverFunction) Call() {
	p.server.call(p.funcType, p.rsp, p.argv, p.replyv)
}

func (p *serverFunction) Free() {
	p.server.freeFunction(p)
}

// NewServerWithCodec ...
func NewServerWithCodec(handlerManager *Handlers, codec ServerCodec) Server {
	return newServerWithCodec(handlerManager, codec)
}

// Precompute the reflect type for error. Can't use error directly
// because Typeof takes an empty interface value. This is annoying.
var typeOfError = reflect.TypeOf((*error)(nil)).Elem()

type server struct {
	*Handlers
	codec              ServerCodec
	contextHandler     ServerContextHandler
	responsePool       *sync.Pool
	request            header
	responseWriterPool *sync.Pool
	funcPool           *sync.Pool
	*client
}

func newServerWithCodec(handlers *Handlers, codec ServerCodec) *server {
	s := &server{
		Handlers: handlers,
		codec:    codec,
		responsePool: &sync.Pool{
			New: func() interface{} {
				return &header{}
			},
		},
		responseWriterPool: &sync.Pool{
			New: func() interface{} {
				return &Response{}
			},
		},
		funcPool: &sync.Pool{
			New: func() interface{} {
				return &serverFunction{}
			},
		},
	}

	return s
}

func (p *server) SetContextHandler(h ServerContextHandler) {
	p.contextHandler = h
}

func (p *server) Serve() {
	var (
		err error
	)
	head := p.getRequest()
	for err == nil {
		head.Reset()
		err = p.codec.ReadHeader(head)
		if err != nil {
			break
		}
		if p.client != nil && !head.IsRequest() {
			err = p.client.dealResp(head)
			continue
		}
		err = p.dealRequestBody(head, false)
	}
	if p.client != nil {
		p.client.dealClose(err)
	}
}

func (p *server) ServeRequest() (err error) {
	head := p.getRequest()
	for err == nil {
		head.Reset()
		err = p.codec.ReadHeader(head)
		if err != nil {
			break
		}
		if p.client != nil && !head.IsRequest() {
			err = p.client.dealResp(head)
			if err != nil {
				break
			}
			continue
		}
		err = p.dealRequestBody(head, true)
		return
	}
	if p.client != nil {
		p.client.dealClose(err)
	}
	return
}

func (p *server) ReadFunction() (sf ServerFunction, err error) {
	head := p.getRequest()
	for err == nil {
		head.Reset()
		err = p.codec.ReadHeader(head)
		if err != nil {
			break
		}
		if p.client != nil && !head.IsRequest() {
			err = p.client.dealResp(head)
			continue
		}
		sf, err = p.dealFunction(head)
		return
	}
	if p.client != nil {
		p.client.dealClose(err)
	}
	return
}

func (p *server) getResponse() *header {
	rsp := p.responsePool.Get().(*header)
	if rsp.Seq() == nil {
		return rsp
	}
	rsp.Reset()
	return rsp
}

func (p *server) freeResponse(rsp *header) {
	p.responsePool.Put(rsp)
}

func (p *server) getResponseWriter(rsp *header) reflect.Value {
	w := p.responseWriterPool.Get().(*Response)
	w.header = rsp
	w.server = p
	return reflect.ValueOf(w)

}

func (p *server) sendResponse(rsp *header, reply interface{}, withFree bool) error {
	e := p.codec.WriteResponse(rsp, reply)
	if withFree {
		p.freeResponse(rsp)
	}
	return e
}

func (p *server) getRequest() *header {
	return &p.request
}

func (p *server) getFunction() *serverFunction {
	return p.funcPool.Get().(*serverFunction)
}

func (p *server) freeFunction(f *serverFunction) {
	p.funcPool.Put(f)
}

func (p *server) dealFunction(req *header) (sf *serverFunction, err error) {
	rsp := p.getResponse()
	rsp.SetSeq(req.Seq())
	rsp.SetMethod(req.Method())
	rsp.SetContext(req.Context())

	s, ok := p.handlers[req.Method()]
	if !ok {
		err = fmt.Errorf("there is no handler for the method: %s, [%w]", req.Method(), os.ErrInvalid)
		rsp.SetErr(err)
		req.SetErr(err)
		err = p.codec.ReadRequestBody(req, nil)
		if err != nil {
			p.freeResponse(rsp)
			return
		}
		err = p.sendResponse(rsp, nil, true)
		if err != nil && debugLog {
			fmt.Println("sendResponse failed: ", err.Error())
		}
		return
	}
	sf = p.getFunction()
	sf.server = p
	sf.funcType = s.fType
	mtype := s.fType
	sf.argv, err = p.getHeaderBody(req, mtype)
	if err != nil {
		p.freeResponse(rsp)
		return
	}
	sf.replyv = p.getReplyv(rsp, mtype)
	sf.rsp = rsp
	return
}

func (p *server) dealRequestBody(req *header, block bool) (err error) {
	rsp := p.getResponse()
	rsp.SetSeq(req.Seq())
	rsp.SetMethod(req.Method())
	rsp.SetContext(req.Context())

	s, ok := p.handlers[req.Method()]
	if !ok {
		err = fmt.Errorf("there is no handler for the method: %s, [%w]", req.Method(), os.ErrInvalid)
		rsp.SetErr(err)
		req.SetErr(err)
		err = p.codec.ReadRequestBody(req, nil)
		if err != nil {
			p.freeResponse(rsp)
			return
		}
		err = p.sendResponse(rsp, nil, true)
		if err != nil && debugLog {
			fmt.Println("sendResponse failed: ", err.Error())
		}
		return
	}
	mtype := s.fType
	argv, e := p.getHeaderBody(req, mtype)
	if e != nil {
		p.freeResponse(rsp)
		err = e
		return
	}
	replyv := p.getReplyv(rsp, mtype)
	if block {
		p.call(mtype, rsp, argv, replyv)
		return
	}
	go p.call(mtype, rsp, argv, replyv)
	return
}

func (p *server) getHeaderBody(req *header, mtype *funcType) (argv reflect.Value, err error) {
	// Decode the argument value.
	argv = mtype.getArgv()
	// argv guaranteed to be a pointer now.
	if err = p.codec.ReadRequestBody(req, argv.Interface()); err != nil {
		return
	}

	return
}

func (p *server) getReplyv(rsp *header, mtype *funcType) (replyv reflect.Value) {
	if mtype.noReply {
		replyv = p.getResponseWriter(rsp)
		return
	}
	replyv = mtype.getReplyv()
	return
}

func (p *server) call(mtype *funcType, rsp *header, argv, replyv reflect.Value) {
	var ctx reflect.Value
	if mtype.numIn == 3 && p.contextHandler != nil {
		ctx = p.contextHandler.GetServerContext(rsp)
	}
	reply, err := mtype.call(argv, replyv, ctx)
	if mtype.noReply {
		return
	}
	if err != nil {
		rsp.SetErr(err)
	}
	err = p.sendResponse(rsp, reply, true)
	if err != nil && debugLog {
		fmt.Println("sendResponse failed: ", err.Error())
	}
}

// Response ...
type Response struct {
	*header
	server *server
}

// Free ...
func (p *Response) Free() {
	p.server.freeResponse(p.header)
}

// Reply ...
func (p *Response) Reply(reply interface{}) error {
	return p.server.sendResponse(p.header, reply, false)
}

var responseWriterType = reflect.TypeOf(&Response{})
