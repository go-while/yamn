// vim: tabstop=2 shiftwidth=2

package main

import (
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path"
	"runtime/debug"
	"strings"
	"time"

	"github.com/Masterminds/log-go"
	"github.com/crooks/yamn/crandom"
	"github.com/crooks/yamn/idlog"
	"github.com/crooks/yamn/keymgr"
	"github.com/crooks/yamn/quickmail"
	//"github.com/codahale/blake2"
)

// Start the server process.  If run with --daemon, this will loop forever.
func loopServer() (err error) {
	// Initialize the Public Keyring
	Pubring = keymgr.NewPubring(
		cfg.Files.Pubring,
		cfg.Files.Mlist2,
	)
	// Fetch keyring and stats URLs
	refreshKeyring(false)
	// Initialize the Secret Keyring
	secret := keymgr.NewSecring(cfg.Files.Secring, cfg.Files.Pubkey)
	Pubring.ImportPubring()
	secret.ImportSecring()
	// Tell the secret keyring some basic info about this remailer
	secret.SetName(cfg.Remailer.Name)
	secret.SetAddress(cfg.Remailer.Address)
	secret.SetExit(cfg.Remailer.Exit)
	secret.SetValidity(cfg.Remailer.Keylife, cfg.Remailer.Keygrace)
	secret.SetVersion(version)
	// Create some dirs if they don't already exist
	createDirs()

	// Open the IDlog
	log.Tracef("Opening ID Log: %s", cfg.Files.IDlog)
	// NewInstance takes the filename and entry validity in days
	IDDb = idlog.NewIDLog(cfg.Files.IDlog, replayLogDays())
	defer IDDb.Close()
	// Open the chunk DB
	log.Tracef("Opening the Chunk DB: %s", cfg.Files.ChunkDB)
	ChunkDb = OpenChunk(cfg.Files.ChunkDB)
	ChunkDb.SetExpire(cfg.Remailer.ChunkExpire)

	// Expire old entries in the ID Log
	idLogExpire()
	// Clean the chunk DB
	chunkClean()
	// Delete old rejected mail
	expireRejected()
	// Complain about poor configs
	nagOperator()
	// Run a key purge
	if purgeSecring(secret) == 0 {
		// If there are zero active keys, generate a new one.
		generateKeypair(secret)
	} else {
		/*
			If the operator changes his configuration, (such as
			upgrading to a new version or switching from exit to
			middleman), the published key will not match the
			configuration.  This element of code writes a new
			key.txt file with current settings.  This only needs to
			be done if we haven't generated a new key.
		*/
		refreshPubkey(secret)
	}

	log.Infof("Secret keyring contains %d keys", secret.Count())
	if flag.MemInfo {
		logMemInfo()
	}

	// Define triggers for timed events
	daily := time.Now()
	hourly := time.Now()
	dayOfMonth := time.Now().Day()
	oneDay := time.Duration(dayLength) * time.Second

	// Determine if this is a single run or the start of a Daemon
	runAsDaemon := cfg.Remailer.Daemon || flag.Daemon

	// Actually start the server loop
	if runAsDaemon {
		log.Infof("Starting YAMN server: %s", cfg.Remailer.Name)
		log.Infof("Detaching Pool processing")
		go serverPoolOutboundSend()
	} else {
		log.Infof("Performing routine remailer functions for: %s",
			cfg.Remailer.Name)
	}
	for {
		// Panic if the pooldir doesn't exist
		assertIsPath(cfg.Files.Pooldir)
		// Process the inbound Pool
		processInpool("i", secret)
		// Process the Maildir
		processMail(secret)

		// Midnight events
		if time.Now().Day() != dayOfMonth {
			log.Info("Performing midnight events")
			// Remove expired keys from memory and rewrite a
			// secring file without expired keys.
			if purgeSecring(secret) == 0 {
				generateKeypair(secret)
			}
			// Expire entries in the ID Log
			idLogExpire()
			// Expire entries in the chunker
			chunkClean()
			// Delete old rejected mail
			expireRejected()
			// Report daily throughput and reset to zeros
			stats.reportAndReset()
			// Reset dayOfMonth to today
			dayOfMonth = time.Now().Day()
		}
		// Daily events
		if time.Since(daily) > oneDay {
			log.Info("Performing daily events")
			// Complain about poor configs
			nagOperator()
			// Reset today so we don't do these tasks for the next
			// 24 hours.
			daily = time.Now()
		}
		// Hourly events
		if time.Since(hourly) > time.Hour {
			log.Trace("Performing hourly events")
			/*
				The following two conditions try to import new
				pubring and mlist2 URLs.  If they fail, a
				warning is logged but no further action is
				taken.  It's better to have old keys/stats than
				none.
			*/
			// Retrieve Mlist2 and Pubring URLs
			refreshKeyring(false)
			// Test to see if the pubring.mix file has been updated
			if Pubring.KeyRefresh() {
				log.Tracef(
					"Reimporting Public Keyring: %s",
					cfg.Files.Pubring,
				)
				Pubring.ImportPubring()
			}
			// Report throughput
			stats.report()
			if flag.MemInfo {
				logMemInfo()
			}
			hourly = time.Now()
		}

		// Break out of the loop if we're not running as a daemon
		if !runAsDaemon {
			break
		}

		// And rest a while
		time.Sleep(60 * time.Second)
	} // End of server loop
	return
}

