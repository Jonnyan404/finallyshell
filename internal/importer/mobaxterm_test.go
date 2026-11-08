package importer

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"finallyshell/internal/config"
)

// The samples in testdata/mobaxterm are made up, in the shape of the
// gist and the readers mobaxterm.go names, with documentation addresses
// (192.0.2.x, 10.0.0.x, example.com). MobaXterm.ini has a [Passwords]
// and a [Credentials] section, of base64 of "sample-not-a-real-secret",
// which must not be read. They were written by a script, not by
// MobaXterm: CRLF lines, Windows-1252 (all ASCII) or GBK.

func mobaSample(name string) string { return filepath.Join("testdata", "mobaxterm", name) }

// readMoba reads the sample name with key paths for goos.
func readMoba(t *testing.T, name, goos string) *Source {
	t.Helper()
	old := mobaGOOS
	mobaGOOS = goos
	t.Cleanup(func() { mobaGOOS = old })
	src, err := ReadMobaXterm(mobaSample(name))
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func entryLabels(src *Source) []string {
	var got []string
	for _, e := range src.Entries {
		got = append(got, e.FolderLabel()+"|"+e.Host.Title())
	}
	return got
}

func TestReadMobaXterm(t *testing.T) {
	for _, name := range []string{"MobaXterm.ini", "export.mxtsessions"} {
		t.Run(name, func(t *testing.T) {
			src := readMoba(t, name, "windows")
			if src.Kind != MobaXterm || src.Path != mobaSample(name) {
				t.Errorf("source %s %q", src.Kind, src.Path)
			}
			// In folder order, then by name; nothing of the other sections.
			want := []string{"|old-box", "|web-1", "|web-2", "Production|app-1", "Production|app-2", "Production|bastion", "Production / Databases|pg-main"}
			if got := entryLabels(src); !slices.Equal(got, want) {
				t.Fatalf("entries %q, want %q", got, want)
			}

			web := byName(t, src, "web-1")
			if h := web.Host; h.Addr != "192.0.2.10" || h.Port != 0 || h.User != "root" || h.Auth != config.AuthPassword ||
				h.Notes != "front end" || h.ID != "" || h.Folder != "" || web.Folder != nil || web.NeedsKey || web.KeyFile != "" || web.Note != "" {
				t.Errorf("web-1 %+v %+v", web, h)
			}
			if !strings.HasPrefix(web.File, name+":") {
				t.Errorf("web-1 in %q", web.File)
			}
			// "<default>" is no user; the reconnection message changes nothing.
			if h := byName(t, src, "web-2").Host; h.User != "" || h.Addr != "192.0.2.11" || h.Notes != "" {
				t.Errorf("web-2 %+v", h)
			}
			// Fields missing at the end: the defaults.
			if old := byName(t, src, "old-box"); old.Host.Addr != "192.0.2.40" || old.Host.Port != 2200 || old.Host.User != "" ||
				old.Host.Auth != config.AuthPassword || old.Note != "" || old.Host.Notes != "" {
				t.Errorf("old-box %+v %+v", old, old.Host)
			}
			// A key on _CurrentDrive_:, which is C:.
			bastion := byName(t, src, "bastion")
			if h := bastion.Host; h.Addr != "bastion.example.com" || h.Port != 2222 || h.User != "deploy" || h.Auth != config.AuthKey ||
				bastion.KeyFile != `C:\Users\ann\.ssh\id_ed25519` || bastion.NeedsKey || !slices.Equal(bastion.Folder, []string{"Production"}) {
				t.Errorf("bastion %+v %+v", bastion, h)
			}
			// Gateways and a proxy are not imported, and say so.
			if app := byName(t, src, "app-1"); app.Note != JumpHostNotImported || app.Host.Addr != "10.0.0.21" || app.Host.Auth != config.AuthPassword {
				t.Errorf("app-1 %+v %+v", app, app.Host)
			}
			if app := byName(t, src, "app-2"); app.Note != ProxyNotImported {
				t.Errorf("app-2 %+v", app)
			}
			// Nested folders; escapes undone.
			pg := byName(t, src, "pg-main")
			if h := pg.Host; h.Port != 5022 || h.User != "postgres" || h.Notes != `primary #1; "main" 50% | rack 4` ||
				!slices.Equal(pg.Folder, []string{"Production", "Databases"}) {
				t.Errorf("pg-main %+v %q", pg, h.Notes)
			}

			for _, p := range src.Problems {
				if !strings.HasPrefix(p.File, name+":") {
					t.Errorf("problem in %q", p.File)
				}
			}
			var reasons []Reason
			for _, p := range src.Problems {
				reasons = append(reasons, p.Reason)
			}
			// Failed first: the line that is not a session, the one without
			// a host; then RDP and SFTP, skipped.
			if want := []Reason{Broken, NoHost, NotSSH, NotSSH}; !slices.Equal(reasons, want) {
				t.Errorf("problems %+v, want %v", src.Problems, want)
			}
			for _, p := range src.Problems {
				if p.Reason == NotSSH && !strings.Contains(p.Detail, "Office desktop") && !strings.Contains(p.Detail, "files") {
					t.Errorf("detail %q", p.Detail)
				}
			}

			// Nothing of the passwords, the cache or the host keys.
			for _, e := range src.Entries {
				if s := e.Host.Name + e.Host.Addr + e.Host.Notes + e.KeyFile; strings.Contains(s, "c2Fz") ||
					strings.Contains(s, "c2Ft") || strings.Contains(s, "198.51.100.99") {
					t.Errorf("read from another section: %+v", e.Host)
				}
			}
		})
	}
}

// A path of Windows is no key file elsewhere: the user chooses it.
func TestMobaXtermKeyElsewhere(t *testing.T) {
	src := readMoba(t, "export.mxtsessions", "linux")
	bastion := byName(t, src, "bastion")
	if bastion.KeyFile != "" || !bastion.NeedsKey || bastion.Host.Auth != config.AuthKey {
		t.Errorf("bastion %+v", bastion)
	}
	for path, want := range map[string]string{
		`_CurrentDrive_:\Users\ann\.ssh\id_rsa`: `C:\Users\ann\.ssh\id_rsa`,
		`_CurrentDrive_:/keys/id_rsa`:           `C:/keys/id_rsa`,
		`_CurrentDrive_\Users\ann\id_rsa`:       `C:\Users\ann\id_rsa`, // no colon: put back
		`D:\keys\id_rsa`:                        `D:\keys\id_rsa`,
		`~/.ssh/id_rsa`:                         `~/.ssh/id_rsa`,
		``:                                      ``,
	} {
		if got := mobaKeyPath(path); got != want {
			t.Errorf("key %q: %q, want %q", path, got, want)
		}
	}
	for _, c := range []struct {
		path string
		win  bool
	}{
		{`C:\Users\ann\.ssh\id_rsa`, true},
		{`d:/keys/id_rsa`, true},
		{`\\nas\keys\id_rsa`, true},
		{`/home/mobaxterm/.ssh/id_rsa`, false},
		{`~/.ssh/id_rsa`, false},
		{`id_rsa`, false},
	} {
		if got := isWindowsPath(c.path); got != c.win {
			t.Errorf("%q: %v", c.path, got)
		}
	}
	// Elsewhere than Windows, a path of here is the key file.
	e, p := mobaSession("k", "#109#0%192.0.2.9%22%u%%%%%%%%%%%~/.ssh/id_ed25519")
	if p != nil || e.KeyFile != "~/.ssh/id_ed25519" || e.NeedsKey || e.Host.Auth != config.AuthKey {
		t.Errorf("%+v %+v", e, p)
	}
}

// A partial export: its [Bookmarks] is the folder exported.
func TestReadMobaXtermPartial(t *testing.T) {
	src := readMoba(t, "partial.mxtsessions", "windows")
	if got, want := entryLabels(src), []string{"Staging|stage-db", "Staging / Web|stage-web"}; !slices.Equal(got, want) || len(src.Problems) != 0 {
		t.Errorf("entries %q, want %q; problems %+v", got, want, src.Problems)
	}
}

// Saved on Chinese Windows, in GBK.
func TestReadMobaXtermGBK(t *testing.T) {
	src := readMoba(t, "gbk.mxtsessions", "windows")
	if len(src.Entries) != 1 {
		t.Fatalf("entries %q", entryLabels(src))
	}
	e := src.Entries[0]
	if e.Host.Name != "树莓派" || e.Host.Notes != "电视柜里那台" || e.Host.User != "pi" || !slices.Equal(e.Folder, []string{"实验室", "客厅"}) {
		t.Errorf("%+v %+v", e, e.Host)
	}
}

// One session saved to a .moba file, with its section or without one.
func TestReadMobaXtermSession(t *testing.T) {
	src := readMoba(t, "session.moba", "windows")
	if got := entryLabels(src); !slices.Equal(got, []string{"|jump"}) {
		t.Errorf("entries %q", got)
	}
	bare := filepath.Join(t.TempDir(), "jump.moba")
	line := "jump= #109#0%jump.example.com%22%ann#MobaFont%10#0# #-1\r\n"
	if err := os.WriteFile(bare, []byte("some=thing\r\n"+line), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := ReadMobaXterm(bare)
	if err != nil {
		t.Fatal(err)
	}
	if got := entryLabels(src); !slices.Equal(got, []string{"|jump"}) || len(src.Problems) != 0 || src.Entries[0].File != "jump.moba:2" {
		t.Errorf("entries %q, problems %+v", got, src.Problems)
	}
}

// A file without sessions is not MobaXterm's; one that cannot be read
// fails.
func TestReadMobaXtermNone(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{
		"empty.ini":   "",
		"other.ini":   "[Misc]\r\nhidden=#109#0%192.0.2.1\r\n",
		"folders.ini": "[Bookmarks]\r\nSubRep=\r\nImgNum=42\r\n[Bookmarks_1]\r\nSubRep=Empty\r\nImgNum=41\r\n",
		"text.txt":    "just some text\nwith = signs\n",
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadMobaXterm(path); !errors.Is(err, ErrNoConnections) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ReadMobaXterm(filepath.Join(dir, "missing.ini")); err == nil || errors.Is(err, ErrNoConnections) {
		t.Errorf("missing: %v", err)
	}
	if _, err := ReadMobaXterm(dir); err == nil {
		t.Error("a folder read")
	}
	// Only RDP: read, with nothing to import.
	path := filepath.Join(dir, "rdp.mxtsessions")
	os.WriteFile(path, []byte("[Bookmarks]\r\nSubRep=\r\nImgNum=42\r\ndesk= #91#4%192.0.2.30%3389%ann#MobaFont#0# #-1\r\n"), 0o600)
	src, err := ReadMobaXterm(path)
	if err != nil || len(src.Entries) != 0 || len(src.Problems) != 1 || src.Problems[0].Reason != NotSSH || src.Problems[0].File != "rdp.mxtsessions:4" {
		t.Errorf("%+v %v", src, err)
	}
}

func TestMobaXtermSections(t *testing.T) {
	for name, want := range map[string]bool{
		"Bookmarks": true, "Bookmarks_1": true, "bookmarks_12": true, "BOOKMARKS": true,
		"Bookmarks_": false, "Bookmarks_x": false, "Bookmarks1": false, "Misc": false, "Passwords": false, "": false,
	} {
		if got := isBookmarks(name); got != want {
			t.Errorf("%q: %v", name, got)
		}
	}
	for subRep, want := range map[string][]string{
		"": nil, `Production`: {"Production"}, `A\B\C`: {"A", "B", "C"}, `A\\B\`: {"A", "B"}, ` A \ B `: {"A", "B"}, `50__PERCENT__`: {"50%"},
	} {
		if got := mobaFolder(subRep); !slices.Equal(got, want) {
			t.Errorf("%q: %q", subRep, got)
		}
	}
	if got := mobaUnescape("a__PTVIRG__b__DBLQUO__c__PIPE__d__PERCENT__e__DIEZE__f"); got != `a;b"c|d%e#f` {
		t.Errorf("unescaped %q", got)
	}
	// Ports: missing or not one is 22.
	for value, want := range map[string]int{"#1#0%h": 0, "#1#0%h%": 0, "#1#0%h%x": 0, "#1#0%h%70000": 0, "#1#0%h%22": 0, "#1#0%h%2022": 2022} {
		if e, p := mobaSession("s", value); p != nil || e.Host.Port != want {
			t.Errorf("%q: %+v %+v", value, e, p)
		}
	}
	// A line with too few fields is broken.
	if _, p := mobaSession("s", "#109"); p == nil || p.Reason != Broken {
		t.Errorf("%+v", p)
	}
}

func TestFindMobaXterm(t *testing.T) {
	env := map[string]string{"APPDATA": `C:\Users\ann\AppData\Roaming`, "USERPROFILE": `C:\Users\ann`}
	got := mobaCandidates("windows", func(k string) string { return env[k] })
	want := []string{
		filepath.Join(`C:\Users\ann\AppData\Roaming`, "MobaXterm", "MobaXterm.ini"),
		filepath.Join(`C:\Users\ann`, "Documents", "MobaXterm", "MobaXterm.ini"),
	}
	if !slices.Equal(got, want) {
		t.Errorf("windows %q, want %q", got, want)
	}
	if got := mobaCandidates("windows", func(string) string { return "" }); got != nil {
		t.Errorf("no environment: %q", got)
	}
	for _, goos := range []string{"linux", "darwin"} {
		if got := mobaCandidates(goos, func(k string) string { return "/home/ann" }); got != nil {
			t.Errorf("%s: %q", goos, got)
		}
	}

	dir := t.TempDir()
	ini := filepath.Join(dir, "b", "MobaXterm.ini")
	os.MkdirAll(filepath.Join(dir, "a", "MobaXterm.ini"), 0o700) // a folder, not the file
	os.MkdirAll(filepath.Dir(ini), 0o700)
	os.WriteFile(ini, []byte("[Bookmarks]\r\n"), 0o600)
	old := MobaXtermCandidates
	t.Cleanup(func() { MobaXtermCandidates = old })
	MobaXtermCandidates = func() []string {
		return []string{filepath.Join(dir, "none", "MobaXterm.ini"), filepath.Join(dir, "a", "MobaXterm.ini"), ini}
	}
	if got := FindMobaXterm(); got != ini {
		t.Errorf("found %q", got)
	}
	MobaXtermCandidates = func() []string { return nil }
	if got := FindMobaXterm(); got != "" {
		t.Errorf("found %q", got)
	}
}

// MobaXterm is listed after FinalShell, a file of its extensions.
func TestMobaXtermReader(t *testing.T) {
	r := ReaderOf(MobaXterm)
	if r == nil || r.Folder || !slices.Equal(r.Extensions, []string{"ini", "mxtsessions", "moba"}) || r.Find == nil || r.Read == nil {
		t.Fatalf("reader %+v", r)
	}
	if i := slices.Index(Readers(), r); i <= slices.Index(Readers(), ReaderOf(FinalShell)) {
		t.Errorf("listed at %d", i)
	}
}
