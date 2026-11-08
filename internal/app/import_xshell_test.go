package app

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/internal/importer"
	"finallyshell/locales"
)

// findXshell makes find what looks for Xshell's Sessions folder.
func findXshell(a *App, find func() string) {
	for i, s := range a.importSources {
		if s.Kind == importer.Xshell {
			r := *s.Reader
			r.Find = find
			a.importSources[i].Reader = &r
		}
	}
}

// xshellSample is a file or folder of the importer's samples of Xshell.
func xshellSample(t *testing.T, parts ...string) string {
	t.Helper()
	return finalShellSample(t, filepath.Join(append([]string{"xshell"}, parts...)...))
}

// Xshell's sessions, in their folders, chosen in the list of sources.
func TestImportXshell(t *testing.T) {
	a, p := testApp(t)
	saved := savedHosts(a)
	// Xshell 7's Sessions folder in a home folder, which the window
	// writes "~" but on Windows.
	home := t.TempDir()
	sessions := filepath.Join(home, "Documents", "NetSarang Computer", "7", "Xshell", "Sessions")
	if err := os.CopyFS(sessions, os.DirFS(xshellSample(t, "Xshell", "Sessions"))); err != nil {
		t.Fatal(err)
	}
	a.home = func() string { return home }
	findFinalShell(a, func() string { return finalShellSample(t, "finalshell") })
	findXshell(a, func() string { return sessions })
	_, tt := openImport(t, a, p)
	choose(t, tt, locales.ImportFrom(), locales.ImportSourceXshell())
	p.until(t, "Xshell read", func() bool { return !a.imp.busy && a.imp.src != nil && a.imp.src.Kind == importer.Xshell })
	tt.Frame()
	if len(p.folderPicks()) != 0 || a.importKind != importer.Xshell {
		t.Errorf("picks %v, kind %s", p.folderPicks(), a.importKind)
	}
	for _, s := range []string{"web-1", "bastion", "pg-main", "树莓派", "legacy", "192.0.2.10", "bastion.example.com:2222", "db.example.com:5022",
		"Production / Databases", "实验室", tildePath(sessions, home, runtime.GOOS), locales.ImportPasswordsNote("Xshell"),
		locales.ImportChosen(5, 5), locales.ImportCount(5), locales.ImportProblems(2, 2),
		locales.ProblemLine("deploy.xsh", locales.ProblemBroken()), locales.ProblemLine("no-host.xsh", locales.ProblemNoHost()),
		locales.ProblemLine("router.xsh", locales.ProblemNotSSH())} {
		if !tt.HasText(s) {
			t.Errorf("missing %q; texts %q", s, tt.Texts())
		}
	}
	if !tt.HasText(locales.ImportNeedsKey()+locales.ListSeparator()+locales.ImportNoProxy()) || tt.HasText("c2FtcGxl") || tt.HasText("web-2") {
		t.Errorf("texts %q", tt.Texts())
	}
	saveShot(t, tt, "import-xshell-en.png")
	sourcesShot(t, tt, "import-xshell-sources-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, s := range []string{"导入来源", "导入 5 个", "导入后需要选择私钥文件", "代理没有导入", "密码不导入（Xshell 对密码做了加密），第一次连接时会询问密码。"} {
		if !tt.HasText(s) {
			t.Errorf("missing %q; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "import-xshell-zh.png")
	tt.SetDark(true)
	tt.Frame()
	saveShot(t, tt, "import-xshell-dark-zh.png")
	p.SetLocale("")
	tt.Frame()
	saveShot(t, tt, "import-xshell-dark-en.png")
	tt.SetDark(false)
	tt.Frame()

	if err := tt.Click(locales.ImportCount(5)); err != nil {
		t.Fatal(err)
	}
	var names []string
	keys := 0
	for _, h := range a.hosts.Live() {
		names = append(names, a.folderLabel(h.Folder)+"|"+h.Title())
		if h.Auth == config.AuthKey && h.KeyPath == "" {
			keys++
		}
	}
	slices.Sort(names)
	if want := []string{"Production / Databases|pg-main", "Production|bastion", "|legacy", "|web-1", "实验室|树莓派"}; !slices.Equal(names, want) || keys != 2 {
		t.Errorf("added %q, want %q; %d keys to choose", names, want, keys)
	}
	if strings.Contains(string(*saved), "c2FtcGxl") || !strings.Contains(string(*saved), "bastion.example.com") {
		t.Errorf("saved %s", *saved)
	}
	manager := p.show(t, WindowManager)
	manager.Frame()
	if want := locales.ImportResult(5, 2, 2) + " " + locales.ImportKeysNote(2); !manager.HasText(want) {
		t.Errorf("no %q; texts %q", want, manager.Texts())
	}

	// The window opens on Xshell the next time.
	_, tt = openImport(t, a, p)
	if a.imp.source.Kind != importer.Xshell || !tt.HasText(locales.ImportPresent("bastion")) {
		t.Errorf("opened on %s; texts %q", a.imp.source.Kind, tt.Texts())
	}
	a.closeWindow(WindowImport)
}

// Without Xshell's folder the window asks for one; a folder with an
// export in it reads the export.
func TestImportXshellExport(t *testing.T) {
	a, p := testApp(t)
	a.importKind = importer.Xshell
	findXshell(a, func() string { return "" })
	_, tt := openImport(t, a, p)
	picks := p.folderPicks()
	if len(picks) != 1 || picks[0].parent != WindowImport || picks[0].title != locales.ChooseXshellFolder() {
		t.Fatalf("picks %+v", picks)
	}
	if !tt.HasText(locales.XshellNotFound()) {
		t.Fatalf("texts %q", tt.Texts())
	}
	saveShot(t, tt, "import-xshell-not-found-en.png")

	dir := t.TempDir()
	data, err := os.ReadFile(xshellSample(t, "sessions.xts"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sessions.xts"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	p.pickFolder(dir)
	if err := tt.Click(locales.ChooseFolder()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the folder read", func() bool { return !a.imp.busy && a.imp.src != nil })
	tt.Frame()
	for _, s := range []string{"stage-web", "Staging / Web", "测试机", "测试", locales.ImportCount(3), locales.ImportProblems(0, 1),
		locales.ProblemLine("sessions.xts/Staging/rlogin.xsh", locales.ProblemNotSSH())} {
		if !tt.HasText(s) {
			t.Errorf("missing %q; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "import-xshell-export-en.png")
	a.closeWindow(WindowImport)
}
