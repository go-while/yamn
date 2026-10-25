package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	dialTimeout = 30 * time.Second
	sendTimeout = 5 * time.Minute
	maxBackoff  = 10 * time.Minute
	giveUpAfter = 48 * time.Hour
	scanEvery   = 10 * time.Second
)

// spoolEntry is mail waiting to be forwarded to one node.  Its message is
// in <ID>.msg next to <ID>.json.
type spoolEntry struct {
	ID       string
	From     string
	Rcpts    []string
	Node     string
	Created  time.Time
	Attempts int
	Next     time.Time
	LastErr  string `json:",omitempty"`
}

func (m *MX) spoolPath(id, ext string) string { return filepath.Join(m.Spool, id+ext) }

// spoolPut writes the message, then the entry, so an entry always has its
// message.
func (m *MX) spoolPut(e *spoolEntry, msg []byte) error {
	if msg != nil {
		if err := writeAtomic(m.spoolPath(e.ID, ".msg"), msg); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(m.spoolPath(e.ID, ".json"), b)
}

func (m *MX) spoolDrop(e *spoolEntry) {
	os.Remove(m.spoolPath(e.ID, ".json"))
	os.Remove(m.spoolPath(e.ID, ".msg"))
}

func writeAtomic(file string, data []byte) error {
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// ForwardLoop forwards spooled mail until stop is closed: every few
// seconds, and at once when new mail is spooled.
func (m *MX) ForwardLoop(stop <-chan struct{}) {
	t := time.NewTicker(scanEvery)
	defer t.Stop()
	for {
		m.forwardDue(time.Now())
		select {
		case <-stop:
			return
		case <-t.C:
		case <-m.kick:
		}
	}
}

// forwardDue tries every entry whose time has come.
func (m *MX) forwardDue(now time.Time) {
	files, _ := filepath.Glob(filepath.Join(m.Spool, "*.json"))
	for _, f := range files {
		var e spoolEntry
		b, err := os.ReadFile(f)
		if err == nil {
			err = json.Unmarshal(b, &e)
		}
		if err != nil {
			log.Printf("spool %s unreadable, removed: %v", f, err)
			os.Remove(f)
			continue
		}
		if now.Before(e.Next) {
			continue
		}
		m.forwardOne(&e, now)
	}
}

func (m *MX) forwardOne(e *spoolEntry, now time.Time) {
	msg, err := os.ReadFile(m.spoolPath(e.ID, ".msg"))
	if err != nil {
		log.Printf("forward %s: message missing, dropped: %v", e.ID, err)
		m.spoolDrop(e)
		return
	}
	node, ok := m.Nodes[e.Node]
	if !ok {
		log.Printf("forward %s: node %s no longer in the table, dropped", e.ID, e.Node)
		m.spoolDrop(e)
		return
	}
	left, err := m.send(node, e.From, e.Rcpts, msg)
	if err == nil && len(left) == 0 {
		log.Printf("forwarded %s to %s (%s)", e.ID, node.Name, strings.Join(e.Rcpts, ", "))
		m.spoolDrop(e)
		return
	}
	if len(left) == 0 {
		// Every recipient was refused for good: there are no bounces in
		// the test network, so the mail ends here.
		log.Printf("forward %s to %s: refused, dropped: %v", e.ID, node.Name, err)
		m.spoolDrop(e)
		return
	}
	e.Rcpts = left
	e.Attempts++
	e.LastErr = fmt.Sprint(err)
	if now.Sub(e.Created) > giveUpAfter {
		log.Printf("forward %s to %s: gave up after %s: %v", e.ID, node.Name, giveUpAfter, err)
		m.spoolDrop(e)
		return
	}
	backoff := min(time.Duration(1<<min(e.Attempts, 10))*15*time.Second, maxBackoff)
	e.Next = now.Add(backoff)
	log.Printf("forward %s to %s: %v; retry in %s", e.ID, node.Name, err, backoff)
	if err := m.spoolPut(e, nil); err != nil {
		log.Printf("forward %s: %v", e.ID, err)
	}
}

// permanent reports whether err is an SMTP 5xx reply.
func permanent(err error) bool {
	var te *textproto.Error
	return errors.As(err, &te) && te.Code >= 500
}

// send hands msg to a node's mx.  It returns the recipients to try again:
// none on success or when all were refused for good.
func (m *MX) send(node Node, from string, rcpts []string, msg []byte) (retry []string, err error) {
	conn, err := net.DialTimeout("tcp", node.Addr, dialTimeout)
	if err != nil {
		return rcpts, err
	}
	conn.SetDeadline(time.Now().Add(sendTimeout))
	host, _, _ := net.SplitHostPort(node.Addr)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return rcpts, err
	}
	defer c.Close()
	if err = c.Hello(m.Self.Domain()); err != nil {
		return rcpts, err
	}
	if err = c.Mail(from); err != nil {
		if permanent(err) {
			return nil, err
		}
		return rcpts, err
	}
	var accepted []string
	for _, r := range rcpts {
		rerr := c.Rcpt(r)
		switch {
		case rerr == nil:
			accepted = append(accepted, r)
		case permanent(rerr):
			log.Printf("forward to %s: <%s> refused: %v", node.Name, r, rerr)
			err = rerr
		default:
			retry = append(retry, r)
			err = rerr
		}
	}
	if len(accepted) == 0 {
		return retry, err
	}
	w, derr := c.Data()
	if derr == nil {
		_, derr = w.Write(msg)
		if cerr := w.Close(); derr == nil {
			derr = cerr
		}
	}
	if derr != nil {
		if permanent(derr) {
			return retry, derr
		}
		return append(retry, accepted...), derr
	}
	c.Quit()
	return retry, err
}
