package config

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config contains all the configuration settings for Yamn.
// Pinger is a source of remailer keys and stats.
type Pinger struct {
	Name    string `yaml:"name"`
	Pubring string `yaml:"pubring"`
	Mlist2  string `yaml:"mlist2"`
}

type Config struct {
	// BaseDir is the directory relative paths of the files section are
	// resolved against (see setBaseDir).  Empty means the current
	// directory.
	BaseDir string `yaml:"-"`
	// cwdFallback is set when the files are read from the current
	// directory instead of the config file's (see setBaseDir).
	cwdFallback bool
	General     struct {
		Loglevel     string `yaml:"loglevel"`
		LogToFile    bool   `yaml:"logtofile"`
		LogToJournal bool   `yaml:"logtojournal"`
	} `yaml:"general"`
	Files struct {
		// Config is the name of the config file in use.  It can't be set
		// in the config file.
		Config   string `yaml:"-"`
		Pubring  string `yaml:"pubring"`
		Mlist2   string `yaml:"mlist2"`
		Pubkey   string `yaml:"pubkey"`
		Secring  string `yaml:"secring"`
		Adminkey string `yaml:"adminkey"`
		Help     string `yaml:"help"`
		Pooldir  string `yaml:"pooldir"`
		Maildir  string `yaml:"maildir"`
		IDlog    string `yaml:"idlog"`
		ChunkDB  string `yaml:"chunkdb"`
		Logfile  string `yaml:"logfile"`
		// Destination block list: one address or "@domain" per line
		DestBlock string `yaml:"dest_block"`
	} `yaml:"files"`
	Urls struct {
		Fetch   bool   `yaml:"fetch"`
		Pubring string `yaml:"pubring"`
		Mlist2  string `yaml:"mlist2"`
		// Pingers to cross-check.  If set, they replace Pubring and Mlist2.
		Pingers []Pinger `yaml:"pingers"`
		// How many pingers must publish a key before it's used
		KeyQuorum int `yaml:"key_quorum"`
	} `yaml:"urls"`
	Mail struct {
		Sendmail     bool   `yaml:"sendmail"`
		Pipe         string `yaml:"pipe"`
		Outfile      bool   `yaml:"outfile"`
		UseTLS       bool   `yaml:"usetls"`
		RequireTLS   bool   `yaml:"require_tls"` // Refuse delivery without verified TLS
		SMTPRelay    string `yaml:"smtp_relay"`
		SMTPPort     string `yaml:"smtp_port"`
		MXRelay      bool   `yaml:"mx_relay"`
		OnionRelay   bool   `yaml:"onion_relay"`
		Sender       string `yaml:"sender"`
		Username     string `yaml:"username"`
		Password     string `yaml:"password"`
		OutboundName string `yaml:"outbound_name"`
		OutboundAddy string `yaml:"outbound_addy"`
		CustomFrom   bool   `yaml:"custom_from"`
		// Domain part of the Message-ID on mail sent by the exit; empty
		// means the domain of remailer.address.
		MessageDomain string `yaml:"message_domain"`
		// Extra headers an exit passes through, besides the built-in list
		AllowHeaders []string `yaml:"allow_headers"`
		// Most recipients an exit mails one message to; 0 = no limit
		MaxRecipients int `yaml:"max_recipients"`
	} `yaml:"mail"`
	Stats struct {
		Minlat     int     `yaml:"minlat"`
		Maxlat     int     `yaml:"maxlat"`
		Minrel     float32 `yaml:"minrel"`
		Relfinal   float32 `yaml:"rel_final"`
		Chain      string  `yaml:"chain"`
		Numcopies  int     `yaml:"num_copies"`
		Distance   int     `yaml:"distance"`
		StaleHrs   int     `yaml:"stale_hours"`
		UseExpired bool    `yaml:"use_expired"`
	} `yaml:"stats"`
	Pool struct {
		Size    int `yaml:"size"`
		Rate    int `yaml:"rate"`
		MinSend int `yaml:"min_send"`
		Loop    int `yaml:"loop"`
		// Delete excessively old messages from the outbound pool
		MaxAge int `yaml:"max_age"`
	} `yaml:"pool"`
	Remailer struct {
		Name        string `yaml:"name"`
		Address     string `yaml:"address"`
		Exit        bool   `yaml:"exit"`
		MaxSize     int    `yaml:"max_size"`
		IDexp       int    `yaml:"id_expire"`
		ChunkExpire int    `yaml:"chunk_expire"`
		ChunkLimit  int    `yaml:"chunk_limit"`
		MaxAge      int    `yaml:"max_age"`
		Keylife     int    `yaml:"key_life"`
		Keygrace    int    `yaml:"key_grace"`
		Daemon      bool   `yaml:"daemon"`
	} `yaml:"remailer"`
	// Warnings about the config file, such as unknown options, to be
	// logged once logging is set up.
	Warnings []string `yaml:"-"`
}

