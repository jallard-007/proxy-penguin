package event

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jallard-007/proxy-pengiun/backend/model"
	"github.com/jallard-007/proxy-pengiun/backend/storage"
)

type FailedInsert struct {
	Event model.RecordEvent
	Err   error
}

// HandleEvents handles events sent over 'events'.
// Processed events are then sent in batches to 'processedEvents'
// To stop this handler, close the events chan.
func HandleEvents(store *storage.Storage, events <-chan model.RecordEvent, processedEvents func(batch []model.RecordEvent)) {
	const maxBatch = 150
	const maxWait = 100 * time.Millisecond
	// batchChanCap controls pipeline depth between the collector and the writer.
	// The slice pool must be batchChanCap+2:
	//   1 currently being collected into
	//   batchChanCap buffered in the channel
	//   1 currently being processed by the writer
	// Raising this lets the collector keep draining events while the writer is
	// mid-transaction, reducing event drops under high load.
	const batchChanCap = 2

	batchChan := make(chan []model.RecordEvent, batchChanCap)

	var wg sync.WaitGroup
	wg.Go(func() {
		// Pre-allocated, reused across batches to avoid per-batch allocations.
		inserts := make([]*model.Request, 0, maxBatch)
		starts := make([]*model.RequestStart, 0, maxBatch)
		updates := make([]*model.Request, 0, maxBatch)

		for batch := range batchChan {
			inserts = inserts[:0]
			starts = starts[:0]
			updates = updates[:0]

			for _, evt := range batch {
				switch evt.Type {
				case model.RecordEventTypeRequest:
					inserts = append(inserts, evt.Record.(*model.Request))
				case model.RecordEventTypeRequestStart:
					starts = append(starts, evt.Record.(*model.RequestStart))
				case model.RecordEventTypeRequestDone:
					updates = append(updates, evt.Record.(*model.Request))
				}
			}

			err := store.Transaction(context.Background(), func(tx storage.Transaction) error {
				if err := store.TransactionBatchInsertRequests(&tx, inserts); err != nil {
					return err
				}
				if err := store.TransactionBatchInsertRequestStarts(&tx, starts); err != nil {
					return err
				}
				for _, r := range updates {
					if err := store.TransactionUpdateRequestDone(&tx, r); err != nil {
						log.Println("failed to update request done:", err)
					}
				}
				return nil
			})

			if err != nil {
				log.Println("storage write:", err)
				continue
			}

			processedEvents(batch)
		}
	})

	batches := [batchChanCap + 2][]model.RecordEvent{}

	for i := range len(batches) {
		batches[i] = make([]model.RecordEvent, 0, maxBatch)
	}

	timer := time.NewTimer(maxWait)
	defer timer.Stop()

	for {
		for i := range len(batches) {
			batch := batches[i]
			batch = batch[:0]
			ev, ok := <-events
			if !ok {
				close(batchChan)
				wg.Wait()
				return
			}
			batch = append(batch, ev)

			timer.Reset(maxWait)

		collect:
			for len(batch) < maxBatch {
				select {
				case ev, ok := <-events:
					if !ok {
						break collect
					}
					batch = append(batch, ev)
				case <-timer.C:
					break collect
				}
			}
			batchChan <- batch
		}
	}
}
