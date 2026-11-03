// Package live pushes events to browsers over Server-Sent Events.
// See docs/dev/events.md.
package live

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	bufferSize      = 500
	clientQueue     = 64
	pingInterval    = 25 * time.Second
	scoreboardEvery = time.Second
)

type message struct {
	id   int64
	kind string
	data []byte
}

// Hub fans events out to connected browsers.
type Hub struct {
	mu      sync.Mutex
	nextID  int64
	buffer  []message // ring of recent events for Last-Event-ID replay
	clients map[chan message]struct{}
	closed  bool

	// scoreboard coalescing
	sbPending bool
	sbLast    time.Time
	sbData    any
}

// NewHub returns an empty hub.
func NewHub() *Hub {
	return &Hub{clients: map[chan message]struct{}{}}
}

// Publish sends an event to every connected browser. data is JSON-encoded.
func (h *Hub) Publish(kind string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.publishLocked(kind, b)
}

func (h *Hub) publishLocked(kind string, data []byte) {
	if h.closed {
		return
	}
	h.nextID++
	m := message{id: h.nextID, kind: kind, data: data}
	h.buffer = append(h.buffer, m)
	if len(h.buffer) > bufferSize {
		h.buffer = h.buffer[len(h.buffer)-bufferSize:]
	}
	for c := range h.clients {
		select {
		case c <- m:
		default:
			// Too slow to keep up: drop the client. Its browser reconnects
			// and catches up from the buffer or resyncs.
			delete(h.clients, c)
			close(c)
		}
	}
}

// PublishScoreboard sends a "scoreboard" event, at most once per second.
// Bursts of solves collapse into one event carrying the latest data.
func (h *Hub) PublishScoreboard(data any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sbData = data
	if h.sbPending {
		return
	}
	wait := scoreboardEvery - time.Since(h.sbLast)
	if wait <= 0 {
		h.flushScoreboardLocked()
		return
	}
	h.sbPending = true
	time.AfterFunc(wait, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.sbPending = false
		h.flushScoreboardLocked()
	})
}

func (h *Hub) flushScoreboardLocked() {
	b, err := json.Marshal(h.sbData)
	if err != nil {
		return
	}
	h.sbLast = time.Now()
	h.publishLocked("scoreboard", b)
}

// Clients returns the number of open connections (players online).
func (h *Hub) Clients() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// Close disconnects everyone; used on shutdown.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for c := range h.clients {
		close(c)
		delete(h.clients, c)
	}
}

// subscribe registers a client and returns the events it missed since lastID.
// resync is true when the gap is too big to replay.
func (h *Hub) subscribe(lastID int64) (ch chan message, missed []message, resync bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch = make(chan message, clientQueue)
	if h.closed {
		close(ch)
		return ch, nil, false
	}
	h.clients[ch] = struct{}{}
	if lastID > 0 && lastID < h.nextID {
		if len(h.buffer) == 0 || h.buffer[0].id > lastID+1 {
			return ch, nil, true
		}
		for _, m := range h.buffer {
			if m.id > lastID {
				missed = append(missed, m)
			}
		}
	}
	return ch, missed, false
}

func (h *Hub) unsubscribe(ch chan message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
}

// ServeHTTP streams events to one browser.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	hd := w.Header()
	hd.Set("Content-Type", "text/event-stream")
	hd.Set("Cache-Control", "no-store")
	hd.Set("Connection", "keep-alive")
	hd.Set("X-Accel-Buffering", "no") // nginx: don't buffer the stream
	w.WriteHeader(http.StatusOK)

	lastID, _ := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64)
	ch, missed, resync := h.subscribe(lastID)
	defer h.unsubscribe(ch)

	fmt.Fprint(w, "retry: 3000\n\n")
	if resync {
		fmt.Fprint(w, "event: resync\ndata: {}\n\n")
	}
	for _, m := range missed {
		writeMessage(w, m)
	}
	if rc.Flush() != nil {
		return
	}

	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
		case m, ok := <-ch:
			if !ok {
				return
			}
			writeMessage(w, m)
		}
		if rc.Flush() != nil {
			return
		}
	}
}

func writeMessage(w http.ResponseWriter, m message) {
	fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", m.id, m.kind, m.data)
}
