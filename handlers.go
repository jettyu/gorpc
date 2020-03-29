/*
	Package gorpc provides access to the exported methods of an object across a
	network or other I/O connection.  A server registers an object, making it visible
	as a service with the name of the type of the object.  After registration, exported
	methods of the object will be accessible remotely.  A server may register multiple
	objects (services) of different types but it is an error to register multiple
	objects of the same type.

		- the method has two or three arguments, both exported (or builtin) types.
		- the method's second argument is a pointer.
		- the method has return type error.
		- if the function has three arguments, the thirst argument is interface{}

	In effect, the method must look schematically like

		func (t *T) MethodName(argType T1, replyType *T2) error
	or
		func (t *T) MethodName(argType T1, replyType *T2, ctx interface{}) error
	or
		func (t *T) MethodName(argType T1, ResponseWriter *T2) error
	or
		func (t *T) MethodName(argType T1, ResponseWriter *T2, ctx interface{}) error

	The method's first argument represents the arguments provided by the caller; the
	second argument represents the result parameters to be returned to the caller.
	The method's return value, if non-nil, is passed back as a string that the client
	sees as if created by errors.New.  If an error is returned, the reply parameter
	will not be sent back to the client.
*/
package gorpc

import (
	"errors"
	"fmt"
	"log"
	"os"
	"reflect"
	"unicode"
	"unicode/utf8"
)

// Handlers ...
type Handlers struct {
	handlers map[interface{}]*service
}

// NewHandlers ...
func NewHandlers() *Handlers {
	return &Handlers{
		handlers: make(map[interface{}]*service),
	}
}

// Register ...
func (p *Handlers) Register(serviceMethod interface{}, rcvr interface{}) error {
	s, e := newService(rcvr)
	if e != nil {
		return e
	}
	p.handlers[serviceMethod] = s
	return nil
}

func (p *Handlers) Has(serviceMethod interface{}) bool {
	_, ok := p.handlers[serviceMethod]
	return ok
}

func (p *Handlers) Del(serviceMethod interface{}) {
	delete(p.handlers, serviceMethod)
}

func (p *Handlers) Range(f func(serviceMethod interface{}, rcvr reflect.Value) bool) {
	for k, v := range p.handlers {
		if !f(k, v.rcvr) {
			break
		}
	}
}

func (p *Handlers) CheckContext(ctx reflect.Type) (err error) {
	for k, v := range p.handlers {
		err = v.fType.checkContext(ctx)
		if err != nil {
			err = fmt.Errorf("[%w] method: %v", err, k)
			break
		}
	}
	return
}

type funcType struct {
	funcValue reflect.Value
	ArgType   reflect.Type
	ReplyType reflect.Type
	numIn     int
	noReply   bool
}

func (p *funcType) getArgv() (argv reflect.Value) {
	// Decode the argument value.
	if p.ArgType.Kind() == reflect.Ptr {
		argv = reflect.New(p.ArgType.Elem())
		return
	}
	argv = reflect.New(p.ArgType)
	return
}

func (p *funcType) getReplyv() (replyv reflect.Value) {
	replyv = reflect.New(p.ReplyType.Elem())
	switch p.ReplyType.Elem().Kind() {
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(p.ReplyType.Elem()))
	case reflect.Slice:
		replyv.Elem().Set(reflect.MakeSlice(p.ReplyType.Elem(), 0, 0))
	}
	return
}

func (p *funcType) call(argv, replyv, ctx reflect.Value) (reply interface{}, err error) {
	// if true, need to indirect before calling.
	if p.ArgType.Kind() != reflect.Ptr {
		argv = argv.Elem()
	}
	function := p.funcValue
	// Invoke the method, providing a new value for the reply.
	values := []reflect.Value{argv, replyv, ctx}
	returnValues := function.Call(values[:p.numIn])
	if p.noReply {
		return
	}
	// The return value for the method is an error.
	errInter := returnValues[0].Interface()
	if errInter != nil {
		err = errInter.(error)
		return
	}
	reply = replyv.Interface()
	return
}