// refreshPubkey updates an existing Public key file
func refreshPubkey(secret *keymgr.Secring) {
	tmpKey := cfg.Files.Pubkey + ".tmp"
	keyidstr := secret.WriteMyKey(tmpKey)
	log.Infof("Advertising keyid: %s", keyidstr)
	log.Tracef("Writing current public key to %s", tmpKey)
	// Overwrite the published key with the refreshed version
	log.Tracef("Renaming %s to %s", tmpKey, cfg.Files.Pubkey)
	err := os.Rename(tmpKey, cfg.Files.Pubkey)
	if err != nil {
		log.Warn(err)
	}
}

// purgeSecring deletes old keys and counts active ones.  If no active keys
// are found, it triggers a generation.
func purgeSecring(secret *keymgr.Secring) (active int) {
	active, expiring, expired, purged := secret.Purge()
	log.Infof(
		"Key purge complete. Active=%d, Expiring=%d, Expired=%d, "+
			"Purged=%d",
		active,
		expiring,
		expired,
		purged,
	)
	return
}

// generateKeypair creates a new keypair and publishes it
func generateKeypair(secret *keymgr.Secring) {
	log.Info("Generating and advertising a new key pair")
	pub, sec := eccGenerate()
	keyidstr := secret.Insert(pub, sec)
	log.Infof("Generated new keypair with keyid: %s", keyidstr)
	log.Info("Writing new Public Key to disc")
	secret.WritePublic(pub, keyidstr)
	log.Info("Inserting Secret Key into Secring")
	secret.WriteSecret(keyidstr)
}

// idLogExpire deletes old entries in the ID Log
func idLogExpire() {
	count, deleted := IDDb.Expire()
	log.Infof("ID Log: Expired=%d, Contains=%d", deleted, count)
}

// chunkClean expires entries from the chunk DB and deletes any stranded files
func chunkClean() {
	cret, cexp := ChunkDb.Expire()
	if cexp > 0 {
		log.Infof(
			"Chunk expiry complete. Retained=%d, Expired=%d\n",
			cret,
			cexp,
		)
	}
	cleanPoolTemp()
	fret, fdel := ChunkDb.Housekeep()
	if fdel > 0 {
		log.Infof(
			"Stranded chunk deletion: Retained=%d, Deleted=%d",
			fret,
			fdel,
		)
	}
}

