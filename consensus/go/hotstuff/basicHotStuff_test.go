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
- view should increase by 1
- blockNumber should be the previous highest QC

d) 2 follower does't repond to Pre-Commit phase, then network recovery
blockNumber should be the previous highest QC
invariants:
- view should increase by 1
- blockNumber should be the current highest QC
- previous leader shoudn't be locked

e) 2 follower does't repond to Commit phase, then network recovery
blockNumber should be the previous highest QC
invariants:
- view should increase by 1
- blockNumber should be the current highest QC
- previous leader shoudn't be locked

f) 2 follower does't repond to Pre-Commit phase, then doesn't send newView to the new leader, then network recovery
blockNumber should be the previous highest QC
invariants:
- view should increase by 2
- blockNumber should be the last highest QC

g) during leader A, only follower D receives PrepareQC; then leader B come up, B haven't recieved newView from D, but received 2 newviews from A,C;
then B sendPrepare, but C is evil, C didn't send vote and B'proposal complies with safetyRule, so D will vote.
invariants:
*/
func TestBasicHotStuffLivenessA(t *testing.T) {
	leaderConf := &BasicLeaderConf{
		LeaderID:     0,
		NextLeaderID: 0,
	}
	nodes := make([]*SimpleNode, NumNodes)
	for i := 0; i < NumNodes; i++ {
		nodes[i] = NewSimpleNode(i, leaderConf)
	}

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
				msg := <-nodes[i].decideCh
				fmt.Printf("Got Node %d committed block %v\n", i, msg.Block.Height)
				assert.EqualValues(t, leaderMsg, msg)
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

func lastCommittedMsg(t *testing.T, round, leaderID int, nodes []*SimpleNode) *Message {
	var lastCommittedMsg *Message
	for i := 0; i < round; i++ {
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
				msg := <-nodes[i].decideCh
				fmt.Printf("Got Node %d committed block %v\n", i, msg.Block.Height)
				assert.EqualValues(t, leaderMsg.View, msg.View)
				assert.EqualValues(t, leaderMsg.Block, msg.Block)
			}()
		}
		wg.Wait()
		lastCommittedMsg = leaderMsg
		fmt.Printf("++++++++++All nodes committed block: %d++++++++++\n", lastCommittedMsg.Block.Height)
	}
	return lastCommittedMsg
}

func TestBasicHotStuffLivenessC(t *testing.T) {
	nodes := make([]*SimpleNode, NumNodes)
	leaderConf := &BasicLeaderConf{
		LeaderID:     0,
		NextLeaderID: 0,
	}
	for i := 0; i < NumNodes; i++ {
		nodes[i] = NewSimpleNode(i, leaderConf)
	}

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
		// leader startNewView:2
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

	runNRound(t, 1, leaderID, nodes)
	lastCommitted := lastCommittedMsg(t, 3, leaderID, nodes)
	assert.Equal(t, 3, lastCommitted.Block.Height)

	<-nodes[leaderID].newViewCh
	nodes[leaderID].syncCh <- 0

	// Cleanup
	for i := 0; i < NumNodes; i++ {
		nodes[i].kill()
	}

	wg.Wait()
}
