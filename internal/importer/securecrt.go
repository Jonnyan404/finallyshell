package importer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"finallyshell/internal/config"
)

// SecureCRT keeps its settings in its config folder, which its Global
// Options can move: by default %APPDATA%\VanDyke\Config on Windows,
// ~/Library/Application Support/VanDyke/SecureCRT/Config on macOS and
// ~/.vandyke/SecureCRT/Config on Linux. Its Sessions folder holds one
// .ini file per session, its subfolders being SecureCRT's folders:
//
//	Config/
//	  SSH2.ini                       the global SSH2 options, its key
//	  Sessions/
//	    __FolderData__.ini           the order of the folder's sessions
//	    Default.ini                  the settings new sessions start from
//	    web-1.ini                    the session "web-1", at the top level
//	    Production/__FolderData__.ini
//	    Production/db-1.ini          "db-1" in the folder Production
//
// The files are UTF-8, with a byte order mark in recent versions. Each
// line is an option, <type>:"<name>"=<value>: S a string, D a 32-bit
// number in hexadecimal (D:"[SSH2] Port"=00000016 is 22), B binary and Z
// a list, both with the byte or item count as the value and the bytes or
// items on the lines after it, each indented by a space:
//
//	S:"Protocol Name"=SSH2
//	D:"[SSH2] Port"=00000016
//	B:"Normal Font v2"=00000060
//	 f3 ff ff ff 00 00 00 00 00 00 00 00 00 00 00 00 90 01 00 00 ...
//	Z:"Description"=00000002
//	 first line
//	 second line
//
// The options read here (the others, as fonts, colors, port forwards and
// logon actions, are not imported):
//
//	Protocol Name          SSH2 or SSH1 is imported; Telnet, Serial,
//	                       RLogin, RDP and the others are not SSH
//	Hostname, Username
//	[SSH2] Port            [SSH1] Port for SSH1
//	Description            a Z list: the notes
//	Identity Filename V2   the session's key file (Identity Filename in
//	                       older versions): the path of the private key
//	                       or of its .pub, with options after "::", as
//	                       "::rawkey"; ${VDS_CONFIG_PATH} is the config
//	                       folder
//	Use Global Public Key  1: the key is SSH2.ini's Identity Filename V2,
//	                       not the session's
//	SSH2 Authentications V2  the methods in order, "password,publickey,…":
//	                       with the global key, publickey before password
//	                       is a key host (a rule of this reader, not
//	                       verified against SecureCRT's behavior or a
//	                       user's files)
//	Firewall Name          "None", the name of a proxy of the Global
//	                       Options (ProxyNotImported), or "Session:<path>"
//	                       for a jump host (JumpHostNotImported): neither
//	                       is imported
//
// Default.ini is skipped at the top of Sessions only: in a folder, it is a
// session named Default. The session's name is its file's, without .ini.
// The config folder is the parent of the Sessions folder chosen, or of
// the one the folder chosen is in. The key of a variable other than
// ${VDS_CONFIG_PATH}, a Windows path read on another system, or the
// global key when SSH2.ini could not be read, is a key to choose after
// importing (NeedsKey).
// Passwords (Password V2,
// Password in older versions) are encrypted with a key of SecureCRT's,
// or with the user's config passphrase: the parser keeps only the options
// above, so they are not even read into memory as options.
//
// SecureCRT's format has no documentation of VanDyke's, besides what its
// scripting examples show: IterateOverAllSessionsRecursively_*.vbs walk
// the Sessions folder, skip __FolderData__.ini and Default.ini, and read
// Hostname, Protocol Name and [SSH2] Port; ImportArbitraryDataFromFileTo
// SecureCRTSessions.py shows a session file with B lines, "Identity
// Filename V2" with ${VDS_USER_DATA_PATH}, and "Firewall Name" as
// "Session:JumpHost"; Get Configuration Path describes ${VDS_CONFIG_PATH}.
// The rest is from open source readers of the format: shcrt's crt2foss.py
// (depau/shcrt: S and D lines, ${VDS_CONFIG_PATH} in key paths), termius-
// cli's SecureCRT provider (Crystalnix/termius-cli: the "::" options and
// .pub of key paths), Termoso's securecrt.rs (termoso/termoso: indented Z
// items, Use Global Public Key, Session: firewalls), FeroSSH's
// securecrt.rs (jkobierczynski/FeroSSH: "path.pub::rawkey"), and a
// SecureCRT config folder published on GitHub (pzhaoyang/SecueCRTConfig),
// whose files have the byte order mark, the B and Z lines and the
// __FolderData__.ini above. It has not been checked against a user's
// files: parsing is lenient, lines not in the format are ignored, and a
// file without a single option is Broken.

