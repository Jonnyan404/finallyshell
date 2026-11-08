package importer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"finallyshell/internal/config"
)

// FinalShell keeps its data in a folder of its own, its 数据目录: the
// install folder on Windows when it was installed there, else one in the
// user's profile. Its conn folder holds the connections and folders:
//
//	conn/
//	  <id>_connect_config.json     a connection at the top level
//	  <folder id>/folder.json      a folder: {"id", "name", "parent_id"}
//	  <folder id>/<id>_connect_config.json
//
// Folders name the folder they are in by its id in parent_id ("root" or
// "" for the top level), as connections do; the folders' directories may
// nest as the folders do. Exporting from FinalShell (right-click, Export)
// writes the same *_connect_config.json files in directories named after
// the folders, without folder.json, so a directory without one is a
// folder of its name.
//
// A connection's file is one JSON object; the keys read here, from what
// other importers found in real files (none is documented by FinalShell):
//
//	id                   its id, also in the file's name
//	name, host, port     port a number, or a string in some versions
//	user_name
//	conection_type       sic: 100 is SSH; others are RDP, VNC and such
//	authentication_type  1 password, 2 public key
//	secret_key_id        the key, in FinalShell's own key store
//	parent_id            the folder's id
//	description          the notes
//	proxy_id             a proxy of FinalShell's own list
//	exec_channel_enable  the server monitor's exec channel
//	delete_time          non-zero for a connection FinalShell deleted
//	password             encrypted by FinalShell: never read here
//
// Others (terminal_encoding, port_forwarding_list, backspace_key_sequence,
// window sizes, times) are not imported. The password is encrypted with a
// key FinalShell derives itself; this package does not decrypt it, and
// does not even decode it: the struct the files are read into has no
// field for it. Users type the password at the first connection, as for
// any host. Private keys are in FinalShell's config.json, not in the
// connection; that file is not read either, and key hosts import without
// a key file for the user to choose.
//
// Sources: the FinalShell importers of Hatoba (scarletkc/Hatoba,
// src-tauri/src/import/finalshell.rs), LiteShell (lanxiake/liteshell,
// src/main/finalshell.ts), MeatShell (yituorou/meatshell,
// src/config/impls/finalshell.rs) and OpenFinalShell
// (kexue-aihao/openfinalshell, src/main/services/finalshellImport.ts),
// which agree on the layout and the keys above; Scoop's FinalShell
// manifest, which persists the install folder's conn.

// FinalShell is the source of FinalShell's connections.
const FinalShell Kind = "finalshell"

var finalShellReader = &Reader{Kind: FinalShell, Folder: true, Find: FindFinalShell, Read: ReadFinalShell}

// ConnDir is the name of FinalShell's folder of connections.
const ConnDir = "conn"

// Limits on what ReadFinalShell reads, should the user choose a folder that is not
// FinalShell's, as their home.
const (
	maxFiles    = 10000
	maxFileSize = 1 << 20
	maxDepth    = 16
)

// FinalShellCandidates are the folders FinalShell keeps its data in on
// this system, the likeliest first. FindFinalShell tries them; tests
// replace it.
var FinalShellCandidates = func() []string { return candidates(runtime.GOOS, os.Getenv) }

