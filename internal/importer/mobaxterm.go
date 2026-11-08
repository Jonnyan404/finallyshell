package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"finallyshell/internal/config"
)

// MobaXterm keeps its sessions in MobaXterm.ini: in %APPDATA%\MobaXterm
// for the installed version, next to MobaXterm.exe for the portable one.
// Exporting sessions (right-click "User sessions", "Export all sessions
// to file", or a folder's "Export sessions from this folder") writes the
// same sections to a .mxtsessions file, and "Save session to file" one
// session to a .moba file. All are read alike.
//
// The file is an INI file with CRLF lines, in the system's ANSI code page
// (Windows-1252 on Western Windows, GBK on Chinese Windows), which
// ReadText decodes as GB18030 when it is not UTF-8: the letters beyond
// ASCII of a Windows-1252 file (é, €) are not read right. The sessions are in
// the sections [Bookmarks] and [Bookmarks_N]; every other section (the
// settings, [Passwords], [Credentials] and such) is skipped unread. A
// section is a folder:
//
//	[Bookmarks_1]
//	SubRep=Production\Databases    the folder's path, "\" between folders;
//	                               empty for the top level ("User sessions")
//	ImgNum=41                      the folder's icon: ignored
//	pg-main=#109#0%db.example.com%5022%postgres%...#MobaFont%10%...#0#primary#-1
//
// A partial export has the exported folder in its [Bookmarks] SubRep,
// which is then where its top-level sessions go. Every other key of a
// section is a session, its name; the value is fields separated by "#":
//
//	0  ";  logout" or " ": whether to say so when the session ends
//	1  the session's icon
//	2  the settings, separated by "%", below
//	3  the terminal's settings (font, colors): not imported
//	4  how it starts (tab, window): not imported
//	5  the comment: the host's notes
//	6  the tab's color: not imported
//
// The settings read, by index, for an SSH session:
//
//	0       the session type: 0 SSH; others (4 RDP, 5 VNC, 7 SFTP,
//	        11 browser and such) are skipped as NotSSH
//	1, 2, 3 host, port (22 when missing), user ("<default>" for none)
//	8, 9, 10  the SSH gateways' hosts, ports and users, "__PIPE__"
//	        between them: the gateway (jump host) is not imported, the
//	        host connects directly, with the note JumpHostNotImported
//	14      the private key's path, "_CurrentDrive_" for the drive
//	        letter "C" (`_CurrentDrive_:\Users\…`): the host's key file
//	19      the proxy type, 0 for none (20 to 22 its host, port, login):
//	        not imported, with the note ProxyNotImported
//
// The other settings (X11 forwarding, compression, the command run at
// login, the gateways' keys, ciphers and such) are not imported. Fields
// missing at the end of a line, as of sessions last saved by an older
// version, have their defaults. In values, MobaXterm writes "__PTVIRG__"
// for ";", "__DBLQUO__" for `"`, "__PIPE__" for "|", "__PERCENT__" for
// "%" and "__DIEZE__" for "#"; they are undone.
//
// MobaXterm keeps passwords apart from the sessions, encrypted (with a
// master password, in recent versions): they are not read. Its SSH
// tunnels (MobaSSHTunnel) are not read either.
//
// The format is not documented by Mobatek. What is above is from the
// description reverse-engineered for MobaXterm 26.3 in Ruzgfpegk's gist
// ".mxtsessions file format"
// (gist.github.com/Ruzgfpegk/ab597838e4abbe8de30d7224afd062ea), and
// from the open source readers of these files, which agree with it on
// what is read here: mobaConverterGo (dbauer23/mobaConverterGo,
// cmd/convert/moba2json.go, data/config.json), mobaxterm2ssh
// (gplv2/mobaxterm2ssh, lib/parsers/mobaxterm.js) and mobaxterm-mcp
// (bobzzgm/mobaxterm-mcp, src/services/ini-parser.ts, which also reads
// GBK files and puts Documents\MobaXterm\MobaXterm.ini first). Where
// these differ, this follows the gist and the first two: the session
// type is the first setting, not the icon (which the user can change),
// and every SubRep is its section's folder. None of it has been checked
// against a user's files yet, so the reading is lenient: unknown
// sections, keys and fields are ignored, and a line it cannot read is a
// Problem of its own.

