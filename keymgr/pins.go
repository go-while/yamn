package keymgr

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Key pinning (Y-03).  Pingers collect remailer keys by unsigned mail, so a
// forged key reply that reaches enough pingers would put someone else's key
// into every client's keyring.  The pins remember each remailer's key, name
// and address as first seen, and hold a change that isn't a normal key
// rotation until it has been published for a while without the old key
// coming back.  The rules are those of the go-echolot pinger.

// PinMode says what ImportPubring does with an unexpected change.
type PinMode int

const (
	PinOff  PinMode = iota // no pinning
	PinWarn                // warn, and use the new key at once
	PinHold                // don't use the remailer until the change is accepted
)

// ParsePinMode reads urls.key_pinning: hold, warn or off.
func ParsePinMode(s string) (PinMode, error) {
	switch s {
	case "hold":
		return PinHold, nil
	case "warn":
		return PinWarn, nil
	case "off":
		return PinOff, nil
	}
	return PinOff, fmt.Errorf("key pinning must be hold, warn or off, not %q", s)
}

const (
	// A new key is a normal rotation when the pinned one expires within
	// this time.  yamn makes its next key when the current one has 48
	// hours left; a day more for the pingers to pick it up.
	pinRotationWindow = 72 * time.Hour
	// A held change is accepted after this time, if it was seen at least
	// pinMinSightings times, pinSightingGap apart, and the old key or name
	// hasn't been seen again since it appeared.
	pinQuarantine   = 24 * time.Hour
	pinMinSightings = 2
	pinSightingGap  = 30 * time.Minute
	// Pins of remailers that are no longer listed, and held changes that
	// are no longer published, are dropped after these times.
	pinRetention      = 90 * 24 * time.Hour
	heldRetention     = 7 * 24 * time.Hour
	rejectedRetention = 30 * 24 * time.Hour
)

// Pin is what is known about one remailer, by address.
type Pin struct {
	Name      string    `json:"name"`
	Address   string    `json:"address"`
	Keyid     string    `json:"keyid"`
	Created   string    `json:"created"` // the key's dates, as in the keyring
	Expires   string    `json:"expires"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"` // when the keyring last listed this key
}

// HeldChange is a keyring entry that differs from the pins in a way a key
// rotation doesn't explain.
type HeldChange struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Keyid   string `json:"keyid"`
	Created string `json:"created"`
	Expires string `json:"expires"`
	// What it changes: the name pinned to its address, the address pinned
	// to its name, the key pinned to its address.
	OldName    string `json:"old_name,omitempty"`
	OldAddress string `json:"old_address,omitempty"`
	OldKeyid   string `json:"old_keyid,omitempty"`
	Reason     string `json:"reason"`

	Since        time.Time `json:"since"`         // first seen
	LastSighting time.Time `json:"last_sighting"` // last one counted
	Sightings    int       `json:"sightings"`
	LastSeen     time.Time `json:"last_seen"` // last listed
	Rejected     bool      `json:"rejected,omitempty"`
}

// PinEvent is something pinning reports after an import.
type PinEvent struct {
	Warn bool
	Msg  string
}

// PinStore is the pin file.
type PinStore struct {
	Pins map[string]*Pin        `json:"pins"` // by address
	Held map[string]*HeldChange `json:"held"` // by the new entry's address
	file string
}

// LoadPins reads the pin file.  A missing file gives an empty store.
func LoadPins(file string) (*PinStore, error) {
	s := &PinStore{file: file}
	b, err := os.ReadFile(file)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, s); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
	}
	if s.Pins == nil {
		s.Pins = make(map[string]*Pin)
	}
	if s.Held == nil {
		s.Held = make(map[string]*HeldChange)
	}
	for addr, p := range s.Pins {
		if p == nil || p.Address != addr {
			return nil, fmt.Errorf("%s: pin for %s is damaged", file, addr)
		}
	}
	for addr, h := range s.Held {
		if h == nil || h.Address != addr {
			return nil, fmt.Errorf("%s: held change for %s is damaged", file, addr)
		}
	}
	return s, nil
}

// Save writes the pin file, replacing it in one step.
func (s *PinStore) Save() error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if old, err := os.ReadFile(s.file); err == nil && string(old) == string(b) {
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.file), filepath.Base(s.file)+".tmp*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(b)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), s.file)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// nameOwner returns the pin that has name for another address than addr.
func (s *PinStore) nameOwner(name, addr string) *Pin {
	for _, p := range s.Pins {
		if p.Name == name && p.Address != addr {
			return p
		}
	}
	return nil
}

