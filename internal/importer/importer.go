// Package importer reads the connections of other SSH clients, and other
// lists of servers, for the connection manager to import.
//
// Each place connections come from is a source: a Kind, and a Reader in
// readers below, in the order the import window lists them. A Reader
// finds the source's file or folder on this system (Find), says whether
// the user chooses a file or a folder when it finds none, and reads it
// (Read) into a Source: Entries, the connections as hosts to add, and
// Problems, the files or lines it did not take a connection from, and
// why. The rest is the same for every source: the window lists the
// entries, Present and SameTarget find those the store has already, and
// Import adds the others in their folders.
//
// To add a source:
//
//  1. Write <source>.go: its Kind constant, its Reader, with Find and
//     Read, and a comment on what is known of its format and where that
//     is from. Its tests go in <source>_test.go, made-up samples in
//     testdata/<source>/ (documentation addresses, no real secrets).
//     Reasons and Notes of its own are constants there too; a reason for
//     a connection skipped on purpose, not failed, goes in skipped in an
//     init there: func init() { skipped[Telnet] = true }.
//  2. Put its Reader in readers, below, where the window should list it.
//  3. Give it its texts in the app: an entry in importTexts, in
//     internal/app/import_sources.go, with the strings in locales/, and
//     the texts of any Reason or Note it adds, there too.
//
// Readers share the helpers here: ReadText reads a text file in the
// encoding it is in (DecodeText), NewProblem and ReadProblem make the
// Problem of a file or of one of its lines, and sortSource puts a
// Source in the order the window lists it.
//
// Passwords: the other clients encrypt the passwords they store, with
// keys of their own; this package does not decrypt them, nor read them.
// Only a list of servers the user wrote, as a CSV file, has them in the
// clear: those are in Entry.Password, which Import does not put in the
// host, and the app keeps in the system's credential store.
package importer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"finallyshell/internal/config"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

// Kind is a source's: "finalshell" and such, constant across versions.
type Kind string

// readers are the sources, in the order the import window lists them.
var readers = []*Reader{
	finalShellReader,
	xshellReader,
	secureCRTReader,
	mobaXtermReader,
	sshConfigReader,
}

// Reader is what the import window knows of a source.
type Reader struct {
	Kind Kind
	// Folder is set for a source that is a folder, which the user
	// chooses when Find finds none; else it is a file.
	Folder bool
	// Extensions are those of the source's files, without the dot, for
	// the file chooser to offer: none for any file.
	Extensions []string
	// Find is the source's file or folder on this system: "" when there
	// is none. It reads the disk, so the app calls it off the UI thread.
	// nil is a source without a place of its own: the user chooses it.
	Find func() string
	// Read reads the file or folder at path. What it cannot read of it,
	// and the connections it does not import, are the Source's Problems;
	// it fails when path cannot be read at all, or has no connections:
	// ErrNoConnections then.
	Read func(path string) (*Source, error)
}

// Readers are the sources, in the order the import window lists them.
func Readers() []*Reader { return slices.Clone(readers) }

// ReaderOf is the source of kind k: nil for none.
func ReaderOf(k Kind) *Reader {
	for _, r := range readers {
		if r.Kind == k {
			return r
		}
	}
	return nil
}

// Source is what a Reader read.
type Source struct {
	Kind Kind
	// Path is the file or folder read, as FinalShell's conn folder when
	// its data folder was chosen.
	Path     string
	Entries  []*Entry
	Problems []Problem
}

// ErrNoConnections is Read's error for a file or folder without
// connections, as one that is not the source's.
var ErrNoConnections = errors.New("no connections found")

// Entry is a connection read from a source.
type Entry struct {
	// File is the file the connection came from, or the line of it, as
	// At writes it: relative to Source.Path when that is a folder.
	File string
	// Host is the connection as a host to add: with no ID and no folder.
	Host *config.Host
	// Folder is the path of the folder it was in, the top-level folder
	// first: nil for the top level, as for a source without folders.
	Folder []string
	// KeyFile is the private key's file the source names, as Host.KeyPath
	// takes it ("~/" for the home folder): Import makes it the host's key.
	KeyFile string
	// NeedsKey is set for a host that logs in with a key the source keeps
	// in a store of its own (FinalShell, Xshell): the user chooses the
	// key's file after importing.
	NeedsKey bool
	// Note is what the window says of the entry, a setting of the source
	// that is not imported: "" for none.
	Note Note
	// Password is the host's password, from a list of servers that has
	// them (CSV): Import does not put it in the host; the app keeps it
	// in the system's credential store.
	Password string
	// Jump is the entry of the same source this one connects through, as
	// a jump host (OpenSSH's ProxyJump): nil for none. Import makes the
	// host of that entry, imported or present already, the host's jump
	// host; when neither, the host connects directly.
	Jump *Entry
}

