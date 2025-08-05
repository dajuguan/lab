package hotstuff

import (
	"learn/rpc"
	"sync"
)

type Phase int

const (
	NewView Phase = iota
	Prepare
	PreCommit
	Commit
	Decide
)

type Block struct {
	Height   int    `json:"height"`
	Hash     string `json:"hash"`
	Parent   int    `json:"parent"`
	Command  string `json:"command"`
	Proposer int    `json:"proposer"`
	Justify  *QC    `json:"justify"`
}

type QC struct {
	Type    Phase `json:"type"`
	View    int   `json:"view"`
	Block   int   `json:"block"`
	Signers []int `json:"signers"`
}

type Message struct {
	Type    Phase  `json:"type"`
	View    int    `json:"view"`
	Block   *Block `json:"block"`
	Justify *QC    `json:"justify"`
	Sender  int    `json:"sender"`
}

type Vote struct {
	Type   Phase `json:"type"`
	View   int   `json:"view"`
	Block  int   `json:"block"`
	Sender int   `json:"sender"`
}

type HotStuff struct {
	mu sync.RWMutex

	// Network
	peers []*rpc.ClientEnd
	me    int

	// Consensus state
	view      int
	phase     Phase
	curHeight int

	// Storage
	blocks     map[int]*Block
	blockchain []*Block

	// Safety rules state
	lockedQC  *QC
	prepareQC *QC
	highQC    *QC

	// Temporary state
	votes       map[int]map[int][]Vote // view -> blockHash -> votes
	newViewMsgs map[int][]Message      // view -> newview messages

	// Channels
	msgCh   chan Message
	voteCh  chan Vote
	timerCh chan bool
	applyCh chan *Block

	// Configuration
	n         int
	f         int
	threshold int

	// State
	dead bool
}
