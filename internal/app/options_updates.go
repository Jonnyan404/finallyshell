package app

import (
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// releasesURL is where the releases are published, for installs that do
// not update themselves.
const releasesURL = "https://github.com/TooonyChen/finallyshell/releases"

// UpdateStatus is what the app's updater knows, for the Updates row of
// Options › General.
type UpdateStatus struct {
	// Version is the version running, "" for a development build.
	Version string
	// Blocked is why this copy cannot update itself; UpdatesEnabled when
	// it can.
	Blocked UpdateBlock
	// Automatic is whether it checks for updates in the background.
	Automatic bool
	// Available is the newer version the last check found, "" for none
	// (or one the user skipped).
	Available string
}

// UpdateBlock is why a copy of the app cannot update itself.
type UpdateBlock int

const (
	UpdatesEnabled UpdateBlock = iota
	// UpdatesDevelopment: a development build (go run, mygo dev).
	UpdatesDevelopment
	// UpdatesManaged: it cannot write where it is installed, as the .deb
	// in /opt, which the package manager updates.
	UpdatesManaged
)

// updatesSection is the Updates row of the General page: the version, and
// checking for updates now or in the background, or why this copy cannot
// update itself and where to get a new version. The update window itself
// is MyGo's updater plugin's (main.go).
func (a *App) updatesSection(c *ui.Context) {
	st := a.plat.Updates()
	ui.Column(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
		ui.Text(c, locales.OptionsUpdates()).Bold()
		if st.Version != "" {
			ui.Text(c, locales.UpdatesVersion(st.Version))
		} else {
			ui.Text(c, locales.UpdatesVersionDevelopment())
		}
		if st.Blocked != UpdatesEnabled {
			why := locales.UpdatesManaged()
			if st.Blocked == UpdatesDevelopment {
				why = locales.UpdatesDevelopment()
			}
			widgets.Muted(c, why).FontSize(12)
			ui.Link(c, locales.UpdatesReleasesPage(), releasesURL)
			return
		}
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			if ui.Button(c, locales.CheckForUpdates()).Clicked() {
				a.plat.CheckForUpdates()
			}
			if st.Available != "" {
				ui.Text(c, locales.UpdatesAvailable(st.Available)).TextColor(c.Theme().Accent)
			}
		})
		auto := st.Automatic
		if ui.Checkbox(c, &auto, locales.OptionCheckUpdatesAutomatically()).Changed() {
			a.plat.SetAutomaticUpdateChecks(auto)
			a.redraw()
		}
	})
}
