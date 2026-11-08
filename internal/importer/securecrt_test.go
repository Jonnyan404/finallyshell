package importer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"finallyshell/internal/config"
)

// The samples in testdata/securecrt are made up, in the shape of the
// files the sources in securecrt.go show: documentation addresses
// (192.0.2.x, 203.0.113.x, example.com), and passwords that are the hex of
// "sample-not-a-real-secret".

// crtSample is the sample config folder.
var crtSample = filepath.Join("testdata", "securecrt", "Config")

// crtEntry is the entry of src named name, failing the test without one.
func crtEntry(t *testing.T, src *Source, name string) *Entry {
	t.Helper()
	for _, e := range src.Entries {
		if e.Host.Name == name {
			return e
		}
	}
	t.Fatalf("no entry %q", name)
	return nil
}

func TestReadSecureCRT(t *testing.T) {
	src, err := ReadSecureCRT(crtSample)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(crtSample, "Sessions"); src.Path != want || src.Kind != SecureCRT {
		t.Errorf("read %s %q, want %q", src.Kind, src.Path, want)
	}
	var got []string
	for _, e := range src.Entries {
		got = append(got, e.FolderLabel()+"|"+e.Host.Title())
	}
	// In folder order, then by name; the top level first. Default.ini of
	// the top level is no session; one in a folder is.
	want := []string{"|web-1", "Lab|global-key", "Lab|legacy", "Production|bastion",
		"Production / Databases|db-1", "Production / Databases|Default", "实验室|树莓派"}
	if !slices.Equal(got, want) {
		t.Fatalf("entries %q, want %q", got, want)
	}

	// CRLF lines, a port of 22 in hexadecimal, the description as notes,
	// and the global key, which its methods try after the password.
	web := crtEntry(t, src, "web-1")
	if h := web.Host; h.Addr != "192.0.2.10" || h.Port != 0 || h.User != "root" || h.Auth != config.AuthPassword ||
		h.Notes != "front end\nrack 4" || h.ID != "" || h.Folder != "" || web.KeyFile != "" || web.NeedsKey || web.Note != "" ||
		web.File != "web-1.ini" || web.Folder != nil {
		t.Errorf("web-1 %+v %+v", web, h)
	}
	// Its own key, in the config folder, named by its .pub with options;
	// a jump host.
	bastion := crtEntry(t, src, "bastion")
	if h := bastion.Host; h.Addr != "bastion.example.com" || h.Port != 2222 || h.User != "deploy" || h.Auth != config.AuthKey ||
		bastion.KeyFile != filepath.Join(crtSample, "Keys", "bastion_ed25519") || bastion.NeedsKey ||
		bastion.Note != JumpHostNotImported || bastion.File != "Production/bastion.ini" {
		t.Errorf("bastion %+v %+v", bastion, h)
	}
	// Nested folders; the older option of the key, with options; a proxy;
	// an indented line that is not the host.
	db := crtEntry(t, src, "db-1")
	if h := db.Host; h.Addr != "db.example.com" || h.Port != 5522 || h.Auth != config.AuthKey ||
		db.KeyFile != "/home/ann/.ssh/id_rsa" || db.Note != ProxyNotImported || !slices.Equal(db.Folder, []string{"Production", "Databases"}) {
		t.Errorf("db-1 %+v %+v", db, h)
	}
	// SSH1 has a port of its own; a key in a folder of SecureCRT's that
	// is not known is chosen after importing.
	legacy := crtEntry(t, src, "legacy")
	if h := legacy.Host; h.Port != 23 || h.Auth != config.AuthKey || legacy.KeyFile != "" || !legacy.NeedsKey {
		t.Errorf("legacy %+v %+v", legacy, h)
	}
	// The global key of SSH2.ini, tried first, with backslashes.
	global := crtEntry(t, src, "global-key")
	if h := global.Host; h.Auth != config.AuthKey || global.KeyFile != filepath.Join(crtSample, "Keys", "Identity") || global.NeedsKey {
		t.Errorf("global-key %+v %+v", global, h)
	}
	if pi := crtEntry(t, src, "树莓派"); pi.Host.Addr != "192.0.2.20" || pi.Host.User != "pi" || !slices.Equal(pi.Folder, []string{"实验室"}) {
		t.Errorf("pi %+v", pi)
	}

	problems := map[string]Reason{}
	for _, p := range src.Problems {
		if _, dup := problems[p.File]; dup {
			t.Errorf("%s twice", p.File)
		}
		problems[p.File] = p.Reason
	}
	// __FolderData__.ini, Default.ini and notes.txt are not sessions, nor
	// problems.
	wantProblems := map[string]Reason{
		"broken.ini":      Broken,
		"Lab/no-host.ini": NoHost,
		"Lab/router.ini":  NotSSH,
		"Lab/console.ini": NotSSH,
	}
	if !maps(problems, wantProblems) {
		t.Errorf("problems %v, want %v", problems, wantProblems)
	}
	if !src.Problems[0].Reason.Failed() || src.Problems[len(src.Problems)-1].Reason.Failed() {
		t.Errorf("problems in order %v", src.Problems)
	}

	// Nothing of the passwords is kept.
	data, err := json.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range src.Entries {
		if e.Password != "" {
			t.Errorf("%s: a password", e.File)
		}
	}
	for _, secret := range []string{"73616d706c65", "sample", "02:", "198.51.100.99"} {
		if strings.Contains(string(data), secret) {
			t.Errorf("%q in what was read: %s", secret, data)
		}
	}
}