type Flags struct {
	Dir      string
	Debug    bool
	Client   bool
	Send     bool
	Refresh  bool
	Remailer bool
	Daemon   bool
	Chain    string
	To       string
	Subject  string
	Args     []string
	Config   string
	Copies   int
	Stdin    bool
	Stdout   bool
	Import   bool
	Dummy    bool
	NoDummy  bool
	// Send identifying headers (User-Agent, Date, ...) instead of removing them
	KeepHeaders bool
	// Send the message's From or Reply-To instead of removing it
	KeepFrom    bool
	KeepReplyTo bool
	Version     bool
	MemInfo     bool
}

// GetCfg parses the command line flags and config file if they haven't been previously parsed.
// This should be an init function but tests fail if flags are parsed in init.
//
//	See: https://github.com/golang/go/issues/46869
func GetCfg() (*Flags, *Config) {
	var err error
	f := ParseFlags()
	// Some config defaults are derived from flags so ParseConfig is a flags method
	c, err := f.ParseConfig()
	if err != nil {
		// No logging is defined at this point so log the error to stderr
		fmt.Fprintf(os.Stderr, "Unable to parse config file: %v\n", err)
		os.Exit(1)
	}
	return f, c
}

// WriteConfig will write the current config to a given filename
func (c *Config) WriteConfig(filename string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(filename, data, 0644)
	if err != nil {
		return err
	}
	return nil
}

// Debug returns the config as YAML for --debug.  The SMTP password is
// replaced by "(hidden)", so the output can be shared.
func (c *Config) Debug() ([]byte, error) {
	shown := *c
	if shown.Mail.Password != "" {
		shown.Mail.Password = "(hidden)"
	}
	data, err := yaml.Marshal(&shown)
	if err != nil {
		return nil, err
	}
	return data, err
}

