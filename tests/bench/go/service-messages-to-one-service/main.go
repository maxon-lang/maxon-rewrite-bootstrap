// One sender and one service: the Go counterpart of tests/bench/service-messages-to-one-service.maxon. A service is a
// goroutine that owns its state and reads a channel of messages as its mailbox; a reply is a channel of its own. Go's add
// wraps where Maxon's is checked.
package main

import (
	"fmt"

	"minimaxon/tests/bench/go/phase"
)

const (
	batches       = 1000
	batchMessages = 1000
	// mailbox is how many messages the service's channel holds, as the Maxon service's mailbox does.
	mailbox = 1024
)

// message adds n to the service's sum, or, when reply is set, asks for the sum.
type message struct {
	n     uint64
	reply chan uint64
}

func counter(in <-chan message) {
	var sum uint64
	for m := range in {
		if m.reply != nil {
			m.reply <- sum
			continue
		}
		sum += m.n
	}
}

func main() {
	t := phase.Start()
	in := make(chan message, mailbox)
	go counter(in)
	var total uint64
	reply := make(chan uint64)
	for b := range uint64(batches) {
		for i := range uint64(batchMessages) {
			in <- message{n: b*batchMessages + i}
		}
		in <- message{reply: reply}
		total = <-reply
	}
	close(in)
	fmt.Println(total)
	t.Finish()
}
