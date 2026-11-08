package importer

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"finallyshell/internal/config"
)

// tableEntry is an entry as the tests write it.
func tableEntry(e *Entry) string {
	h := e.Host
	s := fmt.Sprintf("%s|%s|%s:%d|%s|%s", e.File, h.Name, h.Addr, h.PortOr22(), h.User, e.FolderLabel())
	if e.KeyFile != "" {
		s += "|key " + e.KeyFile
	}
	if e.Password != "" {
		s += "|pw " + e.Password
	}
	if h.Notes != "" {
		s += "|" + h.Notes
	}
	return s
}

func tableProblem(p Problem) string {
	s := p.File + " " + string(p.Reason)
	if p.Detail != "" {
		s += " " + p.Detail
	}
	return s
}

// readTable reads the sample name of testdata/table.
func readTable(t *testing.T, name string) *Source {
	t.Helper()
	path := filepath.Join("testdata", "table", name)
	src, err := ReadTable(path)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if src.Kind != Table || src.Path != path {
		t.Errorf("%s: source %+v", name, src)
	}
	return src
}

func checkTable(t *testing.T, name string, src *Source, entries, problems []string) {
	t.Helper()
	var got, gotProblems []string
	for _, e := range src.Entries {
		got = append(got, tableEntry(e))
		if e.Host.ID != "" || e.Host.Folder != "" {
			t.Errorf("%s: host %+v", name, e.Host)
		}
		if want := config.AuthPassword; e.Host.Auth != want {
			t.Errorf("%s: auth %v", name, e.Host.Auth)
		}
	}
	for _, p := range src.Problems {
		gotProblems = append(gotProblems, tableProblem(p))
	}
	if !slices.Equal(got, entries) {
		t.Errorf("%s: entries\n%s\nwant\n%s", name, strings.Join(got, "\n"), strings.Join(entries, "\n"))
	}
	if !slices.Equal(gotProblems, problems) {
		t.Errorf("%s: problems %q, want %q", name, gotProblems, problems)
	}
}

func TestReadTableCSV(t *testing.T) {
	for _, c := range []struct {
		name     string
		entries  []string
		problems []string
	}{{
		// English headers, UTF-8 with a BOM: host:port, user@host, nested
		// folders with "/" and "\", quotes, an unknown column, an empty row.
		name: "servers-en.csv",
		entries: []string{
			"servers-en.csv:2|web-1|192.0.2.10:22|root|Production / Web|pw sample-password-1|Front end",
			"servers-en.csv:3|web-2|192.0.2.11:2222|deploy|Production / Web|key ~/.ssh/id_ed25519",
			`servers-en.csv:4|db-1|db.example.com:5432|postgres|Production / Databases|pw sample,password-2|Primary database, "main"`,
			"servers-en.csv:6|bastion|bastion.example.com:22|ops|",
			"servers-en.csv:9|lab|2001:db8::1:2200|pi|",
		},
		problems: []string{"servers-en.csv:7 no-host", "servers-en.csv:8 bad-port 70000"},
	}, {
		// Chinese headers, GBK, as Excel's plain "CSV" on Chinese Windows.
		name: "servers-gbk.csv",
		entries: []string{
			"servers-gbk.csv:2|网站服务器|192.0.2.20:22|root|生产 / 网站|pw 示例密码-3|前端",
			"servers-gbk.csv:3|数据库|192.0.2.21:3306|mysql|生产 / 数据库",
		},
	}, {
		// Excel's "Unicode text": UTF-16 with tabs, a quoted tab.
		name:    "servers-unicode.txt",
		entries: []string{"servers-unicode.txt:2||192.0.2.30:22|root|实验室|有\t制表符"},
	}, {
		// ";", and host:port without a port column.
		name: "servers-semicolon.csv",
		entries: []string{
			"servers-semicolon.csv:2|app-1|192.0.2.40:2201|ops|Staging",
			"servers-semicolon.csv:3|app-2|192.0.2.41:22|ops|Staging / Apps",
		},
	}} {
		src := readTable(t, c.name)
		checkTable(t, c.name, src, c.entries, c.problems)
		if src.Entries[0].Host.Port == 22 {
			t.Errorf("%s: port 22 kept", c.name)
		}
	}
}

