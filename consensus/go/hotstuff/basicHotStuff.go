package hotstuff

/* Implement Algorithm 2 Basic HotStuff protocol in "HotStuff: BFT Consensus in the Lens of Blockchain", using go channel to simulate node communication.
4 nodes(including leader) = 3f + 1, f=1
quorum: 2f+1=3 (need 2 votes from followers + 1 vote from leader)
*/

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type SimpleNode struct {
	mu sync.RWMutex

	// Basic info
	ID   int
	view int

	// HotStuff state - reusing types from hotstuff.go
	phase            Phase
	blocks           map[int]*Block
	uncommitedBlocks map[int]*Block
	lockedQC         *QC
	prepareQC        *QC

	// Communication channels for in-memory simulation
	msgCh  chan Message
	voteCh chan Vote

	// Vote collection for leaders - only track if node has voted in current phase
	votes map[int]bool // nodeID -> has voted in current phase

	// NewView message collection
	newViewMsgs map[int][]Message // view -> newview messages

	// Timeout handling
	newViewTimeoutTimer *time.Timer
	lastUpdate          time.Time

	// Apply channel for tracking committed blocks
	applyCh   chan *Block
	prepareCh chan *Block
	// simulate networkDelay
	delay time.Duration

	// Configuration
	threshold  int
	dead       bool
	leaderConf *BasicLeaderConf
}

type BasicLeaderConf struct {
	LeaderID     int
	NextLeaderID int
}

const (
	NumNodes   = 4
	F          = 1
	QuorumSize = 3
	NetDelay   = time.Millisecond * 100
	Timeout    = 4 * NetDelay
)

func NewSimpleNode(id int, leader *BasicLeaderConf) *SimpleNode {
	node := &SimpleNode{
		ID:                  id,
		view:                1,
		phase:               NewView,
		blocks:              make(map[int]*Block),
		uncommitedBlocks:    make(map[int]*Block),
		votes:               make(map[int]bool),
		newViewMsgs:         make(map[int][]Message),
		msgCh:               make(chan Message, 100),
		voteCh:              make(chan Vote, 100),
		applyCh:             make(chan *Block, 100),
		prepareCh:           make(chan *Block, 100),
		delay:               time.Duration(5 * time.Millisecond),
		threshold:           QuorumSize,
		newViewTimeoutTimer: time.NewTimer(Timeout),
		dead:                false,
		leaderConf:          leader,
	}
	// Initialize genesis block and highQC
	genesis := &Block{
		Height:   0,
		Hash:     "genesis",
		Parent:   0,
		Command:  "genesis",
		Proposer: -1,
		Justify:  nil,
	}
	node.blocks[0] = genesis
	node.prepareQC = &QC{
		Type:  Prepare,
		View:  0,
		Block: 0,
	}
	for i := 0; i < NumNodes; i++ {
		node.prepareQC.Signers = append(node.prepareQC.Signers, i)
	}
	return node
}

func (n *SimpleNode) leader(view int) int {
	// simply use the same leader unless manual changing it
	return n.leaderConf.LeaderID
}

func (n *SimpleNode) isLeader(view int) bool {
	return n.leader(view) == n.ID
}

func (n *SimpleNode) nextLeader(view int) int {
	// simply use the same next leader unless manual changing it
	return n.leaderConf.NextLeaderID
}

func (n *SimpleNode) isNextleader(view int) bool {
	return n.nextLeader(view) == n.ID
}

func (n *SimpleNode) blockHash(block *Block) string {
	data, _ := json.Marshal(block)
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash)
}

func (n *SimpleNode) createBlock(parent *Block, command string, justify *QC) *Block {
	block := &Block{
		Height:   parent.Height + 1,
		Parent:   parent.Height,
		Command:  command,
		Proposer: n.ID,
		Justify:  justify,
	}
	block.Hash = n.blockHash(block)
	return block
}

