package keymgr

import (
	"bufio"
	"bytes"
	"crypto/ecdh"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	expiringDays int = 48 // Treat keys as expiring n hours before expired
	maxAddyLen   int = 52 // Max chars in remailer address
)

type secret struct {
	keyid []byte    // keyid
	sk    []byte    // Secret Key
	from  time.Time // Valid from
	until time.Time // Valid Until
}

type Secring struct {
	secringFile string // Filename of secret keyring
	pubkeyFile  string // Public keyfile (key.txt)
	sec         map[string]secret
	name        string        // Local remailer's name
	address     string        // Local remailer's email address
	myKeyid     []byte        // Keyid this remailer is advertising
	validity    time.Duration // Period of key validity
	grace       time.Duration // Period of grace after key expiry
	exit        bool          // Is this an Exit type remailer?
	version     string        // Yamn version string
}

// OpenAppend opens a file in Append mode and sets user-only permissions
func OpenAppend(name string) (f *os.File, err error) {
	f, err = os.OpenFile(name, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	return
}

// OpenCreate opens a new file in Write mode and sets user-only permissions
func OpenCreate(name string) (f *os.File, err error) {
	f, err = os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	return
}

// NewSecring is a constructor for the Secret Keyring
func NewSecring(secfile, pubkey string) *Secring {
	return &Secring{
		secringFile: secfile,
		pubkeyFile:  pubkey,
		sec:         make(map[string]secret),
	}
}

// ListKeyids returns a string slice of all in-memory secret keyids
func (s *Secring) ListKeyids() (keyids []string) {
	keyids = make([]string, 0, len(s.sec))
	for k := range s.sec {
		keyids = append(keyids, k)
	}
	return
}

// SetName validates and sets the remailer name
func (s *Secring) SetName(name string) {
	var err error
	l := len(name)
	if l < 2 || l > 12 {
		err = fmt.Errorf(
			"Remailer name must be 2 to 12 chars, not %d.",
			l,
		)
		panic(err)
	}
	s.name = strings.ToLower(name)
}

// SetAddress validates and sets the remailer address
func (s *Secring) SetAddress(addy string) {
	var err error
	l := len(addy)
	if l < 3 || l > maxAddyLen {
		err = fmt.Errorf(
			"Remailer address must be 2 to 52 chars, not %d.",
			l,
		)
		panic(err)
	}
	index := strings.Index(addy, "@")
	if index == -1 {
		err = fmt.Errorf(
			"%s: Remailer address doesn't contain an @.",
			addy,
		)
		panic(err)
	} else if index == 0 || l-index < 3 {
		err = fmt.Errorf("%s: Invalid remailer address.", addy)
		panic(err)
	}
	s.address = strings.ToLower(addy)
}

// SetExit defines if this is a Middle or Exit remailer
func (s *Secring) SetExit(exit bool) {
	s.exit = exit
}

// SetValidity defines the time duration over which a key is deemed valid
func (s *Secring) SetValidity(valid, grace int) {
	s.validity = time.Duration(24*valid) * time.Hour
	s.grace = time.Duration(24*grace) * time.Hour
}

// SetVersion sets the version string used on keys
func (s *Secring) SetVersion(v string) {
	s.version = "4:" + v
}

// Count returns the number of secret keys in memory
func (s *Secring) Count() int {
	return len(s.sec)
}

// Insert puts a new secret key into memory and returns its keyid
func (s *Secring) Insert(pub, sec []byte) (keyidstr string) {
	var err error
	if len(pub) != 32 {
		err = fmt.Errorf(
			"Invalid pubkey length. Wanted=32, Got=%d",
			len(pub),
		)
		panic(err)
	}
	if len(sec) != 32 {
		err = fmt.Errorf(
			"Invalid seckey length. Wanted=32, Got=%d",
			len(sec),
		)
		panic(err)
	}
	key := new(secret)
	/*
		Keyids are arbitrary, they only server to link public and
		secret keys in a manner that enables clients to know which
		public key to encrypt to and servers to know which secret key
		to use for decryption.  Using a truncated SHA256 of the public
		key provides the means for some, perhaps, useful validation
		that the client-held public key is not corrupt.
	*/
	key.keyid = makeKeyID(pub)
	keyidstr = hex.EncodeToString(key.keyid)
	// Validity dates.  Keyrings use UTC dates.
	key.from = time.Now().UTC()
	key.until = key.from.Add(s.validity)
	// The secret key itself
	key.sk = sec
	s.sec[keyidstr] = *key
	return
}

// WritePublic writes the Public Key to disk.
func (s *Secring) WritePublic(pub []byte, keyidstr string) {
	var err error
	if len(pub) != 32 {
		err = fmt.Errorf(
			"Invalid pubkey length. Wanted=32, Got=%d",
			len(pub),
		)
		panic(err)
	}

	var capstring string
	// M = Middle, E = Exit
	if s.exit {
		capstring += "E"
	} else {
		capstring += "M"
	}

	key, exists := s.sec[keyidstr]
	if !exists {
		err = fmt.Errorf("%s: Keyid does not exist", keyidstr)
		panic(err)
	}

	header := s.name + " "
	header += s.address + " "
	header += keyidstr + " "
	header += s.version + " "
	header += capstring + " "
	header += key.from.UTC().Format(date_format) + " "
	header += key.until.UTC().Format(date_format)

	// Open the file for writing
	f, err := os.Create(s.pubkeyFile)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	fmt.Fprintln(w, header)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "-----Begin Mix Key-----")
	fmt.Fprintln(w, keyidstr)
	fmt.Fprintln(w, hex.EncodeToString(pub))
	fmt.Fprintln(w, "-----End Mix Key-----")
	err = w.Flush()
	if err != nil {
		panic(err)
	}
}

