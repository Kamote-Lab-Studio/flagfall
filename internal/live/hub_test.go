package live

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func readUntil(t *testing.T, sc *bufio.Scanner, want string) []string {
	t.Helper()
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if strings.Contains(sc.Text(), want) {
			return lines
		}
	}
	t.Fatalf("stream ended before %q; got %v", want, lines)
	return nil
}

func connect(t *testing.T, ts *httptest.Server, lastID string) (*bufio.Scanner, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL, nil)
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}
	sc := bufio.NewScanner(res.Body)
	readUntil(t, sc, "retry: 3000")
	return sc, func() { cancel(); res.Body.Close() }
}

func waitClients(t *testing.T, h *Hub, n int) {
	t.Helper()
	for i := 0; i < 200 && h.Clients() != n; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if h.Clients() != n {
		t.Fatalf("clients = %d, want %d", h.Clients(), n)
	}
}

func TestPublishAndReplay(t *testing.T) {
	h := NewHub()
	ts := httptest.NewServer(h)
	defer ts.Close()

	sc, stop := connect(t, ts, "")
	waitClients(t, h, 1)
	h.Publish("announcement", map[string]string{"title": "hello"})
	lines := readUntil(t, sc, `"title":"hello"`)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "id: 1") || !strings.Contains(joined, "event: announcement") {
		t.Fatalf("bad frame: %q", joined)
	}
	stop()
	waitClients(t, h, 0)

	h.Publish("solve", map[string]int{"n": 2})
	h.Publish("solve", map[string]int{"n": 3})

	sc2, stop2 := connect(t, ts, "1")
	defer stop2()
	readUntil(t, sc2, `"n":2`)
	readUntil(t, sc2, `"n":3`)
}

func TestResyncWhenGapTooBig(t *testing.T) {
	h := NewHub()
	ts := httptest.NewServer(h)
	defer ts.Close()
	for i := 0; i < bufferSize+10; i++ {
		h.Publish("solve", i)
	}
	sc, stop := connect(t, ts, "1")
	defer stop()
	readUntil(t, sc, "event: resync")
}

func TestScoreboardCoalesced(t *testing.T) {
	h := NewHub()
	for i := 0; i < 50; i++ {
		h.PublishScoreboard(i)
	}
	time.Sleep(scoreboardEvery + 200*time.Millisecond)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.buffer) != 2 {
		t.Fatalf("published %d scoreboard events for a burst of 50, want 2 (first + trailing)", len(h.buffer))
	}
	if string(h.buffer[1].data) != "49" {
		t.Errorf("trailing event should carry the latest data, got %s", h.buffer[1].data)
	}
}
