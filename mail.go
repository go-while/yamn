package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Masterminds/log-go"
)

func assemble(msg mail.Message) []byte {
	buf := new(bytes.Buffer)
	for h := range msg.Header {
		if strings.HasPrefix(h, "Yamn-") {
			log.Errorf("Ignoring internal mail header in assemble phase: %s", h)
			continue
		}
		// Keep every value of a header that occurs more than once.
		for _, v := range msg.Header[h] {
			buf.WriteString(h + ": " + v + "\n")
		}
	}
	buf.WriteString("\n")
	body, _ := io.ReadAll(msg.Body)
	buf.Write(fixBareCR(body))
	return buf.Bytes()
}

// fixBareCR turns every CR in b that isn't followed by LF into LF, in
// place.  A lone CR isn't a line end to Go's SMTP client, so "\r.\r" would
// reach the server unescaped, and some servers take that as the end of
// DATA.  As an LF it's sent as CRLF, and a leading dot gets escaped.
func fixBareCR(b []byte) []byte {
	for i, c := range b {
		if c == '\r' && (i+1 == len(b) || b[i+1] != '\n') {
			b[i] = '\n'
		}
	}
	return b
}

// badHeader returns the name of the first header in h with a CR or NUL in
// its value, or "" if there is none.  net/mail keeps a lone CR inside a
// value, and a receiver that takes it as a line end would see a header the
// exit never filtered (e.g. "Reply-To: x@example\rFrom: ...").
func badHeader(h mail.Header) string {
	for k, values := range h {
		for _, v := range values {
			if strings.ContainsAny(v, "\r\x00") {
				return k
			}
		}
	}
	return ""
}

// headToAddy parses a header containing email addresses.  Every occurrence
// of the header is read, not only the first.  Addresses that fail
// validRecipient are skipped.
func headToAddy(h mail.Header, header string) (addys []string) {
	for _, v := range h[header] {
		addyList, err := mail.ParseAddressList(v)
		if err != nil {
			log.Warnf("Failed to parse header: %s", header)
			continue
		}
		for _, addy := range addyList {
			if !validRecipient(addy.Address) {
				log.Warnf("Ignoring an invalid address in the %s header", header)
				continue
			}
			addys = append(addys, addy.Address)
		}
	}
	return
}

// validRecipient reports whether addy is a plain ASCII address that is safe
// to pass to RCPT TO and to match against the block list: one "@", a
// dot-atom local part and a host name.  mail.ParseAddress returns a quoted
// local part without its quotes, so it could carry "<", ">" or spaces (and
// with them ESMTP parameters) into the RCPT command.  Source routes ("%",
// "!"), address literals and a trailing dot are refused too, as they would
// get past the destination block list.
func validRecipient(addy string) bool {
	local, domain, ok := strings.Cut(addy, "@")
	if !ok || len(local) > 64 || len(addy) > 254 {
		return false
	}
	for _, atom := range strings.Split(local, ".") {
		if atom == "" || strings.IndexFunc(atom, func(c rune) bool { return !isAtext(c) }) >= 0 {
			return false
		}
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		if strings.IndexFunc(label, func(c rune) bool { return !isAlnum(c) && c != '-' }) >= 0 {
			return false
		}
	}
	return true
}

// isAlnum reports whether c is an ASCII letter or digit.
func isAlnum(c rune) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// isAtext reports whether c may appear in a dot-atom (RFC 5322 atext),
// leaving out "%" and "!", which some MTAs treat as routing.
func isAtext(c rune) bool {
	return isAlnum(c) || strings.ContainsRune("#$&'*+-/=?^_`{|}~", c)
}

// exitHeaders lists the headers passed through from a pooled message to the
// recipient, as canonical header keys.  All others are dropped, so headers
// that identify the sender (User-Agent, X-Mailer, ...) or forge the origin
// (Sender, Resent-*, ...) never reach the recipient.  Content-* headers are
// allowed too, and operators can allow more with mail.allow_headers.  From,
// Date and Message-Id are set by the remailer itself.
var exitHeaders = map[string]bool{
	"To":           true,
	"Cc":           true,
	"Subject":      true,
	"Reply-To":     true,
	"In-Reply-To":  true,
	"References":   true,
	"Newsgroups":   true,
	"Followup-To":  true,
	"Mime-Version": true,
	"X-No-Archive": true,
	"From":         true,
	"Date":         true,
	"Message-Id":   true,
}

