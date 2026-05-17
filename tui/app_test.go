package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.sr.ht/~rockorager/vaxis"

	"github.com/rockorager/comview/diff"
	"github.com/rockorager/comview/review"
)

func TestDiffViewerUsesQueriedDiffColors(t *testing.T) {
	viewer := &diffViewer{}
	colors := TerminalColors{
		Red:   vaxis.RGBColor(1, 2, 3),
		Green: vaxis.RGBColor(4, 5, 6),
	}
	viewer.SetTerminalColors(colors)

	if got := viewer.styleFor(diff.RowDelete).Foreground; got != colors.Red {
		t.Fatalf("delete foreground = %v, want %v", got, colors.Red)
	}
	if got := viewer.styleFor(diff.RowAdd).Foreground; got != colors.Green {
		t.Fatalf("add foreground = %v, want %v", got, colors.Green)
	}
}

func TestRowsForInputReturnsNoRowsForEmptyInput(t *testing.T) {
	rows, err := rowsForInput("")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0", len(rows))
	}
}

func TestRowsForInputReturnsRowsForDiff(t *testing.T) {
	rows, err := rowsForInput(`diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1 +1 @@
-old
+new
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("rows = 0, want diff rows")
	}
}

func TestDiffViewerEditorTargetUsesCursorRow(t *testing.T) {
	rows, err := rowsForInput(`diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -10,2 +10,2 @@
 old context
-old
+new
`)
	if err != nil {
		t.Fatal(err)
	}
	viewer := &diffViewer{
		rows:   rows,
		cursor: selectionPoint{Row: 3, Col: testCodeOffset(rows[3]) + 2},
	}

	target, ok := viewer.EditorTarget()
	if !ok {
		t.Fatal("editor target not found")
	}
	if target.Path != "main.go" || target.Line != 11 || target.Column != 3 {
		t.Fatalf("target = %+v, want main.go:11:3", target)
	}
}

func TestDiffViewerEditorTargetUsesTextColumnForTabs(t *testing.T) {
	row := diff.Row{
		Kind:     diff.RowAdd,
		FileName: "main.go",
		Gutter:   "    12 + ",
		Code:     "\tfoo",
		Review:   review.Anchor{Line: 12},
	}
	codeOffset := testCodeOffset(row)
	tests := []struct {
		name       string
		cursorCell int
		wantColumn int
	}{
		{
			name:       "inside tab",
			cursorCell: 4,
			wantColumn: 1,
		},
		{
			name:       "after tab",
			cursorCell: 8,
			wantColumn: 2,
		},
		{
			name:       "after first rune",
			cursorCell: 9,
			wantColumn: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viewer := &diffViewer{
				rows:   []diff.Row{row},
				cursor: selectionPoint{Row: 0, Col: codeOffset + tt.cursorCell},
			}

			target, ok := viewer.EditorTarget()
			if !ok {
				t.Fatal("editor target not found")
			}
			if target.Column != tt.wantColumn {
				t.Fatalf("column = %d, want %d", target.Column, tt.wantColumn)
			}
		})
	}
}

func TestDiffViewerEditorTargetFallsBackToLineOne(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{Kind: diff.RowFile, Text: "main.go", FileName: "main.go"}},
	}

	target, ok := viewer.EditorTarget()
	if !ok {
		t.Fatal("editor target not found")
	}
	if target.Path != "main.go" || target.Line != 1 || target.Column != 1 {
		t.Fatalf("target = %+v, want main.go:1:1", target)
	}
}

func TestDiffViewerEditorTargetReportsMissingFile(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{Kind: diff.RowBlank}},
	}

	_, ok := viewer.EditorTarget()
	if ok {
		t.Fatal("editor target found for row without file")
	}
	if got, want := viewer.statusMessage, "No file."; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

func TestDiffViewerOOpensEditor(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{Kind: diff.RowAdd, FileName: "main.go", Review: review.Anchor{Line: 12}}},
	}

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "o", Keycode: 'o'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandOpenEditor {
		t.Fatalf("command = %v, want %v", cmd, CommandOpenEditor)
	}
}

func TestDiffViewerSpaceEOpensFileFinder(t *testing.T) {
	rows, err := rowsForInput(`diff --git a/first.go b/first.go
--- a/first.go
+++ b/first.go
@@ -1 +1 @@
-old
+new
diff --git a/second.go b/second.go
--- a/second.go
+++ b/second.go
@@ -1 +1 @@
-old
+new
`)
	if err != nil {
		t.Fatal(err)
	}
	viewer := &diffViewer{rows: rows, height: 4}

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: " ", Keycode: vaxis.KeySpace})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandNone || viewer.keys.Pending() != " " {
		t.Fatalf("space command/pending = %v/%q, want none/space", cmd, viewer.keys.Pending())
	}

	cmd, err = viewer.HandleEvent(vaxis.Key{Text: "e", Keycode: 'e'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw || viewer.mode != modeFuzzy || viewer.finder == nil {
		t.Fatalf("command/mode/finder = %v/%v/%v, want redraw/fuzzy/finder", cmd, viewer.mode, viewer.finder)
	}
	if len(viewer.finder.Items) != 2 {
		t.Fatalf("finder items = %+v, want 2 files", viewer.finder.Items)
	}
}

func TestDiffViewerFileFinderIncludesDiffStatFiles(t *testing.T) {
	rows, err := rowsForInput(` README.md        |  1 +
 tui/app.go       | 12 ++++++------
 2 files changed, 7 insertions(+), 6 deletions(-)
`)
	if err != nil {
		t.Fatal(err)
	}
	viewer := &diffViewer{rows: rows}

	items := viewer.fileFinderItems()
	if len(items) != 2 {
		t.Fatalf("items = %+v, want 2", items)
	}
	if items[1].Label != "tui/app.go" || items[1].Detail != "+6 -6" {
		t.Fatalf("second item = %+v", items[1])
	}
}

func TestDiffViewerStatusCountsDiffStatFiles(t *testing.T) {
	rows, err := rowsForInput(` README.md        |  1 +
 tui/app.go       | 12 ++++++------
 2 files changed, 7 insertions(+), 6 deletions(-)
`)
	if err != nil {
		t.Fatal(err)
	}
	viewer := &diffViewer{rows: rows, cursor: selectionPoint{Row: 1}}

	context := viewer.currentStatusContext()
	if context.Files != 2 || context.FileIndex != 2 || context.File != "tui/app.go" {
		t.Fatalf("context = %+v, want second of two stat files", context)
	}
	if context.TotalStats.Adds != 7 || context.TotalStats.Deletes != 6 {
		t.Fatalf("total stats = %+v, want +7 -6", context.TotalStats)
	}
}

func TestDiffViewerFileFinderJumpsToSelectedFile(t *testing.T) {
	rows, err := rowsForInput(`diff --git a/first.go b/first.go
--- a/first.go
+++ b/first.go
@@ -1 +1 @@
-old
+new
diff --git a/second.go b/second.go
--- a/second.go
+++ b/second.go
@@ -1 +1 @@
-old
+new
`)
	if err != nil {
		t.Fatal(err)
	}
	viewer := &diffViewer{rows: rows}
	if cmd := viewer.openFileFinderCommand(); cmd != CommandRedraw {
		t.Fatalf("open command = %v, want redraw", cmd)
	}
	viewer.finder.SetQuery("second")

	cmd := viewer.handleFuzzyKey(vaxis.Key{Keycode: vaxis.KeyEnter})
	if cmd != CommandRedraw {
		t.Fatalf("enter command = %v, want redraw", cmd)
	}
	if viewer.mode != modeNormal || viewer.finder != nil {
		t.Fatalf("mode/finder = %v/%v, want normal/nil", viewer.mode, viewer.finder)
	}
	if viewer.rows[viewer.cursor.Row].Text != "second.go" {
		t.Fatalf("cursor row = %+v, want second.go file row", viewer.rows[viewer.cursor.Row])
	}
	if viewer.scroll != viewer.cursor.Row {
		t.Fatalf("scroll = %d, want cursor row %d", viewer.scroll, viewer.cursor.Row)
	}
}

func TestDiffViewerFileFinderLayoutDoesNotShrinkWithMatches(t *testing.T) {
	rows, err := rowsForInput(`diff --git a/first.go b/first.go
--- a/first.go
+++ b/first.go
@@ -1 +1 @@
-old
+new
diff --git a/second.go b/second.go
--- a/second.go
+++ b/second.go
@@ -1 +1 @@
-old
+new
diff --git a/third.go b/third.go
--- a/third.go
+++ b/third.go
@@ -1 +1 @@
-old
+new
`)
	if err != nil {
		t.Fatal(err)
	}
	viewer := &diffViewer{rows: rows}
	viewer.openFileFinderCommand()

	before, ok := viewer.fuzzyFinderLayout(80, 24)
	if !ok {
		t.Fatal("finder layout missing")
	}
	viewer.finder.SetQuery("third")
	after, ok := viewer.fuzzyFinderLayout(80, 24)
	if !ok {
		t.Fatal("filtered finder layout missing")
	}
	if after.boxHeight != before.boxHeight || after.y != before.y || after.visibleRows != before.visibleRows {
		t.Fatalf("layout changed from %+v to %+v", before, after)
	}
}

func TestFuzzyFinderRowWidthsKeepStatsVisible(t *testing.T) {
	labelWidth, detailWidth, showDetail := fuzzyFinderRowWidths(20, "+117 -42")
	if !showDetail {
		t.Fatal("show detail = false, want true")
	}
	if detailWidth < textCellWidth("+117 -42") {
		t.Fatalf("detail width = %d, want room for +117 -42", detailWidth)
	}
	if labelWidth != 10 {
		t.Fatalf("label width = %d, want 10", labelWidth)
	}
}

func TestPrintSegmentsHardClippedDoesNotEllipsizeExactFit(t *testing.T) {
	cells := testCells{}

	paintSegmentsHardClipped(cells, 0, 0, textCellWidth("+117 -42"), vaxis.Segment{Text: "+117 -42"})

	if got, want := cells.text(textCellWidth("+117 -42")), "+117 -42"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestPrintSegmentsHardClippedTruncatesRightWithoutEllipsis(t *testing.T) {
	cells := testCells{}

	paintSegmentsHardClipped(cells, 0, 0, 6, vaxis.Segment{Text: "README.md"})

	if got, want := cells.text(6), "README"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestDiffViewerFuzzyDetailSegmentsColorStats(t *testing.T) {
	viewer := &diffViewer{}
	viewer.ensureColorScheme()

	segments := viewer.fuzzyDetailSegments("+117 -42", 9, viewer.scheme.Background)

	if len(segments) != 4 {
		t.Fatalf("segments = %+v, want padding/add/space/delete", segments)
	}
	if segments[1].Text != "+117" || segments[1].Style.Foreground != viewer.scheme.Add {
		t.Fatalf("add segment = %+v", segments[1])
	}
	if segments[3].Text != "-42" || segments[3].Style.Foreground != viewer.scheme.Delete {
		t.Fatalf("delete segment = %+v", segments[3])
	}
}

func TestDiffViewerFallsBackToRGBDiffColors(t *testing.T) {
	viewer := &diffViewer{}
	viewer.ensureColorScheme()

	if got, want := viewer.styleFor(diff.RowDelete).Foreground, DefaultColorScheme().Delete; got != want {
		t.Fatalf("delete foreground = %v, want %v", got, want)
	}
	if got, want := viewer.styleFor(diff.RowAdd).Foreground, DefaultColorScheme().Add; got != want {
		t.Fatalf("add foreground = %v, want %v", got, want)
	}
}

func TestDefaultColorSchemeUsesOnlyRGBColors(t *testing.T) {
	scheme := DefaultColorScheme()
	colors := []vaxis.Color{
		scheme.Base.Foreground,
		scheme.Base.Background,
		scheme.Base.Red,
		scheme.Base.Green,
		scheme.Base.Yellow,
		scheme.Base.Blue,
		scheme.Base.Magenta,
		scheme.Base.Cyan,
		scheme.Foreground,
		scheme.Background,
		scheme.Code,
		scheme.Dim,
		scheme.Header,
		scheme.Muted,
		scheme.Hunk,
		scheme.Gutter,
		scheme.Blue,
		scheme.Yellow,
		scheme.Add,
		scheme.AddLine,
		scheme.AddInline,
		scheme.Delete,
		scheme.DeleteLine,
		scheme.DeleteInline,
		scheme.Selection,
		scheme.Yank,
	}

	for _, color := range colors {
		if params := color.Params(); len(params) != 3 {
			t.Fatalf("color %v has params %v, want RGB params", color, params)
		}
	}
}

func TestColorSchemeDimIsBlendedRGB(t *testing.T) {
	scheme := DefaultColorScheme()
	want := blendRGB(scheme.Foreground, scheme.Background, dimBlend)

	if scheme.Dim != want {
		t.Fatalf("dim = %v, want %v", scheme.Dim, want)
	}
}

func TestColorSchemeGutterShadesWithBackgroundPolarity(t *testing.T) {
	dark := NewColorScheme(BaseColors{
		Foreground: vaxis.RGBColor(0xd7, 0xde, 0xe9),
		Background: vaxis.RGBColor(0x10, 0x14, 0x19),
		Red:        vaxis.RGBColor(0xaa, 0x00, 0x00),
		Green:      vaxis.RGBColor(0x00, 0xaa, 0x00),
		Yellow:     vaxis.RGBColor(0xaa, 0xaa, 0x00),
		Blue:       vaxis.RGBColor(0x00, 0x00, 0xaa),
		Magenta:    vaxis.RGBColor(0xaa, 0x00, 0xaa),
		Cyan:       vaxis.RGBColor(0x00, 0xaa, 0xaa),
	})
	if got, want := dark.Gutter, blendRGB(dark.Background, trueBlack(), gutterBackgroundBlend); got != want {
		t.Fatalf("dark gutter = %v, want black shade %v", got, want)
	}

	light := NewColorScheme(BaseColors{
		Foreground: vaxis.RGBColor(0x44, 0x3f, 0x38),
		Background: vaxis.RGBColor(0xf8, 0xf4, 0xec),
		Red:        vaxis.RGBColor(0xaa, 0x00, 0x00),
		Green:      vaxis.RGBColor(0x00, 0xaa, 0x00),
		Yellow:     vaxis.RGBColor(0xaa, 0xaa, 0x00),
		Blue:       vaxis.RGBColor(0x00, 0x00, 0xaa),
		Magenta:    vaxis.RGBColor(0xaa, 0x00, 0xaa),
		Cyan:       vaxis.RGBColor(0x00, 0xaa, 0xaa),
	})
	if got, want := light.Gutter, blendRGB(light.Background, trueWhite(), gutterBackgroundBlend); got != want {
		t.Fatalf("light gutter = %v, want white shade %v", got, want)
	}
}

func TestChangedLineBackgroundsAreBlendedAndContrasting(t *testing.T) {
	scheme := DefaultColorScheme()

	if scheme.AddLine == scheme.Background {
		t.Fatal("add line background equals base background")
	}
	if scheme.DeleteLine == scheme.Background {
		t.Fatal("delete line background equals base background")
	}
	if got := contrastRatio(scheme.Background, scheme.AddLine); got < minChangedLineContrast {
		t.Fatalf("add line contrast = %f, want at least %f", got, minChangedLineContrast)
	}
	if got := contrastRatio(scheme.Background, scheme.DeleteLine); got < minChangedLineContrast {
		t.Fatalf("delete line contrast = %f, want at least %f", got, minChangedLineContrast)
	}
}

func TestDiffViewerUsesChangedLineBackgrounds(t *testing.T) {
	viewer := &diffViewer{}
	viewer.ensureColorScheme()

	if got, want := viewer.styleFor(diff.RowAdd).Background, viewer.scheme.AddLine; got != want {
		t.Fatalf("add background = %v, want %v", got, want)
	}
	if got, want := viewer.styleFor(diff.RowDelete).Background, viewer.scheme.DeleteLine; got != want {
		t.Fatalf("delete background = %v, want %v", got, want)
	}
}

func TestDiffViewerUsesInlineChangeBackgrounds(t *testing.T) {
	viewer := &diffViewer{}
	viewer.ensureColorScheme()

	if got, want := viewer.inlineBackground(diff.RowAdd), viewer.scheme.AddInline; got != want {
		t.Fatalf("add inline background = %v, want %v", got, want)
	}
	if got, want := viewer.inlineBackground(diff.RowDelete), viewer.scheme.DeleteInline; got != want {
		t.Fatalf("delete inline background = %v, want %v", got, want)
	}
}

func TestDiffViewerCursorLineStyleIsBlendedRGB(t *testing.T) {
	viewer := &diffViewer{}
	viewer.ensureColorScheme()
	base := vaxis.Style{
		Foreground: vaxis.RGBColor(1, 2, 3),
		Background: viewer.scheme.Code,
	}

	got := viewer.rowStyle(base, true)
	want := blendRGB(base.Background, viewer.scheme.Foreground, cursorLineBlend)
	if got.Background != want {
		t.Fatalf("cursor line background = %v, want %v", got.Background, want)
	}
	if params := got.Background.Params(); len(params) != 3 {
		t.Fatalf("cursor line background params = %v, want RGB params", params)
	}
	if got.Foreground != base.Foreground {
		t.Fatalf("cursor line foreground = %v, want %v", got.Foreground, base.Foreground)
	}
}

func TestDiffViewerCursorLineSegmentsDoNotMutateCachedSegments(t *testing.T) {
	viewer := &diffViewer{}
	viewer.ensureColorScheme()
	segments := []vaxis.Segment{{
		Text: "hello",
		Style: vaxis.Style{
			Foreground: vaxis.RGBColor(1, 2, 3),
			Background: viewer.scheme.Code,
		},
	}}

	styled := viewer.rowSegments(segments, true)
	if styled[0].Style.Background == segments[0].Style.Background {
		t.Fatalf("styled background = %v, want cursor line background", styled[0].Style.Background)
	}
	if segments[0].Style.Background != viewer.scheme.Code {
		t.Fatalf("cached segment background mutated to %v", segments[0].Style.Background)
	}
}

func TestCharacterAtCellReturnsSpaceForEmptyText(t *testing.T) {
	char := characterAtCell("", 0)
	if char.Grapheme != " " || char.Width != 1 {
		t.Fatalf("character = %+v, want space", char)
	}
}

func TestDiffViewerCachesRenderedCodeSegments(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{
				Kind:     diff.RowAdd,
				Code:     "hello",
				FileName: "example.txt",
				InlineSpans: []diff.InlineSpan{
					{Start: 1, End: 4, Kind: diff.InlineChange},
				},
			},
		},
		highlighter: NewSyntaxHighlighter(),
	}
	viewer.ensureColorScheme()

	viewer.ensureRenderCache()

	if len(viewer.codeSegments) != 1 {
		t.Fatalf("cached rows = %d, want 1", len(viewer.codeSegments))
	}
	segments := viewer.codeSegments[0]
	if len(segments) != 3 {
		t.Fatalf("segments = %+v, want 3", segments)
	}
	if segments[0].Text != "h" || segments[0].Style.Background == viewer.scheme.AddInline {
		t.Fatalf("first segment = %+v", segments[0])
	}
	if segments[1].Text != "ell" || segments[1].Style.Background != viewer.scheme.AddInline {
		t.Fatalf("inline segment = %+v, want add inline background", segments[1])
	}
	if segments[2].Text != "o" || segments[2].Style.Background == viewer.scheme.AddInline {
		t.Fatalf("last segment = %+v", segments[2])
	}

	cached := viewer.codeSegments
	viewer.ensureRenderCache()
	if &viewer.codeSegments[0][0] != &cached[0][0] {
		t.Fatal("render cache rebuilt when rows did not change")
	}
}

func TestDiffViewerInvalidatesRenderCacheWhenTerminalColorsChange(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowAdd, Code: "hello", FileName: "example.txt"},
		},
		highlighter: NewSyntaxHighlighter(),
	}
	viewer.ensureColorScheme()
	viewer.ensureRenderCache()

	viewer.SetTerminalColors(TerminalColors{Green: vaxis.RGBColor(1, 2, 3)})

	if viewer.codeSegments != nil {
		t.Fatalf("code segment cache = %+v, want nil", viewer.codeSegments)
	}
}

func TestDiffViewerUsesChangedGutterForegroundForChangedLines(t *testing.T) {
	viewer := &diffViewer{}
	viewer.ensureColorScheme()

	addGutter := viewer.gutterStyle(diff.RowAdd)
	if addGutter.Foreground != viewer.scheme.Add {
		t.Fatalf("add gutter foreground = %v, want %v", addGutter.Foreground, viewer.scheme.Add)
	}
	if addGutter.Background != viewer.scheme.Gutter {
		t.Fatalf("add gutter background = %v, want %v", addGutter.Background, viewer.scheme.Gutter)
	}

	deleteGutter := viewer.gutterStyle(diff.RowDelete)
	if deleteGutter.Foreground != viewer.scheme.Delete {
		t.Fatalf("delete gutter foreground = %v, want %v", deleteGutter.Foreground, viewer.scheme.Delete)
	}
	if deleteGutter.Background != viewer.scheme.Gutter {
		t.Fatalf("delete gutter background = %v, want %v", deleteGutter.Background, viewer.scheme.Gutter)
	}
}

func TestDiffViewerUsesSingleDarkGutterSegment(t *testing.T) {
	viewer := &diffViewer{}
	viewer.ensureColorScheme()
	segments := viewer.gutterSegments(diff.Row{
		Kind:   diff.RowAdd,
		Gutter: "1 2 ",
		Marker: "+",
	})

	if len(segments) != 1 {
		t.Fatalf("segments = %+v, want one", segments)
	}
	if segments[0].Text != "1 2 +" || segments[0].Style != viewer.gutterStyle(diff.RowAdd) {
		t.Fatalf("gutter segment = %+v", segments[0])
	}
}

func TestDiffViewerStylesCommitPreambleRows(t *testing.T) {
	viewer := &diffViewer{}
	viewer.ensureColorScheme()

	header, ok := viewer.structuredSegments(diff.Row{Kind: diff.RowCommitHeader, Prefix: "commit ", Code: "abc123"})
	if !ok {
		t.Fatal("commit header segments missing")
	}
	if header[0].Style.Foreground != viewer.scheme.Dim || header[1].Style.Foreground != viewer.scheme.Yellow {
		t.Fatalf("header segments = %+v", header)
	}

	trailer, ok := viewer.structuredSegments(diff.Row{Kind: diff.RowCommitTrailer, Prefix: "    Reviewed-by: ", Code: "Tim"})
	if !ok {
		t.Fatal("trailer segments missing")
	}
	if trailer[0].Style.Foreground != viewer.scheme.Blue || trailer[1].Style.Foreground != viewer.scheme.Dim {
		t.Fatalf("trailer segments = %+v", trailer)
	}
}

func TestDiffViewerStylesDiffStatRows(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowDiffStat, Stat: diff.Stat{Path: "README.md", Changed: 1, Bar: "+"}},
			{Kind: diff.RowDiffStat, Stat: diff.Stat{Path: "tui/app.go", Changed: 12, Bar: "++++++------"}},
		},
	}
	viewer.ensureColorScheme()

	segments, ok := viewer.structuredSegments(viewer.rows[0])
	if !ok {
		t.Fatal("diff stat segments missing")
	}

	var addStyled bool
	for _, segment := range segments {
		if segment.Text == "+" && segment.Style.Foreground == viewer.scheme.Add {
			addStyled = true
		}
	}
	deleteSegments, ok := viewer.structuredSegments(viewer.rows[1])
	if !ok {
		t.Fatal("second diff stat segments missing")
	}
	var deleteStyled bool
	for _, segment := range deleteSegments {
		if segment.Text == "-" && segment.Style.Foreground == viewer.scheme.Delete {
			deleteStyled = true
		}
	}
	if !addStyled || !deleteStyled {
		t.Fatalf("stat segments = %+v, want colored add/delete", segments)
	}
	if got, want := segmentsText(segments[:3]), " README.md  |  1 "; got != want {
		t.Fatalf("stat prefix = %q, want %q", got, want)
	}
}

func TestDiffViewerUsesReviewMarkerInGutterSpace(t *testing.T) {
	anchor := review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}
	viewer := &diffViewer{
		reviewDrafts: []review.CommentDraft{{
			Path: "main.go",
			Line: 2,
			Side: review.SideRight,
			Body: "comment",
		}},
	}
	viewer.ensureColorScheme()
	segments := viewer.gutterSegments(diff.Row{
		Kind:   diff.RowAdd,
		Gutter: "1 2 + ",
		Review: anchor,
	})

	if len(segments) != 2 {
		t.Fatalf("segments = %+v, want two", segments)
	}
	if got, want := segments[0].Text, "1 2 +"; got != want {
		t.Fatalf("gutter text = %q, want %q", got, want)
	}
	if got, want := segments[1].Text, "▐"; got != want {
		t.Fatalf("marker text = %q, want %q", got, want)
	}
	if got, want := segments[1].Style.Foreground, viewer.scheme.Yellow; got != want {
		t.Fatalf("marker foreground = %v, want %v", got, want)
	}
}

func TestDiffViewerStickyFileHeader(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowFile, Text: "one.go"},
			{Kind: diff.RowHunk, Text: "@@ -1 +1 @@"},
			{Kind: diff.RowDelete, Text: "-old"},
			{Kind: diff.RowBlank},
			{Kind: diff.RowFile, Text: "two.go"},
			{Kind: diff.RowHunk, Text: "@@ -1 +1 @@"},
		},
	}

	viewer.scroll = 0
	if row, ok := viewer.stickyFileHeader(); ok {
		t.Fatalf("sticky row at file header = %+v, want none", row)
	}

	viewer.scroll = 2
	row, ok := viewer.stickyFileHeader()
	if !ok || row.Text != "one.go" {
		t.Fatalf("sticky row = %+v, %v; want one.go", row, ok)
	}

	viewer.scroll = 4
	if row, ok := viewer.stickyFileHeader(); ok {
		t.Fatalf("sticky row at second file header = %+v, want none", row)
	}

	viewer.scroll = 5
	row, ok = viewer.stickyFileHeader()
	if !ok || row.Text != "two.go" {
		t.Fatalf("sticky row = %+v, %v; want two.go", row, ok)
	}
}

func TestDiffViewerModeLabels(t *testing.T) {
	viewer := &diffViewer{}
	if got := viewer.modeLabel(); got != "NORMAL" {
		t.Fatalf("mode label = %q, want NORMAL", got)
	}
	viewer.mode = modeVisual
	if got := viewer.modeLabel(); got != "VISUAL" {
		t.Fatalf("mode label = %q, want VISUAL", got)
	}
	viewer.mode = modeVisualLine
	if got := viewer.modeLabel(); got != "V-LINE" {
		t.Fatalf("mode label = %q, want V-LINE", got)
	}
	viewer.mode = modeInsert
	if got := viewer.modeLabel(); got != "INSERT" {
		t.Fatalf("mode label = %q, want INSERT", got)
	}
	viewer.mode = modeCommand
	if got := viewer.modeLabel(); got != "COMMAND" {
		t.Fatalf("mode label = %q, want COMMAND", got)
	}
	viewer.mode = modeSearch
	if got := viewer.modeLabel(); got != "SEARCH" {
		t.Fatalf("mode label = %q, want SEARCH", got)
	}
}

func TestDiffViewerStatusColorsFollowMode(t *testing.T) {
	viewer := &diffViewer{}
	viewer.ensureColorScheme()

	if got, want := viewer.statusColor(), viewer.scheme.Base.Blue; got != want {
		t.Fatalf("normal status color = %v, want blue %v", got, want)
	}
	if got, want := viewer.statusStyle().Background, viewer.scheme.Base.Blue; got != want {
		t.Fatalf("normal status background = %v, want blue %v", got, want)
	}

	viewer.mode = modeVisual
	if got, want := viewer.statusColor(), viewer.scheme.Base.Magenta; got != want {
		t.Fatalf("visual status color = %v, want magenta %v", got, want)
	}
	if got, want := viewer.statusSeparatorStyle(viewer.statusBackground()).Foreground, viewer.scheme.Base.Magenta; got != want {
		t.Fatalf("visual separator foreground = %v, want magenta %v", got, want)
	}

	viewer.mode = modeVisualLine
	if got, want := viewer.statusColor(), viewer.scheme.Base.Magenta; got != want {
		t.Fatalf("visual line status color = %v, want magenta %v", got, want)
	}

	viewer.mode = modeInsert
	if got, want := viewer.statusColor(), viewer.scheme.Base.Green; got != want {
		t.Fatalf("insert status color = %v, want green %v", got, want)
	}
}

func TestDiffViewerStatusFillUsesDistinctBackground(t *testing.T) {
	viewer := &diffViewer{}
	viewer.ensureColorScheme()

	style := viewer.statusFillStyle()
	if style.Background == viewer.scheme.Background {
		t.Fatalf("status background = %v, want distinct shade", style.Background)
	}
	if params := style.Background.Params(); params == nil {
		t.Fatalf("status background params = %v, want RGB params", params)
	}
	if got, want := viewer.statusSeparatorStyle(style.Background).Background, style.Background; got != want {
		t.Fatalf("separator background = %v, want status background %v", got, want)
	}
}

func TestDiffViewerBracketCJumpsBetweenChanges(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "same"},
		{Kind: diff.RowDelete, Gutter: "2   - ", Code: "old"},
		{Kind: diff.RowAdd, Gutter: "  2 + ", Code: "new"},
		{Kind: diff.RowContext, Gutter: "3 3   ", Code: "same"},
		{Kind: diff.RowAdd, Gutter: "  4 + ", Code: "other"},
	}
	for index := range rows {
		rows[index].Text = rows[index].Gutter + rows[index].Code
	}
	viewer := &diffViewer{rows: rows, cursor: selectionPoint{Row: 0}}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	if cmd, err := viewer.HandleEvent(vaxis.Key{Text: "]", Keycode: ']'}); err != nil {
		t.Fatal(err)
	} else if cmd != CommandNone {
		t.Fatalf("] command = %v, want none", cmd)
	}
	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "c", Keycode: 'c'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw || viewer.cursor.Row != 1 {
		t.Fatalf("]c command/cursor = %v/%+v, want redraw row 1", cmd, viewer.cursor)
	}

	if _, err := viewer.HandleEvent(vaxis.Key{Text: "]", Keycode: ']'}); err != nil {
		t.Fatal(err)
	}
	cmd, err = viewer.HandleEvent(vaxis.Key{Text: "c", Keycode: 'c'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw || viewer.cursor.Row != 4 {
		t.Fatalf("second ]c command/cursor = %v/%+v, want redraw row 4", cmd, viewer.cursor)
	}

	if _, err := viewer.HandleEvent(vaxis.Key{Text: "[", Keycode: '['}); err != nil {
		t.Fatal(err)
	}
	cmd, err = viewer.HandleEvent(vaxis.Key{Text: "c", Keycode: 'c'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw || viewer.cursor.Row != 1 {
		t.Fatalf("[c command/cursor = %v/%+v, want redraw row 1", cmd, viewer.cursor)
	}
}

func TestDiffViewerBracketNJumpsBetweenNotes(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Text: "one", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Text: "two", Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
		{Kind: diff.RowAdd, Text: "three", Code: "three", Review: review.Anchor{Path: "main.go", Line: 3, Side: review.SideRight}},
	}
	viewer := &diffViewer{
		rows: rows,
		reviewDrafts: []review.CommentDraft{
			{Path: "main.go", Line: 2, Side: review.SideRight, Body: "two"},
			{Path: "main.go", Line: 3, Side: review.SideRight, Body: "three"},
		},
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	if _, err := viewer.HandleEvent(vaxis.Key{Text: "]", Keycode: ']'}); err != nil {
		t.Fatal(err)
	}
	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "n", Keycode: 'n'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw || viewer.cursor.Row != 1 {
		t.Fatalf("]n command/cursor = %v/%+v, want redraw row 1", cmd, viewer.cursor)
	}

	if _, err := viewer.HandleEvent(vaxis.Key{Text: "]", Keycode: ']'}); err != nil {
		t.Fatal(err)
	}
	cmd, err = viewer.HandleEvent(vaxis.Key{Text: "n", Keycode: 'n'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw || viewer.cursor.Row != 2 {
		t.Fatalf("second ]n command/cursor = %v/%+v, want redraw row 2", cmd, viewer.cursor)
	}

	if _, err := viewer.HandleEvent(vaxis.Key{Text: "[", Keycode: '['}); err != nil {
		t.Fatal(err)
	}
	cmd, err = viewer.HandleEvent(vaxis.Key{Text: "n", Keycode: 'n'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw || viewer.cursor.Row != 1 {
		t.Fatalf("[n command/cursor = %v/%+v, want redraw row 1", cmd, viewer.cursor)
	}
}

func TestDiffViewerQuestionTogglesHelpOverlay(t *testing.T) {
	viewer := newTestDiffViewer(1, 10)

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "/", Keycode: '/', Modifiers: vaxis.ModShift})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw || !viewer.helpVisible {
		t.Fatalf("command/help = %v/%v, want redraw/visible", cmd, viewer.helpVisible)
	}

	cmd, err = viewer.HandleEvent(vaxis.Key{Text: "?", Keycode: '?'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw || viewer.helpVisible {
		t.Fatalf("command/help = %v/%v, want redraw/hidden", cmd, viewer.helpVisible)
	}
}

func TestDiffViewerHelpOverlayMatchesReadmeKeybinds(t *testing.T) {
	readme, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(readme)
	for _, binding := range helpKeybinds {
		row := fmt.Sprintf("| %s | %s |", binding.READMEKey, binding.Action)
		if !strings.Contains(text, row) {
			t.Fatalf("README missing help binding row %q", row)
		}
	}
}

func TestDiffViewerHelpOverlayHasRoomForKeybinds(t *testing.T) {
	viewer := &diffViewer{}
	width, height := viewer.helpOverlaySize(80, len(helpKeybinds)+4)
	if width <= 0 || height < len(helpKeybinds)+4 {
		t.Fatalf("help overlay size = %dx%d, want room for %d bindings", width, height, len(helpKeybinds))
	}
}

func TestDiffViewerStatusModeWidthMatchesPaintedSegments(t *testing.T) {
	viewer := &diffViewer{}
	viewer.ensureColorScheme()

	segments := viewer.statusModeSegments(viewer.statusCommitBackground())
	if got, want := segmentsWidth(segments), textCellWidth(segmentsText(segments)); got != want {
		t.Fatalf("mode width = %d, want painted width %d", got, want)
	}
	if got, want := segmentsText(segments), " NORMAL "; got != want {
		t.Fatalf("mode segments = %q, want %q", got, want)
	}
}

func TestDiffViewerStatusContextShowsSectionedFileAndTotals(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowCommitHeader, Text: "commit abc1234567890", Code: "abc1234567890"},
			{Kind: diff.RowFile, Text: "one.go"},
			{Kind: diff.RowAdd, FileName: "one.go", Code: "new"},
			{Kind: diff.RowDelete, FileName: "one.go", Code: "old"},
			{Kind: diff.RowFile, Text: "two.go"},
			{Kind: diff.RowAdd, FileName: "two.go", Code: "new"},
		},
		cursor: selectionPoint{Row: 2},
	}
	viewer.ensureColorScheme()

	left := viewer.statusLeftSegments()
	if got, want := segmentsText(left), " abc123456789  1/2 one.go  +1 -1"; got != want {
		t.Fatalf("left status = %q, want %q", got, want)
	}
	if left[0].Style.Background == viewer.statusBackground() {
		t.Fatalf("commit section background = %v, want colored section", left[0].Style.Background)
	}
	if left[0].Style.Foreground != viewer.scheme.Base.Blue {
		t.Fatalf("commit section foreground = %v, want blue %v", left[0].Style.Foreground, viewer.scheme.Base.Blue)
	}
	if left[2].Style.Background != viewer.statusBackground() {
		t.Fatalf("file section background = %v, want status background %v", left[2].Style.Background, viewer.statusBackground())
	}
	if left[2].Style.Attribute == vaxis.AttrBold {
		t.Fatalf("file context prefix style = %+v, want regular", left[2].Style)
	}
	if left[3].Text != "one.go" || left[3].Style.Attribute != vaxis.AttrBold {
		t.Fatalf("file context base segment = %q %+v, want bold file name", left[3].Text, left[3].Style)
	}
	if left[len(left)-3].Style.Foreground != viewer.scheme.Add {
		t.Fatalf("add stat style = %+v, want add foreground %v", left[len(left)-3].Style, viewer.scheme.Add)
	}
	if left[len(left)-1].Style.Foreground != viewer.scheme.Delete {
		t.Fatalf("delete stat style = %+v, want delete foreground %v", left[len(left)-1].Style, viewer.scheme.Delete)
	}

	right := viewer.statusRightSegments()
	if got, want := segmentsText(right), "1 commit / 2 files  +2 -1"; got != want {
		t.Fatalf("right status = %q, want %q", got, want)
	}
}

func TestDiffViewerStatusContextBoldsOnlyFileBase(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowFile, Text: "src/pkg/one.go"},
			{Kind: diff.RowAdd, FileName: "src/pkg/one.go", Code: "new"},
			{Kind: diff.RowFile, Text: "README.md"},
		},
		cursor: selectionPoint{Row: 1},
	}
	viewer.ensureColorScheme()

	left := viewer.statusLeftSegments()
	if got, want := segmentsText(left), " 1/2 src/pkg/one.go  +1 -0"; got != want {
		t.Fatalf("left status = %q, want %q", got, want)
	}
	if left[0].Text != " 1/2 src/pkg/" || left[0].Style.Attribute == vaxis.AttrBold {
		t.Fatalf("file context prefix segment = %q %+v, want regular directory prefix", left[0].Text, left[0].Style)
	}
	if left[1].Text != "one.go" || left[1].Style.Attribute != vaxis.AttrBold {
		t.Fatalf("file context base segment = %q %+v, want bold file name", left[1].Text, left[1].Style)
	}
	if left[2].Text != " " || left[2].Style.Attribute == vaxis.AttrBold {
		t.Fatalf("file context suffix segment = %q %+v, want regular padding", left[2].Text, left[2].Style)
	}
}

func TestDiffViewerStatusContextShowsCountsWhenMultipleCommits(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowCommitHeader, Text: "commit abc1234567890", Code: "abc1234567890"},
			{Kind: diff.RowFile, Text: "one.go"},
			{Kind: diff.RowCommitHeader, Text: "commit def1234567890", Code: "def1234567890"},
			{Kind: diff.RowFile, Text: "two.go"},
		},
		cursor: selectionPoint{Row: 3},
	}
	viewer.ensureColorScheme()

	if got, want := segmentsText(viewer.statusLeftSegments()), " 2/2 def123456789  2/2 two.go  +0 -0"; got != want {
		t.Fatalf("left status = %q, want %q", got, want)
	}
}

func TestDiffViewerEscReturnsToNormalMode(t *testing.T) {
	viewer := newTestDiffViewer(10, 10)
	viewer.mode = modeVisual
	viewer.selection = textSelection{
		Active: true,
		Anchor: selectionPoint{Row: 0, Col: 0},
		Cursor: selectionPoint{Row: 1, Col: 0},
	}

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "\x1b", Keycode: vaxis.KeyEsc})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want %v", cmd, CommandRedraw)
	}
	if viewer.mode != modeNormal {
		t.Fatalf("mode = %v, want normal", viewer.mode)
	}
	if viewer.selection.Active {
		t.Fatalf("selection still active: %+v", viewer.selection)
	}
}

func TestDiffViewerVisualModeKeys(t *testing.T) {
	tests := []struct {
		name string
		key  vaxis.Key
		mode viewMode
	}{
		{
			name: "v enters visual",
			key:  vaxis.Key{Text: "v", Keycode: 'v'},
			mode: modeVisual,
		},
		{
			name: "V enters visual line",
			key:  vaxis.Key{Text: "V", Keycode: 'V'},
			mode: modeVisualLine,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viewer := newTestDiffViewer(10, 10)
			viewer.rows[0] = diff.Row{
				Kind:   diff.RowAdd,
				Gutter: "1 1 + ",
				Code:   "hello",
			}
			viewer.rows[0].Text = viewer.rows[0].Gutter + viewer.rows[0].Code

			cmd, err := viewer.HandleEvent(tt.key)
			if err != nil {
				t.Fatal(err)
			}
			if cmd != CommandRedraw {
				t.Fatalf("command = %v, want %v", cmd, CommandRedraw)
			}
			if viewer.mode != tt.mode {
				t.Fatalf("mode = %v, want %v", viewer.mode, tt.mode)
			}
			if !viewer.selection.Active {
				t.Fatal("selection inactive")
			}
		})
	}
}

func TestDiffViewerVisualLineSelectsWholeCodeRows(t *testing.T) {
	rows := []diff.Row{
		{
			Kind:   diff.RowAdd,
			Gutter: "1 1 + ",
			Code:   "hello",
		},
		{
			Kind:   diff.RowAdd,
			Gutter: "2 2 + ",
			Code:   "world",
		},
	}
	for i := range rows {
		rows[i].Text = rows[i].Gutter + rows[i].Code
	}
	viewer := &diffViewer{rows: rows}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "V", Keycode: 'V'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want %v", cmd, CommandRedraw)
	}
	viewer.moveCursorRows(1)

	if got, want := viewer.ClipboardText(), "hello\nworld"; got != want {
		t.Fatalf("clipboard text = %q, want %q", got, want)
	}
}

func TestDiffViewerVisualLineSelectsWholeCodeRowsWhenMovingUp(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "hello"},
		{Kind: diff.RowAdd, Gutter: "2 2 + ", Code: "world"},
	}
	for i := range rows {
		rows[i].Text = rows[i].Gutter + rows[i].Code
	}
	viewer := &diffViewer{rows: rows, cursor: selectionPoint{Row: 1, Col: testCodeOffset(rows[1])}}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))
	viewer.cursorGoal = viewer.cursor.Col

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "V", Keycode: 'V'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want %v", cmd, CommandRedraw)
	}
	viewer.moveCursorRows(-1)

	if got, want := viewer.ClipboardText(), "hello\nworld"; got != want {
		t.Fatalf("clipboard text = %q, want %q", got, want)
	}
}

func TestDiffViewerInsertCreatesReviewDraftAtCursor(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{
			Kind:   diff.RowAdd,
			Text:   "hello",
			Code:   "hello",
			Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight, CommitID: "abc123"},
		}},
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	cmd := openReviewCommentEditor(t, viewer)
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want %v", cmd, CommandRedraw)
	}
	if len(viewer.reviewDrafts) != 0 {
		t.Fatalf("draft count after open = %d, want 0", len(viewer.reviewDrafts))
	}
	cmd = submitReviewComment(t, viewer, "looks good")
	if cmd != CommandRedraw {
		t.Fatalf("submit command = %v, want %v", cmd, CommandRedraw)
	}
	if len(viewer.reviewDrafts) != 1 {
		t.Fatalf("draft count = %d, want 1", len(viewer.reviewDrafts))
	}
	want := review.CommentDraft{Path: "main.go", Line: 12, Side: review.SideRight, CommitID: "abc123", Body: "looks good"}
	if got := viewer.reviewDrafts[0]; got != want {
		t.Fatalf("draft = %+v, want %+v", got, want)
	}
	if !viewer.hasReviewDraft(viewer.rows[0].Review) {
		t.Fatal("draft marker lookup failed")
	}
}

func TestDiffViewerReviewDraftMatchingUsesCommitID(t *testing.T) {
	viewer := &diffViewer{
		reviewDrafts: []review.CommentDraft{{
			Path:     "main.go",
			Line:     12,
			Side:     review.SideRight,
			CommitID: "abc123",
			Body:     "first commit",
		}},
	}

	firstCommit := review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight, CommitID: "abc123"}
	secondCommit := review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight, CommitID: "def456"}
	if !viewer.hasReviewDraft(firstCommit) {
		t.Fatal("draft should match anchor with same commit")
	}
	if viewer.hasReviewDraft(secondCommit) {
		t.Fatal("draft matched anchor from a different commit")
	}

	if reviewDraftMatchesTarget(viewer.reviewDrafts[0], review.CommentDraft{
		Path:     "main.go",
		Line:     12,
		Side:     review.SideRight,
		CommitID: "def456",
	}) {
		t.Fatal("draft target matched a different commit")
	}
}

func TestDiffViewerInsertCreatesReviewDraftForSelectionRange(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Text: "hello", Code: "hello", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}},
		{Kind: diff.RowAdd, Text: "world", Code: "world", Review: review.Anchor{Path: "main.go", Line: 13, Side: review.SideRight}},
	}
	viewer := &diffViewer{
		rows: rows,
		selection: textSelection{
			Active: true,
			Anchor: selectionPoint{Row: 0, Col: 0},
			Cursor: selectionPoint{Row: 1, Col: 0},
		},
		mode: modeVisual,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	cmd := openReviewCommentEditor(t, viewer)
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want %v", cmd, CommandRedraw)
	}
	cmd = submitReviewComment(t, viewer, "range comment")
	if cmd != CommandRedraw {
		t.Fatalf("submit command = %v, want %v", cmd, CommandRedraw)
	}
	if len(viewer.reviewDrafts) != 1 {
		t.Fatalf("draft count = %d, want 1", len(viewer.reviewDrafts))
	}
	want := review.CommentDraft{
		Path:      "main.go",
		Body:      "range comment",
		StartLine: 12,
		StartSide: review.SideRight,
		Line:      13,
		Side:      review.SideRight,
	}
	if got := viewer.reviewDrafts[0]; got != want {
		t.Fatalf("draft = %+v, want %+v", got, want)
	}
	if viewer.selection.Active || viewer.mode != modeNormal {
		t.Fatalf("selection/mode after draft = active:%v mode:%v, want normal", viewer.selection.Active, viewer.mode)
	}
	if !viewer.hasReviewDraft(rows[0].Review) || !viewer.hasReviewDraft(rows[1].Review) {
		t.Fatal("draft range marker lookup failed")
	}
}

func TestDiffViewerDoesNotCreateReviewDraftAcrossCommits(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Text: "first", Code: "first", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight, CommitID: "abc123"}},
		{Kind: diff.RowAdd, Text: "second", Code: "second", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight, CommitID: "def456"}},
	}
	viewer := &diffViewer{
		rows: rows,
		selection: textSelection{
			Active: true,
			Anchor: selectionPoint{Row: 0, Col: 0},
			Cursor: selectionPoint{Row: 1, Col: 0},
		},
		mode: modeVisual,
	}

	if draft, ok := viewer.reviewDraftTarget(); ok {
		t.Fatalf("draft = %+v, want no cross-commit draft", draft)
	}
}

func TestDiffViewerInsertCreatesReviewDraftWithColumnsForSingleLineVisualSelection(t *testing.T) {
	row := diff.Row{
		Kind:   diff.RowAdd,
		Gutter: "1 1 + ",
		Code:   "hello",
		Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight},
	}
	row.Text = row.Gutter + row.Code
	codeOffset := testCodeOffset(row)
	viewer := &diffViewer{
		rows: []diff.Row{row},
		selection: textSelection{
			Active: true,
			Anchor: selectionPoint{Row: 0, Col: codeOffset + 1},
			Cursor: selectionPoint{Row: 0, Col: codeOffset + 3},
		},
		mode: modeVisual,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	cmd := openReviewCommentEditor(t, viewer)
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want %v", cmd, CommandRedraw)
	}
	cmd = submitReviewComment(t, viewer, "column comment")
	if cmd != CommandRedraw {
		t.Fatalf("submit command = %v, want %v", cmd, CommandRedraw)
	}
	if len(viewer.reviewDrafts) != 1 {
		t.Fatalf("draft count = %d, want 1", len(viewer.reviewDrafts))
	}
	draft := viewer.reviewDrafts[0]
	if draft.StartColumn == nil || draft.EndColumn == nil {
		t.Fatalf("draft columns missing: %+v", draft)
	}
	if got, want := *draft.StartColumn, 2; got != want {
		t.Fatalf("start column = %d, want %d", got, want)
	}
	if got, want := *draft.EndColumn, 4; got != want {
		t.Fatalf("end column = %d, want %d", got, want)
	}
}

func TestDiffViewerVisualShiftIOpensCommentEditor(t *testing.T) {
	row := diff.Row{
		Kind:   diff.RowAdd,
		Gutter: "1 1 + ",
		Code:   "hello",
		Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight},
	}
	row.Text = row.Gutter + row.Code
	codeOffset := testCodeOffset(row)
	viewer := &diffViewer{
		rows: []diff.Row{row},
		selection: textSelection{
			Active: true,
			Anchor: selectionPoint{Row: 0, Col: codeOffset + 1},
			Cursor: selectionPoint{Row: 0, Col: codeOffset + 3},
		},
		mode: modeVisual,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "I", Keycode: 'I'})
	if err != nil {
		t.Fatal(err)
	}

	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want redraw", cmd)
	}
	if viewer.mode != modeInsert || viewer.editor == nil {
		t.Fatalf("mode/editor = %v/%v, want insert editor", viewer.mode, viewer.editor)
	}
	if viewer.textObject.active {
		t.Fatal("text object active after opening comment editor")
	}
}

func TestDiffViewerVisualIUsesNextKeyAsTextObject(t *testing.T) {
	viewer := &diffViewer{
		rows:   []diff.Row{{Kind: diff.RowContext, Text: "foo bar", Code: "foo bar"}},
		cursor: selectionPoint{Row: 0, Col: 5},
		mode:   modeVisual,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "i", Keycode: 'i'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandNone {
		t.Fatalf("initial command = %v, want none", cmd)
	}

	cmd, err = viewer.HandleEvent(vaxis.Key{Text: "w", Keycode: 'w'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("text object command = %v, want redraw", cmd)
	}
	if got, want := viewer.ClipboardText(), "bar"; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
}

func TestDiffViewerInsertDoesNotAddColumnsForVisualLineSelection(t *testing.T) {
	row := diff.Row{
		Kind:   diff.RowAdd,
		Gutter: "1 1 + ",
		Code:   "hello",
		Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight},
	}
	row.Text = row.Gutter + row.Code
	codeOffset := testCodeOffset(row)
	viewer := &diffViewer{
		rows: []diff.Row{row},
		selection: textSelection{
			Active: true,
			Anchor: selectionPoint{Row: 0, Col: codeOffset},
			Cursor: selectionPoint{Row: 0, Col: codeOffset + textCellWidth(row.Code) - 1},
		},
		mode: modeVisualLine,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	cmd := openReviewCommentEditor(t, viewer)
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want %v", cmd, CommandRedraw)
	}
	cmd = submitReviewComment(t, viewer, "line comment")
	if cmd != CommandRedraw {
		t.Fatalf("submit command = %v, want %v", cmd, CommandRedraw)
	}
	draft := viewer.reviewDrafts[0]
	if draft.StartColumn != nil || draft.EndColumn != nil {
		t.Fatalf("draft columns = %v:%v, want nil", draft.StartColumn, draft.EndColumn)
	}
}

func TestDiffViewerCommentEditorSupportsMultipleLines(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{
			Kind:   diff.RowAdd,
			Text:   "hello",
			Code:   "hello",
			Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight},
		}},
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))
	openReviewCommentEditor(t, viewer)

	if cmd, err := viewer.HandleEvent(vaxis.Key{Text: "first"}); err != nil {
		t.Fatal(err)
	} else if cmd != CommandRedraw {
		t.Fatalf("text command = %v, want %v", cmd, CommandRedraw)
	}
	if cmd, err := viewer.HandleEvent(vaxis.Key{Keycode: vaxis.KeyEnter}); err != nil {
		t.Fatal(err)
	} else if cmd != CommandRedraw {
		t.Fatalf("enter command = %v, want %v", cmd, CommandRedraw)
	}
	if cmd := submitReviewComment(t, viewer, "second"); cmd != CommandRedraw {
		t.Fatalf("submit command = %v, want %v", cmd, CommandRedraw)
	}

	if got, want := viewer.reviewDrafts[0].Body, "first\nsecond"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestDiffViewerCommentEditorOpensBelowCursor(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{
			Kind:   diff.RowAdd,
			Text:   "hello",
			Code:   "hello",
			Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight},
		}},
		cursor: selectionPoint{Row: 0, Col: 2},
	}
	viewer.Layout(Tight(Size{Width: 40, Height: 10}))
	openReviewCommentEditor(t, viewer)

	x, y, _, _, ok := viewer.commentEditorRect(40, 10)
	if !ok {
		t.Fatal("comment editor rect missing")
	}
	if x != 0 || y != 1 {
		t.Fatalf("editor origin = %d,%d, want 0,1", x, y)
	}
}

func TestDiffViewerInlineCommentRowsPushFollowingDiffRows(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Text: "one", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Text: "two", Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
	}
	viewer := &diffViewer{
		rows: rows,
		reviewDrafts: []review.CommentDraft{{
			Path: "main.go",
			Line: 1,
			Side: review.SideRight,
			Body: "comment",
		}},
	}
	viewer.Layout(Tight(Size{Width: 40, Height: 10}))

	if got, want := viewer.screenRowForDocRow(1, 40, 10), 4; got != want {
		t.Fatalf("second row screen row = %d, want %d", got, want)
	}
	if got, want := viewer.mouseDocumentRow(vaxis.Mouse{Row: 4}), 1; got != want {
		t.Fatalf("mouse document row = %d, want %d", got, want)
	}
	if got, want := viewer.mouseDocumentRow(vaxis.Mouse{Row: 1}), -1; got != want {
		t.Fatalf("comment mouse document row = %d, want %d", got, want)
	}
}

func TestDiffViewerInlineCommentBoxStartsAfterGutter(t *testing.T) {
	row := diff.Row{
		Kind:   diff.RowAdd,
		Gutter: "12 12 ",
		Marker: "+ ",
		Code:   "one",
		Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight},
	}
	row.Text = row.Gutter + row.Marker + row.Code
	viewer := &diffViewer{
		rows: []diff.Row{row},
		reviewDrafts: []review.CommentDraft{{
			Path: "main.go",
			Line: 1,
			Side: review.SideRight,
			Body: "comment",
		}},
	}
	viewer.Layout(Tight(Size{Width: 40, Height: 10}))

	layout, ok := viewer.reviewDraftBoxLayout(40, 10, 0, viewer.reviewDrafts[0])
	if !ok {
		t.Fatal("review draft box layout missing")
	}
	if got, want := layout.x, textCellWidth(row.Gutter+row.Marker); got != want {
		t.Fatalf("box x = %d, want code offset %d", got, want)
	}
}

func TestDiffViewerCommentBoxTextWidthCapsAtSeventyTwo(t *testing.T) {
	row := diff.Row{
		Kind:   diff.RowAdd,
		Gutter: "1 1 ",
		Marker: "+ ",
		Code:   "one",
		Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight},
	}
	row.Text = row.Gutter + row.Marker + row.Code
	viewer := &diffViewer{
		rows: []diff.Row{row},
		reviewDrafts: []review.CommentDraft{{
			Path: "main.go",
			Line: 1,
			Side: review.SideRight,
			Body: strings.Repeat("x", 80),
		}},
	}
	viewer.Layout(Tight(Size{Width: 120, Height: 10}))

	layout, ok := viewer.reviewDraftBoxLayout(120, 10, 0, viewer.reviewDrafts[0])
	if !ok {
		t.Fatal("review draft box layout missing")
	}
	if got, want := layout.bodyWidth, commentTextMaxWidth; got != want {
		t.Fatalf("body width = %d, want %d", got, want)
	}
	if got, want := layout.width, commentTextMaxWidth+4; got != want {
		t.Fatalf("box width = %d, want %d", got, want)
	}
}

func TestDiffViewerSideBySideCursorAccountsForInlineComments(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Text: "one", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Text: "two", Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
	}
	viewer := &diffViewer{
		rows:       rows,
		layoutMode: layoutSideBySide,
		cursor:     selectionPoint{Row: 1, Col: 0},
		reviewDrafts: []review.CommentDraft{{
			Path: "main.go",
			Line: 1,
			Side: review.SideRight,
			Body: "comment",
		}},
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	_, row, ok := viewer.cursorScreenPositionForSize(80, 10)
	if !ok {
		t.Fatal("cursor screen position missing")
	}
	if got, want := row, 4; got != want {
		t.Fatalf("cursor screen row = %d, want %d", got, want)
	}
}

func TestDiffViewerSideBySideHorizontalMovementCrossesPanesAtEdges(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowDelete, Text: "old", Code: "old", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideLeft}},
		{Kind: diff.RowAdd, Text: "new", Code: "new", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
	}
	viewer := &diffViewer{
		rows:       rows,
		layoutMode: layoutSideBySide,
		cursor:     selectionPoint{Row: 1, Col: 0},
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "h", Keycode: 'h'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("h command = %v, want redraw", cmd)
	}
	if got, want := viewer.cursor.Row, 0; got != want {
		t.Fatalf("cursor row after h = %d, want %d", got, want)
	}

	cmd, err = viewer.HandleEvent(vaxis.Key{Text: "l", Keycode: 'l'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("l command = %v, want redraw", cmd)
	}
	if got, want := viewer.cursor.Row, 1; got != want {
		t.Fatalf("cursor row after l = %d, want %d", got, want)
	}
}

func TestDiffViewerInlineCommentEditorRowsPushFollowingDiffRows(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Text: "one", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Text: "two", Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
	}
	viewer := &diffViewer{
		rows: rows,
	}
	viewer.Layout(Tight(Size{Width: 40, Height: 10}))
	openReviewCommentEditor(t, viewer)

	if got, want := viewer.screenRowForDocRow(1, 40, 10), 4; got != want {
		t.Fatalf("second row screen row = %d, want %d", got, want)
	}
}

func TestDiffViewerCommentNormalModeIReentersInsert(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{
			Kind:   diff.RowAdd,
			Text:   "hello",
			Code:   "hello",
			Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight},
		}},
	}
	viewer.Layout(Tight(Size{Width: 40, Height: 10}))
	openReviewCommentEditor(t, viewer)

	cmd, err := viewer.HandleEvent(vaxis.Key{Keycode: vaxis.KeyEsc})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw || viewer.mode != modeNormal || viewer.editor == nil {
		t.Fatalf("escape command/mode/editor = %v/%v/%v, want normal focused comment", cmd, viewer.mode, viewer.editor != nil)
	}

	cmd, err = viewer.HandleEvent(vaxis.Key{Text: "i", Keycode: 'i'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw || viewer.mode != modeInsert {
		t.Fatalf("i command/mode = %v/%v, want insert", cmd, viewer.mode)
	}
}

func TestDiffViewerMousePressInInlineCommentFocusesComment(t *testing.T) {
	row := diff.Row{
		Kind:   diff.RowAdd,
		Gutter: "1 1 ",
		Marker: "+ ",
		Code:   "one",
		Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight},
	}
	row.Text = row.Gutter + row.Marker + row.Code
	viewer := &diffViewer{
		rows: []diff.Row{row},
		reviewDrafts: []review.CommentDraft{{
			Path: "main.go",
			Line: 1,
			Side: review.SideRight,
			Body: "hello",
		}},
	}
	viewer.Layout(Tight(Size{Width: 40, Height: 10}))
	layout, ok := viewer.reviewDraftBoxLayout(40, 10, 0, viewer.reviewDrafts[0])
	if !ok {
		t.Fatal("review draft box layout missing")
	}

	cmd, err := viewer.HandleEvent(vaxis.Mouse{
		Button:    vaxis.MouseLeftButton,
		EventType: vaxis.EventPress,
		Row:       2,
		Col:       layout.x + 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("mouse command = %v, want redraw", cmd)
	}
	if viewer.editor == nil || viewer.mode != modeNormal {
		t.Fatalf("editor/mode = %v/%v, want focused comment", viewer.editor != nil, viewer.mode)
	}
	if got, want := viewer.editor.body(), "hello"; got != want {
		t.Fatalf("editor body = %q, want %q", got, want)
	}
}

func TestDiffViewerDoubleClickInCommentSelectsWord(t *testing.T) {
	row := diff.Row{
		Kind:   diff.RowAdd,
		Gutter: "1 1 ",
		Marker: "+ ",
		Code:   "one",
		Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight},
	}
	row.Text = row.Gutter + row.Marker + row.Code
	viewer := &diffViewer{
		rows: []diff.Row{row},
		reviewDrafts: []review.CommentDraft{{
			Path: "main.go",
			Line: 1,
			Side: review.SideRight,
			Body: "hello world",
		}},
	}
	viewer.Layout(Tight(Size{Width: 40, Height: 10}))
	layout, ok := viewer.reviewDraftBoxLayout(40, 10, 0, viewer.reviewDrafts[0])
	if !ok {
		t.Fatal("review draft box layout missing")
	}
	mouse := vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: 2, Col: layout.x + 2 + len("hello w")}
	if _, err := viewer.HandleEvent(mouse); err != nil {
		t.Fatal(err)
	}
	cmd, err := viewer.HandleEvent(mouse)
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw || !viewer.commentSelection.Active || viewer.mode != modeVisual {
		t.Fatalf("double click command/selection/mode = %v/%v/%v, want visual selection", cmd, viewer.commentSelection.Active, viewer.mode)
	}
	cmd, err = viewer.HandleEvent(vaxis.Key{Text: "y", Keycode: 'y'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandCopy {
		t.Fatalf("y command = %v, want copy", cmd)
	}
	if got, want := viewer.ClipboardText(), "world"; got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}
}

func TestDiffViewerTripleClickInCommentSelectsLine(t *testing.T) {
	row := diff.Row{
		Kind:   diff.RowAdd,
		Gutter: "1 1 ",
		Marker: "+ ",
		Code:   "one",
		Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight},
	}
	row.Text = row.Gutter + row.Marker + row.Code
	viewer := &diffViewer{
		rows: []diff.Row{row},
		reviewDrafts: []review.CommentDraft{{
			Path: "main.go",
			Line: 1,
			Side: review.SideRight,
			Body: "hello world",
		}},
	}
	viewer.Layout(Tight(Size{Width: 40, Height: 10}))
	layout, ok := viewer.reviewDraftBoxLayout(40, 10, 0, viewer.reviewDrafts[0])
	if !ok {
		t.Fatal("review draft box layout missing")
	}
	mouse := vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: 2, Col: layout.x + 2 + len("hello")}
	for i := 0; i < 3; i++ {
		if _, err := viewer.HandleEvent(mouse); err != nil {
			t.Fatal(err)
		}
	}
	if !viewer.commentSelection.Active || viewer.mode != modeVisualLine {
		t.Fatalf("selection/mode = %v/%v, want visual line", viewer.commentSelection.Active, viewer.mode)
	}
	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "y", Keycode: 'y'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandCopy {
		t.Fatalf("y command = %v, want copy", cmd)
	}
	if got, want := viewer.ClipboardText(), "hello world"; got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}
}

func TestDiffViewerVisualSelectionInCommentCopiesText(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{
			Kind:   diff.RowAdd,
			Text:   "one",
			Code:   "one",
			Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight},
		}},
		reviewDrafts: []review.CommentDraft{{
			Path: "main.go",
			Line: 1,
			Side: review.SideRight,
			Body: "hello",
		}},
	}
	viewer.Layout(Tight(Size{Width: 40, Height: 10}))
	if !viewer.openReviewCommentEditorAtIndex(0) {
		t.Fatal("comment editor did not open")
	}

	for _, key := range []vaxis.Key{
		{Text: "v", Keycode: 'v'},
		{Text: "l", Keycode: 'l'},
		{Text: "y", Keycode: 'y'},
	} {
		if _, err := viewer.HandleEvent(key); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := viewer.ClipboardText(), "hel"; got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}
}

func TestDiffViewerCommentVisualLineSelectionShowsEmptyLine(t *testing.T) {
	viewer := &diffViewer{
		editor: &commentEditor{lines: []string{""}},
		mode:   modeVisualLine,
		commentSelection: textSelection{
			Active: true,
			Anchor: selectionPoint{Row: 0, Col: 0},
			Cursor: selectionPoint{Row: 0, Col: 0},
		},
	}
	viewer.ensureColorScheme()

	segments := viewer.commentEditorSegments(commentDisplayLine{line: 0}, viewer.baseStyle())

	if len(segments) != 1 {
		t.Fatalf("segments = %+v, want one segment", segments)
	}
	if got, want := segments[0].Text, " "; got != want {
		t.Fatalf("segment text = %q, want %q", got, want)
	}
	if got, want := segments[0].Style.Background, viewer.selectionStyle().Background; got != want {
		t.Fatalf("segment background = %v, want %v", got, want)
	}
}

func TestDiffViewerCommentVisualDeleteRemovesSelection(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{
			Kind:   diff.RowAdd,
			Text:   "one",
			Code:   "one",
			Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight},
		}},
		reviewDrafts: []review.CommentDraft{{
			Path: "main.go",
			Line: 1,
			Side: review.SideRight,
			Body: "hello",
		}},
	}
	viewer.Layout(Tight(Size{Width: 40, Height: 10}))
	if !viewer.openReviewCommentEditorAtIndex(0) {
		t.Fatal("comment editor did not open")
	}

	for _, key := range []vaxis.Key{
		{Text: "v", Keycode: 'v'},
		{Text: "l", Keycode: 'l'},
		{Text: "d", Keycode: 'd'},
	} {
		if _, err := viewer.HandleEvent(key); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := viewer.editor.body(), "llo"; got != want {
		t.Fatalf("editor body = %q, want %q", got, want)
	}
	if viewer.commentSelection.Active || viewer.mode != modeNormal {
		t.Fatalf("selection/mode = %v/%v, want normal without selection", viewer.commentSelection.Active, viewer.mode)
	}
}

func TestDiffViewerCommentVisualLineDeleteRemovesLines(t *testing.T) {
	viewer := &diffViewer{
		editor: &commentEditor{lines: []string{"one", "two", "three"}},
		mode:   modeVisualLine,
		commentSelection: textSelection{
			Active: true,
			Anchor: selectionPoint{Row: 1, Col: 0},
			Cursor: selectionPoint{Row: 1, Col: 2},
		},
	}

	viewer.deleteCommentSelection()

	if got, want := viewer.editor.body(), "one\nthree"; got != want {
		t.Fatalf("editor body = %q, want %q", got, want)
	}
	if viewer.editor.row != 1 || viewer.editor.col != 0 {
		t.Fatalf("editor cursor = %d,%d, want 1,0", viewer.editor.row, viewer.editor.col)
	}
}

func TestDiffViewerMousePressOutsideFocusedCommentMovesDiffCursor(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Text: "one", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Text: "two", Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
	}
	viewer := &diffViewer{
		rows: rows,
		reviewDrafts: []review.CommentDraft{{
			Path: "main.go",
			Line: 1,
			Side: review.SideRight,
			Body: "hello",
		}},
	}
	viewer.Layout(Tight(Size{Width: 40, Height: 10}))
	if !viewer.openReviewCommentEditorAtIndex(0) {
		t.Fatal("comment editor did not open")
	}

	cmd, err := viewer.HandleEvent(vaxis.Mouse{
		Button:    vaxis.MouseLeftButton,
		EventType: vaxis.EventPress,
		Row:       4,
		Col:       1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("mouse command = %v, want redraw", cmd)
	}
	if viewer.editor != nil {
		t.Fatal("editor still focused after outside click")
	}
	if viewer.cursor.Row != 1 {
		t.Fatalf("cursor row = %d, want 1", viewer.cursor.Row)
	}
}

func TestDiffViewerCommentEditorScrollsIntoView(t *testing.T) {
	rows := make([]diff.Row, 20)
	for i := range rows {
		rows[i] = diff.Row{
			Kind:   diff.RowAdd,
			Text:   "hello",
			Code:   "hello",
			Review: review.Anchor{Path: "main.go", Line: i + 1, Side: review.SideRight},
		}
	}
	viewer := &diffViewer{
		rows:   rows,
		cursor: selectionPoint{Row: 19, Col: 0},
	}
	viewer.Layout(Tight(Size{Width: 40, Height: 10}))
	openReviewCommentEditor(t, viewer)

	_, y, _, height, ok := viewer.commentEditorRect(40, 10)
	if !ok {
		t.Fatal("comment editor rect missing")
	}
	visible := viewer.visibleRowCapacity()
	if y+height > visible {
		t.Fatalf("editor bottom = %d, visible rows = %d", y+height, visible)
	}
	if viewer.scroll <= 11 {
		t.Fatalf("scroll = %d, want extra bottom padding beyond normal max", viewer.scroll)
	}
}

func TestDiffViewerCommentEditorGrowsToAllWrappedRows(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{
			Kind:   diff.RowAdd,
			Text:   "hello",
			Code:   "hello",
			Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight},
		}},
	}
	viewer.Layout(Tight(Size{Width: 40, Height: 20}))
	openReviewCommentEditor(t, viewer)
	viewer.editor.lines = []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}
	viewer.editor.row = len(viewer.editor.lines) - 1
	viewer.editor.col = 3
	viewer.syncCommentEditorScroll()

	layout, ok := viewer.commentEditorLayout(40, 20)
	if !ok {
		t.Fatal("comment editor layout missing")
	}
	if got, want := layout.visibleRows, 10; got != want {
		t.Fatalf("visible rows = %d, want %d", got, want)
	}
	if got, want := layout.boxHeight, 12; got != want {
		t.Fatalf("box height = %d, want %d", got, want)
	}
}

func TestDiffViewerCommentEditorWrapsLongLines(t *testing.T) {
	editor := &commentEditor{lines: []string{"hello world"}}
	wrapped := editor.wrappedLines(7)

	if len(wrapped) != 2 {
		t.Fatalf("wrapped line count = %d, want 2", len(wrapped))
	}
	if got, want := wrapped[0].text(editor.lines), "hello "; got != want {
		t.Fatalf("first wrapped line = %q, want %q", got, want)
	}
	if got, want := wrapped[1].text(editor.lines), "world"; got != want {
		t.Fatalf("second wrapped line = %q, want %q", got, want)
	}
}

func TestDiffViewerCommentEditorDoesNotWrapAtExactInputWidth(t *testing.T) {
	editor := &commentEditor{lines: []string{"abcdef"}}
	wrapped := editor.wrappedLines(6)

	if len(wrapped) != 1 {
		t.Fatalf("wrapped line count = %d, want 1", len(wrapped))
	}
	if got, want := wrapped[0].text(editor.lines), "abcdef"; got != want {
		t.Fatalf("wrapped line = %q, want %q", got, want)
	}
}

func TestDiffViewerCommentEditorEnterInsertsLine(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{
			Kind:   diff.RowAdd,
			Text:   "hello",
			Code:   "hello",
			Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight},
		}},
	}
	viewer.Layout(Tight(Size{Width: 40, Height: 10}))
	openReviewCommentEditor(t, viewer)

	if _, err := viewer.HandleEvent(vaxis.Key{Text: "first"}); err != nil {
		t.Fatal(err)
	}
	if _, err := viewer.HandleEvent(vaxis.Key{Keycode: vaxis.KeyEnter}); err != nil {
		t.Fatal(err)
	}
	if _, err := viewer.HandleEvent(vaxis.Key{Text: "second"}); err != nil {
		t.Fatal(err)
	}

	if got, want := viewer.editor.body(), "first\nsecond"; got != want {
		t.Fatalf("editor body = %q, want %q", got, want)
	}
}

func TestDiffViewerCommentEditorShiftBackspaceDeletes(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{
			Kind:   diff.RowAdd,
			Text:   "hello",
			Code:   "hello",
			Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight},
		}},
	}
	viewer.Layout(Tight(Size{Width: 40, Height: 10}))
	openReviewCommentEditor(t, viewer)

	if _, err := viewer.HandleEvent(vaxis.Key{Text: "ab"}); err != nil {
		t.Fatal(err)
	}
	if _, err := viewer.HandleEvent(vaxis.Key{Keycode: vaxis.KeyBackspace, Modifiers: vaxis.ModShift}); err != nil {
		t.Fatal(err)
	}

	if got, want := viewer.editor.body(), "a"; got != want {
		t.Fatalf("editor body = %q, want %q", got, want)
	}
}

func TestDiffViewerCommentEditorShiftEInsertsCapitalE(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{
			Kind:   diff.RowAdd,
			Text:   "hello",
			Code:   "hello",
			Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight},
		}},
	}
	viewer.Layout(Tight(Size{Width: 40, Height: 10}))
	openReviewCommentEditor(t, viewer)

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "E", Keycode: 'e', ShiftedCode: 'E', Modifiers: vaxis.ModShift})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want %v", cmd, CommandRedraw)
	}
	if viewer.mode != modeInsert {
		t.Fatalf("mode = %v, want insert", viewer.mode)
	}
	if got, want := viewer.editor.body(), "E"; got != want {
		t.Fatalf("editor body = %q, want %q", got, want)
	}
}

func TestDiffViewerCommentEditorEscapeLeavesInsertThenClosesAndSaves(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{
				Kind:   diff.RowAdd,
				Text:   "hello",
				Code:   "hello",
				Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight},
			},
			{
				Kind:   diff.RowAdd,
				Text:   "world",
				Code:   "world",
				Review: review.Anchor{Path: "main.go", Line: 13, Side: review.SideRight},
			},
		},
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))
	openReviewCommentEditor(t, viewer)
	if cmd, err := viewer.HandleEvent(vaxis.Key{Text: "saved"}); err != nil {
		t.Fatal(err)
	} else if cmd != CommandRedraw {
		t.Fatalf("text command = %v, want %v", cmd, CommandRedraw)
	}

	cmd, err := viewer.HandleEvent(vaxis.Key{Keycode: vaxis.KeyEsc})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want %v", cmd, CommandRedraw)
	}
	if viewer.editor == nil {
		t.Fatal("editor closed after first escape")
	}
	if viewer.mode != modeNormal {
		t.Fatalf("mode after first escape = %v, want normal", viewer.mode)
	}

	cmd, err = viewer.HandleEvent(vaxis.Key{Keycode: vaxis.KeyEsc})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("second escape command = %v, want %v", cmd, CommandRedraw)
	}
	if viewer.editor != nil {
		t.Fatal("editor still open after second escape")
	}
	if viewer.mode != modeNormal {
		t.Fatalf("mode = %v, want normal", viewer.mode)
	}
	if len(viewer.reviewDrafts) != 1 {
		t.Fatalf("draft count = %d, want 1", len(viewer.reviewDrafts))
	}
	if got, want := viewer.reviewDrafts[0].Body, "saved"; got != want {
		t.Fatalf("draft body = %q, want %q", got, want)
	}

	cmd, err = viewer.HandleEvent(vaxis.Key{Text: "j", Keycode: 'j'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("move command = %v, want %v", cmd, CommandRedraw)
	}
	if viewer.editor == nil || viewer.mode != modeNormal {
		t.Fatalf("editor/mode after moving into comment = %v/%v, want focused comment", viewer.editor != nil, viewer.mode)
	}
	cmd, err = viewer.HandleEvent(vaxis.Key{Text: "j", Keycode: 'j'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("move out command = %v, want %v", cmd, CommandRedraw)
	}
	if viewer.cursor.Row != 1 || viewer.editor != nil {
		t.Fatalf("cursor/editor = %d/%v, want row 1 and no editor", viewer.cursor.Row, viewer.editor != nil)
	}
}

func TestDiffViewerFocusAdjacentCommentStartsAtDirectionEdge(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{
				Kind:   diff.RowAdd,
				Text:   "hello",
				Code:   "hello",
				Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight},
			},
			{
				Kind:   diff.RowAdd,
				Text:   "world",
				Code:   "world",
				Review: review.Anchor{Path: "main.go", Line: 13, Side: review.SideRight},
			},
		},
		reviewDrafts: []review.CommentDraft{{
			Path: "main.go",
			Line: 12,
			Side: review.SideRight,
			Body: "first\nsecond",
		}},
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "j", Keycode: 'j'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw || viewer.editor == nil || viewer.mode != modeNormal {
		t.Fatalf("down command/editor/mode = %v/%v/%v, want focused comment", cmd, viewer.editor != nil, viewer.mode)
	}
	if got, want := (selectionPoint{Row: viewer.editor.row, Col: viewer.editor.col}), (selectionPoint{Row: 0, Col: 0}); got != want {
		t.Fatalf("down editor cursor = %+v, want %+v", got, want)
	}

	for i := 0; i < 2; i++ {
		cmd, err = viewer.HandleEvent(vaxis.Key{Text: "j", Keycode: 'j'})
		if err != nil {
			t.Fatal(err)
		}
	}
	if cmd != CommandRedraw || viewer.editor != nil || viewer.cursor.Row != 1 {
		t.Fatalf("leave down command/editor/cursor = %v/%v/%d, want row 1", cmd, viewer.editor != nil, viewer.cursor.Row)
	}

	cmd, err = viewer.HandleEvent(vaxis.Key{Text: "k", Keycode: 'k'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw || viewer.editor == nil || viewer.mode != modeNormal {
		t.Fatalf("up command/editor/mode = %v/%v/%v, want focused comment", cmd, viewer.editor != nil, viewer.mode)
	}
	if got, want := (selectionPoint{Row: viewer.editor.row, Col: viewer.editor.col}), (selectionPoint{Row: 1, Col: 0}); got != want {
		t.Fatalf("up editor cursor = %+v, want %+v", got, want)
	}

	cmd, err = viewer.HandleEvent(vaxis.Key{Text: "k", Keycode: 'k'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("move within comment command = %v, want %v", cmd, CommandRedraw)
	}
	if got, want := (selectionPoint{Row: viewer.editor.row, Col: viewer.editor.col}), (selectionPoint{Row: 0, Col: 0}); got != want {
		t.Fatalf("second up editor cursor = %+v, want %+v", got, want)
	}
}

func TestDiffViewerInsertReopensExistingComment(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{
			Kind:   diff.RowAdd,
			Text:   "hello",
			Code:   "hello",
			Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight},
		}},
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))
	openReviewCommentEditor(t, viewer)
	submitReviewComment(t, viewer, "first")

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "i", Keycode: 'i'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("insert command = %v, want %v", cmd, CommandRedraw)
	}
	if viewer.mode != modeInsert {
		t.Fatalf("mode = %v, want insert", viewer.mode)
	}
	if viewer.editor == nil {
		t.Fatal("editor is nil")
	}
	if got, want := viewer.editor.body(), "first"; got != want {
		t.Fatalf("editor body = %q, want %q", got, want)
	}
	if viewer.editor.draftIndex != 0 {
		t.Fatalf("draft index = %d, want 0", viewer.editor.draftIndex)
	}
	if _, err := viewer.HandleEvent(vaxis.Key{Text: " updated"}); err != nil {
		t.Fatal(err)
	}
	submitReviewComment(t, viewer, "")
	if len(viewer.reviewDrafts) != 1 {
		t.Fatalf("draft count = %d, want 1", len(viewer.reviewDrafts))
	}
	if got, want := viewer.reviewDrafts[0].Body, "first updated"; got != want {
		t.Fatalf("draft body = %q, want %q", got, want)
	}
}

func TestDiffViewerXDeletesNoteAtCursor(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{
			Kind:   diff.RowAdd,
			Text:   "hello",
			Code:   "hello",
			Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight},
		}},
		reviewDrafts: []review.CommentDraft{{
			Path: "main.go",
			Line: 12,
			Side: review.SideRight,
			Body: "comment",
		}},
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "x", Keycode: 'x'})
	if err != nil {
		t.Fatal(err)
	}

	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want redraw", cmd)
	}
	if len(viewer.reviewDrafts) != 0 {
		t.Fatalf("drafts = %+v, want none", viewer.reviewDrafts)
	}
	if !viewer.reviewDirty {
		t.Fatal("review not dirty after delete")
	}
	if got, want := viewer.statusMessage, "Note deleted."; got != want {
		t.Fatalf("status message = %q, want %q", got, want)
	}
}

func TestDiffViewerDDDeletesNoteAtCursor(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{
			Kind:   diff.RowAdd,
			Text:   "hello",
			Code:   "hello",
			Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight},
		}},
		reviewDrafts: []review.CommentDraft{{
			Path: "main.go",
			Line: 12,
			Side: review.SideRight,
			Body: "comment",
		}},
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	if cmd, err := viewer.HandleEvent(vaxis.Key{Text: "d", Keycode: 'd'}); err != nil {
		t.Fatal(err)
	} else if cmd != CommandNone {
		t.Fatalf("first d command = %v, want none", cmd)
	}
	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "d", Keycode: 'd'})
	if err != nil {
		t.Fatal(err)
	}

	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want redraw", cmd)
	}
	if len(viewer.reviewDrafts) != 0 {
		t.Fatalf("drafts = %+v, want none", viewer.reviewDrafts)
	}
}

func TestDiffViewerXDeletesNoteOverlappingSelection(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{
			Kind:   diff.RowAdd,
			Text:   "hello",
			Code:   "hello",
			Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight},
		}},
		reviewDrafts: []review.CommentDraft{{
			Path:        "main.go",
			Line:        12,
			Side:        review.SideRight,
			StartColumn: intPtr(2),
			EndColumn:   intPtr(4),
			Body:        "inline",
		}},
		mode: modeVisual,
		selection: textSelection{
			Active: true,
			Anchor: selectionPoint{Row: 0, Col: 0},
			Cursor: selectionPoint{Row: 0, Col: 0},
		},
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "x", Keycode: 'x'})
	if err != nil {
		t.Fatal(err)
	}

	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want redraw", cmd)
	}
	if len(viewer.reviewDrafts) != 0 {
		t.Fatalf("drafts = %+v, want none", viewer.reviewDrafts)
	}
	if viewer.mode != modeNormal || viewer.selection.Active {
		t.Fatalf("mode/selection = %v/%+v, want normal inactive", viewer.mode, viewer.selection)
	}
}

func TestDiffViewerXShowsMessageWithoutNote(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{
			Kind:   diff.RowAdd,
			Text:   "hello",
			Code:   "hello",
			Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight},
		}},
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "x", Keycode: 'x'})
	if err != nil {
		t.Fatal(err)
	}

	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want redraw", cmd)
	}
	if viewer.reviewDirty {
		t.Fatal("review dirty after no-op delete")
	}
	if got, want := viewer.statusMessage, "No note."; got != want {
		t.Fatalf("status message = %q, want %q", got, want)
	}
}

func TestDiffViewerCommandQQuits(t *testing.T) {
	viewer := newTestDiffViewer(1, 10)

	cmd := executeCommand(t, viewer, "q")
	if cmd != CommandQuit {
		t.Fatalf("command = %v, want quit", cmd)
	}
}

func TestDiffViewerCommandQWarnsWithUnsavedComments(t *testing.T) {
	viewer := newTestDiffViewer(1, 10)
	viewer.reviewDrafts = []review.CommentDraft{{Path: "main.go", Line: 1, Side: review.SideRight, Body: "comment"}}
	viewer.reviewDirty = true

	cmd := executeCommand(t, viewer, "q")
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want redraw", cmd)
	}
	if viewer.mode != modeNormal {
		t.Fatalf("mode = %v, want normal", viewer.mode)
	}
	if viewer.statusMessage == "" {
		t.Fatal("status message is empty")
	}
}

func TestDiffViewerCommandWSavesComments(t *testing.T) {
	viewer := newTestDiffViewer(1, 10)
	viewer.reviewDrafts = []review.CommentDraft{{Path: "main.go", Line: 1, Side: review.SideRight, Body: "comment"}}
	viewer.reviewDirty = true

	cmd := executeCommand(t, viewer, "w")
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want redraw", cmd)
	}
	if viewer.reviewDirty {
		t.Fatal("review dirty after :w")
	}
	if got, want := viewer.statusMessage, "Comments saved."; got != want {
		t.Fatalf("status message = %q, want %q", got, want)
	}
}

func TestDiffViewerStatusMessageExpires(t *testing.T) {
	viewer := &diffViewer{
		statusMessage:      "Note deleted.",
		statusMessageUntil: time.Unix(1, 0).Add(statusMessageTimeout),
	}

	if viewer.clearExpiredStatusMessage(time.Unix(1, 0).Add(statusMessageTimeout - time.Millisecond)) {
		t.Fatal("status message expired early")
	}
	if viewer.statusMessage == "" {
		t.Fatal("status message cleared early")
	}
	if !viewer.clearExpiredStatusMessage(time.Unix(1, 0).Add(statusMessageTimeout)) {
		t.Fatal("status message did not expire")
	}
	if viewer.statusMessage != "" || !viewer.statusMessageUntil.IsZero() {
		t.Fatalf("status message = %q until %v, want cleared", viewer.statusMessage, viewer.statusMessageUntil)
	}
}

func TestDiffViewerStatusMessageRequestsTimedRedraw(t *testing.T) {
	viewer := &diffViewer{}
	viewer.setStatusMessage("Note deleted.")

	duration, ok := viewer.RedrawAfter()
	if !ok {
		t.Fatal("timed redraw not requested")
	}
	if duration <= 0 || duration > statusMessageTimeout {
		t.Fatalf("redraw duration = %v, want within %v", duration, statusMessageTimeout)
	}
}

func TestDiffViewerCommandWWritesCommentsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".comview", "comments.json")
	viewer := newTestDiffViewer(1, 10)
	viewer.reviewFile = path
	viewer.reviewDrafts = []review.CommentDraft{{Path: "main.go", Line: 1, Side: review.SideRight, Body: "comment"}}
	viewer.reviewDirty = true

	cmd := executeCommand(t, viewer, "w")
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want redraw", cmd)
	}
	file, err := review.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Comments) != 1 || file.Comments[0].Body != "comment" {
		t.Fatalf("comments file = %+v", file)
	}
}

func TestDiffViewerCommandWQWritesAndQuits(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".comview", "comments.json")
	viewer := newTestDiffViewer(1, 10)
	viewer.reviewFile = path
	viewer.reviewDrafts = []review.CommentDraft{{Path: "main.go", Line: 1, Side: review.SideRight, Body: "comment"}}
	viewer.reviewDirty = true

	cmd := executeCommand(t, viewer, "wq")
	if cmd != CommandQuit {
		t.Fatalf("command = %v, want quit", cmd)
	}
	if viewer.reviewDirty {
		t.Fatal("review dirty after :wq")
	}
	file, err := review.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Comments) != 1 || file.Comments[0].Body != "comment" {
		t.Fatalf("comments file = %+v", file)
	}
}

func TestDiffViewerCommandQBangQuitsWithUnsavedComments(t *testing.T) {
	viewer := newTestDiffViewer(1, 10)
	viewer.reviewDirty = true

	cmd := executeCommand(t, viewer, "q!")
	if cmd != CommandQuit {
		t.Fatalf("command = %v, want quit", cmd)
	}
}

func TestDiffViewerCommandCursorUsesStatusBar(t *testing.T) {
	viewer := &diffViewer{
		mode:        modeCommand,
		commandLine: "w",
	}

	col, row, ok := viewer.commandCursorPositionForSize(20, 10)
	if !ok {
		t.Fatal("command cursor position missing")
	}
	if col != 2 || row != 9 {
		t.Fatalf("command cursor = %d,%d, want 2,9", col, row)
	}
}

func TestDiffViewerSlashSearchMovesToMatch(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "alpha"},
		{Kind: diff.RowContext, Gutter: "2 2   ", Code: "needle"},
	}
	for i := range rows {
		rows[i].Text = rows[i].Gutter + rows[i].Code
	}
	viewer := &diffViewer{rows: rows}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "/", Keycode: '/'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw || viewer.mode != modeSearch {
		t.Fatalf("slash command = %v mode=%v, want redraw search", cmd, viewer.mode)
	}
	if cmd, err = viewer.HandleEvent(vaxis.Key{Text: "needle"}); err != nil {
		t.Fatal(err)
	} else if cmd != CommandRedraw {
		t.Fatalf("query command = %v, want redraw", cmd)
	}
	if cmd, err = viewer.HandleEvent(vaxis.Key{Keycode: vaxis.KeyEnter}); err != nil {
		t.Fatal(err)
	} else if cmd != CommandRedraw {
		t.Fatalf("enter command = %v, want redraw", cmd)
	}

	if viewer.mode != modeNormal {
		t.Fatalf("mode = %v, want normal", viewer.mode)
	}
	if got, want := viewer.cursor.Row, 1; got != want {
		t.Fatalf("cursor row = %d, want %d", got, want)
	}
	if got, want := viewer.cursor.Col, testCodeOffset(rows[1]); got != want {
		t.Fatalf("cursor col = %d, want %d", got, want)
	}
}

func TestDiffViewerSearchNextPrevious(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowContext, Text: "one needle"},
			{Kind: diff.RowContext, Text: "two needle"},
		},
		searchQuery: "needle",
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))
	viewer.updateSearchMatches()

	if !viewer.moveSearchMatch(1) {
		t.Fatal("next search failed")
	}
	if got, want := viewer.cursor.Row, 0; got != want {
		t.Fatalf("cursor row = %d, want %d", got, want)
	}
	if !viewer.moveSearchMatch(1) {
		t.Fatal("second next search failed")
	}
	if got, want := viewer.cursor.Row, 1; got != want {
		t.Fatalf("cursor row = %d, want %d", got, want)
	}
	if !viewer.moveSearchMatch(-1) {
		t.Fatal("previous search failed")
	}
	if got, want := viewer.cursor.Row, 0; got != want {
		t.Fatalf("cursor row = %d, want %d", got, want)
	}
}

func TestDiffViewerEscapeClearsSearch(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{Kind: diff.RowContext, Text: "needle"}},
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))
	viewer.searchQuery = "needle"
	viewer.updateSearchMatches()
	viewer.searchIndex = 0

	cmd, err := viewer.HandleEvent(vaxis.Key{Keycode: vaxis.KeyEsc})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want redraw", cmd)
	}
	if viewer.searchQuery != "" || len(viewer.searchMatches) != 0 || viewer.searchIndex != -1 {
		t.Fatalf("search state = query:%q matches:%+v index:%d", viewer.searchQuery, viewer.searchMatches, viewer.searchIndex)
	}
}

func TestDiffViewerSearchSegmentsHighlightMatches(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{Kind: diff.RowContext, Text: "one needle"}},
		searchMatches: []searchMatch{{
			Row:   0,
			Start: 4,
			End:   10,
		}},
	}
	viewer.ensureColorScheme()

	segments := viewer.searchSegments(0, viewer.rows[0], []vaxis.Segment{{Text: "one needle", Style: viewer.baseStyle()}})

	if len(segments) != 2 {
		t.Fatalf("segments = %+v, want 2", segments)
	}
	if got, want := segments[1].Text, "needle"; got != want {
		t.Fatalf("highlight text = %q, want %q", got, want)
	}
	if segments[1].Style.Background != viewer.scheme.Yellow {
		t.Fatalf("highlight style = %+v", segments[1].Style)
	}
}

func TestDiffViewerPlainQDoesNotQuit(t *testing.T) {
	viewer := newTestDiffViewer(1, 10)

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "q", Keycode: 'q'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd == CommandQuit {
		t.Fatal("plain q quit, want no quit")
	}
}

func TestDiffViewerVimNavigationKeys(t *testing.T) {
	tests := []struct {
		name       string
		start      int
		cursor     int
		key        vaxis.Key
		wantScroll int
		wantCursor int
		wantCmd    Command
		pending    string
		wantPend   string
	}{
		{
			name:       "G moves cursor to bottom",
			key:        vaxis.Key{Text: "G", Keycode: 'G'},
			wantScroll: 91,
			wantCursor: 99,
			wantCmd:    CommandRedraw,
		},
		{
			name:       "End moves cursor to bottom",
			key:        vaxis.Key{Keycode: vaxis.KeyEnd},
			wantScroll: 91,
			wantCursor: 99,
			wantCmd:    CommandRedraw,
		},
		{
			name:       "Home moves cursor to top",
			start:      40,
			cursor:     40,
			key:        vaxis.Key{Keycode: vaxis.KeyHome},
			wantScroll: 0,
			wantCursor: 0,
			wantCmd:    CommandRedraw,
		},
		{
			name:       "j moves cursor down",
			start:      10,
			cursor:     10,
			key:        vaxis.Key{Text: "j", Keycode: 'j'},
			wantScroll: 10,
			wantCursor: 11,
			wantCmd:    CommandRedraw,
		},
		{
			name:       "Down moves cursor down",
			start:      10,
			cursor:     10,
			key:        vaxis.Key{Keycode: vaxis.KeyDown},
			wantScroll: 10,
			wantCursor: 11,
			wantCmd:    CommandRedraw,
		},
		{
			name:       "k moves cursor up",
			start:      10,
			cursor:     10,
			key:        vaxis.Key{Text: "k", Keycode: 'k'},
			wantScroll: 9,
			wantCursor: 9,
			wantCmd:    CommandRedraw,
		},
		{
			name:       "Up moves cursor up",
			start:      10,
			cursor:     10,
			key:        vaxis.Key{Keycode: vaxis.KeyUp},
			wantScroll: 9,
			wantCursor: 9,
			wantCmd:    CommandRedraw,
		},
		{
			name:       "Ctrl+d moves cursor down half page",
			start:      10,
			cursor:     10,
			key:        vaxis.Key{Keycode: 'd', Modifiers: vaxis.ModCtrl},
			wantScroll: 10,
			wantCursor: 14,
			wantCmd:    CommandRedraw,
		},
		{
			name:       "Page Down moves cursor down half page",
			start:      10,
			cursor:     10,
			key:        vaxis.Key{Keycode: vaxis.KeyPgDown},
			wantScroll: 10,
			wantCursor: 14,
			wantCmd:    CommandRedraw,
		},
		{
			name:       "Ctrl+u moves cursor up half page",
			start:      10,
			cursor:     10,
			key:        vaxis.Key{Keycode: 'u', Modifiers: vaxis.ModCtrl},
			wantScroll: 6,
			wantCursor: 6,
			wantCmd:    CommandRedraw,
		},
		{
			name:       "Page Up moves cursor up half page",
			start:      10,
			cursor:     10,
			key:        vaxis.Key{Keycode: vaxis.KeyPgUp},
			wantScroll: 6,
			wantCursor: 6,
			wantCmd:    CommandRedraw,
		},
		{
			name:       "g waits for second g",
			start:      10,
			cursor:     10,
			key:        vaxis.Key{Text: "g", Keycode: 'g'},
			wantScroll: 10,
			wantCursor: 10,
			wantCmd:    CommandNone,
			wantPend:   "g",
		},
		{
			name:       "second g moves cursor to top",
			start:      10,
			cursor:     10,
			key:        vaxis.Key{Text: "g", Keycode: 'g'},
			wantScroll: 0,
			wantCursor: 0,
			wantCmd:    CommandRedraw,
			pending:    "g",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viewer := newTestDiffViewer(100, 10)
			viewer.scroll = tt.start
			viewer.cursor.Row = tt.cursor
			viewer.cursorGoal = viewer.cursor.Col
			if tt.pending != "" {
				viewer.keys.Set(tt.pending, time.Now())
			}

			cmd, err := viewer.HandleEvent(tt.key)
			if err != nil {
				t.Fatal(err)
			}
			if cmd != tt.wantCmd {
				t.Fatalf("command = %v, want %v", cmd, tt.wantCmd)
			}
			if viewer.scroll != tt.wantScroll {
				t.Fatalf("scroll = %d, want %d", viewer.scroll, tt.wantScroll)
			}
			if viewer.cursor.Row != tt.wantCursor {
				t.Fatalf("cursor row = %d, want %d", viewer.cursor.Row, tt.wantCursor)
			}
			if viewer.keys.Pending() != tt.wantPend {
				t.Fatalf("pending keys = %q, want %q", viewer.keys.Pending(), tt.wantPend)
			}
		})
	}
}

func TestDiffViewerIgnoresKeyReleaseEvents(t *testing.T) {
	viewer := newTestDiffViewer(100, 10)
	viewer.scroll = 10

	cmd, err := viewer.HandleEvent(vaxis.Key{
		Text:      "j",
		Keycode:   'j',
		EventType: vaxis.EventRelease,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandNone {
		t.Fatalf("command = %v, want %v", cmd, CommandNone)
	}
	if viewer.scroll != 10 {
		t.Fatalf("scroll = %d, want 10", viewer.scroll)
	}
}

func TestDiffViewerScrollsWhenReviewDraftConsumesCursorViewport(t *testing.T) {
	rows := make([]diff.Row, 8)
	for i := range rows {
		rows[i] = diff.Row{
			Kind:   diff.RowAdd,
			Text:   "line",
			Code:   "line",
			Review: review.Anchor{Path: "main.go", Line: i + 1, Side: review.SideRight},
		}
	}
	viewer := &diffViewer{
		rows: rows,
		reviewDrafts: []review.CommentDraft{{
			Path: "main.go",
			Line: 1,
			Side: review.SideRight,
			Body: "comment",
		}},
		cursor: selectionPoint{Row: 1, Col: testCodeOffset(rows[1])},
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 6}))
	viewer.cursorGoal = viewer.cursor.Col

	viewer.moveCursorRows(1)

	if got, want := viewer.cursor.Row, 2; got != want {
		t.Fatalf("cursor row = %d, want %d", got, want)
	}
	if viewer.scroll == 0 {
		t.Fatal("scroll = 0, want cursor scrolled before leaving viewport")
	}
	if _, _, ok := viewer.cursorScreenPositionForSize(80, 6); !ok {
		t.Fatal("cursor is not visible after moving past review draft")
	}
}

func TestDiffViewerHorizontalNavigationKeys(t *testing.T) {
	tests := []struct {
		name       string
		start      int
		cursor     int
		key        vaxis.Key
		wantScroll int
		wantCursor int
	}{
		{
			name:       "l moves cursor right and scrolls at edge",
			cursor:     79,
			key:        vaxis.Key{Text: "l", Keycode: 'l'},
			wantScroll: 1,
			wantCursor: 80,
		},
		{
			name:       "Right moves cursor right and scrolls at edge",
			cursor:     79,
			key:        vaxis.Key{Keycode: vaxis.KeyRight},
			wantScroll: 1,
			wantCursor: 80,
		},
		{
			name:       "h moves cursor left and scrolls at edge",
			start:      5,
			cursor:     5,
			key:        vaxis.Key{Text: "h", Keycode: 'h'},
			wantScroll: 4,
			wantCursor: 4,
		},
		{
			name:       "Left moves cursor left and scrolls at edge",
			start:      5,
			cursor:     5,
			key:        vaxis.Key{Keycode: vaxis.KeyLeft},
			wantScroll: 4,
			wantCursor: 4,
		},
		{
			name:       "left clamps at zero",
			start:      0,
			cursor:     0,
			key:        vaxis.Key{Keycode: vaxis.KeyLeft},
			wantScroll: 0,
			wantCursor: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viewer := newTestDiffViewer(3, 10)
			viewer.rows[0].Kind = diff.RowContext
			viewer.rows[0].Code = strings.Repeat("x", 120)
			viewer.rows[0].Text = viewer.rows[0].Code
			viewer.xScroll = tt.start
			viewer.cursor.Col = tt.cursor
			viewer.cursorGoal = viewer.cursor.Col

			cmd, err := viewer.HandleEvent(tt.key)
			if err != nil {
				t.Fatal(err)
			}
			if cmd != CommandRedraw {
				t.Fatalf("command = %v, want %v", cmd, CommandRedraw)
			}
			if viewer.xScroll != tt.wantScroll {
				t.Fatalf("xScroll = %d, want %d", viewer.xScroll, tt.wantScroll)
			}
			if viewer.cursor.Col != tt.wantCursor {
				t.Fatalf("cursor col = %d, want %d", viewer.cursor.Col, tt.wantCursor)
			}
		})
	}
}

func TestDiffViewerLayoutPreservesHorizontalScroll(t *testing.T) {
	row := diff.Row{
		Kind: diff.RowContext,
		Code: strings.Repeat("x", 120),
	}
	row.Text = row.Code
	viewer := &diffViewer{
		rows:    []diff.Row{row},
		cursor:  selectionPoint{Row: 0, Col: 0},
		xScroll: 10,
	}

	viewer.Layout(Tight(Size{Width: 20, Height: 5}))

	if viewer.xScroll != 10 {
		t.Fatalf("xScroll = %d, want preserved 10", viewer.xScroll)
	}
}

func TestDiffViewerHidesCursorOutsideHorizontalViewport(t *testing.T) {
	row := diff.Row{
		Kind: diff.RowContext,
		Code: strings.Repeat("x", 120),
	}
	row.Text = row.Code
	viewer := &diffViewer{
		rows:    []diff.Row{row},
		cursor:  selectionPoint{Row: 0, Col: 0},
		xScroll: 10,
	}
	viewer.Layout(Tight(Size{Width: 20, Height: 5}))

	_, _, ok := viewer.cursorScreenPositionForSize(20, 5)
	if ok {
		t.Fatal("cursor position found, want hidden when cursor is left of viewport")
	}
}

func TestDiffViewerVerticalMovementPreservesHorizontalScroll(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Code: strings.Repeat("a", 120), Text: strings.Repeat("a", 120)},
		{Kind: diff.RowContext, Code: strings.Repeat("b", 120), Text: strings.Repeat("b", 120)},
	}
	viewer := &diffViewer{
		rows:       rows,
		cursor:     selectionPoint{Row: 0, Col: 0},
		cursorGoal: 0,
		xScroll:    10,
	}
	viewer.Layout(Tight(Size{Width: 20, Height: 5}))
	viewer.xScroll = 10

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "j", Keycode: 'j'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want redraw", cmd)
	}
	if viewer.cursor.Row != 1 {
		t.Fatalf("cursor row = %d, want 1", viewer.cursor.Row)
	}
	if viewer.xScroll != 10 {
		t.Fatalf("xScroll = %d, want preserved 10", viewer.xScroll)
	}
}

func TestDiffViewerLineBoundaryKeys(t *testing.T) {
	row := diff.Row{
		Kind:   diff.RowAdd,
		Gutter: "    1 + ",
		Code:   "abcdef",
	}
	row.Text = row.Gutter + row.Code
	codeOffset := testCodeOffset(row)

	tests := []struct {
		name       string
		key        vaxis.Key
		wantCursor int
	}{
		{
			name:       "zero moves to code start",
			key:        vaxis.Key{Text: "0", Keycode: '0'},
			wantCursor: codeOffset,
		},
		{
			name:       "dollar moves to code end",
			key:        vaxis.Key{Text: "$", Keycode: '$'},
			wantCursor: codeOffset + 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viewer := &diffViewer{
				rows:   []diff.Row{row},
				cursor: selectionPoint{Row: 0, Col: codeOffset + 2},
			}
			viewer.Layout(Tight(Size{Width: 80, Height: 10}))

			cmd, err := viewer.HandleEvent(tt.key)
			if err != nil {
				t.Fatal(err)
			}

			if cmd != CommandRedraw {
				t.Fatalf("command = %v, want redraw", cmd)
			}
			if viewer.cursor.Col != tt.wantCursor {
				t.Fatalf("cursor col = %d, want %d", viewer.cursor.Col, tt.wantCursor)
			}
		})
	}
}

func TestDiffViewerSideBySideHorizontalNavigationUsesPaneWidth(t *testing.T) {
	row := diff.Row{
		Kind:   diff.RowAdd,
		Gutter: "    1 + ",
		Code:   strings.Repeat("x", 60),
	}
	row.Text = row.Gutter + row.Code
	viewer := &diffViewer{
		rows:       []diff.Row{row},
		cursor:     selectionPoint{Row: 0, Col: testCodeOffset(row) + 40},
		layoutMode: layoutSideBySide,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))
	viewer.cursorGoal = viewer.cursor.Col

	viewer.ensureCursorColumnVisible()

	if viewer.xScroll == 0 {
		t.Fatal("xScroll = 0, want split pane horizontal scroll")
	}
	if got, want := viewer.xScroll, 5; got != want {
		t.Fatalf("xScroll = %d, want %d", got, want)
	}
}

func TestDiffViewerSideBySideHorizontalScrollbarUsesPaneWidth(t *testing.T) {
	row := diff.Row{
		Kind:   diff.RowAdd,
		Gutter: "    1 + ",
		Code:   strings.Repeat("x", 60),
	}
	row.Text = row.Gutter + row.Code
	viewer := &diffViewer{
		rows:       []diff.Row{row},
		layoutMode: layoutSideBySide,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	bar := viewer.horizontalScrollbar(80, 10)

	if !bar.Visible {
		t.Fatal("horizontal scrollbar hidden, want visible for split pane overflow")
	}
	if got, want := bar.Length, 80; got != want {
		t.Fatalf("bar length = %d, want full width track %d", got, want)
	}
	if got, want := bar.Size, 50; got != want {
		t.Fatalf("bar size = %d, want split viewport thumb %d", got, want)
	}
}

func TestDiffViewerCursorDoesNotEnterGutter(t *testing.T) {
	row := diff.Row{
		Kind:   diff.RowAdd,
		Gutter: "12 34 + ",
		Code:   "abcdef",
	}
	row.Text = row.Gutter + row.Code
	codeOffset := testCodeOffset(row)

	t.Run("left clamps at code start", func(t *testing.T) {
		viewer := &diffViewer{rows: []diff.Row{row}}
		viewer.Layout(Tight(Size{Width: 80, Height: 10}))
		viewer.cursor = selectionPoint{Row: 0, Col: codeOffset}
		viewer.cursorGoal = viewer.cursor.Col

		cmd, err := viewer.HandleEvent(vaxis.Key{Keycode: vaxis.KeyLeft})
		if err != nil {
			t.Fatal(err)
		}
		if cmd != CommandRedraw {
			t.Fatalf("command = %v, want %v", cmd, CommandRedraw)
		}
		if viewer.cursor.Col != codeOffset {
			t.Fatalf("cursor col = %d, want code offset %d", viewer.cursor.Col, codeOffset)
		}
	})

	t.Run("mouse press in gutter moves cursor to code start", func(t *testing.T) {
		viewer := &diffViewer{rows: []diff.Row{row}}
		viewer.Layout(Tight(Size{Width: 80, Height: 10}))

		cmd, err := viewer.HandleEvent(vaxis.Mouse{
			Button:    vaxis.MouseLeftButton,
			EventType: vaxis.EventPress,
			Row:       0,
			Col:       0,
		})
		if err != nil {
			t.Fatal(err)
		}
		if cmd != CommandRedraw {
			t.Fatalf("command = %v, want %v", cmd, CommandRedraw)
		}
		if viewer.cursor.Col != codeOffset {
			t.Fatalf("cursor col = %d, want code offset %d", viewer.cursor.Col, codeOffset)
		}
	})

	t.Run("empty code clamps to virtual code cell", func(t *testing.T) {
		empty := row
		empty.Code = ""
		empty.Text = empty.Gutter
		viewer := &diffViewer{rows: []diff.Row{empty}}
		viewer.Layout(Tight(Size{Width: 80, Height: 10}))
		viewer.cursor = selectionPoint{Row: 0, Col: 0}
		viewer.clampCursor()

		if viewer.cursor.Col != codeOffset {
			t.Fatalf("cursor col = %d, want code offset %d", viewer.cursor.Col, codeOffset)
		}
	})
}

func TestDiffViewerPendingGExpires(t *testing.T) {
	viewer := newTestDiffViewer(100, 10)
	viewer.scroll = 10
	viewer.keys.Set("g", time.Now().Add(-pendingKeyTimeout-time.Second))

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "g", Keycode: 'g'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandNone {
		t.Fatalf("command = %v, want %v", cmd, CommandNone)
	}
	if viewer.scroll != 10 {
		t.Fatalf("scroll = %d, want 10", viewer.scroll)
	}
	if viewer.keys.Pending() != "g" {
		t.Fatalf("pending keys = %q, want %q", viewer.keys.Pending(), "g")
	}
}

func TestDiffViewerCtrlDDoesNotQuit(t *testing.T) {
	viewer := newTestDiffViewer(100, 10)

	cmd, err := viewer.HandleEvent(vaxis.Key{Keycode: 'd', Modifiers: vaxis.ModCtrl})
	if err != nil {
		t.Fatal(err)
	}
	if cmd == CommandQuit {
		t.Fatal("Ctrl+d quit, want scroll command")
	}
}

func TestDiffViewerMouseWheelScrolls(t *testing.T) {
	tests := []struct {
		name       string
		start      int
		cursor     int
		mouse      vaxis.Mouse
		want       int
		wantCursor int
		keys       string
		noKeys     bool
	}{
		{
			name:       "wheel down scrolls down",
			start:      10,
			cursor:     14,
			mouse:      vaxis.Mouse{Button: vaxis.MouseWheelDown},
			want:       11,
			wantCursor: 14,
			keys:       "g",
		},
		{
			name:       "wheel up scrolls up",
			start:      10,
			cursor:     14,
			mouse:      vaxis.Mouse{Button: vaxis.MouseWheelUp},
			want:       9,
			wantCursor: 14,
			keys:       "g",
		},
		{
			name:       "wheel up clamps at top",
			start:      1,
			cursor:     1,
			mouse:      vaxis.Mouse{Button: vaxis.MouseWheelUp},
			want:       0,
			wantCursor: 1,
		},
		{
			name:       "wheel down moves cursor only to keep it visible",
			start:      10,
			cursor:     10,
			mouse:      vaxis.Mouse{Button: vaxis.MouseWheelDown},
			want:       11,
			wantCursor: 11,
		},
		{
			name:       "wheel up moves cursor only to keep it visible",
			start:      10,
			cursor:     18,
			mouse:      vaxis.Mouse{Button: vaxis.MouseWheelUp},
			want:       9,
			wantCursor: 17,
		},
		{
			name:       "non-wheel mouse does not scroll",
			start:      10,
			cursor:     14,
			mouse:      vaxis.Mouse{Button: vaxis.MouseNoButton, EventType: vaxis.EventMotion},
			want:       10,
			wantCursor: 14,
			keys:       "g",
			noKeys:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viewer := newTestDiffViewer(100, 10)
			viewer.scroll = tt.start
			viewer.cursor.Row = tt.cursor
			if tt.keys != "" {
				viewer.keys.Set(tt.keys, time.Now())
			}

			cmd, err := viewer.HandleEvent(tt.mouse)
			if err != nil {
				t.Fatal(err)
			}
			if tt.mouse.Button == vaxis.MouseNoButton && cmd != CommandNone {
				t.Fatalf("command = %v, want %v", cmd, CommandNone)
			}
			if tt.mouse.Button != vaxis.MouseNoButton && cmd != CommandRedraw {
				t.Fatalf("command = %v, want %v", cmd, CommandRedraw)
			}
			if viewer.scroll != tt.want {
				t.Fatalf("scroll = %d, want %d", viewer.scroll, tt.want)
			}
			if viewer.cursor.Row != tt.wantCursor {
				t.Fatalf("cursor row = %d, want %d", viewer.cursor.Row, tt.wantCursor)
			}
			if tt.noKeys {
				if viewer.keys.Pending() != tt.keys {
					t.Fatalf("pending keys = %q, want %q", viewer.keys.Pending(), tt.keys)
				}
				return
			}
			if viewer.keys.Pending() != "" {
				t.Fatalf("pending keys = %q, want empty", viewer.keys.Pending())
			}
		})
	}
}

func TestDiffViewerHorizontalMouseWheelScrolls(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{
			Kind: diff.RowContext,
			Text: strings.Repeat("x", 80),
		}},
	}
	viewer.Layout(Tight(Size{Width: 20, Height: 5}))

	cmd, err := viewer.HandleEvent(vaxis.Mouse{Button: mouseWheelRight})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("right wheel command = %v, want redraw", cmd)
	}
	if viewer.xScroll != 1 {
		t.Fatalf("xScroll after right wheel = %d, want 1", viewer.xScroll)
	}

	cmd, err = viewer.HandleEvent(vaxis.Mouse{Button: mouseWheelLeft})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("left wheel command = %v, want redraw", cmd)
	}
	if viewer.xScroll != 0 {
		t.Fatalf("xScroll after left wheel = %d, want 0", viewer.xScroll)
	}
}

func TestDiffViewerMouseWheelAxesApplyIndependently(t *testing.T) {
	var rows []diff.Row
	for range 20 {
		rows = append(rows, diff.Row{
			Kind: diff.RowContext,
			Text: strings.Repeat("x", 80),
		})
	}
	viewer := &diffViewer{rows: rows}
	viewer.Layout(Tight(Size{Width: 20, Height: 5}))

	cmd, err := viewer.HandleEvent(vaxis.Mouse{Button: vaxis.MouseWheelDown})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("vertical command = %v, want redraw", cmd)
	}
	if viewer.scroll != 1 {
		t.Fatalf("scroll = %d, want 1", viewer.scroll)
	}

	cmd, err = viewer.HandleEvent(vaxis.Mouse{Button: mouseWheelRight})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("horizontal command = %v, want redraw", cmd)
	}
	if viewer.scroll != 1 {
		t.Fatalf("scroll after horizontal wheel = %d, want preserved 1", viewer.scroll)
	}
	if viewer.xScroll != 1 {
		t.Fatalf("xScroll = %d, want 1", viewer.xScroll)
	}

	cmd, err = viewer.HandleEvent(vaxis.Mouse{Button: vaxis.MouseWheelUp})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("vertical command after horizontal wheel = %v, want redraw", cmd)
	}
	if viewer.scroll != 0 {
		t.Fatalf("scroll after up wheel = %d, want 0", viewer.scroll)
	}
	if viewer.xScroll != 1 {
		t.Fatalf("xScroll after vertical wheel = %d, want preserved 1", viewer.xScroll)
	}
}

func TestDiffViewerMouseWheelExtendsDraggingSelection(t *testing.T) {
	viewer := newTestDiffViewer(100, 10)
	for i := range viewer.rows {
		viewer.rows[i].Kind = diff.RowContext
		viewer.rows[i].Code = "abcdef"
		viewer.rows[i].Text = "abcdef"
	}
	viewer.scroll = 10
	viewer.selection = textSelection{
		Active:   true,
		Dragging: true,
		Anchor: selectionPoint{
			Row: 10,
			Col: 0,
		},
		Cursor: selectionPoint{
			Row: 10,
			Col: 0,
		},
	}

	cmd, err := viewer.HandleEvent(vaxis.Mouse{
		Button: vaxis.MouseWheelDown,
		Row:    5,
		Col:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want %v", cmd, CommandRedraw)
	}
	if viewer.scroll != 11 {
		t.Fatalf("scroll = %d, want 11", viewer.scroll)
	}
	if got := viewer.selection.Cursor; got != (selectionPoint{Row: 16, Col: 2}) {
		t.Fatalf("cursor = %+v, want row 16 col 2", got)
	}
}

func TestDiffViewerMouseWheelDoesNotExtendFinishedSelection(t *testing.T) {
	viewer := newTestDiffViewer(100, 10)
	for i := range viewer.rows {
		viewer.rows[i].Code = "abcdef"
		viewer.rows[i].Text = "abcdef"
	}
	viewer.scroll = 10
	viewer.selection = textSelection{
		Active: true,
		Anchor: selectionPoint{
			Row: 10,
			Col: 0,
		},
		Cursor: selectionPoint{
			Row: 10,
			Col: 0,
		},
	}

	_, err := viewer.HandleEvent(vaxis.Mouse{
		Button: vaxis.MouseWheelDown,
		Row:    5,
		Col:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := viewer.selection.Cursor; got != (selectionPoint{Row: 10, Col: 0}) {
		t.Fatalf("cursor = %+v, want unchanged", got)
	}
}

func TestDiffViewerMouseSelectionCopiesText(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "abcde"},
		{Kind: diff.RowContext, Gutter: "2 2   ", Code: "fghij"},
	}
	for i := range rows {
		rows[i].Text = rows[i].Gutter + rows[i].Code
	}
	viewer := &diffViewer{
		rows: rows,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))
	codeOffset := testCodeOffset(rows[0])

	cmd, err := viewer.HandleEvent(vaxis.Mouse{
		Button:    vaxis.MouseLeftButton,
		EventType: vaxis.EventPress,
		Row:       0,
		Col:       codeOffset + 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("press command = %v, want %v", cmd, CommandRedraw)
	}
	if got := viewer.cursor; got != (selectionPoint{Row: 0, Col: codeOffset + 1}) {
		t.Fatalf("cursor after press = %+v, want row 0 col %d", got, codeOffset+1)
	}

	cmd, err = viewer.HandleEvent(vaxis.Mouse{
		Button:    vaxis.MouseLeftButton,
		EventType: vaxis.EventMotion,
		Row:       1,
		Col:       codeOffset + 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("motion command = %v, want %v", cmd, CommandRedraw)
	}
	if got := viewer.cursor; got != (selectionPoint{Row: 1, Col: codeOffset + 2}) {
		t.Fatalf("cursor after motion = %+v, want row 1 col %d", got, codeOffset+2)
	}

	cmd, err = viewer.HandleEvent(vaxis.Mouse{
		Button:    vaxis.MouseLeftButton,
		EventType: vaxis.EventRelease,
		Row:       1,
		Col:       codeOffset + 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("release command = %v, want %v", cmd, CommandRedraw)
	}

	if got, want := viewer.ClipboardText(), "bcde\nfgh"; got != want {
		t.Fatalf("clipboard text = %q, want %q", got, want)
	}
}

func TestDiffViewerMouseSelectionCopiesOnlyCode(t *testing.T) {
	rows := []diff.Row{
		{
			Kind:   diff.RowAdd,
			Gutter: "1 1 + ",
			Code:   "hello",
		},
		{
			Kind:   diff.RowDelete,
			Gutter: "2 2 - ",
			Code:   "world",
		},
	}
	for i := range rows {
		rows[i].Text = rows[i].Gutter + rows[i].Code
	}
	viewer := &diffViewer{rows: rows}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))
	codeOffset := testCodeOffset(rows[0])

	cmd, err := viewer.HandleEvent(vaxis.Mouse{
		Button:    vaxis.MouseLeftButton,
		EventType: vaxis.EventPress,
		Row:       0,
		Col:       0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("press command = %v, want %v", cmd, CommandRedraw)
	}

	cmd, err = viewer.HandleEvent(vaxis.Mouse{
		Button:    vaxis.MouseLeftButton,
		EventType: vaxis.EventMotion,
		Row:       1,
		Col:       codeOffset + 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("motion command = %v, want %v", cmd, CommandRedraw)
	}

	cmd, err = viewer.HandleEvent(vaxis.Mouse{
		Button:    vaxis.MouseLeftButton,
		EventType: vaxis.EventRelease,
		Row:       1,
		Col:       codeOffset + 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("release command = %v, want %v", cmd, CommandRedraw)
	}

	if got, want := viewer.ClipboardText(), "hello\nwo"; got != want {
		t.Fatalf("clipboard text = %q, want %q", got, want)
	}
	if viewer.mode != modeVisual {
		t.Fatalf("mode = %v, want visual", viewer.mode)
	}
}

func TestDiffViewerClickDoesNotSelect(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{Kind: diff.RowContext, Text: "abcde", Code: "abcde"}},
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	cmd, err := viewer.HandleEvent(vaxis.Mouse{
		Button:    vaxis.MouseLeftButton,
		EventType: vaxis.EventPress,
		Row:       0,
		Col:       2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("press command = %v, want %v", cmd, CommandRedraw)
	}
	if viewer.selection.Active {
		t.Fatalf("selection active after press: %+v", viewer.selection)
	}

	cmd, err = viewer.HandleEvent(vaxis.Mouse{
		Button:    vaxis.MouseLeftButton,
		EventType: vaxis.EventRelease,
		Row:       0,
		Col:       2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandNone {
		t.Fatalf("release command = %v, want %v", cmd, CommandNone)
	}
	if viewer.selection.Active {
		t.Fatalf("selection active after click: %+v", viewer.selection)
	}
}

func TestDiffViewerCellDragStartsSelection(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{Kind: diff.RowContext, Text: "abcdef", Code: "abcdef"}},
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	_, err := viewer.HandleEvent(vaxis.Mouse{
		Button:    vaxis.MouseLeftButton,
		EventType: vaxis.EventPress,
		Row:       0,
		Col:       1,
	})
	if err != nil {
		t.Fatal(err)
	}

	cmd, err := viewer.HandleEvent(vaxis.Mouse{
		Button:    vaxis.MouseLeftButton,
		EventType: vaxis.EventMotion,
		Row:       0,
		Col:       1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandNone {
		t.Fatalf("small motion command = %v, want %v", cmd, CommandNone)
	}
	if viewer.selection.Active {
		t.Fatalf("selection active before cell drag: %+v", viewer.selection)
	}

	cmd, err = viewer.HandleEvent(vaxis.Mouse{
		Button:    vaxis.MouseLeftButton,
		EventType: vaxis.EventMotion,
		Row:       0,
		Col:       2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("threshold motion command = %v, want %v", cmd, CommandRedraw)
	}
	if !viewer.selection.Active {
		t.Fatal("selection inactive after cell drag")
	}
	if got, want := viewer.ClipboardText(), "bc"; got != want {
		t.Fatalf("clipboard text = %q, want %q", got, want)
	}
}

func TestDiffViewerDragFromHunkHeaderIntoCodeStartsSelection(t *testing.T) {
	hunk := diff.Row{
		Kind:   diff.RowHunk,
		Text:   "@@ -1 +1 @@ func main()",
		Prefix: "@@ -1 +1 @@",
		Code:   " func main()",
	}
	code := diff.Row{Kind: diff.RowContext, Text: "abcdef", Code: "abcdef"}
	viewer := &diffViewer{rows: []diff.Row{hunk, code}}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	cmd, err := viewer.HandleEvent(vaxis.Mouse{
		Button:    vaxis.MouseLeftButton,
		EventType: vaxis.EventPress,
		Row:       0,
		Col:       textCellWidth(hunk.Prefix) + 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandNone {
		t.Fatalf("press command = %v, want %v", cmd, CommandNone)
	}
	if viewer.selection.Active {
		t.Fatalf("selection active after hunk press: %+v", viewer.selection)
	}

	cmd, err = viewer.HandleEvent(vaxis.Mouse{
		Button:    vaxis.MouseLeftButton,
		EventType: vaxis.EventMotion,
		Row:       1,
		Col:       1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("motion command = %v, want %v", cmd, CommandRedraw)
	}
	if !viewer.selection.Active {
		t.Fatal("selection inactive after dragging into code")
	}
	if got, want := viewer.ClipboardText(), "ab"; got != want {
		t.Fatalf("clipboard text = %q, want %q", got, want)
	}

	cmd, err = viewer.HandleEvent(vaxis.Mouse{
		Button:    vaxis.MouseLeftButton,
		EventType: vaxis.EventMotion,
		Row:       1,
		Col:       2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("second motion command = %v, want %v", cmd, CommandRedraw)
	}
	if got, want := viewer.ClipboardText(), "abc"; got != want {
		t.Fatalf("clipboard text = %q, want %q", got, want)
	}
}

func TestDiffViewerDragFromHunkHeaderUpIntoCodeStartsAtEndOfLine(t *testing.T) {
	code := diff.Row{Kind: diff.RowContext, Text: "abcdef", Code: "abcdef"}
	hunk := diff.Row{
		Kind:   diff.RowHunk,
		Text:   "@@ -1 +1 @@ func main()",
		Prefix: "@@ -1 +1 @@",
		Code:   " func main()",
	}
	viewer := &diffViewer{rows: []diff.Row{code, hunk}}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	cmd, err := viewer.HandleEvent(vaxis.Mouse{
		Button:    vaxis.MouseLeftButton,
		EventType: vaxis.EventPress,
		Row:       1,
		Col:       textCellWidth(hunk.Prefix) + 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandNone {
		t.Fatalf("press command = %v, want %v", cmd, CommandNone)
	}

	cmd, err = viewer.HandleEvent(vaxis.Mouse{
		Button:    vaxis.MouseLeftButton,
		EventType: vaxis.EventMotion,
		Row:       0,
		Col:       4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("motion command = %v, want %v", cmd, CommandRedraw)
	}
	if got, want := viewer.ClipboardText(), "ef"; got != want {
		t.Fatalf("clipboard text = %q, want %q", got, want)
	}

	cmd, err = viewer.HandleEvent(vaxis.Mouse{
		Button:    vaxis.MouseLeftButton,
		EventType: vaxis.EventMotion,
		Row:       0,
		Col:       3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("second motion command = %v, want %v", cmd, CommandRedraw)
	}
	if got, want := viewer.ClipboardText(), "def"; got != want {
		t.Fatalf("clipboard text = %q, want %q", got, want)
	}
}

func TestDiffViewerSelectionDoesNotSelectHunkHeaderContext(t *testing.T) {
	row := diff.Row{
		Kind:   diff.RowHunk,
		Text:   "@@ -231,8 +233,10 @@ func (d *diffViewer) Layout(constraints Constraints) Size {",
		Prefix: "@@ -231,8 +233,10 @@",
		Code:   " func (d *diffViewer) Layout(constraints Constraints) Size {",
	}
	viewer := &diffViewer{rows: []diff.Row{row}}
	viewer.Layout(Tight(Size{Width: 100, Height: 10}))

	if _, _, ok := viewer.codeRange(row); ok {
		t.Fatal("hunk header context is selectable")
	}
	if _, ok := viewer.selectionPoint(vaxis.Mouse{Row: 0, Col: textCellWidth(row.Prefix) + 1}); ok {
		t.Fatal("selection point found for hunk header context")
	}

	viewer.selection = textSelection{
		Active: true,
		Anchor: selectionPoint{Row: 0, Col: textCellWidth(row.Prefix)},
		Cursor: selectionPoint{Row: 0, Col: textCellWidth(row.Text) - 1},
	}
	if got := viewer.ClipboardText(); got != "" {
		t.Fatalf("clipboard text = %q, want empty", got)
	}
}

func TestDiffViewerSelectionDoesNotSelectCommitPreamble(t *testing.T) {
	rows := []diff.Row{
		{
			Kind:   diff.RowContext,
			Gutter: "1 1   ",
			Code:   "selectable",
		},
		{
			Kind:   diff.RowCommitHeader,
			Text:   "commit abc123",
			Prefix: "commit ",
			Code:   "abc123",
		},
		{
			Kind:   diff.RowCommitMeta,
			Text:   "Author: Example <example@example.com>",
			Prefix: "Author: ",
			Code:   "Example <example@example.com>",
		},
	}
	rows[0].Text = rows[0].Gutter + rows[0].Code
	viewer := &diffViewer{rows: rows}
	viewer.Layout(Tight(Size{Width: 100, Height: 10}))

	for rowIndex, row := range rows[1:] {
		if _, _, ok := viewer.codeRange(row); ok {
			t.Fatalf("row %d kind %v is selectable", rowIndex+1, row.Kind)
		}
		if _, ok := viewer.selectionPoint(vaxis.Mouse{Row: rowIndex + 1, Col: textCellWidth(row.Text) - 1}); ok {
			t.Fatalf("selection point found for row %d kind %v", rowIndex+1, row.Kind)
		}
	}

	viewer.selection = textSelection{
		Active: true,
		Anchor: selectionPoint{Row: 0, Col: testCodeOffset(rows[0])},
		Cursor: selectionPoint{Row: 2, Col: textCellWidth(rows[2].Text) - 1},
	}
	if got, want := viewer.ClipboardText(), "selectable"; got != want {
		t.Fatalf("clipboard text = %q, want %q", got, want)
	}
	start, end, ok := viewer.selectionRange()
	if !ok {
		t.Fatal("selection range not active")
	}
	if _, _, ok := viewer.selectionPaintRange(1, start, end); ok {
		t.Fatal("commit header should not have a selection paint range")
	}
	if _, _, ok := viewer.selectionPaintRange(2, start, end); ok {
		t.Fatal("commit metadata should not have a selection paint range")
	}
}

func TestDiffViewerClipboardSkipsNonSelectableRowsWithoutExtraNewline(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Code: "hello", Text: "hello"},
		{
			Kind:   diff.RowHunk,
			Text:   "@@ -1 +1 @@ func main()",
			Prefix: "@@ -1 +1 @@",
			Code:   " func main()",
		},
		{Kind: diff.RowContext, Code: "world", Text: "world"},
	}
	viewer := &diffViewer{
		rows: rows,
		selection: textSelection{
			Active: true,
			Anchor: selectionPoint{Row: 0, Col: 0},
			Cursor: selectionPoint{Row: 1, Col: textCellWidth(rows[1].Text) - 1},
		},
	}
	if got, want := viewer.ClipboardText(), "hello"; got != want {
		t.Fatalf("clipboard text = %q, want %q", got, want)
	}

	viewer.selection.Cursor = selectionPoint{Row: 2, Col: textCellWidth(rows[2].Text) - 1}
	if got, want := viewer.ClipboardText(), "hello\nworld"; got != want {
		t.Fatalf("clipboard text = %q, want %q", got, want)
	}
}

func TestDiffViewerClipboardPreservesTabs(t *testing.T) {
	row := diff.Row{
		Kind: diff.RowContext,
		Code: "a\tb",
		Text: "a\tb",
	}
	viewer := &diffViewer{
		rows: []diff.Row{row},
		selection: textSelection{
			Active: true,
			Anchor: selectionPoint{Row: 0, Col: 1},
			Cursor: selectionPoint{Row: 0, Col: 1},
		},
	}

	if got, want := viewer.ClipboardText(), "\t"; got != want {
		t.Fatalf("clipboard text = %q, want %q", got, want)
	}
}

func TestDiffViewerTripleClickSelectsOnlyCode(t *testing.T) {
	row := diff.Row{
		Kind:   diff.RowAdd,
		Gutter: "1 1 + ",
		Code:   "hello",
	}
	row.Text = row.Gutter + row.Code
	viewer := &diffViewer{rows: []diff.Row{row}}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))
	codeOffset := testCodeOffset(row)

	for i := 0; i < 3; i++ {
		_, err := viewer.HandleEvent(vaxis.Mouse{
			Button:    vaxis.MouseLeftButton,
			EventType: vaxis.EventPress,
			Row:       0,
			Col:       codeOffset + 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = viewer.HandleEvent(vaxis.Mouse{
			Button:    vaxis.MouseLeftButton,
			EventType: vaxis.EventRelease,
			Row:       0,
			Col:       codeOffset + 1,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	if got, want := viewer.ClipboardText(), "hello"; got != want {
		t.Fatalf("clipboard text = %q, want %q", got, want)
	}
	if viewer.mode != modeVisualLine {
		t.Fatalf("mode = %v, want visual line", viewer.mode)
	}
}

func TestDiffViewerPaintsSelectedEmptyDiffLineAsOneCellAfterGutter(t *testing.T) {
	row := diff.Row{
		Kind:   diff.RowAdd,
		Gutter: "1 1 + ",
		Code:   "",
	}
	row.Text = row.Gutter
	gutterWidth := testCodeOffset(row)
	viewer := &diffViewer{
		rows: []diff.Row{row},
		selection: textSelection{
			Active: true,
			Anchor: selectionPoint{Row: 0, Col: 0},
			Cursor: selectionPoint{Row: 0, Col: gutterWidth},
		},
	}
	start, end, ok := viewer.selectionRange()
	if !ok {
		t.Fatal("selection range not active")
	}

	startCol, endCol, ok := viewer.selectionPaintRange(0, start, end)
	if !ok {
		t.Fatal("selection paint range not found")
	}
	if startCol != gutterWidth || endCol != gutterWidth+1 {
		t.Fatalf("paint range = %d:%d, want %d:%d", startCol, endCol, gutterWidth, gutterWidth+1)
	}
	if got, want := viewer.ClipboardText(), " "; got != want {
		t.Fatalf("clipboard text = %q, want %q", got, want)
	}
}

func TestDiffViewerPaintsSelectedEmptyCodeAsOneCell(t *testing.T) {
	row := diff.Row{
		Kind:   diff.RowAdd,
		Gutter: "1 1 + ",
		Code:   "",
	}
	row.Text = row.Gutter
	codeOffset := testCodeOffset(row)
	viewer := &diffViewer{
		rows: []diff.Row{row},
		selection: textSelection{
			Active: true,
			Anchor: selectionPoint{Row: 0, Col: codeOffset},
			Cursor: selectionPoint{Row: 0, Col: codeOffset},
		},
	}
	start, end, ok := viewer.selectionRange()
	if !ok {
		t.Fatal("selection range not active")
	}

	startCol, endCol, ok := viewer.selectionPaintRange(0, start, end)
	if !ok {
		t.Fatal("selection paint range not found")
	}
	if startCol != codeOffset || endCol != codeOffset+1 {
		t.Fatalf("paint range = %d:%d, want %d:%d", startCol, endCol, codeOffset, codeOffset+1)
	}
}

func TestDiffViewerDoubleClickSelectsToken(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{Kind: diff.RowContext, Text: "foo bar.baz", Code: "foo bar.baz"}},
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	for i := 0; i < 2; i++ {
		cmd, err := viewer.HandleEvent(vaxis.Mouse{
			Button:    vaxis.MouseLeftButton,
			EventType: vaxis.EventPress,
			Row:       0,
			Col:       5,
		})
		if err != nil {
			t.Fatal(err)
		}
		if cmd != CommandRedraw {
			t.Fatalf("press %d command = %v, want %v", i, cmd, CommandRedraw)
		}
		_, err = viewer.HandleEvent(vaxis.Mouse{
			Button:    vaxis.MouseLeftButton,
			EventType: vaxis.EventRelease,
			Row:       0,
			Col:       5,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	if got, want := viewer.ClipboardText(), "bar"; got != want {
		t.Fatalf("clipboard text = %q, want %q", got, want)
	}
	if got, want := viewer.cursor, (selectionPoint{Row: 0, Col: 6}); got != want {
		t.Fatalf("cursor = %+v, want %+v", got, want)
	}
}

func TestDiffViewerTextObjectSelectsInnerWord(t *testing.T) {
	viewer := &diffViewer{
		rows:   []diff.Row{{Kind: diff.RowContext, Text: "foo bar.baz", Code: "foo bar.baz"}},
		cursor: selectionPoint{Row: 0, Col: 5},
		mode:   modeVisual,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	if _, err := viewer.HandleEvent(vaxis.Key{Text: "i", Keycode: 'i'}); err != nil {
		t.Fatal(err)
	}
	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "w", Keycode: 'w'})
	if err != nil {
		t.Fatal(err)
	}

	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want redraw", cmd)
	}
	if got, want := viewer.ClipboardText(), "bar"; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
}

func TestDiffViewerTextObjectUsesPrintedDelimiterForShiftedKeys(t *testing.T) {
	tests := []struct {
		name   string
		code   string
		cursor int
		object vaxis.Key
		want   string
	}{
		{
			name:   "double quote",
			code:   `const x = "hello"`,
			cursor: strings.Index(`const x = "hello"`, "hello"),
			object: vaxis.Key{Text: `"`, Keycode: '\''},
			want:   `"hello"`,
		},
		{
			name:   "paren",
			code:   `call(foo)`,
			cursor: strings.Index(`call(foo)`, "foo"),
			object: vaxis.Key{Text: "(", Keycode: '9'},
			want:   `(foo)`,
		},
		{
			name:   "brace",
			code:   `before {inside} after`,
			cursor: strings.Index(`before {inside} after`, "inside"),
			object: vaxis.Key{Text: "{", Keycode: '['},
			want:   `{inside}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viewer := &diffViewer{
				rows:   []diff.Row{{Kind: diff.RowContext, Text: tt.code, Code: tt.code}},
				cursor: selectionPoint{Row: 0, Col: tt.cursor},
				mode:   modeVisual,
			}
			viewer.Layout(Tight(Size{Width: 80, Height: 10}))

			if _, err := viewer.HandleEvent(vaxis.Key{Text: "a", Keycode: 'a'}); err != nil {
				t.Fatal(err)
			}
			cmd, err := viewer.HandleEvent(tt.object)
			if err != nil {
				t.Fatal(err)
			}
			if cmd != CommandRedraw {
				t.Fatalf("command = %v, want redraw", cmd)
			}
			if got := viewer.ClipboardText(); got != tt.want {
				t.Fatalf("selection text = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDiffViewerTextObjectIgnoresShiftModifierPress(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowContext, Text: "        vaxis.Segment{", Code: "        vaxis.Segment{"},
			{Kind: diff.RowContext, Text: `            Text:  " " + d.modeLabel() + " ",`, Code: `            Text:  " " + d.modeLabel() + " ",`},
			{Kind: diff.RowContext, Text: "            Style: d.statusStyle(),", Code: "            Style: d.statusStyle(),"},
			{Kind: diff.RowContext, Text: "        },", Code: "        },"},
		},
		cursor: selectionPoint{Row: 2, Col: 8},
		mode:   modeVisual,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	if _, err := viewer.HandleEvent(vaxis.Key{Text: "i", Keycode: 'i'}); err != nil {
		t.Fatal(err)
	}
	if _, err := viewer.HandleEvent(vaxis.Key{Keycode: vaxis.KeyLeftShift}); err != nil {
		t.Fatal(err)
	}
	if !viewer.textObject.active {
		t.Fatal("text object state cleared by shift press")
	}
	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "[", Keycode: '[', Modifiers: vaxis.ModShift})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want redraw", cmd)
	}
	if got, want := viewer.ClipboardText(), `
            Text:  " " + d.modeLabel() + " ",
            Style: d.statusStyle(),
        `; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
}

func TestDiffViewerTextObjectUsesShiftedPunctuationFallback(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowContext, Text: "        vaxis.Segment{", Code: "        vaxis.Segment{"},
			{Kind: diff.RowContext, Text: `            Text:  " " + d.modeLabel() + " ",`, Code: `            Text:  " " + d.modeLabel() + " ",`},
			{Kind: diff.RowContext, Text: "            Style: d.statusStyle(),", Code: "            Style: d.statusStyle(),"},
			{Kind: diff.RowContext, Text: "        },", Code: "        },"},
		},
		cursor: selectionPoint{Row: 2, Col: 8},
		mode:   modeVisual,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	if _, err := viewer.HandleEvent(vaxis.Key{Text: "i", Keycode: 'i'}); err != nil {
		t.Fatal(err)
	}
	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "[", Keycode: '[', Modifiers: vaxis.ModShift})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want redraw", cmd)
	}
	if got, want := viewer.ClipboardText(), `
            Text:  " " + d.modeLabel() + " ",
            Style: d.statusStyle(),
        `; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
}

func TestDiffViewerTextObjectSelectsNextMultilineObject(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowContext, Text: "func main() {", Code: "func main() {"},
			{Kind: diff.RowContext, Text: "\tcall()", Code: "\tcall()"},
			{Kind: diff.RowContext, Text: "}", Code: "}"},
		},
		cursor: selectionPoint{Row: 0, Col: 0},
		mode:   modeVisual,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	if _, err := viewer.HandleEvent(vaxis.Key{Text: "a", Keycode: 'a'}); err != nil {
		t.Fatal(err)
	}
	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "{", Keycode: '['})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want redraw", cmd)
	}
	if got, want := viewer.ClipboardText(), "{\n\tcall()\n}"; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
}

func TestDiffViewerInnerTextObjectHighlightsBoundaryNewlines(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Text: "foo{", Code: "foo{"},
		{Kind: diff.RowContext, Text: "  foo,", Code: "  foo,"},
		{Kind: diff.RowContext, Text: "}", Code: "}"},
	}
	viewer := &diffViewer{
		rows:   rows,
		cursor: selectionPoint{Row: 1, Col: 0},
		mode:   modeVisual,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	if _, err := viewer.HandleEvent(vaxis.Key{Text: "i", Keycode: 'i'}); err != nil {
		t.Fatal(err)
	}
	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "{", Keycode: '['})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want redraw", cmd)
	}
	if got, want := viewer.ClipboardText(), "\n  foo,\n"; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}

	openingSpec, ok := viewer.selectionPaintSpec(0, time.Now())
	if !ok {
		t.Fatal("opening row selection paint spec missing")
	}
	if got, want := openingSpec.startCol, textCellWidth(rows[0].Code); got != want {
		t.Fatalf("opening selection paint start = %d, want %d", got, want)
	}
	if got, want := openingSpec.endCol, textCellWidth(rows[0].Code)+1; got != want {
		t.Fatalf("opening selection paint end = %d, want %d", got, want)
	}
	contentSpec, ok := viewer.selectionPaintSpec(1, time.Now())
	if !ok {
		t.Fatal("content row selection paint spec missing")
	}
	if got, want := contentSpec.endCol, textCellWidth(rows[1].Code)+1; got != want {
		t.Fatalf("content selection paint end = %d, want %d", got, want)
	}
}

func TestDiffViewerTextObjectSelectsAroundBracesWithinHunk(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Text: "func main() {", Code: "func main() {"},
		{Kind: diff.RowContext, Text: "\tcall()", Code: "\tcall()"},
		{Kind: diff.RowContext, Text: "}", Code: "}"},
		{Kind: diff.RowHunk, Text: "@@ -10 +10 @@"},
		{Kind: diff.RowContext, Text: "other {}", Code: "other {}"},
	}
	viewer := &diffViewer{
		rows:   rows,
		cursor: selectionPoint{Row: 1, Col: 2},
		mode:   modeVisual,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	if _, err := viewer.HandleEvent(vaxis.Key{Text: "a", Keycode: 'a'}); err != nil {
		t.Fatal(err)
	}
	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "{", Keycode: '{'})
	if err != nil {
		t.Fatal(err)
	}

	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want redraw", cmd)
	}
	if got, want := viewer.ClipboardText(), "{\n\tcall()\n}"; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
}

func TestDiffViewerTextObjectSelectsAcrossOppositeSideRows(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "  1   1   ", Code: "if ok {", FileName: "main.go"},
		{Kind: diff.RowDelete, Gutter: "  2     - ", Code: "old()", FileName: "main.go"},
		{Kind: diff.RowAdd, Gutter: "      2 + ", Code: "new()", FileName: "main.go"},
		{Kind: diff.RowContext, Gutter: "  3   3   ", Code: "}", FileName: "main.go"},
	}
	for index := range rows {
		rows[index].Text = rows[index].Gutter + rows[index].Code
	}
	cursorCol := testCodeOffset(rows[2]) + strings.Index(rows[2].Code, "new")
	viewer := &diffViewer{
		rows:   rows,
		cursor: selectionPoint{Row: 2, Col: cursorCol},
		mode:   modeVisual,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	if _, err := viewer.HandleEvent(vaxis.Key{Text: "a", Keycode: 'a'}); err != nil {
		t.Fatal(err)
	}
	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "{", Keycode: '['})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want redraw", cmd)
	}
	if got, want := viewer.ClipboardText(), "{\nnew()\n}"; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
}

func TestDiffViewerTextObjectSelectsQuotesAcrossRows(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "  1   1   ", Code: `message = "hello`, FileName: "main.go"},
		{Kind: diff.RowDelete, Gutter: "  2     - ", Code: "old", FileName: "main.go"},
		{Kind: diff.RowAdd, Gutter: "      2 + ", Code: "world", FileName: "main.go"},
		{Kind: diff.RowContext, Gutter: "  3   3   ", Code: `"`, FileName: "main.go"},
	}
	for index := range rows {
		rows[index].Text = rows[index].Gutter + rows[index].Code
	}
	viewer := &diffViewer{
		rows:   rows,
		cursor: selectionPoint{Row: 2, Col: testCodeOffset(rows[2])},
		mode:   modeVisual,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	if _, err := viewer.HandleEvent(vaxis.Key{Text: "a", Keycode: 'a'}); err != nil {
		t.Fatal(err)
	}
	cmd, err := viewer.HandleEvent(vaxis.Key{Text: `"`, Keycode: '\''})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("command = %v, want redraw", cmd)
	}
	if got, want := viewer.ClipboardText(), "\"hello\nworld\n\""; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
}

