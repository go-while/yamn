// vim: tabstop=2 shiftwidth=2

package main

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/mail"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/Masterminds/log-go"
	"github.com/crooks/yamn/crandom"
	"github.com/crooks/yamn/keymgr"
	//"github.com/codahale/blake2"
)

// clientOutput receives notices for the user of the client.  Tests replace
// it.
var clientOutput io.Writer = os.Stderr

// readMessageFile reads the plaintext message to be sent from a file.
func readMessageFile(filename string) []byte {
	f, err := os.Open(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: Unable to open file\n", filename)
		os.Exit(1)
	}
	defer f.Close()
	return readMessage(f, filename)
}

// maxChunks is the most chunks a message can have: the packet stores the
// number of chunks in a single byte.
const maxChunks = 255

// maxMessageRead is the most bytes readMessage reads.  It's well above the
// largest message that fits into maxChunks chunks, as headers can still be
// removed, and only keeps yamn from reading an endless input.
var maxMessageRead int64 = 2 * maxChunks * maxFragLength

// readMessage reads the plaintext message to be sent from r (named name in
// error messages), applies --to and --subject, and removes headers that
// could identify the sender unless --keep-headers is given.
func readMessage(r io.Reader, name string) []byte {
	limited := &io.LimitedReader{R: r, N: maxMessageRead + 1}
	msg, err := mail.ReadMessage(limited)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: Malformed mail message\n", name)
		os.Exit(1)
	}
	if flag.To != "" {
		msg.Header["To"] = []string{flag.To}
		if !strings.Contains(flag.To, "@") {
			fmt.Fprintf(
				os.Stderr,
				"%s: Recipient doesn't appear to be an "+
					"email address\n",
				flag.To,
			)
		}
	}
	if flag.Subject != "" {
		msg.Header["Subject"] = []string{flag.Subject}
	}
	// The exit only sends to To, Cc and Bcc addresses and drops a message
	// without any, so it isn't sent at all.
	if !hasRecipient(msg.Header) {
		fmt.Fprintf(os.Stderr, "%s: No recipient: the message needs a To, Cc or Bcc address\n", name)
		os.Exit(1)
	}
	if !flag.KeepHeaders {
		if removed := stripClientHeaders(msg.Header); len(removed) > 0 {
			fmt.Fprintf(
				clientOutput,
				"Removed from the message, as it could identify you: %s. "+
					"Use --keep-headers to send them.\n",
				strings.Join(removed, ", "),
			)
		}
	}
	plain := assemble(*msg)
	if limited.N <= 0 {
		fmt.Fprintf(os.Stderr, "%s: Message larger than %d bytes\n", name, maxMessageRead)
		os.Exit(1)
	}
	return plain
}

// hasRecipient reports whether h has an address in To, Cc or Bcc, the
// headers an exit sends to.
func hasRecipient(h mail.Header) bool {
	for _, k := range []string{"To", "Cc", "Bcc"} {
		if len(headToAddy(h, k)) > 0 {
			return true
		}
	}
	return false
}

// stripClientHeaders removes headers that could identify the sender before a
// message is encoded, and returns what it removed.  It keeps the headers an
// exit passes on (see headerAllowed) plus Bcc, which the exit needs to find
// the hidden recipients.  Date and Message-Id are removed: the exit sets its
// own, and they can show the sender's time zone and host name.  From is kept
// as a bare address, without the display name.  Headers with a CR or NUL in
// them are removed too, as an exit refuses the message.
func stripClientHeaders(h mail.Header) (removed []string) {
	for k := badHeader(h); k != ""; k = badHeader(h) {
		delete(h, k)
		removed = append(removed, k+" (CR or NUL)")
	}
	extra := extraAllowedHeaders()
	for k := range h {
		keep := k == "Bcc" || (headerAllowed(k, extra) && k != "Date" && k != "Message-Id")
		if !keep {
			delete(h, k)
			removed = append(removed, k)
		}
	}
	if from := h.Get("From"); from != "" {
		addr, err := mail.ParseAddress(from)
		switch {
		case err != nil:
			delete(h, "From")
			removed = append(removed, "From (unreadable)")
		case addr.Name != "":
			h["From"] = []string{addr.Address}
			removed = append(removed, "From display name")
		}
	}
	slices.Sort(removed)
	return
}

