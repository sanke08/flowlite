// Package config persists user settings under the OS's per-user config dir:
// ~/Library/Application Support/FlowLite on macOS, %AppData%\FlowLite on
// Windows. The models directory keeps the layout the previous version used,
// so already-downloaded weights are picked up without a re-download.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sanke08/flowlite/internal/hotkey"
)

const appDir = "FlowLite"

// Config is everything the user can change.
type Config struct {
	Model            string `json:"model"` // catalog key; "" until setup
	Hotkey           string `json:"hotkey"`
	HoldThresholdMS  int    `json:"hold_threshold_ms"` // tap vs hold cut-off
	Language         string `json:"language"`          // "" = autodetect
	InputDevice      string `json:"input_device"`      // device name; "" = default
	RestoreClipboard bool   `json:"restore_clipboard"`
	Sounds           bool   `json:"sounds"`
	MaxSeconds       int    `json:"max_seconds"`     // hard stop for a forgotten toggle
	PillPosition     string `json:"pill_position"`   // screen edge the pill sits on: bottom, top, left, right
	HistoryEnabled   bool   `json:"history_enabled"` // whether transcripts are remembered at all
}

// Default is a fresh, unconfigured settings object.
func Default() *Config {
	return &Config{
		Hotkey:           hotkey.DefaultName(),
		HoldThresholdMS:  400,
		RestoreClipboard: true,
		Sounds:           true,
		MaxSeconds:       300,
		PillPosition:     "bottom",
		HistoryEnabled:   true,
	}
}

// Configured reports whether setup has chosen a model.
func (c *Config) Configured() bool { return c.Model != "" }

// Dir returns (and creates) the FlowLite config directory.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(base, appDir)
	return d, os.MkdirAll(d, 0o755)
}

// ModelsDir returns (and creates) where GGML files live.
func ModelsDir() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	m := filepath.Join(d, "models", "whispercpp")
	return m, os.MkdirAll(m, 0o755)
}

// Path is the config.json location.
func Path() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.json"), nil
}

// LogPath is where the daemon writes its log.
func LogPath() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "flowlite.log"), nil
}

// PIDPath is the background daemon's pidfile.
func PIDPath() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "flowlite.pid"), nil
}

// ModePath is where the running daemon records whether it is a foreground
// session or a detached one. It is deliberately a separate file: the pidfile's
// format is read by every version ever installed, and an older binary that
// finds something it cannot parse there concludes nothing is running and
// starts a second daemon — two event taps, two pastes per dictation.
func ModePath() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "flowlite.mode"), nil
}

// Load reads config.json, returning defaults when it does not exist.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	c := Default()
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", p, err)
	}
	if c.HoldThresholdMS <= 0 {
		c.HoldThresholdMS = 400
	}
	if c.MaxSeconds <= 0 {
		c.MaxSeconds = 300
	}
	if !hotkey.Valid(c.Hotkey) {
		c.Hotkey = hotkey.DefaultName()
	}
	switch c.PillPosition {
	case "bottom", "top", "left", "right":
	default:
		c.PillPosition = "bottom"
	}
	return c, nil
}

// Save writes config.json atomically.
func (c *Config) Save() error {
	p, err := Path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