func TestDiffViewerTextObjectSelectsInsideQuotes(t *testing.T) {
	viewer := &diffViewer{
		rows:   []diff.Row{{Kind: diff.RowContext, Text: `const x = "hello"`, Code: `const x = "hello"`}},
		cursor: selectionPoint{Row: 0, Col: 11},
		mode:   modeVisual,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	open, close, ok := textObjectDelimiters('"')
	if !ok {
		t.Fatal("quote delimiter missing")
	}
	if !viewer.selectDelimitedTextObject(textObjectAround, open, close) {
		t.Fatal("quote text object failed")
	}
	if got, want := viewer.ClipboardText(), `"hello"`; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
}

func TestDiffViewerTripleClickSelectsRow(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{Kind: diff.RowContext, Text: "foo bar.baz", Code: "foo bar.baz"}},
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	for i := 0; i < 3; i++ {
		_, err := viewer.HandleEvent(vaxis.Mouse{
			Button:    vaxis.MouseLeftButton,
			EventType: vaxis.EventPress,
			Row:       0,
			Col:       5,
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = viewer.HandleEvent(vaxis.Mouse{
			Button:    vaxis.MouseLeftButton,
			EventType: vaxis.EventRelease,
			Row:       0,
			Col:       5,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	if got, want := viewer.ClipboardText(), "foo bar.baz"; got != want {
		t.Fatalf("clipboard text = %q, want %q", got, want)
	}
	if got, want := viewer.cursor, (selectionPoint{Row: 0, Col: 5}); got != want {
		t.Fatalf("cursor = %+v, want %+v", got, want)
	}
}

func TestTokenRangeAtUsesUnicodeClasses(t *testing.T) {
	start, end := tokenRangeAt("alpha βeta.gamma", 7)
	if got, want := cellTextRange("alpha βeta.gamma", start, end), "βeta"; got != want {
		t.Fatalf("token = %q, want %q", got, want)
	}

	start, end = tokenRangeAt("alpha βeta.gamma", 10)
	if got, want := cellTextRange("alpha βeta.gamma", start, end), "."; got != want {
		t.Fatalf("punctuation token = %q, want %q", got, want)
	}
}

func TestDiffViewerCopyKeyCopiesSelection(t *testing.T) {
	tests := []struct {
		name string
		key  vaxis.Key
	}{
		{
			name: "y",
			key:  vaxis.Key{Text: "y", Keycode: 'y'},
		},
		{
			name: "copy key",
			key:  vaxis.Key{Keycode: vaxis.KeyCopy},
		},
		{
			name: "super c",
			key:  vaxis.Key{Text: "c", Keycode: 'c', Modifiers: vaxis.ModSuper},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viewer := &diffViewer{
				rows: []diff.Row{{Kind: diff.RowContext, Text: "abcde", Code: "abcde"}},
				selection: textSelection{
					Active: true,
					Anchor: selectionPoint{
						Row: 0,
						Col: 1,
					},
					Cursor: selectionPoint{
						Row: 0,
						Col: 3,
					},
				},
			}

			cmd, err := viewer.HandleEvent(tt.key)
			if err != nil {
				t.Fatal(err)
			}
			if cmd != CommandCopy {
				t.Fatalf("command = %v, want %v", cmd, CommandCopy)
			}
			if viewer.selection.Active {
				t.Fatalf("selection still active after copy: %+v", viewer.selection)
			}
			if viewer.mode != modeNormal {
				t.Fatalf("mode = %v, want normal", viewer.mode)
			}
			if got, want := viewer.ClipboardText(), "bcd"; got != want {
				t.Fatalf("clipboard text = %q, want %q", got, want)
			}
			viewer.ClipboardConsumed()
			if got := viewer.ClipboardText(); got != "" {
				t.Fatalf("clipboard text after consume = %q, want empty", got)
			}
		})
	}
}

func TestDiffViewerHighlightYankChangesSelectionStyleTemporarily(t *testing.T) {
	viewer := &diffViewer{}
	viewer.ensureColorScheme()
	now := time.Unix(1, 0)

	if got, want := viewer.selectionStyleAt(now).Background, viewer.scheme.Selection; got != want {
		t.Fatalf("selection background = %v, want %v", got, want)
	}

	viewer.HighlightYank(now)
	if got, want := viewer.selectionStyleAt(now.Add(time.Millisecond)).Background, viewer.scheme.Yank; got != want {
		t.Fatalf("yank background = %v, want %v", got, want)
	}
	if got, want := viewer.selectionStyleAt(now.Add(yankHighlightDuration+time.Millisecond)).Background, viewer.scheme.Selection; got != want {
		t.Fatalf("expired yank background = %v, want %v", got, want)
	}
}

func TestDiffViewerSelectionPointAccountsForHorizontalScroll(t *testing.T) {
	row := diff.Row{
		Kind:   diff.RowAdd,
		Gutter: "1 1 + ",
		Code:   "abcdef",
	}
	row.Text = row.Gutter + row.Code
	viewer := &diffViewer{
		rows:    []diff.Row{row},
		xScroll: 2,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))
	codeOffset := testCodeOffset(row)

	point, ok := viewer.selectionPoint(vaxis.Mouse{
		Row: 0,
		Col: codeOffset,
	})
	if !ok {
		t.Fatal("selection point not found")
	}
	if want := codeOffset + viewer.xScroll; point.Col != want {
		t.Fatalf("selection col = %d, want %d", point.Col, want)
	}
}

func TestDiffViewerScrollbar(t *testing.T) {
	tests := []struct {
		name   string
		rows   int
		height int
		width  int
		scroll int
		want   scrollbar
	}{
		{
			name:   "hidden when rows fit",
			rows:   5,
			height: 10,
			width:  80,
			want:   scrollbar{},
		},
		{
			name:   "top",
			rows:   100,
			height: 11,
			width:  80,
			want: scrollbar{
				Visible: true,
				Col:     79,
				Row:     0,
				Length:  10,
				Thumb:   0,
				Size:    1,
			},
		},
		{
			name:   "middle",
			rows:   100,
			height: 11,
			width:  80,
			scroll: 45,
			want: scrollbar{
				Visible: true,
				Col:     79,
				Row:     0,
				Length:  10,
				Thumb:   4,
				Size:    1,
			},
		},
		{
			name:   "bottom",
			rows:   100,
			height: 11,
			width:  80,
			scroll: 90,
			want: scrollbar{
				Visible: true,
				Col:     79,
				Row:     0,
				Length:  10,
				Thumb:   9,
				Size:    1,
			},
		},
		{
			name:   "larger thumb",
			rows:   20,
			height: 11,
			width:  80,
			want: scrollbar{
				Visible: true,
				Col:     79,
				Row:     0,
				Length:  10,
				Thumb:   0,
				Size:    5,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viewer := newTestDiffViewer(tt.rows, tt.height)
			viewer.scroll = tt.scroll
			got := viewer.scrollbar(tt.width, tt.height)

			if got != tt.want {
				t.Fatalf("scrollbar = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDiffViewerHorizontalScrollbar(t *testing.T) {
	tests := []struct {
		name    string
		width   int
		height  int
		content string
		xScroll int
		rows    int
		want    scrollbar
	}{
		{
			name:    "hidden when content fits",
			width:   20,
			height:  10,
			content: "short",
			want:    scrollbar{},
		},
		{
			name:    "top",
			width:   20,
			height:  10,
			content: "0123456789012345678901234567890123456789",
			want: scrollbar{
				Visible: true,
				Col:     0,
				Row:     8,
				Length:  20,
				Thumb:   0,
				Size:    10,
			},
		},
		{
			name:    "middle with vertical scrollbar",
			width:   20,
			height:  10,
			rows:    20,
			content: "0123456789012345678901234567890123456789",
			xScroll: 10,
			want: scrollbar{
				Visible: true,
				Col:     0,
				Row:     8,
				Length:  19,
				Thumb:   4,
				Size:    9,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := tt.rows
			if rows == 0 {
				rows = 1
			}
			viewer := newTestDiffViewer(rows, tt.height)
			viewer.width = tt.width
			viewer.rows[0].Text = tt.content
			viewer.xScroll = tt.xScroll
			got := viewer.horizontalScrollbar(tt.width, tt.height)

			if got != tt.want {
				t.Fatalf("horizontal scrollbar = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPaintSegmentsOffset(t *testing.T) {
	base := vaxis.Style{Foreground: vaxis.RGBColor(1, 2, 3)}
	alt := vaxis.Style{Foreground: vaxis.RGBColor(4, 5, 6)}
	segments := []vaxis.Segment{
		{Text: "abc", Style: base},
		{Text: "def", Style: alt},
		{Text: "ghi", Style: base},
	}
	cells := testCells{}

	paintSegmentsOffset(cells, 5, 0, 0, 2, segments...)

	if got, want := cells.text(5), "cdefg"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if got := cells[0].Style; got != base {
		t.Fatalf("first style = %+v, want %+v", got, base)
	}
	if got := cells[1].Style; got != alt {
		t.Fatalf("second style = %+v, want %+v", got, alt)
	}
	if got := cells[4].Style; got != base {
		t.Fatalf("last style = %+v, want %+v", got, base)
	}
}

func TestPaintSegmentsOffsetUsesCellWidths(t *testing.T) {
	base := vaxis.Style{Foreground: vaxis.RGBColor(1, 2, 3)}
	segments := []vaxis.Segment{{Text: "a\tb", Style: base}}
	cells := testCells{}

	paintSegmentsOffset(cells, 4, 0, 0, 1, segments...)

	if got, want := cells.text(4), "    "; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestStyleSegmentsRangeAppliesCurlyUnderline(t *testing.T) {
	base := vaxis.Style{Foreground: vaxis.RGBColor(1, 2, 3)}
	underline := vaxis.Style{
		UnderlineColor: vaxis.RGBColor(4, 5, 6),
		UnderlineStyle: vaxis.UnderlineCurly,
	}

	segments := styleSegmentsRange([]vaxis.Segment{{Text: "abcdef", Style: base}}, 1, 4, underline)

	if got, want := segmentTextWidth(segments), 6; got != want {
		t.Fatalf("width = %d, want %d", got, want)
	}
	for index, segment := range segments {
		switch segment.Text {
		case "a", "ef":
			if segment.Style.UnderlineStyle != vaxis.UnderlineOff {
				t.Fatalf("segment %d = %+v, want no underline", index, segment)
			}
		case "bcd":
			if segment.Style.UnderlineStyle != vaxis.UnderlineCurly || segment.Style.UnderlineColor != underline.UnderlineColor {
				t.Fatalf("segment %d = %+v, want curly underline", index, segment)
			}
		default:
			t.Fatalf("unexpected segment %d = %+v", index, segment)
		}
	}
}

func TestDiffViewerReviewSegmentsUnderlineInlineDraft(t *testing.T) {
	start, end := 2, 5
	anchor := review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}
	viewer := &diffViewer{
		reviewDrafts: []review.CommentDraft{{
			Path:        "main.go",
			Line:        12,
			Side:        review.SideRight,
			StartColumn: &start,
			EndColumn:   &end,
			Body:        "inline",
		}},
	}
	viewer.ensureColorScheme()

	segments := viewer.reviewSegments(diff.Row{Review: anchor}, []vaxis.Segment{{Text: "abcdef", Style: viewer.baseStyle()}})

	if len(segments) != 3 {
		t.Fatalf("segments = %+v, want 3", segments)
	}
	if got, want := segments[1].Text, "bcde"; got != want {
		t.Fatalf("underlined text = %q, want %q", got, want)
	}
	if segments[1].Style.UnderlineStyle != vaxis.UnderlineCurly || segments[1].Style.UnderlineColor != viewer.scheme.Yellow {
		t.Fatalf("underlined style = %+v", segments[1].Style)
	}
}

func TestDiffViewerSelectionPreservesInlineReviewUnderline(t *testing.T) {
	start, end := 2, 5
	anchor := review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}
	row := diff.Row{
		Kind:   diff.RowContext,
		Gutter: "1 1 + ",
		Code:   "abcdef",
		Review: anchor,
	}
	row.Text = row.Gutter + row.Code
	viewer := &diffViewer{
		rows: []diff.Row{row},
		reviewDrafts: []review.CommentDraft{{
			Path:        "main.go",
			Line:        12,
			Side:        review.SideRight,
			StartColumn: &start,
			EndColumn:   &end,
			Body:        "inline",
		}},
	}
	viewer.ensureColorScheme()

	style := viewer.selectionCellStyle(row, testCodeOffset(row)+1, viewer.selectionStyle())
	if style.UnderlineStyle != vaxis.UnderlineCurly || style.UnderlineColor != viewer.scheme.Yellow {
		t.Fatalf("selected inline style = %+v", style)
	}

	style = viewer.selectionCellStyle(row, testCodeOffset(row), viewer.selectionStyle())
	if style.UnderlineStyle != vaxis.UnderlineOff {
		t.Fatalf("selected non-inline style = %+v, want no underline", style)
	}
}

func TestDiffViewerJumpCommitKeys(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowCommitHeader, Text: "commit one"},
			{Kind: diff.RowFile, Text: "one.go"},
			{Kind: diff.RowCommitHeader, Text: "commit two"},
			{Kind: diff.RowFile, Text: "two.go"},
		},
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "J", Keycode: 'J'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("J command = %v, want redraw", cmd)
	}
	if got, want := viewer.cursor.Row, 2; got != want {
		t.Fatalf("cursor row after J = %d, want %d", got, want)
	}

	cmd, err = viewer.HandleEvent(vaxis.Key{Text: "K", Keycode: 'K'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("K command = %v, want redraw", cmd)
	}
	if got, want := viewer.cursor.Row, 0; got != want {
		t.Fatalf("cursor row after K = %d, want %d", got, want)
	}
}

func TestDiffViewerTogglesSideBySideLayout(t *testing.T) {
	viewer := newTestDiffViewer(1, 10)

	cmd, err := viewer.HandleEvent(vaxis.Key{Text: "s", Keycode: 's'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("toggle command = %v, want redraw", cmd)
	}
	if viewer.layoutMode != layoutSideBySide {
		t.Fatalf("layout mode = %v, want side-by-side", viewer.layoutMode)
	}

	cmd, err = viewer.HandleEvent(vaxis.Key{Text: "s", Keycode: 's'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("second toggle command = %v, want redraw", cmd)
	}
	if viewer.layoutMode != layoutStacked {
		t.Fatalf("layout mode = %v, want stacked", viewer.layoutMode)
	}
}

func TestDiffViewerSideBySideRowsPairReplacementBlocks(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowFile, Text: "main.go"},
			{Kind: diff.RowContext, Gutter: "1 1   ", Code: "same"},
			{Kind: diff.RowDelete, Gutter: "2     - ", Code: "old one"},
			{Kind: diff.RowDelete, Gutter: "3     - ", Code: "old two"},
			{Kind: diff.RowAdd, Gutter: "    2 + ", Code: "new one"},
			{Kind: diff.RowContext, Gutter: "4 3   ", Code: "after"},
		},
	}

	rows := viewer.sideBySideRows()
	if len(rows) != 5 {
		t.Fatalf("side rows = %+v, want 5 rows", rows)
	}
	if rows[0].Full != 0 {
		t.Fatalf("file row = %+v, want full row 0", rows[0])
	}
	if rows[1].Left != 1 || rows[1].Right != 1 {
		t.Fatalf("context row = %+v, want row 1 on both sides", rows[1])
	}
	if rows[2].Left != 2 || rows[2].Right != 4 {
		t.Fatalf("paired replacement row = %+v, want delete 2 add 4", rows[2])
	}
	if rows[3].Left != 3 || rows[3].Right != -1 {
		t.Fatalf("unpaired delete row = %+v, want delete 3 only", rows[3])
	}
	if rows[4].Left != 5 || rows[4].Right != 5 {
		t.Fatalf("final context row = %+v, want row 5 on both sides", rows[4])
	}
}

func TestDiffViewerSideBySideRowsUseSimilarityPairing(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowDelete, Gutter: "1     - ", Code: "foo := oldValue + 1"},
			{Kind: diff.RowDelete, Gutter: "2     - ", Code: "keep()"},
			{Kind: diff.RowAdd, Gutter: "    1 + ", Code: "inserted()"},
			{Kind: diff.RowAdd, Gutter: "    2 + ", Code: "foo := newValue + 1"},
			{Kind: diff.RowAdd, Gutter: "    3 + ", Code: "keep()"},
		},
	}

	rows := viewer.sideBySideRows()
	if len(rows) != 3 {
		t.Fatalf("side rows = %+v, want 3 rows", rows)
	}
	if rows[0].Left != 0 || rows[0].Right != 2 {
		t.Fatalf("first compact row = %+v, want delete 0 add 2", rows[0])
	}
	if rows[1].Left != 1 || rows[1].Right != 3 {
		t.Fatalf("second compact row = %+v, want delete 1 add 3", rows[1])
	}
	if rows[2].Left != -1 || rows[2].Right != 4 {
		t.Fatalf("extra add row = %+v, want add-only row 4", rows[2])
	}
}

func TestDiffViewerSideBySideRowsDoNotGapLeftSideForInsertedRows(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowDelete, Gutter: "1921     - ", Code: "name   string"},
			{Kind: diff.RowDelete, Gutter: "1922     - ", Code: "start  int"},
			{Kind: diff.RowDelete, Gutter: "1923     - ", Code: "mouse  vaxis.Mouse"},
			{Kind: diff.RowDelete, Gutter: "1924     - ", Code: "want   int"},
			{Kind: diff.RowDelete, Gutter: "1925     - ", Code: "keys   string"},
			{Kind: diff.RowDelete, Gutter: "1926     - ", Code: "noKeys bool"},
			{Kind: diff.RowAdd, Gutter: "    1921 + ", Code: "name       string"},
			{Kind: diff.RowAdd, Gutter: "    1922 + ", Code: "start      int"},
			{Kind: diff.RowAdd, Gutter: "    1923 + ", Code: "cursor     int"},
			{Kind: diff.RowAdd, Gutter: "    1924 + ", Code: "mouse      vaxis.Mouse"},
			{Kind: diff.RowAdd, Gutter: "    1925 + ", Code: "want       int"},
			{Kind: diff.RowAdd, Gutter: "    1926 + ", Code: "wantCursor int"},
			{Kind: diff.RowAdd, Gutter: "    1927 + ", Code: "keys       string"},
			{Kind: diff.RowAdd, Gutter: "    1928 + ", Code: "noKeys     bool"},
		},
	}

	rows := viewer.sideBySideRows()
	for index, row := range rows[:6] {
		if row.Left < 0 || row.Right < 0 {
			t.Fatalf("row %d = %+v, want both sides populated", index, row)
		}
	}
}

func TestDiffViewerSideBySideRowsPutAddOnlyHunkContextOnRight(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowHunk, Text: "@@ -1,2 +1,3 @@"},
			{Kind: diff.RowContext, Gutter: "1 1   ", Code: "before"},
			{Kind: diff.RowAdd, Gutter: "    2 + ", Code: "added"},
			{Kind: diff.RowContext, Gutter: "2 3   ", Code: "after"},
		},
	}

	rows := viewer.sideBySideRows()
	if len(rows) != 4 {
		t.Fatalf("side rows = %+v, want 4 rows", rows)
	}
	if rows[0].Full != 0 {
		t.Fatalf("hunk row = %+v, want full row 0", rows[0])
	}
	for index, row := range rows[1:] {
		if row.Left != -1 || row.Right != index+1 {
			t.Fatalf("row %d = %+v, want right-only doc row %d", index+1, row, index+1)
		}
	}
}