// extraAllowedHeaders returns the headers allowed by mail.allow_headers, as
// canonical header keys.
func extraAllowedHeaders() map[string]bool {
	extra := make(map[string]bool)
	for _, k := range cfg.Mail.AllowHeaders {
		extra[textproto.CanonicalMIMEHeaderKey(k)] = true
	}
	return extra
}

// headerAllowed reports whether header key k may reach the recipient: it's
// on the built-in list, in extra (from extraAllowedHeaders) or a Content-*
// header.
func headerAllowed(k string, extra map[string]bool) bool {
	return exitHeaders[k] || extra[k] || strings.HasPrefix(k, "Content-")
}

// filterHeaders removes every header from h that isn't allowed to reach the
// recipient.
func filterHeaders(h mail.Header) {
	extra := extraAllowedHeaders()
	for k := range h {
		if headerAllowed(k, extra) {
			continue
		}
		log.Tracef("Dropping header: %s", k)
		delete(h, k)
	}
}

// readDestBlock reads the destination block list.  Each line holds a full
// address or "@domain"; blank lines and lines starting with "#" are ignored.
// Entries are lower case.  A missing file is an empty list.  See destBlocked
// for how they match.
func readDestBlock(filename string) (map[string]bool, error) {
	list := make(map[string]bool)
	content, err := os.ReadFile(filename)
	if os.IsNotExist(err) {
		return list, nil
	}
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.ToLower(strings.TrimSpace(line))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		list[line] = true
	}
	return list, nil
}

// destBlocked reports whether addy matches the block list: the full address,
// or "@domain" for its domain or any parent domain, so "@example.com" also
// blocks "sub.example.com".  Source routes ("%") and address literals never
// get here: headToAddy drops them (see validRecipient).
func destBlocked(list map[string]bool, addy string) bool {
	addy = strings.ToLower(addy)
	if list[addy] {
		return true
	}
	at := strings.LastIndex(addy, "@")
	if at < 0 {
		return false
	}
	for d := addy[at+1:]; d != ""; {
		if list["@"+d] {
			return true
		}
		i := strings.Index(d, ".")
		if i < 0 {
			break
		}
		d = d[i+1:]
	}
	return false
}

// uniqueStrings returns s without duplicates, keeping the first occurrence.
func uniqueStrings(s []string) (u []string) {
	seen := make(map[string]bool)
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			u = append(u, v)
		}
	}
	return
}

type emailAddress struct {
	name   string
	domain string
}

// splitAddress splits an email address into its component parts
func splitEmailAddress(addy string) (e emailAddress, err error) {
	// Email addresses must have '@' signs in them.
	if !strings.Contains(addy, "@") {
		err = fmt.Errorf("%s: Email address contains no '@'", addy)
		return
	}
	components := strings.Split(addy, "@")
	if len(components) != 2 {
		err = fmt.Errorf("%s: Malformed email address", addy)
		return
	}
	e.name = components[0]
	e.domain = components[1]
	return
}

// errPermanent marks a delivery failure that retrying won't fix: the
// recipient's server rejected the message with a 5xx reply, or the domain
// doesn't accept mail.  Such pool files are deleted instead of being retried
// until pool.max_age; there's nobody to bounce them to.
var errPermanent = errors.New("permanent delivery failure")

// isPermanentSMTP reports whether err is a 5xx SMTP reply.
func isPermanentSMTP(err error) bool {
	var tpErr *textproto.Error
	return errors.As(err, &tpErr) && tpErr.Code >= 500 && tpErr.Code <= 599
}

