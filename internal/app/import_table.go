package app

import (
	"errors"
	"strings"

	"finallyshell/internal/importer"
	"finallyshell/locales"
)

// The import window's parts of a server list (importer.Table), a CSV or
// Excel file the user keeps: the template "Save template…" writes, in
// the app's language, and the texts of the reader's errors.

const (
	// tableTemplateName is the template's file name.
	tableTemplateName = "servers.csv"
	// maxHeadersShown are the headers of a list without a host column
	// that the window names.
	maxHeadersShown = 12
)

// tableHeader is the template's header of the column col, in the app's
// language: one importer.ColumnOf reads as col.
func tableHeader(col importer.Column) string {
	switch col {
	case importer.ColName:
		return locales.TableColName()
	case importer.ColHost:
		return locales.TableColHost()
	case importer.ColPort:
		return locales.TableColPort()
	case importer.ColUser:
		return locales.TableColUser()
	case importer.ColPassword:
		return locales.TableColPassword()
	case importer.ColKey:
		return locales.TableColKey()
	case importer.ColFolder:
		return locales.TableColFolder()
	case importer.ColNotes:
		return locales.TableColNotes()
	}
	return string(col)
}

// tableExample is the template's example row's value of the column col:
// a documentation address, and no password.
func tableExample(col importer.Column) string {
	switch col {
	case importer.ColName:
		return locales.TableExampleName()
	case importer.ColHost:
		return "192.0.2.10"
	case importer.ColPort:
		return "22"
	case importer.ColUser:
		return "root"
	case importer.ColFolder:
		return locales.TableExampleFolder()
	case importer.ColNotes:
		return locales.TableExampleNotes()
	}
	return ""
}

func tableTemplate() importTemplate {
	return importTemplate{name: tableTemplateName, typeName: locales.CsvFiles(), data: importer.TableTemplate(tableHeader, tableExample)}
}

// tableErrorText says why a server list could not be read, for the
// reader's own errors: "" for others.
func tableErrorText(err error) string {
	var nh *importer.NoHostColumnError
	switch {
	case errors.As(err, &nh):
		var quoted []string
		for i, h := range nh.Headers {
			if i == maxHeadersShown {
				quoted = append(quoted, "…")
				break
			}
			quoted = append(quoted, "“"+h+"”")
		}
		return locales.TableNoHostColumn(strings.Join(quoted, locales.ListSeparator()))
	case errors.Is(err, importer.ErrWorkbook):
		return locales.TableWorkbook()
	case errors.Is(err, importer.ErrTooLarge):
		return locales.TableTooLarge()
	}
	return ""
}