// SecureCRT is the source of SecureCRT's sessions.
const SecureCRT Kind = "securecrt"

var secureCRTReader = &Reader{Kind: SecureCRT, Folder: true, Find: FindSecureCRT, Read: ReadSecureCRT}

// TooManyFiles is the first session file of a folder with more than an
// import reads (maxFiles), as a home folder chosen by mistake: it and the
// files after it are not read.
const TooManyFiles Reason = "too-many-files"

// SessionsDir is the name of SecureCRT's folder of sessions, in its config
// folder.
const SessionsDir = "Sessions"

// SecureCRTCandidates are the config folders SecureCRT keeps its settings
// in on this system. FindSecureCRT tries them; tests replace it.
var SecureCRTCandidates = func() []string { return secureCRTCandidates(runtime.GOOS, os.Getenv) }

// secureCRTCandidates are SecureCRT's config folders on goos, with env for
// the environment's variables.
func secureCRTCandidates(goos string, env func(string) string) []string {
	home := env("HOME")
	if goos == "windows" {
		home = env("USERPROFILE")
	}
	var out []string
	add := func(parts ...string) {
		if parts[0] != "" {
			out = append(out, filepath.Join(parts...))
		}
	}
	switch goos {
	case "windows":
		roaming := env("APPDATA")
		if roaming == "" && home != "" {
			roaming = filepath.Join(home, "AppData", "Roaming")
		}
		add(roaming, "VanDyke", "Config")
	case "darwin":
		add(home, "Library", "Application Support", "VanDyke", "SecureCRT", "Config")
	default:
		add(home, ".vandyke", "SecureCRT", "Config")
	}
	return out
}

// FindSecureCRT is the first of SecureCRTCandidates that has a Sessions
// folder: "" when none has. It reads the disk, so the app calls it off the
// UI thread.
func FindSecureCRT() string {
	for _, dir := range SecureCRTCandidates() {
		if isDir(filepath.Join(dir, SessionsDir)) {
			return dir
		}
	}
	return ""
}

// ReadSecureCRT reads the sessions of SecureCRT's config folder dir, or of
// its Sessions folder, or of a folder of it. Files that are not sessions
// are Problems, as are sessions it does not import; Entries are in folder
// order, then by name. It fails when dir cannot be read, or has no
// session's file at all.
func ReadSecureCRT(dir string) (*Source, error) { return readSecureCRT(dir, maxFiles) }

// readSecureCRT is ReadSecureCRT reading at most limit session files.
func readSecureCRT(dir string, limit int) (*Source, error) {
	read, root, configDir := secureCRTFolders(dir)
	if _, err := os.ReadDir(read); err != nil {
		return nil, err
	}
	r := &crtReader{src: &Source{Kind: SecureCRT, Path: read}, configDir: configDir, maxFiles: limit}
	if configDir != "" {
		if text, err := ReadText(filepath.Join(configDir, "SSH2.ini")); err == nil {
			r.globalKey, r.globalKnown = parseCRT(text).identity(), true
		}
	}
	r.walk(read, nil, 0, read == root)
	if r.sessionFiles == 0 {
		return nil, fmt.Errorf("%s: %w", read, ErrNoConnections)
	}
	sortSource(r.src)
	return r.src, nil
}

// secureCRTFolders are the folders of dir, the folder chosen: read, the
// folder to read (its Sessions folder for a config folder, else dir);
// root, the Sessions folder it is or is in, the nearest folder of that
// name from read up, else read itself, as a copy of a Sessions folder
// named otherwise; and configDir, the config folder, root's parent: ""
// when no Sessions folder was found. Entries' folders are those under
// read: choosing Sessions/Production imports its sessions at the top
// level.
func secureCRTFolders(dir string) (read, root, configDir string) {
	read = filepath.Clean(dir)
	if sub := filepath.Join(read, SessionsDir); isDir(sub) {
		return sub, sub, read
	}
	for p := read; ; p = filepath.Dir(p) {
		if strings.EqualFold(filepath.Base(p), SessionsDir) {
			return read, p, filepath.Dir(p)
		}
		if filepath.Dir(p) == p {
			return read, read, ""
		}
	}
}

type crtReader struct {
	src *Source
	// configDir is the config folder, for ${VDS_CONFIG_PATH}: "" when
	// not known.
	configDir string
	// globalKey is SSH2.ini's Identity Filename V2: "" for none.
	globalKey string
	// globalKnown is set when SSH2.ini was read, so that globalKey is
	// the global key.
	globalKnown bool
	// maxFiles is the most session files read: those after it are one
	// problem, TooManyFiles.
	maxFiles     int
	files        int
	sessionFiles int
}

