package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"regexp"
	"time"

	"github.com/Masterminds/log-go"
	"github.com/crooks/yamn/config"
	"github.com/crooks/yamn/keymgr"
	"github.com/crooks/yamn/minisign"
)

// validPingerName matches pinger names that are safe as file name suffixes.
var validPingerName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// pingerFile returns where one pinger's copy of file (pubring.mix or
// mlist2.txt) is kept.
func pingerFile(file, pinger string) string {
	return file + "." + pinger
}

// refreshKeyring downloads the remailer keyring and stats.
//
// With a single pinger (urls.pubring and urls.mlist2), the files are
// downloaded directly.  With urls.pingers, each pinger's files are
// downloaded next to them (pubring.mix.<name>, mlist2.txt.<name>) and
// merged: a key is only used if urls.key_quorum pingers publish it, and
// stats are the median of the pingers' (see keymgr.MergePingers).
//
// Files are fetched when they are more than an hour old, unless force is
// set.  Nothing is fetched if urls.fetch is off and force isn't set.
func refreshKeyring(force bool) {
	if !cfg.Urls.Fetch && !force {
		return
	}
	fetch := func(url, filename string, validate func([]byte) error, keys []minisign.PublicKey) {
		if url == "" {
			return
		}
		if plainHTTP(url) && len(keys) == 0 {
			log.Warnf("%s is plain http: anyone on the network path can change the download", url)
		}
		if !force {
			timedURLFetch(url, filename, validate, keys)
			return
		}
		log.Infof("Fetching %s and storing in %s", url, filename)
		if err := fetchFile(url, filename, validate, keys); err != nil {
			log.Warn(err)
		}
	}
	if len(cfg.Urls.Pingers) == 0 {
		keys := pingerKeys(cfg.Urls.PingerKeys)
		fetch(cfg.Urls.Pubring, cfg.Files.Pubring, keymgr.CheckPubring, keys)
		fetch(cfg.Urls.Mlist2, cfg.Files.Mlist2, keymgr.CheckStats, keys)
		return
	}
	var files []keymgr.PingerFiles
	for _, p := range usablePingers(cfg.Urls.Pingers) {
		pubFile := pingerFile(cfg.Files.Pubring, p.Name)
		statsFile := pingerFile(cfg.Files.Mlist2, p.Name)
		keys := pingerKeys(p.Keys)
		fetch(p.Pubring, pubFile, keymgr.CheckPubring, keys)
		fetch(p.Mlist2, statsFile, keymgr.CheckStats, keys)
		files = append(files, keymgr.PingerFiles{
			Name:    p.Name,
			Pubring: readIfExists(pubFile),
			Stats:   readIfExists(statsFile),
		})
	}
	mergePingerFiles(files)
}

// usablePingers returns the configured pingers, without those whose name
// is invalid or used twice.
func usablePingers(pingers []config.Pinger) (usable []config.Pinger) {
	seen := make(map[string]bool)
	for _, p := range pingers {
		switch {
		case !validPingerName.MatchString(p.Name):
			log.Warnf("Ignoring pinger %q: names must be 1-32 letters, digits, - or _", p.Name)
		case seen[p.Name]:
			log.Warnf("Ignoring pinger %q: the name is used twice", p.Name)
		default:
			seen[p.Name] = true
			usable = append(usable, p)
		}
	}
	return
}

// mergePingerFiles merges the pingers' downloaded files into
// files.pubring and files.mlist2, and logs where the pingers disagree.
func mergePingerFiles(files []keymgr.PingerFiles) {
	res := keymgr.MergePingers(
		files,
		cfg.Urls.KeyQuorum,
		cfg.Stats.StaleHrs,
		cfg.Stats.UseExpired,
		time.Now(),
	)
	for _, m := range res.Warnings {
		log.Warn("Pingers: " + m)
	}
	for _, m := range res.Notes {
		log.Info("Pingers: " + m)
	}
	for _, m := range res.Details {
		log.Debug("Pingers: " + m)
	}
	if res.Pubring != nil {
		writeIfChanged(cfg.Files.Pubring, res.Pubring)
	}
	if res.Stats != nil {
		writeIfChanged(cfg.Files.Mlist2, res.Stats)
	}
}

// readIfExists returns a file's content, or nil if it can't be read.
func readIfExists(filename string) []byte {
	content, err := os.ReadFile(filename)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Warn(err)
		}
		return nil
	}
	return content
}

// writeIfChanged replaces filename with content unless it already holds
// exactly that, so an unchanged keyring isn't imported again.
func writeIfChanged(filename string, content []byte) {
	if old, err := os.ReadFile(filename); err == nil && bytes.Equal(old, content) {
		return
	}
	if err := writeFileAtomic(filename, content, 0644); err != nil {
		log.Warnf("Writing %s: %s", filename, err)
	}
}
