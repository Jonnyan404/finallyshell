package app

import (
	"path/filepath"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/internal/importer"
	"finallyshell/locales"
)

// findSecureCRT makes find what looks for SecureCRT's config folder.
func findSecureCRT(a *App, find func() string) {
	for i, s := range a.importSources {
		if s.Kind == importer.SecureCRT {
			r := *s.Reader
			r.Find = find
			a.importSources[i].Reader = &r
		}
	}
}

// SecureCRT is in the list of sources; chosen, its config folder is found
// and its sessions listed, then imported with their key files.
func TestImportSecureCRT(t *testing.T) {
	a, p := testApp(t)
	managerHosts(a)
	sample := finalShellSample(t, filepath.Join("securecrt", "Config"))
	findFinalShell(a, func() string { return "" })
	findSecureCRT(a, func() string { return sample })
	_, tt := openImport(t, a, p)
	choose(t, tt, locales.ImportFrom(), locales.ImportSourceSecureCRT())
	p.until(t, "SecureCRT read", func() bool { return !a.imp.busy && a.imp.src != nil })
	tt.Frame()
	if a.imp.src.Kind != importer.SecureCRT || a.importKind != importer.SecureCRT {
		t.Fatalf("read %s", a.imp.src.Kind)
	}
	for _, s := range []string{"web-1", "bastion", "db-1", "legacy", "树莓派", "bastion.example.com:2222", "db.example.com:5522",
		"203.0.113.5:23", "Production / Databases", filepath.Join(sample, "Sessions"),
		locales.ImportPasswordsNote("SecureCRT"), locales.ImportNoJumpHost(), locales.ImportNoProxy(), locales.ImportNeedsKey(),
		locales.ImportChosen(7, 7), locales.ImportCount(7), locales.ImportProblems(2, 2),
		locales.ProblemLine("broken.ini", locales.ProblemBroken()), locales.ProblemLine("Lab/router.ini", locales.ProblemNotSSH())} {
		if !tt.HasText(s) {
			t.Errorf("missing %q; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "import-securecrt-en.png")
	sourcesShot(t, tt, "import-securecrt-sources-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, s := range []string{"跳板机没有导入", "代理没有导入", "密码不导入（SecureCRT 对密码做了加密），第一次连接时会询问密码。"} {
		if !tt.HasText(s) {
			t.Errorf("missing %q; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "import-securecrt-zh.png")
	tt.SetDark(true)
	tt.Frame()
	saveShot(t, tt, "import-securecrt-dark-zh.png")
	tt.SetDark(false)
	p.SetLocale("")
	tt.Frame()

	if err := tt.Click(locales.ImportCount(7)); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	byName := map[string]*config.Host{}
	for _, h := range a.hosts.Live() {
		byName[h.Name] = h
	}
	if h := byName["bastion"]; h == nil || h.Auth != config.AuthKey || h.KeyPath != filepath.Join(sample, "Keys", "bastion_ed25519") || h.Port != 2222 {
		t.Errorf("bastion %+v", h)
	}
	if h := byName["legacy"]; h == nil || h.Auth != config.AuthKey || h.KeyPath != "" {
		t.Errorf("legacy %+v", h)
	}
	manager := p.show(t, WindowManager)
	manager.Frame()
	// legacy's key is SecureCRT's own: it is chosen after importing.
	if want := locales.ImportResult(7, 2, 2) + " " + locales.ImportKeysNote(1); !manager.HasText(want) {
		t.Errorf("no %q; texts %q", want, manager.Texts())
	}
}

// The chooser of SecureCRT's folder says what to choose when canceled.
func TestImportSecureCRTNotFound(t *testing.T) {
	a, p := testApp(t)
	findFinalShell(a, func() string { return "" })
	findSecureCRT(a, func() string { return "" })
	_, tt := openImport(t, a, p)
	choose(t, tt, locales.ImportFrom(), locales.ImportSourceSecureCRT())
	p.until(t, "the chooser", func() bool { return !a.imp.busy })
	tt.Frame()
	picks := p.folderPicks()
	if len(picks) == 0 || picks[len(picks)-1].title != locales.ChooseSecureCRTFolder() {
		t.Fatalf("picks %+v", picks)
	}
	if !tt.HasText(locales.SecureCRTNotFound()) {
		t.Errorf("texts %q", tt.Texts())
	}
	saveShot(t, tt, "import-securecrt-notfound-en.png")
	a.closeWindow(WindowImport)
}
