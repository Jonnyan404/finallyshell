package importer

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"finallyshell/internal/config"

	"golang.org/x/text/encoding/unicode"
)

// Xshell (NetSarang's) keeps each session in a file of its own,
// <name>.xsh, in its Sessions folder, whose subfolders are its folders.
// On Windows, where it runs, that folder is
//
//	Documents\NetSarang Computer\<version>\Xshell\Sessions   Xshell 6 and later
//	Documents\NetSarang\Xshell\Sessions                      Xshell 5
//	%APPDATA%\NetSarang\Xshell\Sessions                      earlier ones
//
// unless the user chose another in Xshell's options. Exporting sessions
// (File, Export) writes an .xts file, a zip of .xsh files in their
// folders.
//
// A session's file is INI: [SECTION] lines, then key=value lines. Xshell 6
// and later write it in UTF-16 LE with a byte order mark, older versions
// in the ANSI code page (GBK on Chinese Windows); DecodeText reads both.
// The keys read here:
//
//	[CONNECTION]                 Host, Port, Protocol (SSH, TELNET, RLOGIN,
//	                             SERIAL...), Description (the notes);
//	                             Proxy in Xshell 3 and 4
//	[CONNECTION:AUTHENTICATION]  UserName, Method (0 password, 1 public
//	                             key; some tools write PASSWORD and
//	                             PUBLICKEY), UserKey (the name of a key
//	                             in Xshell's own key store, UserKeys)
//	[CONNECTION:PROXY]           Proxy, the name of one of Xshell's proxies
//
// The session's name is its file's, without .xsh. [SessionInfo] and
// [Information] have a Description too, but it is the same for every file
// ("Xshell session file"), and it is not read. Sections and keys are
// matched regardless of case and of spaces around "=", and unknown ones
// are ignored. Password, in [CONNECTION:AUTHENTICATION], is encrypted by
// Xshell with a key of its own: it is not decrypted here, nor even read,
// as the parser keeps only the keys above. Keys of Xshell's store are not
// imported: a key host imports without a key file, for the user to choose.
//
// None of this is documented by NetSarang. Sources: session files
// published on GitHub (nemax/config-xshell, Xshell 3; genishs/temp,
// Xshell 5.2), which have the sections and keys above, Method=0 and
// Method=1 with a UserKey; NetSarang's support article "How can I use my
// Version 6 sessions in Version 5?" (UTF-16 from version 6, .xts is a
// zip of the session files in folders); the Xshell importers or
// converters of rssh-org/migrate (src/lib/sources/xshell.ts: Host, Port,
// Protocol, UserName, Method, UserKey, SSH only), findix's gist to
// MobaXterm (UTF-16 LE, Host, Port, UserName, Protocol, folders as
// directories), timedcy's gist to PuTTY (iconv from UTF-16) and
// ayanamist's gist from version 6 to 5 ([SessionInfo] Version, files
// written with "key = value"). It was not checked against users' own
// files, so the reader takes what it can: a missing Protocol is SSH, a
// missing or unknown Method is a key only with a UserKey, and UTF-16
// without its mark reads too.

// Xshell is the source of Xshell's sessions.
const Xshell Kind = "xshell"

var xshellReader = &Reader{Kind: Xshell, Folder: true, Find: FindXshell, Read: ReadXshell}

const (
	xshExt = ".xsh"
	xtsExt = ".xts"
	// maxArchiveSize is the largest .xts read, and maxArchiveData what is
	// read of its files, uncompressed: far more than any user's sessions.
	maxArchiveSize = 64 << 20
	maxArchiveData = 64 << 20
)

// XshellCandidates are the folders Xshell keeps its sessions in on this
// system, the newest version first: none but on Windows. FindXshell tries
// them; tests replace it.
var XshellCandidates = func() []string { return xshellCandidates(runtime.GOOS, os.Getenv, os.ReadDir) }

// xshellCandidates are Xshell's Sessions folders on goos, with env for the
// environment's variables and readDir to list the versions installed.
func xshellCandidates(goos string, env func(string) string, readDir func(string) ([]fs.DirEntry, error)) []string {
	if goos != "windows" {
		return nil
	}
	// Documents is in the profile, or in OneDrive when it backs it up.
	var docs []string
	for _, v := range []string{"USERPROFILE", "OneDrive"} {
		if dir := env(v); dir != "" {
			docs = append(docs, filepath.Join(dir, "Documents"))
		}
	}
	var out []string
	for _, d := range docs {
		base := filepath.Join(d, "NetSarang Computer")
		for _, v := range xshellVersions(readDir, base) {
			out = append(out, filepath.Join(base, v, "Xshell", "Sessions"))
		}
	}
	for _, d := range docs {
		out = append(out, filepath.Join(d, "NetSarang", "Xshell", "Sessions"))
	}
	if dir := env("APPDATA"); dir != "" {
		out = append(out, filepath.Join(dir, "NetSarang", "Xshell", "Sessions"))
	}
	return out
}

