package backpressure

/*
Incoming Drop with Priority (Control-Plane First): Application-level overload protection.

Application receive buffer is bounded:
- Control messages must always flow, reliable
- Data messages are dropped when overloaded, lossy
*/

import (
	"fmt"
	"testing"
	"time"
)

type MessageType int

const (
	ControlMsg MessageType = iota
	DataMsg
)

type Message struct {
	Type MessageType
	Body string
}

type Receiver struct {
	control chan Message
	data    chan Message
}

func NewReceiver() *Receiver {
	return &Receiver{
		control: make(chan Message, 10), // high priority
		data:    make(chan Message, 2),  // low priority
	}
}

// Incoming message handling with priority-based drop
func (r *Receiver) Receive(msg Message) {
	switch msg.Type {
	case ControlMsg:
		// Control messages should never be dropped
		r.control <- msg // may block intentionally
	case DataMsg:
		// Data messages are dropped if buffer is full
		select {
		case r.data <- msg:
		default:
			fmt.Println("drop incoming data message:", msg.Body)
		}
	}
}

func TestIncomingDrop(t *testing.T) {
	r := NewReceiver()

	// Control message handler
	go func() {
		for msg := range r.control {
			fmt.Println("control handled:", msg.Body)
		}
	}()

	// Slow data handler
	go func() {
		for msg := range r.data {
			time.Sleep(500 * time.Millisecond)
			fmt.Println("data handled:", msg.Body)
		}
	}()

	// Simulate incoming traffic
	go func() {
		for i := 0; i < 10; i++ {
			r.Receive(Message{
				Type: DataMsg,
				Body: fmt.Sprintf("data-%d", i),
			})
		}
	}()

	// Control messages still flow
	go func() {
		for i := 0; i < 10; i++ {
			r.Receive(Message{
				Type: ControlMsg,
				Body: fmt.Sprintf("control-%d", i),
			})
		}
	}()

	time.Sleep(3 * time.Second)
}