// The Sessions folder, or a folder of it, reads too.
func TestReadSecureCRTSessions(t *testing.T) {
	src, err := ReadSecureCRT(filepath.Join(crtSample, "Sessions"))
	if err != nil || len(src.Entries) != 7 {
		t.Fatalf("Sessions: %+v, %v", src, err)
	}
	// Its config folder is still known.
	if e := crtEntry(t, src, "bastion"); e.KeyFile != filepath.Join(crtSample, "Keys", "bastion_ed25519") {
		t.Errorf("bastion's key %q", e.KeyFile)
	}
	src, err = ReadSecureCRT(filepath.Join(crtSample, "Sessions", "Production"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range src.Entries {
		got = append(got, e.FolderLabel()+"|"+e.Host.Title())
	}
	// The folders are those under the folder chosen; the config folder is
	// the parent of the Sessions folder it is in.
	if want := []string{"|bastion", "Databases|db-1", "Databases|Default"}; !slices.Equal(got, want) || len(src.Problems) != 0 {
		t.Errorf("Production %q, problems %v; want %q", got, src.Problems, want)
	}
	if e := crtEntry(t, src, "bastion"); e.KeyFile != filepath.Join(crtSample, "Keys", "bastion_ed25519") || e.NeedsKey {
		t.Errorf("bastion %+v", e)
	}

	// A folder with Default.ini, not the Sessions folder: it is a session.
	src, err = ReadSecureCRT(filepath.Join(crtSample, "Sessions", "Production", "Databases"))
	if err != nil {
		t.Fatal(err)
	}
	got = nil
	for _, e := range src.Entries {
		got = append(got, e.FolderLabel()+"|"+e.Host.Title())
	}
	if want := []string{"|db-1", "|Default"}; !slices.Equal(got, want) {
		t.Errorf("Databases %q; want %q", got, want)
	}

	// Lab: SSH2.ini is read, and the global key found.
	src, err = ReadSecureCRT(filepath.Join(crtSample, "Sessions", "Lab"))
	if err != nil {
		t.Fatal(err)
	}
	if e := crtEntry(t, src, "global-key"); e.Host.Auth != config.AuthKey || e.KeyFile != filepath.Join(crtSample, "Keys", "Identity") || e.NeedsKey {
		t.Errorf("Lab's global-key %+v %+v", e, e.Host)
	}
}

// A Sessions folder without a config folder around it: its Default.ini
// is still no session, and a session of the global key, which SSH2.ini
// would name, has a key to choose.
func TestReadSecureCRTNoConfig(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "copied sessions")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join(crtSample, "Sessions", "Lab"))); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "Default.ini"), []byte("S:\"Hostname\"=192.0.2.250\n"), 0o600)
	src, err := ReadSecureCRT(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(src.Entries) != 2 {
		t.Errorf("entries %+v", src.Entries)
	}
	if e := crtEntry(t, src, "global-key"); e.Host.Auth != config.AuthKey || e.KeyFile != "" || !e.NeedsKey {
		t.Errorf("global-key %+v %+v", e, e.Host)
	}
	// Its own key, of a variable not known, is to choose too.
	if e := crtEntry(t, src, "legacy"); e.Host.Auth != config.AuthKey || !e.NeedsKey {
		t.Errorf("legacy %+v %+v", e, e.Host)
	}
}

func TestSecureCRTFolders(t *testing.T) {
	cfg := filepath.Join("a", "Config")
	for _, c := range []struct{ dir, read, root, configDir string }{
		{crtSample, filepath.Join(crtSample, "Sessions"), filepath.Join(crtSample, "Sessions"), crtSample},
		{filepath.Join(cfg, "Sessions"), filepath.Join(cfg, "Sessions"), filepath.Join(cfg, "Sessions"), cfg},
		{filepath.Join(cfg, "sessions", "Lab", "Pi") + string(filepath.Separator), filepath.Join(cfg, "sessions", "Lab", "Pi"), filepath.Join(cfg, "sessions"), cfg},
		{filepath.Join("a", "copy"), filepath.Join("a", "copy"), filepath.Join("a", "copy"), ""},
	} {
		read, root, configDir := secureCRTFolders(c.dir)
		if read != c.read || root != c.root || configDir != c.configDir {
			t.Errorf("%s: %q %q %q, want %q %q %q", c.dir, read, root, configDir, c.read, c.root, c.configDir)
		}
	}
}

