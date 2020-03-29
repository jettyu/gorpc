package gorpc

import (
	"fmt"
)

type HeaderGetter interface {
	Seq() interface{}
	Method() interface{}
	Context() interface{}
	Err() error
	IsRequest() bool
}

type HeaderSetter interface {
	SetSeq(interface{})
	SetMethod(interface{})
	SetContext(interface{})
	SetErr(error)
	SetIsRequest(bool)
	Reset()
}

type Header interface {
	HeaderGetter
	HeaderSetter
}

type header struct {
	seq       interface{}
	method    interface{}
	context   interface{}
	err       error
	isRequest bool
}

func NewHeader() Header {
	return &header{}
}

func (p *header) Seq() interface{} {
	return p.seq
}

func (p *header) Method() interface{} {
	return p.method
}

func (p *header) Context() interface{} {
	return p.context
}

func (p *header) Err() error {
	return p.err
}

func (p *header) IsRequest() bool {
	return p.isRequest
}

func (p *header) SetSeq(seq interface{}) {
	p.seq = seq
}

func (p *header) SetMethod(method interface{}) {
	p.method = method
}

func (p *header) SetContext(ctx interface{}) {
	p.context = ctx
}

func (p *header) SetErr(e error) {
	p.err = e
}

func (p *header) SetIsRequest(isRequest bool) {
	p.isRequest = isRequest
}

func (p *header) Reset() {
	p.seq = nil
	p.method = nil
	p.context = nil
	p.err = nil
	p.isRequest = false
}

func (p *header) String() string {
	return fmt.Sprintf(`{"seq":"%v", "service_method":"%v", "error":"%v"}`,
		p.Seq(), p.Method(), p.Err())
}
