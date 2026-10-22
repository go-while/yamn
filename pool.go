// vim: tabstop=2 shiftwidth=2

package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	//"github.com/codahale/blake2"
	"github.com/Masterminds/log-go"
	"github.com/crooks/yamn/crandom"
	"github.com/crooks/yamn/keymgr"
	"github.com/luksen/maildir"
)

// serverPoolOutboundSend is intended to be run concurrently with the server
// daemon.  It sends messages from the pool at timed intervals.
func serverPoolOutboundSend() {
	if cfg.Pool.Loop < 120 {
		log.Warnf(
			"Pool loop of %d Seconds is too short. "+
				"Adjusting to minimum of 120 Seconds.",
			cfg.Pool.Loop,
		)
		cfg.Pool.Loop = 120
	}
	sleepFor := time.Duration(cfg.Pool.Loop) * time.Second
	for {
		sendPoolBatch()
		time.Sleep(sleepFor)
	}
}

// errPoolLocked is returned by lockPool when another process is sending
// from the pool.
var errPoolLocked = errors.New("another yamn process is sending from the pool")

// sendPoolBatch sends one binomial mix batch from the pool, unless another
// process holds the pool lock.
func sendPoolBatch() {
	unlock, err := lockPool()
	if err != nil {
		log.Infof("Skipping this pool run: %s", err)
		return
	}
	defer unlock()
	// Read dynamic mix of outbound files from the Pool
	// filenames = dynamicMix()
	// Read binomialMix of outbound files from the Pool
	filenames := binomialMix()
	// remailer-* replies aren't anonymous, so they aren't mixed.
	replies, _ := readDir(cfg.Files.Pooldir, "r")
	sendPoolFiles(append(filenames, replies...))
}

// poolWorkers is how many pool files are sent at the same time.  A mail
// server that is slow or never answers holds up one worker for up to
// smtpTimeout, while the others carry on.
const poolWorkers = 4

// sendPoolFiles sends the given pool files, poolWorkers at a time, and
// returns when all are done.  Names starting with "r" are remailer-*
// replies, the others outbound messages.
func sendPoolFiles(filenames []string) {
	sem := make(chan struct{}, poolWorkers)
	var wg sync.WaitGroup
	for _, filename := range filenames {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() {
				<-sem
				wg.Done()
			}()
			if strings.HasPrefix(filename, "r") {
				sendReplyFile(filename)
			} else {
				emailPoolFile(filename)
			}
		}()
	}
	wg.Wait()
}

// writeReplyToPool stores a remailer-* reply in the pool, to be sent by
// sendReplyFile.
func writeReplyToPool(msg []byte) error {
	_, err := writePoolFile("r", msg)
	return err
}

// sendReplyFile sends a pooled remailer-* reply to its To address and
// deletes it.  A reply gets one attempt: the request's From can be forged,
// so it isn't worth retrying.
func sendReplyFile(filename string) {
	defer poolDelete(filename)
	content, err := os.ReadFile(path.Join(cfg.Files.Pooldir, filename))
	if err != nil {
		log.Warnf("Failed to read reply %s: %s", filename, err)
		return
	}
	msg, err := mail.ReadMessage(bytes.NewReader(content))
	if err != nil {
		log.Warnf("Reply %s is not a mail: %s", filename, err)
		return
	}
	to, err := mail.ParseAddress(msg.Header.Get("To"))
	if err != nil {
		log.Warnf("Reply %s has no valid To: %s", filename, err)
		return
	}
	if err = mailBytes(content, []string{to.Address}); err != nil {
		log.Infof("Failed to send remailer-* reply %s: %s", filename, err)
	}
}

// poolOutboundSend flushes the outbound pool.  This should only be performed
// on clients, where all messages should be sent instantly after creation.
func poolOutboundSend() {
	var err error
	if cfg.Remailer.Daemon || flag.Daemon {
		// This should never happen.  If the server is started as a
		// daemon, the serverPoolOutboundSend process is initiated and
		// runs in an endless loop.  This function would conflict with
		// it.
		err = errors.New("cannot flush pool when running as a daemon")
		panic(err)
	}
	unlock, err := lockPool()
	if err != nil {
		log.Warnf("Not flushing the pool: %s", err)
		return
	}
	defer unlock()
	var filenames []string
	// Read all the pool files
	filenames, err = readDir(cfg.Files.Pooldir, "m")
	if err != nil {
		log.Warnf("Reading pool failed: %s", err)
		return
	}
	if flag.Remailer {
		// During normal operation, the pool shouldn't be flushed.
		log.Warn("Flushing outbound remailer pool")
	}
	replies, _ := readDir(cfg.Files.Pooldir, "r")
	sendPoolFiles(append(filenames, replies...))
}

