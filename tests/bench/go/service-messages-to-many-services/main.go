// 1,000 services with 1,000 messages each: the Go counterpart of tests/bench/service-messages-to-many-services.maxon. A
// service is a goroutine that owns its state and reads a channel of messages as its mailbox; a reply is a channel of its
// own. Go's add wraps where Maxon's is checked.
package main

import (
	"fmt"

	"minimaxon/tests/bench/go/phase"
)

const (
	services = 1000
	rounds   = 1000
	// mailbox is how many messages a service's channel holds, as the Maxon service's mailbox does.
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
	inboxes := make([]chan message, services)
	for s := range inboxes {
		inboxes[s] = make(chan message, mailbox)
		go counter(inboxes[s])
	}
	t.Lap("spawn")
	for r := range uint64(rounds) {
		for s := range inboxes {
			inboxes[s] <- message{n: r}
		}
	}
	t.Lap("send")
	var total uint64
	reply := make(chan uint64)
	for s := range inboxes {
		inboxes[s] <- message{reply: reply}
		total += <-reply
		close(inboxes[s])
	}
	t.Lap("await")
	fmt.Println(total)
	t.Finish()
}
