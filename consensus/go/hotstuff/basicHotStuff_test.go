package hotstuff

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

/*
## Safety check:
a) after 3 honest nodes received PrepareQC, then leader timeout causing viewchange;
the new leader is Byzantine, he propose another proposal with the same view. This should be rejected and cause view change immediatly or after timeout.

## Liveness check:
a) normal case
leader propose => follower vote => we should notice proposed block is increasing and follower's vote
invariant: view don't change

b) one follower doesn't respond to every cmd
should succeed

c) 2 follower doesn't respond to Prepare phase, then network recovery
=> every node send newView to leader(currentView+1)
- how long should the leader wait for the newView: 4δ. δ is maximum network delay.
invariants:
- view should increase by 2 from lastedCommit: QC.h + 2
- commitedBlockNumber should be the previous highest QC

d) 2 follower doesn't respond to PreCommit phase (view: QC.h + 1), allNodes has prepareQC: QC.h, then network recovery
invariants:
- view should increase by 2 from lastedCommit: QC.h + 2
- commitedBlockNumber should be the previous highest QC:QC.h
- newBlockNumber is QC.h + 1

e) 2 follower received prepareQC (view: QC.h) but didn't respond to Commit phase, allNodes has prepareQC: QC.h, then network recovery
blockNumber should be the previous highest QC
invariants:
- view should increase by 1 from lastedCommit: QC.h + 1
- previous leader shoudn't be locked
- QC.h should be committed along with the new blockNumber: QC.h + 1

f) 2 follower does't repond to Decide phase, then network recovery, kinda like e)

g) 2 follower does't repond to Pre-Commit phase (view: QC.h + 1), leader timeout (view change to QC.h + 2);
then doesn't send newView to the new leader (view: QC.h + 2), leader timeout again (view change to QC.h + 3);
then network recovery: (view: QC.h + 3)
invariants:
- view should increase by 3 from lastedCommit: QC.h + 3
- commitedBlockNumber should be the last highest QC: QC.h
- blockNumber should be QC.h+1

h) during leader A, only follower D receives PrepareQC; then leader B come up, B haven't recieved newView from D, but received 2 newviews from A,C;
then B sendPrepare, but C is evil, C didn't send vote and B'proposal complies with safetyRule, so D will vote.
invariants:
*/
func TestBasicHotStuffLivenessA(t *testing.T) {
	nodes, leaderConf := setupNodes()

	var wg sync.WaitGroup

	// Start all nodes
	for i := 0; i < NumNodes; i++ {
		wg.Add(1)
		go nodes[i].runConsensus(nodes, &wg)
	}
	round := 0
	leaderID := leaderConf.LeaderID
	leader := nodes[leaderID]
	command := fmt.Sprintf("transaction-%d", round)
	fmt.Printf("\n---------- Round %d: Leader %d proposes ----------\n", round, leaderID)
	leader.proposeBlock(command, nodes)

	// Wait for all nodes to commit this block
	// Simulate 3 rounds of consensus
	for round := 0; round < 3; round++ {
		leaderMsg := <-nodes[leaderID].decideCh

		var wg sync.WaitGroup
		for i := 0; i < NumNodes; i++ {
			i := i
			if i == leaderID {
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				block := <-nodes[i].decideCh
				fmt.Printf("Got Node %d committed block %v\n", i, block.Height)
				assert.EqualValues(t, leaderMsg, block)
			}()
		}
		wg.Wait()

		<-nodes[leaderID].newViewCh
		nodes[leaderID].syncCh <- 0
		fmt.Printf("++++++++++All nodes committed block for round %d++++++++++\n", round)
	}

	// Cleanup
	for i := 0; i < NumNodes; i++ {
		nodes[i].kill()
	}

	wg.Wait()
}

// Round consensus for N rounds and return lastCommittedMsg after N rounds
func runNRound(t *testing.T, round, leaderID int, nodes []*SimpleNode) {
	for i := 0; i < round; i++ {
		// consume viewViewCh and syncCh
		<-nodes[leaderID].newViewCh
		nodes[leaderID].syncCh <- 0
	}
}

