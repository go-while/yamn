package keymgr

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"
)

// PingerFiles holds what one pinger published: its pubring.mix and
// mlist2.txt.  Either may be nil if it was never downloaded.
type PingerFiles struct {
	Name    string
	Pubring []byte
	Stats   []byte
}

// MergeResult is the outcome of merging several pingers' files.
type MergeResult struct {
	// Pubring is the merged pubring.mix; nil if no pinger had a usable
	// keyring.
	Pubring []byte
	// Stats is the merged mlist2.txt; nil if no pinger had usable stats.
	Stats []byte
	// Quorum is the number of pingers that had to agree on a key.
	Quorum int
	// Warnings, Notes and Details are messages for the log, from most to
	// least important.
	Warnings []string
	Notes    []string
	Details  []string
}

func (m *MergeResult) warn(format string, a ...any) {
	m.Warnings = append(m.Warnings, fmt.Sprintf(format, a...))
}

func (m *MergeResult) note(format string, a ...any) {
	m.Notes = append(m.Notes, fmt.Sprintf(format, a...))
}

func (m *MergeResult) detail(format string, a ...any) {
	m.Details = append(m.Details, fmt.Sprintf(format, a...))
}

// keyVote collects the pingers that publish the same key for a remailer.
type keyVote struct {
	rem     Remailer // the key, with the header fields most pingers agree on
	pingers []string
	headers map[string][]string // header line -> pingers publishing it
}

// pingerStats is one pinger's parsed mlist2.txt.
type pingerStats struct {
	name      string
	generated time.Time
	rows      map[string]statsRow
	fresh     bool // at most stale_hours old
}

// MergePingers cross-checks the keyrings and stats of several pingers and
// returns a merged pubring.mix and mlist2.txt in the usual formats.
//
// A key is only used if at least quorum pingers publish it (the same
// address, name, keyid and public key).  If fewer pingers have a usable
// keyring, all of them must agree.  So a single pinger that is wrong,
// stale or compromised can neither add a key nor replace one.
//
// A remailer's stats must come from at least as many pingers as its key:
// those whose mlist2.txt is no more than staleHours old, and older ones if
// that's too few.  Stats dated in the future aren't used.  Each remailer
// gets the median latency and uptime, and the merged file carries the
// oldest Generated time used.
//
// Keys are checked for validity at now; useExpired keeps expired ones, as
// for ImportPubring.
func MergePingers(pingers []PingerFiles, quorum, staleHours int, useExpired bool, now time.Time) (res MergeResult) {
	if quorum < 1 {
		quorum = 1
	}
	stats := mergeStatsSources(pingers, staleHours, now, &res)
	keys := mergeKeys(pingers, quorum, useExpired, now, stats, &res)
	if keys == nil {
		return
	}
	res.Pubring = writePubring(keys)
	if len(stats) > 0 {
		res.Stats = writeStats(keys, stats, res.Quorum, now, &res)
	}
	return
}

// maxClockSkew is how far in the future a pinger's Generated time may be.
const maxClockSkew = time.Hour

// mergeStatsSources parses the pingers' stats and returns the usable ones,
// newest first, marking those at most staleHours old as fresh.
func mergeStatsSources(pingers []PingerFiles, staleHours int, now time.Time, res *MergeResult) []pingerStats {
	var all []pingerStats
	for _, p := range pingers {
		if p.Stats == nil {
			continue
		}
		if err := CheckStats(p.Stats); err != nil {
			res.warn("%s: stats unusable: %s", p.Name, err)
			continue
		}
		generated, rows, err := readStats(bytes.NewReader(p.Stats), p.Name, func(format string, a ...any) {
			res.detail(p.Name+": stats: "+format, a...)
		})
		if err != nil {
			res.warn("%s: stats unusable: %s", p.Name, err)
			continue
		}
		// Future stats would count as fresh for as long as they claim.
		if generated.After(now.Add(maxClockSkew)) {
			res.warn("%s: stats are dated in the future (%s); not used", p.Name, generated.UTC().Format(generatedFormat))
			continue
		}
		ps := pingerStats{name: p.Name, generated: generated, rows: make(map[string]statsRow)}
		ps.fresh = now.Sub(generated) <= time.Duration(staleHours)*time.Hour
		for _, row := range rows {
			ps.rows[row.name] = row
		}
		all = append(all, ps)
	}
	// Newest first, so ties and options prefer the freshest pinger.
	slices.SortStableFunc(all, func(a, b pingerStats) int {
		return b.generated.Compare(a.generated)
	})
	if len(all) == 0 {
		res.warn("no pinger has usable stats")
	} else if !all[0].fresh {
		res.warn("all pingers' stats are older than %d hours; using them anyway", staleHours)
	}
	return all
}

