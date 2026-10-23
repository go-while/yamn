# Signing test vectors

Copied from go-echolot (github.com/go-while/go-echolot, `testdata/signing/`
at c5214c7), so yamn's verifier and go-echolot's signer are tested against
the same bytes. The secret test key isn't copied: yamn only verifies.

- `pinger.pub`: the test pinger's public key, minisign's two-line format.
- `pubring.mix`, `mlist2.txt`: signed files.
- `<file>.minisig`: go-echolot's signature.
- `<file>.minisign-tool.minisig`: the reference minisign 0.12 tool's
  signature of the same file, with the same key and trusted comment.

Signed at 2026-10-22 12:00:00 UTC (`timestamp:1792670400`); the keyring
expires 48 h later, the stats 6 h later.
