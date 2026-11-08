package importer

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"finallyshell/internal/config"
)

// The samples in testdata/xshell are made up, in the shape of the session
// files published on GitHub that xshell.go names (see its comment), by a
// script that wrote Xshell/Sessions as Xshell 7 does (UTF-16 LE with a
// byte order mark, CRLF), one in GBK as Xshell 5 on Chinese Windows, one
// as Xshell 3 (ASCII, no Method), and sessions.xts as a zip of sessions,
// one named in GBK without the zip's UTF-8 flag. Addresses are for
// documentation, passwords base64 of "sample-not-a-real-secret".

func xshellSample(parts ...string) string {
	return filepath.Join(append([]string{"testdata", "xshell"}, parts...)...)
}

// listing is src's entries as "folder|name".
func listing(src *Source) []string {
	var got []string
	for _, e := range src.Entries {
		got = append(got, e.FolderLabel()+"|"+e.Host.Title())
	}
	return got
}

// problemsOf is src's problems by file, failing the test on one twice.
func problemsOf(t *testing.T, src *Source) map[string]Reason {
	t.Helper()
	out := map[string]Reason{}
	for _, p := range src.Problems {
		if _, dup := out[p.File]; dup {
			t.Errorf("%s twice", p.File)
		}
		out[p.File] = p.Reason
	}
	return out
}

// noSecrets fails the test when anything of the samples' passwords is in
// what was read.
func noSecrets(t *testing.T, src *Source) {
	t.Helper()
	data, err := json.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"c2FtcGxl", "sample-not"} {
		if strings.Contains(string(data), secret) {
			t.Errorf("%q in what was read: %s", secret, data)
		}
	}
}

func TestReadXshell(t *testing.T) {
	sessions := xshellSample("Xshell", "Sessions")
	// The Sessions folder, or the Xshell folder it is in.
	for _, dir := range []string{sessions, xshellSample("Xshell")} {
		src, err := ReadXshell(dir)
		if err != nil {
			t.Fatal(err)
		}
		if src.Kind != Xshell || src.Path != sessions {
			t.Errorf("%s: kind %s, path %q", dir, src.Kind, src.Path)
		}
		// In folder order, then by name; the top level first.
		want := []string{"|legacy", "|web-1", "Production|bastion", "Production / Databases|pg-main", "实验室|树莓派"}
		if got := listing(src); !slices.Equal(got, want) {
			t.Fatalf("entries %q, want %q", got, want)
		}

		// Xshell 7, UTF-16: password auth, the notes, port 22.
		web := byName(t, src, "web-1")
		if h := web.Host; h.Addr != "192.0.2.10" || h.Port != 0 || h.User != "root" || h.Auth != config.AuthPassword ||
			h.Notes != "front end" || h.ID != "" || h.Folder != "" || web.NeedsKey || web.Note != "" || web.Folder != nil || web.File != "web-1.xsh" {
			t.Errorf("web-1 %+v %+v", web, h)
		}
		// A key of Xshell's store, a proxy not imported.
		bastion := byName(t, src, "bastion")
		if h := bastion.Host; h.Addr != "bastion.example.com" || h.Port != 2222 || h.User != "deploy" || h.Auth != config.AuthKey ||
			h.KeyPath != "" || !bastion.NeedsKey || bastion.KeyFile != "" || bastion.Note != ProxyNotImported || bastion.File != "Production/bastion.xsh" {
			t.Errorf("bastion %+v %+v", bastion, h)
		}
		// Nested folders; "key = value".
		pg := byName(t, src, "pg-main")
		if h := pg.Host; h.Addr != "db.example.com" || h.Port != 5022 || h.User != "postgres" || h.Auth != config.AuthKey || h.Notes != "primary" ||
			!slices.Equal(pg.Folder, []string{"Production", "Databases"}) || pg.File != "Production/Databases/pg-main.xsh" {
			t.Errorf("pg-main %+v %+v", pg, h)
		}
		// GBK, as Xshell 5 writes on Chinese Windows; <None> is no proxy.
		pi := byName(t, src, "树莓派")
		if h := pi.Host; h.Addr != "192.0.2.30" || h.User != "pi" || h.Notes != "客厅的树莓派" || h.Auth != config.AuthPassword || pi.Note != "" {
			t.Errorf("pi %+v %+v", pi, h)
		}
		// Xshell 3: no Method, Proxy in [CONNECTION] and empty, and the
		// Description of [Information] is not the notes.
		legacy := byName(t, src, "legacy")
		if h := legacy.Host; h.Addr != "192.0.2.40" || h.User != "admin" || h.Auth != config.AuthPassword || h.Notes != "" || legacy.Note != "" {
			t.Errorf("legacy %+v %+v", legacy, h)
		}

		problems := problemsOf(t, src)
		want2 := map[string]Reason{
			"router.xsh":  NotSSH,
			"console.xsh": NotSSH,
			"no-host.xsh": NoHost,
			"deploy.xsh":  Broken,
		}
		if !maps(problems, want2) {
			t.Errorf("problems %v, want %v", problems, want2)
		}
		// Those that failed first.
		if len(src.Problems) != 4 || !src.Problems[0].Reason.Failed() || !src.Problems[1].Reason.Failed() || src.Problems[2].Reason.Failed() {
			t.Errorf("problems %+v", src.Problems)
		}
		noSecrets(t, src)
	}
}