// walk reads the folder abs, at depth, which is the folder path folder:
// nil for the folder read. root is set for the Sessions folder, whose
// Default.ini is no session.
func (r *crtReader) walk(abs string, folder []string, depth int, root bool) {
	entries, err := os.ReadDir(abs)
	if err != nil {
		if folder != nil {
			r.problem(strings.Join(folder, "/"), Unreadable, err)
		}
		return
	}
	for _, e := range entries {
		name := e.Name()
		path, rel := filepath.Join(abs, name), append(folder[:len(folder):len(folder)], name)
		if e.IsDir() {
			if depth < maxDepth {
				r.walk(path, rel, depth+1, false)
			}
			continue
		}
		ext := filepath.Ext(name)
		stem := name[:len(name)-len(ext)]
		if !strings.EqualFold(ext, ".ini") ||
			strings.EqualFold(stem, "__FolderData__") || root && strings.EqualFold(stem, "Default") {
			continue
		}
		r.sessionFiles++
		r.read(path, strings.Join(rel, "/"), stem, folder)
	}
}

// read reads the session's file at path, rel to the Sessions folder, the
// session named name in the folder path folder.
func (r *crtReader) read(path, rel, name string, folder []string) {
	r.files++
	if r.files > r.maxFiles {
		if r.files == r.maxFiles+1 { // once, for the first not read
			r.problem(rel, TooManyFiles, nil)
		}
		return
	}
	if st, err := os.Stat(path); err == nil && st.Size() > maxFileSize {
		r.problem(rel, TooLarge, nil)
		return
	}
	text, err := ReadText(path)
	if err != nil {
		r.src.Problems = append(r.src.Problems, ReadProblem(rel, err))
		return
	}
	s := parseCRT(text)
	if !s.some {
		r.problem(rel, Broken, errors.New("no SecureCRT options"))
		return
	}
	protocol := strings.ToUpper(s.str("Protocol Name"))
	switch protocol {
	case "", "SSH2", "SSH1": // none is SSH2, SecureCRT's default
	default:
		r.problem(rel, NotSSH, nil)
		return
	}
	addr := s.str("Hostname")
	if addr == "" {
		r.problem(rel, NoHost, nil)
		return
	}
	h := &config.Host{
		Name:  name,
		Addr:  addr,
		User:  s.str("Username"),
		Auth:  config.AuthPassword,
		Notes: strings.TrimSpace(strings.Join(s.lists["Description"], "\n")),
	}
	port, ok := s.num("[SSH2] Port")
	if protocol == "SSH1" {
		if p, ok1 := s.num("[SSH1] Port"); ok1 {
			port, ok = p, true
		}
	}
	if ok && port > 0 && port <= 65535 && port != 22 {
		h.Port = int(port)
	}
	e := &Entry{File: rel, Host: h, Folder: folder}
	switch key, global := r.key(s); {
	case global && !s.prefersKey():
		// The password is tried first: the global key is not imported.
	case global && !r.globalKnown:
		// SSH2.ini was not read: the global key is to choose.
		h.Auth, e.NeedsKey = config.AuthKey, true
	case key != "":
		if file, needsKey := secureCRTKey(key, r.configDir, runtime.GOOS); file != "" || needsKey {
			h.Auth, e.KeyFile, e.NeedsKey = config.AuthKey, file, needsKey
		}
	}
	switch fw := s.str("Firewall Name"); {
	case fw == "" || strings.EqualFold(fw, "None"):
	case len(fw) >= 8 && strings.EqualFold(fw[:8], "Session:"):
		e.Note = JumpHostNotImported
	default:
		e.Note = ProxyNotImported
	}
	r.src.Entries = append(r.src.Entries, e)
}

// key is the identity file the session s logs in with, as SecureCRT has
// it: its own, or the global one of SSH2.ini, when global is set. "" for
// none.
func (r *crtReader) key(s *crtSession) (key string, global bool) {
	if g, ok := s.num("Use Global Public Key"); ok && g != 0 {
		return r.globalKey, true
	}
	// Without the option, as in older versions, a key of the session's own
	// is the one.
	if own := s.identity(); own != "" {
		return own, false
	}
	return r.globalKey, true
}

func (r *crtReader) problem(rel string, reason Reason, err error) {
	r.src.Problems = append(r.src.Problems, NewProblem(rel, 0, reason, err))
}

