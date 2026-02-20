package concurrentmodes

import (
	"testing"
	"time"
)

/*
并发语义模型，决定系统允许什么：多个状态拥有者如何交互(不限制具体采用单线程还是怎么地)，强调内存隔离和消息传递。
解决的问题是谁拥有状态，谁可以修改状态，关注空间上协作(状态所有权) + 语义安全。
Actor不关心底层是 event loop、线程池还是分布式节点等调度方式，可以运行在任意调度方式下来协调多个状态单元。
Each actor has:
- Private state
- Private mailbox
- Sequential message processing
*/

type Message interface{}

type Deposit struct {
	Amount int
}

type Withdraw struct {
	Amount int
}

type Actor struct {
	mailbox chan Message
	balance int
	name    string
}

func NewActor(buffer int, name string) *Actor {
	actor := &Actor{
		mailbox: make(chan Message, buffer),
		balance: 0,
		name:    name,
	}
	go actor.Start()
	return actor
}

func (a *Actor) Start() {
	for msg := range a.mailbox {
		switch m := msg.(type) {
		case Deposit:
			a.balance += m.Amount
			println("Actor:", a.name, "Deposited", m.Amount, "balance:", a.balance)
		case Withdraw:
			a.balance -= m.Amount
			println("Actor:", a.name, "Withdrew", m.Amount, "balance:", a.balance)
		}
	}
}

func (a *Actor) Send(msg Message) {
	a.mailbox <- msg
}

func TestActor(t *testing.T) {
	actor1 := NewActor(10, "a1")
	actor2 := NewActor(10, "a2")

	actor1.Send(Deposit{Amount: 100})
	actor2.Send(Deposit{Amount: 200})

	actor2.Send(Withdraw{Amount: 30})
	actor1.Send(Withdraw{Amount: 50})

	time.Sleep(time.Second)
}
