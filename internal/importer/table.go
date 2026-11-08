package importer

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"finallyshell/internal/config"
)

// A server list is a table the user keeps, in a spreadsheet or exported
// from one: a CSV or tab-separated text file, or an Excel workbook
// (.xlsx, read by xlsx.go), one server a row. No program defines the
// format; teams write their own, so the reader is lenient:
//
//   - The header row names the columns, in English or Chinese, case,
//     spaces, "_" and "-" ignored (tableHeaders). Only the host column is
//     needed; unknown columns are ignored. The header is the first row,
//     or the first of the first rows that has a host column, as a title
//     row may be above it.
//   - Rows without a value are skipped; one without a host, or with a
//     port that is not 1 to 65535, is a Problem with its row's number.
//   - A host written host:port ([IPv6]:port), or user@host, is the host
//     without them; the port, or the user, is used when its own column
//     has none. A bare IPv6 address is not split.
//   - A folder is a path: "Production/Databases", or with "\".
//   - A key file makes the host log in with that key; else with a
//     password, which, when the list has it, goes in Entry.Password: the
//     app keeps it in the system's credential store, never in hosts.json.
//     Cells are trimmed of spaces, but for the password's, whose spaces
//     may be part of it.
//
// A text file is in whatever encoding Excel saves it: "CSV UTF-8" has a
// byte order mark, plain "CSV" is the ANSI code page (GBK on Chinese
// Windows), "Unicode text" is UTF-16 with tabs; DecodeText reads them all.
// Its delimiter is the one, of a tab, ";" (Excel's CSV where the decimal
// separator is a comma, as in much of Europe) and ",", under which a row
// of the first ones is a header with a host column, so that a title row
// above does not count. A first line "sep=;", which Excel writes and
// reads, says it instead. Quotes are read leniently (LazyQuotes).
//
// The rows are kept in the file's order, as the user wrote them, rather
// than sorted as other sources are.

// Table is the source of a server list in a CSV or Excel file.
const Table Kind = "table"

var tableReader = &Reader{Kind: Table, Extensions: []string{"csv", "tsv", "txt", "xlsx", "xls"}, Read: ReadTable}

// Limits on what ReadTable reads, should the user choose another file.
const (
	maxTableSize = 10 << 20
	maxTableRows = 10000
	// headerRows are the rows, not empty, looked through for the header.
	headerRows = 10
)

// Reasons of a server list's rows.
const (
	// BadPort is a row whose port is not a number from 1 to 65535.
	BadPort Reason = "bad-port"
	// TooManyRows is the rows of a list after its first maxTableRows,
	// which are not read.
	TooManyRows Reason = "too-many-rows"
)

// ErrWorkbook is ReadTable's error for a spreadsheet it cannot read: an
// Excel 97-2003 workbook (.xls), one protected with a password, a zip
// file that is not an .xlsx, as an OpenDocument one, or markup, as an
// HTML table. A text file is read whatever its name, a .xls one too.
var ErrWorkbook = errors.New("not a workbook this can read: save it as .xlsx or CSV")

// NoHostColumnError is ReadTable's error for a list without a host
// column.
type NoHostColumnError struct {
	// Headers are the first row's values, as headers that were not known.
	Headers []string
}

func (e *NoHostColumnError) Error() string {
	return "no host column; the headers are: " + strings.Join(e.Headers, ", ")
}

// Column is a field a server list's column has: "host" and such.
type Column string

// The columns of a server list, in the order the template has them.
const (
	ColName     Column = "name"
	ColHost     Column = "host"
	ColPort     Column = "port"
	ColUser     Column = "user"
	ColPassword Column = "password"
	ColKey      Column = "key"
	ColFolder   Column = "folder"
	ColNotes    Column = "notes"
)

// TableColumns are the columns of a server list, in the template's order.
var TableColumns = []Column{ColName, ColHost, ColPort, ColUser, ColPassword, ColKey, ColFolder, ColNotes}

// tableHeaders are the headers of each column, as headerKey has them.
var tableHeaders = map[Column][]string{
	ColName:     {"name", "label", "title", "名称", "名字", "别名"},
	ColHost:     {"host", "hostname", "address", "ip", "ipaddress", "主机", "主机名", "主机地址", "地址", "ip地址"},
	ColPort:     {"port", "端口"},
	ColUser:     {"user", "username", "login", "用户", "用户名", "账号"},
	ColPassword: {"password", "密码"},
	ColKey:      {"key", "privatekey", "keyfile", "identityfile", "私钥", "私钥文件"},
	ColFolder:   {"folder", "group", "文件夹", "分组"},
	ColNotes:    {"notes", "note", "description", "comment", "comments", "备注", "说明"},
}

// headerKey is a header as tableHeaders has it: lower case, without
// spaces, "_" and "-".
func headerKey(h string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '_' || r == '-' {
			return -1
		}
		return unicode.ToLower(r)
	}, h)
}

// ColumnOf is the column the header h names: "" for none.
func ColumnOf(h string) Column {
	k := headerKey(h)
	for _, col := range TableColumns {
		if slices.Contains(tableHeaders[col], k) {
			return col
		}
	}
	return ""
}

