package event

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"

	"github.com/jallard-007/proxy-penguin/backend/model"
)

const maxBatch = 500
const maxWait = 25 * time.Millisecond

type FailedInsert struct {
	Event model.RecordEvent
	Err   error
}

type RecordEvents struct {
	Type    []model.RecordEventType
	Request []model.Request
}

func NewRecordEvents() *RecordEvents {
	return &RecordEvents{
		Type:    make([]model.RecordEventType, 0, maxBatch),
		Request: make([]model.Request, 0, maxBatch),
	}
}

type EventPool struct {
	pool sync.Pool
}

func NewEventPool() *EventPool {
	ePool := &EventPool{
		pool: sync.Pool{
			New: func() any {
				return NewRecordEvents()
			},
		},
	}

	return ePool
}

func (e *EventPool) Get() *RecordEvents {
	return e.pool.Get().(*RecordEvents)
}

func (e *EventPool) Put(re *RecordEvents) {
	re.Request = re.Request[:0]
	re.Type = re.Type[:0]
	e.pool.Put(re)
}

// HandleEvents handles events sent over 'events'.
// Processed events are then sent in batches to 'processedEvents'
// To stop this handler, close the events chan.
func HandleEvents(store *Storage, events <-chan model.RecordEvent, ePool *EventPool, processedEvents func(batch *RecordEvents)) {
	batchChan := make(chan *RecordEvents, 2)
	ctx := context.Background()

	// pre alloc
	ePool.Put(NewRecordEvents())
	ePool.Put(NewRecordEvents())
	ePool.Put(NewRecordEvents())
	ePool.Put(NewRecordEvents())

	var wg sync.WaitGroup
	wg.Go(func() {
		for batch := range batchChan {
			err := store.Transaction(ctx, func(tx *sql.Tx) error {
				return store.TransactionBatchInsertRequestsPreboxed(tx, batch.Request)
			})

			if err != nil {
				log.Println("storage write:", err)
				continue
			}

			processedEvents(batch)
		}
	})

	timer := time.NewTimer(maxWait)
	defer timer.Stop()

	for {
		batch := ePool.Get()
		ev, ok := <-events
		if !ok {
			close(batchChan)
			wg.Wait()
			return
		}
		batch.Request = append(batch.Request, ev.Record)
		batch.Type = append(batch.Type, ev.Type)

		timer.Reset(maxWait)

	collect:
		for len(batch.Request) < maxBatch {
			select {
			case ev, ok := <-events:
				if !ok {
					break collect
				}
				batch.Request = append(batch.Request, ev.Record)
				batch.Type = append(batch.Type, ev.Type)
			case <-timer.C:
				select {
				case batchChan <- batch:
					goto end
				default:
					// if we get here, batchChan is full.
					// the sqlite setup is very performant so this is unlikely under normal load.
					//
					// insert speeds using this HandleEvents function:
					// 2015 MacBook Pro - Intel(R) Core(TM) i5-7360U CPU @ 2.30GHz:
					//   ~500 k inserts per second
					// 2024 MacBook Pro - Apple M4 Pro:
					//   ~1.5 m inserts per second

					timer.Reset(maxWait)
					continue
				}
			}
		}

		batchChan <- batch

	end:
	}
}
