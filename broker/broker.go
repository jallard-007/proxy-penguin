// Package broker implements a fan-out pub/sub broker for distributing
// completed request records to active subscribers.
package broker

import (
	"sync"

	"github.com/jallard-007/proxy-pengiun/model"
)

// Broker distributes published request records to all current subscribers.
type Broker struct {
	mu          sync.RWMutex
	subscribers map[uint64]chan *model.RequestRecord
	nextID      uint64
}

// New returns a new, ready-to-use Broker.
func New() *Broker {
	return &Broker{
		subscribers: make(map[uint64]chan *model.RequestRecord),
	}
}

// Subscribe registers a new subscriber and returns its unique ID along with a
// channel on which published records will be delivered.
func (b *Broker) Subscribe() (uint64, <-chan *model.RequestRecord) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.nextID
	b.nextID++
	ch := make(chan *model.RequestRecord, 256)
	b.subscribers[id] = ch
	return id, ch
}

// Unsubscribe removes the subscriber with the given ID and closes its channel.
func (b *Broker) Unsubscribe(id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ch, ok := b.subscribers[id]; ok {
		close(ch)
		delete(b.subscribers, id)
	}
}

// Publish sends rec to every subscriber's channel, dropping the record for
// any subscriber whose buffer is full.
func (b *Broker) Publish(rec *model.RequestRecord) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subscribers {
		select {
		case ch <- rec:
		default:
		}
	}
}