func TestDiffViewerSideBySideRowsPutDeleteOnlyHunkContextOnLeft(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowHunk, Text: "@@ -1,3 +1,2 @@"},
			{Kind: diff.RowContext, Gutter: "1 1   ", Code: "before"},
			{Kind: diff.RowDelete, Gutter: "2     - ", Code: "deleted"},
			{Kind: diff.RowContext, Gutter: "3 2   ", Code: "after"},
		},
	}

	rows := viewer.sideBySideRows()
	if len(rows) != 4 {
		t.Fatalf("side rows = %+v, want 4 rows", rows)
	}
	if rows[0].Full != 0 {
		t.Fatalf("hunk row = %+v, want full row 0", rows[0])
	}
	for index, row := range rows[1:] {
		if row.Left != index+1 || row.Right != -1 {
			t.Fatalf("row %d = %+v, want left-only doc row %d", index+1, row, index+1)
		}
	}
}

func TestDiffViewerSideBySideRowsPreserveRightOrderBeforePair(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowDelete, Gutter: "62     - ", Code: "dp := make([][]float64, len(deletes)+1)"},
			{Kind: diff.RowDelete, Gutter: "63     - ", Code: "for i := range dp {"},
			{Kind: diff.RowDelete, Gutter: "64     - ", Code: "\tdp[i] = make([]float64, len(adds)+1)"},
			{Kind: diff.RowAdd, Gutter: "    91 + ", Code: "return bestLinePairs(scores)"},
			{Kind: diff.RowAdd, Gutter: "    92 + ", Code: "}"},
			{Kind: diff.RowAdd, Gutter: "    93 + ", Code: " "},
			{Kind: diff.RowAdd, Gutter: "    94 + ", Code: "func bestLinePairs(scores [][]float64) []inlineLinePair {"},
			{Kind: diff.RowAdd, Gutter: "    95 + ", Code: "if len(scores) == 0 || len(scores[0]) == 0 {"},
			{Kind: diff.RowAdd, Gutter: "    96 + ", Code: "\treturn nil"},
			{Kind: diff.RowAdd, Gutter: "    97 + ", Code: "}"},
			{Kind: diff.RowDelete, Gutter: "67     - ", Code: "for i := len(deletes) - 1; i >= 0; i-- {"},
			{Kind: diff.RowDelete, Gutter: "68     - ", Code: "\tfor j := len(adds) - 1; j >= 0; j-- {"},
			{Kind: diff.RowAdd, Gutter: "    99 + ", Code: "deletes := len(scores)"},
			{Kind: diff.RowAdd, Gutter: "   100 + ", Code: "adds := len(scores[0])"},
			{Kind: diff.RowAdd, Gutter: "   101 + ", Code: "dp := make([][]float64, deletes+1)"},
			{Kind: diff.RowAdd, Gutter: "   102 + ", Code: "for i := range dp {"},
			{Kind: diff.RowAdd, Gutter: "   103 + ", Code: "\tdp[i] = make([]float64, adds+1)"},
			{Kind: diff.RowAdd, Gutter: "   104 + ", Code: "}"},
			{Kind: diff.RowAdd, Gutter: "   105 + ", Code: " "},
			{Kind: diff.RowAdd, Gutter: "   106 + ", Code: "for i := deletes - 1; i >= 0; i-- {"},
			{Kind: diff.RowAdd, Gutter: "   107 + ", Code: "\tfor j := adds - 1; j >= 0; j-- {"},
		},
	}

	rows := viewer.sideBySideRows()
	var right []int
	for _, row := range rows {
		if row.Right >= 0 {
			right = append(right, row.Right)
		}
	}
	for i := 1; i < len(right); i++ {
		if right[i] <= right[i-1] {
			t.Fatalf("right rows out of order: %+v from side rows %+v", right, rows)
		}
	}
}