// A folder of the user's named Sessions, in the folder of sessions, is one
// of its folders: the folder found, or chosen, is not read for it.
func TestReadXshellSessionsFolder(t *testing.T) {
	session := func(host string) []byte { return []byte("[CONNECTION]\r\nHost=" + host + "\r\n") }
	write := func(path, host string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, session(host), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	base := t.TempDir()
	version := filepath.Join(base, "NetSarang Computer", "7")
	root := filepath.Join(version, "Xshell", "Sessions")
	write(filepath.Join(root, "web.xsh"), "192.0.2.10")
	write(filepath.Join(root, "Sessions", "db.xsh"), "192.0.2.20")
	// A folder of sessions of another name, chosen.
	custom := filepath.Join(base, "My sessions")
	write(filepath.Join(custom, "web.xsh"), "192.0.2.10")
	write(filepath.Join(custom, "Sessions", "db.xsh"), "192.0.2.20")

	saved := XshellCandidates
	t.Cleanup(func() { XshellCandidates = saved })
	XshellCandidates = func() []string { return []string{root} }
	found := FindXshell()
	if found != root {
		t.Fatalf("found %q", found)
	}
	want := []string{"|web", "Sessions|db"}
	for _, dir := range []string{found, root, filepath.Dir(root), version, custom} {
		src, err := ReadXshell(dir)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		wantPath := root
		if dir == custom {
			wantPath = custom
		}
		if got := listing(src); src.Path != wantPath || !slices.Equal(got, want) {
			t.Errorf("%s: path %q, entries %q; want %q", dir, src.Path, got, want)
		}
		if e := byName(t, src, "db"); e.File != "Sessions/db.xsh" {
			t.Errorf("%s: db %+v", dir, e)
		}
	}
}

// An export, chosen itself or in a folder chosen, reads without being
// extracted.
func TestReadXshellExport(t *testing.T) {
	xts := xshellSample("sessions.xts")
	src, err := ReadXshell(xts)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Staging|stage", "Staging / Web|stage-web", "测试|测试机"}
	if got := listing(src); src.Path != xts || !slices.Equal(got, want) {
		t.Fatalf("path %q, entries %q; want %q", src.Path, got, want)
	}
	web := byName(t, src, "stage-web")
	if h := web.Host; h.Addr != "192.0.2.60" || h.Port != 8022 || h.Auth != config.AuthKey || !web.NeedsKey || web.File != "sessions.xts/Staging/Web/stage-web.xsh" {
		t.Errorf("stage-web %+v %+v", web, h)
	}
	if p := problemsOf(t, src); !maps(p, map[string]Reason{"sessions.xts/Staging/rlogin.xsh": NotSSH}) {
		t.Errorf("problems %v", p)
	}
	noSecrets(t, src)

	// In a folder, with a session beside it: its folders are in the
	// folder's.
	dir := t.TempDir()
	data, err := os.ReadFile(xts)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(dir, "backup"), 0o700)
	os.WriteFile(filepath.Join(dir, "backup", "old.XTS"), data, 0o600)
	web1, _ := os.ReadFile(xshellSample("Xshell", "Sessions", "web-1.xsh"))
	os.WriteFile(filepath.Join(dir, "web-1.xsh"), web1, 0o600)
	os.WriteFile(filepath.Join(dir, "._web-1.xsh"), []byte("\x00\x05\x16\x07"), 0o600) // macOS's
	os.WriteFile(filepath.Join(dir, "broken.xts"), []byte("not a zip"), 0o600)
	src, err = ReadXshell(dir)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"|web-1", "backup / Staging|stage", "backup / Staging / Web|stage-web", "backup / 测试|测试机"}
	if got := listing(src); src.Path != dir || !slices.Equal(got, want) {
		t.Fatalf("path %q, entries %q; want %q", src.Path, got, want)
	}
	if e := byName(t, src, "测试机"); e.File != "backup/old.XTS/测试/测试机.xsh" {
		t.Errorf("file %q", e.File)
	}
	p := problemsOf(t, src)
	if !maps(p, map[string]Reason{"broken.xts": Broken, "backup/old.XTS/Staging/rlogin.xsh": NotSSH}) {
		t.Errorf("problems %v", p)
	}
}

