package keymgr

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/dchest/blake2s"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	date_format     string = "2006-01-02"
	generatedFormat string = "Mon 02 Jan 2006 15:04:05 GMT"
)

type Remailer struct {
	name    string    // Remailer Shortname
	Address string    // Remailer Address
	Keyid   []byte    // 16 Byte Mixmaster KeyID
	version string    // Mixmaster version
	caps    string    // Remailer capstring
	PK      []byte    // Curve25519 Public Key
	from    time.Time // Valid-from date
	until   time.Time // Valid until date
	latent  int       // Latency (minutes)
	uptime  int       // Uptime (10ths of a %)
}

type Pubring struct {
	pubringFile    string // Pubring filename
	statsFile      string // mlist type file
	useExpired     bool   // Consider exired keys (for Echolot)
	pub            map[string]Remailer
	xref           map[string]string // A cross-reference of shortnames to addresses
	stats          bool              // Have current reliability stats been imported?
	advertised     string            // The keyid a local server is currently advertising
	keysImported   time.Time         // Timestamp on most recently read pubring.mix file
	statsImported  time.Time         // Timestamp on most recently read mlist2.txt file
	statsGenerated time.Time         // Generated timestamp on mlist2.txt file
}

func NewPubring(pubfile, statfile string) *Pubring {
	return &Pubring{
		pubringFile: pubfile,
		statsFile:   statfile,
		useExpired:  false,
		pub:         make(map[string]Remailer),
		xref:        make(map[string]string),
		stats:       false,
	}
}

// StatsStale returns true if stats are over h hours old
func (p *Pubring) StatsStale(h int) bool {
	return p.stats && int((time.Since(p.statsGenerated).Hours())) > h
}

func (p *Pubring) HaveStats() bool {
	return p.stats
}

func (p *Pubring) UseExpired() {
	p.useExpired = true
}

// KeyRefresh returns True if the Pubring file has been modified
func (p *Pubring) KeyRefresh() bool {
	stat, err := os.Stat(p.pubringFile)
	if err != nil {
		panic(err)
	}
	return stat.ModTime().After(p.keysImported)
}

// StatRefresh returns True if the mlist2.txt file has been modified
func (p *Pubring) StatRefresh() (refresh bool) {
	stat, err := os.Stat(p.statsFile)
	if err != nil {
		// If there's no stats file, it's not time to refresh it
		return
	}
	refresh = stat.ModTime().After(p.statsImported)
	return
}

// Candidates provides a list of remailer addresses that match the specified criteria
func (p Pubring) Candidates(minlat, maxlat int, minrel float32, exit bool) (c []string) {
	for addy := range p.pub {
		stats := p.pub[addy]
		if exit {
			if strings.Contains(stats.caps, "M") {
				// Exits are required and this is a Middle
				continue
			}
		}
		if stats.latent < minlat || stats.latent > maxlat {
			continue
		}
		if stats.uptime < int(minrel*10) {
			continue
		}
		c = append(c, addy)
	}
	return
}

// Count returns the number of known Public keys
func (p Pubring) Count() int {
	return len(p.pub)
}

// Produces a list of public key headers
func (p Pubring) KeyList() (addresses []string) {
	for addy := range p.pub {
		key := p.pub[addy]
		header := key.name + " "
		header += key.Address + " "
		header += hex.EncodeToString(key.Keyid) + " "
		header += key.version + " "
		header += key.caps + " "
		header += key.from.UTC().Format(date_format) + " "
		header += key.until.UTC().Format(date_format)
		addresses = append(addresses, header)
	}
	return
}

// Put inserts a new remailer struct into the Keyring
func (p Pubring) Put(r Remailer) {
	p.pub[r.Address] = r
	p.xref[r.name] = r.Address
}

// Get returns a remailer's public info when requested by name or address
func (p Pubring) Get(ref string) (r Remailer, err error) {
	var exists bool
	if strings.Contains(ref, "@") {
		r, exists = p.pub[ref]
		if !exists {
			err = fmt.Errorf(
				"%s: Remailer address not found in public "+
					"keyring",
				ref,
			)
			return
		}
	} else {
		var addy string
		addy, exists = p.xref[ref]
		if !exists {
			err = fmt.Errorf(
				"%s: Remailer name not found in public "+
					"keyring",
				ref,
			)
			return
		}
		r = p.pub[addy]
	}
	return
}