func TestDiffViewerSideBySideRowsCompactUnpairedChanges(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowDelete, Gutter: "1     - ", Code: "alpha beta"},
			{Kind: diff.RowDelete, Gutter: "2     - ", Code: "gamma delta"},
			{Kind: diff.RowAdd, Gutter: "    1 + ", Code: "one two"},
			{Kind: diff.RowAdd, Gutter: "    2 + ", Code: "three four"},
		},
	}

	rows := viewer.sideBySideRows()
	if len(rows) != 2 {
		t.Fatalf("side rows = %+v, want 2 rows", rows)
	}
	if rows[0].Left != 0 || rows[0].Right != 2 {
		t.Fatalf("first compact row = %+v, want delete 0 add 2", rows[0])
	}
	if rows[1].Left != 1 || rows[1].Right != 3 {
		t.Fatalf("second compact row = %+v, want delete 1 add 3", rows[1])
	}
}

func TestDiffViewerSideBySideGutterUsesOneSideLineNumbers(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowDelete, Gutter: "10     - ", Code: "old"},
			{Kind: diff.RowAdd, Gutter: "    11 + ", Code: "new"},
			{Kind: diff.RowContext, Gutter: "12 13   ", Code: "same"},
		},
	}

	if got, want := viewer.sideBySideGutter(viewer.rows[0], sideLeft), "10 - "; got != want {
		t.Fatalf("delete left gutter = %q, want %q", got, want)
	}
	if got, want := viewer.sideBySideGutter(viewer.rows[1], sideRight), "11 + "; got != want {
		t.Fatalf("add right gutter = %q, want %q", got, want)
	}
	if got, want := viewer.sideBySideGutter(viewer.rows[2], sideLeft), "12   "; got != want {
		t.Fatalf("context left gutter = %q, want %q", got, want)
	}
	if got, want := viewer.sideBySideGutter(viewer.rows[2], sideRight), "13   "; got != want {
		t.Fatalf("context right gutter = %q, want %q", got, want)
	}
}