// xshellVersions are the folders of base named as versions ("6", "7",
// "8"), the newest first.
func xshellVersions(readDir func(string) ([]fs.DirEntry, error), base string) []string {
	entries, _ := readDir(base)
	type version struct {
		name string
		n    float64
	}
	var vs []version
	for _, e := range entries {
		if n, err := strconv.ParseFloat(e.Name(), 64); err == nil && e.IsDir() {
			vs = append(vs, version{e.Name(), n})
		}
	}
	slices.SortFunc(vs, func(a, b version) int {
		switch {
		case a.n > b.n:
			return -1
		case a.n < b.n:
			return 1
		}
		return strings.Compare(a.name, b.name)
	})
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.name
	}
	return out
}

// FindXshell is the first of XshellCandidates that is a folder: "" when
// none is. It reads the disk, so the app calls it off the UI thread.
func FindXshell() string {
	for _, dir := range XshellCandidates() {
		if isDir(dir) {
			return dir
		}
	}
	return ""
}

// ReadXshell reads the sessions of the folder at path, as Xshell's
// Sessions folder, the Xshell folder it is in, or the version's folder
// that one is in (see sessionsDir): its .xsh files, in folders as its subfolders are, and
// the .xts exports in it. path may be an .xts, or one .xsh, too. Files it
// does not read are Problems, as are sessions it does not import; Entries
// are in folder order, then by name. It fails when path cannot be read,
// or has no session's file at all.
func ReadXshell(path string) (*Source, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	r := &xshReader{src: &Source{Kind: Xshell, Path: path}}
	switch {
	case !st.IsDir() && hasExt(path, xtsExt):
		zr, err := openXts(path, st)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		r.archive(zr, filepath.Base(path), nil)
	case !st.IsDir():
		r.file(path, filepath.Base(path), nil)
	default:
		r.src.Path = sessionsDir(path)
		if _, err := os.ReadDir(r.src.Path); err != nil {
			return nil, err
		}
		r.walk(r.src.Path, "", nil, 0)
	}
	if r.found == 0 {
		return nil, fmt.Errorf("%s: %w", r.src.Path, ErrNoConnections)
	}
	sortSource(r.src)
	return r.src, nil
}

// sessionsDir is the folder of sessions dir is or has: the Sessions
// folder in it when dir is the Xshell folder (named so), or the one in its
// Xshell folder when dir is the version's folder; else dir itself. A
// folder of sessions may have a folder of its own named Sessions, so dir
// is the folder of sessions when it is named Sessions, as those FindXshell
// finds are, or has .xsh files of its own.
func sessionsDir(dir string) string {
	if strings.EqualFold(filepath.Base(dir), "Sessions") || hasSessions(dir) {
		return dir
	}
	if strings.EqualFold(filepath.Base(dir), "Xshell") {
		if p := filepath.Join(dir, "Sessions"); isDir(p) {
			return p
		}
	}
	if p := filepath.Join(dir, "Xshell", "Sessions"); isDir(p) {
		return p
	}
	return dir
}

// hasSessions tells whether dir has .xsh files of its own, not in its
// folders.
func hasSessions(dir string) bool {
	entries, _ := os.ReadDir(dir)
	return slices.ContainsFunc(entries, func(e fs.DirEntry) bool { return !e.IsDir() && hasExt(e.Name(), xshExt) })
}

// hasExt tells whether name ends in ext, regardless of case.
func hasExt(name, ext string) bool { return strings.EqualFold(filepath.Ext(name), ext) }

// xshName is the session of the file named name: its name without .xsh.
func xshName(name string) string {
	if hasExt(name, xshExt) {
		return name[:len(name)-len(xshExt)]
	}
	return name
}

type xshReader struct {
	src   *Source
	files int // the session files read, of maxFiles
	found int // the session files found, and the exports not read
}

func (r *xshReader) problem(file string, reason Reason, err error) {
	r.src.Problems = append(r.src.Problems, NewProblem(file, 0, reason, err))
}

