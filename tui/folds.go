package tui

import (
	"fmt"
	"slices"
	"sort"
	"time"

	"go.rockorager.dev/vaxis"
	vui "go.rockorager.dev/vaxis/ui"

	"go.rockorager.dev/comview/diff"
)

// uiDiffFold is a file or hunk header and the body rows folding it hides.
type uiDiffFold struct {
	Row int
	End int
	// Key names the fold by commit, file, and hunk position so folds
	// survive watch-mode reloads of the same diff.
	Key string
}

// uiDiffFoldLayout records every foldable header and which rows the folded
// ones hide. hidden and visible are nil when nothing is folded.
type uiDiffFoldLayout struct {
	folds   []uiDiffFold
	hidden  []bool
	visible []int
}

func uiDiffFolds(rows []diff.Row) []uiDiffFold {
	folds := make([]uiDiffFold, 0)
	commit := 0
	fileKey := ""
	hunk := 0
	for index, row := range rows {
		switch row.Kind {
		case diff.RowCommitHeader:
			commit++
		case diff.RowFile:
			fileKey = fmt.Sprintf("%d\x00%s", commit, row.Text)
			hunk = 0
			if end := uiDiffFoldEnd(rows, index, uiDiffFileBodyRow); end > index+1 {
				folds = append(folds, uiDiffFold{Row: index, End: end, Key: fileKey})
			}
		case diff.RowHunk:
			hunk++
			if end := uiDiffFoldEnd(rows, index, uiDiffHunkBodyRow); end > index+1 {
				folds = append(folds, uiDiffFold{Row: index, End: end, Key: fmt.Sprintf("%s\x00%d", fileKey, hunk)})
			}
		}
	}
	return folds
}

// uiDiffFoldEnd returns the row after header's body. Blank rows that
// separate the body from the next file stay visible.
func uiDiffFoldEnd(rows []diff.Row, header int, body func(diff.RowKind) bool) int {
	end := header + 1
	for index := header + 1; index < len(rows) && body(rows[index].Kind); index++ {
		if rows[index].Kind != diff.RowBlank {
			end = index + 1
		}
	}
	return end
}

func uiDiffFileBodyRow(kind diff.RowKind) bool {
	switch kind {
	case diff.RowMeta, diff.RowHunk, diff.RowBlank:
		return true
	default:
		return uiDiffHunkBodyRow(kind)
	}
}

func uiDiffHunkBodyRow(kind diff.RowKind) bool {
	switch kind {
	case diff.RowContext, diff.RowAdd, diff.RowDelete, diff.RowNoNewline:
		return true
	default:
		return false
	}
}

func (l uiDiffFoldLayout) foldAt(row int) (uiDiffFold, bool) {
	index := sort.Search(len(l.folds), func(i int) bool { return l.folds[i].Row >= row })
	if index < len(l.folds) && l.folds[index].Row == row {
		return l.folds[index], true
	}
	return uiDiffFold{}, false
}

func (s *uiDiffViewState) foldLayout(rows []diff.Row) uiDiffFoldLayout {
	if s.foldRows != nil && uiDiffRowsShareBacking(s.foldRows, rows) {
		return s.foldCache
	}
	layout := uiDiffFoldLayout{folds: uiDiffFolds(rows)}
	for _, fold := range layout.folds {
		if !s.folded[fold.Key] {
			continue
		}
		if layout.hidden == nil {
			layout.hidden = make([]bool, len(rows))
		}
		for row := fold.Row + 1; row < fold.End; row++ {
			layout.hidden[row] = true
		}
	}
	if layout.hidden != nil {
		layout.visible = make([]int, 0, len(rows))
		for row, hidden := range layout.hidden {
			if !hidden {
				layout.visible = append(layout.visible, row)
			}
		}
	}
	s.foldRows = rows
	s.foldCache = layout
	return layout
}

func (s *uiDiffViewState) setFolded(key string, folded bool) {
	if folded {
		if s.folded == nil {
			s.folded = make(map[string]bool)
		}
		s.folded[key] = true
	} else {
		delete(s.folded, key)
	}
	s.foldRows = nil
	s.sideRowsRows = nil
	s.foldRevealFrames = 2
}

// TickFrame runs before each frame is built. Folding changes the list's item
// count, but the list only takes the new count when that frame is laid out,
// so a reveal of the cursor's new item can fall outside the old layout. Reveal
// it again on the frame after.
func (s *uiDiffViewState) TickFrame(time.Time) bool {
	if s.foldRevealFrames == 0 {
		return false
	}
	s.foldRevealFrames--
	if s.foldRevealFrames > 0 {
		return true
	}
	s.revealCursorRow()
	return false
}

func (s *uiDiffViewState) rowHidden(rows []diff.Row, row int) bool {
	hidden := s.foldLayout(rows).hidden
	return hidden != nil && hidden[row]
}

// foldedHeader reports the fold whose folded header is row.
func (s *uiDiffViewState) foldedHeader(rows []diff.Row, row int) (uiDiffFold, bool) {
	fold, ok := s.foldLayout(rows).foldAt(row)
	return fold, ok && s.folded[fold.Key]
}

