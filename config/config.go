package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const configDir = ".tilitili"
const configFile = "config"

// Config holds user-configurable parameters.
type Config struct {
	// VideoWidthPct is the percentage of screen width for the PiP window (10-90).
	VideoWidthPct int `json:"video_width_pct"`
	// VideoPosition controls where the PiP window appears.
	// Supported: "bottom-right", "bottom-left", "top-right", "top-left"
	VideoPosition string `json:"video_position"`
	// Ontop keeps the PiP window always on top.
	Ontop bool `json:"ontop"`
	// Source is the last used video source (e.g. "bilibili", "youtube").
	Source string `json:"source"`
}

func Default() *Config {
	return &Config{
		VideoWidthPct: 25,
		VideoPosition: "bottom-right",
		Ontop:         true,
		Source:        "bilibili",
	}
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, configDir, configFile)
}

// Load reads config from ~/.tilitili/config. Returns defaults for missing fields.
func Load() *Config {
	cfg := Default()
	data, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}
	// Unmarshal on top of defaults so missing fields keep their default values
	json.Unmarshal(data, cfg)
	return cfg
}

// Save writes the config to ~/.tilitili/config.
func (c *Config) Save() error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0644)
}


// MpvGeometry returns the mpv --geometry flag value based on config.
// Uses percentage-based sizing so it works correctly on Retina/HiDPI displays.
func (c *Config) MpvGeometry() string {
	pct := c.VideoWidthPct
	if pct < 10 {
		pct = 10
	}
	// Format: W%{+-}X{+-}Y — percentage width, mpv auto-calculates height from aspect ratio
	switch c.VideoPosition {
	case "bottom-left":
		return fmt.Sprintf("%d%%+20-40", pct)
	case "top-right":
		return fmt.Sprintf("%d%%-20+40", pct)
	case "top-left":
		return fmt.Sprintf("%d%%+20+40", pct)
	default: // bottom-right
		return fmt.Sprintf("%d%%-20-40", pct)
	}
}

// Parameters returns the list of configurable parameters for the interactive CLI.
func (c *Config) Parameters() []Parameter {
	return []Parameter{
		{
			Name:        "video_width",
			Description: "PiP window width as % of screen width (10 - 90)",
			Value:       fmt.Sprintf("%d%%", c.VideoWidthPct),
			Set: func(val string) error {
				val = strings.TrimSuffix(val, "%")
				n, err := strconv.Atoi(val)
				if err != nil {
					return fmt.Errorf("invalid number: %s", val)
				}
				if n < 10 || n > 90 {
					return fmt.Errorf("must be between 10 and 90")
				}
				c.VideoWidthPct = n
				return nil
			},
		},
		{
			Name:        "video_position",
			Description: "PiP window position (bottom-right, bottom-left, top-right, top-left)",
			Value:       c.VideoPosition,
			Set: func(val string) error {
				valid := map[string]bool{
					"bottom-right": true, "bottom-left": true,
					"top-right": true, "top-left": true,
				}
				if !valid[val] {
					return fmt.Errorf("must be one of: bottom-right, bottom-left, top-right, top-left")
				}
				c.VideoPosition = val
				return nil
			},
		},
		{
			Name:        "ontop",
			Description: "Keep PiP window always on top (true/false)",
			Value:       fmt.Sprintf("%t", c.Ontop),
			Set: func(val string) error {
				switch strings.ToLower(val) {
				case "true", "1", "yes":
					c.Ontop = true
				case "false", "0", "no":
					c.Ontop = false
				default:
					return fmt.Errorf("must be true or false")
				}
				return nil
			},
		},
	}
}

// Parameter represents a single configurable parameter.
type Parameter struct {
	Name        string
	Description string
	Value       string
	Set         func(string) error
}

// Verbose controls debug logging for config operations.
var Verbose bool

func debugf(format string, args ...interface{}) {
	if Verbose {
		log.Printf("[DEBUG] "+format, args...)
	}
}