// check N(round) blocks has been commited, and return the block the blockNumber:round
func lastCommittedBlock(t *testing.T, round, leaderID int, nodes []*SimpleNode) *Block {
	var lastCommittedBlock *Block
	for i := 0; i < round; i++ {
		leaderBlock := <-nodes[leaderID].decideCh
		var wg sync.WaitGroup
		for i := 0; i < NumNodes; i++ {
			i := i
			if i == leaderID {
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				block := <-nodes[i].decideCh
				fmt.Printf("Got Node %d committed block %v\n", i, block.Height)
				assert.EqualValues(t, leaderBlock.View, block.View)
				assert.EqualValues(t, leaderBlock.Hash, block.Hash)
			}()
		}
		wg.Wait()
		lastCommittedBlock = leaderBlock
		fmt.Printf("++++++++++All nodes committed block: %d++++++++++\n", lastCommittedBlock.Height)
	}
	return lastCommittedBlock
}

func TestBasicHotStuffLivenessC(t *testing.T) {
	nodes, leaderConf := setupNodes()
	var wg sync.WaitGroup

	// Start all nodes
	// View = 1
	for i := 0; i < NumNodes; i++ {
		wg.Add(1)
		go nodes[i].runConsensus(nodes, &wg)
	}
	round := 0
	leaderID := leaderConf.LeaderID
	// view=1
	leader := nodes[leaderID]
	command := fmt.Sprintf("transaction-%d", round)
	fmt.Printf("\n---------- Round %d: Leader %d proposes ----------\n", round, leaderID)
	leader.proposeBlock(command, nodes)

	{
		// leader startNewView:2, block 1 committed
		<-nodes[leaderID].newViewCh
		// node 1,2 respond to prepare lately
		nodes[1].delay = Timeout + NetDelay
		nodes[2].delay = Timeout + NetDelay
		nodes[leaderID].syncCh <- 0

		// leader startNewView:3
		<-nodes[leaderID].newViewCh
		// node 1,2 restore normal connection in view3
		nodes[1].delay = DefaultDelay
		nodes[2].delay = DefaultDelay
		nodes[leaderID].syncCh <- 0

	}

	runNRound(t, 1, leaderID, nodes) // block 2 committed
	lastCommitted := lastCommittedBlock(t, 2, leaderID, nodes)
	assert.Equal(t, 3, lastCommitted.View)

	<-nodes[leaderID].newViewCh
	nodes[leaderID].syncCh <- 0

	// Cleanup
	for i := 0; i < NumNodes; i++ {
		nodes[i].kill()
	}

	wg.Wait()
}

func TestBasicHotStuffLivenessD(t *testing.T) {
	nodes, leaderConf := setupNodes()
	// set sync channel to simulate preCommit resp timeout
	nodes[1].precommitSyncCh = make(chan int)
	nodes[2].precommitSyncCh = make(chan int)

	var wg sync.WaitGroup

	// Start all nodes
	// View = 1
	for i := 0; i < NumNodes; i++ {
		wg.Add(1)
		go nodes[i].runConsensus(nodes, &wg)
	}

	round := 0
	leaderID := leaderConf.LeaderID
	// view=1
	leader := nodes[leaderID]
	command := fmt.Sprintf("transaction-%d", round)
	fmt.Printf("\n---------- Round %d: Leader %d proposes ----------\n", round, leaderID)
	leader.proposeBlock(command, nodes)

	// consume precommitCh
	<-nodes[1].preCommitCh
	<-nodes[2].preCommitCh
	nodes[1].precommitSyncCh <- 0
	nodes[2].precommitSyncCh <- 0
	{
		// leader startNewView:2, newProposal: block2, block 1 committed
		<-nodes[leaderID].newViewCh
		nodes[leaderID].syncCh <- 0
		// delay node 1,2's pre-commit resp
		<-nodes[1].preCommitCh
		<-nodes[2].preCommitCh
		nodes[1].delay = Timeout + NetDelay
		nodes[2].delay = Timeout + NetDelay
		nodes[1].precommitSyncCh <- 0
		nodes[2].precommitSyncCh <- 0
		// all nodes has block 2, but block2 didn't get prepare QC, it hasn't been committed.
	}

	{
		// leader 0 and follower 3 pre-commit resp timeout, newView=3, highQC=block 1, newBlock:2(old is overwritten).
		// node 1,2 currently don't use timeout in new-view so newView will succeed.
		<-nodes[leaderID].newViewCh
		// reconnect node 1,2
		nodes[1].delay = DefaultDelay
		nodes[1].precommitSyncCh = nil
		nodes[2].delay = DefaultDelay
		nodes[2].precommitSyncCh = nil
		nodes[leaderID].syncCh <- 0
	}

	{
		// last QC.h(block 1) and the new proposal (block 2)  must be committed.
		<-nodes[leaderID].newViewCh
		nodes[leaderID].syncCh <- 0
		lastCommitted := lastCommittedBlock(t, 2, leaderID, nodes)
		assert.Equal(t, 3, lastCommitted.View)
		assert.Equal(t, 2, lastCommitted.Height)
	}
}