func TestReadSecureCRTFails(t *testing.T) {
	dir := t.TempDir()
	// Only what is not a session.
	os.WriteFile(filepath.Join(dir, "Default.ini"), []byte(`S:"Hostname"=192.0.2.1`), 0o600)
	os.WriteFile(filepath.Join(dir, "__FolderData__.ini"), []byte(`S:"Session List"=`), 0o600)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(`S:"Hostname"=192.0.2.1`), 0o600)
	if _, err := ReadSecureCRT(dir); !errors.Is(err, ErrNoConnections) {
		t.Errorf("a folder without sessions: %v", err)
	}
	if _, err := ReadSecureCRT(filepath.Join(dir, "missing")); err == nil || errors.Is(err, ErrNoConnections) {
		t.Errorf("a missing folder: %v", err)
	}
	// A large file is not read.
	big := filepath.Join(dir, "big.ini")
	os.WriteFile(big, []byte(`S:"Hostname"=192.0.2.1`+"\n"+strings.Repeat("x", maxFileSize)), 0o600)
	src, err := ReadSecureCRT(dir)
	if err != nil || len(src.Entries) != 0 || len(src.Problems) != 1 || src.Problems[0].Reason != TooLarge {
		t.Errorf("a large file: %+v, %v", src, err)
	}
}

// Past the limit of files, the files are one problem, which fails.
func TestReadSecureCRTTooMany(t *testing.T) {
	src, err := readSecureCRT(crtSample, 3)
	if err != nil {
		t.Fatal(err)
	}
	var many []string
	for _, p := range src.Problems {
		if p.Reason == TooManyFiles {
			many = append(many, p.File)
		}
	}
	if len(many) != 1 || !TooManyFiles.Failed() {
		t.Errorf("too many: %q in %v", many, src.Problems)
	}
	// Those read are 3, of entries and problems besides.
	if n := len(src.Entries) + len(src.Problems) - 1; n != 3 {
		t.Errorf("read %d: %v %v", n, src.Entries, src.Problems)
	}
}

func TestParseCRT(t *testing.T) {
	s := parseCRT("S:\"Hostname\"= 192.0.2.7 \r\n" +
		"B:\"Window Placement\"=0000002c\r\n 2c 00 00 00\r\n S:\"Username\"=no\r\n" +
		"D:\"[SSH2] Port\"=zz\r\n" + // not a number: none
		"S:\"Password V2\"=02:abcd\r\n" +
		"D:\"Hostname\"=00000001\r\n" + // not of its type: ignored
		"Z:\"Description\"=00000002\r\n  indented\r\n \r\n not an item\r\n" +
		"junk\r\n" +
		"S:\"Username\"=ann")
	if s.str("Hostname") != "192.0.2.7" || s.str("Username") != "ann" || !s.some {
		t.Errorf("strings %v", s.strs)
	}
	if _, ok := s.num("[SSH2] Port"); ok {
		t.Errorf("numbers %v", s.nums)
	}
	if got := s.lists["Description"]; !slices.Equal(got, []string{" indented", ""}) {
		t.Errorf("description %q", got)
	}
	if len(s.strs) != 2 || len(s.nums) != 0 {
		t.Errorf("kept %v %v", s.strs, s.nums)
	}
	if parseCRT("[Session]\nHostname=192.0.2.8\n").some {
		t.Error("an ini file of another kind")
	}

	for _, c := range []struct {
		methods string
		key     bool
	}{
		{"password,publickey,keyboard-interactive,gssapi", false},
		{"publickey,keyboard-interactive,password", true},
		{" PublicKey , gssapi", true},
		{"keyboard-interactive", false},
		{"", false},
	} {
		s := &crtSession{strs: map[string]string{"SSH2 Authentications V2": c.methods}}
		if s.prefersKey() != c.key {
			t.Errorf("%q: %v", c.methods, !c.key)
		}
	}
}