// FindHeld returns the held change for a remailer name or address.
func (s *PinStore) FindHeld(ref string) *HeldChange {
	for _, h := range s.Held {
		if h.Address == ref || h.Name == ref {
			return h
		}
	}
	return nil
}

// Accept applies the held change for ref (a name or address) now.
func (s *PinStore) Accept(ref string, now time.Time) (*HeldChange, error) {
	h := s.FindHeld(ref)
	if h == nil {
		return nil, fmt.Errorf("%s: no held change", ref)
	}
	s.apply(h, now)
	return h, nil
}

// Reject keeps the pins for ref (a name or address): its held change isn't
// accepted automatically.  It's forgotten once it hasn't been published
// for 30 days.
func (s *PinStore) Reject(ref string) (*HeldChange, error) {
	h := s.FindHeld(ref)
	if h == nil {
		return nil, fmt.Errorf("%s: no held change", ref)
	}
	h.Rejected = true
	return h, nil
}

// apply makes the held change h the pin for its address.
func (s *PinStore) apply(h *HeldChange, now time.Time) {
	first := now
	if p := s.Pins[h.Address]; p != nil && p.Name == h.Name {
		first = p.FirstSeen
	}
	if h.OldAddress != "" {
		delete(s.Pins, h.OldAddress)
	}
	s.Pins[h.Address] = &Pin{
		Name: h.Name, Address: h.Address, Keyid: h.Keyid,
		Created: h.Created, Expires: h.Expires,
		FirstSeen: first, LastSeen: now,
	}
	delete(s.Held, h.Address)
}

func newPin(r Remailer, now time.Time) *Pin {
	return &Pin{
		Name: r.name, Address: r.Address, Keyid: hex.EncodeToString(r.Keyid),
		Created: r.from.UTC().Format(date_format), Expires: r.until.UTC().Format(date_format),
		FirstSeen: now, LastSeen: now,
	}
}

// rotationProblem returns why r isn't a normal rotation of the pinned key
// p, or "".
func rotationProblem(p *Pin, r Remailer, now time.Time) string {
	created, err1 := time.Parse(date_format, p.Created)
	expires, err2 := time.Parse(date_format, p.Expires)
	if err1 != nil || err2 != nil {
		return "" // a damaged pin can't object
	}
	if r.from.Before(created) {
		return fmt.Sprintf("its new key %x was created %s, before the pinned key %s (%s)",
			r.Keyid, r.from.UTC().Format(date_format), p.Keyid, p.Created)
	}
	_, end := midnights(expires)
	if left := end.Sub(now); left > pinRotationWindow {
		return fmt.Sprintf("it has a new key %x while the pinned key %s is valid for another %s",
			r.Keyid, p.Keyid, roundDuration(left))
	}
	return ""
}