// emailPoolFile tries to email a given file from the Pool.  If conditions are
// met, the file is then deleted.
func emailPoolFile(filename string) {
	delFlag, err := mailPoolFile(path.Join(cfg.Files.Pooldir, filename))
	if err != nil {
		log.Warnf("Pool mailing failed: %s", err)
		if delFlag {
			// If delFlag is true, we delete the file, even though
			// mailing failed.
			poolDelete(filename)
		}
	} else {
		stats.outMail.Add(1)
		poolDelete(filename)
	}
}

// getBatchSize takes a Pool size and returns a corresponding batch size.  This
// is intended for use with Binomial Mix Pools.
func getBatchSize(poolSize int) int {
	/*
		poolSize         -  Number of files in the pool
		cfg.Pool.Size    -  Minimum messages to keep in pool
		cfg.Pool.MinSend -  Minimum number of messages to consider sending
		cfg.Pool.Rate    -  Percentage of Pool in the batch
	*/
	if poolSize < (cfg.Pool.Size + cfg.Pool.MinSend) {
		return 0
	}
	sendable := poolSize - cfg.Pool.Size
	rate := float32(cfg.Pool.Rate) / 100
	maxSend := max(1, int(float32(poolSize)*rate))
	return min(sendable, maxSend)
}

// binomialMix returns a batched subset of Pool files to send using a
// Probability B/P method of selecting each file.
func binomialMix() (batch []string) {
	poolFiles, err := readDir(cfg.Files.Pooldir, "m")
	if err != nil {
		log.Warnf("Unable to access pool: %s", err)
		return
	}
	poolSize := len(poolFiles)
	batchSize := getBatchSize(poolSize)
	if batchSize == 0 {
		log.Tracef("Binomial Mix Pool: Size=%d", poolSize)
		// If the batch is empty, don't bother to process it.
		return
	}
	// Shuffle the slice of filenames now as we're only going to consider a
	// subset in the following loop.
	crandom.Shuffle(poolFiles)
	// Multiply probability by 255 as dice() returns 0-255.
	prob := int((float32(batchSize) / float32(poolSize)) * 255)
	// Test each pool filename against a biased coin-toss
	for _, s := range poolFiles[:batchSize] {
		if prob >= crandom.Dice() {
			batch = append(batch, s)
		}
	}
	log.Tracef(
		"Binomial Mix Pool: Size=%d, Batch=%d, Prob=%d/255, Sending=%d",
		poolSize,
		batchSize,
		prob,
		len(batch),
	)
	return
}

// Delete a given file from the pool
func poolDelete(filename string) {
	// Delete a pool file
	err := os.Remove(path.Join(cfg.Files.Pooldir, filename))
	if err != nil {
		log.Errorf("Failed to remove %s from %s\n", filename, cfg.Files.Pooldir)
	} else {
		log.Tracef("Deleted %s from Pool", filename)
	}
}

// processMail reads the Remailer's Maildir and processes the content
func processMail(secret *keymgr.Secring) (err error) {
	dir := maildir.Dir(cfg.Files.Maildir)
	// Move new mail to cur, then read everything in cur.  If Unseen fails
	// part way, the messages it already moved are still in cur; they'd
	// never be read if only its result were used.  Processed mail is
	// deleted or moved to .rejected, so cur holds nothing else.
	if _, err = dir.Unseen(); err != nil {
		log.Warnf("Reading %s/new: %s", cfg.Files.Maildir, err)
	}
	keys, err := dir.Keys()
	if err != nil {
		return
	}
	newMsgs := len(keys)
	if newMsgs == 0 {
		// Nothing to do, move along!
		return
	}
	log.Tracef(
		"Reading %d messages from %s\n",
		newMsgs,
		cfg.Files.Maildir,
	)
	// Increment inbound Email counter
	stats.inMail.Add(int64(newMsgs))
	// Fetch headers for each Maildir key
	var head mail.Header
	for _, key := range keys {
		head, err = dir.Header(key)
		if err != nil {
			rejectMail(dir, key, fmt.Sprintf("getting headers failed: %s", err))
			continue
		}
		// The Subject determines if the message needs remailer-foo handling
		subject := strings.TrimSpace(strings.ToLower(head.Get("Subject")))
		if strings.HasPrefix(subject, "remailer-") {
			// It's a remailer-foo request
			err = remailerFoo(subject, head.Get("From"))
			if err == nil {
				// Increments stats counter
				stats.inRemFoo.Add(1)
			} else {
				log.Info(err)
			}
			err = dir.Purge(key)
			if err != nil {
				log.Warnf(
					"Cannot delete remailer-foo mail: %s",
					err,
				)
			}
			// Nothing else to do, move on to the next message
			continue
		}
		// It's not a remailer-foo request so assume a remailer message
		var mailMsg *mail.Message
		mailMsg, err := dir.Message(key)
		if err != nil {
			rejectMail(dir, key, fmt.Sprintf("reading message failed: %s", err))
			continue
		}
		var msg []byte
		// Convert the armored Yamn message to its byte components
		msg, err = stripArmor(mailMsg.Body)
		if err != nil {
			rejectMail(dir, key, fmt.Sprintf("not a yamn message (%s), From=%q, Subject=%q",
				err, head.Get("From"), head.Get("Subject")))
			continue
		}
		if msg == nil {
			rejectMail(dir, key, "dearmor returned zero bytes")
			continue
		}
		err = decodeSafely(msg, secret)
		if errors.Is(err, errPoolWrite) {
			// A local problem, so keep the mail for the next run.
			log.Warnf("%s: keeping the mail: %s", key, err)
			requeueMail(dir, key)
			continue
		}
		logDecodeError(err, log.Info)
		err = dir.Purge(key)
		if err != nil {
			log.Warnf("Cannot delete mail: %s", err)
		}
	} // Maildir keys loop
	return
}