func (n *SimpleNode) extends(block *Block, ancestor int) bool {
	current := block
	parent, exists := n.blocks[current.Parent]
	if !exists || parent.Height != ancestor {
		return false
	}
	return current != nil
}

func (n *SimpleNode) safetyRule(block *Block, qc *QC) bool {
	// Voting rule from HotStuff paper:
	// vote only if (lockedQC = ⊥ ∨ extends(b, lockedQC.node) ∨ qc.view > lockedQC.view)
	if n.lockedQC == nil {
		return true
	}
	if n.extends(block, n.lockedQC.Block) {
		return true
	}
	if qc != nil && qc.View > n.lockedQC.View {
		return true
	}
	return false
}

func (n *SimpleNode) commit(block *Block) {
	fmt.Printf("[Node %d] Committing block %v (cmd: %s)\n", n.ID, block.Height, block.Command)

	// Add block to local storage
	n.blocks[block.Height] = block
	if n.uncommitedBlocks[block.Height] != nil {
		delete(n.uncommitedBlocks, block.Height)
	}

	// Send block to applyCh for external tracking
	n.applyCh <- block
}

func (n *SimpleNode) onPrepare(msg Message, allNodes []*SimpleNode) {
	fmt.Printf("[Node %d] onPrepare %v onView:%v from [leader:%v]\n", n.ID, msg, n.view, msg.Sender)
	n.mu.Lock()
	defer n.mu.Unlock()

	if time.Since(n.lastUpdate) > Timeout {
		return
	}

	if msg.View < n.view {
		return
	}

	// Safety check
	if !n.safetyRule(msg.Block, msg.Justify) {
		return
	}

	n.view = msg.View
	// Update timer
	n.newViewTimeoutTimer.Reset(Timeout)
	n.lastUpdate = time.Now()

	// Send vote to leader
	vote := Vote{
		Type:   Prepare,
		View:   msg.View,
		Block:  msg.Block.Height,
		Sender: n.ID,
	}

	leaderID := n.leader(msg.View)
	go func() {
		if n.delay > 0 {
			time.Sleep(n.delay)
		}
		allNodes[leaderID].voteCh <- vote
	}()
}

func (n *SimpleNode) matchingQC(qc *QC, qcType Phase) bool {
	return qc.Type == qcType && qc.View == n.view && len(qc.Signers) >= n.threshold
}

func (n *SimpleNode) onPreCommit(msg Message, allNodes []*SimpleNode) {
	fmt.Printf("[Node %d] onPreCommit %v onView:%v from [leader:%v]\n", n.ID, msg, n.view, msg.Sender)
	n.mu.Lock()
	defer n.mu.Unlock()

	//TODO: validate safetyRoll against late RPC and fetch missed blocks
	if time.Since(n.lastUpdate) > Timeout {
		return
	}
	if !n.matchingQC(msg.Justify, Prepare) {
		return
	}

	n.view = msg.View
	// Update timer
	n.newViewTimeoutTimer.Reset(Timeout)
	n.lastUpdate = time.Now()
	// Process justify QC
	n.prepareQC = msg.Justify

	// Send vote
	vote := Vote{
		Type:   PreCommit,
		View:   msg.View,
		Block:  msg.Block.Height,
		Sender: n.ID,
	}

	leaderID := n.leader(msg.View)
	go func() {
		n.prepareCh <- msg.Block
		if n.delay > 0 {
			time.Sleep(n.delay)
		}
		allNodes[leaderID].voteCh <- vote
	}()
}