// walk reads the folder abs, rel (with slashes) to the folder read, of the
// Xshell folder folder, at depth.
func (r *xshReader) walk(abs, rel string, folder []string, depth int) {
	entries, err := os.ReadDir(abs)
	if err != nil {
		if rel != "" {
			r.problem(rel, Unreadable, err)
		}
		return
	}
	for _, e := range entries {
		name := e.Name()
		p, relp := filepath.Join(abs, name), path.Join(rel, name)
		switch {
		case e.IsDir():
			if depth < maxDepth {
				r.walk(p, relp, append(slices.Clone(folder), name), depth+1)
			}
		case strings.HasPrefix(name, "._"): // macOS's metadata of a copy
		case hasExt(name, xshExt):
			r.file(p, relp, folder)
		case hasExt(name, xtsExt):
			r.walkArchive(p, relp, folder)
		}
	}
}

// walkArchive reads the .xts at abs, rel to the folder read, in the
// Xshell folder folder: one it cannot read is a problem.
func (r *xshReader) walkArchive(abs, rel string, folder []string) {
	st, err := os.Stat(abs)
	var zr *zip.ReadCloser
	if err == nil {
		zr, err = openXts(abs, st)
	}
	switch {
	case errors.Is(err, ErrTooLarge):
		r.problem(rel, TooLarge, nil)
	case err != nil && st == nil:
		r.problem(rel, Unreadable, err)
	case err != nil:
		r.problem(rel, Broken, err)
	default:
		defer zr.Close()
		r.archive(zr, rel, folder)
		return
	}
	r.found++
}

// file reads the session's file at abs, rel to the folder read, in the
// Xshell folder folder.
func (r *xshReader) file(abs, rel string, folder []string) {
	if r.files >= maxFiles {
		return
	}
	r.files++
	r.found++
	st, err := os.Stat(abs)
	if err == nil && st.Size() > maxFileSize {
		r.problem(rel, TooLarge, nil)
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		r.problem(rel, Unreadable, err)
		return
	}
	r.session(data, rel, xshName(filepath.Base(abs)), folder)
}

// openXts opens the .xts at abs, of the size st has: one larger than
// maxArchiveSize is ErrTooLarge.
func openXts(abs string, st fs.FileInfo) (*zip.ReadCloser, error) {
	if st.Size() > maxArchiveSize {
		return nil, fmt.Errorf("%s: %w", abs, ErrTooLarge)
	}
	zr, err := zip.OpenReader(abs)
	if err != nil {
		return nil, fmt.Errorf("not an Xshell export: %w", err)
	}
	return zr, nil
}

// archive reads the session files of the .xts zr, rel to the folder read,
// in the Xshell folder folder: those of the export's folders are in
// subfolders of folder. Nothing of it is written to disk.
func (r *xshReader) archive(zr *zip.ReadCloser, rel string, folder []string) {
	budget := int64(maxArchiveData)
	for _, f := range zr.File {
		parts := xtsPath(f)
		if len(parts) == 0 || len(parts) > maxDepth+1 {
			continue
		}
		base := parts[len(parts)-1]
		if f.FileInfo().IsDir() || !hasExt(base, xshExt) || strings.HasPrefix(base, "._") || parts[0] == "__MACOSX" {
			continue
		}
		if r.files >= maxFiles {
			return
		}
		r.files++
		r.found++
		file := rel + "/" + strings.Join(parts, "/")
		if f.UncompressedSize64 > maxFileSize || int64(f.UncompressedSize64) > budget {
			r.problem(file, TooLarge, nil)
			continue
		}
		data, err := readZipFile(f)
		if errors.Is(err, ErrTooLarge) || err == nil && int64(len(data)) > budget {
			r.problem(file, TooLarge, nil)
			continue
		} else if err != nil {
			r.problem(file, Broken, err)
			continue
		}
		budget -= int64(len(data))
		r.session(data, file, xshName(base), append(slices.Clone(folder), parts[:len(parts)-1]...))
	}
}