// requeueMail moves a message back from cur to new in the Maildir, so the
// next processMail run reads it again.
func requeueMail(dir maildir.Dir, key string) {
	filename, err := dir.Filename(key)
	if err == nil {
		err = os.Rename(filename, path.Join(string(dir), "new", key))
	}
	if err != nil {
		log.Warnf("%s: moving it back to new failed: %s", key, err)
	}
}

// rejectedMaildir is a Maildir++ subfolder for incoming mail that isn't a
// yamn message (spam, abuse complaints, bounces...).  Operators can read it
// with a mail client; expireRejected deletes old messages from it.
func rejectedMaildir() maildir.Dir {
	return maildir.Dir(path.Join(cfg.Files.Maildir, ".rejected"))
}

// rejectMail moves a message that can't be processed from the remailer's
// Maildir to the rejected folder.  If that fails, the message stays where it
// is.
func rejectMail(dir maildir.Dir, key, reason string) {
	rejected := rejectedMaildir()
	err := rejected.Create()
	if err == nil {
		err = dir.Move(rejected, key)
	}
	if err != nil {
		log.Warnf("%s: %s. Moving it to %s failed: %s", key, reason, rejected, err)
		return
	}
	log.Infof("%s: %s. Moved to %s", key, reason, rejected)
}

// expireRejected deletes messages in the rejected Maildir folder that are
// older than pool.rejected_age days.
func expireRejected() {
	rejected := rejectedMaildir()
	maxAge := time.Duration(cfg.Pool.RejectedAge) * 24 * time.Hour
	deleted := 0
	for _, sub := range []string{"cur", "new"} {
		subdir := path.Join(string(rejected), sub)
		entries, err := os.ReadDir(subdir)
		if err != nil {
			// The folder only exists once a message has been rejected.
			continue
		}
		for _, e := range entries {
			info, err := e.Info()
			if err != nil || e.IsDir() || time.Since(info.ModTime()) <= maxAge {
				continue
			}
			if err := os.Remove(path.Join(subdir, e.Name())); err != nil {
				log.Warnf("Failed to delete expired rejected mail %s: %s", e.Name(), err)
				continue
			}
			deleted++
		}
	}
	if deleted > 0 {
		log.Infof("Deleted %d rejected mails older than %d days", deleted, cfg.Pool.RejectedAge)
	}
}

// logDecodeError logs an error from decoding a received message with logf.
// A duplicate packet is normal (see errDuplicatePacket), so it's only logged
// at Trace level.  Based on lampeh/yamn 0f2038d "duplicate messsages are not
// worth a warning".
func logDecodeError(err error, logf func(...interface{})) {
	switch {
	case err == nil:
	case errors.Is(err, errDuplicatePacket):
		log.Trace("Discarding duplicate packet: another copy was already processed")
	default:
		logf(err)
	}
}

// processInpool is similar to processMail but reads the Inbound Pool
func processInpool(prefix string, secret *keymgr.Secring) {
	poolFiles, err := readDir(cfg.Files.Pooldir, prefix)
	if err != nil {
		log.Warnf("Unable to access inbound pool: %s", err)
		return
	}
	poolSize := len(poolFiles)
	processed := 0
	for _, f := range poolFiles {
		filename := path.Join(cfg.Files.Pooldir, f)
		msg := make([]byte, messageBytes)
		msg, err = os.ReadFile(filename)
		if err != nil {
			log.Warnf("Failed to read %s from pool: %s", f, err)
			continue
		}
		err = decodeSafely(msg, secret)
		if errors.Is(err, errPoolWrite) {
			// Keep the file to try again on the next run.
			log.Warnf("%s: keeping it in the pool: %s", f, err)
			continue
		}
		logDecodeError(err, log.Warn)
		poolDelete(f)
		processed++
	}
	if poolSize > 0 {
		log.Tracef("Inbound pool processing complete. Read=%d, Decoded=%d",
			poolSize, processed)
	}
}