// TableTemplate is a server list of one row, as the template the user
// fills in: the header of each column of TableColumns, then its value of
// example, "" for none. It is CSV in UTF-8 with a byte order mark, and
// CRLF line ends, as Excel writes "CSV UTF-8", so that Excel opens it
// with its Chinese right.
func TableTemplate(header, example func(Column) string) []byte {
	var buf bytes.Buffer
	buf.WriteString("\xef\xbb\xbf") // the byte order mark
	w := csv.NewWriter(&buf)
	w.UseCRLF = true
	var head, row []string
	for _, col := range TableColumns {
		head, row = append(head, header(col)), append(row, example(col))
	}
	w.WriteAll([][]string{head, row}) // to a buffer: no error
	return buf.Bytes()
}

// ReadTable reads the server list in the file at path: a CSV or text
// file, or an .xlsx workbook's first sheet. Rows it does not take a host
// from are Problems. It fails when the file cannot be read, is not a list
// (no host column: a NoHostColumnError), or has no rows.
func ReadTable(path string) (*Source, error) {
	data, err := readTableFile(path)
	if err != nil {
		return nil, err
	}
	name := filepath.Base(path)
	t := &tableRows{src: &Source{Kind: Table, Path: path}, file: name}
	switch {
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		err = xlsxRows(data, t.row)
	case bytes.HasPrefix(data, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}): // a compound file: .xls, or protected
		return nil, fmt.Errorf("%s: %w", name, ErrWorkbook)
	default:
		text := DecodeText(data)
		// Markup: an HTML table, or Excel's XML spreadsheet, which some
		// systems export as .xls.
		if strings.HasPrefix(strings.TrimSpace(text), "<") {
			return nil, fmt.Errorf("%s: %w", name, ErrWorkbook)
		}
		err = csvRows(text, t.row)
		// What follows a line that does not parse is not read.
		if le := (*lineError)(nil); errors.As(err, &le) {
			t.src.Problems = append(t.src.Problems, NewProblem(name, le.line, Broken, le.err))
			err = nil
		}
	}
	if err != nil {
		return nil, err
	}
	switch {
	case t.cols == nil && t.first != nil:
		return nil, &NoHostColumnError{Headers: t.first}
	case len(t.src.Entries) == 0 && len(t.src.Problems) == 0:
		return nil, fmt.Errorf("%s: %w", name, ErrNoConnections)
	}
	return t.src, nil
}

// readTableFile is the file at path, of at most maxTableSize bytes.
func readTableFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil {
		return nil, err
	} else if st.IsDir() {
		return nil, fmt.Errorf("%s: is a folder", path)
	} else if st.Size() > maxTableSize {
		return nil, fmt.Errorf("%s: %w", path, ErrTooLarge)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxTableSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxTableSize { // it grew
		return nil, fmt.Errorf("%s: %w", path, ErrTooLarge)
	}
	return data, nil
}

// tableRows makes a list's rows entries, one at a time.
type tableRows struct {
	src  *Source
	file string
	// first is the first row not empty, cols the index of each column
	// once the header is found: nil before.
	first []string
	cols  map[Column]int
	seen  int // rows not empty before the header, then rows of data
}

// row reads the row of the file's line: false when no more are read. The
// cells are as the file has them, spaces and all: add trims those it
// reads but the password.
func (t *tableRows) row(line int, cells []string) bool {
	if emptyRow(cells) {
		return true
	}
	t.seen++
	if t.cols == nil {
		if t.first == nil {
			t.first = trimCells(cells)
		}
		t.cols = headerColumns(cells)
		if t.cols == nil && t.seen >= headerRows {
			return false
		}
		if t.cols != nil {
			t.seen = 0
		}
		return true
	}
	if t.seen > maxTableRows {
		t.src.Problems = append(t.src.Problems, NewProblem(t.file, line, TooManyRows, nil))
		return false
	}
	t.add(line, cells)
	return true
}

// emptyRow tells whether cells have no value but spaces.
func emptyRow(cells []string) bool {
	return !slices.ContainsFunc(cells, func(c string) bool { return strings.TrimSpace(c) != "" })
}