// nagOperator prompts a remailer operator about poor practices.
func nagOperator() {
	if cfg.Mail.MessageDomain != "" && !validMessageDomain(cfg.Mail.MessageDomain) {
		log.Warnf(
			"mail.message_domain %q isn't a valid domain and is ignored. "+
				"Message-IDs use the domain of remailer.address.",
			cfg.Mail.MessageDomain,
		)
	}
	if pipeReadsHeaders(cfg.Mail.Pipe) {
		log.Warnf(
			"mail.pipe %q reads the recipients from the headers (-t), "+
				"so yamn can't pass them: the block list doesn't apply "+
				"and Bcc recipients get nothing. Remove the -t; yamn "+
				"then passes the recipients as arguments.",
			cfg.Mail.Pipe,
		)
	}
	// Complain about excessively small loop values.
	if cfg.Pool.Loop < 60 {
		log.Warnf(
			"Loop time of %d is excessively low. Will loop "+
				"every 60 seconds. A higher setting is recommended.",
			cfg.Pool.Loop,
		)
	}
	// Complain about high pool rates.
	if cfg.Pool.Rate > 90 && !flag.Send {
		log.Warnf(
			"Your pool rate of %d is excessively high. Unless "+
				"testing, a lower setting is recommended.",
			cfg.Pool.Rate,
		)
	}
	// Complain about running a remailer with flag_send
	if flag.Send && flag.Remailer {
		log.Warnf(
			"Your remailer will flush the outbound pool every "+
				"%d seconds. Unless you're testing, this is "+
				"probably not what you want.",
			cfg.Pool.Loop,
		)
	}
}

func createDirs() {
	var err error
	err = os.MkdirAll(cfg.Files.IDlog, 0700)
	if err != nil {
		log.Errorf(
			"Failed to create %s. %s",
			cfg.Files.IDlog,
			err,
		)
		os.Exit(1)
	}
	err = os.MkdirAll(cfg.Files.Pooldir, 0700)
	if err != nil {
		log.Errorf(
			"Failed to create %s. %s",
			cfg.Files.Pooldir,
			err,
		)
		os.Exit(1)
	}
	err = os.MkdirAll(cfg.Files.ChunkDB, 0700)
	if err != nil {
		log.Errorf(
			"Failed to create %s. %s",
			cfg.Files.ChunkDB,
			err,
		)
		os.Exit(1)
	}
	err = os.MkdirAll(cfg.Files.Maildir, 0700)
	if err != nil {
		log.Errorf(
			"Failed to create %s. %s",
			cfg.Files.Maildir,
			err,
		)
		os.Exit(1)
	}
	mdirnew := path.Join(cfg.Files.Maildir, "new")
	err = os.MkdirAll(mdirnew, 0700)
	if err != nil {
		log.Errorf("Failed to create %s. %s", mdirnew, err)
		os.Exit(1)
	}
	mdircur := path.Join(cfg.Files.Maildir, "cur")
	err = os.MkdirAll(mdircur, 0700)
	if err != nil {
		log.Errorf("Failed to create %s. %s", mdircur, err)
		os.Exit(1)
	}
	mdirtmp := path.Join(cfg.Files.Maildir, "tmp")
	err = os.MkdirAll(mdirtmp, 0700)
	if err != nil {
		log.Errorf("Failed to create %s. %s", mdirtmp, err)
		os.Exit(1)
	}
}

// checkNextHop verifies the next-hop address from an intermediate header.  It
// is set by the sender, so it must be a remailer in our public keyring and
// must not contain characters that could inject mail headers.
func checkNextHop(addr string) error {
	if addr == "" {
		return errors.New("next hop is empty")
	}
	for _, r := range addr {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("next hop contains control characters: %q", addr)
		}
	}
	if !strings.Contains(addr, "@") {
		return fmt.Errorf("next hop is not an email address: %q", addr)
	}
	if _, err := Pubring.Get(addr); err != nil {
		return fmt.Errorf("next hop is not a known remailer: %s", addr)
	}
	return nil
}

// errDuplicatePacket is returned for a packet whose ID was already seen.
// That's usually another copy of the same message (clients send 2 copies by
// default and share the packet ID so it's delivered once); it can't be told
// apart from a replay, and both are simply dropped.
var errDuplicatePacket = errors.New("packet ID collision")