// mixprep fetches the plaintext and prepares it for mix encoding
func mixprep() {
	var err error
	err = os.MkdirAll(cfg.Files.Pooldir, 0700)
	if err != nil {
		panic(err)
	}
	// plain will contain the byte version of the plain text message
	var plain []byte
	// final is consistent across multiple copies so we define it early
	final := newSlotFinal()
	if len(flag.Args) == 0 {
		//fmt.Println("Enter message, complete with headers.  Ctrl-D to finish")
		plain = readMessage(os.Stdin, "stdin")
	} else if len(flag.Args) == 1 {
		// A single arg should be the filename
		plain = readMessageFile(flag.Args[0])
	} else if len(flag.Args) == 2 {
		// Two args should be recipient and filename
		flag.To = flag.Args[0]
		plain = readMessageFile(flag.Args[1])
	} else {
		fmt.Fprintf(os.Stderr, "Too many arguments: %q (expected [recipient] [filename])\n", flag.Args)
		os.Exit(1)
	}
	// plainLen is the length of the plain byte message and can exceed
	// the total body size of the payload.
	plainLen := len(plain)
	if plainLen == 0 {
		fmt.Fprintln(os.Stderr, "No bytes in message")
		os.Exit(1)
	}

	// Download the keyring and stats if the time is right
	refreshKeyring(false)

	// Create the Public Keyring
	Pubring = keymgr.NewPubring(
		cfg.Files.Pubring,
		cfg.Files.Mlist2,
	)
	// Set the Use Expired flag to include remailers with expired keys as
	// candidates.
	if cfg.Stats.UseExpired {
		Pubring.UseExpired()
	}
	err = Pubring.ImportPubring()
	if err != nil {
		log.Errorf("Pubring import failed: %s: %s", cfg.Files.Pubring, err)
		os.Exit(1)
	}
	// Read the chain from flag or config
	chainText := cfg.Stats.Chain
	if flag.Chain != "" {
		chainText = flag.Chain
	}
	inChain, err := parseChain(chainText)
	if err != nil {
		log.Error(err)
		os.Exit(1)
	}
	var inChainFunc []string
	copies := numCopies()
	var cnum int // Chunk number
	numc := int(math.Ceil(float64(plainLen) / float64(maxFragLength)))
	// Checked before anything is pooled: a part of the message must not be
	// sent.
	if numc > maxChunks {
		fmt.Fprintf(
			os.Stderr,
			"Message too large: %d bytes need %d chunks, at most %d (%d bytes) are possible\n",
			plainLen, numc, maxChunks, maxChunks*maxFragLength,
		)
		os.Exit(1)
	}
	final.setNumChunks(numc)
	// The exit is chosen once for the whole message: every chunk and copy
	// must reach the same exit remailer for the chunks to be reassembled.
	var exitnode string // Address of exit node (for multiple copy chains)
	var gotExit bool    // Flag to indicate an exit node has been selected
	var firstByte int   // First byte of message slice
	var lastByte int    // Last byte of message slice
	// All chunks and copies are encoded before the first is pooled, so a
	// failure can't leave a part of the message to be sent.
	var encoded []encodedMail
	// Fragments loop begins here
	for cnum = 1; cnum <= numc; cnum++ {
		final.setChunkNum(cnum)
		final.newPacketID()
		// First byte of message fragment
		firstByte = (cnum - 1) * maxFragLength
		lastByte = firstByte + maxFragLength
		// Don't slice beyond the end of the message
		if lastByte > plainLen {
			lastByte = plainLen
		}
		// Copies loop begins here
		for n := 0; n < copies; n++ {
			if gotExit {
				// Set the last node in the chain to the
				// previously select exitnode
				inChain[len(inChain)-1] = exitnode
			}
			var chain []string
			inChainFunc = append(inChain[:0:0], inChain...)
			chain, err = makeChain(inChainFunc)
			if err != nil {
				log.Error(err)
				os.Exit(1)
			}
			if len(chain) != len(inChain) {
				err = fmt.Errorf("chain length mismatch: in=%d, out=%d", len(inChain), len(chain))
				panic(err)
			}
			//fmt.Println(chain)
			if !gotExit {
				exitnode = chain[len(chain)-1]
				gotExit = true
			}
			// Retain the entry hop.  We need to mail the message to it.
			sendTo := chain[0]
			// Report the chain if we're running as a client, at debug
			// level only: a log file shouldn't show who was mailed how.
			if flag.Client {
				log.Debugf("Chain: %s", strings.Join(chain, ","))
			}
			yamnMsg, err := encodeMsg(
				plain[firstByte:lastByte],
				chain,
				*final,
			)
			if err != nil {
				log.Error(err)
				os.Exit(1)
			}
			encoded = append(encoded, encodedMail{sendTo, yamnMsg})
		} // End of copies loop
	} // End of fragments loop
	for _, m := range encoded {
		if err = outputMessage(m.sendTo, m.msg); err != nil {
			log.Error(err)
			os.Exit(1)
		}
	}

	// Decide if we want to inject a dummy.  Not with --stdout: dummies are
	// sent from the pool, and nothing else from this message is.
	if !flag.NoDummy && !flag.Stdout && Pubring.HaveStats() && crandom.Dice() < 80 {
		dummy()
	}
}