// secureCRTKey is the private key's file that the identity file raw of
// SecureCRT names, for a host on goos, configDir being SecureCRT's config
// folder ("" for unknown): its options after "::" cut off, the private key
// for a .pub, ${VDS_CONFIG_PATH} replaced. needsKey is set when it names a
// key whose file this cannot tell: a path of another of SecureCRT's
// variables, or a Windows path on another system.
func secureCRTKey(raw, configDir, goos string) (file string, needsKey bool) {
	key, _, _ := strings.Cut(strings.TrimSpace(raw), "::")
	key = strings.TrimSpace(key)
	if len(key) > 4 && strings.EqualFold(key[len(key)-4:], ".pub") {
		key = key[:len(key)-4]
	}
	if key == "" {
		return "", false
	}
	const configVar = "${VDS_CONFIG_PATH}"
	if len(key) >= len(configVar) && strings.EqualFold(key[:len(configVar)], configVar) && configDir != "" {
		parts := strings.FieldsFunc(key[len(configVar):], func(r rune) bool { return r == '/' || r == '\\' })
		key = filepath.Join(append([]string{configDir}, parts...)...)
	}
	windowsPath := len(key) >= 3 && key[1] == ':' && (key[2] == '\\' || key[2] == '/') ||
		strings.HasPrefix(key, `\\`)
	if strings.Contains(key, "${") || goos != "windows" && windowsPath {
		return "", true
	}
	return key, false
}

// crtSession is the options of a SecureCRT .ini file that the reader
// uses: those of crtOptions, and no other.
type crtSession struct {
	strs  map[string]string
	nums  map[string]uint32
	lists map[string][]string
	// some is set when the file had an option of any name.
	some bool
}

// crtOptions are the options parseCRT keeps, with their types. Passwords
// are not among them, so they are never read into a crtSession.
var crtOptions = map[string]byte{
	"Protocol Name":           'S',
	"Hostname":                'S',
	"Username":                'S',
	"Identity Filename V2":    'S',
	"Identity Filename":       'S',
	"SSH2 Authentications V2": 'S',
	"Firewall Name":           'S',
	"[SSH2] Port":             'D',
	"[SSH1] Port":             'D',
	"Use Global Public Key":   'D',
	"Description":             'Z',
}

// maxCRTList is the most items of a Z list kept: no description is
// longer.
const maxCRTList = 1000

// parseCRT reads the options of the .ini file text that are crtOptions.
// Lines not in the format are ignored; an indented line is a line of the
// B or Z option above it.
func parseCRT(text string) *crtSession {
	s := &crtSession{strs: map[string]string{}, nums: map[string]uint32{}, lists: map[string][]string{}}
	list, left := "", 0 // the Z option being read, and its items still to read
	for line := range strings.Lines(text) {
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if list != "" && left > 0 {
				s.lists[list] = append(s.lists[list], line[1:])
				left--
			}
			continue
		}
		list = ""
		if len(line) < 4 || line[1] != ':' || line[2] != '"' {
			continue
		}
		name, value, ok := strings.Cut(line[3:], `"=`)
		if !ok {
			continue
		}
		s.some = true
		typ := line[0]
		if crtOptions[name] != typ {
			continue
		}
		value = strings.TrimSpace(value)
		switch typ {
		case 'S':
			s.strs[name] = value
		case 'D':
			if n, err := strconv.ParseUint(value, 16, 32); err == nil {
				s.nums[name] = uint32(n)
			}
		case 'Z':
			n, err := strconv.ParseUint(value, 16, 32)
			if err != nil || n > maxCRTList {
				n = maxCRTList
			}
			list, left = name, int(n)
			s.lists[name] = nil
		}
	}
	return s
}

// str is the string option name: "" for none.
func (s *crtSession) str(name string) string { return s.strs[name] }

// num is the number option name, if s has it.
func (s *crtSession) num(name string) (uint32, bool) {
	n, ok := s.nums[name]
	return n, ok
}

// identity is the identity file s names: "" for none.
func (s *crtSession) identity() string {
	if v := s.str("Identity Filename V2"); v != "" {
		return v
	}
	return s.str("Identity Filename")
}

// prefersKey tells whether s's authentication methods put publickey
// before password, or have publickey and no password.
func (s *crtSession) prefersKey() bool {
	key, password := -1, -1
	for i, m := range strings.Split(s.str("SSH2 Authentications V2"), ",") {
		switch strings.ToLower(strings.TrimSpace(m)) {
		case "publickey":
			if key < 0 {
				key = i
			}
		case "password":
			if password < 0 {
				password = i
			}
		}
	}
	return key >= 0 && (password < 0 || key < password)
}
