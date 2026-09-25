package config

import (
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"

	"gabe565.com/utils/colorx"
	"gabe565.com/utils/slogx"
	"github.com/spf13/pflag"
)

type Config struct {
	File      string
	Flags     *pflag.FlagSet
	Version   string
	callbacks []func()
	data      atomic.Pointer[Data]
}

func (conf *Config) Data() Data {
	d := conf.data.Load()
	if d != nil {
		return *d
	}
	return Data{}
}

type Data struct {
	Title       string      `toml:"title"        comment:"Tray title."`
	LibreLinkUp LibreLinkUp `toml:"librelinkup"  comment:"LibreLinkUp account settings."`
	Units       Unit        `toml:"units"        comment:"Blood sugar unit. (one of: mg/dL, mmol/L)"`
	LastReading LastReading `toml:"last-reading" comment:"Disable parts of the menu bar text. Only supported on macOS and Linux."`
	DynamicIcon DynamicIcon `toml:"dynamic-icon" comment:"Makes the tray icon show the current blood sugar reading."`
	Arrows      Arrows      `toml:"arrows"       comment:"Customize the arrows."`
	Socket      Socket      `toml:"socket"       comment:"Exposes the latest reading to other applications over a local socket."`
	Log         Log         `toml:"log"          comment:"Log configuration"`
	Advanced    Advanced    `toml:"advanced"     comment:"Advanced settings."`
}

type LibreLinkUp struct {
	Username  string `toml:"username"   comment:"LibreLinkUp email address. (required)"`
	Password  string `toml:"password"   comment:"LibreLinkUp password. (required)"`
	Region    string `toml:"region"     comment:"LibreLinkUp region. (one of: ae, ap, au, ca, de, eu, eu2, fr, jp, us, la, ru, cn)\nIf left blank, the region will be detected automatically."`
	PatientID string `toml:"patient-id" comment:"Patient ID of the connection to follow. If left blank, the first connection will be used."`
}

type DynamicIcon struct {
	Enabled     bool       `toml:"enabled"`
	FontColor   colorx.Hex `toml:"font-color"    comment:"Hex code used to render text."`
	FontFile    string     `toml:"font-file"     comment:"Font path or filename of a system font. If left blank, an embedded font will be used."`
	MaxFontSize float64    `toml:"max-font-size" comment:"Maximum font size in points."`
}

type LastReading struct {
	StaleThreshold Duration `toml:"stale-threshold" comment:"Readings older than this duration will be displayed as stale (strikethrough)."`
	HideArrow      bool     `toml:"hide-arrow"`
	HideDelta      bool     `toml:"hide-delta"`
	HideTimeAgo    bool     `toml:"hide-time-ago"`
}

type Arrows struct {
	DoubleUp      string `toml:"double-up"`
	SingleUp      string `toml:"single-up"`
	FortyFiveUp   string `toml:"forty-five-up"`
	Flat          string `toml:"flat"`
	FortyFiveDown string `toml:"forty-five-down"`
	SingleDown    string `toml:"single-down"`
	DoubleDown    string `toml:"double-down"`
	Unknown       string `toml:"unknown"`
}

type Socket struct {
	Enabled bool   `toml:"enabled"`
	Format  string `toml:"format"  comment:"Local file format. (one of: csv)"`
	Path    string `toml:"path"    comment:"File path. $TMPDIR will be replaced with the current temp directory."`
}

type Log struct {
	Level  slogx.Level  `toml:"level"  comment:"Values: trace, debug, info, warn, error, fatal, panic"`
	Format slogx.Format `toml:"format" comment:"Values: auto, color, plain, json"`
}

type Advanced struct {
	Interval   Duration `toml:"interval"    comment:"How often to fetch new readings from LibreLinkUp."`
	APIVersion string   `toml:"api-version" comment:"LibreLinkUp app version sent to the API.\nOnly change this if LibreLinkUp starts rejecting requests with a version error."`
	RoundAge   bool     `toml:"round-age"   comment:"If enabled, the reading's age will be rounded up to the nearest minute."`
}

const (
	configDir  = "nightscout-menu-bar"
	configFile = "config.toml"
)

func GetDir() (string, error) {
	dirs, err := GetDirs()
	if err != nil {
		return "", err
	}
	return resolveDir(dirs), nil
}

func GetDirs() ([]string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dirs := []string{filepath.Join(dir, configDir)}

	if runtime.GOOS == "darwin" {
		legacy, err := legacyDarwinDir()
		if err != nil {
			return nil, err
		}
		dirs = append(dirs, legacy)
	}

	return dirs, nil
}

func legacyDarwinDir() (string, error) {
	if xdgConfigHome := os.Getenv("XDG_CONFIG_HOME"); xdgConfigHome != "" {
		return filepath.Join(xdgConfigHome, configDir), nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".config", configDir), nil
}

func resolveDir(dirs []string) string {
	for _, dir := range dirs {
		if _, err := os.Stat(filepath.Join(dir, configFile)); err == nil {
			return dir
		}
	}
	return dirs[0]
}
