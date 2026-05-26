package tui

import (
	vui "go.rockorager.dev/vaxis/ui"

	"go.rockorager.dev/comview/diff"
	"go.rockorager.dev/comview/review"
)

func runUIDiff(rows []diff.Row) error {
	cfg := loadConfig()
	commentPath := cfg.CommentFile
	if commentPath == "" {
		commentPath = review.DefaultFilePath
	}
	commentFile, err := review.LoadFile(commentPath)
	if err != nil {
		return err
	}
	root := uiDiffRootWithReviewFileAndBindings(rows, cfg.Wrap, commentFile.Comments, commentPath, true, newBindings(cfg.Keybindings)).(uiDiffView)
	root.WorkTreeRoot = gitWorkTreeRoot()
	if cfg.Theme != "" {
		if t, ok := ThemeByName(cfg.Theme); ok {
			theme := uiThemeFromBaseColors(t.Colors)
			return vui.Run(root, vui.WithTheme(theme))
		}
	}
	return vui.Run(root)
}