// roundDuration writes d as days or hours.
func roundDuration(d time.Duration) string {
	if d >= 48*time.Hour {
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
	return fmt.Sprintf("%d hours", int(d.Hours()))
}

// released reports whether the held change h may be applied at now.
func (s *PinStore) released(h *HeldChange, now time.Time) bool {
	if h.Rejected || now.Sub(h.Since) < pinQuarantine || h.Sightings < pinMinSightings {
		return false
	}
	for _, addr := range []string{h.Address, h.OldAddress} {
		if p := s.Pins[addr]; addr != "" && p != nil && !p.LastSeen.Before(h.Since) {
			return false // the old key or name is still published
		}
	}
	return true
}

// check compares the keys of a keyring (after dropAmbiguous) with the pins
// at now.  It returns the keys to use, why remailers are held (by name and
// address), and what to report.
func (s *PinStore) check(rems []Remailer, mode PinMode, now time.Time) (use []Remailer, held map[string]string, events []PinEvent) {
	held = make(map[string]string)
	note := func(warn bool, format string, a ...any) {
		events = append(events, PinEvent{Warn: warn, Msg: fmt.Sprintf(format, a...)})
	}

	// The entries for each address, in keyring order; the last one is the
	// one ImportPubring would use.
	var order []string
	byAddr := make(map[string][]Remailer)
	for _, r := range rems {
		if byAddr[r.Address] == nil {
			order = append(order, r.Address)
		}
		byAddr[r.Address] = append(byAddr[r.Address], r)
	}
	listed := func(addr, name, keyid string) (Remailer, bool) {
		for _, r := range byAddr[addr] {
			if r.name == name && hex.EncodeToString(r.Keyid) == keyid {
				return r, true
			}
		}
		return Remailer{}, false
	}
	// Pinned keys that are still published.
	for addr, p := range s.Pins {
		if _, ok := listed(addr, p.Name, p.Keyid); ok {
			p.LastSeen = now
		}
	}

	for _, addr := range order {
		entries := byAddr[addr]
		r := entries[len(entries)-1]
		keyid := hex.EncodeToString(r.Keyid)
		pin := s.Pins[addr]
		owner := s.nameOwner(r.name, addr)

		var why []string
		h := &HeldChange{
			Name: r.name, Address: addr, Keyid: keyid,
			Created: r.from.UTC().Format(date_format), Expires: r.until.UTC().Format(date_format),
		}
		switch {
		case pin == nil && owner == nil:
			s.Pins[addr] = newPin(r, now)
			note(false, "%s (%s): first seen, key %s pinned", r.name, addr, keyid)
			use = append(use, r)
			continue
		case pin != nil && pin.Name == r.name && owner == nil:
			if pin.Keyid == keyid {
				use = append(use, r)
				continue
			}
			problem := rotationProblem(pin, r, now)
			if problem == "" {
				note(false, "%s: new key %s replaces %s (expires %s)", r.name, keyid, pin.Keyid, pin.Expires)
				first := pin.FirstSeen
				s.Pins[addr] = newPin(r, now)
				s.Pins[addr].FirstSeen = first
				delete(s.Held, addr)
				use = append(use, r)
				continue
			}
			h.OldKeyid = pin.Keyid
			why = append(why, problem)
		default:
			if pin != nil {
				h.OldKeyid = pin.Keyid
				if pin.Name != r.name {
					h.OldName = pin.Name
					why = append(why, fmt.Sprintf("its address %s was pinned to the name %s", addr, pin.Name))
				}
			}
			if owner != nil {
				h.OldAddress = owner.Address
				why = append(why, fmt.Sprintf("its name was pinned to the address %s", owner.Address))
			}
		}
		h.Reason = strings.Join(why, "; ")

		// The same change as before counts as another sighting.
		if old := s.Held[addr]; old != nil && old.Name == h.Name && old.Keyid == h.Keyid {
			old.OldName, old.OldAddress, old.OldKeyid, old.Reason = h.OldName, h.OldAddress, h.OldKeyid, h.Reason
			h = old
			if now.Sub(h.LastSighting) >= pinSightingGap {
				h.Sightings++
				h.LastSighting = now
			}
		} else {
			h.Since, h.LastSighting, h.Sightings = now, now, 1
			s.Held[addr] = h
		}
		h.LastSeen = now

		switch {
		case mode == PinWarn:
			s.apply(h, now)
			note(true, "%s (%s): %s. Using it anyway (key_pinning: warn).", r.name, addr, h.Reason)
			use = append(use, r)
		case s.released(h, now):
			s.apply(h, now)
			note(true, "%s (%s): accepted after %s held: %s.", r.name, addr, roundDuration(now.Sub(h.Since)), h.Reason)
			use = append(use, r)
		default:
			state := fmt.Sprintf("held since %s", h.Since.UTC().Format("2006-01-02 15:04 MST"))
			if h.Rejected {
				state = "rejected"
			}
			msg := fmt.Sprintf("%s (%s): %s; %s. Check it with the remailer's operator; "+
				"yamn --pin-accept %s uses it now.", r.name, addr, h.Reason, state, r.name)
			note(true, "%s", msg)
			// A key change while the pinned key is still published: use that.
			if pin != nil && pin.Name == r.name && owner == nil {
				if old, ok := listed(addr, pin.Name, pin.Keyid); ok {
					use = append(use, old)
					continue
				}
			}
			held[r.name] = msg
			held[addr] = msg
		}
	}

	// Forget remailers and changes that are gone.
	for addr, p := range s.Pins {
		if now.Sub(p.LastSeen) > pinRetention && s.Held[addr] == nil {
			delete(s.Pins, addr)
		}
	}
	for addr, h := range s.Held {
		keep := heldRetention
		if h.Rejected {
			keep = rejectedRetention
		}
		if now.Sub(h.LastSeen) > keep {
			delete(s.Held, addr)
		}
	}
	return
}

// List returns the pins and held changes, sorted by name, for --pins.
func (s *PinStore) List() (pins []Pin, held []HeldChange) {
	for _, p := range s.Pins {
		pins = append(pins, *p)
	}
	for _, h := range s.Held {
		held = append(held, *h)
	}
	slices.SortFunc(pins, func(a, b Pin) int { return strings.Compare(a.Name+a.Address, b.Name+b.Address) })
	slices.SortFunc(held, func(a, b HeldChange) int { return strings.Compare(a.Name+a.Address, b.Name+b.Address) })
	return
}