func ParseFlags() *Flags {
	f := new(Flags)
	// Base DIR for easy setting of some default file paths
	flag.StringVar(&f.Dir, "dir", "", "Base DIR for YAMN files")
	// Function as a client
	flag.BoolVar(&f.Client, "mail", false, "Function as a client")
	flag.BoolVar(&f.Client, "m", false, "Function as a client")
	// Send (from pool)
	flag.BoolVar(&f.Send, "send", false, "Force pool send")
	flag.BoolVar(&f.Send, "S", false, "Force pool send")
	// Perform remailer actions
	flag.BoolVar(&f.Remailer, "remailer", false, "Perform routine remailer actions")
	flag.BoolVar(&f.Remailer, "M", false, "Perform routine remailer actions")
	// Start remailer as a daemon
	flag.BoolVar(&f.Daemon, "daemon", false, "Start remailer as a daemon. (Requires -M)")
	flag.BoolVar(&f.Daemon, "D", false, "Start remailer as a daemon. (Requires -M)")
	// Remailer chain
	flag.StringVar(&f.Chain, "chain", "", "Remailer chain")
	flag.StringVar(&f.Chain, "l", "", "Remailer chain")
	// Recipient address
	flag.StringVar(&f.To, "to", "", "Recipient email address")
	flag.StringVar(&f.To, "t", "", "Recipient email address")
	// Subject header
	flag.StringVar(&f.Subject, "subject", "", "Subject header")
	flag.StringVar(&f.Subject, "s", "", "Subject header")
	// Number of copies
	// 0 means "not given": the config's stats.num_copies applies
	flag.IntVar(&f.Copies, "copies", 0, "Number of copies (default: stats.num_copies)")
	flag.IntVar(&f.Copies, "c", 0, "Number of copies (default: stats.num_copies)")
	// Config file
	flag.StringVar(&f.Config, "config", "", "Config file")
	// Read STDIN
	flag.BoolVar(&f.Stdin, "read-mail", false, "Read a message from stdin")
	flag.BoolVar(&f.Stdin, "R", false, "Read a message from stdin")
	// Write to STDOUT
	flag.BoolVar(&f.Stdout, "stdout", false, "Write the encrypted mails to stdout (mbox) instead of the pool")
	// Import encrypted mails (e.g. --stdout output) into the pool
	flag.BoolVar(&f.Import, "import", false, "Read encrypted mails (mbox) from stdin into the pool; send with -S")
	// Inject dummy
	flag.BoolVar(&f.Dummy, "dummy", false, "Inject a dummy message")
	flag.BoolVar(&f.Dummy, "d", false, "Inject a dummy message")
	// Disable dummy messaging
	flag.BoolVar(&f.NoDummy, "nodummy", false, "Don't send dummies")
	// Keep identifying headers in client messages
	flag.BoolVar(&f.KeepHeaders, "keep-headers", false, "Don't remove headers that could identify you")
	flag.BoolVar(&f.KeepFrom, "keep-from", false, "Send the message's From header (removed by default)")
	flag.BoolVar(&f.KeepReplyTo, "keep-reply-to", false, "Send the message's Reply-To header (removed by default)")
	// Print Version
	flag.BoolVar(&f.Version, "version", false, "Print version string")
	flag.BoolVar(&f.Version, "V", false, "Print version string")
	// Print debug info
	flag.BoolVar(&f.Debug, "debug", false, "Print detailed config")
	// Memory usage
	flag.BoolVar(&f.MemInfo, "meminfo", false, "Log memory use with the hourly stats (loglevel info)")
	// Refresh remailer stats files
	flag.BoolVar(&f.Refresh, "refresh", false, "Refresh remailer stats files")

	flag.Parse()
	// The message file, or recipient and message file, for client mode
	f.Args = flag.Args()
	return f
}

// systemConfig is the system-wide config file, tried last.
var systemConfig = "/etc/yamn.yml"

// findConfig attempts to locate a yamn config file.  The current directory is
// deliberately not searched: a yamn.yml planted there could redirect the
// pubring download or SMTP relay, or run a command via mail.pipe.
func (f *Flags) findConfig() (string, error) {
	exists := func(name string) bool {
		_, err := os.Stat(name)
		return name != "" && err == nil
	}
	// A file given with --config or $YAMNCFG must exist: falling back to
	// another one would silently use other settings.
	if f.Config != "" {
		if !exists(f.Config) {
			return "", fmt.Errorf("--config %s: no such file", f.Config)
		}
		return f.Config, nil
	}
	if env := os.Getenv("YAMNCFG"); env != "" {
		if !exists(env) {
			return "", fmt.Errorf("YAMNCFG=%s: no such file", env)
		}
		return env, nil
	}
	// Is there a yamn.yml file in the --dir directory?  Only if --dir was
	// set: an empty --dir would mean the current directory.
	if f.Dir != "" {
		if cfgFile := path.Join(f.Dir, "yamn.yml"); exists(cfgFile) {
			return cfgFile, nil
		}
	}
	// Look for a yamn.yml in the user's homedir
	if home, err := os.UserHomeDir(); err == nil {
		if cfgFile := path.Join(home, "yamn.yml"); exists(cfgFile) {
			return cfgFile, nil
		}
	}
	// Last gasp: the system-wide config.
	if exists(systemConfig) {
		return systemConfig, nil
	}
	// Return an error to indicate no config file has been found
	return "", os.ErrNotExist
}

