# Pinger keys

Pingers running [go-echolot](https://github.com/go-while/go-echolot) sign
the files yamn downloads (`pubring.mix`, `mlist2.txt`) with a minisign key
and publish `<file>.minisig` next to each. yamn checks these signatures when
the pinger's public key is configured (`urls.pinger_keys`, or `keys` in a
`urls.pingers` entry; see "Pinger signatures" in `yamn(1)`).

This list is where yamn users take the keys from. A key on a pinger's own web
site proves nothing: whoever can change the site can change the key. Keys are
added here by a reviewed pull request from the pinger's operator, and the
reviewer confirms the key with the operator by a second channel.

## Signing pingers

None yet.

<!--
Entry format:

### <name>

- Operator: <name or nick, contact>
- Pages: <https://…/> (pubring.mix, mlist2.txt, pinger.pub)
- Key: `RWQ…` (key ID <16 hex digits>)
- Added: <YYYY-MM-DD>, <how the key was confirmed>

Config:

    urls:
        pingers:
            - name: <name>
              pubring: https://…/pubring.mix
              mlist2: https://…/mlist2.txt
              keys: ["RWQ…"]
-->

## Checking a key by hand

    minisign -Vm pubring.mix -P RWQ…

shows the trusted comment (`timestamp`, `file`, `expires`) if the signature
is good.

## Key changes

A pinger that changes its key signs the new `pinger.pub` with the old key
(`pinger.pub.minisig`) and updates its entry here by pull request. List both
keys in your config until the new one is in use: yamn accepts a signature by
any key listed for a pinger.
