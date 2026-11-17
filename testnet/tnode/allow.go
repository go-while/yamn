package main

import (
	"fmt"
	"log"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	refusedEvery  = 10 * time.Minute // at most one refusal log line per source address
	maxRefusedLog = 256              // sources remembered for that; beyond, they are counted together
)

// allowList holds the addresses tnode accepts connections from: the nodes
// of the test network listed by IP address, loopback, and extra addresses
// or prefixes given with -allow. Everyone else is turned away, so a node on
// a public address serves only the other nodes.
//
// Admission doesn't depend on DNS: a node listed by host name in the nodes
// table is not allowed by its name; its addresses go into -allow
// (allow.txt). Link-local addresses (fe80::/10, which need a zone) aren't
// supported: nodes talk over their normal addresses.
type allowList struct {
	addrs    map[netip.Addr]bool
	prefixes []netip.Prefix
	byName   []string // nodes listed by host name, for the start-up log

	logMu    sync.Mutex
	refused  map[string]*refusal // source address -> refusals not logged yet
	overflow refusal             // refusals from sources not tracked, the map being full
}

type refusal struct {
	last  time.Time // last log line
	count int       // refusals since
}

// newAllowList allows every node listed by IP address and the extra
// addresses or prefixes. It fails on what can't be right: a node address
// without a port, an -allow value that is no address or prefix, and a
// zoned or link-local address.
func newAllowList(nodes map[string]Node, extra []string) (*allowList, error) {
	a := &allowList{
		addrs:   make(map[netip.Addr]bool),
		refused: make(map[string]*refusal),
	}
	for _, n := range nodes {
		host, _, err := net.SplitHostPort(n.Addr)
		if err != nil {
			return nil, fmt.Errorf("node %s: %w", n.Name, err)
		}
		ip, err := netip.ParseAddr(host)
		if err != nil {
			a.byName = append(a.byName, n.Name+" ("+host+")")
			continue
		}
		if ip, err = usable(ip); err != nil {
			return nil, fmt.Errorf("node %s: %w", n.Name, err)
		}
		a.addrs[ip] = true
	}
	sort.Strings(a.byName)
	for _, s := range extra {
		if p, err := netip.ParsePrefix(s); err == nil {
			if p, err = normPrefix(p); err != nil {
				return nil, fmt.Errorf("-allow %q: %w", s, err)
			}
			a.prefixes = append(a.prefixes, p)
			continue
		}
		ip, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("-allow %q: not an address or prefix", s)
		}
		if ip, err = usable(ip); err != nil {
			return nil, fmt.Errorf("-allow %q: %w", s, err)
		}
		a.addrs[ip] = true
	}
	return a, nil
}

// usable checks a configured address and returns it unmapped
// (::ffff:10.0.0.1 is 10.0.0.1), the form addresses are compared in.
func usable(ip netip.Addr) (netip.Addr, error) {
	if ip.Zone() != "" || ip.Unmap().IsLinkLocalUnicast() {
		return ip, fmt.Errorf("%s: link-local and zoned addresses aren't supported", ip)
	}
	return ip.Unmap(), nil
}

// normPrefix turns an IPv4-mapped IPv6 prefix (::ffff:10.0.0.0/104) into
// the IPv4 prefix it covers (10.0.0.0/8), since addresses are compared
// unmapped.
func normPrefix(p netip.Prefix) (netip.Prefix, error) {
	// Check the address as written: masking a mapped prefix shorter than
	// /96 would turn it into an unrelated IPv6 prefix.
	if !p.Addr().Is4In6() {
		return p.Masked(), nil
	}
	if p.Bits() < 96 {
		return netip.Prefix{}, fmt.Errorf("IPv4-mapped prefix shorter than /96")
	}
	return netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96).Masked(), nil
}

// Allows reports whether a connection from remote ("host:port", as
// net.Conn.RemoteAddr gives it) is welcome. Loopback is always allowed;
// anything that isn't an IP address, and any zoned (link-local) address,
// is not. A nil list allows loopback only.
func (a *allowList) Allows(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	if ip.IsLoopback() && ip.Zone() == "" {
		return true
	}
	if a == nil || ip.Zone() != "" {
		return false
	}
	if a.addrs[ip] {
		return true
	}
	for _, p := range a.prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// Refused logs a turned-away connection from remote, at most once per
// source address every refusedEvery, with the number of refusals since the
// last line, so strangers can't fill the log.
func (a *allowList) Refused(remote, what string) {
	src := remote
	if host, _, err := net.SplitHostPort(remote); err == nil {
		src = host
	}
	if a == nil {
		log.Printf("refused %s from %q: not a test network node", what, src)
		return
	}
	now := time.Now()
	a.logMu.Lock()
	r := a.refused[src]
	if r == nil && len(a.refused) >= maxRefusedLog {
		// Forget only sources whose interval is over; their next refusal
		// would be logged anyway.
		for k, old := range a.refused {
			if now.Sub(old.last) >= refusedEvery {
				delete(a.refused, k)
			}
		}
	}
	if r == nil && len(a.refused) >= maxRefusedLog {
		// Still full: count the untracked sources together, one line per
		// interval for all of them.
		a.overflow.count++
		if !a.overflow.last.IsZero() && now.Sub(a.overflow.last) < refusedEvery {
			a.logMu.Unlock()
			return
		}
		n := a.overflow.count
		a.overflow.last, a.overflow.count = now, 0
		a.logMu.Unlock()
		log.Printf("refused %d connections from more than %d sources that aren't test network nodes (latest: %s from %q)", n, maxRefusedLog, what, src)
		return
	}
	if r == nil {
		r = &refusal{}
		a.refused[src] = r
	}
	r.count++
	if !r.last.IsZero() && now.Sub(r.last) < refusedEvery {
		a.logMu.Unlock()
		return
	}
	first := r.last.IsZero()
	n := r.count
	r.last, r.count = now, 0
	a.logMu.Unlock()
	if first {
		log.Printf("refused %s from %q: not a test network node", what, src)
		return
	}
	log.Printf("refused %s from %q: not a test network node (%d times since the last such line)", what, src, n)
}

// String lists what is allowed, for the log.
func (a *allowList) String() string {
	if a == nil {
		return "loopback"
	}
	var s []string
	for ip := range a.addrs {
		s = append(s, ip.String())
	}
	for _, p := range a.prefixes {
		s = append(s, p.String())
	}
	sort.Strings(s)
	return "loopback " + strings.Join(s, " ")
}

// loadAllow reads the nodes table and builds the list both daemons use.
func loadAllow(nodesFile, port string, extra []string) (map[string]Node, *allowList, error) {
	nodes, err := readNodes(nodesFile, port)
	if err != nil {
		return nil, nil, err
	}
	al, err := newAllowList(nodes, extra)
	if err != nil {
		return nil, nil, err
	}
	log.Printf("accepting connections from %s", al)
	if len(al.byName) > 0 {
		log.Printf("nodes listed by host name are not allowed by name; put their addresses in -allow (allow.txt): %s", strings.Join(al.byName, ", "))
	}
	return nodes, al, nil
}

// allowListener closes connections from addresses the list doesn't allow
// as soon as they are accepted, before a byte is read or written.
type allowListener struct {
	net.Listener
	allow *allowList
	what  string
}

func (l allowListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if l.allow.Allows(c.RemoteAddr().String()) {
			return c, nil
		}
		l.allow.Refused(c.RemoteAddr().String(), l.what)
		c.Close()
	}
}

// listFlag collects a repeatable flag's values.
type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }

func (l *listFlag) Set(s string) error {
	*l = append(*l, s)
	return nil
}