// MobaXterm is the source of MobaXterm's sessions.
const MobaXterm Kind = "mobaxterm"

var mobaXtermReader = &Reader{
	Kind:       MobaXterm,
	Extensions: []string{"ini", "mxtsessions", "moba"},
	Find:       FindMobaXterm,
	Read:       ReadMobaXterm,
}

// MobaXtermCandidates are the files MobaXterm keeps its sessions in on
// this system, the likeliest first: none outside Windows. FindMobaXterm
// tries them; tests replace it.
var MobaXtermCandidates = func() []string { return mobaCandidates(runtime.GOOS, os.Getenv) }

// mobaCandidates are MobaXterm.ini's places on goos, with env for the
// environment's variables: the installed version's, then the one some
// installs keep in Documents. The portable version's is next to
// MobaXterm.exe, wherever that is: the user chooses it.
func mobaCandidates(goos string, env func(string) string) []string {
	if goos != "windows" {
		return nil
	}
	var out []string
	if dir := env("APPDATA"); dir != "" {
		out = append(out, filepath.Join(dir, "MobaXterm", "MobaXterm.ini"))
	}
	if home := env("USERPROFILE"); home != "" {
		out = append(out, filepath.Join(home, "Documents", "MobaXterm", "MobaXterm.ini"))
	}
	return out
}

// FindMobaXterm is the first of MobaXtermCandidates that is a file: ""
// when none is. It reads the disk, so the app calls it off the UI thread.
func FindMobaXterm() string {
	for _, path := range MobaXtermCandidates() {
		if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
			return path
		}
	}
	return ""
}

// mobaGOOS is the system the key paths are for: a Windows path is a key
// file only on Windows. Tests replace it.
var mobaGOOS = runtime.GOOS

// ReadMobaXterm reads the sessions of MobaXterm.ini, or of a file of
// sessions exported from MobaXterm, at path. Lines it cannot read, and
// sessions it does not import, are Problems; Entries are in folder
// order, then by name. It fails when the file cannot be read, or has no
// session at all.
func ReadMobaXterm(path string) (*Source, error) {
	text, err := ReadText(path)
	if err != nil {
		return nil, err
	}
	src := &Source{Kind: MobaXterm, Path: path}
	file := filepath.Base(path)
	sessions := 0
	// The sessions of the section being read, which get its folder at its
	// end, as SubRep need not come first.
	var section []*Entry
	inBookmarks, header := false, true // header: before any section
	var folder []string
	flush := func() {
		for _, e := range section {
			e.Folder = folder
		}
		src.Entries = append(src.Entries, section...)
		section, folder = nil, nil
	}
	for i, line := range strings.Split(text, "\n") {
		n := i + 1
		line = strings.TrimSpace(line) // and the CR
		if line == "" || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			flush()
			name := strings.TrimSpace(strings.TrimSuffix(line[1:], "]"))
			inBookmarks, header = isBookmarks(name), false
			continue
		}
		if !inBookmarks && !header {
			continue // another section: never read
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		switch {
		case inBookmarks && strings.EqualFold(key, "SubRep"):
			folder = mobaFolder(value)
			continue
		case inBookmarks && strings.EqualFold(key, "ImgNum"):
			continue
		case header && (!ok || strings.Count(value, "#") < 2):
			// Before any section, as in a .moba file without one, only
			// what reads as a session is one.
			continue
		}
		sessions++
		if !ok || key == "" {
			src.Problems = append(src.Problems, NewProblem(file, n, Broken, nil))
			continue
		}
		e, p := mobaSession(key, value)
		if p != nil {
			p.File = At(file, n)
			src.Problems = append(src.Problems, *p)
			continue
		}
		e.File = At(file, n)
		section = append(section, e)
	}
	flush()
	if sessions == 0 {
		return nil, fmt.Errorf("%s: %w", path, ErrNoConnections)
	}
	sortSource(src)
	return src, nil
}

// isBookmarks tells whether the section name is one of sessions:
// Bookmarks, or Bookmarks_ and a number.
func isBookmarks(name string) bool {
	const prefix = "bookmarks"
	if len(name) < len(prefix) || !strings.EqualFold(name[:len(prefix)], prefix) {
		return false
	}
	rest := name[len(prefix):]
	if rest == "" {
		return true
	}
	n, ok := strings.CutPrefix(rest, "_")
	if !ok || n == "" {
		return false
	}
	_, err := strconv.ParseUint(n, 10, 32)
	return err == nil
}

