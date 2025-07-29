package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

const TestReply = "TestService"

type TestService struct {
	Name string
}

type TestArg struct {
	Peer int
}

func (s *TestService) Hello(args *TestArg, reply *string) {
	*reply = TestReply
}

func TestRpc(t *testing.T) {
	h := TestService{Name: "hello"}
	svc := MakeService(&h)
	args := &TestArg{
		Peer: 1,
	}
	req := reqMsg{}
	req.svcMeth = "Hello"
	req.argsType = reflect.TypeOf(args)
	req.args, _ = json.Marshal(args)

	resp := svc.dispatch("Hello", req)
	var reply *string
	json.Unmarshal(resp.data, &reply)
	assert.Equal(t, *reply, TestReply)
}

func TestNetwork(t *testing.T) {
	h := TestService{Name: "hello"}

	network := MakeNetwork()
	srv := MakeService(&h)
	server := &Server{}
	server.AddService(srv)
	network.AddServer(0, server)

	client := network.MakeClient(0)
	args := TestArg{
		Peer: 1,
	}
	reply := "111"
	client.Call("Hello", &args, &reply)
	assert.Equal(t, reply, TestReply)
}
