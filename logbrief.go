package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"slices"
	"strings"

	"github.com/Masterminds/log-go"
)

// Delivery errors carry recipient addresses, the recipients' MX hosts and
// the servers' replies, which quote the recipients.  An exit that logs them
// at info level keeps a record of who got mail when.  Info and warn logs
// therefore get logBrief(err), and the full error goes to trace.

// errNoTLS and errRelayNoTLS are delivery errors whose text is safe to log.
var (
	errNoTLS      = errors.New("no TLS available and require_tls is set")
	errRelayNoTLS = errors.New("configured relay does not offer STARTTLS")
)

// safeError marks an error whose whole text is safe to log: yamn's own
// words, a file name or a count, nothing from the mail.
type safeError struct{ error }

func (e safeError) Unwrap() error { return e.error }

// safeErrorf is fmt.Errorf for an error whose text is safe to log.
func safeErrorf(format string, a ...interface{}) error {
	return safeError{fmt.Errorf(format, a...)}
}

// pendingError reports a message with recipients still to do.  It lists
// its causes but doesn't wrap them: some may be errPermanent, while the
// message as a whole waits for a retry.
type pendingError struct {
	todo, total int
	causes      error
}

func (e *pendingError) Error() string {
	return fmt.Sprintf("%d of %d recipients still to do: %v", e.todo, e.total, e.causes)
}

// logBrief describes a delivery error without addresses, host names or reply
// text: permanent or temporary, the SMTP reply codes, and the kind of any
// other error.
func logBrief(err error) string {
	var pe *pendingError
	switch {
	case errors.As(err, &pe):
		return logKinds(err)
	case errors.Is(err, errPermanent):
		return "permanent: " + logKinds(err)
	default:
		return "temporary: " + logKinds(err)
	}
}

// logKinds is logBrief without "permanent" or "temporary", for lines that
// say what happens to the message themselves.
func logKinds(err error) string {
	var kinds []string
	var pe *pendingError
	prefix := ""
	if errors.As(err, &pe) {
		prefix = fmt.Sprintf("%d of %d recipients still to do: ", pe.todo, pe.total)
		err = pe.causes
	}
	errKinds(err, &kinds)
	if len(kinds) == 0 {
		kinds = []string{"error"}
	}
	return prefix + strings.Join(kinds, ", ")
}

// errKinds adds the kinds of err's leaf errors to kinds, once each.
func errKinds(err error, kinds *[]string) {
	add := func(k string) {
		if !slices.Contains(*kinds, k) {
			*kinds = append(*kinds, k)
		}
	}
	switch e := err.(type) {
	case nil:
		return
	case *pendingError:
		errKinds(e.causes, kinds)
		return
	case *textproto.Error:
		add(fmt.Sprintf("SMTP %d", e.Code))
		return
	case safeError:
		add(e.Error())
		return
	}
	switch err {
	case errPermanent:
		return
	case errNoTLS, errRelayNoTLS:
		add(err.Error())
		return
	}
	// Network and TLS errors wrap their cause (connection refused, ...):
	// name the kind where it shows, before unwrapping.
	if k := netKind(err); k != "" {
		add(k)
		return
	}
	if u, ok := err.(interface{ Unwrap() []error }); ok {
		for _, e := range u.Unwrap() {
			errKinds(e, kinds)
		}
		return
	}
	if u := errors.Unwrap(err); u != nil {
		errKinds(u, kinds)
		return
	}
	add(leafKind(err))
}

// netKind names err's kind if err itself is a network, DNS or TLS error,
// else "".
func netKind(err error) string {
	switch e := err.(type) {
	case *net.DNSError:
		return "DNS error"
	case *tls.CertificateVerificationError, x509.UnknownAuthorityError, x509.HostnameError, x509.CertificateInvalidError:
		return "TLS certificate error"
	case tls.AlertError, tls.RecordHeaderError:
		return "TLS error"
	case *net.OpError:
		if e.Timeout() {
			return "timeout"
		}
		return "network error"
	case net.Error:
		// syscall.Errno is a net.Error too, also for file errors.
		if e.Timeout() {
			return "timeout"
		}
	}
	return ""
}

// leafKind names the kind of an error that wraps nothing.
func leafKind(err error) string {
	if k := netKind(err); k != "" {
		return k
	}
	return "error"
}

// smtpLog logs a failed SMTP step with logf (info or warn) without the
// recipients' MX or reply text, and in full at trace level.  A what ending
// in ":" is a sentence of its own ("Recipient rejected:"); otherwise
// " failed" follows it.
func smtpLog(logf func(string, ...interface{}), what, serverAddr string, directMX bool, err error) {
	if !strings.HasSuffix(what, ":") {
		what += " failed:"
	}
	logf("%s %s, %s", what, serverFor(serverAddr, directMX), logKinds(err))
	log.Tracef("%s Server=%s, Error=%s", what, serverAddr, err)
}

// serverFor names a server in info and warn logs: smtp_relay by its
// address (the operator's own setting), a recipient's MX not at all.
func serverFor(serverAddr string, directMX bool) string {
	if directMX {
		return "a recipient's MX"
	}
	return serverAddr
}
