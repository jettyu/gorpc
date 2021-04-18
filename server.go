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
	GetServerContext(Header) interface{}
}

type ServerContextHandlerFunc func(Header) reflect.Value

func (p ServerContextHandlerFunc) GetServerContext(h Header) interface{} {
	return p(h).Interface()
}

type ServerContextFunc func(Header) interface{}

func (p ServerContextFunc) GetServerContext(h Header) interface{} {
	return p(h)
}

func NewServerContextHandler(ctx interface{}) ServerContextHandler {
	if ctx == nil {
		return nil
	}
	return &serverContextHandler{
		ctx: reflect.ValueOf(ctx),
	}
}

type serverContextHandler struct {
	ctx interface{}
}

func (p *serverContextHandler) GetServerContext(Header) interface{} {
	return p.ctx
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
	server *server
	svc    Service
	rsp    *header
}

// Call ...
func (p *serverFunction) Call() {
	p.server.call(p.rsp, p.svc)
}

func (p *serverFunction) Free() {
	p.server.freeFunction(p)
}

// NewServerWithCodec ...
func NewServerWithCodec(handlerManager ServiceManager, codec ServerCodec) Server {
	return newServerWithCodec(handlerManager, codec)
}

// Precompute the reflect type for error. Can't use error directly
// because Typeof takes an empty interface value. This is annoying.
var typeOfError = reflect.TypeOf((*error)(nil)).Elem()

type server struct {
	handlers           ServiceManager
	codec              ServerCodec
	contextHandler     ServerContextHandler
	responsePool       *sync.Pool
	request            header
	responseWriterPool *sync.Pool
	funcPool           *sync.Pool
	*client
}

func newServerWithCodec(handlers ServiceManager, codec ServerCodec) *server {
	s := &server{
		handlers: handlers,
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
	var err error
	for err == nil {
		err = p.ServeRequest()
	}
}

func (p *server) ServeRequest() (err error) {
	head := p.getRequest()
	for err == nil {
		head.Reset()
		err = p.codec.ReadHeader(head)
		if err != nil {
			continue
		}
		if p.client != nil && !head.IsRequest() {
			err = p.client.dealResp(head)
			continue
		}
		err = p.dealRequestBody(head, false)
		if err != nil {
			continue
		}
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

func (p *server) getResponseWriter(rsp *header) ResponseWriter {
	w := p.responseWriterPool.Get().(*Response)
	w.header = rsp
	w.server = p
	return w

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

func (p *server) getService(req, rsp *header) (s Service, ok bool, err error) {
	s, ok = p.handlers.Get(req.Method())
	if ok {
		s = s.Clone()
		return
	}
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

func (p *server) dealFunction(req *header) (sf *serverFunction, err error) {
	rsp := p.getResponse()
	rsp.SetSeq(req.Seq())
	rsp.SetMethod(req.Method())
	rsp.SetContext(req.Context())

	s, ok, err := p.getService(req, rsp)
	if !ok || err != nil {
		return
	}
	sf = p.getFunction()
	sf.server = p
	sf.svc = s
	sf.rsp = rsp

	err = p.getHeaderBody(req, s.GetArg())
	if err != nil {
		p.freeResponse(rsp)
		return
	}
	return
}

func (p *server) dealRequestBody(req *header, block bool) (err error) {
	rsp := p.getResponse()
	rsp.SetSeq(req.Seq())
	rsp.SetMethod(req.Method())
	rsp.SetContext(req.Context())

	s, ok, err := p.getService(req, rsp)
	if !ok || err != nil {
		return
	}
	e := p.getHeaderBody(req, s.GetArg())
	if e != nil {
		p.freeResponse(rsp)
		err = e
		return
	}
	if block {
		p.call(rsp, s)
		return
	}
	go p.call(rsp, s)
	return
}

func (p *server) getHeaderBody(req *header, arg interface{}) (err error) {
	// Decode the argument value.
	// argv guaranteed to be a pointer now.
	return p.codec.ReadRequestBody(req, arg)
}

func (p *server) call(rsp *header, svc Service) {
	sc, ok := svc.(ServiceWithContext)
	if ok && p.contextHandler != nil {
		sc.WithContext(p.contextHandler.GetServerContext(rsp))
	}
	sh, ok := svc.(ServiceWithHeader)
	if ok {
		sh.WithHeader(rsp)
	}
	ss, ok := svc.(SyncService)
	if ok {
		reply, err := ss.Do()
		if err != nil {
			rsp.SetErr(err)
		}
		err = p.sendResponse(rsp, reply, true)
		if err != nil && debugLog {
			fmt.Println("sendResponse failed: ", err.Error())
		}
		return
	}
	as, ok := svc.(AsyncService)
	if ok {
		as.Do(p.getResponseWriter(rsp))
		return
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
