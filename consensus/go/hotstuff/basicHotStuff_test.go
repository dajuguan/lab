package hotstuff

import (
	"fmt"
	"reflect"
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
- blockNumber shoule be the previous highest QC

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
	nodes := make([]*SimpleNode, NumNodes)
	for i := 0; i < NumNodes; i++ {
		nodes[i] = NewSimpleNode(i)
	}

	var wg sync.WaitGroup

	// Start all nodes
	for i := 0; i < NumNodes; i++ {
		wg.Add(1)
		go nodes[i].runConsensus(nodes, &wg)
	}
	round := 0
	leader := nodes[BasicLeader]
	command := fmt.Sprintf("transaction-%d", round)
	fmt.Printf("\n---------- Round %d: Leader %d proposes ----------\n", round, BasicLeader)
	leader.proposeBlock(command, nodes)

	// Simulate 10 rounds of consensus
	var leaderBlock *Block
	for round := 0; round < 3; round++ {
		// Wait for all nodes to commit this block
		cases := make([]reflect.SelectCase, NumNodes)
		for i := 0; i < NumNodes; i++ {
			cases[i] = reflect.SelectCase{
				Dir:  reflect.SelectRecv,
				Chan: reflect.ValueOf(nodes[i].applyCh),
			}
		}

		for commitCount := 0; commitCount < NumNodes; commitCount++ {
			chosen, value, _ := reflect.Select(cases)
			block := value.Interface().(*Block)
			if leaderBlock == nil {
				leaderBlock = block
			} else {
				assert.EqualValues(t, leaderBlock, block)
			}
			fmt.Printf("Got Node %d committed block %v\n", chosen, block.Height)
		}
		fmt.Printf("All nodes committed block for round %d\n", round)
		leaderBlock = nil
	}

	// Cleanup
	for i := 0; i < NumNodes; i++ {
		nodes[i].kill()
	}

	wg.Wait()
}