// randPoolFilename returns a random filename with a given prefix.  This should
// be used in all instances where a new pool file is required.
func randPoolFilename(prefix string) (fqfn string) {
	for {
		outfileName := prefix + hex.EncodeToString(crandom.Randbytes(7))
		fqfn = path.Join(cfg.Files.Pooldir, outfileName)
		_, err := os.Stat(fqfn)
		if err != nil {
			// For once we want an error (indicating the file
			// doesn't exist)
			break
		}
	}
	return
}

// errPoolWrite marks a failure to store a file in the pool.  That's a local
// problem (disk full, permissions), not a bad message, so the message is
// kept where it came from and processed again later.
var errPoolWrite = errors.New("pool write failed")

// poolTempPrefix starts the names of pool files that are still being
// written.  No pool reader looks for it.
const poolTempPrefix = ".tmp-"

// writePoolFile stores content as a new pool file and returns its name.
// Supported prefixes:
//
//	m        outbound message (final or intermediate)
//	i        inbound message (destined for this remailer)
//	p        partial message chunk needing assembly
//	outfile- delivered message (mail.outfile)
//
// The content goes to a temporary file (mode 0600, created exclusively) that
// is synced and then renamed, so a pool reader never sees a partial file and
// a crash leaves none behind.
func writePoolFile(prefix string, content ...[]byte) (filename string, err error) {
	fqfn := randPoolFilename(prefix)
	if err = writePoolFileAs(fqfn, content...); err != nil {
		return "", err
	}
	_, filename = path.Split(fqfn)
	return filename, nil
}

// writePoolFileAs atomically writes content to the pool file fqfn, replacing
// it if it exists, in the same way as writePoolFile.
func writePoolFileAs(fqfn string, content ...[]byte) (err error) {
	tmp, err := os.CreateTemp(cfg.Files.Pooldir, poolTempPrefix+"*")
	if err != nil {
		return fmt.Errorf("%w: %w", errPoolWrite, err)
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			err = fmt.Errorf("%w: %w", errPoolWrite, err)
		}
	}()
	for _, c := range content {
		if _, err = tmp.Write(c); err != nil {
			return
		}
	}
	if err = tmp.Sync(); err != nil {
		return
	}
	if err = tmp.Close(); err != nil {
		return
	}
	if err = os.Rename(tmp.Name(), fqfn); err != nil {
		return
	}
	// Sync the directory too, so the new name survives a crash.
	if d, derr := os.Open(cfg.Files.Pooldir); derr == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

// cleanPoolTemp deletes temporary pool files left behind by a crash.
func cleanPoolTemp() {
	names, err := readDir(cfg.Files.Pooldir, poolTempPrefix)
	if err != nil {
		return
	}
	for _, name := range names {
		fqfn := path.Join(cfg.Files.Pooldir, name)
		info, err := os.Stat(fqfn)
		if err == nil && time.Since(info.ModTime()) > 24*time.Hour {
			os.Remove(fqfn)
		}
	}
}

// internalHeader returns the internal Yamn-Pooled-Date line that starts
// every pooled message.
func internalHeader() []byte {
	buf := new(bytes.Buffer)
	writeInternalHeader(buf)
	return buf.Bytes()
}

// writeMessageToPool requires a recipient address (another remailer) and a
// payload (that gets Base64 armored).
func writeMessageToPool(sendTo string, payload []byte) error {
	// Build the mail first, so nothing is pooled if it's refused.
	mail := new(bytes.Buffer)
	if err := writeYamnMail(mail, sendTo, payload); err != nil {
		return err
	}
	_, err := writePoolFile("m", internalHeader(), mail.Bytes())
	return err
}

// deliverToMaildir stores incoming mail (-R, --read-mail) in the remailer's
// Maildir.  data is a single mail, or an mbox of several (as written by
// --stdout), which is split into one Maildir message per mail.
func deliverToMaildir(dir maildir.Dir, data []byte) (delivered int, err error) {
	mails := [][]byte{data}
	if isMbox(data) {
		mails = splitMbox(data)
	}
	for _, mail := range mails {
		d, err := dir.NewDelivery()
		if err != nil {
			return delivered, err
		}
		if _, err := d.Write(mail); err != nil {
			d.Abort()
			return delivered, err
		}
		if err := d.Close(); err != nil {
			return delivered, err
		}
		delivered++
	}
	return delivered, nil
}

// writePlainToPool writes a plaintext file to the pool and returns the filename
func writePlainToPool(payload []byte, prefix string) (filename string, err error) {
	return writePoolFile(prefix, internalHeader(), payload)
}