// WriteSecret adds the selected secret key to the secret keyring file
func (s *Secring) WriteSecret(keyidstr string) {
	var err error
	key, exists := s.sec[keyidstr]
	if !exists {
		err = fmt.Errorf("%s: Keyid does not exist", keyidstr)
		panic(err)
	}
	f, err := OpenAppend(s.secringFile)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	keydata := "\n-----Begin Mixmaster Secret Key-----\n"
	keydata += fmt.Sprintf(
		"Created: %s\n",
		key.from.UTC().Format(date_format),
	)
	keydata += fmt.Sprintf(
		"Expires: %s\n",
		key.until.UTC().Format(date_format),
	)
	keydata += keyidstr + "\n"
	keydata += hex.EncodeToString(key.sk) + "\n"
	keydata += "-----End Mixmaster Secret Key-----\n"
	_, err = f.WriteString(keydata)
	if err != nil {
		panic(err)
	}
	// The key must be on disk before it's advertised.
	err = f.Sync()
	if err != nil {
		panic(err)
	}
}

// WriteMyKey writes the local public key to filename with current
// configurtaion settings.
func (s *Secring) WriteMyKey(filename string) (keyidstr string) {
	infile, err := os.Open(s.pubkeyFile)
	if err != nil {
		panic(err)
	}
	defer infile.Close()
	// Create a tmp file rather than overwriting directly
	outfile, err := os.Create(filename)
	if err != nil {
		panic(err)
	}
	defer outfile.Close()
	in := bufio.NewScanner(infile)
	out := bufio.NewWriter(outfile)
	var line string
	for in.Scan() {
		line = in.Text()
		elements := strings.Fields(line)
		if len(elements) == 7 {
			var capstring string
			// M = Middle, E = Exit
			if s.exit {
				capstring += "E"
			} else {
				capstring += "M"
			}
			// Extract the keyid so we can return it
			keyidstr = elements[2]
			if len(keyidstr) != 32 {
				err = fmt.Errorf(
					"Invalid public keyid length.  Expected=32, Got=%d.",
					len(keyidstr),
				)
				panic(err)
			}
			header := s.name + " "
			header += s.address + " "
			header += keyidstr + " "
			header += s.version + " "
			header += capstring + " "
			header += elements[5] + " "
			header += elements[6]
			fmt.Fprintln(out, header)
		} else {
			fmt.Fprintln(out, line)
		}
	}
	err = out.Flush()
	if err != nil {
		panic(err)
	}
	return
}

