// Package mailtest runs a tiny SMTP server for tests. It speaks just enough
// SMTP (EHLO, AUTH PLAIN, MAIL, RCPT, DATA, QUIT) to receive one message per
// connection and offers no STARTTLS.
package mailtest

import (
	"bufio"
	"encoding/base64"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flagfall/flagfall/internal/mail"
)

// Received is one delivered message.
type Received struct {
	From, To, Auth, Data string
}

// Server is a running test SMTP server.
type Server struct {
	ln   net.Listener
	mu   sync.Mutex
	msgs []Received
	got  chan struct{}
}

// Start listens on 127.0.0.1 until the test ends.
func Start(t testing.TB) *Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{ln: ln, got: make(chan struct{}, 16)}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s
}

// Settings points a mail.Server at this server (no TLS, no auth).
func (s *Server) Settings() mail.Server {
	host, port, _ := net.SplitHostPort(s.ln.Addr().String())
	p, _ := strconv.Atoi(port)
	return mail.Server{Host: host, Port: p, From: "Flagfall <ctf@example.org>", Security: mail.None}
}

// Last waits up to 3 seconds for a message and returns the newest.
func (s *Server) Last(t testing.TB) Received {
	t.Helper()
	select {
	case <-s.got:
	case <-time.After(3 * time.Second):
		t.Fatal("no email arrived")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.msgs[len(s.msgs)-1]
}

// Count is the number of messages received so far.
func (s *Server) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.msgs)
}

func (s *Server) serve(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(c)
	say := func(line string) { _, _ = c.Write([]byte(line + "\r\n")) }
	var m Received
	say("220 mailtest ready")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			say("250-mailtest")
			say("250 AUTH PLAIN")
		case strings.HasPrefix(cmd, "AUTH PLAIN"):
			b, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(line[len("AUTH PLAIN"):]))
			m.Auth = string(b)
			say("235 ok")
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			m.From = strings.Trim(line[len("MAIL FROM:"):], "<> ")
			say("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			m.To = strings.Trim(line[len("RCPT TO:"):], "<> ")
			say("250 ok")
		case cmd == "DATA":
			say("354 go on")
			var data strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				data.WriteString(l)
			}
			m.Data = data.String()
			s.mu.Lock()
			s.msgs = append(s.msgs, m)
			s.mu.Unlock()
			s.got <- struct{}{}
			say("250 queued")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}