// IgnoredLocalConfig reports a yamn.yml in the current directory that isn't
// the config file in use (used, empty if none).  Earlier versions read it
// automatically; now it has to be passed with --config.
func IgnoredLocalConfig(used string) (string, bool) {
	local, err := filepath.Abs("yamn.yml")
	if err != nil {
		return "", false
	}
	localInfo, err := os.Stat(local)
	if err != nil {
		return "", false
	}
	if used != "" {
		if usedInfo, err := os.Stat(used); err == nil && os.SameFile(localInfo, usedInfo) {
			return "", false
		}
	}
	return local, true
}

// setBaseDir sets BaseDir, the directory relative paths of the files
// section are read from:
//   - --dir, if given;
//   - else the directory of the config file, if it was given with --config
//     or $YAMNCFG;
//   - else the current directory (an empty BaseDir), as before.
//
// Setups that run yamn from its directory with a config file elsewhere
// keep working: if the config file's directory has none of yamn's files
// and the current directory has, the current directory is used, with a
// warning (see PathWarnings).
func (c *Config) setBaseDir(f *Flags, cfgFile string) {
	if f.Dir != "" {
		c.BaseDir = f.Dir
		return
	}
	if cfgFile == "" || (cfgFile != f.Config && cfgFile != os.Getenv("YAMNCFG")) {
		return
	}
	dir := filepath.Dir(cfgFile)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	cwd, err := os.Getwd()
	if err == nil && cwd != dir && !c.hasDataFiles(dir) && c.hasDataFiles(cwd) {
		c.cwdFallback = true
		return
	}
	c.BaseDir = dir
}

// hasDataFiles reports whether dir holds one of the files a yamn
// directory has after its first run: the keyring, the secret keyring or
// the pool.  Only relative paths are looked for.
func (c *Config) hasDataFiles(dir string) bool {
	for _, p := range []string{c.Files.Pubring, c.Files.Secring, c.Files.Pooldir} {
		if p == "" || filepath.IsAbs(p) {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, p)); err == nil {
			return true
		}
	}
	return false
}

// relativePaths returns the paths of the files section that are still
// relative, i.e. relative to the current directory.
func (c *Config) relativePaths() (rel []string) {
	for _, p := range []string{
		c.Files.Pubring, c.Files.Mlist2, c.Files.Pubkey, c.Files.Secring,
		c.Files.Adminkey, c.Files.Help, c.Files.Pooldir, c.Files.Maildir,
		c.Files.IDlog, c.Files.ChunkDB, c.Files.Logfile, c.Files.DestBlock,
	} {
		if p != "" && !filepath.IsAbs(p) {
			rel = append(rel, p)
		}
	}
	return
}

