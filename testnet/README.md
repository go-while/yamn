# yamn test network

A private yamn network for testing: one remailer on each of the machines
in [`nodes.txt`](nodes.txt), with mail between them and nowhere else. It
isn't announced to the public pingers, and no mail can leave it.

## How it fits together

- **Names and addresses.** A node is named after the last two octets of
  its IP (10.10.11.101 is `11101`). Its domain is `<name>.yamn.test`, and
  its remailer is `mix@<name>.yamn.test`. `.test` is reserved and never
  resolves on the internet; `nodes.txt` maps names to IPs, so there is no
  DNS.
- **Mail.** [`tnode mx`](tnode) is a small SMTP relay (Go standard library
  only) on port 2525 of every node:
  - mail for its own domain goes into the Maildir of the local part, with
    anything after a `+` ignored (`pinger+token@` is `pinger@`); it adds
    `X-Original-To` with the address as received;
  - mail for another node's domain is forwarded to that node's relay,
    with retries for up to 48 hours;
  - **any other recipient is refused**, so nothing leaks out, and there
    are no bounces.

  yamn sends through it: `smtp_relay: 127.0.0.1`, port 2525.
- **Remailers.** One yamn daemon per node, as the user `yamn-test` (yamn
  refuses root), exit or middleman as `nodes.txt` says. The pool sends
  every 2 minutes whatever it has (`size: 0`, `min_send: 1`): a test
  network has too little traffic for the defaults.
- **Keyring and stats.** In step 1, the hub serves `pubring.mix` (the
  remailers' `key.txt` files, collected by `testnet.sh keyring`) and an
  `mlist2.txt` that lists every remailer as perfect, both over http on
  port 8080 ([`tnode www`](tnode)). In step 2, go-echolot pingers on the
  nodes with the `pinger` role measure the network and publish signed
  files; tnode delivers `pinger@` to `/var/lib/go-echolot/Maildir` and
  serves `/var/lib/go-echolot/results` at `/pinger/`.
- **The hub** also has the inbox (`inbox@<hub>.yamn.test`) and a yamn
  client that sends test messages through random chains.

## Use

`testnet.sh` runs on a workstation that reaches the nodes as root over ssh.
It builds static binaries locally, so nothing is compiled on the nodes.

```sh
./testnet.sh build     # static yamn (from this checkout) and tnode
./testnet.sh deploy    # copy them, write each node's configs
./testnet.sh init      # each remailer creates its keys
./testnet.sh keyring   # publish the keys on the hub
./testnet.sh start     # the hub first, then the others
./testnet.sh send 5    # 5 test messages from the hub to its inbox
./testnet.sh inbox     # what arrived (a 3-hop chain takes ~5-10 minutes)
./testnet.sh status    # daemons, pool, Maildir and spool per node
./testnet.sh log 11103 mx       # also www, yamn (daemon output) or remailer (yamn.log)
./testnet.sh stop
./testnet.sh clean     # stop and remove ~yamn-test/testnet
```

Commands that take nodes run on all of them without arguments, six at a
time (`PAR=`). On a node, everything is in `~yamn-test/testnet`: `bin/`,
`remailer/` (the yamn directory), `mx/spool`, `log/`, `run/` (pid files)
and, on the hub, `www/`, `inbox/` and `client/`. [`node.sh`](node.sh)
does the work there and can be run by hand as `yamn-test`.

## Forged-key test (go-echolot phase 4 milestone)

Run on 2026-10-25 with the three go-echolot pingers (11102, 11107, 11114).
go-echolot's session wrote a copy of 11114's `pubring.mix` in which
`mix@11103.yamn.test` has another key (`907bb07d` → `691a0cb7`, a new
public key with a matching keyid), signed with 11114's real key and the
daemon's trusted comment, at `/var/lib/go-echolot/results/forged/`
(served at `http://10.10.11.114:8080/pinger/forged/`; the pingers
themselves never read it). On the hub, `fclient` is a copy of `pclient`
with pinger 11114 pointed there:

| Case | Pingers reachable | Result |
|---|---|---|
| 1 | 11102, 11107 (honest), 11114 (forged) | 11103 keeps `907bb07d` (11102 + 11107); log: `pingers disagree: 11103 691a0cb7 (11114); 11103 907bb07d (11102, 11107)` |
| 2 | 11107 (honest), 11114 (forged) | neither key reaches quorum 2: 11103 is left out; the 15 keys both agree on are used |
| 3 | 11114 (forged) only | `only 1 of 3 pinger(s) have a usable keyring, key_quorum is 2; keeping the current one`; keyring unchanged |

The forged key was in no merged keyring. For case 2 and 3, the down
pingers' stored copies were removed first: a pinger that is down keeps
counting with its last accepted copy until its signature expires (48 hours
for go-echolot's keyrings).
