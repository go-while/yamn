package main

import (
	"bytes"
	"fmt"
	"io"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

// Encrypted yamn mails can be written to stdout (--stdout) and read back in
// (-R, --read-mail) as an mbox: one or more mails, each starting with a
// "From " separator line.  The mboxrd variant is used: a body line starting
// with "From " (after any number of ">") gets one more ">" on writing and
// loses it on reading, so mails are passed through unchanged.

// mboxFromLine matches a line that has to be escaped: "From ", ">From ",
// ">>From " and so on.
var mboxFromLine = regexp.MustCompile(`^>*From `)

// writeYamnMail writes a complete mail carrying an armored yamn packet:
// headers addressed to sendTo, then the armored payload.  It's what goes
// into the pool for sending, and what --stdout prints.
func writeYamnMail(w io.Writer, sendTo string, payload []byte) error {
	// sendTo becomes the To header.  A line break in it would let extra
	// headers be injected into the outgoing mail.
	if strings.ContainsAny(sendTo, "\r\n") {
		return fmt.Errorf("refusing to write mail: recipient contains a line break: %q", sendTo)
	}
	writeMailHeaders(w, sendTo)
	armor(w, payload)
	return nil
}

// writeMbox appends mail to w as one mbox entry.
func writeMbox(w io.Writer, mail []byte) error {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From yamn %s\n", time.Now().UTC().Format(time.ANSIC))
	for _, line := range strings.SplitAfter(string(mail), "\n") {
		if line == "" {
			continue
		}
		if mboxFromLine.MatchString(line) {
			buf.WriteByte('>')
		}
		buf.WriteString(line)
	}
	if !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
		buf.WriteByte('\n')
	}
	// A blank line ends the entry.
	buf.WriteByte('\n')
	_, err := w.Write(buf.Bytes())
	return err
}

// isMbox reports whether data looks like an mbox rather than a single mail.
// A mail starts with a header, and "From " (with a space, no colon) is never
// a valid header name.
func isMbox(data []byte) bool {
	return bytes.HasPrefix(data, []byte("From "))
}

// splitMbox returns the mails in an mbox, with the separator lines removed
// and escaped "From " lines restored.
func splitMbox(data []byte) (mails [][]byte) {
	var current *bytes.Buffer
	finish := func() {
		if current == nil {
			return
		}
		// Drop the blank line that ends each entry.
		mail := bytes.TrimSuffix(current.Bytes(), []byte("\n"))
		mails = append(mails, mail)
	}
	for _, line := range strings.SplitAfter(string(data), "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "From ") {
			finish()
			current = new(bytes.Buffer)
			continue
		}
		if current == nil {
			// Text before the first separator isn't part of any mail.
			continue
		}
		if mboxFromLine.MatchString(line) {
			line = line[1:]
		}
		current.WriteString(line)
	}
	finish()
	return
}

// importMails reads encrypted yamn mails from r (an mbox as written by
// --stdout, or a single mail) and puts each one into the pool, to be sent
// later (--import, then -S).  Every mail must have a single To address and
// a valid armored yamn packet; anything else is skipped and reported.
func importMails(r io.Reader) (imported int, errs []error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, []error{err}
	}
	mails := [][]byte{data}
	if isMbox(data) {
		mails = splitMbox(data)
	}
	for i, m := range mails {
		if err := importMail(m); err != nil {
			errs = append(errs, fmt.Errorf("mail %d: %w", i+1, err))
			continue
		}
		imported++
	}
	return
}

// importMail checks one encrypted mail and writes it into the pool the same
// way the client would.
func importMail(m []byte) error {
	msg, err := mail.ReadMessage(bytes.NewReader(m))
	if err != nil {
		return fmt.Errorf("not a mail: %w", err)
	}
	to, err := mail.ParseAddress(msg.Header.Get("To"))
	if err != nil {
		return fmt.Errorf("no valid To address: %w", err)
	}
	payload, err := stripArmor(bytes.NewReader(m))
	if err != nil {
		return fmt.Errorf("no valid yamn packet: %w", err)
	}
	return writeMessageToPool(to.Address, payload)
}
