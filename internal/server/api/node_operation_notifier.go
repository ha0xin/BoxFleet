package api

import "sync"

// nodeOperationNotifier is only a latency optimization. Durable operations
// live in SQLite and claim handlers always query the database before and after
// waiting, so a process restart or a missed notification cannot lose work.
type nodeOperationNotifier struct {
	mu      sync.Mutex
	waiters map[string]chan struct{}
}

func newNodeOperationNotifier() *nodeOperationNotifier {
	return &nodeOperationNotifier{waiters: make(map[string]chan struct{})}
}

func (n *nodeOperationNotifier) subscribe(nodeName string) <-chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	ch := n.waiters[nodeName]
	if ch == nil {
		ch = make(chan struct{})
		n.waiters[nodeName] = ch
	}
	return ch
}

func (n *nodeOperationNotifier) notify(nodeName string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if ch := n.waiters[nodeName]; ch != nil {
		close(ch)
	}
	n.waiters[nodeName] = make(chan struct{})
}
