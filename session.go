package gorpc

// DualCodec ...
type SessionCodec interface {
	ClientCodec
	ReadRequestBody(header Header, args interface{}) error
	WriteResponse(header Header, reply interface{}) error
}

// Dual ...
type Session interface {
	Server
	Client
}

func NewSessionWithCodec(codec SessionCodec, handlers *Handlers) Session {
	return newSessionWithCodec(codec, handlers)
}

type sess struct {
	codec SessionCodec
	*client
	*server
}

func newSessionWithCodec(codec SessionCodec, handlers *Handlers) *sess {
	s := &sess{
		codec:  codec,
		client: newClientWithCodec(codec),
		server: newServerWithCodec(handlers, codec),
	}
	return s
}

func (p *sess) Client() Client {
	return p.client
}

func (p *sess) ServeRequest() (err error) {
	head := p.getRequest()
	for err == nil {
		head.Reset()
		err = p.codec.ReadHeader(head)
		if err != nil {
			break
		}
		if !head.IsRequest() {
			err = p.client.dealResp(head)
			if err != nil {
				break
			}
			continue
		}
		err = p.server.dealRequestBody(head, true)
		return
	}
	p.client.dealClose(err)
	return
}

func (p *sess) ReadFunction() (sf ServerFunction, err error) {
	head := p.getRequest()
	for err == nil {
		head.Reset()
		err = p.codec.ReadHeader(head)
		if err != nil {
			break
		}
		if !head.IsRequest() {
			err = p.client.dealResp(head)
			continue
		}
		sf, err = p.server.dealFunction(head)
		return
	}
	p.client.dealClose(err)
	return
}

func (p *sess) Serve() {
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
		if !head.IsRequest() {
			err = p.client.dealResp(head)
			continue
		}
		err = p.server.dealRequestBody(head, false)
	}
	p.client.dealClose(err)
}
