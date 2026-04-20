package api

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/goccy/go-json"
	"github.com/jallard-007/proxy-pengiun/backend/event"
)

const heartbeatFrequency = 30 * time.Second

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

	switch {
	case s.cfg.MaxStreamConnections == 0:
		// no limit
	case s.cfg.MaxStreamConnections < 0:
		// this should be gated during router registration
		http.Error(w, "streaming not enabled", http.StatusNotFound)
		return
	case s.cfg.MaxStreamConnections > 0:
		defer s.numClients.Add(-1)
		if s.numClients.Add(1) > s.cfg.MaxStreamConnections {
			http.Error(w, "max total stream connections reached. wait for someone to disconnect", http.StatusLocked)
			return
		}
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
		ch: make(chan []byte, 100),
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

func (s *Server) RunSSE(ctx context.Context, ePool *event.EventPool, evtsChan <-chan *event.RecordEvents) {
	heartbeat := time.NewTicker(heartbeatFrequency)
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

				if len(evts.Request) == 0 {
					ePool.Put(evts)
					continue
				}

				for i := 0; i < len(evts.Request); i++ {
					bytesBuf.WriteString("event: ")
					bytesBuf.WriteString(string(evts.Type[i]))
					bytesBuf.WriteString("\ndata: ")
					err := json.NewEncoder(&bytesBuf).Encode(&evts.Request[i])
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
				heartbeat.Reset(heartbeatFrequency)
				ePool.Put(evts)

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

func (s *Server) SSEAvailable() bool {
	return s.cfg.MaxStreamConnections >= 0
}
