package app

import (
	"path/filepath"
	"slices"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/internal/importer"
	"finallyshell/locales"
)

// MobaXterm, without its file here: the chooser asks for MobaXterm.ini
// or an export, and the window lists its sessions.
func TestImportMobaXterm(t *testing.T) {
	a, p := testApp(t)
	findFinalShell(a, func() string { return finalShellSample(t, "finalshell") })
	for i, s := range a.importSources {
		if s.Kind == importer.MobaXterm {
			r := *s.Reader
			r.Find = func() string { return "" }
			a.importSources[i].Reader = &r
		}
	}
	// web-1 is here already.
	a.hosts.Add(&config.Host{ID: "mine", Name: "web", Addr: "192.0.2.10", User: "root", Auth: config.AuthPassword})
	ini, err := filepath.Abs(filepath.Join("..", "importer", "testdata", "mobaxterm", "MobaXterm.ini"))
	if err != nil {
		t.Fatal(err)
	}
	_, tt := openImport(t, a, p)
	// The chooser canceled: the window says what to choose.
	choose(t, tt, locales.ImportFrom(), locales.ImportSourceMobaXterm())
	p.until(t, "the chooser", func() bool { return !a.imp.busy })
	tt.Frame()
	picks := p.typedPicks()
	if len(picks) != 1 || picks[0].title != locales.ChooseMobaXtermFile() || picks[0].typeName != locales.MobaXtermFileTypes() ||
		!slices.Equal(picks[0].extensions, []string{"ini", "mxtsessions", "moba"}) {
		t.Fatalf("picks %+v", picks)
	}
	if !tt.HasText(locales.MobaXtermNotFound()) {
		t.Errorf("texts %q", tt.Texts())
	}
	saveShot(t, tt, "import-mobaxterm-not-found-en.png")

	p.pickTyped(ini)
	if err := tt.Click(locales.ChooseFile()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the file read", func() bool { return !a.imp.busy && a.imp.src != nil })
	tt.Frame()
	for _, s := range []string{"web-2", "old-box", "bastion", "app-1", "app-2", "pg-main", "bastion.example.com:2222", "db.example.com:5022",
		"Production / Databases", locales.ImportPresent("web"), locales.ImportNoJumpHost(), locales.ImportNoProxy(), locales.ImportNeedsKey(),
		locales.ImportPasswordsNote("MobaXterm"), locales.ChooseFile(), locales.ImportCount(6), locales.ImportProblems(2, 2),
		locales.ProblemLine("MobaXterm.ini:20", locales.ProblemNotSSH()), locales.ProblemLine("MobaXterm.ini:35", locales.ProblemNoHost())} {
		if !tt.HasText(s) {
			t.Errorf("missing %q; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "import-mobaxterm-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, s := range []string{"跳板机没有导入", "密码不导入（MobaXterm 对密码做了加密），第一次连接时会询问密码。", "选择文件…"} {
		if !tt.HasText(s) {
			t.Errorf("missing %q; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "import-mobaxterm-zh.png")
	tt.SetDark(true)
	tt.Frame()
	saveShot(t, tt, "import-mobaxterm-dark-zh.png")
	tt.SetDark(false)
	p.SetLocale("")
	tt.Frame()

	if err := tt.Click(locales.ImportCount(6)); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	var got []string
	for _, h := range a.hosts.Live()[1:] {
		got = append(got, a.folderLabel(h.Folder)+"|"+h.Title())
	}
	if want := []string{"|old-box", "|web-2", "Production|app-1", "Production|app-2", "Production|bastion", "Production / Databases|pg-main"}; !slices.Equal(got, want) {
		t.Errorf("imported %q, want %q", got, want)
	}
}