// mergeKeys returns the keys that reach the quorum, one per remailer, or
// nil if no pinger has a usable keyring.
func mergeKeys(pingers []PingerFiles, quorum int, useExpired bool, now time.Time, stats []pingerStats, res *MergeResult) []Remailer {
	// votes[address][key] collects the pingers publishing each key.
	votes := make(map[string]map[string]*keyVote)
	usable := 0
	for _, p := range pingers {
		if p.Pubring == nil {
			continue
		}
		if err := CheckPubring(p.Pubring); err != nil {
			res.warn("%s: keyring unusable: %s", p.Name, err)
			continue
		}
		rems, err := readPubring(bytes.NewReader(p.Pubring), now, useExpired, func(format string, a ...any) {
			res.detail(p.Name+": keyring: "+format, a...)
		})
		if err != nil {
			res.warn("%s: keyring unusable: %s", p.Name, err)
			continue
		}
		usable++
		// A pinger votes once per remailer: if its keyring lists an
		// address twice, the last entry counts, as in ImportPubring.
		mine := make(map[string]Remailer)
		for _, rem := range rems {
			mine[rem.Address] = rem
		}
		for addr, rem := range mine {
			id := rem.name + " " + hex.EncodeToString(rem.Keyid) + " " + hex.EncodeToString(rem.PK)
			if votes[addr] == nil {
				votes[addr] = make(map[string]*keyVote)
			}
			v := votes[addr][id]
			if v == nil {
				v = &keyVote{rem: rem, headers: make(map[string][]string)}
				votes[addr][id] = v
			}
			v.pingers = append(v.pingers, p.Name)
			h := rem.header()
			v.headers[h] = append(v.headers[h], p.Name)
		}
	}
	if usable == 0 {
		res.warn("no pinger has a usable keyring; keeping the current one")
		return nil
	}
	res.Quorum = min(quorum, usable)
	if res.Quorum < quorum {
		res.warn(
			"only %d pinger(s) have a usable keyring; accepting keys published by %d instead of %d",
			usable, res.Quorum, quorum,
		)
	}

	// Pick one key per address.
	byName := make(map[string][]*keyVote)
	for _, addr := range sortedKeys(votes) {
		candidates := votes[addr]
		var accepted []*keyVote
		var summary []string
		for _, id := range sortedKeys(candidates) {
			v := candidates[id]
			v.rem = pickHeader(v, stats)
			summary = append(summary, fmt.Sprintf(
				"%s %x (%s)", v.rem.name, v.rem.Keyid[:4], strings.Join(v.pingers, ", "),
			))
			if len(v.pingers) >= res.Quorum {
				accepted = append(accepted, v)
			}
		}
		if len(candidates) > 1 {
			res.note("%s: pingers disagree: %s", addr, strings.Join(summary, "; "))
		}
		if len(accepted) == 0 {
			// Normal for a new remailer or a fresh key that not
			// enough pingers have yet, so not a warning.
			res.note("%s: no key is published by %d pingers: %s", addr, res.Quorum, strings.Join(summary, "; "))
			continue
		}
		// Several accepted keys mean a key rotation: prefer the newest.
		slices.SortFunc(accepted, func(a, b *keyVote) int {
			if c := b.rem.from.Compare(a.rem.from); c != 0 {
				return c
			}
			if c := b.rem.until.Compare(a.rem.until); c != 0 {
				return c
			}
			return bytes.Compare(a.rem.Keyid, b.rem.Keyid)
		})
		chosen := accepted[0]
		byName[chosen.rem.name] = append(byName[chosen.rem.name], chosen)
	}

	// A name must stand for one address: chains and stats refer to
	// remailers by name.
	var keys []Remailer
	for _, name := range sortedKeys(byName) {
		vs := byName[name]
		if len(vs) > 1 {
			slices.SortStableFunc(vs, func(a, b *keyVote) int {
				return len(b.pingers) - len(a.pingers)
			})
			var addrs []string
			for _, v := range vs {
				addrs = append(addrs, fmt.Sprintf("%s (%d pingers)", v.rem.Address, len(v.pingers)))
			}
			if len(vs[0].pingers) == len(vs[1].pingers) {
				res.warn("name %s is used by several addresses: %s; ignoring all of them",
					name, strings.Join(addrs, ", "))
				continue
			}
			res.warn("name %s is used by several addresses: %s; using %s",
				name, strings.Join(addrs, ", "), vs[0].rem.Address)
		}
		keys = append(keys, vs[0].rem)
	}
	for _, p := range pingers {
		res.detail("%s: %d of its keys are used", p.Name, countUsed(keys, votes, p.Name))
	}
	return keys
}