func TestSecureCRTKey(t *testing.T) {
	cfg := filepath.Join("home", "ann", ".vandyke", "SecureCRT", "Config")
	for _, c := range []struct {
		raw, goos, file string
		needsKey        bool
	}{
		{"", "linux", "", false},
		{"  ", "linux", "", false},
		{"/home/ann/.ssh/id_ed25519", "linux", "/home/ann/.ssh/id_ed25519", false},
		{"/home/ann/.ssh/id_ed25519.pub::rawkey", "linux", "/home/ann/.ssh/id_ed25519", false},
		{"~/.ssh/id_rsa.PUB", "darwin", "~/.ssh/id_rsa", false},
		{`C:\Users\ann\Keys\id_rsa::rawkey`, "windows", `C:\Users\ann\Keys\id_rsa`, false},
		{`C:\Users\ann\Keys\id_rsa::rawkey`, "linux", "", true},
		{`\\server\keys\id_rsa`, "darwin", "", true},
		{`${VDS_CONFIG_PATH}\Keys\Identity.pub::rawkey`, "linux", filepath.Join(cfg, "Keys", "Identity"), false},
		{"${VDS_USER_DATA_PATH}/Identity::rawkey", "linux", "", true},
		{".pub", "linux", ".pub", false},
	} {
		file, needsKey := secureCRTKey(c.raw, cfg, c.goos)
		if file != c.file || needsKey != c.needsKey {
			t.Errorf("%q on %s: %q %v, want %q %v", c.raw, c.goos, file, needsKey, c.file, c.needsKey)
		}
	}
	if file, needsKey := secureCRTKey("${VDS_CONFIG_PATH}/Keys/Identity", "", "linux"); file != "" || !needsKey {
		t.Errorf("no config folder: %q %v", file, needsKey)
	}
}

func TestSecureCRTCandidatesAndFind(t *testing.T) {
	env := func(vars map[string]string) func(string) string { return func(k string) string { return vars[k] } }
	if got, want := secureCRTCandidates("darwin", env(map[string]string{"HOME": "/Users/ann"})),
		[]string{filepath.Join("/Users/ann", "Library", "Application Support", "VanDyke", "SecureCRT", "Config")}; !slices.Equal(got, want) {
		t.Errorf("darwin %q", got)
	}
	if got, want := secureCRTCandidates("linux", env(map[string]string{"HOME": "/home/ann"})),
		[]string{filepath.Join("/home/ann", ".vandyke", "SecureCRT", "Config")}; !slices.Equal(got, want) {
		t.Errorf("linux %q", got)
	}
	got := secureCRTCandidates("windows", env(map[string]string{"USERPROFILE": `C:\Users\ann`, "APPDATA": `C:\Users\ann\AppData\Roaming`}))
	if len(got) != 1 || !strings.Contains(got[0], "Roaming") || !strings.HasSuffix(got[0], "Config") || !strings.Contains(got[0], "VanDyke") {
		t.Errorf("windows %q", got)
	}
	if got := secureCRTCandidates("windows", env(map[string]string{"USERPROFILE": `C:\Users\ann`})); len(got) != 1 || !strings.Contains(got[0], "Roaming") {
		t.Errorf("windows without APPDATA %q", got)
	}
	if got := secureCRTCandidates("linux", env(nil)); len(got) != 0 {
		t.Errorf("no home %q", got)
	}

	// Find takes the first candidate with a Sessions folder.
	none := t.TempDir()
	old := SecureCRTCandidates
	t.Cleanup(func() { SecureCRTCandidates = old })
	SecureCRTCandidates = func() []string { return []string{none, crtSample} }
	if got := FindSecureCRT(); got != crtSample {
		t.Errorf("found %q", got)
	}
	SecureCRTCandidates = func() []string { return []string{none} }
	if got := FindSecureCRT(); got != "" {
		t.Errorf("found %q in none", got)
	}
	if r := ReaderOf(SecureCRT); r == nil || !r.Folder || r.Read == nil || r.Find == nil {
		t.Errorf("reader %+v", r)
	}
	if i := slices.Index(Readers(), ReaderOf(SecureCRT)); i < 1 {
		t.Errorf("listed at %d", i)
	}
}

// Importing makes the key files the hosts' keys, and the folders
// SecureCRT's.
func TestImportSecureCRT(t *testing.T) {
	src, err := ReadSecureCRT(crtSample)
	if err != nil {
		t.Fatal(err)
	}
	s := config.NewStore()
	added := Import(s, src.Entries, src.Entries[0].Host.CreatedAt)
	if len(added) != len(src.Entries) {
		t.Fatalf("added %d of %d", len(added), len(src.Entries))
	}
	for _, h := range added {
		switch h.Name {
		case "db-1":
			if h.Auth != config.AuthKey || h.KeyPath != "/home/ann/.ssh/id_rsa" || len(s.FolderPath(h.Folder)) != 2 {
				t.Errorf("db-1 %+v", h)
			}
		case "legacy":
			if h.Auth != config.AuthKey || h.KeyPath != "" {
				t.Errorf("legacy %+v", h)
			}
		}
	}
}