func (n *SimpleNode) onCommit(msg Message, allNodes []*SimpleNode) {
	fmt.Printf("[Node %d] onCommit %v onView:%v from [leader:%v]\n", n.ID, msg, n.view, msg.Sender)
	n.mu.Lock()
	defer n.mu.Unlock()

	//TODO: validate safetyRoll against late RPC and fetch missed blocks
	if time.Since(n.lastUpdate) > Timeout {
		return
	}
	if !n.matchingQC(msg.Justify, PreCommit) {
		return
	}

	n.view = msg.View
	// Update timer
	n.newViewTimeoutTimer.Reset(Timeout)
	n.lastUpdate = time.Now()
	n.lockedQC = msg.Justify

	// Send vote
	vote := Vote{
		Type:   Commit,
		View:   msg.View,
		Block:  msg.Block.Height,
		Sender: n.ID,
	}

	leaderID := n.leader(msg.View)
	go func() {
		if n.delay > 0 {
			time.Sleep(n.delay)
		}
		allNodes[leaderID].voteCh <- vote
	}()
}

func (n *SimpleNode) onDecideQC(msg Message, allNodes []*SimpleNode) {
	fmt.Printf("[Node %d] onDecideQC %v onView:%v from [leader:%v]\n", n.ID, msg, n.view, msg.Sender)
	n.mu.Lock()
	defer n.mu.Unlock()

	//TODO: validate safetyRoll against late RPC and fetch missed blocks
	if time.Since(n.lastUpdate) > Timeout {
		return
	}
	// Verify this is a valid commitQC
	if !n.matchingQC(msg.Justify, Commit) {
		return
	}

	n.view = msg.View
	n.newViewTimeoutTimer.Reset(Timeout)
	n.lastUpdate = time.Now()

	// Commit the block locally - this adds block to n.blocks
	n.commit(msg.Block)

	// Advance to next view and send newview to next leader
	n.view++
	n.phase = NewView
	n.votes = make(map[int]bool) // Clear votes for new view

	newViewMsg := Message{
		Type:    NewView,
		View:    n.view,
		Justify: n.prepareQC,
		Sender:  n.ID,
	}

	// Send newview to new leader
	newLeaderID := n.leader(n.view)
	go func() {
		if n.delay > 0 {
			time.Sleep(n.delay)
		}
		allNodes[newLeaderID].msgCh <- newViewMsg
	}()

	fmt.Printf("[Node %d] Committed block %v and advanced to view %d\n", n.ID, msg.Block.Height, n.view)
}

func (n *SimpleNode) onNewView(msg Message, allNodes []*SimpleNode) {
	fmt.Printf("[Leader %d] onNewView %v onView:%v from [peer:%v]\n", n.ID, msg, n.view, msg.Sender)
	n.mu.Lock()
	defer n.mu.Unlock()

	if msg.View < n.view || n.phase != NewView {
		return
	}

	n.view = msg.View

	// If we are the new leader, collect newview messages
	if n.isNextleader(msg.View) {
		// Initialize newview collection for this view
		if n.newViewMsgs[msg.View] == nil {
			n.newViewMsgs[msg.View] = make([]Message, 0)
			newViewMsg := Message{
				Type:    NewView,
				View:    n.view,
				Justify: n.prepareQC,
				Sender:  n.ID,
			}
			// append leader itself's newViewMsg
			n.newViewMsgs[msg.View] = append(n.newViewMsgs[msg.View], newViewMsg)
		}

		// Add this newview message
		n.newViewMsgs[msg.View] = append(n.newViewMsgs[msg.View], msg)

		fmt.Printf("[Leader %d] Received NewView from Node %d for view %d (%d/%d)\n",
			n.ID, msg.Sender, msg.View, len(n.newViewMsgs[msg.View]), n.threshold)

		// Check if we have enough newview messages, including leader itself
		if len(n.newViewMsgs[msg.View]) >= n.threshold {
			if time.Since(n.lastUpdate) < Timeout {
				n.startNewViewConsensus(msg.View, allNodes)
			}
		}
	}
}