// encodedMail is an encrypted packet and the first hop to send it to.
type encodedMail struct {
	sendTo string
	msg    []byte
}

// parseChain splits a chain given as "rem1,rem2,..." into its hops.  Spaces
// around a hop are ignored, and an empty hop is an error.
func parseChain(s string) ([]string, error) {
	hops := strings.Split(s, ",")
	for i, hop := range hops {
		hops[i] = strings.TrimSpace(hop)
		if hops[i] == "" {
			return nil, fmt.Errorf("chain %q: empty hop", s)
		}
	}
	return hops, nil
}

// mailOutput receives the encrypted mails with --stdout.  Tests replace it.
var mailOutput io.Writer = os.Stdout

// outputMessage hands an encrypted mail for its first hop to the pool, or
// with --stdout prints it as an mbox entry instead.
func outputMessage(sendTo string, yamnMsg []byte) error {
	if !flag.Stdout {
		return writeMessageToPool(sendTo, yamnMsg)
	}
	mail := new(bytes.Buffer)
	if err := writeYamnMail(mail, sendTo, yamnMsg); err != nil {
		return err
	}
	return writeMbox(mailOutput, mail.Bytes())
}

// numCopies returns how many copies of each chunk to send: the --copies flag
// if it was given, otherwise stats.num_copies from the config, limited to
// 1-maxCopies.
func numCopies() int {
	copies := flag.Copies
	if copies <= 0 {
		copies = cfg.Stats.Numcopies
	}
	return max(1, min(copies, maxCopies))
}

