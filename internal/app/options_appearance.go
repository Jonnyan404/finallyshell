package app

import (
	"finallyshell/internal/config"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// appearanceRow is the Appearance row of the General page: light,
// dark, or the system's appearance.
func (a *App) appearanceRow(c *ui.Context) {
	ui.Column(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
		ui.Text(c, locales.OptionsAppearance()).Bold()
		chosen := a.settings.Appearance.Normal()
		ui.RadioGroup(c, func() {
			ui.Radio(c, &chosen, config.AppearanceSystem, locales.AppearanceSystem())
			ui.Radio(c, &chosen, config.AppearanceLight, locales.AppearanceLight())
			ui.Radio(c, &chosen, config.AppearanceDark, locales.AppearanceDark())
		}).Row().Gap(16).Label(locales.OptionsAppearance())
		widgets.Muted(c, locales.AppearanceNote()).FontSize(12)
		if chosen != a.settings.Appearance.Normal() {
			a.setAppearance(chosen)
		}
	})
}

// setAppearance makes every window light, dark, or follow the system,
// and saves the choice; the system's, the default, leaves the key out.
func (a *App) setAppearance(ap config.Appearance) {
	ap = ap.Normal()
	a.plat.SetAppearance(ap)
	a.changeSettings(func() {
		a.settings.Appearance = ap
		if ap == config.AppearanceSystem {
			a.settings.Appearance = ""
		}
	})
}
