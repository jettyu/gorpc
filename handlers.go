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
	"fmt"
	"log"
	"reflect"
	"unicode"
	"unicode/utf8"
)

// Handlers ...
type Handlers struct {
	handlers map[interface{}]Service
}

// NewHandlers ...
func NewHandlers() *Handlers {
	return &Handlers{
		handlers: make(map[interface{}]Service),
	}
}

// Register ...
func (p *Handlers) Register(serviceMethod interface{}, rcvr interface{}) error {
	ss, ok := rcvr.(SyncService)
	if ok {
		p.handlers[serviceMethod] = ss
		return nil
	}
	as, ok := rcvr.(AsyncService)
	if ok {
		p.handlers[serviceMethod] = as
		return nil
	}
	s, e := FuncToService(rcvr)
	if e != nil {
		return e
	}
	p.handlers[serviceMethod] = s
	return nil
}

func (p *Handlers) Get(serviceMethod interface{}) (Service, bool) {
	s, ok := p.handlers[serviceMethod]
	return s, ok
}

func (p *Handlers) Has(serviceMethod interface{}) bool {
	_, ok := p.handlers[serviceMethod]
	return ok
}

func (p *Handlers) Del(serviceMethod interface{}) {
	delete(p.handlers, serviceMethod)
}

func (p *Handlers) Clone() *Handlers {
	d := NewHandlers()
	for k, v := range p.handlers {
		d.handlers[k] = v
	}
	return d
}

func (p *Handlers) CheckContext(ctx interface{}) (err error) {
	for k, v := range p.handlers {
		c, ok := v.(ServiceCheckContext)
		if !ok {
			continue
		}
		err = c.CheckContext(ctx)
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
