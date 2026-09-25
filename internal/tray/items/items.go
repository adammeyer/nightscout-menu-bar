package items

import (
	"fyne.io/systray"
	"gabe565.com/nightscout-menu-bar/internal/config"
	"gabe565.com/nightscout-menu-bar/internal/tray/items/preferences"
)

type Items struct {
	LastReading *systray.MenuItem
	Error       *systray.MenuItem
	History     History
	Preferences preferences.Preferences
	About       *systray.MenuItem
	Quit        *systray.MenuItem
}

func New(conf *config.Config) Items {
	var items Items

	items.LastReading = NewLastReading()
	items.Error = NewError()
	items.History = NewHistory()
	systray.AddSeparator()

	items.Preferences = preferences.New(conf)
	systray.AddSeparator()

	items.About = NewAbout(conf.Version)
	items.Quit = NewQuit()

	return items
}