// mobaFolder is the folder path of a SubRep: nil for the top level.
func mobaFolder(subRep string) []string {
	var path []string
	for _, name := range strings.Split(subRep, `\`) {
		if name = strings.TrimSpace(mobaUnescape(name)); name != "" {
			path = append(path, name)
		}
	}
	return path
}

// mobaEscapes are what MobaXterm writes for characters of the format.
var mobaEscapes = strings.NewReplacer(
	"__PTVIRG__", ";",
	"__DBLQUO__", `"`,
	"__PIPE__", "|",
	"__PERCENT__", "%",
	"__DIEZE__", "#",
)

// mobaUnescape is the value v as the user wrote it.
func mobaUnescape(v string) string { return mobaEscapes.Replace(v) }

// mobaSession is the session named name, of the value value: its entry,
// without file or folder, or the problem, without file, of one not
// imported.
func mobaSession(name, value string) (*Entry, *Problem) {
	fields := strings.Split(value, "#")
	if len(fields) < 3 {
		return nil, &Problem{Reason: Broken, Detail: fmt.Sprintf("session %q: no settings", name)}
	}
	settings := strings.Split(fields[2], "%")
	setting := func(i int) string {
		if i < len(settings) {
			return strings.TrimSpace(mobaUnescape(settings[i]))
		}
		return ""
	}
	name = strings.TrimSpace(mobaUnescape(name))
	// The type is never empty in the files described; one that is, is
	// taken for SSH, the default.
	if t := setting(0); t != "" && t != "0" {
		return nil, &Problem{Reason: NotSSH, Detail: fmt.Sprintf("session %q: type %s", name, t)}
	}
	addr := setting(1)
	if addr == "" {
		return nil, &Problem{Reason: NoHost, Detail: fmt.Sprintf("session %q", name)}
	}
	h := &config.Host{Name: name, Addr: addr, Auth: config.AuthPassword}
	if p, err := strconv.Atoi(setting(2)); err == nil && p > 0 && p <= 65535 && p != 22 {
		h.Port = p
	}
	if u := setting(3); u != "<default>" {
		h.User = u
	}
	if len(fields) > 5 {
		h.Notes = strings.TrimSpace(mobaUnescape(fields[5]))
	}
	e := &Entry{Host: h}
	if key := mobaKeyPath(setting(14)); key != "" {
		h.Auth = config.AuthKey
		if isWindowsPath(key) && mobaGOOS != "windows" {
			// A path on the Windows MobaXterm ran on, which is not a file
			// here: the user chooses the key.
			e.NeedsKey = true
		} else {
			e.KeyFile = key
		}
	}
	switch {
	case strings.Trim(setting(8), "|") != "":
		e.Note = JumpHostNotImported
	case setting(19) != "" && setting(19) != "0":
		e.Note = ProxyNotImported
	}
	return e, nil
}

// mobaKeyPath is the key path of the setting path, "_CurrentDrive_" back
// to "C": MobaXterm writes it for the drive letter alone, so
// `_CurrentDrive_:\Users\…` is `C:\Users\…`. A path with no colon after
// it, `_CurrentDrive_\Users\…`, is not one MobaXterm is known to write,
// but would be on C: too: the colon is put back.
func mobaKeyPath(path string) string {
	const drive = "_CurrentDrive_"
	if rest, ok := strings.CutPrefix(path, drive); ok && (strings.HasPrefix(rest, `\`) || strings.HasPrefix(rest, "/")) {
		path = drive + ":" + rest
	}
	return strings.ReplaceAll(path, drive, "C")
}

// isWindowsPath tells whether path is a Windows one: a drive's, as
// `C:\…`, or a share's, as `\\server\…`.
func isWindowsPath(path string) bool {
	if strings.HasPrefix(path, `\\`) {
		return true
	}
	return len(path) >= 2 && path[1] == ':' &&
		('a' <= path[0] && path[0] <= 'z' || 'A' <= path[0] && path[0] <= 'Z')
}