// CheckAdvertised returns an error unless the public key file advertises a
// key whose secret is in the keyring and not expired.  It returns the
// advertised keyid.
func (s *Secring) CheckAdvertised() (keyidstr string, err error) {
	f, err := os.Open(s.pubkeyFile)
	if err != nil {
		return
	}
	defer f.Close()
	var pub []byte
	phase := 0 // 0 header, 1 Begin cutmark, 2 keyid, 3 public key
	scanner := bufio.NewScanner(f)
	for phase < 4 && scanner.Scan() {
		line := scanner.Text()
		switch phase {
		case 0:
			if elements := strings.Fields(line); len(elements) == 7 {
				keyidstr = elements[2]
				phase++
			}
		case 1:
			if line == "-----Begin Mix Key-----" {
				phase++
			}
		case 2:
			phase++
		case 3:
			pub, _ = hex.DecodeString(line)
			phase++
		}
	}
	if err = scanner.Err(); err != nil {
		return
	}
	if phase < 4 {
		err = fmt.Errorf("%s: no public key found", s.pubkeyFile)
		return
	}
	key, exists := s.sec[keyidstr]
	if !exists {
		err = fmt.Errorf("%s advertises keyid %s, which is not in the secret keyring", s.pubkeyFile, keyidstr)
		return
	}
	if _, untilNextMidnight := midnights(key.until); time.Now().After(untilNextMidnight) {
		err = fmt.Errorf("%s advertises keyid %s, which expired on %s",
			s.pubkeyFile, keyidstr, key.until.UTC().Format(date_format))
		return
	}
	// The advertised public key must belong to the secret key.
	priv, err := ecdh.X25519().NewPrivateKey(key.sk)
	if err != nil {
		return
	}
	if !bytes.Equal(priv.PublicKey().Bytes(), pub) {
		err = fmt.Errorf("%s: the public key of keyid %s doesn't match its secret key", s.pubkeyFile, keyidstr)
	}
	return
}

// Return the Secret struct that corresponds to the requested Keyid
func (s *Secring) Get(keyid string) (sec secret, err error) {
	var exists bool
	sec, exists = s.sec[keyid]
	if !exists {
		err = fmt.Errorf(
			"%s: Keyid not found in secret keyring",
			keyid,
		)
		return
	}
	return
}

// Return the Secret Key that corresponds to the requested Keyid
func (s *Secring) GetSK(keyid string) (sk []byte, err error) {
	sec, exists := s.sec[keyid]
	if !exists {
		err = fmt.Errorf(
			"%s: Keyid not found in secret keyring",
			keyid,
		)
		return
	}
	sk = sec.sk
	return
}

// Purge deletes expired keys and writes current ones to a backup secring
func (s *Secring) Purge() (active, expiring, expired, purged int) {
	/*
		Keys exist in four possible states:-

		active  - Keys that are valid and not yet expiring
		expiring - Active keys that will expire soon
		expired - Expired keys, not yet purged from the Secring
		purged  - Keys that have just been deleted from the Secring
	*/

	// Running Purge with undefined validity parameters would probably
	// trash the Secret Keyring.  This tests prevents that unfortunate
	// circumstance.
	if s.validity == 0 || s.grace == 0 {
		err := errors.New("Cannot purge without validity parameters")
		panic(err)
	}

	if _, err := os.Stat(s.secringFile); err != nil {
		// The implication is that no secring file exists yet
		return
	}

	// Set the duration prior to expired that we consider "expiring".
	now := time.Now()
	expirePeriod := time.Duration(-expiringDays) * time.Hour

	var keydata strings.Builder
	var drop []string
	for _, k := range sortedKeys(s.sec) {
		m := s.sec[k]
		purgeDate := m.until.Add(s.grace)
		_, purgeNextMidnight := midnights(purgeDate)
		if now.After(purgeNextMidnight) {
			// Key has expired. Purge from memory and don't write
			// it back to disk.
			drop = append(drop, k)
			purged++
			continue
		}
		keydata.WriteString("-----Begin Mixmaster Secret Key-----\n")
		fmt.Fprintf(&keydata, "Created: %s\n", m.from.UTC().Format(date_format))
		fmt.Fprintf(&keydata, "Expires: %s\n", m.until.UTC().Format(date_format))
		keydata.WriteString(hex.EncodeToString(m.keyid) + "\n")
		keydata.WriteString(hex.EncodeToString(m.sk) + "\n")
		keydata.WriteString("-----End Mixmaster Secret Key-----\n\n")
		// Key dates on keyrings are in days so a we use nextMidnight
		// so that keys remain valid throughout their whole last day.
		_, untilNextMidnight := midnights(m.until)
		expiringThreshold := m.until.Add(expirePeriod)
		_, expiringNextMidnight := midnights(expiringThreshold)
		if now.After(untilNextMidnight) {
			expired++
		} else if now.After(expiringNextMidnight) {
			expiring++
		} else {
			active++
		}
	}
	// The old file stays in place until the new one is complete and on
	// disk, so a crash or a full disk can't lose the keys.
	if err := writeFileSync(s.secringFile, []byte(keydata.String())); err != nil {
		panic(err)
	}
	for _, k := range drop {
		delete(s.sec, k)
	}
	// Older versions left the previous secring here, purged keys
	// included.
	if err := os.Remove(s.secringFile + ".tmp"); err != nil && !errors.Is(err, os.ErrNotExist) {
		panic(err)
	}
	return
}