func (n *SimpleNode) startNewViewConsensus(view int, allNodes []*SimpleNode) {
	// Update timer
	n.newViewTimeoutTimer.Reset(Timeout)
	n.lastUpdate = time.Now()

	n.phase = Prepare
	// simulate leader rotation
	n.leaderConf.LeaderID = n.ID
	n.leaderConf.NextLeaderID = n.nextLeader(view)
	// Clear votes for new consensus
	n.votes = make(map[int]bool)

	// Find highest QC from newview messages
	var highestQC *QC
	for _, msg := range n.newViewMsgs[view] {
		if msg.Justify != nil && (highestQC == nil || msg.Justify.View > highestQC.View) && len(msg.Justify.Signers) >= n.threshold {
			highestQC = msg.Justify
		}
	}
	// Clear n.newViewMsgs
	n.newViewMsgs[view] = nil

	// Create new block
	parent := n.blocks[0]
	if highestQC != nil {
		if block, exists := n.blocks[highestQC.Block]; exists {
			parent = block
		}
	}

	command := fmt.Sprintf("new-cmd-%d", view)
	newBlock := n.createBlock(parent, command, highestQC)
	n.uncommitedBlocks[newBlock.Height] = newBlock

	// Broadcast prepare message
	prepareMsg := Message{
		Type:    Prepare,
		View:    view,
		Block:   newBlock,
		Justify: highestQC,
		Sender:  n.ID,
	}

	fmt.Printf("[Leader %d] Starting new view %d with block %v\n", n.ID, view, newBlock.Height)
	fmt.Printf("\n---------- View %d: Leader %d proposes ----------\n", n.view, n.ID)
	if n.delay > 0 {
		time.Sleep(n.delay)
	}
	n.broadcast(prepareMsg, allNodes)
}

func (n *SimpleNode) broadcast(msg Message, allNodes []*SimpleNode) {
	for i, node := range allNodes {
		if i != n.ID {
			go func(node *SimpleNode) {
				node.msgCh <- msg
			}(node)
		}
	}
}

func (n *SimpleNode) onQuorum(view int, blockNumber int, allNodes []*SimpleNode) {
	if time.Since(n.lastUpdate) > Timeout {
		return
	}
	n.lastUpdate = time.Now()

	fmt.Printf("[Leader %d] onQuorum phase:%v, onView:%v\n", n.ID, n.phase, n.view)
	block := n.uncommitedBlocks[blockNumber]
	if block == nil {
		panic("block not exists")
	}
	// Create QC - collect signers from those who voted
	qc := &QC{
		Type:    n.phase,
		View:    view,
		Block:   blockNumber,
		Signers: make([]int, 0),
	}

	// Add leader's signature
	qc.Signers = append(qc.Signers, n.ID)

	// Add followers who voted
	for nodeID, hasVoted := range n.votes {
		if hasVoted {
			qc.Signers = append(qc.Signers, nodeID)
		}
	}

	// Clear votes for next phase
	n.votes = make(map[int]bool)

	// Update timer
	n.newViewTimeoutTimer.Reset(Timeout)

	// Advance to next phase
	nextPhase := n.phase
	switch n.phase {
	case Prepare:
		n.phase = PreCommit
		nextPhase = n.phase
	case PreCommit:
		n.phase = Commit
		nextPhase = n.phase
	case Commit:
		// Send commitQC to followers so they can commit the block
		nextPhase = Decide
		// Leader also commits the block locally
		n.commit(block)
		n.phase = NewView
		n.view++
	}

	msg := Message{
		Type:    nextPhase,
		View:    view,
		Block:   block,
		Justify: qc,
		Sender:  n.ID,
	}
	fmt.Printf("[Leader %d] Broadcasting [Phase:%v] for block %v\n", n.ID, nextPhase, block.Height)
	if n.delay > 0 {
		time.Sleep(n.delay)
	}
	n.broadcast(msg, allNodes)
}