// trimCells are the values of cells, trimmed, without the empty ones.
func trimCells(cells []string) []string {
	var out []string
	for _, c := range cells {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// headerColumns is the index of each column of the header cells: nil
// when none is a host's, and cells are not a header.
func headerColumns(cells []string) map[Column]int {
	cols := map[Column]int{}
	for i, c := range cells {
		if col := ColumnOf(c); col != "" {
			if _, ok := cols[col]; !ok {
				cols[col] = i
			}
		}
	}
	if _, ok := cols[ColHost]; !ok {
		return nil
	}
	return cols
}

// add makes the row of line an entry, or a problem.
func (t *tableRows) add(line int, cells []string) {
	// The cells, trimmed: but for the password, whose spaces may be its
	// own. One of spaces only is none.
	cell := func(col Column) string {
		i, ok := t.cols[col]
		if !ok || i >= len(cells) || strings.TrimSpace(cells[i]) == "" {
			return ""
		}
		if col == ColPassword {
			return cells[i]
		}
		return strings.TrimSpace(cells[i])
	}
	addr, port, user := cell(ColHost), cell(ColPort), cell(ColUser)
	if u, rest, ok := strings.Cut(addr, "@"); ok {
		addr = rest
		if user == "" {
			user = u
		}
	}
	// host:port or [IPv6]:port: the Port cell, when it has one, wins. A
	// bare IPv6 address, with its many colons, is not split.
	if h, p, err := net.SplitHostPort(addr); err == nil {
		addr = h
		if port == "" {
			port = p
		}
	}
	addr = strings.TrimSuffix(strings.TrimPrefix(addr, "["), "]") // an IPv6 address written [::1]
	if addr == "" {
		t.src.Problems = append(t.src.Problems, NewProblem(t.file, line, NoHost, nil))
		return
	}
	h := &config.Host{Name: cell(ColName), Addr: addr, User: user, Auth: config.AuthPassword, Notes: cell(ColNotes)}
	if port != "" {
		p, err := strconv.Atoi(numberText(port))
		if err != nil || p < 1 || p > 65535 {
			t.src.Problems = append(t.src.Problems, Problem{File: At(t.file, line), Reason: BadPort, Detail: port})
			return
		}
		if p != 22 {
			h.Port = p
		}
	}
	e := &Entry{File: At(t.file, line), Host: h, Folder: folderPath(cell(ColFolder)), KeyFile: cell(ColKey)}
	if e.KeyFile == "" {
		e.Password = cell(ColPassword)
	}
	t.src.Entries = append(t.src.Entries, e)
}

// folderPath is the folders of a path written "Production/Databases", or
// with "\": nil for the top level.
func folderPath(s string) []string {
	var path []string
	for _, name := range strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == '\\' }) {
		if name = strings.TrimSpace(name); name != "" {
			path = append(path, name)
		}
	}
	return path
}

// numberText is the number s as a list's cell shows it: "22" for "22.0"
// or "2.2E1", and s itself when it is not a decimal number.
func numberText(s string) string {
	if s == "" || strings.Trim(s, "0123456789.eE+-") != "" {
		return s
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// lineError is a line of a CSV file that does not parse.
type lineError struct {
	line int
	err  error
}

func (e *lineError) Error() string { return fmt.Sprintf("line %d: %v", e.line, e.err) }

// csvRows calls each with the rows of the CSV, or tab-separated, text,
// and the line each starts on, until it returns false. A line that does
// not parse is a lineError.
func csvRows(text string, each func(line int, cells []string) bool) error {
	skip := 0
	var comma rune
	first, rest, _ := strings.Cut(text, "\n")
	if sep, ok := strings.CutPrefix(strings.ToLower(strings.TrimSpace(first)), "sep="); ok && utf8.RuneCountInString(sep) == 1 {
		comma, _ = utf8.DecodeRuneInString(sep)
		text, skip = rest, 1
	} else {
		comma = headerComma(text)
	}
	if comma == 0 || comma == '"' || comma == '\r' || comma == '\n' || comma == utf8.RuneError {
		comma = ','
	}
	r := csv.NewReader(strings.NewReader(text))
	r.Comma, r.LazyQuotes, r.FieldsPerRecord = comma, true, -1
	for {
		cells, err := r.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			if pe := (*csv.ParseError)(nil); errors.As(err, &pe) {
				return &lineError{pe.StartLine + skip, pe.Err}
			}
			return err
		}
		line, _ := r.FieldPos(0)
		if !each(line+skip, cells) {
			return nil
		}
	}
}

// headerComma is the delimiter of the text's header: the one of tab, ";"
// and "," under which one of its first headerRows rows, not empty, has a
// host column, with the most columns known when more than one has. With
// none, as for a file without a host column, it is sniffComma's, of the
// first line not empty.
func headerComma(text string) rune {
	best, most := rune(0), 0
	for _, comma := range []rune{'\t', ';', ','} {
		r := csv.NewReader(strings.NewReader(text))
		r.Comma, r.LazyQuotes, r.FieldsPerRecord, r.ReuseRecord = comma, true, -1, true
		for rows := 0; rows < headerRows; {
			cells, err := r.Read()
			if err != nil {
				break
			}
			if emptyRow(cells) {
				continue
			}
			rows++
			if cols := headerColumns(cells); cols != nil {
				if len(cols) > most {
					best, most = comma, len(cols)
				}
				break
			}
		}
	}
	if best != 0 {
		return best
	}
	for l := range strings.Lines(text) {
		if strings.TrimSpace(l) != "" {
			return sniffComma(l)
		}
	}
	return ','
}

// sniffComma is the delimiter of the line head: a tab, ";" when it has
// more of them than of ",", else ",".
func sniffComma(head string) rune {
	switch {
	case strings.Contains(head, "\t"):
		return '\t'
	case strings.Count(head, ";") > strings.Count(head, ","):
		return ';'
	}
	return ','
}
