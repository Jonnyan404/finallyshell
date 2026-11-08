package importer

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// An .xlsx workbook is a zip of XML parts (Office Open XML, ECMA-376
// part 1, SpreadsheetML). xlsxRows reads those it needs of it:
//
//	xl/workbook.xml             <sheets><sheet name r:id/>: the sheets, in order
//	xl/_rels/workbook.xml.rels  <Relationship Id Target/>: each r:id's part
//	xl/sharedStrings.xml        <si>: the strings cells of t="s" index
//	xl/worksheets/sheet1.xml    <sheetData><row r><c r t><v>, <is>: the cells
//
// The sheet read is the first one listed that is not hidden. A cell's
// type t is "s" for a shared string (v its index), "inlineStr" for a
// string of its own (<is>), "str" for a formula's string, "b" for a
// boolean, "e" for an error (read as empty), "d" for an ISO date, and
// "n", or none, for a number. Numbers are written as they are stored,
// "22" for 22 and 22.0 alike; their number formats (dates, currency) are
// not applied. A formula's cell is its cached value, the one Excel and
// LibreOffice save with it. Rich text (<r>) is its runs' text, without
// phonetic guides (<rPh>). Elements are matched by their local name, as
// both the transitional and the strict namespaces have the same.
//
// The parts are read as a stream, each at most maxXMLPart bytes however
// much the zip says it holds, and a sheet's cells, rows and columns are
// counted, so that a zip bomb, or a huge sheet, fails rather than fills
// the memory.

// Limits on the workbooks xlsxRows reads.
const (
	maxXMLPart = 32 << 20 // bytes of a part, unzipped
	maxStrings = 1 << 20  // shared strings
	maxCells   = 1 << 20  // cells of the sheet
	maxColumn  = 1 << 10  // columns: cells right of this are not read
)

// errXLSXLimit is xlsxRows' error for a workbook over one of its limits.
var errXLSXLimit = fmt.Errorf("workbook too large: %w", ErrTooLarge)

// xlsxRows calls each with the rows of the first sheet of the workbook
// data, and the number of each, until it returns false.
func xlsxRows(data []byte, each func(line int, cells []string) bool) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("%w (%v)", ErrWorkbook, err)
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[strings.ToLower(strings.TrimPrefix(f.Name, "/"))] = f
	}
	part := func(name string) *zip.File { return files[strings.ToLower(name)] }
	if part("xl/workbook.xml") == nil {
		return ErrWorkbook
	}
	sheet, err := xlsxFirstSheet(part)
	if err != nil {
		return err
	}
	var strs []string
	if f := part("xl/sharedStrings.xml"); f != nil {
		if strs, err = xlsxStrings(f); err != nil {
			return err
		}
	}
	return xlsxSheet(sheet, strs, each)
}

// openPart is an XML decoder of f, failing past maxXMLPart bytes.
func openPart(f *zip.File) (*xml.Decoder, io.Closer, error) {
	if f.UncompressedSize64 > maxXMLPart {
		return nil, nil, errXLSXLimit
	}
	rc, err := f.Open()
	if err != nil {
		return nil, nil, err
	}
	d := xml.NewDecoder(&limitedReader{r: rc, n: maxXMLPart})
	d.Strict = false // as Excel is with what other programs write
	return d, rc, nil
}

// limitedReader is io.LimitReader failing with errXLSXLimit, not io.EOF,
// past its limit.
type limitedReader struct {
	r io.Reader
	n int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, errXLSXLimit
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= int64(n)
	return n, err
}

