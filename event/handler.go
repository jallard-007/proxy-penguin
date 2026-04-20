package event

import (
	"database/sql"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jallard-007/proxy-pengiun/broker"
	"github.com/jallard-007/proxy-pengiun/model"
	"github.com/jallard-007/proxy-pengiun/storage"
)

func Handle(events <-chan model.RecordEvent, s *storage.Storage, b *broker.Broker) {
	uidMap := make(map[uuid.UUID]int64)
	const maxBatch = 50
	const maxWait = 25 * time.Millisecond

	batch := make([]model.RecordEvent, 0, maxBatch)
	successBatch := make([]model.RecordEvent, 0, maxBatch)

	for {
		ev, ok := <-events
		if !ok {
			return
		}
		batch = append(batch, ev)

		timer := time.NewTimer(maxWait)

	collect:
		for len(batch) < maxBatch {
			select {
			case ev, ok := <-events:
				if !ok {
					timer.Stop()
					break collect
				}
				batch = append(batch, ev)
			case <-timer.C:
				break collect
			}
		}
		timer.Stop()

		err := s.Transaction(func(tx *sql.Tx) error {
			for _, evt := range batch {
				rec := evt.Record
				switch evt.Type {
				case model.RecordEventTypeRequestStart:
					// New (pending) record.
					id, err := s.TransactionInsert(tx, rec)
					if err != nil {
						log.Printf("storage insert: %v", err)
						continue
					}
					uidMap[evt.UID] = id
				case model.RecordEventTypeRequestDone:
					if evt.UID == uuid.Nil {
						// New (completed) record.
						_, err := s.TransactionInsert(tx, rec)
						if err != nil {
							log.Printf("storage insert: %v", err)
							continue
						}
					} else {
						id, ok := uidMap[evt.UID]
						if !ok {
							panic("bug! uid not in map")
						}
						delete(uidMap, evt.UID)
						rec.ID = id

						err := s.TransactionUpdate(tx, rec)
						// Completion update for an existing record.
						if err != nil {
							log.Printf("storage update: %v", err)
							continue
						}
					}
				}
				successBatch = append(successBatch, evt)
			}
			return nil
		})

		if err != nil {
			log.Println("storage write:", err)
		}

		b.Publish(successBatch...)

		batch = batch[:0]
		successBatch = successBatch[:0]
	}
}
