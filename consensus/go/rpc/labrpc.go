package rpc

import (
	"encoding/json"
	"log"
	"reflect"
)

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
	- process(args from sharedEndCh)
		- reply := service(requestArg)
		- replyCh <- reply
	- service
service
	- [methodName]method
*/

type Network struct {
	servers     map[int]*Server
	clients     map[int]*ClientEnd
	sharedReqCh chan reqMsg
	done        chan struct{}
}

func MakeNetwork() *Network {
	rn := &Network{}
	rn.servers = map[int]*Server{}
	rn.clients = map[int]*ClientEnd{}
	rn.sharedReqCh = make(chan reqMsg)
	rn.done = make(chan struct{})

	go func() {
		for {
			select {
			case xreq := <-rn.sharedReqCh:
				{
					go rn.processReq(xreq)
				}
			case <-rn.done:
				return
			}
		}
	}()
	return rn
}

func (rn *Network) Cleanup() {
	select {
	case <-rn.done:
	default:
		close(rn.done)
	}
}

func (rn *Network) processReq(req reqMsg) {
	server, ok := rn.servers[req.endId]
	if !ok {
		panic("server is not initialized yet!")
	}
	resp := server.dispatch(req)
	req.replyCh <- resp
}

func (rn *Network) AddServer(serverId int, server *Server) {
	rn.servers[serverId] = server
}

func (rn *Network) MakeClient(clientId int) *ClientEnd {
	c := ClientEnd{
		endId:       clientId,
		sharedReqCh: rn.sharedReqCh,
		done:        rn.done,
	}
	rn.clients[clientId] = &c
	return &c
}

type ClientEnd struct {
	endId       int
	sharedReqCh chan reqMsg
	done        chan struct{} // closed when Network is cleaned up
}

func (c *ClientEnd) Call(svcMeth string, args interface{}, reply interface{}) bool {
	req := reqMsg{}
	req.svcMeth = svcMeth
	req.endId = c.endId
	req.argsType = reflect.TypeOf(args)
	req.args, _ = json.Marshal(args)

	req.replyCh = make(chan replyMsg)

	select {
	case c.sharedReqCh <- req:
	case <-c.done:
		return false
	}

	resp := <-req.replyCh
	if resp.ok {
		json.Unmarshal(resp.data, reply)
		return true
	} else {
		return false
	}
}

type Server struct {
	// mu       sync.Mutex
	// rpcCount int
	service *Service
}

func (s *Server) dispatch(req reqMsg) replyMsg {
	return s.service.dispatch(req.svcMeth, req)
}

func (s *Server) AddService(service *Service) {
	s.service = service
}

type replyMsg struct {
	ok   bool
	data []byte
}

type reqMsg struct {
	endId    int    // name of sending ClientEnd
	svcMeth  string // e.g. "Raft.AppendEntries"
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
		function.Call([]reflect.Value{svc.rcvr, args.Elem(), replyv})

		// encode the reply.
		out, _ := json.Marshal(replyv.Interface())

		return replyMsg{ok: true, data: out}
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

	return svc
}
