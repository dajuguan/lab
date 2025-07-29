package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	hs "learn/hotstuff"
)

func TestRpc(t *testing.T) {
	h := hs.HotStuff{Name: "hello"}
	svc := MakeService(&h)
	args := hs.ReqMsg{
		Peer: 1,
	}
	req := reqMsg{}
	req.svcMeth = "Hello"
	req.argsType = reflect.TypeOf(args)
	req.args, _ = json.Marshal(args)

	reply := svc.dispatch("Hello", req)
	fmt.Println("reply:", *(reply.reply.(*string)))
}
