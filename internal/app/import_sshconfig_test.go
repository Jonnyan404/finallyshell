package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/internal/importer"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// findSSHConfig makes find what looks for ~/.ssh/config.
func findSSHConfig(a *App, find func() string) {
	for i, s := range a.importSources {
		if s.Kind == importer.SSHConfig {
			r := *s.Reader
			r.Find = find
			a.importSources[i].Reader = &r
		}
	}
}

// Importing ~/.ssh/config: its hosts, at the top level, with their key
// files, and the jump host of those with a ProxyJump to another of them.
func TestImportSSHConfig(t *testing.T) {
	a, p := testApp(t)
	findFinalShell(a, func() string { return "" })
	// The sample as ~/.ssh/config, which the window writes so.
	home := t.TempDir()
	if err := os.CopyFS(filepath.Join(home, ".ssh"), os.DirFS(filepath.Join("..", "importer", "testdata", "sshconfig"))); err != nil {
		t.Fatal(err)
	}
	a.home = func() string { return home }
	sample := filepath.Join(home, ".ssh", "config")
	findSSHConfig(a, func() string { return sample })
	p.pickFolder("") // FinalShell's chooser canceled
	_, tt := openImport(t, a, p)
	choose(t, tt, locales.ImportFrom(), locales.ImportSourceSSHConfig())
	p.until(t, "the config read", func() bool { return !a.imp.busy && a.imp.src != nil })
	tt.Frame()
	if len(p.typedPicks()) != 0 {
		t.Errorf("asked for the file that was found: %+v", p.typedPicks())
	}
	for _, s := range []string{"app", "bastion", "work-1", tildePath(sample, home, runtime.GOOS), "bastion.example.com:2222", "work-1.corp.example.com:2022", "postgres", "alice",
		locales.ImportSSHConfigNote(), locales.ImportNoJumpHost(), locales.ImportSSHKeyTokens(), locales.ImportNoProxy(),
		locales.ImportJumpHost("bastion"), locales.ImportChosen(11, 11), locales.ImportCount(11), locales.ImportProblems(1, 3),
		locales.ProblemLine("config:35", locales.ProblemSSHPort()), locales.ProblemLine("config:37", locales.ProblemSSHMatch()),
		locales.ProblemLine("config.d/work.conf:5", locales.ProblemSSHPatterns())} {
		if !tt.HasText(s) {
			t.Errorf("missing %q; texts %q", s, tt.Texts())
		}
	}
	if tt.HasText(locales.ImportNeedsKey()) || tt.HasText("not-included") {
		t.Errorf("texts %q", tt.Texts())
	}
	saveShot(t, tt, "import-ssh-config-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, s := range []string{"SSH 配置文件", "跳板机没有导入", "跳板机：bastion", "导入 11 个", "不支持 Match，已跳过"} {
		if !tt.HasText(s) {
			t.Errorf("missing %q; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "import-ssh-config-zh.png")
	tt.SetDark(true)
	tt.Frame()
	saveShot(t, tt, "import-ssh-config-dark-zh.png")
	tt.SetDark(false)
	p.SetLocale("")
	tt.Frame()

	if err := tt.Click(locales.ImportCount(11)); err != nil {
		t.Fatal(err)
	}
	byName := map[string]*config.Host{}
	for _, h := range a.hosts.Live() {
		if h.Folder != "" || h.Notes != "" {
			t.Errorf("%s in folder %q, notes %q", h.Name, h.Folder, h.Notes)
		}
		byName[h.Name] = h
	}
	if len(byName) != 11 {
		t.Fatalf("hosts %v", byName)
	}
	bastion, app, chain := byName["bastion"], byName["app"], byName["chain"]
	if bastion.Auth != config.AuthKey || bastion.KeyPath != "~/.ssh/id_bastion" || bastion.Port != 2222 || bastion.Proxy != nil {
		t.Errorf("bastion %+v", bastion)
	}
	if app.Auth != config.AuthKey || app.KeyPath != "~/.ssh/keys/my key" || app.User != "deploy" {
		t.Errorf("app %+v", app)
	}
	for _, h := range []*config.Host{app, chain} {
		if jumps, err := a.hosts.JumpHosts(h); err != nil || len(jumps) != 1 || jumps[0] != bastion {
			t.Errorf("%s's jump hosts %v %v", h.Name, jumps, err)
		}
	}
	if h := byName["legacy"]; h.Proxy != nil || h.Auth != config.AuthPassword {
		t.Errorf("legacy %+v", h)
	}
	if h := byName["work-2"]; h.KeyPath != "~/.ssh/alice-work-2.corp.example.com" || h.Port != 2022 || h.User != "alice" {
		t.Errorf("work-2 %+v", h)
	}
	manager := p.show(t, WindowManager)
	manager.Frame()
	if want := locales.ImportResult(11, 3, 1); !manager.HasText(want) || manager.HasText(locales.ImportKeysNote(1)) {
		t.Errorf("no %q; texts %q", want, manager.Texts())
	}
}

// Without ~/.ssh/config the window asks for a file, of any type, and
// says where ssh has it when the choice is canceled.
func TestImportSSHConfigChooseFile(t *testing.T) {
	a, p := testApp(t)
	findFinalShell(a, func() string { return "" })
	findSSHConfig(a, func() string { return "" })
	a.importKind = importer.SSHConfig
	_, tt := openImport(t, a, p)
	picks := p.typedPicks()
	if len(picks) != 1 || picks[0].title != locales.ChooseSSHConfigFile() || picks[0].typeName != "" || picks[0].extensions != nil {
		t.Fatalf("picks %+v", picks)
	}
	if !tt.HasText(locales.ImportSSHConfigNotFound(sshConfigPath(runtime.GOOS))) {
		t.Errorf("texts %q", tt.Texts())
	}
	saveShot(t, tt, "import-ssh-config-not-found-en.png")
	p.pickTyped(filepath.Join("..", "importer", "testdata", "sshconfig", "config.d", "work.conf"))
	if err := tt.Click(locales.ChooseFile()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the file read", func() bool { return !a.imp.busy && a.imp.src != nil })
	tt.Frame()
	if !tt.HasText("work-1") || !tt.HasText(locales.ImportCount(2)) {
		t.Errorf("texts %q", tt.Texts())
	}
	if sshConfigPath("windows") != `%USERPROFILE%\.ssh\config` || sshConfigPath("darwin") != "~/.ssh/config" {
		t.Error(sshConfigPath("windows"))
	}
}

// openSSHSample opens the import window of a new app on the importer's
// sample SSH config, with the hosts present.
func openSSHSample(t *testing.T, present ...*config.Host) (*App, *fakePlatform, *ui.Tester) {
	t.Helper()
	a, p := testApp(t)
	sample, err := filepath.Abs(filepath.Join("..", "importer", "testdata", "sshconfig", "config"))
	if err != nil {
		t.Fatal(err)
	}
	findSSHConfig(a, func() string { return sample })
	a.importKind = importer.SSHConfig
	for _, h := range present {
		a.hosts.Add(h)
	}
	_, tt := openImport(t, a, p)
	return a, p, tt
}

// countText is how many texts of tt are s.
func countText(tt *ui.Tester, s string) int {
	n := 0
	for _, x := range tt.Texts() {
		if x == s {
			n++
		}
	}
	return n
}

// The note of a host with a jump host follows the rows chosen: the jump
// host's name while its row is checked, or it is present, "not imported"
// otherwise, and the host then imports without it.
func TestImportSSHConfigJumpUnchecked(t *testing.T) {
	a, _, tt := openSSHSample(t)
	// app and chain go through bastion; legacy's jump host is never
	// imported.
	via, none := locales.ImportJumpHost("bastion"), locales.ImportNoJumpHost()
	toggle := func() {
		t.Helper()
		box := importBox(t, tt, "bastion")
		tt.ClickAt(box.Bounds.X+box.Bounds.W/2, box.Bounds.Y+box.Bounds.H/2)
		tt.Frame()
	}
	if countText(tt, via) != 2 || countText(tt, none) != 1 {
		t.Fatalf("checked: texts %q", tt.Texts())
	}
	toggle()
	if importBox(t, tt, "bastion").Checked || countText(tt, via) != 0 || countText(tt, none) != 3 {
		t.Fatalf("unchecked: texts %q", tt.Texts())
	}
	toggle()
	if !importBox(t, tt, "bastion").Checked || countText(tt, via) != 2 || countText(tt, none) != 1 {
		t.Fatalf("checked again: texts %q", tt.Texts())
	}
	toggle()
	if err := tt.Click(locales.ImportCount(10)); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	if len(a.hosts.Live()) != 10 {
		t.Errorf("%d hosts", len(a.hosts.Live()))
	}
	for _, h := range a.hosts.Live() {
		if h.Name == "bastion" || h.Proxy != nil {
			t.Errorf("%s imported, proxy %+v", h.Name, h.Proxy)
		}
	}

	// bastion present already, the same address, port and user: its row is
	// skipped, the note names the jump host, and the hosts go through it.
	user := ""
	src, err := importer.ReaderOf(importer.SSHConfig).Read(filepath.Join("..", "importer", "testdata", "sshconfig", "config"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range src.Entries {
		if e.Host.Name == "bastion" {
			user = e.Host.User // the local user's name
		}
	}
	mine := &config.Host{ID: "mine", Name: "my bastion", Addr: "bastion.example.com", Port: 2222, User: user, Auth: config.AuthPassword}
	a, _, tt = openSSHSample(t, mine)
	if countText(tt, via) != 2 || !tt.HasText(locales.ImportPresent("my bastion")) {
		t.Fatalf("present: texts %q", tt.Texts())
	}
	if err := tt.Click(locales.ImportCount(10)); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	for _, h := range a.hosts.Live() {
		if (h.Name == "app" || h.Name == "chain") && (h.Proxy == nil || h.Proxy.JumpHost != "mine") {
			t.Errorf("%s proxy %+v", h.Name, h.Proxy)
		}
	}
}