// replayLogDays returns how many days packet IDs are kept in the ID log.  A
// packet is accepted until it's max_age days old, so its ID must be kept at
// least max_age+1 days (it may arrive at the very start of its stamp day).
// Shorter settings would let a packet be replayed after its ID has expired,
// so they're raised to max_age+2, a day of margin.
func replayLogDays() int {
	minimum := cfg.Remailer.MaxAge + 1
	if cfg.Remailer.IDexp >= minimum {
		return cfg.Remailer.IDexp
	}
	days := cfg.Remailer.MaxAge + 2
	log.Warnf(
		"remailer.id_expire (%d days) must be at least remailer.max_age+1 (%d) "+
			"to prevent replays. Using %d days.",
		cfg.Remailer.IDexp,
		minimum,
		days,
	)
	return days
}

// decodeSafely runs decodeMsg and turns a panic into an error.  Received
// messages come from untrusted senders, so a bug triggered by one message
// must drop that message instead of stopping the remailer.
func decodeSafely(rawMsg []byte, secret *keymgr.Secring) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Recovered from panic while decoding message: %v\n%s", r, debug.Stack())
			err = fmt.Errorf("message decode panicked: %v", r)
		}
	}()
	return decodeMsg(rawMsg, secret)
}

// decodeMsg is the actual YAMN message decoder.  It's output is always a
// pooled file, either in the Inbound or Outbound queue.
func decodeMsg(rawMsg []byte, secret *keymgr.Secring) (err error) {

	// At this point, rawMsg should always be messageBytes in length
	err = lenCheck(len(rawMsg), messageBytes)
	if err != nil {
		log.Error(err)
		return
	}

	d := newDecMessage(rawMsg)
	// Extract the top header
	header := newDecodeHeader(d.getHeader())
	recipientKeyID := header.getRecipientKeyID()
	recipientSK, err := secret.GetSK(recipientKeyID)
	if err != nil {
		log.Warnf("Failed to ascertain Recipient SK: %s", err)
		return
	}
	header.setRecipientSK(recipientSK)

	slotDataBytes, packetVersion, err := header.decode()
	if err != nil {
		log.Warnf("Header decode failed: %s", err)
		return
	}
	switch packetVersion {
	case 2:
		err = decodeV2(d, slotDataBytes)
	default:
		err = fmt.Errorf("cannot decode packet version %d", packetVersion)
	}
	return
}

func decodeV2(d *decMessage, slotDataBytes []byte) (err error) {
	// Convert the raw Slot Data Bytes to meaningful slotData.
	slotData := decodeSlotData(slotDataBytes)
	if !d.testAntiTag(slotData.getTagHash()) {
		log.Warn("Anti-tag digest mismatch")
		return
	}
	if slotData.ageTimestamp() > cfg.Remailer.MaxAge {
		log.Warnf(
			"Max packet age in days exceeded. Age=%d, Max=%d",
			slotData.ageTimestamp(),
			cfg.Remailer.MaxAge,
		)
		return
	}
	if slotData.ageTimestamp() < 0 {
		log.Warn("Packet timestamp is in the future. Rejecting")
		return
	}
	// Test uniqueness of packet ID.  This runs after the checks above, so a
	// tampered or expired copy can't record the ID and get the genuine
	// packet rejected as a replay.
	if !IDDb.Unique(slotData.getPacketID()) {
		err = errDuplicatePacket
		return
	}
	defer func() {
		// A local write failure isn't the packet's fault.  Forget its ID,
		// so the copy that's kept can be processed again.
		if errors.Is(err, errPoolWrite) {
			IDDb.Forget(slotData.getPacketID())
		}
	}()
	if slotData.getPacketType() == 0 {
		d.shiftHeaders()
		// Decode Intermediate
		inter := decodeIntermediate(slotData.packetInfo)
		d.decryptAll(slotData.aesKey, inter.aesIV12)
		/*
			The following conditional tests if we are the next hop
			in addition to being the current hop.  If we are, then
			it's better to store the message in the inbound pool.
			This prevents it being emailed back to us.
		*/
		if inter.getNextHop() == cfg.Remailer.Address {
			log.Info(
				"Message loops back to us.",
				"Storing in pool instead of sending it.")
			if _, err = writePoolFile("i", d.getPayload()); err != nil {
				log.Warnf("Failed to write to pool: %s", err)
				return
			}
			stats.outLoop.Add(1)
		} else {
			if err = checkNextHop(inter.getNextHop()); err != nil {
				err = fmt.Errorf("refusing to forward: %w", err)
				return
			}
			if err = writeMessageToPool(inter.getNextHop(), d.getPayload()); err != nil {
				return
			}
			stats.outYamn.Add(1)
			// Decide if we want to inject a dummy
			if !flag.NoDummy && crandom.Dice() < 55 {
				dummy()
				stats.outDummy.Add(1)
			}
		} // End of local or remote delivery
	} else if slotData.getPacketType() == 1 {
		// Decode Exit
		final := decodeFinal(slotData.packetInfo)
		if err = final.validate(); err != nil {
			err = fmt.Errorf("invalid exit header: %w", err)
			return
		}
		if final.getDeliveryMethod() == 255 {
			log.Trace("Discarding dummy message")
			stats.inDummy.Add(1)
			return
		}
		// Decrypt the payload body
		// This could be done under Delivery Method 0 but, future
		// delivery methods (other than dummies) will require a
		// decrypted body.
		plain := d.decryptBody(
			slotData.getAesKey(),
			final.getAesIV(),
			final.getBodyBytes(),
		)
		// Test delivery methods
		switch final.getDeliveryMethod() {
		case 0:
			stats.inYamn.Add(1)
			if !cfg.Remailer.Exit {
				if final.numChunks == 1 {
					// Need to randhop as we're not an exit
					// remailer
					err = randhop(plain)
				} else {
					log.Warn(
						"Randhopping doesn't support " +
							"multi-chunk messages. As " +
							"per Mixmaster, this " +
							"message will be dropped.",
					)
				}
				return
			}
			err = smtpMethod(plain, final)
		default:
			log.Warnf(
				"Unsupported Delivery Method: %d",
				final.getDeliveryMethod(),
			)
			return
		}
	} else {
		log.Warnf(
			"Unknown Packet Type: %d",
			slotData.getPacketType(),
		)
		return
	}
	return
}