// rowCursorable reports whether the cursor may rest on row. Folded headers
// stand in for their hidden body, so the cursor can rest on them to unfold.
func (s *uiDiffViewState) rowCursorable(rows []diff.Row, row int) bool {
	if s.rowHidden(rows, row) {
		return false
	}
	if uiDiffCursorableRow(rows[row]) {
		return true
	}
	_, folded := s.foldedHeader(rows, row)
	return folded
}

// filterFoldedSideRows drops paired rows whose document rows are all hidden.
func (s *uiDiffViewState) filterFoldedSideRows(rows []diff.Row, sideRows []sideBySideRow) []sideBySideRow {
	if s.foldLayout(rows).hidden == nil {
		return sideRows
	}
	return slices.DeleteFunc(sideRows, func(sideRow sideBySideRow) bool {
		for _, docRow := range []int{sideRow.Full, sideRow.Left, sideRow.Right} {
			if docRow >= 0 && !s.rowHidden(rows, docRow) {
				return false
			}
		}
		return true
	})
}

// visualIndex returns the list item showing docRow in the current layout.
func (s *uiDiffViewState) visualIndex(rows []diff.Row, docRow int) (int, bool) {
	if s.sideBySide {
		for index, row := range s.sideBySideRows(rows) {
			if rowContainsDocRow(row, docRow) {
				return index, true
			}
		}
		return docRow, false
	}
	visible := s.foldLayout(rows).visible
	if visible == nil {
		return docRow, docRow >= 0 && docRow < len(rows)
	}
	index := sort.SearchInts(visible, docRow)
	return index, index < len(visible) && visible[index] == docRow
}

func (s *uiDiffViewState) visualRowCount(rows []diff.Row) int {
	if s.sideBySide {
		return len(s.sideBySideRows(rows))
	}
	if visible := s.foldLayout(rows).visible; visible != nil {
		return len(visible)
	}
	return len(rows)
}

// unifiedDocRow maps a unified list item to its document row. Side-by-side
// items are returned unchanged because they pair several document rows.
func (s *uiDiffViewState) unifiedDocRow(rows []diff.Row, visual int) int {
	if s.sideBySide {
		return visual
	}
	visible := s.foldLayout(rows).visible
	if visible == nil || visual < 0 || visual >= len(visible) {
		return visual
	}
	return visible[visual]
}

// toggleFold opens the folded header under the cursor, or folds the
// innermost hunk or file containing the cursor.
func (s *uiDiffViewState) toggleFold(rows []diff.Row) {
	if fold, ok := s.foldedHeader(rows, s.cursor.Row); ok {
		s.setFolded(fold.Key, false)
		s.setCursorRow(rows, fold.Row)
		return
	}
	s.closeFold(rows)
}

// closeFold folds the innermost open hunk or file containing the cursor.
// On a folded hunk header, that is the hunk's file.
func (s *uiDiffViewState) closeFold(rows []diff.Row) {
	folds := s.foldLayout(rows).folds
	for index := len(folds) - 1; index >= 0; index-- {
		fold := folds[index]
		if fold.Row > s.cursor.Row || s.cursor.Row >= fold.End || s.folded[fold.Key] {
			continue
		}
		s.clearLineSelection()
		s.setFolded(fold.Key, true)
		s.setCursorRow(rows, fold.Row)
		return
	}
}

// foldAllFiles folds every file, keeping each file's hunk folds for when
// it is opened again.
func (s *uiDiffViewState) foldAllFiles(rows []diff.Row) {
	header := -1
	for _, fold := range s.foldLayout(rows).folds {
		if rows[fold.Row].Kind != diff.RowFile {
			continue
		}
		s.setFolded(fold.Key, true)
		if fold.Row <= s.cursor.Row && s.cursor.Row < fold.End {
			header = fold.Row
		}
	}
	if header >= 0 {
		s.clearLineSelection()
		s.setCursorRow(rows, header)
	}
	s.SetState(func() {})
}

func (s *uiDiffViewState) unfoldAll(rows []diff.Row) {
	for key := range s.folded {
		s.setFolded(key, false)
	}
	s.setCursorRow(rows, s.cursor.Row)
}

// revealRow opens every fold that hides row or is folded at row.
func (s *uiDiffViewState) revealRow(rows []diff.Row, row int) {
	for _, fold := range s.foldLayout(rows).folds {
		if fold.Row <= row && row < fold.End && s.folded[fold.Key] {
			s.setFolded(fold.Key, false)
		}
	}
}

// foldSummary describes what a folded header hides.
func (s *uiDiffViewState) foldSummary(rows []diff.Row, row int, theme vui.Theme) []vaxis.Segment {
	fold, ok := s.foldedHeader(rows, row)
	if !ok {
		return nil
	}
	hidden := fold.End - fold.Row - 1
	stats := rowsStats(rows[fold.Row+1 : fold.End])
	dim := uiDimStyle(theme)
	return []vaxis.Segment{
		{Text: fmt.Sprintf("  ⋯ %d %s ", hidden, pluralize(hidden, "line")), Style: dim},
		{Text: fmt.Sprintf("+%d", stats.Adds), Style: vaxis.Style{Foreground: theme.Palette.Green.Tone500, Background: theme.Background}},
		{Text: " ", Style: dim},
		{Text: fmt.Sprintf("-%d", stats.Deletes), Style: vaxis.Style{Foreground: theme.Palette.Red.Tone500, Background: theme.Background}},
		// Keeps the stats clear of the scrollbar in the last column.
		{Text: " ", Style: dim},
	}
}
