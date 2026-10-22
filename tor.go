package main

import (
	"context"
	"crypto/rand"
	"crypto/sha3"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// Mail to .onion hosts goes through Tor's SOCKS port (mail.tor_socks).
// The .onion name itself is sent to Tor and never looked up in DNS.

// torDialTimeout bounds connecting to a .onion host, the SOCKS handshake
// included.  Tor may need a minute or more to build the circuits to an
// onion service.  Tests shorten it.
var torDialTimeout = 2 * time.Minute

// onionDelivery reports whether .onion hosts can be reached.
func onionDelivery() bool {
	return cfg.Mail.TorSocks != ""
}

// validOnion reports whether host is a v3 onion address, optionally with
// subdomains (Tor ignores those): 56 base32 characters encoding the
// service's public key, a checksum and version 3.  Version 2 addresses
// (16 characters) stopped working in 2021.
func validOnion(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	name, ok := strings.CutSuffix(host, ".onion")
	if !ok {
		return false
	}
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		name = name[i+1:]
	}
	if len(name) != 56 {
		return false
	}
	raw, err := base32.StdEncoding.DecodeString(strings.ToUpper(name))
	if err != nil || len(raw) != 35 {
		return false
	}
	pubkey, checksum, version := raw[:32], raw[32:34], raw[34]
	if version != 3 {
		return false
	}
	h := sha3.New256()
	h.Write([]byte(".onion checksum"))
	h.Write(pubkey)
	h.Write([]byte{version})
	sum := h.Sum(nil)
	return sum[0] == checksum[0] && sum[1] == checksum[1]
}

// onionDial connects to a .onion host:port through mail.tor_socks.  Tests
// replace it.
var onionDial = func(address string) (net.Conn, error) {
	return torDial(cfg.Mail.TorSocks, address, torDialTimeout)
}

// torDial connects to address (host:port) through the SOCKS5 proxy at
// proxyAddr.  The host name goes to the proxy as it is, so Tor resolves
// it, not the local resolver.  Every call logs in with a new random user
// name, so Tor (IsolateSOCKSAuth, on by default) builds it its own
// circuit, and deliveries of different messages don't share one.
func torDial(proxyAddr, address string, timeout time.Duration) (net.Conn, error) {
	user := make([]byte, 16)
	if _, err := rand.Read(user); err != nil {
		return nil, err
	}
	auth := &proxy.Auth{User: hex.EncodeToString(user), Password: "x"}
	dialer, err := proxy.SOCKS5("tcp", proxyAddr, auth, &net.Dialer{Timeout: timeout})
	if err != nil {
		return nil, fmt.Errorf("tor_socks %s: %w", proxyAddr, err)
	}
	// The deadline covers the SOCKS handshake too, which waits for Tor
	// to reach the onion service.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	conn, err := dialer.(proxy.ContextDialer).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("tor_socks %s to %s: %w", proxyAddr, address, describeTorError(err))
	}
	return conn, nil
}

// describeTorError adds the meaning of Tor's own SOCKS5 reply codes for
// onion services, which golang.org/x/net/proxy reports as "unknown code:
// N".  Tor sends them with the SocksPort flag ExtendedErrors.
func describeTorError(err error) error {
	_, after, ok := strings.Cut(err.Error(), "unknown code: ")
	if !ok {
		return err
	}
	end := strings.IndexFunc(after, func(c rune) bool { return c < '0' || c > '9' })
	if end < 0 {
		end = len(after)
	}
	code, cerr := strconv.Atoi(after[:end])
	if cerr != nil {
		return err
	}
	if desc := torReply(code); desc != "" {
		return fmt.Errorf("%w (%s)", err, desc)
	}
	return err
}

// torReply describes Tor's SOCKS5 reply codes for onion services.
func torReply(code int) string {
	switch code {
	case 0xf0:
		return "onion service descriptor not found"
	case 0xf1:
		return "onion service descriptor is invalid"
	case 0xf2:
		return "onion service introduction failed"
	case 0xf3:
		return "onion service rendezvous failed"
	case 0xf4:
		return "onion service needs client authorization"
	case 0xf5:
		return "onion service client authorization is wrong"
	case 0xf6:
		return "invalid onion address"
	case 0xf7:
		return "onion service introduction timed out"
	}
	return ""
}

// onionAuth lets net/smtp's PlainAuth log in to a .onion relay that
// doesn't offer STARTTLS.  PlainAuth only sends the password over TLS or to
// localhost, but a connection to an onion service is encrypted end to end
// and the address authenticates the server.
type onionAuth struct{ smtp.Auth }

func (a onionAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	s := *server
	s.TLS = true
	return a.Auth.Start(&s)
}
