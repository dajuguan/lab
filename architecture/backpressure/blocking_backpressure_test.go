package backpressure

/*
When buffer is full, producer is blocked.
- No message loss
- Producer blocks when consumer is slow
- Backpressure propagates upstream
- Risk of cascading stalls
*/

import (
	"fmt"
	"testing"
	"time"
)

func TestBlockingBackpressure(t *testing.T) {
	ch := make(chan int, 2) // small buffer

	// Producer
	go func() {
		for i := 0; i < 5; i++ {
			fmt.Println("sending", i)
			ch <- i // BLOCKS when channel buffer is full
			fmt.Println("sent", i)
		}
		close(ch)
	}()

	// Slow consumer
	for v := range ch {
		time.Sleep(1 * time.Second)
		fmt.Println("received", v)
	}
}