func (p *funcType) checkContext(ctx reflect.Type) (err error) {
	if p.numIn < 3 {
		return
	}
	mt := p.funcValue.Type().In(2)
	if ctx.ConvertibleTo(mt) {
		return
	}
	err = fmt.Errorf("[%w] context' type is %v, but funcType's 3d type is %v",
		os.ErrInvalid, ctx, mt)
	return
}

type service struct {
	rcvr  reflect.Value // receiver of methods for the service
	typ   reflect.Type  // type of the receiver
	fType *funcType     // registered methods
}

func newService(rcvr interface{}) (s *service, err error) {
	s = new(service)
	s.typ = reflect.TypeOf(rcvr)
	s.rcvr = reflect.ValueOf(rcvr)
	// Install the methods
	s.fType, err = suitableFuncValue(s.rcvr, false)

	sname := reflect.Indirect(s.rcvr).Type().Name()
	if err != nil {
		str := "rpc.Register: type " + sname + " not suitable type"
		err = errors.New(str)
		return
	}
	return
}

func suitableFuncValue(funcValue reflect.Value, reportErr bool) (ft *funcType, err error) {
	mtype := funcValue.Type()
	mname := funcValue.Type().Name()
	if mtype.NumIn() != 2 && mtype.NumIn() != 3 {
		err = fmt.Errorf("rpc.Register: method %q has %d input parameters; needs exactly two or three", mname, mtype.NumIn())
		if reportErr {
			log.Println(err)
		}
		return
	}
	// if mtype.NumIn() == 3 {
	// 	if mtype.In(2).Kind() != reflect.Interface {
	// 		err = fmt.Errorf("rpc.Register: method %q's thirst input parameter must be interface{} ", mname)
	// 		if reportErr {
	// 			log.Println(err)
	// 		}
	// 		return
	// 	}
	// }
	// First arg need not be a pointer.
	argType := mtype.In(0)
	if !isExportedOrBuiltinType(argType) {
		err = fmt.Errorf("rpc.Register: argument type of method %q is not exported: %q", mname, argType)
		if reportErr {
			log.Println(err)
		}
		return
	}
	replyType := mtype.In(1)
	noReply := false
	noReply, err = isValidResponseType(replyType, mname, reportErr)
	if err != nil {
		return
	}
	// Method needs one out.
	if mtype.NumOut() != 1 {
		err = fmt.Errorf("rpc.Register: method %q has %d output parameters; needs exactly one", mname, mtype.NumOut())
		if reportErr {
			log.Println(err)
		}
		return
	}
	// The return type of the method must be error.
	if returnType := mtype.Out(0); returnType != typeOfError {
		err = fmt.Errorf("rpc.Register: return type of method %q is %q, must be error", mname, returnType)
		if reportErr {
			log.Println(err)
		}
		return
	}

	ft = &funcType{funcValue: funcValue, ArgType: argType, ReplyType: replyType, numIn: mtype.NumIn(), noReply: noReply}
	return
}

// A value sent as a placeholder for the server's response value when the server
// receives an invalid request. It is never decoded by the client since the Response
// contains an error when it is used.
var invalidRequest = struct{}{}

// Is this type exported or a builtin?
func isExportedOrBuiltinType(t reflect.Type) bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	// PkgPath will be non-empty even for an exported type,
	// so we need to check the type name as well.
	return isExported(t.Name()) || t.PkgPath() == ""
}

// Is this an exported - upper case - name?
func isExported(name string) bool {
	rune, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(rune)
}

func isResponseWriter(t reflect.Type) bool {
	return responseWriterType.AssignableTo(t)
}

func isValidResponseType(replyType reflect.Type, mname string, reportErr bool) (noReply bool, err error) {
	noReply = isResponseWriter(replyType)
	if noReply {
		return
	}
	// Second arg must be a pointer.
	if replyType.Kind() != reflect.Ptr {
		err = fmt.Errorf("rpc.Register: reply type of method %q is not a pointer: %q", mname, replyType)
		if reportErr {
			log.Println(err)
		}
		return
	}
	// Reply type must be exported.
	if !isExportedOrBuiltinType(replyType) {
		err = fmt.Errorf("rpc.Register: reply type of method %q is not exported: %q", mname, replyType)
		if reportErr {
			log.Println(err)
		}
		return
	}
	return
}
