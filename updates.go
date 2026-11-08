package main

import (
	"sync"

	"finallyshell/internal/app"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
)

// useUpdater adds MyGo's update window, in native UI. It checks for
// updates in the background, a day apart, unless the user turned that off
// (Options › General), and keeps the user's choices in updater.json in the
// user data folder. The releases it reads, and the key their updates are
// signed with, are updates in mygo.json.
//
// Its Language is left empty: the window then speaks App.Locale, the
// language the app shows, which follows the app's Language setting
// through App.SetLocale, also when the user changes it while the app runs.
// A Language set here would keep the window in the one the app started in.
func useUpdater() {
	mygo.Use(native.New(updater.Options{}))
}

// canUpdate is mygo.Updater.Enabled, asked once: it tries to write where
// the app is installed.
var canUpdate = sync.OnceValue(mygo.Updater.Enabled)

// watchUpdates draws the windows again when what Updates reports changed:
// a check found a version, or the user answered the update window.
// updater.OnChange calls on the UI thread.
func (p *platform) watchUpdates() {
	updater.OnChange(p.Redraw)
}

func (p *platform) Updates() app.UpdateStatus {
	if mygo.IsDev() {
		return app.UpdateStatus{Blocked: app.UpdatesDevelopment}
	}
	st := app.UpdateStatus{Version: mygo.App.Version()}
	if !canUpdate() {
		st.Blocked = app.UpdatesManaged
		return st
	}
	st.Automatic = updater.AutomaticChecks()
	st.Available = updater.AvailableVersion()
	return st
}

func (p *platform) CheckForUpdates()                 { updater.CheckForUpdates() }
func (p *platform) SetAutomaticUpdateChecks(on bool) { updater.SetAutomaticChecks(on) }

// setAppMenu sets the macOS menu bar: MyGo's default but its File menu,
// whose Close Window takes Cmd+W before the windows (the main one closes
// the tab with it, a secondary one itself: openWindow in internal/app),
// with "Check for Updates…" after About. The item says it in the language
// the app shows when it is made, so main sets the menu again when the
// language changes.
func setAppMenu() {
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu, Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			updater.MenuItem(),
			mygo.Separator(),
			{Role: mygo.RoleServices},
			mygo.Separator(),
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			{Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Role: mygo.RoleEditMenu}, {Role: mygo.RoleViewMenu}, {Role: mygo.RoleWindowMenu},
	}))
}