// FolderLabel is e's folder path, as "Production / Databases".
func (e *Entry) FolderLabel() string { return strings.Join(e.Folder, " / ") }

// Note is a setting of a connection that is not imported, which the
// window says in words of its own. Sources add those they need.
type Note string

// ProxyNotImported is a connection through one of the other client's
// proxies, which are not imported: the host connects directly.
const ProxyNotImported Note = "proxy"

// Reason is why a file, or a line of it, was not read, or a connection
// not imported, for the window to say in words of its own. Sources add
// those they need.
type Reason string

const (
	// Broken is a file, or a line, not in the format of the source: not
	// JSON, not an object.
	Broken Reason = "broken"
	// Unreadable is a file the system did not let Read open.
	Unreadable Reason = "unreadable"
	// TooLarge is a file larger than any of the source's.
	TooLarge Reason = "too-large"
	// NoHost is a connection without an address.
	NoHost Reason = "no-host"
	// NotSSH is a connection of another kind: RDP, VNC, Telnet and such.
	NotSSH Reason = "not-ssh"
	// DeletedThere is a connection deleted in the other client.
	DeletedThere Reason = "deleted"
)

// skipped are the reasons of connections skipped on purpose, rather than
// failed to import. A source that adds such a reason adds it here in an
// init of its own file, so that sources do not edit the same lines:
//
//	func init() { skipped[Telnet] = true }
var skipped = map[Reason]bool{NotSSH: true, DeletedThere: true}

// Failed tells whether a problem with the reason r failed to import,
// rather than was skipped on purpose.
func (r Reason) Failed() bool { return !skipped[r] }

// Problem is a file, or a line of one, a Reader did not take a
// connection from.
type Problem struct {
	// File is the file, or the line of it, as At writes it: relative to
	// Source.Path when that is a folder.
	File   string
	Reason Reason
	// Detail is what went wrong, in English, from the system or a parser:
	// "" for none.
	Detail string
}

// NewProblem is the problem of file, or of its line when line is not 0,
// with the detail of err, nil for none.
func NewProblem(file string, line int, reason Reason, err error) Problem {
	p := Problem{File: At(file, line), Reason: reason}
	if err != nil {
		p.Detail = err.Error()
	}
	return p
}

// ReadProblem is the problem of a file ReadText did not read: TooLarge or
// Unreadable.
func ReadProblem(file string, err error) Problem {
	if errors.Is(err, ErrTooLarge) {
		return NewProblem(file, 0, TooLarge, nil)
	}
	return NewProblem(file, 0, Unreadable, err)
}

// At is the line of file as entries and problems name it, "file:12": file
// for line 0.
func At(file string, line int) string {
	if line == 0 {
		return file
	}
	return file + ":" + strconv.Itoa(line)
}

// maxTextSize is the largest file ReadText reads: no list of connections
// is larger, and a file chosen by mistake may be.
const maxTextSize = 8 << 20

// ErrTooLarge is ReadText's error for a file over its limit.
var ErrTooLarge = errors.New("file too large")

