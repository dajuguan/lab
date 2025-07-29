package main

import (
	"encoding/json"
	"fmt"
	"log"
	"reflect"
)

func main() {
	fmt.Println("test")
}

/*
network
	- schema:
		- sharedEndCh:
		- servers
	- scheduler: check every clientCh, and dispatch server to process it with replyMsg

clientEnd
	- schema:
		- sharedEndCh
	- interfaces:
		- call(server, requestArg, reply) -> ok
			- make request sharedEndCh <- req.arg, req.reply
			- wait rep <- req.reply
			- reply = req
			- return reply
server
	- process(sharedEndCh)
		- reply := service(requestArg)
		- replyCh <- reply
	- service
service
	- [methodName]method
*/

type replyMsg struct {
	ok    bool
	reply interface{}
}

type reqMsg struct {
	endname  interface{} // name of sending ClientEnd
	svcMeth  string      // e.g. "Raft.AppendEntries"
	argsType reflect.Type
	args     []byte
	replyCh  chan replyMsg
}

type Service struct {
	name    string
	rcvr    reflect.Value // the accure server
	typ     reflect.Type
	methods map[string]reflect.Method
}

func (svc *Service) dispatch(methname string, req reqMsg) replyMsg {
	if method, ok := svc.methods[methname]; ok {
		// prepare space into which to read the argument.
		// the Value's type will be a pointer to req.argsType.
		args := reflect.New(req.argsType)

		// decode the argument.
		json.Unmarshal(req.args, args.Interface())

		// allocate space for the reply.
		replyType := method.Type.In(2)
		replyType = replyType.Elem()
		replyv := reflect.New(replyType)

		// call the method.
		function := method.Func
		function.Call([]reflect.Value{svc.rcvr, args, replyv})

		// encode the reply.
		// out, _ := json.Marshal(replyv.Interface())

		return replyMsg{true, replyv.Interface()}
	} else {
		choices := []string{}
		for k, _ := range svc.methods {
			choices = append(choices, k)
		}
		log.Fatalf("labrpc.Service.dispatch(): unknown method %v in %v; expecting one of %v\n",
			methname, req.svcMeth, choices)
		return replyMsg{false, nil}
	}
}

func MakeService(rcvr interface{}) *Service {
	svc := &Service{}
	svc.typ = reflect.TypeOf(rcvr)
	svc.rcvr = reflect.ValueOf(rcvr)
	svc.name = reflect.Indirect(svc.rcvr).Type().Name()
	svc.methods = map[string]reflect.Method{}

	for m := 0; m < svc.typ.NumMethod(); m++ {
		method := svc.typ.Method(m)
		mtype := method.Type
		mname := method.Name

		//fmt.Printf("%v pp %v ni %v 1k %v 2k %v no %v\n",
		//	mname, method.PkgPath, mtype.NumIn(), mtype.In(1).Kind(), mtype.In(2).Kind(), mtype.NumOut())

		if method.PkgPath != "" || // capitalized?
			mtype.NumIn() != 3 ||
			//mtype.In(1).Kind() != reflect.Ptr ||
			mtype.In(2).Kind() != reflect.Ptr ||
			mtype.NumOut() != 0 {
			// the method is not suitable for a handler
			//fmt.Printf("bad method: %v\n", mname)
		} else {
			// the method looks like a handler
			svc.methods[mname] = method
		}
	}

	fmt.Println("num methods:", len(svc.methods))

	return svc
}

// func (c *expectedCall) Execute(t *testing.T, out interface{}) error {
// 	output, err := c.abiMethod.Outputs.Pack(c.outputs...)
// 	require.NoErrorf(t, err, "Invalid outputs for method %v: %v", c.abiMethod.Name, c.outputs)

// 	// I admit I do not understand Go reflection.
// 	// So leverage json.Unmarshal to set the out value correctly.
// 	j, err := json.Marshal(hexutil.Bytes(output))
// 	require.NoError(t, err)
// 	require.NoError(t, json.Unmarshal(j, out))
// 	return c.err
// }