// permanentFor reports whether a 5xx reply means the message can never be
// delivered.  From the recipient's own mail server (direct MX) it does.
// From the configured relay, only an address error (enhanced status 5.1.x,
// e.g. "5.1.1 no such user") does.  Other 5xx replies from the relay, such
// as "5.7.1 relaying denied" or a missing login, usually mean the relay or
// yamn is misconfigured; dropping the mail would hide that, so it's kept
// and retried like before.
func permanentFor(err error, directMX bool) bool {
	if !isPermanentSMTP(err) {
		return false
	}
	if directMX {
		return true
	}
	var tpErr *textproto.Error
	if !errors.As(err, &tpErr) {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(tpErr.Msg), "5.1.")
}

// isNotFound reports whether err is a DNS lookup that found no records
// (NXDOMAIN, or no records of that type).
func isNotFound(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr) && dnsErr.IsNotFound
}

// isOnion reports whether host is a Tor .onion name, in any case and with or
// without the trailing dot.  Go's resolver doesn't look these up (RFC 7686)
// and yamn connects directly, so it can't deliver to them.
func isOnion(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "onion" || strings.HasSuffix(host, ".onion")
}

// DNS lookups for direct MX delivery.  Tests replace them.
var (
	lookupMX   = net.LookupMX
	lookupHost = net.LookupHost
)

// mxLookup returns the responsible MX for a given email address.  An error
// marked errPermanent means the domain doesn't accept mail; other errors
// (SERVFAIL, timeouts) are temporary.
func mxLookup(email string) (relay string, err error) {
	emailParts, err := splitEmailAddress(email)
	if err != nil {
		// Failed to ascertain domain name from email address
		err = fmt.Errorf("%w: %w", errPermanent, err)
		return
	}
	domain := emailParts.domain
	if isOnion(domain) {
		// The lookup would fail as if DNS were down, and the message
		// would be retried until pool.max_age.
		err = fmt.Errorf("%w: %s is a .onion domain, which can't be reached directly", errPermanent, domain)
		return
	}
	mxRecords, err := lookupMX(domain)
	if err != nil {
		if !isNotFound(err) {
			err = fmt.Errorf("MX lookup for %s failed: %w", domain, err)
			return
		}
		// No MX records: the domain's own address is its implicit MX
		// (RFC 5321, 5.1), if it has one.
		if _, err = lookupHost(domain); err != nil {
			if isNotFound(err) {
				err = fmt.Errorf("%w: %s has no MX or address records", errPermanent, domain)
			} else {
				err = fmt.Errorf("address lookup for %s failed: %w", domain, err)
			}
			return
		}
		log.Tracef("No MX records for %s. Using hostname.", domain)
		return domain, nil
	}
	for _, mx := range mxRecords {
		if mx.Host == "." {
			// A null MX (RFC 7505): the domain accepts no mail.
			continue
		}
		if isOnion(mx.Host) {
			// Unreachable whatever onion_relay says; find another.
			continue
		}
		relay = strings.TrimSuffix(mx.Host, ".")
		break
	}
	if relay == "" {
		// Having MX records rules out the implicit MX.
		err = fmt.Errorf("%w: no usable MX for %s (null MX, or .onion only)", errPermanent, domain)
		return
	}
	log.Tracef(
		"DNS lookup: Hostname=%s, MX=%s",
		emailParts.domain,
		relay,
	)
	return
}

// parseFrom takes a mail address of the format Name <name@foo> and validates
// it.  If custom From headers are not allowed, it will be tweaked to conform
// with the Remailer's configuration.
func parseFrom(h mail.Header) []string {
	// mail.Address.String quotes and encodes the display name, so a name
	// can't pose as an address (e.g. "Bank <security@bank.example>").
	defaultFrom := []string{(&mail.Address{
		Name:    cfg.Mail.OutboundName,
		Address: cfg.Mail.OutboundAddy,
	}).String()}
	from, err := h.AddressList("From")
	if err != nil || len(from) == 0 {
		// The supplied address is invalid or missing.  Use defaults instead.
		return defaultFrom
	}
	if cfg.Mail.CustomFrom {
		// Accept whatever was provided (it's already been validated by
		// AddressList).
		return []string{from[0].String()}
	}
	if len(from[0].Name) == 0 {
		return defaultFrom
	}
	return []string{(&mail.Address{
		Name:    from[0].Name,
		Address: cfg.Mail.OutboundAddy,
	}).String()}
}