// pickHeader returns the vote's key with the header fields (version,
// capabilities, dates) published by most of its pingers.  On a tie, the
// pinger with the newest stats wins, then the first pinger listed.
func pickHeader(v *keyVote, stats []pingerStats) Remailer {
	rank := func(pinger string) int {
		for i, ps := range stats {
			if ps.name == pinger {
				return i
			}
		}
		return len(stats)
	}
	best, bestCount, bestRank := "", 0, 0
	for _, h := range sortedKeys(v.headers) {
		ps := v.headers[h]
		r := len(stats) + 1
		for _, p := range ps {
			r = min(r, rank(p))
		}
		if len(ps) > bestCount || (len(ps) == bestCount && r < bestRank) {
			best, bestCount, bestRank = h, len(ps), r
		}
	}
	rem := v.rem
	f := strings.Split(best, " ")
	rem.version, rem.caps = f[3], f[4]
	rem.from, _ = time.Parse(date_format, f[5])
	rem.until, _ = time.Parse(date_format, f[6])
	return rem
}

// countUsed returns how many of the chosen keys pinger published.
func countUsed(keys []Remailer, votes map[string]map[string]*keyVote, pinger string) (n int) {
	for _, k := range keys {
		for _, v := range votes[k.Address] {
			if bytes.Equal(v.rem.Keyid, k.Keyid) && slices.Contains(v.pingers, pinger) {
				n++
			}
		}
	}
	return
}

// header returns the remailer's pubring.mix header line.
func (r Remailer) header() string {
	return strings.Join([]string{
		r.name,
		r.Address,
		hex.EncodeToString(r.Keyid),
		r.version,
		r.caps,
		r.from.UTC().Format(date_format),
		r.until.UTC().Format(date_format),
	}, " ")
}

// writePubring formats keys as a pubring.mix file, sorted by header line
// as Echolot does.
func writePubring(keys []Remailer) []byte {
	sorted := slices.Clone(keys)
	slices.SortFunc(sorted, func(a, b Remailer) int {
		return strings.Compare(a.header(), b.header())
	})
	var b bytes.Buffer
	for _, k := range sorted {
		keyid := hex.EncodeToString(k.Keyid)
		fmt.Fprintf(&b, "%s\n\n", k.header())
		fmt.Fprintln(&b, "-----Begin Mix Key-----")
		fmt.Fprintln(&b, keyid)
		fmt.Fprintln(&b, hex.EncodeToString(k.PK))
		fmt.Fprint(&b, "-----End Mix Key-----\n\n")
	}
	return b.Bytes()
}