func TestBasicHotStuffLivenessE(t *testing.T) {
	nodes, leaderConf := setupNodes()
	// set sync channel to simulate preCommit resp timeout
	nodes[1].commitSyncCh = make(chan int)
	nodes[2].commitSyncCh = make(chan int)

	var wg sync.WaitGroup

	// Start all nodes
	// View = 1
	for i := 0; i < NumNodes; i++ {
		wg.Add(1)
		go nodes[i].runConsensus(nodes, &wg)
	}

	round := 0
	leaderID := leaderConf.LeaderID
	// view=1
	leader := nodes[leaderID]
	command := fmt.Sprintf("transaction-%d", round)
	fmt.Printf("\n---------- Round %d: Leader %d proposes ----------\n", round, leaderID)
	leader.proposeBlock(command, nodes)

	// consume precommitCh
	<-nodes[1].commitCh
	<-nodes[2].commitCh
	nodes[1].commitSyncCh <- 0
	nodes[2].commitSyncCh <- 0
	{
		// leader startNewView:2, newProposal: block2, block 1 committed
		<-nodes[leaderID].newViewCh
		nodes[leaderID].syncCh <- 0
		// delay node 1,2's pre-commit resp
		<-nodes[1].commitCh
		<-nodes[2].commitCh
		nodes[1].delay = Timeout + NetDelay
		nodes[2].delay = Timeout + NetDelay
		nodes[1].commitSyncCh <- 0
		nodes[2].commitSyncCh <- 0
		// all nodes has block 2 as prepare QC, but it hasn't been committed.
	}

	{
		// leader 0 and follower 3 pre-commit resp timeout, newView=3, highQC=block 2, newBlock:3.
		// leader has preprareQC QC.h, so QC.h will be the highQC even leader hasn't receive node 3's newView.
		// node 1,2 currently don't use timeout in new-view so newView will succeed.
		<-nodes[leaderID].newViewCh
		// reconnect node 1,2
		nodes[1].delay = DefaultDelay
		nodes[1].commitSyncCh = nil
		nodes[2].delay = DefaultDelay
		nodes[2].commitSyncCh = nil
		nodes[leaderID].syncCh <- 0
	}

	{
		// last QC.h(block 2) must be committed along with the new proposal (block 3).
		<-nodes[leaderID].newViewCh
		nodes[leaderID].syncCh <- 0
		lastCommitted := lastCommittedBlock(t, 3, leaderID, nodes)
		assert.Equal(t, 3, lastCommitted.View)
		assert.Equal(t, 3, lastCommitted.Height)
	}
}

func setupNodes() ([]*SimpleNode, *BasicLeaderConf) {
	nodes := make([]*SimpleNode, NumNodes)
	leaderConf := &BasicLeaderConf{
		LeaderID:     0,
		NextLeaderID: 0,
	}
	for i := 0; i < NumNodes; i++ {
		nodes[i] = NewSimpleNode(i, leaderConf)
	}
	return nodes, leaderConf
}
