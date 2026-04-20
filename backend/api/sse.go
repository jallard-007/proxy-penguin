package api

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/goccy/go-json"

	"github.com/jallard-007/proxy-pengiun/backend/model"
)

const heartbeatDuration = 30 * time.Second

var heartbeatMessage = []byte(": heartbeat\n\n")
var connectedMessage = []byte("event: connected\ndata: {}\n\n")

type client struct {
	ch chan []byte
}

// https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events#event_stream_format

func (s *Server) HandleRequestsStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Send a connected event so the client knows the stream is ready.
	w.WriteHeader(http.StatusOK)
	w.Write(connectedMessage)
	flusher.Flush()

	c := &client{
		ch: make(chan []byte, 10),
	}

	s.register <- c
	defer func() {
		s.unregister <- c
	}()

	for {
		select {
		case <-ctx.Done():
			log.Println("client disconnected")
			return
		case b, ok := <-c.ch:
			if !ok {
				return
			}
			_, err := w.Write(b)
			if err != nil {
				log.Println("err writing to client:", err)
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) RunSSE(ctx context.Context, evtsChan <-chan []model.RecordEvent) {
	heartbeat := time.NewTicker(heartbeatDuration)
	defer heartbeat.Stop()

	var wg sync.WaitGroup
	wg.Go(func() {
		bytesBuf := bytes.Buffer{}
		for {
			select {
			case evts, ok := <-evtsChan:
				if !ok {
					return
				}

				if len(evts) == 0 {
					continue
				}

				for _, e := range evts {
					bytesBuf.WriteString("event: ")
					bytesBuf.WriteString(string(e.Type))
					bytesBuf.WriteString("\ndata: ")
					err := json.NewEncoder(&bytesBuf).Encode(e.Record)
					if err != nil {
						panic(err)
					}
					bytesBuf.WriteByte('\n')
					bytesBuf.WriteByte('\n')
				}

				cp := make([]byte, bytesBuf.Len())
				copy(cp, bytesBuf.Bytes())
				s.publish <- cp
				bytesBuf.Reset()
				heartbeat.Reset(heartbeatDuration)

			case <-heartbeat.C:
				s.publish <- heartbeatMessage

			case <-ctx.Done():
				return
			}
		}
	})

	for {
		select {
		case c := <-s.register:
			s.clients[c] = struct{}{}

		case c := <-s.unregister:
			_, ok := s.clients[c]
			if ok {
				log.Println("unregistered client")
				delete(s.clients, c)
				close(c.ch)
			}

		case msg := <-s.publish:
			for c := range s.clients {
				select {
				case c.ch <- msg:
				default:
					// drop, disconnect, or apply backpressure policy
					s.unregister <- c // drop
				}
			}

		case <-ctx.Done():
			for c := range s.clients {
				close(c.ch)
			}
			s.clients = map[*client]struct{}{}
			wg.Wait()
			return
		}
	}
}