// statsRow is one remailer's line in an mlist2.txt file.
type statsRow struct {
	name    string
	latent  int    // Latency (minutes)
	uptime  int    // Uptime (10ths of a %)
	options string // Options column, as published
}

// warnFunc reports a problem with a single entry of a keyring or stats
// file.  The entry is skipped and reading continues.
type warnFunc func(format string, a ...any)

// stderrWarn prints warnings to stderr, as the Import functions always have.
func stderrWarn(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
}

// ImportStats reads an mlist2.txt style file into a Pubring struct
func (p *Pubring) ImportStats() (err error) {
	f, err := os.Open(p.statsFile)
	if err != nil {
		return
	}
	defer f.Close()
	generated, rows, err := readStats(f, p.statsFile, stderrWarn)
	if err != nil {
		return
	}
	p.statsGenerated = generated
	for _, row := range rows {
		remAddr, exists := p.xref[row.name]
		if !exists {
			fmt.Fprintf(os.Stderr, "%s: Stats for unknown remailer\n", row.name)
			continue
		}
		tmp := p.pub[remAddr]
		tmp.latent = row.latent
		tmp.uptime = row.uptime
		p.pub[remAddr] = tmp
	}
	// Update last-imported timestamp for stats
	stat, err := os.Stat(p.statsFile)
	if err != nil {
		panic(err)
	}
	p.statsImported = stat.ModTime()
	p.stats = true
	return
}

// readStats parses an mlist2.txt style file: its Generated timestamp and
// the rows of the stats table.  Malformed rows are reported through warn
// and skipped.  name identifies the file in errors.
func readStats(r io.Reader, name string, warn warnFunc) (generated time.Time, rows []statsRow, err error) {
	scanner := bufio.NewScanner(r)
	parsePhase := 0
	/* Stat phases are:
	0 Want Generated timestamp
	1 Expecting long string of dashes
	2 Expecting stats lines
	3 Done (the empty line after the table)
	*/
	for parsePhase < 3 && scanner.Scan() {
		line := scanner.Text()
		switch parsePhase {
		case 0:
			// Generated: Tue 07 Oct 2014 10:50:01 GMT
			if stamp, ok := strings.CutPrefix(line, "Generated: "); ok {
				generated, err = time.Parse(generatedFormat, stamp)
				if err != nil {
					err = fmt.Errorf("Failed to parse Generated date: %s", err)
					return
				}
				parsePhase++
			}
		case 1:
			// Expecting dashes
			if strings.HasPrefix(line, "----------") {
				parsePhase++
			}
		case 2:
			// Expecting stats.  Splitting the line at the % sign takes
			// out the risk of Options being interpreted as an
			// unpredictable number of fields.
			fields, options, _ := strings.Cut(line, "%")
			elements := strings.Fields(fields)
			if len(elements) == 0 {
				// An empty line implies we've read all stats entries
				parsePhase++
				continue
			}
			if len(elements) != 5 {
				warn("Invalid stats line.  Expected 7 elements, got %d", len(elements))
				continue
			}
			row, ok := parseStatsRow(elements, warn)
			if !ok {
				continue
			}
			row.options = strings.TrimSpace(options)
			rows = append(rows, row)
		}
	}
	if err = scanner.Err(); err != nil {
		return
	}
	// Test that all stats phases have been achieved.
	if parsePhase < 3 {
		err = fmt.Errorf(
			"Unexpected EOF reading %s during phase %d",
			name,
			parsePhase,
		)
	}
	return
}