// smtpMethod is concerned with final-hop processing.
func smtpMethod(plain []byte, final *slotFinal) (err error) {
	// remailer.max_size limits the size of delivered messages (0 = none).
	limit := cfg.Remailer.MaxSize * 1024
	if final.getNumChunks() == 1 {
		if limit > 0 && len(plain) > limit {
			err = fmt.Errorf("message of %d bytes exceeds max_size of %d kB", len(plain), cfg.Remailer.MaxSize)
			return
		}
		// If this is a single chunk message, pool it and get out.
		if _, err = writePlainToPool(plain, "m"); err != nil {
			return
		}
		stats.outPlain.Add(1)
		return
	}
	// Every chunk but the last is maxFragLength bytes, so a message whose
	// other chunks already exceed the limit can be rejected now, before its
	// chunks fill the pool.  The exact size is checked when it's assembled.
	if limit > 0 && (final.getNumChunks()-1)*maxFragLength >= limit {
		err = fmt.Errorf("message of %d chunks exceeds max_size of %d kB", final.getNumChunks(), cfg.Remailer.MaxSize)
		return
	}
	// Fetch the chunks info from the DB for the given message ID
	chunks := ChunkDb.Get(final.getMessageID(), final.getNumChunks())
	// A stored record from an earlier chunk must agree with this header,
	// otherwise the chunk number below could index outside it.
	if len(chunks) != final.getNumChunks() {
		err = fmt.Errorf(
			"chunk count mismatch for MsgID %x: header says %d, stored record has %d",
			final.getMessageID(),
			final.getNumChunks(),
			len(chunks),
		)
		return
	}
	// We're an exit and this is a multi-chunk message
	// This saves losts of -1's as slices start at 0 and chunks at 1
	cslot := final.getChunkNum() - 1
	// Keep the first copy of a chunk.  Storing another one would orphan
	// the first file and extend the record's life.
	if chunks[cslot] != "" {
		log.Infof(
			"Duplicate chunk %d in MsgID %x, keeping the first copy",
			final.getChunkNum(),
			final.getMessageID(),
		)
		if !IsPopulated(chunks) {
			return
		}
		// The record is complete, so an earlier assembly failed.
		// Try again below.
	} else {
		// Limit the space taken by messages that never complete.  A
		// chunk that completes its message is always accepted, as
		// assembling it frees space.
		if cfg.Remailer.ChunkLimit > 0 && countEmpty(chunks) > 1 {
			if n := ChunkDb.Count(); n >= cfg.Remailer.ChunkLimit {
				err = fmt.Errorf("refusing chunk: %d partial chunks stored, chunk_limit is %d", n, cfg.Remailer.ChunkLimit)
				return
			}
		}
		var chunkFilename string
		if chunkFilename, err = writePlainToPool(plain, "p"); err != nil {
			return
		}
		log.Tracef(
			"Pooled partial chunk. MsgID=%x, Num=%d, "+
				"Parts=%d, Filename=%s",
			final.getMessageID(),
			final.getChunkNum(),
			final.getNumChunks(),
			chunkFilename,
		)
		// Insert the new chunk into the slice
		chunks[cslot] = chunkFilename
	}
	log.Tracef(
		"Chunk state: %s",
		strings.Join(chunks, ","),
	)
	// Test if all chunk slots are populated
	if IsPopulated(chunks) {
		if size := ChunkDb.AssembledSize(chunks); limit > 0 && size > limit {
			ChunkDb.DeleteItems(chunks)
			ChunkDb.Delete(final.getMessageID())
			err = fmt.Errorf("assembled message of %d bytes exceeds max_size of %d kB", size, cfg.Remailer.MaxSize)
			return
		}
		var newPoolFile string
		newPoolFile, err = ChunkDb.Assemble(chunks)
		if err != nil {
			// Keep the record and its chunks.  After a write error
			// the packet is processed again; otherwise they expire
			// after chunk_expire days.
			ChunkDb.Insert(final.getMessageID(), chunks)
			err = fmt.Errorf("chunk assembly failed: %w", err)
			return
		}
		log.Tracef("Assembled chunked message into %s", newPoolFile)
		// Now the message is assembled into the Pool, the DB record
		// can be deleted
		ChunkDb.Delete(final.getMessageID())
		stats.outPlain.Add(1)
	} else {
		// Write the updated chunk status to
		// the DB
		ChunkDb.Insert(final.getMessageID(), chunks)
	}
	return
}

