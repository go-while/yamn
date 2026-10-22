package main

import (
	"fmt"
	"os"
	"time"

	"github.com/Masterminds/log-go"
	"github.com/crooks/yamn/keymgr"
)

// newPubring returns the public keyring, with key pinning as configured.
func newPubring() *keymgr.Pubring {
	p := keymgr.NewPubring(cfg.Files.Pubring, cfg.Files.Mlist2)
	if mode, err := keymgr.ParsePinMode(cfg.Urls.KeyPinning); err == nil && mode != keymgr.PinOff {
		p.SetPinning(cfg.Files.Keypins, mode)
	}
	return p
}

// importPubring imports the public keyring and logs what key pinning
// found.  A remailer that runs for days logs each warning once a day.
func importPubring() error {
	err := Pubring.ImportPubring()
	now := time.Now()
	for _, e := range Pubring.PinEvents() {
		// Routine events (first seen, normal rotations) are only
		// logged at debug level.
		if e.Warn {
			dailyWarn(now, "Key pinning: "+e.Msg)
		} else {
			log.Debug("Key pinning: " + e.Msg)
		}
	}
	return err
}

// pinCommand runs --pins, --pin-accept or --pin-reject.
func pinCommand() error {
	pins, err := keymgr.LoadPins(cfg.Files.Keypins)
	if err != nil {
		return err
	}
	switch {
	case flag.PinAccept != "":
		h, err := pins.Accept(flag.PinAccept, time.Now())
		if err != nil {
			return err
		}
		if err := pins.Save(); err != nil {
			return err
		}
		fmt.Printf("Accepted: %s (%s), key %s, valid until %s.\n", h.Name, h.Address, h.Keyid, h.Expires)
		return nil
	case flag.PinReject != "":
		h, err := pins.Reject(flag.PinReject)
		if err != nil {
			return err
		}
		if err := pins.Save(); err != nil {
			return err
		}
		fmt.Printf("Rejected: %s (%s), key %s. The pinned key, name and address stay; "+
			"the change is forgotten once it hasn't been published for 30 days.\n", h.Name, h.Address, h.Keyid)
		return nil
	}
	if cfg.Urls.KeyPinning == "off" {
		fmt.Println("Key pinning is off (urls.key_pinning).")
	}
	list, held := pins.List()
	fmt.Printf("Pinned remailers (%s):\n", cfg.Files.Keypins)
	if len(list) == 0 {
		fmt.Println("  none yet")
	}
	for _, p := range list {
		fmt.Printf("  %-12s %-36s key %s, %s to %s, last seen %s\n",
			p.Name, p.Address, p.Keyid, p.Created, p.Expires, p.LastSeen.UTC().Format("2006-01-02"))
	}
	if len(held) == 0 {
		return nil
	}
	fmt.Println("\nHeld changes:")
	for _, h := range held {
		seen := "once"
		if h.Sightings > 1 {
			seen = fmt.Sprintf("%d times", h.Sightings)
		}
		state := fmt.Sprintf("held since %s, seen %s", h.Since.UTC().Format("2006-01-02 15:04 MST"), seen)
		if h.Rejected {
			state = "rejected"
		}
		fmt.Printf("  %-12s %-36s key %s, %s to %s\n    %s; %s\n",
			h.Name, h.Address, h.Keyid, h.Created, h.Expires, h.Reason, state)
	}
	fmt.Println("\nyamn --pin-accept NAME uses a held change now; --pin-reject NAME keeps the pin.")
	return nil
}

// pinCommandRequested reports whether a key pinning command was given.
func pinCommandRequested() bool {
	return flag.Pins || flag.PinAccept != "" || flag.PinReject != ""
}

// exitOnPinCommand runs a key pinning command and exits.
func exitOnPinCommand() {
	if err := pinCommand(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}