func TestDiffViewerSideBySideGutterUsesReviewMarker(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{{
			Kind:   diff.RowAdd,
			Gutter: "    11 + ",
			Code:   "new",
			Review: review.Anchor{Path: "main.go", Line: 11, Side: review.SideRight},
		}},
		reviewDrafts: []review.CommentDraft{{
			Path: "main.go",
			Line: 11,
			Side: review.SideRight,
			Body: "comment",
		}},
	}
	viewer.ensureColorScheme()

	segments := viewer.sideBySideGutterSegments(viewer.rows[0], sideRight)
	if len(segments) != 2 {
		t.Fatalf("segments = %+v, want two", segments)
	}
	if got, want := segments[0].Text, "11 +"; got != want {
		t.Fatalf("gutter text = %q, want %q", got, want)
	}
	if got, want := segments[1].Text, "▐"; got != want {
		t.Fatalf("marker text = %q, want %q", got, want)
	}
	if got, want := segments[1].Style.Foreground, viewer.scheme.Yellow; got != want {
		t.Fatalf("marker foreground = %v, want %v", got, want)
	}
}

func TestDiffViewerSideBySideSelectionPaintsCodeCell(t *testing.T) {
	row := diff.Row{Kind: diff.RowAdd, Gutter: "    11 + ", Code: "new value"}
	row.Text = row.Gutter + row.Code
	viewer := &diffViewer{
		rows: []diff.Row{row},
		selection: textSelection{
			Active: true,
			Anchor: selectionPoint{Row: 0, Col: testCodeOffset(row) + 4},
			Cursor: selectionPoint{Row: 0, Col: testCodeOffset(row) + 4},
		},
	}
	viewer.ensureColorScheme()
	cells := testCells{}

	viewer.paintSideBySideSelectionCells(cells, 24, 0, row, textCellWidth(viewer.sideBySideGutter(row, sideRight)))

	selectedCol := textCellWidth(viewer.sideBySideGutter(row, sideRight)) + 4
	cell := cells[selectedCol]
	if cell.Grapheme != "v" {
		t.Fatalf("selected cell grapheme = %q, want v", cell.Grapheme)
	}
	if cell.Background != viewer.scheme.Selection {
		t.Fatalf("selected cell style = %+v, want selection background %v", cell.Style, viewer.scheme.Selection)
	}
}