// poolFileAge returns how many days ago a pool file was written.  The date is
// read from the first line only, where writeInternalHeader puts it, so the
// sender's data that follows can't change it (a plaintext starting with a
// space used to continue the header).  If the line is missing or its date
// doesn't parse, the file's modification time is used, so every file expires
// after pool.max_age days.
func poolFileAge(filename string, content []byte) int {
	if line, ok := bytes.CutPrefix(content, []byte("Yamn-Pooled-Date: ")); ok {
		if i := bytes.IndexByte(line, '\n'); i >= 0 {
			line = line[:i]
		}
		date, err := time.Parse(shortdate, strings.TrimSpace(string(line)))
		if err == nil {
			return daysAgo(date)
		}
		log.Warnf("%s: Failed to parse Yamn-Pooled-Date, using the file time: %s", filename, err)
	} else {
		// Legacy condition.  All current versions apply this header.
		log.Warnf("%s: No Yamn-Pooled-Date header, using the file time", filename)
	}
	info, err := os.Stat(filename)
	if err != nil {
		return 0
	}
	return daysAgo(info.ModTime())
}

// Read a file from the outbound pool and mail it
func mailPoolFile(filename string) (delFlag bool, err error) {
	// This flag implies that, by default, we don't delete pool messages
	delFlag = false

	content, err := os.ReadFile(filename)
	if err != nil {
		log.Errorf("Failed to read file for mailing: %s", err)
		return
	}
	age := poolFileAge(filename, content)
	if age > cfg.Pool.MaxAge {
		// The message has expired.  Give up trying to send it.
		log.Infof(
			"%s: Refusing to mail pool file. Exceeds max age of %d days",
			filename,
			cfg.Pool.MaxAge,
		)
		// Set deletion flag.  We don't want to retain old messages
		// forever.
		delFlag = true
		return
	}
	if age > 0 {
		log.Tracef("Mailing pooled file that's %d days old.", age)
	}

	// The message follows the internal header line.
	msg, err := mail.ReadMessage(bytes.NewReader(stripInternalHeader(content)))
	if err != nil {
		log.Errorf("Failed to process mail file: %s", err)
		// If we can't process it, it'll never get sent.  Mark for delete.
		delFlag = true
		return
	}
	if h := badHeader(msg.Header); h != "" {
		err = fmt.Errorf("%s: refusing to mail a %s header with a CR or NUL in it", filename, h)
		delFlag = true
		return
	}
	// The sender can't set internal headers.
	delete(msg.Header, "Yamn-Pooled-Date")

	// Add some required headers to the message.
	msg.Header["Date"] = []string{time.Now().Format(rfc5322date)}
	msg.Header["Message-Id"] = []string{messageID()}
	msg.Header["From"] = parseFrom(msg.Header)
	sendTo := headToAddy(msg.Header, "To")
	sendTo = append(sendTo, headToAddy(msg.Header, "Cc")...)
	// Bcc recipients get the message, but the Bcc header itself is removed
	// by filterHeaders below so other recipients don't see them.
	sendTo = uniqueStrings(append(sendTo, headToAddy(msg.Header, "Bcc")...))
	filterHeaders(msg.Header)
	if len(sendTo) == 0 {
		err = fmt.Errorf("%s: No email recipients found", filename)
		// No point in repeatedly trying to resend a malformed file.
		delFlag = true
		return
	}
	// Remove recipients who asked not to receive mail from this remailer.
	// The list is read for every message, so edits apply immediately.  If
	// it can't be read, keep the message in the pool rather than risk
	// mailing a blocked recipient.
	blockList, err := readDestBlock(cfg.Files.DestBlock)
	if err != nil {
		err = fmt.Errorf("%s: unable to read destination block list: %w", filename, err)
		return
	}
	var allowed []string
	for _, addy := range sendTo {
		if destBlocked(blockList, addy) {
			log.Infof("%s: recipient %s is on the destination block list", filename, addy)
			continue
		}
		allowed = append(allowed, addy)
	}
	if len(allowed) == 0 {
		err = fmt.Errorf("%s: all recipients are on the destination block list", filename)
		delFlag = true
		return
	}
	sendTo = allowed
	// Limit the fan-out of a single anonymous message.
	if limit := cfg.Mail.MaxRecipients; limit > 0 && len(sendTo) > limit {
		err = fmt.Errorf("%s: %d recipients exceed max_recipients of %d", filename, len(sendTo), limit)
		delFlag = true
		return
	}
	// Temporary errors keep the file for the next pool run.  Permanent
	// ones delete it: retrying won't help.
	err = mailBytes(assemble(*msg), sendTo)
	if errors.Is(err, errPermanent) {
		delFlag = true
	}
	return
}

