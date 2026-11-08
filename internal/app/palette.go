package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// The app has a light and a dark look, and shows the one of the system's
// appearance, or the one the user chose in Options › General, which
// forces the app's appearance (options_appearance.go). Each window picks
// its look at the start of each frame (useTheme), from the appearance
// MyGo gives the frame.

// newTheme is MyGo's theme t made compact, with fonts that cover Chinese.
func newTheme(t *ui.Theme) *ui.Theme {
	t.Spacing = 3
	t.FontSize = 13
	t.Font = "Microsoft YaHei UI, PingFang SC, Noto Sans CJK SC, WenQuanYi Zen Hei, system-ui"
	return t
}

// The app's two themes, made once: the light one and the dark one.
var (
	lightTheme = newTheme(ui.LightTheme())
	darkTheme  = newTheme(ui.DarkTheme())
)

// theme and colors are the theme and the palette of the window being
// built. useTheme sets them at the start of each window's frame; every
// window builds on the UI thread, one at a time, and all show the same
// appearance.
var (
	theme  = lightTheme
	colors = &lightColors
)

// useTheme gives the frame c builds the app's theme for its appearance,
// and makes it, with its palette, theme and colors.
func useTheme(c *ui.Context) {
	theme, colors = lightTheme, &lightColors
	if c.Theme().Dark {
		theme, colors = darkTheme, &darkColors
	}
	c.SetTheme(theme)
}

// palette is the app's colors besides its theme's, for one appearance.
type palette struct {
	// sidebar is the gray of the side panel, as FinalShell's, and of the
	// bars around the file editor's text.
	sidebar ui.Color
	// line divides parts of a window: under tab strips and toolbars,
	// between toolbar groups, around the file editor's text.
	line ui.Color
	// rowLine is the fainter line between the rows of a list.
	rowLine ui.Color
	// frame outlines the side panel's tables and divides its halves.
	frame ui.Color
	// procTable and diskTable fill the side panel's tables; procHead
	// and procHeadText are the process table's header.
	procTable, diskTable   ui.Color
	procHead, procHeadText ui.Color
	// cpu, mem and swap fill the side panel's usage bars, with the
	// theme's text over them.
	cpu, mem, swap ui.Color
	// tx and rx are the upload and download speeds.
	tx, rx ui.Color
	// tableHead is the header row of the start page's table; note is the
	// box of details in a dialog.
	tableHead, note ui.Color
	// dangerBanner and warningBanner fill the file editor's notes above
	// the text.
	dangerBanner, warningBanner ui.Color
	// problemText and warningText are the shortcuts page's notes on a
	// key: one that cannot be used, and a conflict.
	problemText, warningText ui.Color
	// activeLine is the line with the caret in the file editor, and
	// lineNumber its line numbers.
	activeLine, lineNumber ui.Color
}

var lightColors = palette{
	sidebar:       ui.Hex("#f3f4f6"),
	line:          ui.Hex("#e5e7eb"),
	rowLine:       ui.Hex("#f1f5f9"),
	frame:         ui.Hex("#d1d5db"),
	procTable:     ui.Hex("#fafafa"),
	diskTable:     ui.Hex("#ffffff"),
	procHead:      ui.Hex("#60a5fa"),
	procHeadText:  ui.Hex("#ffffff"),
	cpu:           ui.Hex("#86efac"),
	mem:           ui.Hex("#fdba74"),
	swap:          ui.Hex("#93c5fd"),
	tx:            ui.Hex("#ea580c"),
	rx:            ui.Hex("#16a34a"),
	tableHead:     ui.Hex("#f9fafb"),
	note:          ui.Hex("#f3f4f6"),
	dangerBanner:  ui.Hex("#fef2f2"),
	warningBanner: ui.Hex("#fffbeb"),
	problemText:   ui.Hex("#b91c1c"),
	warningText:   ui.Hex("#a16207"),
	activeLine:    ui.Hex("#f6f7f9"),
	lineNumber:    ui.Hex("#9ca3af"),
}

// darkColors are lightColors for the dark theme's background (#18181b):
// the panels a little lighter than it, the tables in them darker, and
// the fills under text deep enough for the theme's light text.
var darkColors = palette{
	sidebar:       ui.Hex("#202024"),
	line:          ui.Hex("#2e2e33"),
	rowLine:       ui.Hex("#232327"),
	frame:         ui.Hex("#3f3f46"),
	procTable:     ui.Hex("#1b1b1e"),
	diskTable:     ui.Hex("#18181b"),
	procHead:      ui.Hex("#1d4ed8"),
	procHeadText:  ui.Hex("#ffffff"),
	cpu:           ui.Hex("#166534"),
	mem:           ui.Hex("#9a3412"),
	swap:          ui.Hex("#1e40af"),
	tx:            ui.Hex("#fb923c"),
	rx:            ui.Hex("#4ade80"),
	tableHead:     ui.Hex("#202024"),
	note:          ui.Hex("#27272a"),
	dangerBanner:  ui.Hex("#3b1d1f"),
	warningBanner: ui.Hex("#3a2c12"),
	problemText:   ui.Hex("#f87171"),
	warningText:   ui.Hex("#facc15"),
	activeLine:    ui.RGBA(255, 255, 255, 0.045),
	lineNumber:    ui.Hex("#71717a"),
}

// Colors that read on both backgrounds.
var (
	// folderColor is the app's own folder icon.
	folderColor = ui.Hex("#eab308")
	// The side panel's charts: upload and download, and latency.
	txChart, rxChart, latencyChart = ui.Hex("#fb923c"), ui.Hex("#4ade80"), ui.Hex("#60a5fa")
)

// codeThemes are the file editor's colors on each theme: the editor's
// own, with the app's accent, and the line with the caret faint.
var codeThemes = [2]*editor.Theme{newCodeTheme(lightTheme, &lightColors), newCodeTheme(darkTheme, &darkColors)}

func newCodeTheme(t *ui.Theme, p *palette) *editor.Theme {
	th := editor.DefaultTheme(t)
	th.ActiveLine = p.activeLine
	th.LineNumber = p.lineNumber
	sel := float32(0.18)
	if t.Dark {
		sel = 0.35
	}
	th.Selection = t.Accent.Alpha(sel)
	th.InactiveSelection = t.Accent.Alpha(sel * 0.55)
	return th
}

// codeTheme is the file editor's colors in the window being built.
func codeTheme() *editor.Theme {
	if theme.Dark {
		return codeThemes[1]
	}
	return codeThemes[0]
}
