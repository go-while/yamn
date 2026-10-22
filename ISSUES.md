# Known Issues

Findings from a full code review of `dev-101` (2026-10-20). Line numbers refer
to that branch at commit `788b526`.

Status values: **open**, **in progress**, **fixed** (with commit).

Y-23 to Y-52 come from a second review of `master` at `15acb78`
(2026-10-21; crypto/packets, mail/network, client/config). The review test
names refer to tests written in a scratch copy during that review; they are
not part of the repository yet and come with the fixes.

| ID | Severity | Summary | Status |
|---|---|---|---|
| [Y-01](#y-01-any-sender-can-crash-a-remailer-with-one-packet) | High | Any sender can crash a remailer with one packet | fixed (4f370cc) |
| [Y-02](#y-02-smtp-tls-without-certificate-verification) | High | SMTP TLS without certificate verification | fixed (6c0fb7e) |
| [Y-03](#y-03-public-keyring-is-trusted-without-verification) | High | Public keyring is trusted without verification | in progress (keyid check: fa8b18c; pinger cross-check: de59490) |
| [Y-04](#y-04-next-hop-not-validated-header-injection-at-middle-remailers) | High | Next hop not validated; header injection at middle remailers | fixed (a4ad3ce) |
| [Y-05](#y-05-multi-chunk-messages-are-never-delivered) | Medium | Multi-chunk messages are never delivered | fixed (486f1e6) |
| [Y-06](#y-06-smtp-has-no-timeouts) | Medium | SMTP has no timeouts | fixed (f894bf0) |
| [Y-07](#y-07-exit-header-handling) | Medium | Exit header handling (Bcc leak, no filtering, From spoofing) | fixed (54d7a84) |
| [Y-08](#y-08-no-abuse-controls-at-the-exit) | Medium | No abuse controls at the exit | fixed (3bc21e1) |
| [Y-09](#y-09-replay-window) | Medium | Replay window | fixed (b364696) |
| [Y-10](#y-10-malformed-files-crash-yamn) | Medium | Malformed stats / secring files crash yamn | fixed (40678e5) |
| [Y-11](#y-11-failed-download-overwrites-a-good-keyring) | Medium | Failed download overwrites a good keyring | fixed (3e7f17c) |
| [Y-12](#y-12-non-yamn-mail-is-never-deleted) | Medium | Non-yamn mail is never deleted | fixed (a1bf598) |
| [Y-13](#y-13-config-file-is-read-from-the-working-directory) | Medium | Config file is read from the working directory | fixed (d9393b0) |
| [Y-14](#y-14-biased-shuffle) | Low | Biased shuffle | fixed (019b95c) |
| [Y-15](#y-15-num_copies-config-setting-is-ignored) | Low | `num_copies` config setting is ignored | fixed (f1d760e) |
| [Y-16](#y-16-packet-size-and-bounds-checks) | Low | Packet size and bounds checks | fixed (73de26b) |
| [Y-17](#y-17-data-race-on-stats) | Low | Data race on `stats` | fixed (d94c550) |
| [Y-18](#y-18-resource-leaks-and-small-cleanups) | Low | Resource leaks and small cleanups | fixed (205d80f) |
| [Y-19](#y-19-remailer--replies-go-to-a-forgeable-address) | Low | `remailer-*` replies go to a forgeable address | fixed (5e5e2d1) |
| [Y-20](#y-20-client-does-not-strip-identifying-headers) | Low | Client does not strip identifying headers | fixed (0d01f6a) |
| [Y-21](#y-21-client-ignores-the-message-file-argument) | Medium | Client ignores the message file argument | fixed (13fcf17) |
| [Y-22](#y-22-relative-paths-in-yamnyml-ignore---dir) | Low | Relative paths in yamn.yml ignore `--dir` | fixed (6886742) |
| [Y-23](#y-23-malformed-pubring-entries-can-stop-a-remailer) | Medium | Malformed pubring entries can stop a remailer | open |
| [Y-24](#y-24-quoted-addresses-inject-esmtp-parameters-past-the-block-list) | Medium | Quoted addresses inject ESMTP parameters past the block list | open |
| [Y-25](#y-25-a-bare-cr-reaches-the-smtp-wire) | Medium | A bare CR reaches the SMTP wire | open |
| [Y-26](#y-26-pipe-mode-ignores-the-block-list-and-drops-bcc) | Medium | Pipe mode ignores the block list and drops Bcc | open |
| [Y-27](#y-27-a-message-can-stay-in-the-pool-forever) | Medium | A message can stay in the pool forever | open |
| [Y-28](#y-28-a-slow-mail-server-blocks-the-remailer-tarpit) | Medium | A slow mail server blocks the remailer (tarpit) | open |
| [Y-29](#y-29-data-files-are-read-from-the-current-directory-without---dir) | Medium | Data files are read from the current directory without --dir | open |
| [Y-30](#y-30-distance-is-ignored-for-hops-named-in---chain) | Medium | `distance` is ignored for hops named in --chain | open |
| [Y-31](#y-31-messages-over-255-chunks-are-truncated) | Medium | Messages over 255 chunks are truncated | open |
| [Y-32](#y-32-merged-stats-have-no-quorum) | Medium | Merged stats have no quorum | open |
| [Y-33](#y-33-pool-and-chunk-files-are-not-written-atomically) | Low | Pool and chunk files are not written atomically | open |
| [Y-34](#y-34-partial-chunks-have-no-storage-limit) | Low | Partial chunks have no storage limit | open |
| [Y-35](#y-35-a-running-remailer-keeps-keys-that-left-the-keyring) | Low | A running remailer keeps keys that left the keyring | open |
| [Y-36](#y-36-the-advertised-key-is-not-checked-against-the-secret-keyring) | Low | The advertised key is not checked against the secret keyring | open |
| [Y-37](#y-37-secret-keyring-handling-purge-crash-safety-dates) | Low | Secret keyring handling: purge, crash safety, dates | open |
| [Y-38](#y-38-one-corrupt-idlog-entry-stops-every-startup) | Low | One corrupt idlog entry stops every startup | open |
| [Y-39](#y-39-multi-pinger-merge-quorum-shrinks-ties-go-to-the-freshest-pinger) | Low | Multi-pinger merge: quorum shrinks, ties go to the freshest pinger | open |
| [Y-40](#y-40-downloads-weaker-checks-than-the-parsers-httpshttp-redirects) | Low | Downloads: weaker checks than the parsers, https→http redirects | open |
| [Y-41](#y-41-delivery-failures-5xx-retried-for-28-days-mx-fallback) | Low | Delivery failures: 5xx retried for 28 days, MX fallback | open |
| [Y-42](#y-42-remailer--replies-skip-the-block-list-and-lack-datemessage-id) | Low | `remailer-*` replies skip the block list and lack Date/Message-ID | open |
| [Y-43](#y-43-no-limit-on-recipients-per-exit-message) | Low | No limit on recipients per exit message | open |
| [Y-44](#y-44-no-pool-lock--s-next-to-a---daemon-remailer-sends-twice) | Low | No pool lock: `-S` next to a `--daemon` remailer sends twice | open |
| [Y-45](#y-45-config-silent-fallbacks-unknown-keys-no-value-checks) | Low | Config: silent fallbacks, unknown keys, no value checks | open |
| [Y-46](#y-46-client-exit-status-and-argument-checks) | Low | Client: exit status and argument checks | open |
| [Y-47](#y-47-the-client-keeps-the-from-address-and-reply-to) | Low | The client keeps the From address and Reply-To | open |
| [Y-48](#y-48-logs-reveal-recipients-and-chains) | Low | Logs reveal recipients and chains | open |
| [Y-49](#y-49-the-keyring-is-fetched-at-send-time) | Low | The keyring is fetched at send time | open |
| [Y-50](#y-50-smaller-mail-issues) | Info | Smaller mail issues | open |
| [Y-51](#y-51-smaller-client-and-documentation-issues) | Info | Smaller client and documentation issues | open |
| [Y-52](#y-52-crypto-notes-no-action-needed) | Info | Crypto notes (no action needed) | no action |

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
  - Done in `de59490` (branch `feat/multi-pinger`): with `urls.pingers`, the
    keyrings of several pingers are cross-checked and a key is only used if
    `urls.key_quorum` of them (default 2) publish it. A single pinger can no
    longer add or replace a key. On 2026-10-21 the three public pingers
    (mixmin, victor, frell) merged to the same six keys mixmin publishes.
  - Open: pingers still collect keys by unsigned mail, so a forged key that
    reaches every pinger is accepted. Signed pinger output and key pinning
    are planned with the go-echolot pinger (see its design).

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
- **Status:** fixed in `3bc21e1` (branch `fix/08-exit-abuse-controls`).
  `files.dest_block` (one address or `@domain` per line) is applied at
  delivery; `remailer.max_size` (kB, default 0 = no limit) is enforced on
  the exit's plaintext deliveries. Self-service opt-out requests by mail
  were left out on purpose: `From:` can be forged, so it would need a
  confirmation step. Tests: `abuse_test.go`.

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
- **Status:** fixed in `b364696` (branch `fix/09-replay-window`). `id_expire`
  below `max_age+1` is clamped to `max_age+2` with a warning; the default is
  now 16 days. The packet ID is recorded after the anti-tag and age checks.
  Tests: `replay_test.go`.

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
- **Status:** fixed in `40678e5` (branch `fix/10-malformed-files`). Bad stats
  lines are skipped ("Invalid latency"); secring date lines use
  `strings.CutPrefix`. Tests: `keymgr/malformed_test.go`.

### Y-11: Failed download overwrites a good keyring

- **Where:** `utils.go:148` (`httpGet`)
- **Impact:** A truncated download or an HTML error page served with status
  200 replaces a working pubring or stats file, leaving the client with no
  usable remailers.
- **Fix:** Download to a temporary file, check that it parses, then rename it
  over the old file.
- **Status:** fixed in `3e7f17c` (branch `fix/11-safe-download`). Downloads are
  checked with `keymgr.CheckPubring` / `keymgr.CheckStats` before anything is
  written, then written to a temp file and renamed into place. Tests:
  `keymgr/check_test.go`, `TestHTTPGetValidation`.

### Y-12: Non-yamn mail is never deleted

- **Where:** `pool.go:217-220` (`processMail`)
- **Impact:** Spam or other non-yamn mail accumulates in `Maildir/cur`
  forever.
- **Details:** `Unseen()` moves each message to `cur/`. If dearmoring fails,
  the loop continues without purging it, and nothing else ever cleans `cur/`.
- **Fix:** Purge the message on every path, or move unparseable mail to a
  separate folder with expiry.
- **Status:** fixed in `a1bf598` (branch `fix/12-maildir-rejected`). Mail that
  can't be processed is moved to `Maildir/.rejected` (readable with a mail
  client) and deleted there after `pool.max_age` days. Tests:
  `rejected_test.go`.

### Y-13: Config file is read from the working directory

- **Where:** `config/config.go` (`findConfig`)
- **Impact:** Running yamn in an untrusted directory can execute an arbitrary
  command, because `mail.pipe` in a planted `yamn.yml` is executed.
- **Details:** A `yamn.yml` in the current directory takes priority over
  `~/yamn.yml` and `/etc/yamn.yml`.
- **Fix:** Drop the working-directory lookup, or move it below the home and
  system locations and warn when it is used.
- **Status:** fixed in `d9393b0` (branch `fix/13-config-cwd`). The
  working-directory lookup is dropped (also via an unset `--dir`). Lookup
  order: `--config`, `$YAMNCFG`, `<--dir>/yamn.yml` (if set), `~/yamn.yml`,
  `/etc/yamn.yml`. An unused `./yamn.yml` triggers a warning pointing to
  `--config`. Tests: `config/config_test.go`.

### Y-21: Client ignores the message file argument

- **Where:** `config/config.go` (`ParseFlags`), read in `client.go` (`mixprep`)
- **Found:** 2026-10-20, while testing `--stdout` with the real binary (not
  part of the original review).
- **Impact:** `yamn -m message.txt` ignored the file and waited for the
  message on stdin; `yamn -m recipient message.txt` ignored both arguments.
- **Details:** `config.Flags.Args` was never filled. It broke when the flags
  moved into the config package (upstream `9d9edb5`, 2021-08-15), which
  replaced `flagArgs` (from `flag.Args()`) with `flags.Args` without setting
  it. Tests set `flag.Args` directly, so they didn't notice.
- **Status:** fixed in `13fcf17` (branch `fix/21-client-args`): `ParseFlags` sets
  `f.Args = flag.Args()`. Test: `TestFlagsArgs` in `config/config_test.go`.

### Y-23: Malformed pubring entries can stop a remailer

- **Where:** `keymgr/public.go` (`readPubring`: address and name not
  validated), `client.go` (`encodeMsg` calls `os.Exit(1)` when a hop is
  unknown), `packet.go` (`setNextHop` panics above 52 bytes); reached from
  the decode path through `dummy()` and `randhop` in `server.go`.
- **Impact:**
  - A pubring entry whose address has no `@` (and differs from its name)
    makes a running remailer exit with status 1 as soon as it picks that
    entry for a dummy or a randhop. `decodeSafely` can't catch `os.Exit`, so
    the remailer dies again after every restart.
  - Addresses over 52 bytes are imported and make `encodeMsg` panic.
  - Names aren't validated either. A name with `@` can't be looked up, `%`
    breaks the stats format, and with duplicate names the last one wins.
  - Whoever controls the keyring content can trigger this: one pinger, or a
    quorum with `urls.pingers`. A single malformed key can also do it by
    accident.
- **Fix:**
  - `readPubring` accepts only `local@domain` addresses of at most 52 bytes
    without spaces or control characters, and names matching `[a-z0-9]+`.
  - `encodeMsg` returns an error instead of exiting; dummies and randhops log
    and drop.
- **Found:** review 2, C-1 and K-20. Tests: `TestReviewC1PubringAddressWithoutAt`,
  `TestReviewC1LongAddressPanics`, `TestReviewDuplicateNameLastWins`.
- **Status:** fixed in `5d873f0` (branch `fix/23-pubring-validation`, #50).
  Tests: `TestEncodeMsgUnknownHop`, `TestImportPubringAmbiguous`,
  `TestLongPubringAddress`, `TestRandhopBadPubringAddress`,
  `TestReadPubringValidatesHeader`, `TestRealPubringsValid`,
  `TestSetNameAddressValidated`.

### Y-24: Quoted addresses inject ESMTP parameters past the block list

- **Where:** `mail.go` (`headToAddy` returns the unquoted addr-spec,
  `destBlocked`, `client.Rcpt`), `server.go` (`remailerFoo`).
- **Impact:**
  - `"victim@blocked.example> NOTIFY=NEVER ORCPT=rfc822;x"@b.example` is sent
    as `RCPT TO:<victim@blocked.example> NOTIFY=NEVER ORCPT=rfc822;x@b.example>`.
    A DSN-capable MTA reads that as recipient `victim@blocked.example`.
  - It gets past the destination block list (Y-08) and the per-address
    `remailer-*` reply limit (Y-19).
  - The `"x@y"@relay` and `x%y@relay` forms also pass the block list.
- **Fix:**
  - Accept only dot-atom local parts. Reject `<>()[]\",;:`, whitespace,
    control characters, more than one `@` (and optionally `%` and `!`), in
    `headToAddy` and in `remailerFoo`.
  - Check the block list against exactly the string passed to `Rcpt`.
- **Found:** review 2, M-1 (plus M-16: matching is exact only). Tests:
  `TestReviewRcptInjectionBypassesDestBlock`, `TestReviewDestBlockForms`.
  How real MTAs parse it is untested.
- **Status:** fixed in `9d0a28c` (branch `fix/24-rcpt-address`, #35). Tests:
  `TestMailPoolFileQuotedRecipient`, `TestRemailerFooQuotedFrom`,
  `TestValidRecipient`.

### Y-25: A bare CR reaches the SMTP wire

- **Where:** `mail.go` (`assemble`); `net/mail` keeps a lone `\r` inside
  header values.
- **Impact:**
  - `Reply-To: x@example\rFrom: ceo@bank.example` passes the exit header
    allow-list as a single Reply-To. A receiver that treats a lone CR as a
    line end sees an extra From.
  - `\r.\r` in the body passes Go's dot-stuffing unchanged. That's the
    precondition for SMTP smuggling against receivers that accept
    `<CR>.<CR>` as end of data.
- **Fix:**
  - Reject header values containing CR or NUL.
  - Normalise a lone CR in the body to CRLF.
  - Do the same in `stripClientHeaders`.
- **Found:** review 2, M-2. Tests: `TestReviewBareCRReachesWire`,
  `TestReviewBareCRHeaderOnWire`. No smuggling payload was written.
- **Status:** fixed in `f683da2` (branch `fix/25-bare-cr`, #36). Tests:
  `TestMailPoolFileBareCRBody`, `TestMailPoolFileBareCRHeader`,
  `TestStripClientHeadersBareCR`.

### Y-26: Pipe mode ignores the block list and drops Bcc

- **Where:** `mail.go` (`execSend` never gets `sendTo`); the docs suggest
  `/usr/sbin/sendmail -t`.
- **Impact:** with `sendmail -t`, recipients come from the headers.
  - Blocked recipients are still in To/Cc and get the mail, so the opt-out
    doesn't work at all in pipe mode.
  - `filterHeaders` has removed Bcc, so Bcc recipients get nothing.
- **Fix:**
  - Pass the recipients as arguments (`sendmail -i -- addr…`).
  - Stop documenting `-t`, and warn in `nagOperator` when the pipe command
    contains it.
- **Found:** review 2, M-3. Test: `TestReviewPipeIgnoresBlockListAndBcc`.
- **Status:** fixed in `1be63d6`, `ba4e52e` (branch `fix/26-pipe-recipients`,
  #37). Tests: `TestMailPoolFilePipeDashT`, `TestMailPoolFilePipeRecipients`,
  `TestPipeReadsHeaders`.

### Y-27: A message can stay in the pool forever

- **Where:** `pool.go` (`writePlainToPool`, `writeMessageToPool`),
  `utils.go` (`writeInternalHeader`), `mail.go` (`mailPoolFile`).
- **Impact:**
  - A plaintext that starts with a space or tab continues the internal
    `Yamn-Pooled-Date` header. The date no longer parses, and the file is
    kept on every run: it's never sent and never expires.
  - Disk use grows without bound.
  - Stuck files count toward `pool.size`, so a sender who plants some can
    flush real messages through almost alone. That weakens the mixing.
- **Fix:**
  - Keep the pooled date out of sender-controlled data (file name or mtime),
    or reject plaintext starting with SP/HT.
  - Treat an unparseable date as "expire by mtime", never "keep".
- **Found:** review 2, M-4. Test: `TestReviewPooledDateContinuationSticks`.
- **Status:** fixed in `7e4d2ef` (branch `fix/27-pooled-date`, #40). Tests:
  `TestPooledDateContinuation`, `TestPooledDateFallback`.

### Y-28: A slow mail server blocks the remailer (tarpit)

- **Where:** `pool.go` (`processMail` calls `remailerFoo` synchronously),
  `server.go` (`remailerFoo`: a reply is recorded only after a successful
  send), `pool.go` (the outbound pool sends one file at a time), `mail.go`
  (30 s dial plus 5 min session deadline).
- **Impact:**
  - A `remailer-help` request "from" an address whose mail server never
    answers blocks the main loop, and with it all inbound processing, for up
    to 5.5 minutes.
  - The failure isn't recorded, so the same address can repeat it. About 12
    mails an hour stop the remailer.
  - Exit mail to such domains stalls the serial outbound pool in the same
    way, for 28 days of retries.
- **Fix:**
  - Pool the `remailer-*` replies like other outbound mail.
  - Record the attempt before sending.
  - Give the outbound pool per-destination backoff or concurrent workers.
- **Found:** review 2, M-5. Test: `TestReviewFooTarpitBlocksMainLoop`.
- **Status:** fixed in `f52684e` (branch `fix/28-tarpit`, #45). Tests:
  `TestPoolSendConcurrent`, `TestRemailerFooDoesNotBlock`,
  `TestRemailerFooFailedSendCounted`.

### Y-29: Data files are read from the current directory without --dir

- **Where:** `config/config.go` (defaults are plain names; `resolvePaths`
  only applies with `--dir`), `client.go` (`timedURLFetch`).
- **Impact:**
  - Without `--dir`, the keyring, stats, per-pinger files and pool come from
    the current directory. That's Y-13 for data files: a planted
    `pubring.mix` there is used.
  - With `--config` alone, paths are relative to the current directory, not
    to the config file (the README suggests "--dir … or --config").
  - A file with an mtime in the future (tar and zip keep mtimes) is never
    downloaded again.
- **Fix:**
  - Without `--dir`, resolve paths against the config file's directory; with
    no config at all, use `~/.yamn`.
  - Treat a future mtime as stale.
- **Found:** review 2, K-1. Tests: `TestReviewPathEdgeCases`,
  `TestReviewFutureMtimeNeverFetched`.
- **Status:** fixed in `190c4df` (branch `fix/29-data-paths`, #59). Tests:
  `TestBaseDir`, `TestBaseDirAbsolutePaths`, `TestFetchFutureMtime`,
  `TestRelativePathsCoversFiles`.

### Y-30: `distance` is ignored for hops named in --chain

- **Where:** `chain.go` (`makeChain`): the first random hop ignores fixed hops
  to its left, and fixed hops are kept as names while candidates are
  addresses.
- **Impact:** a remailer can be used in two adjacent hops.
  - `alpha,*,*` repeated alpha in 106 of 400 runs.
  - `*,alpha,*` repeated it in about 197 of 400.
  - `*,*,exit` (the README example) is not affected.
- **Fix:** resolve every fixed hop to its address first, then exclude hops
  within `distance` on both sides.
- **Found:** review 2, K-2. Tests: `TestReviewDistanceWithNamedHop`,
  `TestReviewDistanceNamedMiddle`.
- **Status:** fixed in `d318037` (branch `fix/30-chain-distance`, #54). Tests:
  `TestChainDistanceNamedHops`, `TestChainNamedHopsUnchanged`,
  `TestChainNegativeDistance`.

### Y-31: Messages over 255 chunks are truncated

- **Where:** `client.go` (`mixprep`), `packet.go` (`setNumChunks` stores a
  `uint8`; `setChunkNum` panics).
- **Impact:**
  - At about 4.5 MB the chunk count wraps: part of the message is pooled
    (with 257 chunks, chunk 1 as a complete 1-chunk message), then the client
    panics.
  - The exit delivers a truncated message, and a retry delivers the part
    again.
  - `readMessage` has no size limit.
- **Fix:** reject more than 255 chunks (or a configurable limit) before
  anything is pooled, and return errors instead of panicking.
- **Found:** review 2, K-3. Test: `TestReviewTooManyChunks` (17,940 of
  4,585,060 bytes delivered).
- **Status:** fixed in `10c7f3a` (branch `fix/31-chunk-limit`, #55). Tests:
  `TestChunkLimit`, `TestChunkLimitExact`, `TestMessageReadLimit`.

### Y-32: Merged stats have no quorum

- **Where:** `keymgr/merge.go` (`mergeStatsSources`, `writeStats`).
- **Impact:**
  - With two fresh pingers, one decides the "worse" value of the median. It
    can set any remailer to 0% uptime and so remove it from random
    selection.
  - With one fresh pinger (the others older than `stale_hours`), that pinger
    decides all stats.
  - Stats dated in the future count as fresh.
- **Fix:**
  - Require `key_quorum` stats sources per remailer, falling back to older
    ones to reach that number.
  - With an even count, take the true median.
  - Reject a `Generated` time in the future.
- **Found:** review 2, K-4. Test: `TestReviewStatsOnePingerDecides`.
- **Status:** fixed in `acc3a29` (branch `fix/32-stats-quorum`, #52). Tests:
  `TestMedian`, `TestMergeStatsQuorum`.

### Y-53: Mail with several recipients goes to `smtp_relay`

- **Where:** `mail.go` (`smtpRelay`, `smtpSend`, `mailPoolFile`).
- **Impact:** with `mx_relay`, only a message with one recipient goes to the
  recipient's MX. One with more (To and Cc, or Bcc) goes to `smtp_relay`,
  whose default `fleegle.mixmin.net` belongs to the upstream author: it has
  no SMTP AUTH and answers `554 5.7.1 Relay access denied` to other hosts
  (checked 2026-10-22 from two addresses, without sending). An exit that kept
  the default retried such mail until `pool.max_age` and dropped it. Where a
  relay does take it, that server sees every recipient, Bcc included.
  Delivery is all or nothing: one `4xx` for one recipient (greylisting)
  stops the whole message, and a retry sends to everyone again.
- **Fix:** deliver to each recipient domain's MX, and remember in the pool
  file who already has the message, so a retry only sends to the rest.
- **Found:** review 2, M-18 (not in Y-50).
- **Status:** fixed in `da315a3` (branch `fix/53-multi-recipient`). With
  `mx_relay`, each recipient domain gets its own MX session; `smtpSend`
  reports per recipient, so a `4xx` only defers that recipient; after a
  partial delivery the pool file's first line notes the done recipients and a
  retry sends only to the rest; a warning at startup when mail would go to the
  default `fleegle.mixmin.net`. Tests: `TestMXSendPerDomain`,
  `TestMXSendPartialRetry`, `TestPoolDoneNotForgeable`,
  `TestParseInternalHeader`, `TestDeliveryFailures`, `TestNagDefaultRelay`.

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
- **Status:** fixed in `019b95c` (branch `fix/14-shuffle`). Tests:
  `TestShuffleUniform`, `TestRandIntsUniform` in `crandom/crandom_test.go`.

### Y-15: `num_copies` config setting is ignored

- **Where:** `client.go:136`
- **Details:** The `--copies` flag defaults to 2, so `flag.Copies == 0` is
  never true and `stats.num_copies` from the config is never used.
- **Fix:** Default the flag to 0 so the config value applies.
- **Status:** fixed in `f1d760e` (branch `fix/15-num-copies`). The flag defaults
  to "not given" and `num_copies` defaults to 2, so the effective default is
  unchanged; `num_copies` is now honoured, limited to 1-5. Tests:
  `copies_test.go`.

### Y-16: Packet size and bounds checks

- **Where:** `packet.go:653`, `packet.go:711`
- **Details:** `setPlainText` compares `m.plainLength` (always 0) instead of
  the new length, so its size check never fires. `getPartialIV` uses `>`
  where it needs `>=`. Both are internal and not reachable from received
  packets today.
- **Fix:** Compare the actual length; use `>=`.
- **Status:** fixed in `73de26b` (branch `fix/16-packet-bounds`). Tests:
  `bounds_test.go`.

### Y-17: Data race on `stats`

- **Where:** `stats.go`, `pool.go` (`emailPoolFile`), `server.go`
- **Details:** The `stats` counters are written from the main loop and from the
  `serverPoolOutboundSend` goroutine without synchronisation.
- **Fix:** Use a mutex or atomic counters.
- **Status:** fixed in `d94c550` (branch `fix/17-stats-race`). Atomic counters;
  the midnight reset swaps them to zero in one step. Tests: `stats_test.go`.

### Y-18: Resource leaks and small cleanups

- ~~`ImportPubring`, `ImportStats` and `Headers` (`keymgr/public.go`) never
  close their files.~~ Fixed in `205d80f`.
- ~~`smtpRelay` (`mail.go`) leaves the connection open on error paths.~~
  Fixed in `f894bf0` (Y-06).
- ~~`Chunk.Expire` (`chunker.go`) shifts the items with `copy` without
  shortening the slice, so the last chunk file is processed twice.~~ Fixed in
  `205d80f`, which also releases its iterator and stops double-deleting records
  with a corrupt timestamp.
- **Status:** fixed (branch `fix/18-resource-leaks`). Tests:
  `keymgr/leak_test.go`, `chunkexpire_test.go`.

### Y-19: `remailer-*` replies go to a forgeable address

- **Where:** `pool.go` (`processMail`), `server.go` (`remailerFoo`)
- **Details:** Replies to `remailer-key`, `remailer-help` and similar go to the
  incoming `From:` header, so anyone can direct them at a third party. This
  matches Mixmaster behaviour but could be rate-limited.
- **Status:** fixed in `5e5e2d1` (branch `fix/19-remailer-foo-limit`). One reply
  per address per UTC day, tracked as 64-bit keyed hashes (HMAC-SHA256 with
  a random startup key) rather than addresses. Also fixed: replies failed
  for a `From:` with a display name. Tests: `foo_test.go`.

### Y-20: Client does not strip identifying headers

- **Where:** `client.go` (`readMessage`), `mail.go` (`assemble`)
- **Details:** Headers such as `User-Agent`, `X-Mailer`, `Reply-To` and
  `Message-ID` in the user's message are passed through to the exit and
  delivered to the recipient.
- **Fix:** Strip known identifying headers in the client, or warn about them.
- **Status:** fixed in `0d01f6a` (branch `fix/20-client-headers`). The client
  keeps only what an exit passes on (plus Bcc), drops Date and Message-Id,
  reduces From to the bare address, and reports what it removed;
  `--keep-headers` turns it off. Stdin messages get the same treatment.
  Tests: `clientheaders_test.go`.

### Y-22: Relative paths in yamn.yml ignore `--dir`

- **Where:** `config/config.go` (`newConfig`, `ParseConfig`)
- **Found:** 2026-10-21, while running the patched Echolot pinger with yamn
  (not part of the original review).
- **Details:** `--dir` was only added to the built-in default paths. A
  relative path set in `yamn.yml` (e.g. `files: pubring: my.mix`) was used
  relative to the current directory. That contradicts
  `examples/yamn.yml.defaults` ("Paths are relative to --dir"). Echolot runs
  yamn from its own directory, so a pinger's one-key keyring set this way
  wasn't found.
- **Status:** fixed in `6886742` (branch `fix/dir-relative-paths`). The
  defaults are plain names; `ParseConfig` joins every relative path of the
  files section with `--dir` after reading the file, and keeps absolute
  paths. Tests: `TestDirRelativePaths`, `TestResolvePathsCoversFiles` in
  `config/config_test.go`.

### Y-33: Pool and chunk files are not written atomically

- **Where:** `pool.go` (`writeMessageToPool`, `writePlainToPool`),
  `chunker.go` (`Assemble`), `mbox.go` (`--import` into a live pool),
  `mail.go` (outfile).
- **Impact:**
  - Files get their final `m…` name before they're complete, while the
    outbound goroutine reads the same directory. A header-only file is
    deleted as "No email recipients found"; a partly written one is sent
    truncated.
  - Write and Close errors are ignored, nothing is fsynced, and `Assemble`
    skips missing chunks. The Maildir message has already been purged, so
    the message is lost.
  - Assembled plaintext and outfile files are created with 0666&umask
    instead of 0600.
- **Fix:** write to a temporary name (0600, O_EXCL), check every write, fsync,
  rename. `Assemble` fails and keeps the chunks.
- **Found:** review 2, M-7, C-3 and M-14. Tests:
  `TestReviewPartialAssembledFileVisible`, `TestReviewC3HeaderOnlyFileDeleted`,
  `TestReviewPartialPoolFileDeleted`.
- **Status:** fixed in `18fa9b6` (branch `fix/33-atomic-pool`, #41). Tests:
  `TestAssembleMissingChunk`, `TestForget`, `TestPoolFileModes`,
  `TestPoolReaderSeesNoPartialFile`, `TestProcessMailKeepsMailOnPoolError`.

### Y-34: Partial chunks have no storage limit

- **Where:** `server.go` (`smtpMethod`), `chunker.go` (`Insert`,
  `Housekeep`); defaults `max_size: 0`, `chunk_expire: 60`.
- **Impact:**
  - A sender can make an exit keep about 17.9 kB of plaintext per packet for
    60 days by sending chunks of messages that never complete.
  - A duplicate chunk orphans the earlier file for 120 days and refreshes the
    record's expiry.
  - Only the sender can do this, since the message ID is encrypted.
- **Fix:**
  - Cap the total size and number of partial chunks.
  - Shorter default expiry (Mixmaster uses about 7 days).
  - Keep the first copy of a duplicate.
  - Don't extend the expiry.
- **Found:** review 2, C-4. Tests: `TestReviewC4DuplicateChunkOrphansFile`,
  `TestReviewC4PartialChunksUnbounded`.
- **Status:** fixed in `357ca71` (branch `fix/34-chunk-limits`, #42). Tests:
  `TestChunkExpiryNotExtended`, `TestDuplicateChunkKeepsFirst`,
  `TestPartialChunkLimit`.

### Y-35: A running remailer keeps keys that left the keyring

- **Where:** `keymgr/public.go` (`ImportPubring` only adds; `KeyRefresh`
  panics on a missing file), `server.go` (hourly refresh).
- **Impact:**
  - Keys removed from `pubring.mix` (a remailer that left, or one that lost
    the Y-03 quorum) stay in a running daemon. They remain valid next hops
    (`checkNextHop`) and dummy and randhop targets.
  - Keys that expired after the import stay in use, so a randhop of real mail
    can be encrypted to a key the next remailer has already purged.
  - A remailer started without `pubring.mix` panics at its first hourly run.
- **Fix:** import into a fresh `Pubring` and swap it in, check `until` when
  using keys, and treat a missing file as "nothing to refresh".
- **Found:** review 2, C-6 and K-10. Tests: `TestReviewC6StaleKeysKept`,
  `TestReviewReimportKeepsRemovedKeys`, `TestReviewKeyRefreshPanicsOnMissingFile`.
- **Status:** fixed in `10bdce7` (branch `fix/35-stale-keys`, #49). Tests:
  `TestExpiredKeyNotUsed`, `TestKeyRefreshMissingFile`,
  `TestReimportDropsRemovedKeys`, `TestReimportRemovedNextHop`.

### Y-36: The advertised key is not checked against the secret keyring

- **Where:** `server.go` (`generateKeypair` publishes before writing the
  secret; `refreshPubkey`), `keymgr/secret.go` (`WriteMyKey`,
  `ImportSecring` skips keys "not valid yet").
- **Impact:** after a crash between the two writes, a key generated with the
  clock ahead, or a mismatched restore, `key.txt` advertises a key the
  remailer can't decrypt. It keeps doing so while any other key is active.
- **Fix:** write the secret first, and check the advertised keyid against the
  secret keyring at startup.
- **Found:** review 2, C-2. Test: `TestReviewC2AdvertisedKeyNotInSecring`.
- **Status:** fixed in `facd846` (branch `fix/36-advertised-key`, #48). Tests:
  `TestGenerateKeypairSecretFirst`, `TestStartupKeysAdvertisedNotInSecring`,
  `TestStartupKeysKeepsGoodKey`, `TestStartupKeysPublicKeyMismatch`.

### Y-37: Secret keyring handling: purge, crash safety, dates

- **Where:** `keymgr/secret.go` (`Purge`, `ImportSecring`).
- **Impact:**
  - `Purge` leaves the key it just purged in `secring.mix.tmp` until the next
    purge, which weakens forward secrecy.
  - It renames the secring away before writing the new one, without sync. A
    disk-full panic followed by a restart can lose all keys.
  - It writes dates in local time, so `Expires` can be a day off from
    `key.txt`.
  - `ImportSecring` accepts a secret-key line with trailing non-hex
    (`continue` missing).
- **Fix:**
  - Write `secring.mix.new`, Sync, rename over the original, and drop the
    `.tmp` copy.
  - Use UTC.
  - Add the `continue`.
- **Found:** review 2, C-5 and C-8. Tests: `TestReviewC5PurgeKeepsPurgedKeyInTmp`,
  `TestReviewC5PurgeWritesLocalDates`, `TestReviewC8SecretKeyHexNotChecked`.
- **Status:** fixed in `ec384eb` (branch `fix/37-secring`, #47). Tests:
  `TestImportSecringBadSecretHex`, `TestPurgeRemovesPurgedKey`,
  `TestPurgeWritesUTCDates`, `TestWriteFileSyncFailureKeepsOriginal`.

### Y-38: One corrupt idlog entry stops every startup

- **Where:** `idlog/idlog.go` (`Expire` panics on a `GobDecode` error);
  `Expire` runs at startup and at midnight, outside `decodeSafely`.
- **Impact:** one corrupt LevelDB value crashes every start. The obvious
  workaround, deleting the idlog, reopens the replay window.
- **Also:** errors are compared as strings (`"leveldb: not found"`).
- **Fix:** log and delete the bad entry, and use
  `errors.Is(err, leveldb.ErrNotFound)`.
- **Found:** review 2, C-7. Test: `TestReviewC7ExpirePanicsOnBadValue`.
- **Status:** fixed in `33a7b1f` (branch `fix/38-idlog-expire`, #46). Tests:
  `TestExpireBadValue`.

### Y-39: Multi-pinger merge: quorum shrinks, ties go to the freshest pinger

- **Where:** `keymgr/merge.go` (`res.Quorum = min(quorum, usable)`;
  `pickHeader`).
- **Impact:**
  - On a first run, while no pinger files exist yet, failing HTTPS pingers
    lower the quorum to the ones that answer (fail-open, logged as a
    warning).
  - On a header tie, the pinger with the newest (or future-dated) stats sets
    caps and dates: it can turn a middleman into an exit, or keep an expired
    `until`.
- **Fix:**
  - Don't go below `key_quorum` unless explicitly allowed.
  - Warn about `http://` pinger URLs.
  - Decide ties per field, conservatively: `M` if any source has it, the
    earliest `until`.
- **Found:** review 2, K-5 and K-6. Test: `TestReviewHeaderTieFutureStats`.
- **Status:** fixed in `e03aacd` (branch `fix/39-merge-quorum`, #53). Tests:
  `TestMergeHeaderTie`, `TestMergeQuorumNotLowered`,
  `TestRefreshKeyringPlainHTTPWarning`.

### Y-40: Downloads: weaker checks than the parsers, https→http redirects

- **Where:** `keymgr/check.go` (`CheckPubring`, `CheckStats`), `utils.go`
  (`httpGet` uses the default `CheckRedirect`).
- **Impact:**
  - A truncated stats table, a keyring with a line over 64 KB, or a keyring
    with no valid key passes the check and replaces a working file, so Y-11
    is incomplete.
  - An https URL can be redirected to plain http.
- **Fix:** validate with `readStats`/`readPubring` and require a key valid now,
  and refuse redirects that downgrade to http.
- **Found:** review 2, K-11 and M-11. Tests: `TestReviewCheckWeakerThanParser`,
  `TestReviewHTTPGetDowngradeRedirect`.
- **Status:** fixed in `db79575` (branch `fix/40-download-checks`, #51).
  Tests: `TestCheckLikeParser`, `TestHTTPGetDowngradeRedirect`.

### Y-41: Delivery failures: 5xx retried for 28 days, MX fallback

- **Where:** `mail.go` (`mailPoolFile`, `smtpSend`, `mxLookup`), `pool.go`.
- **Impact:**
  - A permanent 5xx rejection is retried every pool run until `max_age`
    (28 days), with no bounce. One rejected RCPT stops delivery to all
    recipients.
  - On Null MX (RFC 7505), SERVFAIL and NXDOMAIN, the MX lookup falls back to
    the domain's A record.
- **Fix:**
  - Drop or dead-letter on 5xx; skip rejected RCPTs; retry only 4xx and
    network errors.
  - Treat Null MX as permanent and keep the file on temporary DNS errors.
- **Found:** review 2, M-6 and M-8. Tests: `TestReviewPermanentFailureKept`,
  `TestReviewMXLookup`.
- **Status:** fixed in `a32d44a`, `2381a01` (branch
  `fix/41-delivery-failures`, #43). Tests: `TestDeliveryFailures`,
  `TestDeliveryFailuresDirectMX`, `TestMXLookupFailures`.

### Y-42: `remailer-*` replies skip the block list and lack Date/Message-ID

- **Where:** `server.go` (`remailerFoo`), `quickmail/quickmail.go`.
- **Impact:** blocked addresses still get help, key and conf replies. Replies
  have only From, To and Subject, so some MTAs reject or spam-score them.
- **Fix:** check `destBlocked`, and add Date, Message-ID, MIME-Version and
  Content-Type.
- **Found:** review 2, M-9. Tests: `TestReviewFooIgnoresDestBlock`,
  `TestReviewFooReplyHeaders`.
- **Status:** fixed in `b937780` (branch `fix/42-reply-headers`, #38). Tests:
  `TestRemailerFooDestBlock`, `TestRemailerFooReplyHeaders`.

### Y-43: No limit on recipients per exit message

- **Where:** `mail.go` (`mailPoolFile`). Upstream behaves the same.
- **Impact:** one anonymous message can go to any number of To/Cc/Bcc
  addresses. 500 addresses gave 500 RCPT commands, which makes the exit a
  cheap spam amplifier.
- **Fix:** `mail.max_recipients` (default around 10), announced in
  `remailer-conf`.
- **Found:** review 2, M-10. Test: `TestReviewRecipientFanOut`.
- **Status:** fixed in `7107b69` (branch `fix/43-max-recipients`, #39). Tests:
  `TestMailPoolFileMaxRecipients`, `TestRemailerConfMaxRecipients`.

### Y-44: No pool lock: `-S` next to a `--daemon` remailer sends twice

- **Where:** `pool.go` (`poolOutboundSend` only checks its own process's
  flags).
- **Impact:** a manual `yamn -S` and a remailer started with `--daemon` can
  both deliver the same pool file before either removes it.
- **Fix:** flock `<pooldir>/.lock`, or claim each file by renaming it before
  sending.
- **Found:** review 2, M-12 (from reading the code).
- **Status:** fixed in `6545c02` (branch `fix/44-pool-lock`, #44). Tests:
  `TestPoolBatchLocked`, `TestPoolFlushLocked`.

### Y-45: Config: silent fallbacks, unknown keys, no value checks

- **Where:** `config/config.go` (`findConfig`, `ParseConfig`).
- **Impact:**
  - A missing `--config` or `$YAMNCFG` file silently falls back to
    `~/yamn.yml`, `/etc/yamn.yml` or the defaults.
  - Misspelt keys (`key_qorum`, `pinger:`) are ignored, and `files.config` in
    YAML overrides the config file name.
  - A negative `stats.distance` panics. `minlat`/`maxlat`, `minrel` and
    `key_quorum` aren't checked either.
- **Fix:**
  - Error out when an explicitly given config can't be read.
  - Decode with `KnownFields(true)` (or warn), and tag `Config` with
    `yaml:"-"`.
  - Validate the values on load.
- **Found:** review 2, K-7, K-8 and K-14. Tests: `TestReviewMissingConfigFallsBack`,
  `TestReviewUnknownKeys`, `TestReviewNegativeDistance`.
- **Status:** fixed in `6182aa3` (branch `fix/45-config-checks`, #60). Tests:
  `TestMissingExplicitConfig`, `TestUnknownOptions`, `TestValidate`.

### Y-46: Client: exit status and argument checks

- **Where:** `client.go` (`mixprep`), `chain.go` (`makeChain` calls
  `os.Exit`).
- **Impact:**
  - An unknown remailer or a failed import sends nothing and exits 0.
  - When the dummy's chain fails after the message was pooled, the client
    exits 1, so the user may send again.
  - A `makeChain` failure on copy 2..n leaves earlier copies pooled.
  - A message without recipients is pooled and dropped at the exit.
  - Chain elements aren't trimmed (`"*, *"` fails), and a third argument is
    ignored.
- **Fix:**
  - `makeChain` returns errors.
  - Encode everything first, then pool it.
  - A failed dummy is only a warning.
  - Refuse a message without recipients; trim and check the arguments.
- **Found:** review 2, K-9, K-15 and K-16. Tests: `TestReviewChainErrorExitZero`,
  `TestReviewDummyExitStatus`, `TestReviewNoRecipient`, `TestReviewChainWithSpaces`.
- **Status:** fixed in `b2b3976` (branch `fix/46-client-exit-status`, #56).
  Tests: `TestClientChainErrorExitStatus`, `TestClientChainWithSpaces`,
  `TestClientDummyFailureExitStatus`, `TestClientImportFailureExitStatus`,
  `TestClientNoRecipient`, `TestClientTooManyArgs`, `TestHasRecipient`,
  `TestParseChain`.

### Y-47: The client keeps the From address and Reply-To

- **Where:** `client.go` (`stripClientHeaders`), `mail.go` (`parseFrom` with
  `custom_from`).
- **Impact:** a message written in a mail program carries the real From and
  often Reply-To. Y-20 strips only the display name. `custom_from` exits
  deliver the address, and every exit delivers Reply-To.
- **Fix:** a design decision. Either drop From and Reply-To unless
  `--keep-headers` (or a `--from` pseudonym) is given, or warn clearly that
  they are sent.
- **Found:** review 2, K-12. Test: `TestReviewFromAddressKept`.
- **Status:** fixed in `3510062` (branch `fix/47-from-reply-to`). The client
  removes From and Reply-To by default and reports it; `--keep-from` and
  `--keep-reply-to` send them, `--keep-headers` still keeps everything. Tests:
  `TestStripClientHeaders`, `TestStripClientHeadersKeepFrom`,
  `TestMixprepStripsHeaders`, `TestMixprepKeepFrom`, `TestMixprepKeepHeaders`.

### Y-48: Logs reveal recipients and chains

- **Where:** `mail.go` (SMTP replies, MX hosts and blocked recipients at
  warn/info), `pool.go` (From and Subject of rejected mail), `client.go`
  (chains at info), `yamn.go` (the log file is created 0644; `--debug`
  prints the SMTP password).
- **Impact:** logs on exits and clients can link traffic with timestamps.
- **Fix:**
  - Log file names and SMTP codes, with details at trace only.
  - Create the log file 0600.
  - Mask the password in `--debug`.
- **Found:** review 2, M-13 and K-18 (from reading the code).
- **Status:** fixed in `36d0474` (branch `fix/48-client-logging`, #57). Tests:
  `TestClientChainNotLogged`, `TestDebugHidesPassword`, `TestLogFileMode`.

### Y-49: The keyring is fetched at send time

- **Where:** `client.go` (`refreshKeyring(false)` in `mixprep`), `pingers.go`.
- **Impact:** when the files are older than an hour, every `yamn -m` contacts
  each pinger right before sending. That shows the client's IP and send time,
  and blocks for up to 30 s per pinger that's down.
- **Fix:** recommend a scheduled `--refresh` with jitter, or add an option to
  never fetch at send time.
- **Found:** review 2, K-13 (from reading the code).
- **Status:** fixed in `6902217` (branch `fix/49-fetch-on-send`, #61). Tests:
  `TestFetchOnSend`.

### Y-54: Mail to `.onion` hosts can't be delivered

- **Where:** `mail.go` (`mxLookup`, `smtpSend`), `config/config.go`
  (`onion_relay`).
- **Impact:** yamn connects to mail servers directly and Go's resolver
  refuses `.onion` names (RFC 7686), so an address at a `.onion` domain, a
  `.onion` MX or a `.onion` `smtp_relay` can't be reached. Upstream's
  `onion_relay` only stopped skipping `.onion` MX hosts, which then failed
  to resolve. Since Y-50, mail to a `.onion` domain is refused at once.
- **Fix:** an option for Tor's SOCKS port, and delivery to `.onion` hosts
  through it, without DNS.
- **Found:** discussion after Y-50.
- **Status:** fixed in `436880c` and `ab102ec` (branch `fix/54-tor-onion`).
  New `mail.tor_socks`: `.onion` recipients, MX hosts and `smtp_relay` go
  through Tor's SOCKS5 port (`golang.org/x/net/proxy`, with Tor's onion
  reply codes described), never through DNS, with a random login per
  delivery for its own circuit; v3 addresses are checked (checksum);
  `.onion` counts as secure for TLS. `onion_relay` stays ignored, its warning
  points to `tor_socks`. Tested live through the Tor network. Tests:
  `TestValidOnion`, `TestTorDial`, `TestTorDialErrors`,
  `TestOnionRecipient`, `TestOnionRecipientWithoutTor`, `TestOnionMX`,
  `TestOnionViaRelay`, `TestOnionViaRelayPartial`, `TestOnionTLS`,
  `TestOnionRelayWithoutTor`, `TestNagTor`, `TestDescribeTorError`,
  `TestValidate`.

## Info

### Y-50: Smaller mail issues

- **`.onion`:**
  - `onion_relay` can't work with Go's pure resolver;
  - the `.onion` check is case-sensitive;
  - `.onion` recipients are retried for 28 days.
- **Block list:** matching is exact only (no subdomains, no IP literals).
- **Maildir:** a `Maildir.Unseen` error leaves messages stuck in `cur/`.
- **Config and protocol details:**
  - `require_tls` without `usetls` fails every delivery, with no warning.
  - EHLO is `localhost`.
  - Date and Message-ID use local time, so the time zone is visible.
  - The pipe command has no timeout.
  - `uniqueStrings` is case-sensitive.
  - The `remailer-conf` reply has an empty "filtered headers" line.
- **`.rejected`:** keeps bounces that quote delivered plaintext for 28 days.
- **Display names:** they can be spoofed under `custom_from: false`
  (`"security@bank" <remailer@…>`).
- **Found:** review 2, M-15 to M-20.
- **Status:** fixed in `f2f1910`, `13e55df`, `a13e71a`, `5d352fa`, `86af59b`,
  `8a5c483`, `d210c8d`, `08e3bf3`, `46a9568`, `8129593`, `55a9c74`, `c6b451c`
  (branch `fix/50-mail-details`). `.onion` MX hosts are always skipped
  (`onion_relay` is ignored with a warning) and `.onion` recipients fail
  permanently; `@domain` block entries stay exact and `@*.domain` matches
  subdomains (`%` and address literals were already refused by
  `validRecipient`); Maildir `cur/` is always processed; `require_tls` implies
  `usetls`; EHLO gives `mailDomain()`; Date and Message-ID are UTC; the pipe
  command is killed after 5 minutes and an empty one is an error; duplicate
  recipients ignore case; `remailer-conf` lists the passed headers;
  `.rejected` uses the new `pool.rejected_age` (7 days); a display name with
  `@` is replaced by `outbound_name`. Tests: `TestMXLookupFailures`,
  `TestDestBlock`, `TestProcessMailReadsCur`,
  `TestSmtpSendRequireTLSWithoutUseTLS`, `TestSmtpSendHello`,
  `TestMailDatesUTC`, `TestExecSendTimeout`, `TestUniqueAddresses`,
  `TestRemailerConfHeaders`, `TestExpireRejected`, `TestParseFromName`.

### Y-51: Smaller client and documentation issues

- The dummy follows `--chain`, i.e. the real message's path, and not
  `stats.chain`.
- Documentation and config mismatches:
  - the man page SYNOPSIS says `[-s]` (it should be `-S`);
  - `~` in YAML paths isn't expanded;
  - the fallback to stale stats isn't documented.
- `linebreaker` with a line length of 0 would recurse forever (only 64 is
  used).
- **Found:** review 2, K-17 and K-19.
- **Status:** fixed in `f221517` (branch `fix/51-client-docs`, #58). Tests:
  `TestDummyIgnoresChain`, `TestHomePaths`, `TestLineLengthBelowOne`,
  `TestManPageSynopsis`.

### Y-52: Crypto notes (no action needed)

- **Malleable bits:** only the 8 random padding bytes of the top header and
  the top bit of the sender's X25519 key can be changed without detection
  (exhaustive bit-flip test). A modified copy is a replay, and the top
  header is discarded at that hop.
- **Anti-tag comparison:** it uses `bytes.Equal`. There's no secret
  involved, so `subtle.ConstantTimeCompare` would only be hygiene.
- **Interop:** the packet format is unchanged from upstream.
  - Upstream 0.2.7 exits now assemble this fork's multi-chunk messages, but
    insert `Yamn-Pooled-Date` lines into the body (upstream's half of Y-05).
  - Upstream clients reuse one packet ID for all chunks, so every exit drops
    their chunks 2+.
- **Found:** review 2, C-9 to C-11.
- **Status:** no action needed.