func TestDiffViewerSideBySideMouseSelectionUsesPaneCoordinates(t *testing.T) {
	row := diff.Row{Kind: diff.RowAdd, Gutter: "    11 + ", Code: "new value"}
	row.Text = row.Gutter + row.Code
	viewer := &diffViewer{
		rows:       []diff.Row{row},
		layoutMode: layoutSideBySide,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))
	_, rightStart, _ := viewer.sideBySidePaneGeometry(vaxis.Window{Width: 80, Height: 10})
	mouseCol := rightStart + textCellWidth(viewer.sideBySideGutter(row, sideRight)) + 4

	point, ok := viewer.selectionPoint(vaxis.Mouse{Row: 0, Col: mouseCol})
	if !ok {
		t.Fatal("selection point not found")
	}

	if got, want := point, (selectionPoint{Row: 0, Col: testCodeOffset(row) + 4}); got != want {
		t.Fatalf("selection point = %+v, want %+v", got, want)
	}
}

func TestDiffViewerSideBySideCursorUsesFullRowCoordinates(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowFile, Text: "main.go"},
			{Kind: diff.RowContext, Gutter: "1 1   ", Code: "same"},
		},
		cursor:     selectionPoint{Row: 0, Col: 2},
		layoutMode: layoutSideBySide,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	col, row, ok := viewer.cursorScreenPositionForSize(80, 10)
	if !ok {
		t.Fatal("cursor position not found")
	}
	if col != 2 || row != 0 {
		t.Fatalf("cursor position = %d,%d, want 2,0", col, row)
	}
}

