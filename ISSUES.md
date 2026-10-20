# Known Issues

Findings from a full code review of `dev-101` (2026-10-20). Line numbers refer
to that branch at commit `788b526`.

Status values: **open**, **in progress**, **fixed** (with commit).

| ID | Severity | Summary | Status |
|---|---|---|---|
| [Y-01](#y-01-any-sender-can-crash-a-remailer-with-one-packet) | High | Any sender can crash a remailer with one packet | fixed (4f370cc) |
| [Y-02](#y-02-smtp-tls-without-certificate-verification) | High | SMTP TLS without certificate verification | fixed (6c0fb7e) |
| [Y-03](#y-03-public-keyring-is-trusted-without-verification) | High | Public keyring is trusted without verification | in progress (keyid check: fa8b18c) |
| [Y-04](#y-04-next-hop-not-validated-header-injection-at-middle-remailers) | High | Next hop not validated; header injection at middle remailers | fixed (a4ad3ce) |
| [Y-05](#y-05-multi-chunk-messages-are-never-delivered) | Medium | Multi-chunk messages are never delivered | fixed (486f1e6) |
| [Y-06](#y-06-smtp-has-no-timeouts) | Medium | SMTP has no timeouts | fixed (f894bf0) |
| [Y-07](#y-07-exit-header-handling) | Medium | Exit header handling (Bcc leak, no filtering, From spoofing) | fixed (54d7a84) |
| [Y-08](#y-08-no-abuse-controls-at-the-exit) | Medium | No abuse controls at the exit | open |
| [Y-09](#y-09-replay-window) | Medium | Replay window | open |
| [Y-10](#y-10-malformed-files-crash-yamn) | Medium | Malformed stats / secring files crash yamn | open |
| [Y-11](#y-11-failed-download-overwrites-a-good-keyring) | Medium | Failed download overwrites a good keyring | open |
| [Y-12](#y-12-non-yamn-mail-is-never-deleted) | Medium | Non-yamn mail is never deleted | open |
| [Y-13](#y-13-config-file-is-read-from-the-working-directory) | Medium | Config file is read from the working directory | open |
| [Y-14](#y-14-biased-shuffle) | Low | Biased shuffle | open |
| [Y-15](#y-15-num_copies-config-setting-is-ignored) | Low | `num_copies` config setting is ignored | open |
| [Y-16](#y-16-packet-size-and-bounds-checks) | Low | Packet size and bounds checks | open |
| [Y-17](#y-17-data-race-on-stats) | Low | Data race on `stats` | open |
| [Y-18](#y-18-resource-leaks-and-small-cleanups) | Low | Resource leaks and small cleanups | open |
| [Y-19](#y-19-remailer--replies-go-to-a-forgeable-address) | Low | `remailer-*` replies go to a forgeable address | open |
| [Y-20](#y-20-client-does-not-strip-identifying-headers) | Low | Client does not strip identifying headers | open |

---

## High

### Y-01: Any sender can crash a remailer with one packet

- **Where:** `packet.go:936` (`decryptBody`), called from `server.go:438`;
  `server.go:502-512` (`smtpMethod`)
- **Impact:** Remote, unauthenticated crash of any yamn remailer. Middle
  remailers are affected too, because `decryptBody` runs before the exit check.
- **Details:** Fields from the decrypted exit header are used as slice bounds
  without validation:
  - `bodyBytes` (a 32-bit value) bounds the slice of the body. Values above
    `bodyBytes` (17920) panic.
  - `chunkNum` and `numChunks` index the chunk list. `chunkNum` 0, a
    `chunkNum` greater than the stored chunk count, or a `numChunks` that
    differs from the stored record panics.

  The sender builds this header with the remailer's public key, so it passes
  the authentication, anti-tag and timestamp checks. Nothing recovers the
  panic. Each packet causes one crash; on restart it is rejected as a replay.
- **Fix:** In `decodeV2`, reject packets where `bodyBytes > bodyBytes`,
  `numChunks == 0`, `chunkNum == 0` or `chunkNum > numChunks`. In
  `smtpMethod`, reject when the stored chunk list length differs from
  `numChunks`. Consider a `recover` around per-message processing so a single
  bad message can never stop the daemon.
- **Status:** fixed in `4f370cc` (branch `fix/01-remote-crash`).
  `slotFinal.validate()` rejects bad exit headers before the body is
  decrypted; `smtpMethod` rejects chunks that disagree with the stored record;
  `decodeSafely` recovers from any remaining panic and drops the message.
  Tests: `validate_test.go`.

### Y-02: SMTP TLS without certificate verification

- **Where:** `mail.go:303`, `mail.go:347`
- **Impact:** Anyone on the network path can read the plaintext that exit
  remailers deliver and capture the SMTP AUTH password.
- **Details:** `tls.Config` sets `InsecureSkipVerify = true` unconditionally.
  If the server does not advertise STARTTLS, mail is sent unencrypted without
  any warning, even with `usetls: true`.
- **Fix:** Verify certificates (set `ServerName`). Add a config option to
  require TLS and refuse to send without it. Never send AUTH credentials over an
  unverified connection.
- **Status:** fixed in `6c0fb7e` (branch `fix/02-smtp-tls-verify`).
  - Configured relay: certificate verified; with `usetls` on, a relay that
    offers no STARTTLS is refused (no plaintext fallback).
  - Direct MX delivery: opportunistic TLS (any certificate, plaintext if no
    STARTTLS), unless the new `mail.require_tls: true` is set, which requires
    verified TLS for every delivery.
  - Relay on localhost: plaintext or any certificate accepted.
  - Credentials are only sent to the configured relay, never to an MX.
  - ~~Remaining gap: `require_tls` is not applied in `sendmail: true` mode.~~
    Closed in `f894bf0` (Y-06): sendmail mode now uses `smtpSend` and the
    same relay TLS policy.
  - Tests: `mail_test.go`.

### Y-03: Public keyring is trusted without verification

- **Where:** `keymgr/public.go` (`ImportPubring`, check at line 402); download
  in `utils.go` (`httpGet`)
- **Impact:** Whoever controls the pubring file or its download source can
  replace every remailer's public key with their own and read all traffic.
- **Details:** `pubring.mix` is fetched from a single URL and imported without
  a signature check. The importer only compares the two copies of the keyid in
  each block. It never checks that the keyid matches the public key, which is
  defined as `blake2s-128(PK)` (see `makeKeyID`). HTTPS protects the download
  in transit, not against the server itself.
- **Fix:** At minimum, reject keys where `keyid != makeKeyID(PK)`. Longer
  term: signed pubrings, or key pinning with a warning when a known
  remailer's key changes unexpectedly.
- **Status:** in progress.
  - Done in `fa8b18c` (branch `fix/03-pubring-keyid`): `ImportPubring` ignores
    keys whose keyid is not the hash of the public key. All six keys in the
    mixmin.net pubring (2026-10-20) pass.
  - Open: this doesn't stop whoever controls the pubring from publishing a
    new, self-consistent key. Signed pubrings or key pinning to be discussed
    after the other fixes.

### Y-04: Next hop not validated; header injection at middle remailers

- **Where:** `server.go:418`, `utils.go:227` (`writeMailHeaders`)
- **Impact:** A sender can make any middle remailer mail arbitrary addresses,
  and inject extra headers (for example `Cc:`) into the outgoing mail.
- **Details:** The next-hop address comes from the decrypted packet. It is
  checked neither against the pubring nor for line breaks, and is written raw
  into `To: %s\n`. `mailPoolFile` then sends to every `To` and `Cc` address
  it finds.
- **Fix:** Only forward to next hops that exist in the pubring. Reject
  addresses containing control characters.
- **Status:** fixed in `a4ad3ce` (branch `fix/04-next-hop-validation`).
  `checkNextHop` requires a known remailer address without control
  characters before a middle remailer forwards; `writeMessageToPool` refuses
  recipients containing line breaks. A remailer whose keyring is missing the
  next hop now drops the packet instead of forwarding it. Tests:
  `nexthop_test.go`.

## Medium

### Y-05: Multi-chunk messages are never delivered

- **Where:** `client.go:60`, `client.go:134`; `chunker.go:128`;
  `pool.go` (`writePlainToPool`)
- **Impact:** Messages larger than one packet (about 17.9 KB) are never
  delivered. Confirmed with a test.
- **Details:**
  1. `mixprep` creates one `slotFinal`, with one packet ID, and reuses it for
     every chunk. The exit's replay protection rejects chunk 2 onward with
     "packet ID collision".
  2. With distinct packet IDs, reassembly concatenates the chunk files
     wholesale. Each chunk's internal `Yamn-Pooled-Date:` line ends up in the
     middle of the delivered message.
  3. `gotExit` resets for every chunk. With `-c 1` each chunk can pick a
     different random exit, so the chunks could never be reassembled.
- **Fix:** Give each chunk its own packet ID (copies of the same chunk must
  share one). Strip the internal header from chunks 2..n when assembling, or
  store chunks without it. Choose the exit once per message, not per chunk.
  Add a multi-chunk case to `roundtrip_test.go`.
- **Status:** fixed in `486f1e6` (branch `fix/05-multi-chunk`). Fresh packet ID
  per chunk (`slotFinal.newPacketID`), exit chosen once per message,
  internal headers stripped on assembly. Tests: `chunks_test.go` runs the
  real client on 3- and 6-chunk messages through middle and exit remailers.

### Y-06: SMTP has no timeouts

- **Where:** `mail.go:330` (`net.Dial`) and the SMTP exchange after it
- **Impact:** One slow or tarpitting mail server stalls the single outbound
  sender goroutine, and with it the whole outbound pool, indefinitely.
- **Fix:** Use `net.DialTimeout` and set a deadline on the connection for the
  whole SMTP exchange. Close `conn` on every error path.
- **Status:** fixed in `f894bf0` (branch `fix/06-smtp-timeouts`). 30s dial
  timeout, 5 minute deadline on the whole SMTP exchange, connection closed on
  every path. `sendmail: true` mode now uses the same code (`smtpSend`)
  instead of `net/smtp.SendMail`, so it gets the timeouts too. Tests:
  `TestSmtpSendTimeout`, `TestSendmailUsesSmtpSend` in `mail_test.go`.

### Y-07: Exit header handling

- **Where:** `mail.go:18` (`assemble`), `mail.go:119-157` (`parseFrom`),
  `mail.go:216`
- **Impact:** Privacy leak to recipients and From spoofing despite
  `custom_from: false`.
- **Details:**
  - `assemble` copies every header, including `Bcc:`, into the delivered
    message. Bcc addresses are revealed to recipients and never actually
    delivered (only `To` and `Cc` become recipients).
  - No headers are filtered. `Sender:`, `Reply-To:`, `Resent-From:` and
    similar pass through, although `remailer-conf` advertises header filtering.
  - `parseFrom` builds `Name <addr>` with `fmt` instead of
    `mail.Address.String()`. A display name containing `<other@domain>` is not
    quoted, so the From line can show a forged address.
  - Only the first value of each header is kept (`Header.Get`).
- **Fix:** Deliver to Bcc addresses and strip the header. Add a header
  allow-list or block-list. Use `mail.Address.String()` for From.
- **Status:** fixed in `54d7a84` (branch `fix/07-exit-headers`). Allow-list
  chosen: To, Cc, Subject, Reply-To, In-Reply-To, References, Newsgroups,
  Followup-To, MIME-Version, X-No-Archive and Content-* pass (plus From,
  Date, Message-Id set by the remailer); `mail.allow_headers` adds more.
  Bcc recipients are delivered and the header removed; From names are
  quoted; repeated headers keep every value. Tests: `exitheaders_test.go`.

### Y-08: No abuse controls at the exit

- **Where:** exit delivery path (`smtpMethod`, `mailPoolFile`)
- **Impact:** Operators cannot honour opt-out requests or limit abuse.
- **Details:** There is no destination block list, so recipients have no way
  to opt out (Mixmaster has `dest.blk`). The `max_size` setting is advertised
  in `remailer-conf` but never enforced.
- **Fix:** Add a destination block list and enforce `max_size`.

### Y-09: Replay window

- **Where:** `config/config.go:299`, `config/config.go:302`; `server.go:371`
- **Impact:** Packets can be replayed in some configurations.
- **Details:**
  - The default replay-log lifetime (`id_expire`) equals the maximum packet
    age (`max_age`), both 14 days. A same-day packet can be replayed on day 14
    if the remailer restarts that day, because the ID log expires at startup.
  - Nothing warns if `id_expire` is set below `max_age`, which opens a wide
    window.
  - The packet ID is recorded before the anti-tag and age checks, so a
    tampered copy that arrives first "burns" the ID of the genuine packet.
- **Fix:** Require `id_expire > max_age` (warn in `nagOperator` or clamp), with
  a margin of a day or more. Record the ID only after the packet passes the
  other checks.

### Y-10: Malformed files crash yamn

- **Where:** `keymgr/public.go:244`; `keymgr/secret.go:451`,
  `keymgr/secret.go:482`
- **Impact:** A malformed stats file (downloaded from the network) crashes
  clients and remailers. A damaged secret keyring crashes the remailer at
  startup.
- **Details:** A stats line whose latency field has no `:` indexes `lat[1]`
  out of range. Secring lines shorter than 9 characters are sliced with
  `line[:9]`.
- **Fix:** Check lengths before indexing (`strings.Cut`,
  `strings.HasPrefix`).

### Y-11: Failed download overwrites a good keyring

- **Where:** `utils.go:148` (`httpGet`)
- **Impact:** A truncated download or an HTML error page served with status
  200 replaces a working pubring or stats file, leaving the client with no
  usable remailers.
- **Fix:** Download to a temporary file, check that it parses, then rename it
  over the old file.

### Y-12: Non-yamn mail is never deleted

- **Where:** `pool.go:217-220` (`processMail`)
- **Impact:** Spam or other non-yamn mail accumulates in `Maildir/cur`
  forever.
- **Details:** `Unseen()` moves each message to `cur/`. If dearmoring fails,
  the loop continues without purging it, and nothing else ever cleans `cur/`.
- **Fix:** Purge the message on every path, or move unparseable mail to a
  separate folder with expiry.

### Y-13: Config file is read from the working directory

- **Where:** `config/config.go` (`findConfig`)
- **Impact:** Running yamn in an untrusted directory can execute an arbitrary
  command, because `mail.pipe` in a planted `yamn.yml` is executed.
- **Details:** A `yamn.yml` in the current directory takes priority over
  `~/yamn.yml` and `/etc/yamn.yml`.
- **Fix:** Drop the working-directory lookup, or move it below the home and
  system locations and warn when it is used.

## Low

### Y-14: Biased shuffle

- **Where:** `crandom/crandom.go:86` (`Shuffle`), `crandom/crandom.go:73`
  (`RandInts`)
- **Details:** `Shuffle` swaps each element with a random position in the whole
  slice, which does not give a uniform permutation. It is used to pick pool
  messages; impact is small because pool filenames are random. `RandInts` uses
  `Intn(i)` instead of `Intn(i+1)`, so it never leaves an element in place.
  It is unused.
- **Fix:** Use `rand.Shuffle` / `rand.Perm` on the crypto-backed source.

### Y-15: `num_copies` config setting is ignored

- **Where:** `client.go:136`
- **Details:** The `--copies` flag defaults to 2, so `flag.Copies == 0` is
  never true and `stats.num_copies` from the config is never used.
- **Fix:** Default the flag to 0 so the config value applies.

### Y-16: Packet size and bounds checks

- **Where:** `packet.go:653`, `packet.go:711`
- **Details:** `setPlainText` compares `m.plainLength` (always 0) instead of
  the new length, so its size check never fires. `getPartialIV` uses `>`
  where it needs `>=`. Both are internal and not reachable from received
  packets today.
- **Fix:** Compare the actual length; use `>=`.

### Y-17: Data race on `stats`

- **Where:** `stats.go`, `pool.go` (`emailPoolFile`), `server.go`
- **Details:** The `stats` counters are written from the main loop and from the
  `serverPoolOutboundSend` goroutine without synchronisation.
- **Fix:** Use a mutex or atomic counters.

### Y-18: Resource leaks and small cleanups

- `ImportPubring`, `ImportStats` and `Headers` (`keymgr/public.go`) never close
  their files.
- ~~`smtpRelay` (`mail.go`) leaves the connection open on error paths.~~
  Fixed in `f894bf0` (Y-06).
- `Chunk.Expire` (`chunker.go`) shifts the items with `copy` without
  shortening the slice, so the last chunk file is processed twice.

### Y-19: `remailer-*` replies go to a forgeable address

- **Where:** `pool.go` (`processMail`), `server.go` (`remailerFoo`)
- **Details:** Replies to `remailer-key`, `remailer-help` and similar go to the
  incoming `From:` header, so anyone can direct them at a third party. This
  matches Mixmaster behaviour but could be rate-limited.

### Y-20: Client does not strip identifying headers

- **Where:** `client.go` (`readMessage`), `mail.go` (`assemble`)
- **Details:** Headers such as `User-Agent`, `X-Mailer`, `Reply-To` and
  `Message-ID` in the user's message are passed through to the exit and
  delivered to the recipient.
- **Fix:** Strip known identifying headers in the client, or warn about them.