// parseStatsRow reads the name, latency and uptime of a stats line, split
// into its fields: name, latency history, latency, uptime history, uptime.
func parseStatsRow(elements []string, warn warnFunc) (row statsRow, ok bool) {
	row.name = elements[0]
	// Element 2 is Latency in the format (hrs:mins)
	lat := strings.Split(elements[2], ":")
	if len(lat) != 2 {
		warn("%s: Invalid latency", row.name)
		return
	}
	lathrs := 0
	if lat[0] != "" {
		var err error
		lathrs, err = strconv.Atoi(lat[0])
		if err != nil {
			warn("%s: Invalid latent hours", row.name)
			return
		}
		if lathrs < 0 || lathrs > 99 {
			warn("%s: Latent hours out of range", row.name)
			return
		}
	}
	latmin, err := strconv.Atoi(lat[1])
	if err != nil {
		warn("%s: Invalid latent minutes", row.name)
		return
	}
	if latmin < 0 || latmin > 59 {
		warn("%s: Latent minutes out of range", row.name)
		return
	}
	// Element 4 is Uptime in format (xxx.xx)
	uptmp, err := strconv.ParseFloat(elements[4], 32)
	if err != nil {
		warn("%s: Invalid uptime", row.name)
		return
	}
	if uptmp < 0 || uptmp > 100 {
		warn("%s: Uptime out of range", row.name)
		return
	}
	row.latent = (lathrs * 60) + latmin
	row.uptime = int(uptmp * 10)
	return row, true
}

func Headers(filename string) (headers []string, err error) {
	var f *os.File
	f, err = os.Open(filename)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if len(strings.Split(line, " ")) == 7 {
			headers = append(headers, line)
		}
	}
	return
}

// ImportPubring reads a YAMN Pubring.mix file
func (p *Pubring) ImportPubring() (err error) {
	var f *os.File
	f, err = os.Open(p.pubringFile)
	if err != nil {
		return
	}
	defer f.Close()
	rems, err := readPubring(f, time.Now(), p.useExpired, stderrWarn)
	if err != nil {
		return
	}
	// MergePingers decides ambiguous names and addresses by the pingers'
	// votes; within one keyring there's nothing to decide by.
	rems = dropAmbiguous(rems, stderrWarn)
	for _, rem := range rems {
		p.Put(rem)
	}
	// Set key imported timestamp
	stat, err := os.Stat(p.pubringFile)
	if err != nil {
		panic(err)
	}
	p.keysImported = stat.ModTime()
	return
}

// readPubring parses a pubring.mix file and returns its keys that are
// valid at now (and expired ones too if useExpired is set).  Malformed or
// invalid keys are reported through warn and skipped.
func readPubring(r io.Reader, now time.Time, useExpired bool, warn warnFunc) (rems []Remailer, err error) {
	scanner := bufio.NewScanner(r)
	var rem *Remailer
	key_phase := 0
	/* Key phases are:
	0	Expecting header line
	1	Expecting Begin cutmark
	2	Expecting Keyid line
	3	Expecting public key
	4	Got End cutmark
	*/

	for scanner.Scan() {
		line := scanner.Text()
		switch key_phase {
		case 0:
			// Expecting key header line
			elements := strings.Split(line, " ")
			// 7 elements indicates a remailer header line in Pubring.mix
			if len(elements) != 7 {
				continue
			}
			if !ValidName(elements[0]) {
				warn("%q: Invalid remailer name. Ignoring key.", elements[0])
				continue
			}
			if !ValidAddress(elements[1]) {
				warn("%s: Invalid remailer address %q. Ignoring key.", elements[0], elements[1])
				continue
			}
			from, err := time.Parse(date_format, elements[5])
			if err != nil {
				warn("Malformed valid-from date")
				continue
			}
			fromLastMidnight, _ := midnights(from)
			if now.Before(fromLastMidnight) {
				warn("%s: Key not yet valid", elements[0])
				continue
			}
			until, err := time.Parse(date_format, elements[6])
			if err != nil {
				warn("Malformed valid-to date")
				continue
			}
			// Calculate the next midnight after the until date.
			_, untilNextMidnight := midnights(until)
			if !useExpired && now.After(untilNextMidnight) {
				warn(
					"Key expired: Name=%s, Key=%s, Date=%s",
					elements[0],
					elements[2],
					elements[6],
				)
				continue
			}
			rem = new(Remailer)
			rem.name = elements[0]
			rem.Keyid, err = hex.DecodeString(elements[2])
			if err != nil {
				// keyid is not valid hex
				warn("Keyid in header is not hex")
				continue
			}
			rem.from = from
			rem.until = until
			rem.version = elements[3]
			rem.caps = elements[4]
			rem.Address = elements[1]
			key_phase = 1
		case 1:
			// Expecting Begin cutmark
			if line == "-----Begin Mix Key-----" {
				key_phase = 2
			}
		case 2:
			// Expecting Keyid line
			keyid, err := hex.DecodeString(line)
			if err != nil {
				// keyid is not valid hex
				warn("Keyid in pubkey is not hex")
				key_phase = 0
				continue
			}
			if !bytes.Equal(keyid, rem.Keyid) {
				// Corrupt keyblock - header keyid doesn't match keyid in block
				warn("Keyid in header differs from keyid in pubkey")
				key_phase = 0
				continue
			}
			key_phase = 3
		case 3:
			// Expecting Public key
			pkdata, err := hex.DecodeString(line)
			if err != nil {
				// Public key is not valid hex
				warn("Unable to decode Public key")
				key_phase = 0
				continue
			}
			if len(pkdata) != 32 {
				warn("Public key is not 32 bits")
				key_phase = 0
				continue
			}
			// The keyid is defined as the hash of the public key.  A
			// mismatch means the key block was altered or corrupted.
			if !bytes.Equal(makeKeyID(pkdata), rem.Keyid) {
				warn("%s: Keyid does not match public key. Ignoring key.", rem.name)
				key_phase = 0
				continue
			}
			rem.PK = pkdata
			key_phase = 4
		case 4:
			// Expecting end cutmark
			if line == "-----End Mix Key-----" {
				rems = append(rems, *rem)
				key_phase = 0
			}
		} // End of phases
	} // End of file scan loop
	err = scanner.Err()
	return
}