func TestDiffViewerSideBySideCursorMovesByVisualRows(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowDelete, Gutter: "1     - ", Code: "old"},
			{Kind: diff.RowAdd, Gutter: "    1 + ", Code: "new"},
			{Kind: diff.RowContext, Gutter: "2 2   ", Code: "same"},
		},
		cursor:     selectionPoint{Row: 0, Col: len("1     - ")},
		layoutMode: layoutSideBySide,
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 10}))

	viewer.moveCursorRows(1)

	if got, want := viewer.cursor.Row, 2; got != want {
		t.Fatalf("cursor row = %d, want visual next row %d", got, want)
	}
}

func TestDiffViewerJumpCommitScrollsTargetToTop(t *testing.T) {
	rows := make([]diff.Row, 30)
	for i := range rows {
		rows[i] = diff.Row{Kind: diff.RowContext, Text: "line", Code: "line"}
	}
	rows[0] = diff.Row{Kind: diff.RowCommitHeader, Text: "commit one"}
	rows[12] = diff.Row{Kind: diff.RowCommitHeader, Text: "commit two"}
	rows[28] = diff.Row{Kind: diff.RowCommitHeader, Text: "commit three"}
	viewer := &diffViewer{
		rows:   rows,
		cursor: selectionPoint{Row: 0},
	}
	viewer.Layout(Tight(Size{Width: 80, Height: 6}))

	if !viewer.jumpCommit(1) {
		t.Fatal("jump to next commit failed")
	}
	if got, want := viewer.cursor.Row, 12; got != want {
		t.Fatalf("cursor row = %d, want %d", got, want)
	}
	if got, want := viewer.scroll, 12; got != want {
		t.Fatalf("scroll = %d, want %d", got, want)
	}

	if !viewer.jumpCommit(1) {
		t.Fatal("jump to final commit failed")
	}
	if got, want := viewer.cursor.Row, 28; got != want {
		t.Fatalf("cursor row = %d, want %d", got, want)
	}
	if got, want := viewer.scroll, viewer.maxScroll(); got != want {
		t.Fatalf("scroll near eof = %d, want max scroll %d", got, want)
	}
}

