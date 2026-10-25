// Command tnode runs the mail and web side of the yamn test network: the
// machines in testnet/nodes.txt, each with the domain <name>.yamn.test.
//
//	tnode mx  -nodes nodes.txt -self 11101 -deliver mix=/path/Maildir ...
//	tnode www -serve /=/path/www -mlist2 /path/www ...
//	tnode mlist2 /path/www
//
// mx is an SMTP relay for the test network only.  Mail for its own domain
// goes into the Maildir of the local part (before any "+"); mail for
// another node's domain is forwarded to that node's mx; any other
// recipient is refused, so nothing can leave the test network.
//
// www serves directories over http, and can write an mlist2.txt that lists
// every remailer in a pubring.mix as perfect, for the network's first
// keyring before pingers measure it; mlist2 writes it once.
//
// It needs only Go's standard library.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"
)

// Domain is the parent domain of every node.
const Domain = "yamn.test"

// Node is one machine of the test network.
type Node struct {
	Name  string
	Addr  string // host:port of its mx
	Roles []string
}

// Domain returns the node's mail domain.
func (n Node) Domain() string { return n.Name + "." + Domain }

// readNodes reads a nodes table: lines of name, ip (or ip:port) and roles,
// with # comments.  port is used where an ip has none.
func readNodes(file, port string) (map[string]Node, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	nodes := make(map[string]Node)
	s := bufio.NewScanner(f)
	for line := 1; s.Scan(); line++ {
		text, _, _ := strings.Cut(s.Text(), "#")
		fields := strings.Fields(text)
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 3 {
			return nil, fmt.Errorf("%s:%d: want name, ip and roles", file, line)
		}
		n := Node{Name: fields[0], Addr: fields[1], Roles: strings.Split(fields[2], ",")}
		if _, _, err := net.SplitHostPort(n.Addr); err != nil {
			n.Addr = net.JoinHostPort(n.Addr, port)
		}
		if _, dup := nodes[n.Name]; dup {
			return nil, fmt.Errorf("%s:%d: node %s twice", file, line, n.Name)
		}
		nodes[n.Name] = n
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("%s: no nodes", file)
	}
	return nodes, nil
}

// mapFlag collects repeated key=value flags.
type mapFlag map[string]string

func (m mapFlag) String() string { return fmt.Sprint(map[string]string(m)) }

func (m mapFlag) Set(s string) error {
	k, v, ok := strings.Cut(s, "=")
	if !ok || k == "" || v == "" {
		return fmt.Errorf("want key=value, not %q", s)
	}
	m[k] = v
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: tnode mx|www [flags] (-h for the flags), or tnode mlist2 DIR")
	os.Exit(2)
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "mx":
		mxMain(os.Args[2:])
	case "www":
		wwwMain(os.Args[2:])
	case "mlist2":
		if len(os.Args) != 3 {
			usage()
		}
		if err := writeMlist2(os.Args[2], time.Now()); err != nil {
			log.Fatal(err)
		}
	default:
		usage()
	}
}

func mxMain(args []string) {
	fs := flag.NewFlagSet("mx", flag.ExitOnError)
	nodesFile := fs.String("nodes", "nodes.txt", "nodes table")
	self := fs.String("self", "", "this node's name")
	listen := fs.String("listen", ":2525", "address to listen on")
	port := fs.String("port", "2525", "mx port of nodes listed without one")
	spool := fs.String("spool", "spool", "directory for mail waiting to be forwarded")
	deliver := mapFlag{}
	fs.Var(deliver, "deliver", "local part=Maildir, repeatable")
	fs.Parse(args)
	nodes, err := readNodes(*nodesFile, *port)
	if err != nil {
		log.Fatal(err)
	}
	me, ok := nodes[*self]
	if !ok {
		log.Fatalf("-self %q isn't in %s", *self, *nodesFile)
	}
	mx := &MX{Self: me, Nodes: nodes, Deliver: deliver, Spool: *spool}
	if err := mx.Init(); err != nil {
		log.Fatal(err)
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("mx %s listening on %s", me.Domain(), ln.Addr())
	go mx.ForwardLoop(nil)
	log.Fatal(mx.Serve(ln))
}
