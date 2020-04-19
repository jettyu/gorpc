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
	*server
}

func newSessionWithCodec(codec SessionCodec, handlers *Handlers) *sess {
	s := &sess{
		codec:  codec,
		server: newServerWithCodec(handlers, codec),
	}
	s.client = newClientWithCodec(codec)
	return s
}
