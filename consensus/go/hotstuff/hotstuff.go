package hotstuff

type HotStuff struct {
	Name string
}

type ReqMsg struct {
	Peer int
}

func (h *HotStuff) Hello(arg *ReqMsg, reply *string) {
	*reply = "Hello from hotstuff"
}