// writeZip writes a zip of files, by name, at path.
func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	names := slices.Sorted(func(yield func(string) bool) {
		for n := range files {
			if !yield(n) {
				return
			}
		}
	})
	for _, name := range names {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		fw.Write([]byte(files[name]))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// An export with the Sessions folder in it, Windows' separators, and a
// file too large.
func TestReadXshellExportLayouts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "all.xts")
	session := "[CONNECTION]\r\nHost=192.0.2.90\r\n[CONNECTION:AUTHENTICATION]\r\nUserName=ops\r\n"
	writeZip(t, path, map[string]string{
		"Xshell/Sessions/a.xsh":       session,
		`Xshell\Sessions\Lab\b.xsh`:   strings.Replace(session, "90", "91", 1),
		"Sessions/c.xsh":              strings.Replace(session, "90", "92", 1),
		"Xshell/Sessions/big.xsh":     "[CONNECTION]\n" + strings.Repeat("x", maxFileSize+1),
		"Xshell/Sessions/readme.txt":  "not a session",
		"__MACOSX/Xshell/._a.xsh":     "metadata",
		"Xshell/Sessions/Empty/":      "",
		"../../outside/escape.xsh":    strings.Replace(session, "90", "93", 1),
		"Xshell/Sessions/./dots.xsh":  strings.Replace(session, "90", "94", 1),
		"Xshell/Sessions/Lab/._b.xsh": "metadata",
	})
	src, err := ReadXshell(path)
	if err != nil {
		t.Fatal(err)
	}
	// Nothing is extracted, and ".." is no folder: its sessions are in the
	// folders after it.
	want := []string{"|a", "|c", "|dots", "Lab|b", "outside|escape"}
	if got := listing(src); !slices.Equal(got, want) {
		t.Errorf("entries %q, want %q", got, want)
	}
	if e := byName(t, src, "escape"); e.File != "all.xts/outside/escape.xsh" {
		t.Errorf("escape %+v", e)
	}
	if p := problemsOf(t, src); !maps(p, map[string]Reason{"all.xts/big.xsh": TooLarge}) {
		t.Errorf("problems %v", p)
	}
}