// candidates are FinalShell's data folders on goos, with env for the
// environment's variables.
func candidates(goos string, env func(string) string) []string {
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
		// The installer's default, then the roaming profile, then the
		// install folders, where FinalShell keeps its data when it can
		// write there.
		local := env("LOCALAPPDATA")
		if local == "" && home != "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		add(local, "finalshell")
		add(env("APPDATA"), "finalshell")
		add(env("ProgramFiles"), "FinalShell")
		add(env("ProgramFiles(x86)"), "FinalShell")
		if drive := env("SystemDrive"); drive != "" {
			add(drive+`\`, "finalshell")
		}
	case "darwin":
		add(home, "Library", "FinalShell")
		add(home, "Library", "Application Support", "finalshell")
	default:
		add(home, ".finalshell")
		data := env("XDG_DATA_HOME")
		if data == "" && home != "" {
			data = filepath.Join(home, ".local", "share")
		}
		add(data, "finalshell")
	}
	return out
}

// FindFinalShell is the first of FinalShellCandidates that has a conn
// folder: "" when none has. It reads the disk, so the app calls it off
// the UI thread.
func FindFinalShell() string {
	for _, dir := range FinalShellCandidates() {
		if isDir(filepath.Join(dir, ConnDir)) {
			return dir
		}
	}
	return ""
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// ReadFinalShell reads the connections of FinalShell's data folder dir,
// or of its conn folder, or of a folder FinalShell exported. Files that do
// not parse are Problems, as are connections it does not import; Entries
// are in folder order, then by name. It fails when dir cannot be read, or
// has no connection's file at all.
func ReadFinalShell(dir string) (*Source, error) {
	conn := dir
	if sub := filepath.Join(dir, ConnDir); isDir(sub) {
		conn = sub
	}
	if _, err := os.ReadDir(conn); err != nil {
		return nil, err
	}
	r := &fsReader{src: &Source{Kind: FinalShell, Path: conn}, folders: map[string]*fsFolder{}, dirFolder: map[string]string{}}
	r.walk(conn, "", 0)
	if r.connFiles == 0 {
		return nil, fmt.Errorf("%s: %w", conn, ErrNoConnections)
	}
	for _, c := range r.conns {
		r.add(c)
	}
	sortSource(r.src)
	return r.src, nil
}

// fsFolder is a folder of FinalShell's: from a folder.json, or a
// directory of an export.
type fsFolder struct {
	id, name, parent string
	deleted          bool
}

// fsConn is a connection's file as read: the keys Read uses, in whatever
// type the file has them. There is no field for the password, which is
// therefore never decoded.
type fsConn struct {
	ID          json.RawMessage `json:"id"`
	Name        json.RawMessage `json:"name"`
	Host        json.RawMessage `json:"host"`
	Port        json.RawMessage `json:"port"`
	UserName    json.RawMessage `json:"user_name"`
	Type        json.RawMessage `json:"conection_type"`
	Auth        json.RawMessage `json:"authentication_type"`
	SecretKeyID json.RawMessage `json:"secret_key_id"`
	ParentID    json.RawMessage `json:"parent_id"`
	Description json.RawMessage `json:"description"`
	ProxyID     json.RawMessage `json:"proxy_id"`
	ExecChannel json.RawMessage `json:"exec_channel_enable"`
	DeleteTime  json.RawMessage `json:"delete_time"`

	file string // relative to the conn folder
	dir  string // the folder of the directory it is in
}

// fsFolderJSON is a folder.json.
type fsFolderJSON struct {
	ID         json.RawMessage `json:"id"`
	Name       json.RawMessage `json:"name"`
	ParentID   json.RawMessage `json:"parent_id"`
	DeleteTime json.RawMessage `json:"delete_time"`
}

type fsReader struct {
	src     *Source
	folders map[string]*fsFolder // by id
	// dirFolder is the folder of each directory, by its path relative to
	// the conn folder: "" for the top level.
	dirFolder map[string]string
	conns     []*fsConn
	files     int
	connFiles int
}

// walk reads the directory rel of the conn folder, at depth, whose
// enclosing directory's folder is the one of filepath.Dir(rel).
func (r *fsReader) walk(abs, rel string, depth int) {
	entries, err := os.ReadDir(abs)
	if err != nil {
		if rel != "" {
			r.problem(rel, Unreadable, err)
		}
		return
	}
	outer := ""
	if rel != "" {
		outer = r.dirFolder[parentRel(rel)]
		// A directory is its folder.json's folder, or one of its name.
		id := "dir:" + filepath.ToSlash(rel)
		f := &fsFolder{id: id, name: filepath.Base(rel), parent: outer}
		if data, ok := r.readFile(filepath.Join(abs, "folder.json"), filepath.Join(rel, "folder.json")); ok {
			var v fsFolderJSON
			if err := decodeObject(data, &v); err != nil {
				r.problem(filepath.Join(rel, "folder.json"), Broken, err)
			} else {
				if fid := text(v.ID); fid != "" {
					f.id = fid
				}
				if name := text(v.Name); name != "" {
					f.name = name
				}
				f.parent = topOr(text(v.ParentID), outer)
				f.deleted = number(v.DeleteTime) != 0
			}
		}
		if r.folders[f.id] == nil {
			r.folders[f.id] = f
		}
		r.dirFolder[rel] = f.id
	}
	for _, e := range entries {
		name := e.Name()
		path, relPath := filepath.Join(abs, name), filepath.Join(rel, name)
		switch {
		case e.IsDir():
			if depth < maxDepth {
				r.walk(path, relPath, depth+1)
			}
		case strings.HasSuffix(strings.ToLower(name), "_connect_config.json"):
			r.connFiles++
			data, ok := r.readFile(path, relPath)
			if !ok {
				continue
			}
			r.parseConn(data, relPath)
		}
	}
}

// parentRel is the directory rel is in, "" for the conn folder.
func parentRel(rel string) string {
	p := filepath.Dir(rel)
	if p == "." {
		return ""
	}
	return p
}

// topOr is the folder parent_id names: "" for the top level, as FinalShell
// writes "root", or outer when there is none.
func topOr(parent, outer string) string {
	switch parent {
	case "":
		return outer
	case "root":
		return ""
	}
	return parent
}

// readFile reads the file at path, rel to the conn folder, noting a
// problem when it cannot: ok is false then, and for a file that is not
// there.
func (r *fsReader) readFile(path, rel string) ([]byte, bool) {
	if r.files >= maxFiles {
		return nil, false
	}
	st, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false
	}
	r.files++
	if err == nil && st.Size() > maxFileSize {
		r.problem(rel, TooLarge, nil)
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		r.problem(rel, Unreadable, err)
		return nil, false
	}
	return data, true
}

func (r *fsReader) problem(rel string, reason Reason, err error) {
	p := Problem{File: filepath.ToSlash(rel), Reason: reason}
	if err != nil {
		p.Detail = err.Error()
	}
	r.src.Problems = append(r.src.Problems, p)
}

// parseConn reads a connection's file: one object, or a list of them.
func (r *fsReader) parseConn(data []byte, rel string) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // a BOM
	var list []json.RawMessage
	if t := bytes.TrimSpace(data); len(t) > 0 && t[0] == '[' {
		if err := json.Unmarshal(t, &list); err != nil {
			r.problem(rel, Broken, err)
			return
		}
	} else {
		list = []json.RawMessage{data}
	}
	broken := false
	for _, raw := range list {
		c := &fsConn{file: filepath.ToSlash(rel), dir: r.dirFolder[parentRel(rel)]}
		if err := decodeObject(raw, c); err != nil {
			if !broken { // once for the file
				r.problem(rel, Broken, err)
			}
			broken = true
			continue
		}
		r.conns = append(r.conns, c)
	}
}

// decodeObject decodes the JSON object data into v; anything else, null
// included, is an error.
func decodeObject(data []byte, v any) error {
	t := bytes.TrimSpace(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
	if len(t) == 0 || t[0] != '{' {
		return errors.New("not a JSON object")
	}
	return json.Unmarshal(t, v)
}

// add makes the connection c an entry, or a problem, once every folder is
// known.
func (r *fsReader) add(c *fsConn) {
	bad := func(reason Reason) { r.src.Problems = append(r.src.Problems, Problem{File: c.file, Reason: reason}) }
	if number(c.DeleteTime) != 0 {
		bad(DeletedThere)
		return
	}
	if t, ok := numberOK(c.Type); ok && t != 100 {
		bad(NotSSH)
		return
	}
	addr := text(c.Host)
	if addr == "" {
		bad(NoHost)
		return
	}
	h := &config.Host{
		Name:  text(c.Name),
		Addr:  addr,
		User:  text(c.UserName),
		Auth:  config.AuthPassword,
		Notes: strings.TrimSpace(textRaw(c.Description)),
	}
	if p := number(c.Port); p > 0 && p <= 65535 && p != 22 {
		h.Port = int(p)
	}
	// 2 is a key; with no type, or one this does not know, a key's id is.
	if auth := number(c.Auth); auth == 2 || auth != 1 && text(c.SecretKeyID) != "" {
		h.Auth = config.AuthKey
	}
	if on, ok := boolean(c.ExecChannel); ok && !on {
		h.NoMonitor = true
	}
	e := &Entry{
		File:     c.file,
		Host:     h,
		Folder:   r.folderPath(topOr(text(c.ParentID), c.dir), c.dir),
		NeedsKey: h.Auth == config.AuthKey,
	}
	if text(c.ProxyID) != "" {
		e.Note = ProxyNotImported
	}
	r.src.Entries = append(r.src.Entries, e)
}

// folderPath is the names of the folder id and those it is in, the
// top-level one first, without deleted folders. An id no folder has is
// the folder of the directory, dir.
func (r *fsReader) folderPath(id, dir string) []string {
	if id != "" && r.folders[id] == nil {
		id = dir
	}
	var path []string
	for range len(r.folders) + 1 { // a loop of parents ends
		f := r.folders[id]
		if f == nil {
			break
		}
		if !f.deleted && strings.TrimSpace(f.name) != "" {
			path = append(path, strings.TrimSpace(f.name))
		}
		id = f.parent
	}
	slices.Reverse(path)
	return path
}

// textRaw is a JSON string, or a number's digits; "" for anything else.
func textRaw(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

// text is textRaw trimmed.
func text(raw json.RawMessage) string { return strings.TrimSpace(textRaw(raw)) }

// numberOK is a JSON number, or a string of one, as an integer.
func numberOK(raw json.RawMessage) (int64, bool) {
	s := text(raw)
	if s == "" {
		return 0, false
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, true
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int64(f), true
	}
	return 0, false
}

// number is numberOK's number, 0 for none.
func number(raw json.RawMessage) int64 {
	n, _ := numberOK(raw)
	return n
}

// boolean is a JSON true or false, or a string of one.
func boolean(raw json.RawMessage) (value, ok bool) {
	if json.Unmarshal(raw, &value) == nil {
		return value, true
	}
	if b, err := strconv.ParseBool(text(raw)); err == nil {
		return b, true
	}
	return false, false
}