// encodeMsg encodes a plaintext fragment into mixmaster format.  It fails if
// a hop isn't in the public keyring.
func encodeMsg(
	plain []byte,
	chain []string,
	final slotFinal) ([]byte, error) {

	var err error
	var hop string
	m := newEncMessage()
	m.setChainLength(len(chain))
	length := m.setPlainText(plain)
	// Pop the exit remailer address from the chain
	hop = popstr(&chain)
	// Insert the plain message length into the Final Hop header.

	final.setBodyBytes(length)
	slotData := newSlotData()
	// Identify this hop as Packet-Type 1 (Exit).
	slotData.setExit()
	// For exit hops, the AES key can be entirely random.
	slotData.setAesKey(crandom.Randbytes(32))
	// Override the random PacketID so that multi-copy messages all share a
	// common Exit PacketID.
	slotData.setPacketID(final.getPacketID())
	// Encode the (final) Packet Info and store it in the Slot Data.
	slotData.setPacketInfo(final.encode())
	// Get KeyID and NaCl PK for the remailer we're enrypting to.
	remailer, err := Pubring.Get(hop)
	if err != nil {
		return nil, err
	}
	// Create a new Header.
	header := newEncodeHeader()
	// Tell the header function what KeyID and PK to NaCl encrypt with.
	header.setRecipient(remailer.Keyid, remailer.PK)
	log.Tracef(
		"Encrypting Final Hop: Hop=%s, KeyID=%x",
		hop,
		remailer.Keyid,
	)
	// Only the body needs to be encrypted during Exit encoding.  At all other
	// hops, the entire header stack will also need encrypting.
	m.encryptBody(slotData.aesKey, final.aesIV)
	// Shift all the header down by headerBytes
	m.shiftHeaders()
	// We've already popped an entry from the Chain so were testing for
	// length greater than zero rather than 1.
	if len(chain) > 0 {
		// Single hop chains don't require deterministic headers.  All
		// longer chains do.
		m.deterministic(0)
	}
	// Set the Anti-tag hash in the slotData.
	slotData.setTagHash(m.getAntiTag())
	// Encode the slot data into Byte form.
	slotDataBytes := slotData.encode()
	// Encode the header and insert it into the payload.
	m.insertHeader(header.encode(slotDataBytes))

	// That concludes Exit hop compilation.  Now for intermediates.

	interHops := m.getIntermediateHops()
	for interHop := 0; interHop < interHops; interHop++ {
		inter := newSlotIntermediate()
		inter.setPartialIV(m.getPartialIV(interHop))
		// hop still contains the previous iteration (or exit) address.
		inter.setNextHop(hop)
		// Pop another remailer from the left side of the Chain
		hop = popstr(&chain)
		// Create new Slot Data
		slotData = newSlotData()
		slotData.setAesKey(m.getKey(interHop))
		slotData.setPacketInfo(inter.encode())
		m.encryptAll(interHop)
		m.shiftHeaders()
		m.deterministic(interHop + 1)
		slotData.setTagHash(m.getAntiTag())
		slotDataBytes = slotData.encode()
		header = newEncodeHeader()
		remailer, err := Pubring.Get(hop)
		if err != nil {
			return nil, err
		}
		header.setRecipient(remailer.Keyid, remailer.PK)
		log.Tracef(
			"Encrypting: Hop=%s, KeyID=%x",
			hop,
			remailer.Keyid,
		)
		m.insertHeader(header.encode(slotDataBytes))
	}
	if len(chain) != 0 {
		panic("After encoding, chain was not empty.")
	}
	return m.getPayload(), nil
}

func injectDummy() {
	// Populate public keyring
	Pubring = keymgr.NewPubring(
		cfg.Files.Pubring,
		cfg.Files.Mlist2,
	)
	Pubring.ImportPubring()
	dummy()
}

// TimedURLFetch attempts to read a url into a file if the file is more
// than an hour old or doesn't exist.
func timedURLFetch(url, filename string, validate func([]byte) error) {
	var err error
	var stamp time.Time
	var doFetch bool
	if cfg.Urls.Fetch {
		stamp, err = fileTime(filename)
		if err != nil {
			doFetch = true
		} else if age := time.Since(stamp); age > time.Hour || age < -time.Minute {
			// A file dated in the future, e.g. unpacked from an archive,
			// would otherwise never be fetched again.
			doFetch = true
		} else {
			doFetch = false
		}
		if doFetch {
			log.Infof("Fetching %s and storing in %s", url, filename)
			err = httpGet(url, filename, validate)
			if err != nil {
				log.Warn(err)
			}
		}
	}
}

// dummy is a simplified client function that sends dummy messages
func dummy() {
	var err error
	plainMsg := []byte("I hope Len approves")
	// A random two-hop chain.  Not --chain: the dummy shouldn't take the
	// path of the real message.
	inChain := []string{"*", "*"}
	final := newSlotFinal()
	// Override the default delivery method (255 = Dummy)
	final.setDeliveryMethod(255)
	var chain []string
	chain, err = makeChain(inChain)
	if len(chain) == 0 {
		log.Warn("There are no known remailers. Unable to send dummy.")
		return
	}
	sendTo := chain[0]
	if err != nil {
		log.Warnf("Dummy creation failed: %s", err)
		return
	}
	log.Tracef("Sending dummy through: %s.", strings.Join(chain, ","))
	yamnMsg, err := encodeMsg(plainMsg, chain, *final)
	if err != nil {
		log.Warnf("Dummy creation failed: %s", err)
		return
	}
	if err = writeMessageToPool(sendTo, yamnMsg); err != nil {
		log.Warnf("Dummy creation failed: %s", err)
	}
}