// PathWarnings returns warnings about files read from the current
// directory, where someone else could have put a pubring.mix or similar.
func (c *Config) PathWarnings() (warnings []string) {
	if c.BaseDir != "" {
		return
	}
	rel := c.relativePaths()
	if len(rel) == 0 {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	if c.cwdFallback {
		warnings = append(warnings, fmt.Sprintf(
			"Reading yamn's files from the current directory %s, not from the "+
				"config file's directory %s, which has none of them. Pass "+
				"--dir to say where they are.",
			cwd, filepath.Dir(c.Files.Config),
		))
		return
	}
	if len(rel) > 3 {
		rel = append(rel[:3], "...")
	}
	warnings = append(warnings, fmt.Sprintf(
		"No --dir given: relative paths (%s) are read from the current "+
			"directory %s. Pass --dir, or set absolute paths in the files "+
			"section.",
		strings.Join(rel, ", "), cwd,
	))
	return
}

// newConfig returns a new instance of Config with some predefined defaults
func (f *Flags) newConfig() *Config {
	c := new(Config)
	// Default values defined here will be overridden by unmarshaling a config file
	c.General.Loglevel = "warn"
	c.General.LogToFile = false    // By default, log to stdout/stderr
	c.General.LogToJournal = false // Don't log to journal by default
	// Files are named relative to BaseDir; ParseConfig adds it (see
	// setBaseDir and resolvePaths).
	c.Files.Pubkey = "key.txt"
	c.Files.Pubring = "pubring.mix"
	c.Files.Secring = "secring.mix"
	c.Files.Mlist2 = "mlist2.txt"
	c.Files.Adminkey = "adminkey.txt"
	c.Files.Help = "help.txt"
	c.Files.Pooldir = "pool"
	c.Files.Maildir = "Maildir"
	c.Files.IDlog = "idlog"
	c.Files.ChunkDB = "chunkdb"
	c.Files.Logfile = "yamn.log"
	c.Files.DestBlock = "dest.blk"
	c.Urls.Fetch = true
	c.Urls.Pubring = "https://www.mixmin.net/yamn/pubring.mix"
	c.Urls.Mlist2 = "https://www.mixmin.net/yamn/mlist2.txt"
	c.Urls.KeyQuorum = 2
	c.Mail.Sendmail = false
	c.Mail.Outfile = false
	c.Mail.SMTPRelay = "fleegle.mixmin.net"
	c.Mail.SMTPPort = "587"
	c.Mail.UseTLS = true
	c.Mail.RequireTLS = false
	c.Mail.MXRelay = true
	c.Mail.OnionRelay = false // Allow .onion addresses as MX relays
	c.Mail.Sender = ""
	c.Mail.Username = ""
	c.Mail.Password = ""
	c.Mail.OutboundName = "Anonymous Remailer"
	c.Mail.OutboundAddy = "remailer@domain.invalid"
	c.Mail.CustomFrom = false
	c.Mail.MessageDomain = ""
	// Enough for personal mail, but one message can't be fanned out to
	// hundreds of addresses.
	c.Mail.MaxRecipients = 10
	c.Stats.Minrel = 98.0
	c.Stats.Relfinal = 99.0
	c.Stats.Minlat = 2
	c.Stats.Maxlat = 60
	c.Stats.Chain = "*,*,*"
	c.Stats.Numcopies = 2
	c.Stats.Distance = 2
	c.Stats.StaleHrs = 24
	c.Stats.UseExpired = false
	c.Pool.Size = 5 // Good for startups, too small for established
	c.Pool.Rate = 65
	c.Pool.MinSend = 5 // Only used in Binomial Mix Pools
	c.Pool.Loop = 300
	c.Pool.MaxAge = 28
	c.Remailer.Name = "anon"
	c.Remailer.Address = "mix@nowhere.invalid"
	c.Remailer.Exit = false
	c.Remailer.MaxSize = 0 // Max exit message size in kB, 0 = no limit
	// Keep packet IDs longer than packets are accepted (max_age), so a
	// packet can't be replayed after its ID expires.
	c.Remailer.IDexp = 16
	// The chunks of a message are sent together and arrive within hours.
	// Mixmaster keeps partial messages for about a week too.
	c.Remailer.ChunkExpire = 7
	// At most this many partial chunk files, up to 17910 bytes each (about
	// 18 MB in all).
	c.Remailer.ChunkLimit = 1000
	// Discard messages if packet timestamp exceeds this age in days
	c.Remailer.MaxAge = 14
	c.Remailer.Keylife = 14
	c.Remailer.Keygrace = 28
	c.Remailer.Daemon = false
	return c
}

// ParseConfig returns an instance of Config with defaults overridden by the content of a config file
func (f *Flags) ParseConfig() (*Config, error) {
	// Fetch an instance of Config with defaults predefined
	c := f.newConfig()
	// Try (really hard) to locate a yamn config file
	cfgFile, err := f.findConfig()
	if err != nil && err != os.ErrNotExist {
		return nil, err
	}
	if err == nil {
		// Useful for informing the user what config file is being read
		c.Files.Config = cfgFile
		yamlBytes, err := os.ReadFile(cfgFile)
		if err != nil {
			return nil, err
		}
		// Unmarshal the content of the YAML config file over the existing struct instance
		err = yaml.Unmarshal(yamlBytes, c)
		if err != nil {
			return nil, err
		}
		c.Warnings = append(c.Warnings, unknownOptions(cfgFile, yamlBytes)...)
		if err := c.validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", cfgFile, err)
		}
	}
	c.setBaseDir(f, cfgFile)
	c.resolvePaths(c.BaseDir)
	return c, nil
}

