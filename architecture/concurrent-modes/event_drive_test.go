package concurrentmodes

/*
- One global queue
- One processing loop
- Shared state (balance), No isolation
- Single-threaded execution model
*/

import (
	"testing"
	"time"
)

type Event struct {
	name string
	fn   func()
}

type EventLoop struct {
	queue chan Event
}

func NewEventLoop(buffer int) *EventLoop {
	return &EventLoop{
		queue: make(chan Event, buffer),
	}
}

func (e *EventLoop) Start() {
	for event := range e.queue {
		event.fn()
	}
}

func (e *EventLoop) Submit(event Event) {
	e.queue <- event
}

func TestEventLoop(t *testing.T) {

	globalBalance := 0

	el := NewEventLoop(10)
	go el.Start()

	el.Submit(Event{
		name: "Deposit",
		fn: func() {
			globalBalance += 100
			println("Deposited 100, balance:", globalBalance)
		},
	})

	el.Submit(Event{
		name: "Withdraw",
		fn: func() {
			globalBalance -= 50
			println("Withdrew 50, balance:", globalBalance)
		},
	})

	time.Sleep(time.Second)
}
