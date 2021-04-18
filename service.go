package gorpc

import (
	"errors"
	"fmt"
	"os"
	"reflect"
)

type ServiceManager interface {
	Get(method interface{}) (Service, bool)
}

type Service interface {
	Clone() Service
	GetArg() interface{}
}

type ServiceWithHeader interface {
	WithHeader(Header)
}

type ServiceWithContext interface {
	WithContext(interface{})
}

type ServiceCheckContext interface {
	CheckContext(interface{}) error
}

type SyncService interface {
	Service
	Do() (interface{}, error)
}

type AsyncService interface {
	Service
	Do(ResponseWriter)
}

func FuncToService(fn interface{}) (s Service, err error) {
	rcvr := reflect.ValueOf(fn)
	// Install the methods
	fType, err := suitableFuncValue(rcvr, false)
	sname := reflect.Indirect(rcvr).Type().Name()
	if err != nil {
		str := "rpc.Register: type " + sname + " not suitable type"
		err = errors.New(str)
		return
	}
	var (
		argv, replyv reflect.Value
	)
	argv = ReflectNew(fType.ArgType)
	fs := newFuncService(rcvr, argv, replyv)
	if !fType.noReply {
		fs.replyv = ReflectNew(fType.ReplyType)
		s = &syncService{fs}
		return
	}
	s = &asyncService{fs}
	return
}

type syncService struct {
	*funcService
}

func (p *syncService) Clone() Service {
	return &syncService{p.funcService.Clone()}
}

func (p *syncService) Do() (interface{}, error) {
	return p.call()
}

type asyncService struct {
	*funcService
}

func (p *asyncService) Clone() Service {
	return &asyncService{p.funcService.Clone()}
}

func (p *asyncService) Do(w ResponseWriter) {
	p.callAsync(w)
}

type funcService struct {
	fn     reflect.Value
	argv   reflect.Value
	replyv reflect.Value
	ctx    reflect.Value
}

func newFuncService(fn, argv, replyv reflect.Value) *funcService {
	return &funcService{
		fn:     fn,
		argv:   argv,
		replyv: replyv,
	}
}

func ReflectNew(srcType reflect.Type) (dst reflect.Value) {
	if srcType.Kind() == reflect.Ptr {
		srcType = srcType.Elem()
	}
	dst = reflect.New(srcType)
	switch srcType.Kind() {
	case reflect.Map:
		dst.Elem().Set(reflect.MakeMap(srcType))
	case reflect.Slice:
		dst.Elem().Set(reflect.MakeSlice(srcType, 0, 0))
	}
	return dst
}

func (p funcService) Clone() *funcService {
	var argv, replyv reflect.Value
	if p.argv.IsValid() {
		argv = ReflectNew(p.argv.Type())
	}
	if p.replyv.IsValid() {
		replyv = ReflectNew(p.replyv.Type())
	}
	return newFuncService(p.fn, argv, replyv)
}

func (p *funcService) GetArg() (v interface{}) {
	return p.argv.Interface()
}

func (p *funcService) WithContext(v interface{}) {
	p.ctx = ReflectValueOf(v)
}

func (p *funcService) CheckContext(v interface{}) error {
	if v == nil {
		return nil
	}
	if p.fn.Type().NumIn() < 3 {
		return nil
	}
	mt := p.fn.Type().In(2)
	ctx := ReflectValueOf(v).Type()
	if ctx.ConvertibleTo(mt) {
		return nil
	}
	err := fmt.Errorf("[%w] context' type is %v, but funcType's 3d type is %v",
		os.ErrInvalid, ctx, mt)
	return err
}

func (p *funcService) call() (interface{}, error) {
	argv := p.argv
	if p.fn.Type().In(0).Kind() != reflect.Ptr {
		argv = argv.Elem()
	}
	v := p.fn.Call([]reflect.Value{argv, p.replyv, p.ctx}[:p.fn.Type().NumIn()])[0].Interface()
	if v != nil {
		return nil, v.(error)
	}
	return p.replyv.Interface(), nil
}

func (p *funcService) callAsync(w ResponseWriter) {
	argv := p.argv
	if p.fn.Type().In(0).Kind() != reflect.Ptr {
		argv = argv.Elem()
	}
	p.fn.Call([]reflect.Value{argv, reflect.ValueOf(w), p.ctx}[:p.fn.Type().NumIn()])
}

func ReflectValueOf(v interface{}) (rv reflect.Value) {
	if v == nil {
		return
	}
	rv, ok := v.(reflect.Value)
	if ok {
		return
	}
	rv = reflect.ValueOf(v)
	return
}
