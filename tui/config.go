package tui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"git.sr.ht/~rockorager/vaxis"
)

// Config holds user configuration loaded from ~/.config/comview/config.json.
type Config struct {
	// CommentFile overrides the default path for storing review comments.
	CommentFile string `json:"comment_file,omitempty"`
	// Keybindings maps action names to lists of key strings in vaxis
	// MatchString format (e.g. "ctrl+d", "shift+j", "Page_Down").
	// A non-empty list replaces the defaults for that action.
	Keybindings map[string][]string `json:"keybindings,omitempty"`
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
	return cfg
}

func configFilePath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "comview", "config.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "comview", "config.json")
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
	actions map[string][]string
}

func newBindings(overrides map[string][]string) Bindings {
	actions := make(map[string][]string, len(defaultKeybindings))
	for action, keys := range defaultKeybindings {
		actions[action] = keys
	}
	for action, keys := range overrides {
		if len(keys) > 0 {
			actions[action] = keys
		}
	}
	return Bindings{actions: actions}
}

// Matches reports whether key matches any configured key string for action.
func (b Bindings) Matches(key vaxis.Key, action string) bool {
	keys := b.actions[action]
	if keys == nil {
		keys = defaultKeybindings[action]
	}
	for _, s := range keys {
		if key.MatchString(s) {
			return true
		}
	}
	return false
}