// Mail a byte payload to a given address
func mailBytes(payload []byte, sendTo []string) (err error) {
	// Test if the message is destined for the local remailer
	log.Tracef("Message recipients are: %s", strings.Join(sendTo, ","))
	if cfg.Mail.Outfile {
		var filename string
		filename, err = writePoolFile("outfile-", payload)
		if err != nil {
			log.Warnf("Outfile write failed: %s", err)
			return
		}
		log.Tracef("Wrote output to %s", filename)
	} else if cfg.Mail.Pipe != "" {
		err = execSend(payload, cfg.Mail.Pipe, sendTo)
		if err != nil {
			log.Warn("Email pipe failed")
			return
		}
	} else if cfg.Mail.Sendmail {
		err = sendmail(payload, sendTo)
		if err != nil {
			log.Warn("Sendmail failed")
			return
		}
	} else {
		err = smtpRelay(payload, sendTo)
		if err != nil {
			log.Warn("SMTP relay failed")
			return
		}
	}
	return
}

// pipeReadsHeaders reports whether the pipe command has sendmail's -t
// option (alone or as in -ti or -it), which takes the recipients from the
// headers rather than from execSend's arguments.
func pipeReadsHeaders(execCmd string) bool {
	fields := strings.Fields(execCmd)
	for i := 1; i < len(fields); i++ {
		f := fields[i]
		if f == "--" {
			break
		}
		if len(f) > 1 && f[0] == '-' && strings.Trim(f[1:], "it") == "" && strings.Contains(f, "t") {
			return true
		}
	}
	return false
}

// execSend pipes mail to an external command (e.g. "/usr/sbin/sendmail -i").
// The recipients are added as arguments after "--", so they can't be read
// as options.  They are the ones left after the block list, and include
// Bcc, which is no longer in the headers.
//
// A command with -t gets no recipient arguments, as before: it takes them
// from the headers, and sendmail-like MTAs differ in what they do with
// arguments given together with -t (Exim and Sendmail remove them from the
// recipients).  Setups shared with upstream yamn, which always needs -t,
// keep working that way; nagOperator warns that the block list and Bcc
// don't apply then.
func execSend(payload []byte, execCmd string, sendTo []string) (err error) {
	sendmail := new(exec.Cmd)
	sendmail.Args = strings.Fields(execCmd)
	sendmail.Path = sendmail.Args[0]
	if !pipeReadsHeaders(execCmd) {
		if sendmail.Args[len(sendmail.Args)-1] != "--" {
			sendmail.Args = append(sendmail.Args, "--")
		}
		sendmail.Args = append(sendmail.Args, sendTo...)
	}

	stdin, err := sendmail.StdinPipe()
	if err != nil {
		log.Errorf("%s: %s", execCmd, err)
		return
	}
	defer stdin.Close()
	sendmail.Stdout = os.Stdout
	sendmail.Stderr = os.Stderr
	err = sendmail.Start()
	if err != nil {
		log.Errorf("%s: %s", execCmd, err)
		return
	}
	stdin.Write(payload)
	stdin.Close()
	err = sendmail.Wait()
	if err != nil {
		log.Errorf("%s: %s", execCmd, err)
		return
	}
	return
}