// randhop is a simplified client function that does single-hop encodings.
// Only a failure to pool the result is returned; other problems drop the
// message.
func randhop(plainMsg []byte) error {
	if len(plainMsg) == 0 {
		log.Info("Zero-byte message during randhop, ignoring it.")
		return nil
	}
	// Make a single hop chain with a random node
	inChain := []string{"*"}
	final := newSlotFinal()
	chain, err := makeChain(inChain)
	if err != nil {
		log.Warn(err)
		return nil
	}
	sendTo := chain[0]
	if len(chain) != 1 {
		err = fmt.Errorf("randhop chain must be single hop.  Got=%d", len(chain))
		panic(err)
	}
	log.Tracef("Performing a random hop to Exit Remailer: %s.", chain[0])
	yamnMsg := encodeMsg(plainMsg, chain, *final)
	if err = writeMessageToPool(sendTo, yamnMsg); err != nil {
		return err
	}
	stats.outRandhop.Add(1)
	return nil
}

// remailerFoo responds to requests for remailer-* info
func remailerFoo(subject, from string) (err error) {
	// Reply to the bare address: the From header usually includes a display
	// name ("Alice <alice@example.org>"), which isn't a valid SMTP recipient.
	fromAddr, err := mail.ParseAddress(from)
	if err != nil {
		err = fmt.Errorf("ignoring remailer-* request with invalid From %q: %w", from, err)
		return
	}
	sender := fromAddr.Address
	if !validRecipient(sender) {
		err = errors.New("ignoring remailer-* request from an address that can't be replied to safely")
		return
	}
	// The From header can be forged, so reply at most once a day per address.
	if !fooReplies.allowed(sender) {
		err = fmt.Errorf("already replied to %s today, ignoring %s", sender, subject)
		return
	}
	// Addresses on the destination block list get no replies either.  If
	// the list can't be read, don't reply.
	blockList, err := readDestBlock(cfg.Files.DestBlock)
	if err != nil {
		err = fmt.Errorf("unable to read destination block list, ignoring %s: %w", subject, err)
		return
	}
	if destBlocked(blockList, sender) {
		err = fmt.Errorf("%s is on the destination block list, ignoring %s", sender, subject)
		return
	}
	m := quickmail.NewMessage()
	m.Set("From", cfg.Remailer.Address)
	m.Set("To", sender)
	m.Set("Date", time.Now().Format(rfc5322date))
	m.Set("Message-ID", messageID())
	m.Set("MIME-Version", "1.0")
	m.Set("Content-Type", "text/plain; charset=UTF-8")
	if strings.HasPrefix(subject, "remailer-key") {
		// remailer-key
		log.Tracef("remailer-key request from %s", sender)
		m.Set("Subject", fmt.Sprintf("Remailer key for %s", cfg.Remailer.Name))
		m.Filename = cfg.Files.Pubkey
		m.Prefix = "Here is the Mixmaster key:\n\n=-=-=-=-=-=-=-=-=-=-=-="
	} else if strings.HasPrefix(subject, "remailer-conf") {
		// remailer-conf
		log.Tracef("remailer-conf request from %s", sender)
		m.Set(
			"Subject",
			fmt.Sprintf("Capabilities of the %s remailer", cfg.Remailer.Name))
		m.Text(fmt.Sprintf("Remailer-Type: Mixmaster %s\n", version))
		m.Text("Supported Formats:\n   Mixmaster\n")
		m.Text(fmt.Sprintf("Pool size: %d\n", cfg.Pool.Size))
		if cfg.Remailer.MaxSize > 0 {
			m.Text(fmt.Sprintf("Maximum message size: %d kB\n", cfg.Remailer.MaxSize))
		} else {
			m.Text("Maximum message size: unlimited\n")
		}
		if cfg.Mail.MaxRecipients > 0 {
			m.Text(fmt.Sprintf("Maximum recipients: %d\n", cfg.Mail.MaxRecipients))
		} else {
			m.Text("Maximum recipients: unlimited\n")
		}
		m.Text("The following header lines will be filtered:\n")
		m.Text(
			fmt.Sprintf("\n$remailer{\"%s\"} = \"<%s>",
				cfg.Remailer.Name, cfg.Remailer.Address))
		if !cfg.Remailer.Exit {
			m.Text(" middle")
		}
		packetVersions := []string{"v2"}
		for _, v := range packetVersions {
			m.Text(fmt.Sprintf(" %s", v))
		}
		m.Text("\";\n")
		m.Text("\nSUPPORTED MIXMASTER (TYPE II) REMAILERS")
		var pubList []string
		pubList, err := keymgr.Headers(cfg.Files.Pubring)
		if err != nil {
			log.Infof("Could not read %s", cfg.Files.Pubring)
		} else {
			m.List(pubList)
		}
	} else if strings.HasPrefix(subject, "remailer-adminkey") {
		// remailer-adminkey
		log.Tracef("remailer-adminkey request from %s", sender)
		m.Set(
			"Subject",
			fmt.Sprintf("Admin key for the %s remailer", cfg.Remailer.Name))
		m.Filename = cfg.Files.Adminkey
	} else if strings.HasPrefix(subject, "remailer-help") {
		// remailer-help
		log.Tracef("remailer-help request from %s", sender)
		m.Set(
			"Subject",
			fmt.Sprintf("Your help request for the %s Anonymous Remailer",
				cfg.Remailer.Name))
		m.Filename = cfg.Files.Help
	} else {
		if len(subject) > 20 {
			// Truncate long subject headers before logging them
			subject = subject[:20]
		}
		err = fmt.Errorf("ignoring request for %s", subject)
		return
	}
	var msg []byte
	msg, err = m.Compile()
	if err != nil {
		log.Infof("Unable to send %s", subject)
		return
	}
	err = mailBytes(msg, []string{sender})
	if err != nil {
		log.Warnf("Failed to send %s to %s", subject, sender)
		return
	}
	// Only count replies that were sent, so a failed send can be retried.
	fooReplies.record(sender)
	return
}