// xlsxFirstSheet is the part of the workbook's first sheet not hidden.
func xlsxFirstSheet(part func(string) *zip.File) (*zip.File, error) {
	var wb struct {
		Sheets []struct {
			ID    string `xml:"id,attr"` // r:id
			State string `xml:"state,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := decodePart(part("xl/workbook.xml"), &wb); err != nil {
		return nil, err
	}
	var rels struct {
		Rels []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if f := part("xl/_rels/workbook.xml.rels"); f != nil {
		if err := decodePart(f, &rels); err != nil {
			return nil, err
		}
	}
	target := func(id string) *zip.File {
		for _, r := range rels.Rels {
			if r.ID == id {
				// Relative to xl/, or to the package's root with a "/".
				t := r.Target
				if !strings.HasPrefix(t, "/") {
					t = path.Join("xl", t)
				}
				return part(strings.TrimPrefix(path.Clean(t), "/"))
			}
		}
		return nil
	}
	var first *zip.File
	for _, s := range wb.Sheets {
		f := target(s.ID)
		if f == nil {
			continue
		}
		if s.State == "" || s.State == "visible" {
			return f, nil
		}
		if first == nil {
			first = f
		}
	}
	if first == nil {
		first = part("xl/worksheets/sheet1.xml") // without relationships
	}
	if first == nil {
		return nil, fmt.Errorf("%w (no sheet)", ErrWorkbook)
	}
	return first, nil
}

// decodePart decodes the XML part f into v.
func decodePart(f *zip.File, v any) error {
	d, c, err := openPart(f)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("%s: %w", f.Name, err)
	}
	return nil
}

// xlsxStrings are the shared strings of the part f.
func xlsxStrings(f *zip.File) ([]string, error) {
	d, c, err := openPart(f)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	var strs []string
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return strs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "si" {
			if len(strs) >= maxStrings {
				return nil, errXLSXLimit
			}
			s, err := xlsxText(d, se)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", f.Name, err)
			}
			strs = append(strs, s)
		}
	}
}

// xlsxText is the text of the element start, a string item (<si>, <is>):
// that of its <t>, or of its runs', without phonetic guides.
func xlsxText(d *xml.Decoder, start xml.StartElement) (string, error) {
	var b strings.Builder
	depth, inT, phonetic := 1, 0, 0
	for depth > 0 {
		tok, err := d.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch t.Name.Local {
			case "t":
				inT++
			case "rPh":
				phonetic++
			}
		case xml.EndElement:
			depth--
			switch t.Name.Local {
			case "t":
				inT--
			case "rPh":
				phonetic--
			}
		case xml.CharData:
			if inT > 0 && phonetic == 0 {
				b.Write(t)
			}
		}
	}
	return b.String(), nil
}

// xlsxSheet calls each with the rows of the sheet's part f, strs its
// shared strings.
func xlsxSheet(f *zip.File, strs []string, each func(line int, cells []string) bool) error {
	d, c, err := openPart(f)
	if err != nil {
		return err
	}
	defer c.Close()
	fail := func(err error) error {
		if errors.Is(err, ErrTooLarge) {
			return err
		}
		return fmt.Errorf("%s: %w", f.Name, err)
	}
	var (
		line, cells int
		row         []string
		inRow       bool
	)
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fail(err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				n, err := strconv.Atoi(attr(t, "r"))
				if err != nil || n <= line {
					n = line + 1
				}
				line, row, inRow = n, row[:0], true
			case "c":
				if !inRow {
					continue
				}
				if cells++; cells > maxCells {
					return errXLSXLimit
				}
				col := len(row)
				if ref := attr(t, "r"); ref != "" {
					if n, ok := columnOf(ref); ok {
						col = n
					}
				}
				v, err := xlsxCell(d, t, strs)
				if err != nil {
					return fail(err)
				}
				if col >= maxColumn || v == "" {
					continue
				}
				for len(row) <= col {
					row = append(row, "")
				}
				row[col] = v
			}
		case xml.EndElement:
			if t.Name.Local == "row" && inRow {
				inRow = false
				if !each(line, row) {
					return nil
				}
			}
		}
	}
}

// xlsxCell is the value of the cell element start, as text.
func xlsxCell(d *xml.Decoder, start xml.StartElement, strs []string) (string, error) {
	typ := attr(start, "t")
	var v, inline string
	for {
		tok, err := d.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "v":
				var s string
				if err := d.DecodeElement(&s, &t); err != nil {
					return "", err
				}
				v = s
			case "is":
				if inline, err = xlsxText(d, t); err != nil {
					return "", err
				}
			default: // <f>, <extLst>
				if err := d.Skip(); err != nil {
					return "", err
				}
			}
		case xml.EndElement: // the cell's
			switch typ {
			case "s":
				i, err := strconv.Atoi(strings.TrimSpace(v))
				if err != nil || i < 0 || i >= len(strs) {
					return "", nil
				}
				return strs[i], nil
			case "inlineStr":
				if inline == "" {
					return v, nil
				}
				return inline, nil
			case "str", "d":
				return v, nil
			case "b":
				if strings.TrimSpace(v) == "1" {
					return "TRUE", nil
				}
				if strings.TrimSpace(v) == "0" {
					return "FALSE", nil
				}
				return v, nil
			case "e":
				return "", nil
			}
			return numberText(strings.TrimSpace(v)), nil
		}
	}
}

// attr is the value of the attribute of el with the local name name.
func attr(el xml.StartElement, name string) string {
	for _, a := range el.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// columnOf is the index of the column of the cell reference ref, as 0
// for "A1" and 27 for "AB3".
func columnOf(ref string) (int, bool) {
	n, i := 0, 0
	for ; i < len(ref); i++ {
		c := ref[i] | 0x20 // lower case
		if c < 'a' || c > 'z' {
			break
		}
		n = n*26 + int(c-'a'+1)
		if n > 1<<20 {
			return 0, false
		}
	}
	return n - 1, i > 0
}