// writeFileSync replaces filename with data: it writes filename.new (mode
// 0600), syncs it and renames it over filename.
func writeFileSync(filename string, data []byte) (err error) {
	tmp := filename + ".new"
	// A leftover from a crash holds nothing the original doesn't.
	if err = os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return
	}
	f, err := OpenCreate(tmp)
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if _, err = f.Write(data); err != nil {
		return
	}
	if err = f.Sync(); err != nil {
		return
	}
	if err = f.Close(); err != nil {
		return
	}
	if err = os.Rename(tmp, filename); err != nil {
		return
	}
	// Make the rename itself durable.
	if d, derr := os.Open(filepath.Dir(filename)); derr == nil {
		d.Sync()
		d.Close()
	}
	return
}

// ImportSecring reads a YAML secring.mix file into memory
func (s *Secring) ImportSecring() (err error) {
	var f *os.File
	f, err = os.Open(s.secringFile)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	var line string        //Each line within secring.mix
	var skdata []byte      // Decoded secret key
	var keyidMapKey string // String representation of keyid to key map with
	var valid time.Time
	var expire time.Time
	var sec *secret
	now := time.Now().UTC()
	nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	key_phase := 0
	/* Key phases are:
	0 Expecting Begin cutmark
	1 Expecting Valid-from date
	2 Expecting Valid-to date
	3 Expecting Keyid line
	4	Expecting secret key
	5 Got End cutmark
	*/

	for scanner.Scan() {
		line = scanner.Text()
		switch key_phase {
		case 0:
			// Expecting begin cutmark
			if line == "-----Begin Mixmaster Secret Key-----" {
				sec = new(secret)
				key_phase = 1
			}
		case 1:
			// Valid-from date
			if date, ok := strings.CutPrefix(line, "Created: "); ok {
				valid, err = time.Parse(date_format, date)
				if err != nil {
					fmt.Fprintln(
						os.Stderr,
						"Malformed Created date",
					)
					key_phase = 0
					continue
				}
			} else {
				fmt.Fprintln(
					os.Stderr,
					"Expected Created line",
				)
				key_phase = 0
				continue
			}
			if valid.After(now) {
				// Key is not yet valid
				fmt.Fprintln(
					os.Stderr,
					"Key is not valid yet",
				)
				key_phase = 0
				continue
			}
			sec.from = valid
			key_phase = 2
		case 2:
			// Expire date
			if date, ok := strings.CutPrefix(line, "Expires: "); ok {
				expire, err = time.Parse(date_format, date)
				if err != nil {
					fmt.Fprintln(
						os.Stderr,
						"Malformed Expires date",
					)
					key_phase = 0
					continue
				}
			} else {
				fmt.Fprintln(
					os.Stderr,
					"Expected Expires line",
				)
				key_phase = 0
				continue
			}
			if expire.Before(nextMidnight) {
				// Key has expired (but we don't care)
			}
			sec.until = expire
			key_phase = 3
		case 3:
			if len(line) != 32 {
				// Invalid keyid length
				key_phase = 0
				continue
			}
			var keyid []byte
			keyid, err = hex.DecodeString(line)
			if err != nil {
				// Non hex keyid
				fmt.Fprintln(os.Stderr, err)
				key_phase = 0
				continue
			}
			sec.keyid = keyid
			// Retain a textual representation to key the secring
			// map with
			keyidMapKey = line
			key_phase = 4
		case 4:
			// Expecting Private key
			skdata, err = hex.DecodeString(line)
			if err != nil {
				// Non hex Private key
				fmt.Fprintln(os.Stderr, err)
				key_phase = 0
				continue
			}
			if len(skdata) != 32 {
				fmt.Fprintln(os.Stderr, "Incorrect key length")
				key_phase = 0
				continue
			}
			sec.sk = skdata
			key_phase = 5
		case 5:
			// Expecting end cutmark
			if line == "-----End Mixmaster Secret Key-----" {
				// Add the key to the Keyring
				s.sec[keyidMapKey] = *sec
				key_phase = 0
			}
		} // End of switch
	} // End of file lines loop
	return
}
