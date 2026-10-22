package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/log-go"
	"github.com/crooks/yamn/keymgr"
)

// Pingers collect remailer keys by unsigned mail, so a forged remailer-key
// reply can get a key into their keyrings that the remailer doesn't have.
// Clients would then encrypt to whoever sent it.  The remailer is the one
// that can tell: it checks every keyring it downloads against its secret
// keyring.

// keyringSource is a downloaded keyring and who published it.
type keyringSource struct {
	label string // e.g. "pinger mixmin"
	file  string
}

// keyringSources returns the keyrings to check: each pinger's copy with
// urls.pingers, otherwise files.pubring.
func keyringSources() (sources []keyringSource) {
	if len(cfg.Urls.Pingers) == 0 {
		return []keyringSource{{label: "the keyring " + cfg.Files.Pubring, file: cfg.Files.Pubring}}
	}
	for _, p := range usablePingers(cfg.Urls.Pingers) {
		sources = append(sources, keyringSource{
			label: "pinger " + p.Name,
			file:  pingerFile(cfg.Files.Pubring, p.Name),
		})
	}
	return
}

// checkPublishedKeys warns when a keyring lists a key for this remailer's
// name or address that isn't in its secret keyring, its name with another
// address, or its address under another name.  It returns the problems
// found (also those already warned about recently).
func checkPublishedKeys(secret *keymgr.Secring, now time.Time) (problems []string) {
	name, addy := cfg.Remailer.Name, cfg.Remailer.Address
	for _, src := range keyringSources() {
		content := readIfExists(src.file)
		if content == nil {
			continue
		}
		keys := keymgr.PublishedFor(content, name, addy, now)
		if len(keys) == 0 {
			publishedNote(now, fmt.Sprintf("%s doesn't list this remailer (%s) yet", src.label, name))
			continue
		}
		for _, k := range keys {
			var p string
			switch {
			case !strings.EqualFold(k.Address, addy):
				p = fmt.Sprintf("%s lists this remailer's name %s with the address %s instead of %s, "+
					"so clients send mail for %s there. Report it to the pinger's operator.",
					src.label, k.Name, k.Address, addy, name)
			case !strings.EqualFold(k.Name, name):
				p = fmt.Sprintf("%s lists this remailer's address %s under the name %s instead of %s. "+
					"Report it to the pinger's operator.",
					src.label, k.Address, k.Name, name)
			case !secret.Has(strings.ToLower(k.Keyid)):
				p = fmt.Sprintf("%s publishes key %s for this remailer, which isn't in its secret "+
					"keyring %s: clients using it encrypt to someone else. A forged remailer-key "+
					"reply to the pinger can cause this, or keys lost in a restore. Report it to "+
					"the pinger's operator.",
					src.label, k.Keyid, cfg.Files.Secring)
			default:
				continue
			}
			problems = append(problems, p)
			publishedWarn(now, p)
		}
	}
	return
}

// Problems found by checkPublishedKeys are logged once a day, not at every
// hourly check.
var (
	publishedMu     sync.Mutex
	publishedLogged = make(map[string]time.Time)
)

func publishedWarn(now time.Time, msg string) {
	if publishedDue(now, msg) {
		log.Warn(msg)
	}
}

func publishedNote(now time.Time, msg string) {
	if publishedDue(now, msg) {
		log.Info(msg)
	}
}

func publishedDue(now time.Time, msg string) bool {
	publishedMu.Lock()
	defer publishedMu.Unlock()
	if last, ok := publishedLogged[msg]; ok && now.Sub(last) < 24*time.Hour {
		return false
	}
	publishedLogged[msg] = now
	return true
}
