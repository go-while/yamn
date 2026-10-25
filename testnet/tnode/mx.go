package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

const (
	maxMessage    = 20 << 20 // bytes
	maxRecipients = 100
	maxReceived   = 30 // Received headers; more means a mail loop
	cmdTimeout    = 5 * time.Minute
)

// MX is the SMTP relay of one node.
type MX struct {
	Self    Node
	Nodes   map[string]Node
	Deliver map[string]string // local part -> Maildir
	Spool   string

	kick     chan struct{}
	hostname string
	seq      atomic.Uint64
}

// Init checks the Maildirs and creates the spool.
func (m *MX) Init() error {
	m.kick = make(chan struct{}, 1)
	m.hostname, _ = os.Hostname()
	if m.hostname == "" {
		m.hostname = m.Self.Name
	}
	for user, dir := range m.Deliver {
		for _, sub := range []string{"tmp", "new", "cur"} {
			if err := os.MkdirAll(filepath.Join(dir, sub), 0700); err != nil {
				return fmt.Errorf("Maildir of %s: %w", user, err)
			}
		}
	}
	return os.MkdirAll(m.Spool, 0700)
}

// Serve accepts SMTP connections until ln is closed.
func (m *MX) Serve(ln net.Listener) error {
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		go m.session(c)
	}
}

// rcpt is a recipient as given, and where it goes.
type rcpt struct {
	addr  string // as received, for X-Original-To
	local string // Maildir, or "" to forward
	node  string // node to forward to
}

// route decides where addr goes, or why it's refused.
func (m *MX) route(addr string) (rcpt, error) {
	at := strings.LastIndexByte(addr, '@')
	if at <= 0 || at == len(addr)-1 {
		return rcpt{}, errors.New("550 5.1.3 not an address")
	}
	user := strings.ToLower(addr[:at])
	domain := strings.ToLower(addr[at+1:])
	name, ok := strings.CutSuffix(domain, "."+Domain)
	node, known := m.Nodes[name]
	if !ok || !known {
		return rcpt{}, fmt.Errorf("550 5.7.1 %s is not in the test network", domain)
	}
	if node.Name != m.Self.Name {
		return rcpt{addr: addr, node: node.Name}, nil
	}
	base, _, _ := strings.Cut(user, "+")
	dir, ok := m.Deliver[base]
	if !ok {
		return rcpt{}, fmt.Errorf("550 5.1.1 no mailbox %s here", base)
	}
	return rcpt{addr: addr, local: dir}, nil
}

// pathArg returns the address in "FROM:<addr> params" or "TO:<addr>".
func pathArg(arg, prefix string) (string, bool) {
	if len(arg) < len(prefix) || !strings.EqualFold(arg[:len(prefix)], prefix) {
		return "", false
	}
	arg = strings.TrimSpace(arg[len(prefix):])
	if !strings.HasPrefix(arg, "<") {
		return "", false
	}
	end := strings.IndexByte(arg, '>')
	if end < 0 {
		return "", false
	}
	return arg[1:end], true
}

