package app

import (
	"runtime"

	"finallyshell/internal/importer"
	"finallyshell/locales"
)

// The import window's texts of each source (import.go). The sources, and
// their order, are importer's (importer.Readers); a source is listed once
// it has its texts here. Adding a source is in the importer package's
// comment and in docs/en/development.md.

// importText are a source's texts in the import window.
type importText struct {
	// name is the source's, in the "Import from" list and in texts that
	// name it, as "FinalShell".
	name func() string
	// choose is the title of the chooser of its file or folder.
	choose func() string
	// fileTypes names the files of its Extensions, in the file chooser:
	// nil for a folder, or any file.
	fileTypes func() string
	// notFound is said when Find found nothing, or the source has no
	// Find, and the chooser was canceled: what to choose.
	notFound func() string
	// note is said above the list: what is not imported, as passwords.
	// nil for nothing.
	note func() string
}

// importTexts are the texts of each source, by kind.
var importTexts = map[importer.Kind]importText{
	importer.FinalShell: {
		name:     locales.ImportSourceFinalShell,
		choose:   locales.ChooseFinalShellFolder,
		notFound: locales.FinalShellNotFound,
		note:     func() string { return locales.ImportPasswordsNote(locales.ImportSourceFinalShell()) },
	},
	importer.MobaXterm: {
		name:      locales.ImportSourceMobaXterm,
		choose:    locales.ChooseMobaXtermFile,
		fileTypes: locales.MobaXtermFileTypes,
		notFound:  locales.MobaXtermNotFound,
		note:      func() string { return locales.ImportPasswordsNote(locales.ImportSourceMobaXterm()) },
	},
	importer.SSHConfig: {
		name:     locales.ImportSourceSSHConfig,
		choose:   locales.ChooseSSHConfigFile,
		notFound: func() string { return locales.ImportSSHConfigNotFound(sshConfigPath(runtime.GOOS)) },
		note:     locales.ImportSSHConfigNote,
	},
}

// sshConfigPath is where ssh reads its user's config on goos, as its
// users write it.
func sshConfigPath(goos string) string {
	if goos == "windows" {
		return `%USERPROFILE%\.ssh\config`
	}
	return "~/.ssh/config"
}

// importSource is a source the window lists: what reads it, and its texts.
type importSource struct {
	*importer.Reader
	importText
}

// allImportSources are the sources with texts, in importer's order.
func allImportSources() []importSource {
	var out []importSource
	for _, r := range importer.Readers() {
		if t, ok := importTexts[r.Kind]; ok {
			out = append(out, importSource{r, t})
		}
	}
	return out
}

// importProblemText says why a file, or a line of it, of the source
// named source was not imported from.
func importProblemText(p importer.Problem, source string) string {
	switch p.Reason {
	case importer.Broken:
		return locales.ProblemBroken()
	case importer.Unreadable:
		return locales.ProblemUnreadable()
	case importer.TooLarge:
		return locales.ProblemTooLarge()
	case importer.NoHost:
		return locales.ProblemNoHost()
	case importer.NotSSH:
		return locales.ProblemNotSSH()
	case importer.DeletedThere:
		return locales.ProblemDeleted(source)
	case importer.SSHMatch:
		return locales.ProblemSSHMatch()
	case importer.SSHPatternsOnly:
		return locales.ProblemSSHPatterns()
	case importer.SSHBadPort:
		return locales.ProblemSSHPort()
	case importer.SSHBadLine:
		return locales.ProblemSSHLine()
	case importer.SSHIncludeLimit:
		return locales.ProblemSSHIncludeLimit()
	}
	return string(p.Reason)
}

// importNoteText says what an entry's note is.
func importNoteText(n importer.Note) string {
	switch n {
	case importer.ProxyNotImported:
		return locales.ImportNoProxy()
	case importer.JumpHostNotImported:
		return locales.ImportNoJumpHost()
	case importer.SSHKeyTokens:
		return locales.ImportSSHKeyTokens()
	}
	return string(n)
}