func TestReadTableXLSX(t *testing.T) {
	for _, c := range []struct {
		name    string
		entries []string
	}{{
		// As Excel writes it: shared strings, rich text, a phonetic guide,
		// numbers, a formula's cached value, a row missing; Chinese headers.
		name: "excel.xlsx",
		entries: []string{
			"excel.xlsx:2|web-1|192.0.2.60:2222|root|生产 / 网站|pw sample-password-4|Front end",
			"excel.xlsx:4|東京|192.0.2.61:2223|root|",
		},
	}, {
		// Saved by LibreOffice Calc: a hidden first sheet, a title row
		// above the header, numbers of t="n", a formula.
		name: "libreoffice.xlsx",
		entries: []string{
			"libreoffice.xlsx:3|calc-1|192.0.2.70:22|admin|Lab / Calc|Saved by LibreOffice",
			"libreoffice.xlsx:5|calc-2|192.0.2.71:2201|admin|",
		},
	}, {
		// Inline strings, as XlsxWriter writes them in constant memory
		// mode; 22.0 written as a number.
		name: "inline.xlsx",
		entries: []string{
			"inline.xlsx:2||192.0.2.80:22|ops|Inline",
			"inline.xlsx:3||192.0.2.81:2200|ops|Inline / Sub",
		},
	}} {
		checkTable(t, c.name, readTable(t, c.name), c.entries, nil)
	}
}