func (m *MX) session(c net.Conn) {
	defer c.Close()
	remote, _, _ := net.SplitHostPort(c.RemoteAddr().String())
	tc := textproto.NewConn(c)
	reply := func(format string, a ...any) error {
		c.SetDeadline(time.Now().Add(cmdTimeout))
		return tc.PrintfLine(format, a...)
	}
	if reply("220 %s tnode ESMTP (yamn test network)", m.Self.Domain()) != nil {
		return
	}
	var (
		helo  string
		from  *string
		rcpts []rcpt
	)
	for {
		c.SetDeadline(time.Now().Add(cmdTimeout))
		line, err := tc.ReadLine()
		if err != nil {
			return
		}
		verb, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(verb) {
		case "EHLO":
			helo = arg
			from, rcpts = nil, nil
			err = reply("250-%s\r\n250-SIZE %d\r\n250 8BITMIME", m.Self.Domain(), maxMessage)
		case "HELO":
			helo = arg
			from, rcpts = nil, nil
			err = reply("250 %s", m.Self.Domain())
		case "MAIL":
			addr, ok := pathArg(arg, "FROM:")
			if !ok {
				err = reply("501 5.5.4 MAIL FROM:<address>")
				break
			}
			from, rcpts = &addr, nil
			err = reply("250 2.1.0 ok")
		case "RCPT":
			addr, ok := pathArg(arg, "TO:")
			switch {
			case from == nil:
				err = reply("503 5.5.1 MAIL first")
			case !ok:
				err = reply("501 5.5.4 RCPT TO:<address>")
			case len(rcpts) >= maxRecipients:
				err = reply("452 4.5.3 too many recipients")
			default:
				r, rerr := m.route(addr)
				if rerr != nil {
					log.Printf("refused rcpt <%s> from <%s> (%s): %v", addr, *from, remote, rerr)
					err = reply("%s", rerr.Error())
					break
				}
				rcpts = append(rcpts, r)
				err = reply("250 2.1.5 ok")
			}
		case "DATA":
			if from == nil || len(rcpts) == 0 {
				err = reply("503 5.5.1 MAIL and RCPT first")
				break
			}
			if err = reply("354 end with <CRLF>.<CRLF>"); err != nil {
				return
			}
			data, rerr := io.ReadAll(io.LimitReader(tc.DotReader(), maxMessage+1))
			if rerr != nil {
				return
			}
			switch {
			case len(data) > maxMessage:
				err = reply("552 5.3.4 message too big")
			case received(data) > maxReceived:
				log.Printf("refused mail from <%s>: %d Received headers, a loop", *from, received(data))
				err = reply("554 5.4.6 mail loop")
			default:
				id, aerr := m.accept(*from, rcpts, helo, remote, data)
				if aerr != nil {
					log.Printf("accepting mail from <%s>: %v", *from, aerr)
					err = reply("451 4.3.0 %v", aerr)
				} else {
					err = reply("250 2.0.0 ok %s", id)
				}
			}
			from, rcpts = nil, nil
		case "RSET":
			from, rcpts = nil, nil
			err = reply("250 2.0.0 ok")
		case "NOOP":
			err = reply("250 2.0.0 ok")
		case "VRFY":
			err = reply("252 2.5.0 not verified")
		case "QUIT":
			reply("221 2.0.0 bye")
			return
		default:
			err = reply("502 5.5.2 not implemented")
		}
		if err != nil {
			return
		}
	}
}

// received counts the Received headers of a message.
func received(data []byte) (n int) {
	header, _, _ := bytes.Cut(data, []byte("\n\n"))
	for _, l := range bytes.Split(header, []byte("\n")) {
		if len(l) > 9 && strings.EqualFold(string(l[:9]), "received:") {
			n++
		}
	}
	return
}

func (m *MX) newID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("%x.%d.%s", time.Now().UnixNano(), m.seq.Add(1), hex.EncodeToString(b))
}

// accept delivers a message to its local recipients and spools it for the
// others, one spool entry per node.
func (m *MX) accept(from string, rcpts []rcpt, helo, remote string, data []byte) (string, error) {
	id := m.newID()
	recv := fmt.Sprintf("Received: from %s ([%s]) by %s (tnode) id %s; %s\n",
		helo, remote, m.Self.Domain(), id, time.Now().UTC().Format(time.RFC1123Z))
	log.Printf("in %s from <%s> [%s] size %d rcpts %d", id, from, remote, len(data), len(rcpts))
	forward := make(map[string][]string)
	for _, r := range rcpts {
		if r.local == "" {
			forward[r.node] = append(forward[r.node], r.addr)
			continue
		}
		head := fmt.Sprintf("Return-Path: <%s>\nDelivered-To: %s\nX-Original-To: %s\n", from, r.addr, r.addr)
		file, err := writeMaildir(r.local, m.hostname, m.newID(), []byte(head+recv), data)
		if err != nil {
			return "", fmt.Errorf("deliver to %s: %w", r.addr, err)
		}
		log.Printf("delivered %s to %s: %s", id, r.addr, file)
	}
	for node, addrs := range forward {
		e := &spoolEntry{ID: id + "." + node, From: from, Rcpts: addrs, Node: node, Created: time.Now()}
		if err := m.spoolPut(e, append([]byte(recv), data...)); err != nil {
			return "", fmt.Errorf("spool for %s: %w", node, err)
		}
		log.Printf("queued %s for %s: %s", id, node, strings.Join(addrs, ", "))
	}
	if len(forward) > 0 {
		select {
		case m.kick <- struct{}{}:
		default:
		}
	}
	return id, nil
}

// writeMaildir writes a message into a Maildir: into tmp, then renamed
// into new.  Files are 0660, so a Maildir shared through its group (e.g.
// a pinger's) can be read by its owner.
func writeMaildir(dir, host, id string, parts ...[]byte) (string, error) {
	name := id + "." + host
	tmp := filepath.Join(dir, "tmp", name)
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0660)
	if err != nil {
		return "", err
	}
	for _, p := range parts {
		if _, err = f.Write(p); err != nil {
			break
		}
	}
	if err == nil {
		err = f.Chmod(0660) // past the umask
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	dst := filepath.Join(dir, "new", name)
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return dst, nil
}