// writeStats formats the merged stats of keys as an mlist2.txt file.
// stats must be sorted newest first.
//
// A remailer gets stats only from at least quorum pingers, so fewer can't
// decide them.  All fresh pingers are used; if they're not enough, older
// ones fill up to the quorum, newest first.
func writeStats(keys []Remailer, stats []pingerStats, quorum int, now time.Time, res *MergeResult) []byte {
	var rows []statsRow
	used := make(map[string]bool) // pingers whose stats were used
	for _, k := range keys {
		var lat, up []int
		var from []string
		options := ""
		for _, ps := range stats {
			row, ok := ps.rows[k.name]
			if !ok || (!ps.fresh && len(lat) >= quorum) {
				continue
			}
			if len(lat) == 0 {
				options = row.options
			}
			lat = append(lat, row.latent)
			up = append(up, row.uptime)
			from = append(from, ps.name)
		}
		if len(lat) < quorum {
			res.note("%s: stats from %d pingers, %d needed; not used", k.name, len(lat), quorum)
			continue
		}
		for _, p := range from {
			used[p] = true
		}
		rows = append(rows, statsRow{
			name:    k.name,
			latent:  median(lat, true),
			uptime:  median(up, false),
			options: options,
		})
	}
	var usedStats []pingerStats
	for _, ps := range stats {
		age := now.Sub(ps.generated).Round(time.Minute)
		switch {
		case !used[ps.name] && ps.fresh:
			res.note("%s: stats not used", ps.name)
			continue
		case !used[ps.name]:
			res.note("%s: stats are %s old, not used", ps.name, age)
			continue
		case !ps.fresh:
			res.note("%s: stats are %s old, used where fresher ones are missing", ps.name, age)
		}
		usedStats = append(usedStats, ps)
	}
	if len(usedStats) == 0 {
		res.warn("no remailer has stats from %d pingers", quorum)
		return nil
	}
	stats = usedStats
	// Most reliable first, then by name, as Echolot sorts.
	slices.SortFunc(rows, func(a, b statsRow) int {
		if a.uptime != b.uptime {
			return b.uptime - a.uptime
		}
		return strings.Compare(a.name, b.name)
	})
	// The merged stats are only as fresh as the oldest source used.
	generated := stats[0].generated
	for _, ps := range stats {
		if ps.generated.Before(generated) {
			generated = ps.generated
		}
	}
	const unknownHist = "????????????"
	var b bytes.Buffer
	fmt.Fprintln(&b, "Stats-Version: 2.0")
	fmt.Fprintf(&b, "Generated: %s\n", generated.UTC().Format(generatedFormat))
	fmt.Fprintln(&b, "Mixmaster    Latent-Hist   Latent  Uptime-Hist   Uptime  Options")
	fmt.Fprintln(&b, strings.Repeat("-", 72))
	for _, r := range rows {
		latency := fmt.Sprintf("  :%02d", r.latent%60)
		if h := r.latent / 60; h > 0 {
			latency = fmt.Sprintf("%2d:%02d", h, r.latent%60)
		}
		line := fmt.Sprintf("%-12s %-12s %6s   %-12s  %5.1f%%  %s",
			r.name, unknownHist, latency, unknownHist, float64(r.uptime)/10, r.options)
		fmt.Fprintln(&b, strings.TrimRight(line, " "))
	}
	// An empty line ends the table.
	fmt.Fprintln(&b)
	return b.Bytes()
}

// median returns the median of v.  With an even count it's the mean of the
// two middle values, rounded towards the worse one: up for a latency
// (higherWorse), down for an uptime.
func median(v []int, higherWorse bool) int {
	slices.Sort(v)
	n := len(v)
	if n%2 == 1 {
		return v[n/2]
	}
	sum := v[n/2-1] + v[n/2]
	if higherWorse {
		return (sum + 1) / 2
	}
	return sum / 2
}

// sortedKeys returns the keys of m in sorted order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