// unknownOptions returns a warning for every option in the YAML config data
// that yamn doesn't know, e.g. a misspelt one.  They are ignored, not an
// error, so a config file with an option of another yamn version still
// works.
func unknownOptions(name string, data []byte) (warnings []string) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var typeErr *yaml.TypeError
	if !errors.As(dec.Decode(new(Config)), &typeErr) {
		return
	}
	for _, msg := range typeErr.Errors {
		// "line 7: field key_qorum not found in type struct {...}"
		line, rest, ok := strings.Cut(msg, ": field ")
		option, _, found := strings.Cut(rest, " not found in type")
		if ok && found {
			warnings = append(warnings, fmt.Sprintf("%s, %s: unknown option %q, ignored", name, line, option))
		}
	}
	return
}

// validate checks the values that would make yamn fail or panic later.
func (c *Config) validate() error {
	var errs []error
	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}
	s := c.Stats
	check(s.Distance >= 0, "stats.distance must be at least 0, not %d", s.Distance)
	check(s.Minlat >= 0, "stats.minlat must be at least 0, not %d", s.Minlat)
	check(s.Maxlat >= s.Minlat, "stats.maxlat (%d) must be at least stats.minlat (%d)", s.Maxlat, s.Minlat)
	check(s.Minrel >= 0 && s.Minrel <= 100, "stats.minrel must be 0 to 100, not %g", s.Minrel)
	check(s.Relfinal >= 0 && s.Relfinal <= 100, "stats.rel_final must be 0 to 100, not %g", s.Relfinal)
	check(c.Urls.KeyQuorum >= 1, "urls.key_quorum must be at least 1, not %d", c.Urls.KeyQuorum)
	return errors.Join(errs...)
}

// resolvePaths makes the relative paths of the files section, from the
// defaults or from the config file, relative to dir (BaseDir).  Absolute
// paths are kept.  With an empty dir, relative paths stay relative to the
// current directory.
func (c *Config) resolvePaths(dir string) {
	for _, p := range []*string{
		&c.Files.Pubkey, &c.Files.Pubring, &c.Files.Secring, &c.Files.Mlist2,
		&c.Files.Adminkey, &c.Files.Help, &c.Files.Pooldir, &c.Files.Maildir,
		&c.Files.IDlog, &c.Files.ChunkDB, &c.Files.Logfile, &c.Files.DestBlock,
	} {
		// A leading "~/" is the home directory, as in a shell.
		*p = expandHome(*p)
		if dir != "" && *p != "" && !filepath.IsAbs(*p) {
			*p = filepath.Join(dir, *p)
		}
	}
}

// expandHome replaces a leading "~/" (or a lone "~") in p with the home
// directory.  "~user" isn't supported.  Without a home directory, p is
// returned as it is.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[1:])
}
