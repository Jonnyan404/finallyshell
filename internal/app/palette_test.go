package app

import (
	"image/color"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// fakeAppearance keeps what the app made the windows' appearance.
type fakeAppearance struct {
	appearances []config.Appearance // SetAppearance's calls
}

func (p *fakeAppearance) SetAppearance(a config.Appearance) {
	p.appearances = append(p.appearances, a)
}

// pixel is the color of tt's frame at x, y, in DIPs at scale.
func pixel(tt *ui.Tester, x, y, scale int) color.NRGBA {
	return color.NRGBAModel.Convert(tt.Image().At(x*scale, y*scale)).(color.NRGBA)
}

func sameColor(got color.NRGBA, want ui.Color) bool {
	return got.R == want.R && got.G == want.G && got.B == want.B
}

// The Appearance row of the General page sets the app's appearance at
// once, and keeps it in the settings.
func TestAppearanceOption(t *testing.T) {
	a, p := testApp(t)
	a.openOptions()
	tt := p.show(t, WindowOptions)
	for _, s := range []string{locales.OptionsAppearance(), locales.AppearanceSystem(), locales.AppearanceLight(), locales.AppearanceDark(), locales.AppearanceNote()} {
		if !tt.HasText(s) {
			t.Errorf("the General page has no %q", s)
		}
	}
	for _, tc := range []struct {
		label string
		want  config.Appearance
	}{
		{locales.AppearanceDark(), config.AppearanceDark},
		{locales.AppearanceLight(), config.AppearanceLight},
		{locales.AppearanceSystem(), config.AppearanceSystem},
		{locales.AppearanceDark(), config.AppearanceDark},
	} {
		if err := tt.Click(tc.label); err != nil {
			t.Fatal(err)
		}
		n := len(p.appearances)
		if n == 0 || p.appearances[n-1] != tc.want {
			t.Fatalf("%s: SetAppearance calls %q", tc.label, p.appearances)
		}
		saved := tc.want
		if saved == config.AppearanceSystem {
			saved = "" // the default is left out
		}
		if a.settings.Appearance != saved || config.LoadSettings().Appearance != saved {
			t.Fatalf("%s: settings %q, saved %q", tc.label, a.settings.Appearance, config.LoadSettings().Appearance)
		}
	}
	calls := len(p.appearances)
	tt.Frame()
	if len(p.appearances) != calls {
		t.Errorf("a frame set the appearance again: %q", p.appearances)
	}
	// A new run reads it back.
	if b := New(p); b.settings.Appearance != config.AppearanceDark {
		t.Errorf("read back %q", b.settings.Appearance)
	}

	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, s := range []string{"外观", "跟随系统", "浅色", "深色"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in Chinese; texts %q", s, tt.Texts())
		}
	}
}

func TestAppearanceNormal(t *testing.T) {
	for in, want := range map[config.Appearance]config.Appearance{
		"": config.AppearanceSystem, "system": config.AppearanceSystem, "light": config.AppearanceLight,
		"dark": config.AppearanceDark, "Dark": config.AppearanceSystem, "sepia": config.AppearanceSystem,
	} {
		if got := in.Normal(); got != want {
			t.Errorf("%q.Normal() = %q, want %q", in, got, want)
		}
	}
}

// Every window takes the dark theme and palette on a dark appearance,
// and the light ones back on a light one, as the system's appearance
// changes while the app runs.
func TestDarkAppearance(t *testing.T) {
	a, p := testApp(t)
	demoSession(a, "web-1", "45.56.78.90")
	a.active = 0
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()
	// The side panel's background, beside its text.
	const x, y = 3, 300
	if theme != lightTheme || colors != &lightColors || !sameColor(pixel(tt, x, y, 1), lightColors.sidebar) {
		t.Fatalf("light: theme dark %v, side panel %v", theme.Dark, pixel(tt, x, y, 1))
	}
	saveShot(t, tt, "light-main-en.png")

	tt.SetDark(true)
	tt.Frame()
	if theme != darkTheme || colors != &darkColors || codeTheme() != codeThemes[1] {
		t.Fatalf("dark: theme dark %v", theme.Dark)
	}
	if got := pixel(tt, x, y, 1); !sameColor(got, darkColors.sidebar) {
		t.Errorf("dark side panel %v, want %v", got, darkColors.sidebar)
	}
	if darkTheme.Spacing != lightTheme.Spacing || darkTheme.FontSize != lightTheme.FontSize || darkTheme.Font != lightTheme.Font {
		t.Errorf("the dark theme's metrics differ: %+v", darkTheme)
	}
	saveShot(t, tt, "dark-main-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	saveShot(t, tt, "dark-main-zh.png")

	// A child window builds dark too.
	a.openOptions()
	opts := p.show(t, WindowOptions)
	opts.SetDark(true)
	opts.Frame()
	if got := pixel(opts, 400, 300, 2); !sameColor(got, darkTheme.Background) {
		t.Errorf("dark options window %v", got)
	}
	saveShot(t, opts, "dark-options-zh.png")
	p.SetLocale("en")
	opts.Frame()
	saveShot(t, opts, "dark-options-en.png")

	tt.SetDark(false)
	tt.Frame()
	if theme != lightTheme || !sameColor(pixel(tt, x, y, 1), lightColors.sidebar) {
		t.Errorf("back to light: theme dark %v, side panel %v", theme.Dark, pixel(tt, x, y, 1))
	}
}

// The file panel shows the icon theme's light section on a light
// appearance only.
func TestFileIconsFollowAppearance(t *testing.T) {
	a, p := testApp(t)
	iconsSession(a)
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()
	waitIconTheme(t, a, p, tt)
	for _, tc := range []struct {
		name      string
		dir, dark bool
		want      string
	}{
		{"page.jinja", false, false, "jinja_light"},
		{"page.jinja", false, true, "jinja"},
		{"jinja", true, false, "folder-jinja_light"},
		{"jinja", true, true, "folder-jinja"},
		{"main.go", false, true, "go"},
		{"main.go", false, false, "go"},
	} {
		if ic := a.iconOf(tc.name, tc.dir, false, false, tc.dark); ic == nil || ic.ID != tc.want {
			t.Errorf("%s (dark %v): %+v, want %s", tc.name, tc.dark, ic, tc.want)
		}
	}
	tt.SetDark(true)
	tt.Frame()
	saveShot(t, tt, "dark-file-icons-en.png")
}
