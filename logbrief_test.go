package main

import (
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"os"
	"strings"
	"syscall"
	"testing"
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
