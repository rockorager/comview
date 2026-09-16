package tui

import (
	"path/filepath"
	"testing"

	"go.rockorager.dev/vaxis"
)

func TestExpandHomePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got, want := expandHomePath("~/comments.json"), filepath.Join(home, "comments.json"); got != want {
		t.Fatalf("expanded path = %q, want %q", got, want)
	}
	if got, want := expandHomePath("~"), home; got != want {
		t.Fatalf("expanded home = %q, want %q", got, want)
	}
	if got, want := expandHomePath("~other/comments.json"), "~other/comments.json"; got != want {
		t.Fatalf("expanded other user path = %q, want %q", got, want)
	}
}

func TestConfigFilePathExpandsXDGConfigHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "~/.config")

	want := filepath.Join(home, ".config", "comview", "config.json")
	if got := configFilePath(); got != want {
		t.Fatalf("config path = %q, want %q", got, want)
	}
}

func TestBindingsExplicitKeysTakePrecedence(t *testing.T) {
	for _, tt := range []struct {
		name      string
		overrides map[string][]string
		key       vaxis.Key
		action    string
		want      bool
	}{
		{"default full page", nil, vaxis.Key{Keycode: 'f', Modifiers: vaxis.ModCtrl}, "full_page_down", true},
		{"custom cursor wins", map[string][]string{"cursor_down": {"Ctrl+f"}}, vaxis.Key{Keycode: 'f', Modifiers: vaxis.ModCtrl}, "cursor_down", true},
		{"custom cursor suppresses default", map[string][]string{"cursor_down": {"Ctrl+f"}}, vaxis.Key{Keycode: 'f', Modifiers: vaxis.ModCtrl}, "full_page_down", false},
		{"custom left suppresses default", map[string][]string{"cursor_left": {"ctrl+b"}}, vaxis.Key{Keycode: 'b', Modifiers: vaxis.ModCtrl}, "full_page_up", false},
		{"custom full page wins", map[string][]string{"full_page_down": {"Page_Down"}}, vaxis.Key{Keycode: vaxis.KeyPgDown}, "full_page_down", true},
		{"custom full page suppresses half page", map[string][]string{"full_page_down": {"Page_Down"}}, vaxis.Key{Keycode: vaxis.KeyPgDown}, "half_page_down", false},
		{"replacement removes old default", map[string][]string{"full_page_down": {"ctrl+n"}}, vaxis.Key{Keycode: 'f', Modifiers: vaxis.ModCtrl}, "full_page_down", false},
		{"empty override keeps default", map[string][]string{"full_page_down": {}}, vaxis.Key{Keycode: 'f', Modifiers: vaxis.ModCtrl}, "full_page_down", true},
		{"unknown action does not claim key", map[string][]string{"typo": {"ctrl+f"}}, vaxis.Key{Keycode: 'f', Modifiers: vaxis.ModCtrl}, "full_page_down", true},
		{"finder override leaves diff default", map[string][]string{"fuzzy_next": {"ctrl+f"}}, vaxis.Key{Keycode: 'f', Modifiers: vaxis.ModCtrl}, "full_page_down", true},
		{"diff override leaves finder default", map[string][]string{"full_page_down": {"ctrl+n"}}, vaxis.Key{Keycode: 'n', Modifiers: vaxis.ModCtrl}, "fuzzy_next", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := newBindings(tt.overrides).Matches(tt.key, tt.action); got != tt.want {
				t.Fatalf("Matches(%q) = %v, want %v", tt.action, got, tt.want)
			}
		})
	}
}