func TestReadTableFails(t *testing.T) {
	_, err := ReadTable(filepath.Join("testdata", "table", "no-host.csv"))
	var nh *NoHostColumnError
	if !errors.As(err, &nh) || !slices.Equal(nh.Headers, []string{"Name", "Port", "User"}) {
		t.Errorf("no host column: %v", err)
	}
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for name, c := range map[string]struct {
		data []byte
		want error
	}{
		"empty.csv":   {nil, ErrNoConnections},
		"header.csv":  {[]byte("Host,User\n\n,\n"), ErrNoConnections},
		"old.xls":     {[]byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1rest of a compound file"), ErrWorkbook},
		"html.xls":    {[]byte("\xef\xbb\xbf <html><table><tr><td>Host</td></tr></table></html>"), ErrWorkbook},
		"other.xlsx":  {zipOf(t, map[string]string{"content.xml": "<x/>"}), ErrWorkbook},
		"broken.xlsx": {[]byte("PK\x03\x04 not a zip"), ErrWorkbook},
	} {
		if _, err := ReadTable(write(name, c.data)); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
	// Text named .xls is read as text, as some systems export it.
	if src, err := ReadTable(write("export.xls", []byte("Host\t用户\n192.0.2.1\troot\n"))); err != nil || len(src.Entries) != 1 {
		t.Errorf("text .xls: %+v, %v", src, err)
	}
	big := write("big.csv", bytes.Repeat([]byte("x"), maxTableSize+1))
	if _, err := ReadTable(big); !errors.Is(err, ErrTooLarge) {
		t.Errorf("big: %v", err)
	}
	if _, err := ReadTable(dir); err == nil {
		t.Error("read a folder")
	}
	if _, err := ReadTable(filepath.Join(dir, "none.csv")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("none: %v", err)
	}
}

// csvSource reads the CSV text.
func csvSource(t *testing.T, text string) (*Source, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "list.csv")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return ReadTable(path)
}

func TestReadTableRows(t *testing.T) {
	for _, c := range []struct {
		text     string
		entries  []string
		problems []string
	}{
		// Headers in any case, with spaces, "_" and "-"; the first of two
		// columns of a field; a title row above the header.
		{"Servers of the team\n\nHOST NAME,User_Name,user,Private-Key,Comment\n192.0.2.1,ann,bob,,hi\n",
			[]string{"list.csv:4||192.0.2.1:22|ann||hi"}, nil},
		// Excel's "sep=" line.
		{"sep=;\nhost;port\n192.0.2.1;2200\n", []string{"list.csv:3||192.0.2.1:2200||"}, nil},
		// Ports: written 22.0, out of range, not a number, port 0.
		{"host,port\n192.0.2.1,22.0\n192.0.2.2,0\n192.0.2.3,ssh\n192.0.2.4,65535\n",
			[]string{"list.csv:2||192.0.2.1:22||", "list.csv:5||192.0.2.4:65535||"},
			[]string{"list.csv:3 bad-port 0", "list.csv:4 bad-port ssh"}},
		// A bare IPv6 address is not host:port; a bad port in host:port.
		{"ip\n2001:db8::2\nexample.com:http\n", []string{"list.csv:2||2001:db8::2:22||"}, []string{"list.csv:3 bad-port http"}},
		// A row of spaces is empty; a short row has no host.
		{"name,host\n  ,  \nweb\n", nil, []string{"list.csv:3 no-host"}},
		// A key file: the password is not kept.
		{"host,password,key\n192.0.2.1,sample-pw,~/.ssh/id_rsa\n", []string{"list.csv:2||192.0.2.1:22|||key ~/.ssh/id_rsa"}, nil},
		// The delimiter is the header's, under a title row without one.
		{"Team servers\nHost;User\n192.0.2.1;root\n", []string{"list.csv:3||192.0.2.1:22|root|"}, nil},
		{"Team servers, November\n\nHost\tUser\n192.0.2.1\troot\n", []string{"list.csv:4||192.0.2.1:22|root|"}, nil},
		{"Team: a;b\nName,Host,User\nweb,192.0.2.1,root\n", []string{"list.csv:3|web|192.0.2.1:22|root|"}, nil},
		// A password keeps its spaces, quoted or not; one of spaces only
		// is none. The other cells are trimmed.
		{"host,password,user\n 192.0.2.1 ,\"  sample pw \", root \n192.0.2.2,  x  ,root\n192.0.2.3,   ,root\n",
			[]string{"list.csv:2||192.0.2.1:22|root||pw   sample pw ", "list.csv:3||192.0.2.2:22|root||pw   x  ", "list.csv:4||192.0.2.3:22|root|"}, nil},
		// host:port is split even with a Port column: its cell wins when
		// it has a port. A bare IPv6 address is not split.
		{"host,port\nexample.com:2222,2200\nexample.com:2222,\n[2001:db8::3]:2201,22\n2001:db8::4,2202\n[2001:db8::5],\n",
			[]string{"list.csv:2||example.com:2200||", "list.csv:3||example.com:2222||", "list.csv:4||2001:db8::3:22||",
				"list.csv:5||2001:db8::4:2202||", "list.csv:6||2001:db8::5:22||"}, nil},
		// A quote left open takes the rest of the file, leniently.
		{"host,notes\n192.0.2.1,\"open\n192.0.2.2,x\n", []string{"list.csv:2||192.0.2.1:22|||open\n192.0.2.2,x"}, nil},
	} {
		src, err := csvSource(t, c.text)
		if err != nil {
			t.Errorf("%q: %v", c.text, err)
			continue
		}
		checkTable(t, fmt.Sprintf("%q", c.text), src, c.entries, c.problems)
	}

	// "sep=" wins over the header's delimiter.
	_, err := csvSource(t, "sep=,\nHost;User\n192.0.2.1;root\n")
	if nh := (*NoHostColumnError)(nil); !errors.As(err, &nh) || !slices.Equal(nh.Headers, []string{"Host;User"}) {
		t.Errorf("sep=: %v", err)
	}
	// The header is looked for in the first rows only.
	_, err = csvSource(t, strings.Repeat("title\n", headerRows)+"host\n192.0.2.1\n")
	if nh := (*NoHostColumnError)(nil); !errors.As(err, &nh) || !slices.Equal(nh.Headers, []string{"title"}) {
		t.Errorf("late header: %v", err)
	}
	// Rows after the limit are not read.
	var b strings.Builder
	b.WriteString("host\n")
	for i := range maxTableRows + 5 {
		fmt.Fprintf(&b, "192.0.%d.%d\n", i/250, i%250)
	}
	src, err := csvSource(t, b.String())
	if err != nil || len(src.Entries) != maxTableRows || len(src.Problems) != 1 ||
		src.Problems[0] != (Problem{File: At("list.csv", maxTableRows+2), Reason: TooManyRows}) {
		t.Errorf("rows: %d entries, problems %v, %v", len(src.Entries), src.Problems, err)
	}
}

func TestColumnOf(t *testing.T) {
	for h, want := range map[string]Column{
		"Host": ColHost, " IP Address ": ColHost, "IP地址": ColHost, "ip 地址": ColHost, "主机": ColHost,
		"Private Key": ColKey, "identity_file": ColKey, "私钥": ColKey,
		"User Name": ColUser, "用户名": ColUser, "LOGIN": ColUser,
		"Group": ColFolder, "分组": ColFolder, "Description": ColNotes, "备注": ColNotes,
		"名称": ColName, "Label": ColName, "端口": ColPort, "密码": ColPassword,
		"Owner": "", "": "", "hostess": "",
	} {
		if got := ColumnOf(h); got != want {
			t.Errorf("%q: %q, want %q", h, got, want)
		}
	}
	for _, col := range TableColumns {
		if len(tableHeaders[col]) == 0 || ColumnOf(string(col)) != col {
			t.Errorf("column %q", col)
		}
	}
}

func TestNumberText(t *testing.T) {
	for s, want := range map[string]string{
		"22": "22", "22.0": "22", "2.2E1": "22", "1.5": "1.5", "0x16": "0x16", "Inf": "Inf", "NaN": "NaN",
		"": "", "192.0.2.1": "192.0.2.1", "-1": "-1", "1e400": "1e400",
	} {
		if got := numberText(s); got != want {
			t.Errorf("%q: %q, want %q", s, got, want)
		}
	}
}

func TestTableTemplate(t *testing.T) {
	header := func(c Column) string { return strings.ToUpper(string(c)) }
	example := func(c Column) string {
		switch c {
		case ColHost:
			return "192.0.2.1"
		case ColNotes:
			return `an "example", to delete`
		case ColFolder:
			return "Examples/Web"
		}
		return ""
	}
	data := TableTemplate(header, example)
	if !bytes.HasPrefix(data, []byte("\xef\xbb\xbfNAME,HOST,PORT,USER,PASSWORD,KEY,FOLDER,NOTES\r\n")) {
		t.Errorf("template %q", data)
	}
	src, err := csvSource(t, string(data))
	if err != nil {
		t.Fatal(err)
	}
	checkTable(t, "template", src, []string{`list.csv:2||192.0.2.1:22||Examples / Web|an "example", to delete`}, nil)
}

// zipOf is a zip of the files.
func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(files[name]))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// workbook is an .xlsx of one sheet of the rows, and shared strings.
func workbook(t *testing.T, sheetData, strs string) []byte {
	t.Helper()
	files := map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="r"><sheets><sheet name="A" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="/xl/worksheets/a.xml"/></Relationships>`,
		"xl/worksheets/a.xml":        `<worksheet><sheetData>` + sheetData + `</sheetData></worksheet>`,
	}
	if strs != "" {
		files["xl/sharedStrings.xml"] = `<sst>` + strs + `</sst>`
	}
	return zipOf(t, files)
}

func xlsxSource(t *testing.T, data []byte) (*Source, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "list.xlsx")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return ReadTable(path)
}

func TestXLSXCells(t *testing.T) {
	// Cells without references, out of order, of every type; a row
	// without a number; an absolute target; a shared string out of range.
	sheet := `<row><c t="inlineStr"><is><t>host</t></is></c><c t="inlineStr"><is><t>port</t></is></c><c t="inlineStr"><is><t>notes</t></is></c></row>` +
		`<row r="3"><c r="C3" t="b"><v>1</v></c><c r="A3" t="s"><v>0</v></c><c r="B3"><v>22.0</v></c></row>` +
		`<row><c t="str"><f>A3</f><v>192.0.2.2</v></c><c><v>2.2E3</v></c><c t="e"><v>#N/A</v></c></row>` +
		`<row r="9"><c r="A9" t="s"><v>7</v></c><c r="C9" t="d"><v>2026-11-08T00:00:00</v></c></row>` +
		`<row r="10"><c r="A10" t="inlineStr"><is><r><t>192.0.</t></r><r><t>2.3</t></r></is></c><c r="XFD10"><v>1</v></c></row>`
	src, err := xlsxSource(t, workbook(t, sheet, `<si><t>192.0.2.1</t></si>`))
	if err != nil {
		t.Fatal(err)
	}
	checkTable(t, "cells", src, []string{
		"list.xlsx:3||192.0.2.1:22|||TRUE",
		"list.xlsx:4||192.0.2.2:2200||",
		"list.xlsx:10||192.0.2.3:22||",
	}, []string{"list.xlsx:9 no-host"})
}