// ValidName reports whether name can be a remailer name: lowercase letters
// and digits.  Chains, stats and Get refer to remailers by name, so it
// mustn't contain "@", "," or "%", nor be "*".
func ValidName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// ValidAddress reports whether addr can be a remailer address: one
// local@domain of at most 52 bytes (the size of the next-hop field), in
// printable ASCII without spaces or the special characters of mail
// headers.
func ValidAddress(addr string) bool {
	if len(addr) > maxAddyLen {
		return false
	}
	local, domain, ok := strings.Cut(addr, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return false
	}
	for i := 0; i < len(addr); i++ {
		c := addr[i]
		if c <= ' ' || c >= 0x7f || strings.IndexByte(`"(),:;<>[\]`, c) >= 0 {
			return false
		}
	}
	return true
}

// dropAmbiguous removes the keys whose name is used by several addresses,
// or whose address is used by several names: there's no telling which one
// is meant.  Several keys for one name and address (a key rotation) are
// fine.
func dropAmbiguous(rems []Remailer, warn warnFunc) []Remailer {
	addrs := make(map[string]map[string]bool) // name -> addresses
	names := make(map[string]map[string]bool) // address -> names
	for _, r := range rems {
		if addrs[r.name] == nil {
			addrs[r.name] = make(map[string]bool)
		}
		addrs[r.name][r.Address] = true
		if names[r.Address] == nil {
			names[r.Address] = make(map[string]bool)
		}
		names[r.Address][r.name] = true
	}
	var keep []Remailer
	for _, r := range rems {
		switch {
		case len(addrs[r.name]) > 1:
			warn("%s: Name is used by several addresses. Ignoring %s.", r.name, r.Address)
		case len(names[r.Address]) > 1:
			warn("%s: Address is used by several names. Ignoring %s.", r.Address, r.name)
		default:
			keep = append(keep, r)
		}
	}
	return keep
}

// makeKeyID generates a 16 Byte KeyID based on the hash of a Public Key
func makeKeyID(pub []byte) []byte {
	digest, err := blake2s.New(&blake2s.Config{Size: 16})
	if err != nil {
		panic(err)
	}
	digest.Write(pub)
	return digest.Sum(nil)
}

// midnights returns the timestamp for the previous and next midnights of a
// given timestamp
func midnights(t time.Time) (lastMidnight, nextMidnight time.Time) {
	lastMidnight = time.Date(t.Year(), t.Month(), t.Day(),
		0, 0, 0, 0, time.UTC)
	nextMidnight = time.Date(t.Year(), t.Month(), t.Day()+1,
		0, 0, 0, 0, time.UTC)
	return
}