// ReadText reads the text file at path, as DecodeText decodes it: a file
// larger than a list of connections is ErrTooLarge.
func ReadText(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil {
		return "", err
	} else if st.IsDir() {
		return "", fmt.Errorf("%s: is a folder", path)
	} else if st.Size() > maxTextSize {
		return "", fmt.Errorf("%s: %w", path, ErrTooLarge)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(io.LimitReader(f, maxTextSize+1)); err != nil {
		return "", err
	}
	if buf.Len() > maxTextSize { // it grew
		return "", fmt.Errorf("%s: %w", path, ErrTooLarge)
	}
	return DecodeText(buf.Bytes()), nil
}

// DecodeText is the text of data, in whatever encoding the other clients
// write theirs in: a byte order mark says UTF-8 or UTF-16 (little or big
// endian); without one, valid UTF-8 is UTF-8, and anything else GB18030,
// the ANSI code page of Chinese Windows (a superset of GBK and GB2312),
// which Xshell, MobaXterm and Excel's "CSV" write on it. The mark is not
// in the text.
func DecodeText(data []byte) string {
	var dec interface {
		Bytes([]byte) ([]byte, error)
	}
	switch {
	case bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}):
		return string(data[3:])
	case bytes.HasPrefix(data, []byte{0xff, 0xfe}):
		dec = unicode.UTF16(unicode.LittleEndian, unicode.ExpectBOM).NewDecoder()
	case bytes.HasPrefix(data, []byte{0xfe, 0xff}):
		dec = unicode.UTF16(unicode.BigEndian, unicode.ExpectBOM).NewDecoder()
	case utf8.Valid(data):
		return string(data)
	default:
		dec = simplifiedchinese.GB18030.NewDecoder()
	}
	out, err := dec.Bytes(data)
	if err != nil {
		// The decoders replace what they cannot decode, so this is not
		// expected; the bytes as UTF-8 are better than nothing.
		return strings.ToValidUTF8(string(data), "�")
	}
	return string(out)
}

// sortSource puts src's entries in folder order, then by name, the top
// level first, and its problems by file, those that failed first.
func sortSource(src *Source) {
	slices.SortStableFunc(src.Entries, func(a, b *Entry) int {
		if n := slices.CompareFunc(a.Folder, b.Folder, compareFold); n != 0 {
			return n
		}
		return compareFold(a.Host.Title(), b.Host.Title())
	})
	slices.SortStableFunc(src.Problems, func(a, b Problem) int {
		if a.Reason.Failed() != b.Reason.Failed() {
			if a.Reason.Failed() {
				return -1
			}
			return 1
		}
		return strings.Compare(a.File, b.File)
	})
}

func compareFold(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) }

// SameTarget tells whether a and b reach the same account: the same
// address, regardless of case, port and user.
func SameTarget(a, b *config.Host) bool {
	return strings.EqualFold(strings.TrimSpace(a.Addr), strings.TrimSpace(b.Addr)) &&
		a.PortOr22() == b.PortOr22() && a.User == b.User
}

// Present is the host of s, not deleted, that h is the same target as:
// nil for none.
func Present(s *config.Store, h *config.Host) *config.Host {
	for _, x := range s.Live() {
		if SameTarget(x, h) {
			return x
		}
	}
	return nil
}

// Import adds the entries' hosts to s, each a copy with an ID, in the
// folder of its folder path: s's folder of that name in each folder from
// the top, or a new one. An entry's KeyFile is its host's key, and the
// host of its Jump, in s once the others are added, its jump host; its
// Password is not put in the host. Hosts present already, as Present has
// them, or earlier in entries, are skipped. Each host added is created at
// at, the time of the import. It returns the hosts added.
func Import(s *config.Store, entries []*Entry, at time.Time) []*config.Host {
	var added []*config.Host
	var from []*Entry // the entry of each host added
	for _, e := range entries {
		if Present(s, e.Host) != nil {
			continue
		}
		h := e.Host.Clone()
		h.ID = config.NewHostID()
		for s.Host(h.ID) != nil {
			h.ID += "-"
		}
		if e.KeyFile != "" {
			h.Auth, h.KeyPath = config.AuthKey, e.KeyFile
		}
		h.Folder = folderFor(s, e.Folder)
		h.CreatedAt = at
		s.Add(h)
		added = append(added, h)
		from = append(from, e)
	}
	for i, h := range added {
		if j := from[i].Jump; j != nil {
			if via := Present(s, j.Host); via != nil && via.ID != h.ID {
				h.Proxy = &config.ProxyConfig{Kind: config.ProxyJump, JumpHost: via.ID}
			}
		}
	}
	return added
}

// folderFor is the ID of the folder at path in s, made as needed: "" for
// the top level.
func folderFor(s *config.Store, path []string) string {
	parent := ""
	for _, name := range path {
		i := slices.IndexFunc(s.Subfolders(parent), func(f *config.Folder) bool { return f.Name == name })
		if i >= 0 {
			parent = s.Subfolders(parent)[i].ID
			continue
		}
		f := s.AddFolder(name, parent)
		for s.Folder(f.ID) != f {
			f.ID += "-"
		}
		parent = f.ID
	}
	return parent
}