// isLoopbackHost reports whether host is the local machine, where mail
// travels without leaving the host and plaintext SMTP is acceptable.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// smtpRootCAs overrides the system certificate pool when verifying SMTP
// servers.  It is nil (use the system pool) except in tests.
var smtpRootCAs *x509.CertPool

// SMTP timeouts.  smtpTimeout bounds the whole SMTP exchange, from the
// greeting to QUIT, so a slow or tarpitting server cannot stall the outbound
// pool for longer than that.  They are variables so tests can shorten them.
var (
	smtpDialTimeout = 30 * time.Second
	smtpTimeout     = 5 * time.Minute
)

// smtpDial opens the TCP connection to an SMTP server.  Tests replace it.
var smtpDial = func(network, address string) (net.Conn, error) {
	return net.DialTimeout(network, address, smtpDialTimeout)
}

// smtpTLSConfig returns the TLS settings for an SMTP connection to host.  The
// configured relay is always verified, as the SMTP password may be sent to
// it.  Direct deliveries to a recipient's MX use opportunistic TLS (any
// certificate is accepted, as most MTAs do) unless require_tls is set.  A
// relay on the local machine is not verified either, as its traffic never
// leaves the host.
func smtpTLSConfig(host string, directMX bool) *tls.Config {
	return &tls.Config{
		ServerName:         host,
		RootCAs:            smtpRootCAs,
		InsecureSkipVerify: isLoopbackHost(host) || (directMX && !cfg.Mail.RequireTLS),
	}
}

func smtpRelay(payload []byte, sendTo []string) (err error) {
	relay := cfg.Mail.SMTPRelay
	port := cfg.Mail.SMTPPort
	directMX := false

	/*
		The following section tries to get the MX record for the
		recipient email address, when there is only a single recipient.
		If it succeeds, the email will be sent directly to the
		recipient MX.
	*/
	if cfg.Mail.MXRelay && len(sendTo) == 1 {
		log.Tracef("DNS lookup of MX record for %s.", sendTo[0])
		mx, err := mxLookup(sendTo[0])
		if err != nil {
			log.Warn(err)
			return err
		}
		log.Tracef(
			"Doing direct relay for %s to %s:25.",
			sendTo[0],
			mx,
		)
		relay = mx
		port = "25"
		directMX = true
	}
	return smtpSend(relay, port, directMX, payload, sendTo)
}

