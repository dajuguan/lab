package concurrentmodes

/*
执行模型：决定系统如何实现。
一个线程如何调度任务的机制，不规定任务的并发安全。
解决多个task什么时候被执行，关注时间调度。
- One global queue
- One processing loop
- Shared state (doesn't enforce memory isolation in event loop), No isolation
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
