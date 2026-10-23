package config

import (
	"fmt"

	"github.com/crooks/yamn/minisign"
)

// PublicKeys parses minisign public keys (base64 lines).
func PublicKeys(keys []string) ([]minisign.PublicKey, error) {
	var pubs []minisign.PublicKey
	for _, k := range keys {
		p, err := minisign.ParsePublicKey(k)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", k, err)
		}
		pubs = append(pubs, p)
	}
	return pubs, nil
}

// checkPingerKeys checks the pinger keys and require_signatures.
func (c *Config) checkPingerKeys() (errs []error) {
	u := c.Urls
	if _, err := PublicKeys(u.PingerKeys); err != nil {
		errs = append(errs, fmt.Errorf("urls.pinger_keys: %w", err))
	}
	if len(u.Pingers) > 0 && len(u.PingerKeys) > 0 {
		errs = append(errs, fmt.Errorf("urls.pinger_keys is for urls.pubring and urls.mlist2; "+
			"with urls.pingers, give each pinger its keys"))
	}
	if u.RequireSignatures && len(u.Pingers) == 0 && len(u.PingerKeys) == 0 {
		errs = append(errs, fmt.Errorf("urls.require_signatures needs urls.pinger_keys"))
	}
	for _, p := range u.Pingers {
		if _, err := PublicKeys(p.Keys); err != nil {
			errs = append(errs, fmt.Errorf("urls.pingers %q keys: %w", p.Name, err))
		}
		if u.RequireSignatures && len(p.Keys) == 0 {
			errs = append(errs, fmt.Errorf("urls.require_signatures: pinger %q has no keys", p.Name))
		}
	}
	if u.MaxSignatureAge < 0 {
		errs = append(errs, fmt.Errorf("urls.max_signature_age_hours must be at least 0, not %d", u.MaxSignatureAge))
	}
	return
}