// smtpSend delivers payload to sendTo through the SMTP server at relay:port.
// directMX is true when relay is the recipient's MX rather than the
// configured relay.  Recipients rejected with a 5xx reply are skipped, and
// the message goes to the others.  The error is marked errPermanent if every
// recipient, or the message itself, got a 5xx reply.  Other failures (4xx
// replies, network and TLS errors) are temporary and nothing is delivered,
// so the message can be sent again later.
func smtpSend(relay, port string, directMX bool, payload []byte, sendTo []string) (err error) {
	serverAddr := net.JoinHostPort(relay, port)

	conn, err := smtpDial("tcp", serverAddr)
	if err != nil {
		log.Warnf("Dial Error: Server=%s, Error=%s", serverAddr, err)
		return
	}
	// Closing the connection also covers every early return below.  After a
	// successful QUIT it's already closed and this is a no-op.
	defer conn.Close()
	// The deadline also applies through the TLS layer after STARTTLS, as
	// that wraps this connection.
	if err = conn.SetDeadline(time.Now().Add(smtpTimeout)); err != nil {
		return
	}

	client, err := smtp.NewClient(conn, relay)
	if err != nil {
		log.Warnf(
			"SMTP Connection Error: Server=%s, Error=%s",
			serverAddr,
			err,
		)
		return
	}
	// Test if the remote MTA supports STARTTLS
	ok, _ := client.Extension("STARTTLS")
	if ok && cfg.Mail.UseTLS {
		if err = client.StartTLS(smtpTLSConfig(relay, directMX)); err != nil {
			log.Warnf(
				"Error performing STARTTLS: Server=%s, Error=%s",
				serverAddr,
				err,
			)
			return
		}
	} else if cfg.Mail.RequireTLS {
		err = fmt.Errorf("%s: no TLS available and require_tls is set", serverAddr)
		log.Warn(err)
		return
	} else if !ok && cfg.Mail.UseTLS && !directMX && !isLoopbackHost(relay) {
		// The configured relay is expected to offer STARTTLS.  If it
		// doesn't, it's misconfigured or STARTTLS was stripped in
		// transit, so don't fall back to plaintext.
		err = fmt.Errorf("%s: configured relay does not offer STARTTLS", serverAddr)
		log.Warn(err)
		return
	} else if !ok {
		log.Infof("%s does not offer STARTTLS. Sending without TLS.", serverAddr)
	}
	// If AUTH is supported and a UserID and Password are configured, try to
	// authenticate to the remote MTA.  Credentials are only ever sent to the
	// configured relay, never to a recipient's MX.
	ok, _ = client.Extension("AUTH")
	if ok && !directMX && cfg.Mail.Username != "" && cfg.Mail.Password != "" {
		auth := smtp.PlainAuth(
			"",
			cfg.Mail.Username,
			cfg.Mail.Password,
			cfg.Mail.SMTPRelay,
		)
		if err = client.Auth(auth); err != nil {
			log.Warnf("Auth Error:  Server=%s, Error=%s", serverAddr, err)
			return
		}
	}
	// Remailer.Address is a legacy setting as clients may also need to
	// set the sender address if their ISPs MTA demands it's valid.
	// TODO remove cfg.Remailer.Address in a later version (27/04/2015)
	var sender string
	if cfg.Mail.Sender != "" {
		sender = cfg.Mail.Sender
	} else {
		sender = cfg.Remailer.Address
	}
	// A 5xx reply to MAIL FROM usually means the sender address or the relay
	// is misconfigured, so it's not treated as permanent.
	if err = client.Mail(sender); err != nil {
		log.Warnf("SMTP Error: Server=%s, Error=%s", serverAddr, err)
		return
	}

	accepted := 0
	for _, addr := range sendTo {
		if err = client.Rcpt(addr); err != nil {
			if permanentFor(err, directMX) {
				log.Infof("Recipient rejected: Server=%s, Error=%s", serverAddr, err)
				continue
			}
			log.Warnf("Error: %s\n", err)
			return
		}
		accepted++
	}
	if accepted == 0 {
		err = fmt.Errorf("%w: every recipient was rejected: %w", errPermanent, err)
		log.Warn(err)
		return
	}
	err = nil

	w, err := client.Data()
	if err != nil {
		if permanentFor(err, directMX) {
			err = fmt.Errorf("%w: %w", errPermanent, err)
		}
		log.Warnf("Error: %s\n", err)
		return
	}

	_, err = w.Write(payload)
	if err != nil {
		log.Warnf("Error: %s\n", err)
		return

	}

	err = w.Close()
	if err != nil {
		if permanentFor(err, directMX) {
			err = fmt.Errorf("%w: message rejected: %w", errPermanent, err)
		}
		log.Warnf("Error: %s\n", err)
		return

	}

	// The message has been accepted at this point.  A failed QUIT must not
	// be reported as a failed delivery, or the message would be sent again.
	if qerr := client.Quit(); qerr != nil {
		log.Tracef("SMTP QUIT failed after delivery: Server=%s, Error=%s", serverAddr, qerr)
	}
	return
}

// sendmail delivers through the configured relay only (no direct MX
// delivery).  It shares smtpSend with smtpRelay, so it uses the same TLS
// policy and timeouts.
func sendmail(payload []byte, sendTo []string) (err error) {
	err = smtpSend(cfg.Mail.SMTPRelay, cfg.Mail.SMTPPort, false, payload, sendTo)
	if err != nil {
		log.Warn(err)
	}
	return
}
