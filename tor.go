package main

import (
	"crypto/rand"
	"crypto/sha3"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
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
// proxyAddr (RFC 1928).  The host name goes to the proxy as it is, so Tor
// resolves it, not the local resolver.  Every call logs in with a new
// random user name, so Tor (IsolateSOCKSAuth, on by default) builds it its
// own circuit, and deliveries of different messages don't share one.
func torDial(proxyAddr, address string, timeout time.Duration) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("bad port in %s", address)
	}
	if len(host) == 0 || len(host) > 255 {
		return nil, fmt.Errorf("bad host name in %s", address)
	}
	conn, err := net.DialTimeout("tcp", proxyAddr, timeout)
	if err != nil {
		return nil, fmt.Errorf("tor_socks %s: %w", proxyAddr, err)
	}
	if err = conn.SetDeadline(time.Now().Add(timeout)); err == nil {
		err = socks5Connect(conn, host, uint16(port))
	}
	if err == nil {
		err = conn.SetDeadline(time.Time{})
	}
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("tor_socks %s to %s: %w", proxyAddr, address, err)
	}
	return conn, nil
}

// socks5Connect does the SOCKS5 handshake on conn and asks for a
// connection to host:port.
func socks5Connect(conn net.Conn, host string, port uint16) error {
	// Offer no authentication and username/password.  Tor picks the
	// latter when offered.
	if _, err := conn.Write([]byte{5, 2, 0, 2}); err != nil {
		return err
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return err
	}
	if reply[0] != 5 {
		return fmt.Errorf("not a SOCKS5 proxy (version %d)", reply[0])
	}
	switch reply[1] {
	case 0:
	case 2:
		if err := socks5Login(conn); err != nil {
			return err
		}
	default:
		return errors.New("the proxy accepts none of the offered login methods")
	}
	req := []byte{5, 1, 0, 3, byte(len(host))} // CONNECT to a domain name
	req = append(req, host...)
	req = binary.BigEndian.AppendUint16(req, port)
	if _, err := conn.Write(req); err != nil {
		return err
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}
	if head[0] != 5 {
		return fmt.Errorf("bad SOCKS5 reply version %d", head[0])
	}
	if head[1] != 0 {
		return fmt.Errorf("SOCKS5 error 0x%02x: %s", head[1], socks5Reply(head[1]))
	}
	// Skip the bound address and port.
	var skip int
	switch head[3] {
	case 1:
		skip = 4 + 2
	case 4:
		skip = 16 + 2
	case 3:
		n := make([]byte, 1)
		if _, err := io.ReadFull(conn, n); err != nil {
			return err
		}
		skip = int(n[0]) + 2
	default:
		return fmt.Errorf("bad SOCKS5 address type %d", head[3])
	}
	_, err := io.ReadFull(conn, make([]byte, skip))
	return err
}

// socks5Login sends a random user name and password (RFC 1929).  They
// only select the circuit.
func socks5Login(conn net.Conn) error {
	user := make([]byte, 16)
	if _, err := rand.Read(user); err != nil {
		return err
	}
	name := hex.EncodeToString(user)
	req := []byte{1, byte(len(name))}
	req = append(req, name...)
	req = append(req, 1, 'x')
	if _, err := conn.Write(req); err != nil {
		return err
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return err
	}
	if reply[1] != 0 {
		return errors.New("the proxy refused the login")
	}
	return nil
}

// socks5Reply describes a SOCKS5 reply code, with Tor's codes for onion
// services (sent with the SocksPort flag ExtendedErrors).
func socks5Reply(code byte) string {
	switch code {
	case 1:
		return "general failure"
	case 2:
		return "not allowed by the proxy"
	case 3:
		return "network unreachable"
	case 4:
		return "host unreachable"
	case 5:
		return "connection refused"
	case 6:
		return "TTL expired"
	case 7:
		return "command not supported"
	case 8:
		return "address type not supported"
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
	return "unknown error"
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