func TestReadXshellFails(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("[CONNECTION]\nHost=192.0.2.1\n"), 0o600)
	if _, err := ReadXshell(dir); !errors.Is(err, ErrNoConnections) {
		t.Errorf("a folder without sessions: %v", err)
	}
	if _, err := ReadXshell(filepath.Join(dir, "nothing")); err == nil || errors.Is(err, ErrNoConnections) {
		t.Errorf("no folder: %v", err)
	}
	bad := filepath.Join(dir, "bad.xts")
	os.WriteFile(bad, []byte("PK not really"), 0o600)
	if _, err := ReadXshell(bad); err == nil || errors.Is(err, ErrNoConnections) || !strings.Contains(err.Error(), "not an Xshell export") {
		t.Errorf("a broken export: %v", err)
	}
	empty := filepath.Join(dir, "empty.xts")
	writeZip(t, empty, map[string]string{"readme.txt": "nothing"})
	if _, err := ReadXshell(empty); !errors.Is(err, ErrNoConnections) {
		t.Errorf("an export without sessions: %v", err)
	}

	// One session's file, of any name.
	one := filepath.Join(dir, "one.session")
	os.WriteFile(one, []byte("[CONNECTION]\nHost = 192.0.2.5\nPort = 2200\nProtocol = ssh\n"), 0o600)
	src, err := ReadXshell(one)
	if err != nil || src.Path != one || len(src.Entries) != 1 {
		t.Fatalf("one: %+v, %v", src, err)
	}
	if e := src.Entries[0]; e.Host.Name != "one.session" || e.Host.Port != 2200 || e.File != "one.session" {
		t.Errorf("one %+v %+v", e, e.Host)
	}

	// A file too large is a problem, not a read of it.
	big := filepath.Join(dir, "big")
	os.Mkdir(big, 0o700)
	os.WriteFile(filepath.Join(big, "big.xsh"), make([]byte, maxFileSize+1), 0o600)
	if src, err := ReadXshell(big); err != nil || !maps(problemsOf(t, src), map[string]Reason{"big.xsh": TooLarge}) {
		t.Errorf("big: %+v, %v", src, err)
	}
}

func TestParseXsh(t *testing.T) {
	ini := parseXsh([]byte("\ufeff ; a comment\n[ Connection ]\nHOST=192.0.2.1\nHost=192.0.2.2\nPort\n=no key\n[CONNECTION:AUTHENTICATION]\nPassword=c2FtcGxl\nUserName= ops \n[Other]\nHost=192.0.2.3\n"))
	if ini["connection"]["host"] != "192.0.2.2" || ini["connection:authentication"]["username"] != "ops" || len(ini["other"]) != 0 {
		t.Errorf("ini %v", ini)
	}
	// The password is not kept.
	if _, ok := ini["connection:authentication"]["password"]; ok {
		t.Error("the password kept")
	}
	if parseXsh([]byte("Host=192.0.2.1\n")) != nil || parseXsh(nil) != nil {
		t.Error("a file without sections")
	}
	// UTF-16 LE without its mark.
	u := []byte{}
	for _, c := range "[CONNECTION]\r\nHost=192.0.2.7\r\n" {
		u = append(u, byte(c), 0)
	}
	if ini := parseXsh(u); ini["connection"]["host"] != "192.0.2.7" {
		t.Errorf("utf-16: %v", ini)
	}

	for _, c := range []struct {
		method, key string
		want        bool
	}{
		{"0", "", false}, {"0", "left-over", false}, {"1", "k", true}, {"1", "", true},
		{"PUBLICKEY", "k", true}, {"Public Key", "", true}, {"PASSWORD", "k", false},
		{"", "k", true}, {"", "", false}, {"2", "k", false}, {"GSSAPI", "", false},
	} {
		if got := xshKeyAuth(c.method, c.key); got != c.want {
			t.Errorf("method %q key %q: %v", c.method, c.key, got)
		}
	}
	for name, want := range map[string]bool{"": false, "<None>": false, "none": false, "office": true, "<office>": true} {
		if xshProxy(name) != want {
			t.Errorf("proxy %q", name)
		}
	}
}

