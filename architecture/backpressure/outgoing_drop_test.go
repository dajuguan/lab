package backpressure

/*
Backpressure is localized per peer.
Each peer has its own send buffer:
- Slow peer does NOT block fast peers, messages to slow peers are dropped when send buffer is full
- No global blocking
- Message loss is acceptable
*/

import (
	"fmt"
	"testing"
	"time"
)

type Peer struct {
	id   string
	send chan string
}

// Non-blocking send with drop
func (p *Peer) Send(msg string) {
	select {
	case p.send <- msg:
		// message enqueued
	default:
		// send buffer full → drop message
		fmt.Println("drop outgoing message:", msg, "to peer:", p.id)
	}
}

func TestOutgoingDrop(t *testing.T) {
	fastPeer := &Peer{
		id:   "fast",
		send: make(chan string, 10),
	}

	slowPeer := &Peer{
		id:   "slow",
		send: make(chan string, 1), // very small buffer
	}

	// Fast peer consumes quickly
	go func() {
		for msg := range fastPeer.send {
			fmt.Println("fast peer received:", msg)
		}
	}()

	// Slow peer consumes slowly
	go func() {
		for msg := range slowPeer.send {
			time.Sleep(1 * time.Second)
			fmt.Println("slow peer received:", msg)
		}
	}()

	// Broadcast messages
	for i := 0; i < 10; i++ {
		msg := fmt.Sprintf("msg-%d", i)
		fastPeer.Send(msg)
		slowPeer.Send(msg)
		time.Sleep(100 * time.Millisecond)
	}

	time.Sleep(3 * time.Second)
}