// xtsPath is the path of f in its export, as names: its name decoded
// (zips of Windows have them in its ANSI code page), "\" a separator too,
// and the export's Sessions folder, or Xshell\Sessions, left out.
func xtsPath(f *zip.File) []string {
	name := f.Name
	if f.NonUTF8 {
		name = DecodeText([]byte(name))
	}
	var parts []string
	for p := range strings.SplitSeq(strings.ReplaceAll(name, `\`, "/"), "/") {
		if p != "" && p != "." && p != ".." {
			parts = append(parts, p)
		}
	}
	if len(parts) > 2 && strings.EqualFold(parts[0], "Xshell") && strings.EqualFold(parts[1], "Sessions") {
		parts = parts[2:]
	} else if len(parts) > 1 && strings.EqualFold(parts[0], "Sessions") {
		parts = parts[1:]
	}
	return parts
}

// readZipFile reads f, one of maxFileSize at most: ErrTooLarge for more.
func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileSize {
		return nil, ErrTooLarge
	}
	return data, nil
}

// errNotXsh is the detail of a file without any INI section.
var errNotXsh = errors.New("not an Xshell session file")

// session makes the session's file data, named file, an entry, or a
// problem: name is the session's, folder its Xshell folder.
func (r *xshReader) session(data []byte, file, name string, folder []string) {
	ini := parseXsh(data)
	if ini == nil {
		r.problem(file, Broken, errNotXsh)
		return
	}
	conn, auth := ini["connection"], ini["connection:authentication"]
	if p := conn["protocol"]; p != "" && !strings.HasPrefix(strings.ToUpper(p), "SSH") {
		r.problem(file, NotSSH, nil)
		return
	}
	addr := conn["host"]
	if addr == "" {
		r.problem(file, NoHost, nil)
		return
	}
	h := &config.Host{
		Name:  name,
		Addr:  addr,
		User:  auth["username"],
		Auth:  config.AuthPassword,
		Notes: conn["description"],
	}
	if p, err := strconv.Atoi(conn["port"]); err == nil && p > 0 && p <= 65535 && p != 22 {
		h.Port = p
	}
	if xshKeyAuth(auth["method"], auth["userkey"]) {
		h.Auth = config.AuthKey
	}
	if len(folder) == 0 {
		folder = nil
	}
	e := &Entry{File: file, Host: h, Folder: folder, NeedsKey: h.Auth == config.AuthKey}
	if xshProxy(conn["proxy"]) || xshProxy(conn["firewall"]) ||
		xshProxy(ini["connection:proxy"]["proxy"]) || xshProxy(ini["connection:proxy"]["firewall"]) {
		e.Note = ProxyNotImported
	}
	r.src.Entries = append(r.src.Entries, e)
}

// xshKeyAuth tells whether a session of the Method method, with the
// UserKey key, logs in with a key: method 1 or PUBLICKEY, or no method
// this knows and a key.
func xshKeyAuth(method, key string) bool {
	m := strings.ToUpper(strings.ReplaceAll(method, " ", ""))
	switch {
	case m == "1" || strings.Contains(m, "PUBLICKEY"):
		return true
	case m == "" || strings.Trim(m, "0123456789") != "" && !strings.Contains(m, "PASSWORD"):
		return key != ""
	}
	return false
}

// xshProxy tells whether name, a Proxy key's, names a proxy: "" and
// <None> do not.
func xshProxy(name string) bool {
	n := strings.Trim(name, "<>")
	return n != "" && !strings.EqualFold(n, "none")
}

// xshKeys are the keys parseXsh keeps, by section, in lower case: the
// others, the password among them, are not even kept.
var xshKeys = map[string][]string{
	"connection":                {"host", "port", "protocol", "description", "proxy", "firewall"},
	"connection:authentication": {"username", "method", "userkey"},
	"connection:proxy":          {"proxy", "firewall"},
}

// parseXsh is the keys of xshKeys that data, a session's file, has, by
// section, both in lower case, the values trimmed: nil for a file without
// a section, which is not one. Of a key given twice, the last counts.
func parseXsh(data []byte) map[string]map[string]string {
	out := map[string]map[string]string{}
	sections := 0
	var keys []string
	section := ""
	for line := range strings.Lines(decodeXsh(data)) {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || line[0] == ';' || line[0] == '#':
		case line[0] == '[' && strings.HasSuffix(line, "]"):
			sections++
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			keys = xshKeys[section]
		default:
			k, v, ok := strings.Cut(line, "=")
			k = strings.ToLower(strings.TrimSpace(k))
			if !ok || !slices.Contains(keys, k) {
				continue
			}
			if out[section] == nil {
				out[section] = map[string]string{}
			}
			out[section][k] = strings.TrimSpace(v)
		}
	}
	if sections == 0 {
		return nil
	}
	return out
}

// decodeXsh is the text of a session's file: as DecodeText has it, and
// UTF-16 LE without its byte order mark, as an INI file of ASCII starts.
func decodeXsh(data []byte) string {
	if len(data) >= 4 && data[0] != 0 && data[1] == 0 && data[2] != 0 && data[3] == 0 {
		if out, err := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewDecoder().Bytes(data); err == nil {
			return string(out)
		}
	}
	return DecodeText(data)
}