// dirEntry is a made-up folder, or a file.
type dirEntry struct {
	name string
	dir  bool
}

func (d dirEntry) Name() string { return d.name }
func (d dirEntry) IsDir() bool  { return d.dir }
func (d dirEntry) Type() fs.FileMode {
	if d.dir {
		return fs.ModeDir
	}
	return 0
}
func (d dirEntry) Info() (fs.FileInfo, error) { return nil, errors.New("no info") }

func TestXshellCandidates(t *testing.T) {
	env := map[string]string{`USERPROFILE`: `/u`, `OneDrive`: `/od`, `APPDATA`: `/u/AppData/Roaming`}
	readDir := func(dir string) ([]fs.DirEntry, error) {
		if dir == filepath.Join("/u", "Documents", "NetSarang Computer") {
			return []fs.DirEntry{dirEntry{"6", true}, dirEntry{"Common", true}, dirEntry{"8", true},
				dirEntry{"10", true}, dirEntry{"7", true}, dirEntry{"9", false}}, nil
		}
		return nil, fs.ErrNotExist
	}
	got := xshellCandidates("windows", func(k string) string { return env[k] }, readDir)
	j := filepath.Join
	want := []string{
		j("/u", "Documents", "NetSarang Computer", "10", "Xshell", "Sessions"),
		j("/u", "Documents", "NetSarang Computer", "8", "Xshell", "Sessions"),
		j("/u", "Documents", "NetSarang Computer", "7", "Xshell", "Sessions"),
		j("/u", "Documents", "NetSarang Computer", "6", "Xshell", "Sessions"),
		j("/u", "Documents", "NetSarang", "Xshell", "Sessions"),
		j("/od", "Documents", "NetSarang", "Xshell", "Sessions"),
		j("/u/AppData/Roaming", "NetSarang", "Xshell", "Sessions"),
	}
	if !slices.Equal(got, want) {
		t.Errorf("candidates\n%q, want\n%q", got, want)
	}
	if got := xshellCandidates("linux", func(k string) string { return env[k] }, readDir); got != nil {
		t.Errorf("linux %q", got)
	}
	if got := xshellCandidates("windows", func(string) string { return "" }, readDir); len(got) != 0 {
		t.Errorf("no environment %q", got)
	}

	// Find: the first that is a folder.
	dir := t.TempDir()
	newer, older := filepath.Join(dir, "8"), filepath.Join(dir, "7")
	os.Mkdir(older, 0o700)
	saved := XshellCandidates
	t.Cleanup(func() { XshellCandidates = saved })
	XshellCandidates = func() []string { return []string{newer, older} }
	if got := FindXshell(); got != older {
		t.Errorf("found %q", got)
	}
	XshellCandidates = func() []string { return []string{newer} }
	if got := FindXshell(); got != "" {
		t.Errorf("found %q", got)
	}
}

// Xshell is listed after FinalShell, and reads its samples.
func TestXshellReader(t *testing.T) {
	rs := Readers()
	if len(rs) < 2 || rs[1].Kind != Xshell {
		t.Fatalf("readers %+v", rs)
	}
	r := ReaderOf(Xshell)
	if !r.Folder || r.Find == nil || len(r.Extensions) != 0 {
		t.Errorf("xshell %+v", r)
	}
	if src, err := r.Read(xshellSample("Xshell")); err != nil || src.Kind != Xshell || len(src.Entries) == 0 {
		t.Errorf("read %+v, %v", src, err)
	}
}