func TestDiffViewerStickyFileHeaderDoesNotCoverCommitHeader(t *testing.T) {
	viewer := &diffViewer{
		rows: []diff.Row{
			{Kind: diff.RowFile, Text: "one.go"},
			{Kind: diff.RowContext, Text: "line", Code: "line"},
			{Kind: diff.RowCommitHeader, Text: "commit two"},
			{Kind: diff.RowCommitMeta, Text: "Author: Example"},
			{Kind: diff.RowFile, Text: "two.go"},
		},
		scroll: 2,
	}

	if row, ok := viewer.stickyFileHeader(); ok {
		t.Fatalf("sticky header = %+v, want none over commit header", row)
	}
	viewer.scroll = 3
	if row, ok := viewer.stickyFileHeader(); ok {
		t.Fatalf("sticky header = %+v, want none over commit metadata", row)
	}
	viewer.scroll = 1
	if row, ok := viewer.stickyFileHeader(); !ok || row.Text != "one.go" {
		t.Fatalf("sticky header = %+v ok=%v, want one.go", row, ok)
	}
}

func TestSegmentTextWidthUsesCellWidths(t *testing.T) {
	got := segmentTextWidth([]vaxis.Segment{{Text: "a\tb"}})

	if got != 10 {
		t.Fatalf("width = %d, want 10", got)
	}
}

func newTestDiffViewer(rows int, height int) *diffViewer {
	viewer := &diffViewer{
		rows: make([]diff.Row, rows),
	}
	viewer.Layout(Tight(Size{
		Width:  80,
		Height: height,
	}))
	return viewer
}

func testCodeOffset(row diff.Row) int {
	return textCellWidth(row.Gutter + row.Marker)
}

func segmentsText(segments []vaxis.Segment) string {
	var text strings.Builder
	for _, segment := range segments {
		text.WriteString(segment.Text)
	}
	return text.String()
}

func intPtr(value int) *int {
	return &value
}

func openReviewCommentEditor(t *testing.T, viewer *diffViewer) Command {
	t.Helper()
	key := vaxis.Key{Text: "i", Keycode: 'i'}
	if viewer.mode == modeVisual || viewer.mode == modeVisualLine {
		key = vaxis.Key{Text: "I", Keycode: 'I'}
	}
	cmd, err := viewer.HandleEvent(key)
	if err != nil {
		t.Fatal(err)
	}
	if viewer.editor == nil {
		t.Fatal("comment editor is nil after open")
	}
	if viewer.mode != modeInsert {
		t.Fatalf("mode = %v, want insert", viewer.mode)
	}
	return cmd
}

func submitReviewComment(t *testing.T, viewer *diffViewer, body string) Command {
	t.Helper()
	if body != "" {
		cmd, err := viewer.HandleEvent(vaxis.Key{Text: body})
		if err != nil {
			t.Fatal(err)
		}
		if cmd != CommandRedraw {
			t.Fatalf("text command = %v, want %v", cmd, CommandRedraw)
		}
	}
	cmd, err := viewer.HandleEvent(vaxis.Key{Keycode: vaxis.KeyEsc})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("escape command = %v, want %v", cmd, CommandRedraw)
	}
	cmd, err = viewer.HandleEvent(vaxis.Key{Keycode: vaxis.KeyEsc})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("submit escape command = %v, want %v", cmd, CommandRedraw)
	}
	return cmd
}

func executeCommand(t *testing.T, viewer *diffViewer, command string) Command {
	t.Helper()
	cmd, err := viewer.HandleEvent(vaxis.Key{Text: ":", Keycode: ':'})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != CommandRedraw {
		t.Fatalf("colon command = %v, want %v", cmd, CommandRedraw)
	}
	if viewer.mode != modeCommand {
		t.Fatalf("mode = %v, want command", viewer.mode)
	}
	if command != "" {
		cmd, err = viewer.HandleEvent(vaxis.Key{Text: command})
		if err != nil {
			t.Fatal(err)
		}
		if cmd != CommandRedraw {
			t.Fatalf("command text result = %v, want %v", cmd, CommandRedraw)
		}
	}
	cmd, err = viewer.HandleEvent(vaxis.Key{Keycode: vaxis.KeyEnter})
	if err != nil {
		t.Fatal(err)
	}
	return cmd
}

type testCells map[int]vaxis.Cell

func (t testCells) SetCell(col int, row int, cell vaxis.Cell) {
	t[col] = cell
}

func (t testCells) text(width int) string {
	var b strings.Builder
	for col := 0; col < width; col++ {
		b.WriteString(t[col].Grapheme)
	}
	return b.String()
}

func TestApplyInlineSpans(t *testing.T) {
	base := vaxis.Style{
		Foreground: vaxis.RGBColor(1, 2, 3),
		Background: vaxis.RGBColor(4, 5, 6),
	}
	inlineBackground := vaxis.RGBColor(7, 8, 9)

	segments := applyInlineSpans([]vaxis.Segment{
		{Text: "foo ", Style: base},
		{Text: "bar", Style: base},
	}, []diff.InlineSpan{
		{Start: 4, End: 7, Kind: diff.InlineChange},
	}, inlineBackground)

	if len(segments) != 2 {
		t.Fatalf("segments = %+v, want 2 segments", segments)
	}
	if segments[0].Text != "foo " || segments[0].Style.Background != base.Background {
		t.Fatalf("first segment = %+v", segments[0])
	}
	if segments[1].Text != "bar" || segments[1].Style.Background != inlineBackground {
		t.Fatalf("second segment = %+v", segments[1])
	}
}