// A password keeps its spaces in a workbook too, shared or inline; the
// other cells are trimmed.
func TestXLSXPasswordSpaces(t *testing.T) {
	sheet := `<row><c t="inlineStr"><is><t>host</t></is></c><c t="inlineStr"><is><t>password</t></is></c></row>` +
		`<row><c t="s"><v>0</v></c><c t="s"><v>1</v></c></row>` +
		`<row><c t="inlineStr"><is><t xml:space="preserve"> 192.0.2.2</t></is></c><c t="inlineStr"><is><t xml:space="preserve">sample pw  </t></is></c></row>`
	src, err := xlsxSource(t, workbook(t, sheet, `<si><t xml:space="preserve">192.0.2.1 </t></si><si><t xml:space="preserve"> sample pw</t></si>`))
	if err != nil {
		t.Fatal(err)
	}
	checkTable(t, "spaces", src, []string{"list.xlsx:2||192.0.2.1:22|||pw  sample pw", "list.xlsx:3||192.0.2.2:22|||pw sample pw  "}, nil)
}

func TestXLSXLimits(t *testing.T) {
	// A part that unzips past the limit: a zip bomb.
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, name := range []string{"xl/workbook.xml", "xl/_rels/workbook.xml.rels"} {
		w, _ := z.Create(name)
		w.Write([]byte(map[string]string{
			"xl/workbook.xml":            `<workbook><sheets><sheet id="rId1"/></sheets></workbook>`,
			"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
		}[name]))
	}
	w, _ := z.Create("xl/worksheets/sheet1.xml")
	w.Write([]byte("<worksheet><sheetData>"))
	row := []byte(`<row><c t="inlineStr"><is><t>host</t></is></c></row>`)
	for range maxXMLPart/len(row) + 1 {
		w.Write(row)
	}
	z.Close()
	if buf.Len() > maxTableSize {
		t.Fatalf("the bomb is %d bytes", buf.Len())
	}
	if _, err := xlsxSource(t, buf.Bytes()); !errors.Is(err, ErrTooLarge) {
		t.Errorf("bomb: %v", err)
	}

	// Too many cells, in a sheet small enough.
	cells := strings.Repeat(`<c/>`, maxCells)
	if _, err := xlsxSource(t, workbook(t, `<row><c t="inlineStr"><is><t>host</t></is></c>`+cells+`</row>`, "")); !errors.Is(err, ErrTooLarge) {
		t.Errorf("cells: %v", err)
	}
	// Too many shared strings.
	if _, err := xlsxSource(t, workbook(t, `<row/>`, strings.Repeat(`<si/>`, maxStrings+1))); !errors.Is(err, ErrTooLarge) {
		t.Errorf("strings: %v", err)
	}
	// Broken XML in the sheet.
	if _, err := xlsxSource(t, workbook(t, `<row><c t="inlineStr"><is><t>host`, "")); err == nil || errors.Is(err, ErrTooLarge) {
		t.Errorf("broken: %v", err)
	}
}

func TestColumnOfRef(t *testing.T) {
	for ref, want := range map[string]int{"A1": 0, "Z9": 25, "AA1": 26, "ab3": 27, "XFD1048576": 16383} {
		if got, ok := columnOf(ref); !ok || got != want {
			t.Errorf("%s: %d %v", ref, got, ok)
		}
	}
	for _, ref := range []string{"", "12", strings.Repeat("Z", 10)} {
		if _, ok := columnOf(ref); ok {
			t.Errorf("%q read", ref)
		}
	}
}
