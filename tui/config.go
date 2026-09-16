package tui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"go.rockorager.dev/vaxis"
)

// Config holds user configuration loaded from ~/.config/comview/config.json.
type Config struct {
	// CommentFile overrides the default path for storing review comments.
	CommentFile string `json:"comment_file,omitempty"`
	// Wrap enables soft-wrapping of long diff lines instead of horizontal scrolling.
	Wrap bool `json:"wrap,omitempty"`
	// Keybindings maps action names to lists of key strings in vaxis
	// MatchString format (e.g. "ctrl+d", "shift+j", "Page_Down").
	// A non-empty list replaces the defaults for that action.
	// Explicit bindings take precedence over defaults in the same view.
	Keybindings map[string][]string `json:"keybindings,omitempty"`
	// Theme sets the color theme by name.
	Theme string `json:"theme,omitempty"`
}

func loadConfig() Config {
	data, err := os.ReadFile(configFilePath())
	if errors.Is(err, os.ErrNotExist) {
		return Config{}
	}
	if err != nil {
		return Config{}
	}
	var cfg Config
	_ = json.Unmarshal(data, &cfg)
	cfg.CommentFile = expandHomePath(cfg.CommentFile)
	return cfg
}

func configFilePath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(expandHomePath(xdg), "comview", "config.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "comview", "config.json")
}

func expandHomePath(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
		return path
	}
	if len(path) >= 2 && path[0] == '~' && os.IsPathSeparator(path[1]) {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// defaultKeybindings maps action names to their default key strings.
// Key strings use vaxis MatchString format: modifier names joined by "+",
// then the key name or character. Examples: "ctrl+d", "shift+j", "Page_Down".
var defaultKeybindings = map[string][]string{
	"cursor_down":    {"j", "Down"},
	"cursor_up":      {"k", "Up"},
	"cursor_left":    {"h", "Left"},
	"cursor_right":   {"l", "Right"},
	"half_page_down": {"ctrl+d", "Page_Down"},
	"half_page_up":   {"ctrl+u", "Page_Up"},
	"full_page_down": {"ctrl+f"},
	"full_page_up":   {"ctrl+b"},
	"cursor_bottom":  {"G", "End"},
	"next_commit":    {"J"},
	"prev_commit":    {"K"},
	"toggle_layout":  {"s"},
	"search":         {"/"},
	"next_result":    {"n"},
	"prev_result":    {"N"},
	"open_editor":    {"o"},
	"yank":           {"y", "Copy", "super+c"},
	// fuzzy finder navigation
	"fuzzy_next": {"Down", "ctrl+n", "ctrl+j"},
	"fuzzy_prev": {"Up", "ctrl+p", "ctrl+k"},
}

// Bindings resolves key events to named actions.
type Bindings struct {
	overrides map[string][]string
}

func newBindings(overrides map[string][]string) Bindings {
	actions := make(map[string][]string, len(overrides))
	for action, keys := range overrides {
		if len(keys) > 0 {
			actions[action] = keys
		}
	}
	return Bindings{overrides: actions}
}

// Matches reports whether key matches any configured key string for action.
func (b Bindings) Matches(key vaxis.Key, action string) bool {
	keys := b.overrides[action]
	if len(keys) == 0 {
		// Finder and diff navigation are independent binding scopes.
		for other, bindings := range b.overrides {
			if defaultKeybindings[other] == nil || strings.HasPrefix(other, "fuzzy_") != strings.HasPrefix(action, "fuzzy_") {
				continue
			}
			for _, binding := range bindings {
				if key.MatchString(binding) {
					return false
				}
			}
		}
		keys = defaultKeybindings[action]
	}
	for _, s := range keys {
		if key.MatchString(s) {
			return true
		}
	}
	return false
}
