package importer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"finallyshell/internal/config"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

func TestReaders(t *testing.T) {
	rs := Readers()
	if len(rs) == 0 || rs[0].Kind != FinalShell {
		t.Fatalf("readers %+v", rs)
	}
	seen := map[Kind]bool{}
	for _, r := range rs {
		if r.Kind == "" || seen[r.Kind] || r.Read == nil || ReaderOf(r.Kind) != r {
			t.Errorf("reader %+v", r)
		}
		seen[r.Kind] = true
		if r.Folder && len(r.Extensions) > 0 {
			t.Errorf("%s: a folder with extensions", r.Kind)
		}
	}
	if ReaderOf("nothing") != nil {
		t.Error("a reader of no kind")
	}
	// Readers is a copy.
	rs[0] = nil
	if Readers()[0] == nil {
		t.Error("Readers is the list itself")
	}
	if r := ReaderOf(FinalShell); !r.Folder || r.Find == nil {
		t.Errorf("finalshell %+v", r)
	}
	src, err := ReaderOf(FinalShell).Read(filepath.Join("testdata", "finalshell"))
	if err != nil || src.Kind != FinalShell || len(src.Entries) == 0 {
		t.Errorf("read %+v, %v", src, err)
	}
}

func TestDecodeText(t *testing.T) {
	const want = "Host 服务器\r\nUser root\n"
	utf16 := func(order unicode.Endianness) []byte {
		b, err := unicode.UTF16(order, unicode.UseBOM).NewEncoder().Bytes([]byte(want))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	gb, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(want))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		data []byte
	}{
		{"utf-8", []byte(want)},
		{"utf-8 bom", append([]byte{0xef, 0xbb, 0xbf}, want...)},
		{"utf-16 le bom", utf16(unicode.LittleEndian)},
		{"utf-16 be bom", utf16(unicode.BigEndian)},
		{"gb18030", gb},
	} {
		if got := DecodeText(c.data); got != want {
			t.Errorf("%s: %q", c.name, got)
		}
	}
	if c := utf16(unicode.LittleEndian); c[0] != 0xff || c[1] != 0xfe {
		t.Fatalf("no bom: % x", c[:2])
	}
	// ASCII is the same in all; an empty file is empty.
	if DecodeText([]byte("Host a")) != "Host a" || DecodeText(nil) != "" {
		t.Error("ascii")
	}
	// A broken UTF-16 file, an odd byte at its end, decodes what it can.
	odd := append(utf16(unicode.LittleEndian), 'x')
	if got := DecodeText(odd); len(got) < len(want) || got[:len(want)] != want {
		t.Errorf("odd utf-16: %q", got)
	}
}

func TestReadText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MobaXterm.ini")
	gb, _ := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("[Bookmarks]\nSubRep=生产\n"))
	os.WriteFile(path, gb, 0o600)
	if got, err := ReadText(path); err != nil || got != "[Bookmarks]\nSubRep=生产\n" {
		t.Errorf("read %q, %v", got, err)
	}
	if _, err := ReadText(filepath.Join(dir, "missing")); err == nil || ReadProblem("missing", err).Reason != Unreadable {
		t.Errorf("missing: %v", err)
	}
	if _, err := ReadText(dir); err == nil {
		t.Error("read a folder")
	}
	big := filepath.Join(dir, "big.csv")
	f, _ := os.Create(big)
	f.Truncate(maxTextSize + 1)
	f.Close()
	_, err := ReadText(big)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("big: %v", err)
	}
	if p := ReadProblem("big.csv", err); p.Reason != TooLarge || p.File != "big.csv" || p.Detail != "" {
		t.Errorf("big: %+v", p)
	}
}

func TestProblems(t *testing.T) {
	if At("hosts.csv", 0) != "hosts.csv" || At("config", 12) != "config:12" {
		t.Error("at")
	}
	p := NewProblem("hosts.csv", 3, NoHost, nil)
	if p.File != "hosts.csv:3" || p.Reason != NoHost || p.Detail != "" {
		t.Errorf("%+v", p)
	}
	if p := NewProblem("a.json", 0, Broken, errors.New("bad")); p.File != "a.json" || p.Detail != "bad" {
		t.Errorf("%+v", p)
	}
	if !Broken.Failed() || !NoHost.Failed() || !TooLarge.Failed() || !Unreadable.Failed() || NotSSH.Failed() || DeletedThere.Failed() {
		t.Error("which reasons fail")
	}
	// A source's own reason fails unless its init skips it.
	if !Reason("telnet").Failed() {
		t.Error("an unknown reason is skipped")
	}

	src := &Source{
		Entries: []*Entry{
			{Host: &config.Host{Name: "b"}, Folder: []string{"x"}},
			{Host: &config.Host{Name: "B2"}},
			{Host: &config.Host{Name: "a"}},
		},
		Problems: []Problem{{File: "b", Reason: NotSSH}, {File: "c", Reason: Broken}, {File: "a", Reason: NoHost}},
	}
	sortSource(src)
	var got []string
	for _, e := range src.Entries {
		got = append(got, e.Host.Name)
	}
	for _, p := range src.Problems {
		got = append(got, p.File)
	}
	if want := []string{"a", "B2", "b", "a", "c", "b"}; !slices.Equal(got, want) {
		t.Errorf("sorted %q, want %q", got, want)
	}
}

// An entry's key file is its host's key; its password is not in the host.
func TestImportKeyFileAndPassword(t *testing.T) {
	s := config.NewStore()
	entries := []*Entry{
		{File: "config:4", Host: &config.Host{Name: "k", Addr: "192.0.2.1", Auth: config.AuthPassword}, KeyFile: "~/.ssh/id_ed25519"},
		{File: "hosts.csv:2", Host: &config.Host{Name: "p", Addr: "192.0.2.2", User: "root", Auth: config.AuthPassword}, Password: "sample-not-a-real-secret"},
	}
	added := Import(s, entries, time.Now())
	if len(added) != 2 || added[0].Auth != config.AuthKey || added[0].KeyPath != "~/.ssh/id_ed25519" || added[0].Folder != "" {
		t.Fatalf("added %+v", added)
	}
	if h := added[1]; h.Auth != config.AuthPassword || h.KeyPath != "" {
		t.Errorf("p %+v", h)
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sample-not-a-real-secret") {
		t.Errorf("the password is saved: %s", data)
	}
	if entries[0].Host.KeyPath != "" {
		t.Error("the entry's host changed")
	}
}
