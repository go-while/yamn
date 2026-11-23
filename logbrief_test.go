package main

import (
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"testing"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout on 192.0.2.1:25" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestLogBrief(t *testing.T) {
	reject := &textproto.Error{Code: 550, Msg: "5.1.1 <bob@secret.invalid>: no such user"}
	deferred := &textproto.Error{Code: 451, Msg: "4.3.0 <carol@secret.invalid>: later"}
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w: every recipient was rejected: %w", errPermanent, reject), "permanent (SMTP 550)"},
		{fmt.Errorf("mx.secret.invalid:25: no recipient accepted the message yet: %w", deferred), "temporary (SMTP 451)"},
		{&pendingError{1, 3, errors.Join(fmt.Errorf("%w: x", errPermanent), deferred, timeoutErr{})},
			"1 of 3 recipients still to do (SMTP 451, timeout)"},
		{&net.DNSError{Err: "no such host", Name: "secret.invalid"}, "temporary (DNS error)"},
		{&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}, "temporary (network error)"},
		{fmt.Errorf("tls: %w", x509.HostnameError{Host: "mx.secret.invalid"}), "temporary (TLS certificate error)"},
		{fmt.Errorf("mx.secret.invalid:25: %w", errNoTLS), "temporary (" + errNoTLS.Error() + ")"},
		{fmt.Errorf("%w: bob@secret.invalid is a .onion domain", errPermanent), "permanent"},
		{errors.New("something about bob@secret.invalid"), "temporary (error)"},
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