func (n *SimpleNode) onTimeout(allNodes []*SimpleNode) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if time.Since(n.lastUpdate) < Timeout { // drop expired timeout to avoid race condition with other events
		return
	}

	fmt.Printf("[Node %d] Timeout in view %d, advancing to view %d\n", n.ID, n.view, n.view+1)

	// Advance view
	n.view++
	n.phase = NewView
	// Update timer
	n.newViewTimeoutTimer.Reset(Timeout)
	n.lastUpdate = time.Now()

	// Send NewView message to new leader according to HotStuff paper
	newViewMsg := Message{
		Type:    NewView,
		View:    n.view,
		Justify: n.prepareQC,
		Sender:  n.ID,
	}

	// Send newview message to the new leader
	newLeaderID := n.nextLeader(n.view)
	if n.ID == newLeaderID {
		//donothing, just wait for other peers's newViewMSG
		if n.newViewMsgs[n.view] == nil {
			n.newViewMsgs[n.view] = make([]Message, 0)
			newViewMsg := Message{
				Type:    NewView,
				View:    n.view,
				Justify: n.prepareQC,
				Sender:  n.ID,
			}
			// append leader itself's newViewMsg
			n.newViewMsgs[n.view] = append(n.newViewMsgs[n.view], newViewMsg)
		}
	} else {
		go func() {
			allNodes[newLeaderID].msgCh <- newViewMsg
		}()
	}
}

func (n *SimpleNode) proposeBlock(command string, allNodes []*SimpleNode) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if !n.isLeader(n.view) {
		return
	}

	// Find parent block
	parent := n.blocks[0]
	if n.prepareQC != nil {
		if block, exists := n.blocks[n.prepareQC.Block]; exists {
			parent = block
		}
	}

	// Create new block
	newBlock := n.createBlock(parent, command, n.prepareQC)
	n.uncommitedBlocks[newBlock.Height] = newBlock
	n.phase = Prepare

	// Broadcast prepare message
	prepareMsg := Message{
		Type:    Prepare,
		View:    n.view,
		Block:   newBlock,
		Justify: n.prepareQC,
		Sender:  n.ID,
	}

	fmt.Printf("[Leader %d] Proposing block %v with command '%s' at view %d\n",
		n.ID, newBlock.Height, command, n.view)
	if n.delay > 0 {
		time.Sleep(n.delay)
	}
	n.broadcast(prepareMsg, allNodes)
}

func (n *SimpleNode) runConsensus(allNodes []*SimpleNode, wg *sync.WaitGroup) {
	defer wg.Done()

	for !n.dead {
		select {
		case msg := <-n.msgCh:
			switch msg.Type {
			case NewView:
				n.onNewView(msg, allNodes)
			case Prepare:
				n.onPrepare(msg, allNodes)
			case PreCommit:
				n.onPreCommit(msg, allNodes)
			case Commit:
				n.onCommit(msg, allNodes)
			case Decide:
				n.onDecideQC(msg, allNodes)
			}

		case vote := <-n.voteCh:
			{
				n.mu.Lock()
				if n.isLeader(vote.View) && vote.View == n.view && vote.Type == n.phase {
					fmt.Printf("[Leader %d] gotVote %v onView:%v, onPhase:%v, from [peer:%v]\n", n.ID, vote, n.view, n.phase, vote.Sender)

					// Check if this node already voted in current phase - prevent duplicate voting
					if n.votes[vote.Sender] {
						n.mu.Unlock()
						continue // Ignore duplicate vote
					}

					// Record that this node has voted
					n.votes[vote.Sender] = true

					// Count total votes
					voteCount := 0
					for _, hasVoted := range n.votes {
						if hasVoted {
							voteCount++
						}
					}

					// Check if we have enough votes (including leader's implicit vote)
					if voteCount+1 >= n.threshold { // +1 for leader's implicit vote
						n.onQuorum(vote.View, vote.Block, allNodes)
					}
				}
				n.mu.Unlock()
			}

		case <-n.newViewTimeoutTimer.C:
			n.onTimeout(allNodes)
		}
	}
}

func (n *SimpleNode) kill() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.dead = true
}
