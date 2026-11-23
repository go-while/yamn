package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/Masterminds/log-go"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout on 192.0.2.1:25" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// A pendingError never counts as permanent, even with a permanent cause:
// the message still has recipients to do.
func TestPendingNotPermanent(t *testing.T) {
	err := &pendingError{1, 2, errors.Join(fmt.Errorf("%w: refused", errPermanent), errors.New("later"))}
	if errors.Is(err, errPermanent) {
		t.Error("a pendingError is errPermanent")
	}
}

func TestLogBrief(t *testing.T) {
	reject := &textproto.Error{Code: 550, Msg: "5.1.1 <bob@secret.invalid>: no such user"}
	deferred := &textproto.Error{Code: 451, Msg: "4.3.0 <carol@secret.invalid>: later"}
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w: every recipient was rejected: %w", errPermanent, reject), "permanent: SMTP 550 5.1.1"},
		{fmt.Errorf("mx.secret.invalid:25: no recipient accepted the message yet: %w", deferred), "temporary: SMTP 451 4.3.0"},
		{&pendingError{1, 3, errors.Join(fmt.Errorf("%w: x", errPermanent), deferred, timeoutErr{})},
			"1 of 3 recipients still to do: SMTP 451 4.3.0, timeout"},
		{&net.DNSError{Err: "no such host", Name: "secret.invalid"}, "temporary: DNS error"},
		{&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}, "temporary: network error"},
		{&net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "mx.secret.invalid"}}, "temporary: DNS error"},
		{&textproto.Error{Code: 554, Msg: "no enhanced code here, bob@secret.invalid"}, "temporary: SMTP 554"},
		{fmt.Errorf("tls: %w", x509.HostnameError{Host: "mx.secret.invalid"}), "temporary: TLS certificate error"},
		{fmt.Errorf("mx.secret.invalid:25: %w", errNoTLS), "temporary: " + errNoTLS.Error()},
		{fmt.Errorf("%w: bob@secret.invalid is a .onion domain", errPermanent), "permanent: error"},
		{errors.New("something about bob@secret.invalid"), "temporary: error"},
		// A file error isn't a network error, although syscall.Errno
		// is a net.Error.
		{&os.PathError{Op: "open", Path: "/x/dest.blk", Err: syscall.EACCES}, "temporary: error"},
		{safeErrorf("m123: %d recipients exceed max_recipients of %d", 9, 5),
			"temporary: m123: 9 recipients exceed max_recipients of 5"},
	} {
		got := logBrief(tc.err)
		if got != tc.want {
			t.Errorf("logBrief(%q) = %q, want %q", tc.err, got, tc.want)
		}
		if strings.Contains(got, "secret") || strings.Contains(got, "192.0.2") {
			t.Errorf("logBrief(%q) = %q leaks", tc.err, got)
		}
	}
}

// TestSMTPConnLog checks that with smtp_relay, a connection error is
// logged in full, but a reply from the relay (well-formed or not), or any
// other error, only by its code or kind.
func TestSMTPConnLog(t *testing.T) {
	buf := logAt(t, log.InfoLevel)
	smtpConnLog(log.Warnf, "SMTP MAIL FROM", "localhost:587", false,
		&textproto.Error{Code: 421, Msg: "4.4.2 previous delivery to bob@secret.invalid failed"})
	smtpConnLog(log.Warnf, "SMTP connection", "localhost:587", false,
		textproto.ProtocolError("short response: oops previous delivery to bob@secret.invalid"))
	smtpConnLog(log.Warnf, "SMTP EHLO", "localhost:587", false,
		errors.New("unexpected text from bob@secret.invalid"))
	smtpConnLog(log.Warnf, "STARTTLS", "localhost:587", false,
		&tls.CertificateVerificationError{Err: x509.HostnameError{Certificate: &x509.Certificate{DNSNames: []string{"bob.secret.invalid"}}, Host: "localhost"}})
	smtpConnLog(log.Warnf, "Dial", "localhost:587", false,
		&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")})
	out := buf.String()
	if strings.Contains(out, "secret") {
		t.Errorf("the relay's reply text is logged:\n%s", out)
	}
	for _, want := range []string{"SMTP MAIL FROM failed: localhost:587, SMTP 421 4.4.2",
		"STARTTLS failed: localhost:587, TLS certificate error", "Dial failed: localhost:587, dial tcp: connection refused"} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}
}
