package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.rockorager.dev/vaxis"
	vui "go.rockorager.dev/vaxis/ui"

	"go.rockorager.dev/comview/diff"
	"go.rockorager.dev/comview/review"
)

func uiDiffTestTheme() vui.Theme {
	return uiThemeFromBaseColors(DefaultBaseColors())
}

func newUIDiffTestApp(rows []diff.Row, wrap bool) *vui.App {
	base := DefaultBaseColors()
	return newUIDiffTestAppWithBase(rows, base, wrap)
}

func newUIDiffTestAppWithBase(rows []diff.Row, base BaseColors, wrap bool) *vui.App {
	return newUIDiffTestAppWithBaseAndDrafts(rows, base, wrap, nil)
}

func newUIDiffTestAppWithBaseAndDrafts(rows []diff.Row, base BaseColors, wrap bool, drafts []review.CommentDraft) *vui.App {
	return newUIDiffTestAppWithBaseDraftsAndStatus(rows, base, wrap, drafts, false)
}

func newUIDiffTestAppWithBaseDraftsAndStatus(rows []diff.Row, base BaseColors, wrap bool, drafts []review.CommentDraft, showStatus bool) *vui.App {
	theme := uiThemeFromBaseColors(base)
	return vui.NewApp(uiDiffRootWithStatus(rows, wrap, drafts, showStatus), vui.WithTheme(theme))
}

func newUIDiffTestAppWithReviewFile(rows []diff.Row, drafts []review.CommentDraft, reviewFile string) *vui.App {
	return vui.NewApp(uiDiffRootWithReviewFile(rows, false, drafts, reviewFile, true), vui.WithTheme(uiDiffTestTheme()))
}

func newUIDiffTestAppWithBindings(rows []diff.Row, bindings map[string][]string) *vui.App {
	return vui.NewApp(uiDiffRootWithReviewFileAndBindings(rows, false, nil, "", true, newBindings(bindings)), vui.WithTheme(uiDiffTestTheme()))
}

func TestUIDiffViewRendersRowsAsSliverTable(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "same"},
		{Kind: diff.RowDelete, Gutter: "2     ", Marker: "-", Code: "old"},
		{Kind: diff.RowAdd, Gutter: "  2   ", Marker: "+", Code: "new"},
	}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 20, Height: 3})
	app.Pump(vui.Size{Width: 20, Height: 3})

	p := vui.NewPainter(vui.Size{Width: 20, Height: 3})
	app.Paint(p)
	if got := p.Cell(0, 0).Grapheme; got != "1" {
		t.Fatalf("old gutter = %q, want 1", got)
	}
	if got := p.Cell(2, 0).Grapheme; got != "1" {
		t.Fatalf("new gutter = %q, want 1", got)
	}
	if got := p.Cell(6, 0).Grapheme; got != "s" {
		t.Fatalf("code start = %q, want s", got)
	}
	if got := p.Cell(4, 1).Grapheme; got != "-" {
		t.Fatalf("delete marker = %q, want -", got)
	}
	if got := p.Cell(4, 2).Grapheme; got != "+" {
		t.Fatalf("add marker = %q, want +", got)
	}
}

func TestUIDiffViewHighlightsInlineSpans(t *testing.T) {
	theme := uiDiffTestTheme()
	// A leading context row keeps the default cursor off the changed rows so they
	// render with their plain add/delete backgrounds rather than the cursor row tone.
	// Code offset is six cells: oldWidth(1) + 1 + newWidth(1) + 1 + marker(1) + 1.
	// InlineSpan bytes 7..15 cover "oldValue"/"newValue" (all ASCII, so byte == cell),
	// landing at screen columns 13..20 inclusive.
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "context"},
		{Kind: diff.RowDelete, Gutter: "2     ", Marker: "-", Code: "foo := oldValue + 1", InlineSpans: []diff.InlineSpan{{Start: 7, End: 15, Kind: diff.InlineChange}}},
		{Kind: diff.RowAdd, Gutter: "  2   ", Marker: "+", Code: "foo := newValue + 1", InlineSpans: []diff.InlineSpan{{Start: 7, End: 15, Kind: diff.InlineChange}}},
	}
	app := newUIDiffTestApp(rows, false)
	size := vui.Size{Width: 40, Height: 3}
	app.Pump(size)
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)

	deleteEmphasis := uiDiffInlineSpanBackground(diff.RowDelete, theme)
	if deleteEmphasis == uiDiffLineBackground(theme, theme.Palette.Red) {
		t.Fatalf("delete emphasis background %v must differ from delete row background", deleteEmphasis)
	}
	if got := p.Cell(15, 1).Background; got != deleteEmphasis {
		t.Fatalf("delete in-span background = %v, want emphasis %v", got, deleteEmphasis)
	}
	if got := p.Cell(6, 1).Background; got != theme.Palette.Red.Tone950 {
		t.Fatalf("delete out-of-span background = %v, want base delete %v", got, theme.Palette.Red.Tone950)
	}

	addEmphasis := uiDiffInlineSpanBackground(diff.RowAdd, theme)
	if addEmphasis == theme.Surface {
		t.Fatalf("add emphasis background %v must differ from add row background %v", addEmphasis, theme.Surface)
	}
	if got := p.Cell(15, 2).Background; got != addEmphasis {
		t.Fatalf("add in-span background = %v, want emphasis %v", got, addEmphasis)
	}
	if got := p.Cell(6, 2).Background; got != theme.Surface {
		t.Fatalf("add out-of-span background = %v, want base add surface %v", got, theme.Surface)
	}
}

// uiDiffRowColumnOf returns the first screen column on the painter row whose
// grapheme equals want. It lets the inline-span tests locate a changed-word cell
// without hardcoding gutter widths, which differ between unified and side-by-side
// layouts.
func uiDiffRowColumnOf(p *vui.Painter, row int, want string) (int, bool) {
	for col := 0; col < p.Size().Width; col++ {
		if p.Cell(col, row).Grapheme == want {
			return col, true
		}
	}
	return 0, false
}

func TestUIDiffViewHighlightsInlineSpansSideBySide(t *testing.T) {
	theme := uiDiffTestTheme()
	// The fix touches buildSideBySideCell; render the same delete/add pair after
	// toggling to side-by-side (send 's') and assert the inline emphasis lands on
	// the changed word. A leading context row holds the cursor so the changed pair
	// renders with its plain add/delete tones, not the active cursor-row tone. The
	// delete/add pair is paired by similarity onto one visual row, delete on the
	// left pane and add on the right.
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "context"},
		{Kind: diff.RowDelete, Gutter: "2     - ", Marker: "-", Code: "foo := oldValue + 1", InlineSpans: []diff.InlineSpan{{Start: 7, End: 15, Kind: diff.InlineChange}}},
		{Kind: diff.RowAdd, Gutter: "    2 + ", Marker: "+", Code: "foo := newValue + 1", InlineSpans: []diff.InlineSpan{{Start: 7, End: 15, Kind: diff.InlineChange}}},
	}
	app := newUIDiffTestApp(rows, false)
	size := vui.Size{Width: 60, Height: 3}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "s", Keycode: 's'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)

	deleteEmphasis := uiDiffInlineSpanBackground(diff.RowDelete, theme)
	addEmphasis := uiDiffInlineSpanBackground(diff.RowAdd, theme)

	// The delete/add pair lands on visual row 1 (row 0 is the context row holding
	// the cursor). Locate the changed word by grapheme: 'V' starts "oldValue"/
	// "newValue", inside the span; the 'f' of "foo" sits before the span.
	const pairRow = 1
	deleteSpanCol, ok := uiDiffRowColumnOf(p, pairRow, "V")
	if !ok {
		t.Fatalf("side-by-side delete row = %q, want changed word", uiDiffPainterText(p, pairRow))
	}
	if got := p.Cell(deleteSpanCol, pairRow).Background; got != deleteEmphasis {
		t.Fatalf("side-by-side delete in-span background = %v, want emphasis %v", got, deleteEmphasis)
	}
	// The 'f' of "foo" on the left pane is before the span -> base delete tone.
	deleteOutCol, ok := uiDiffRowColumnOf(p, pairRow, "f")
	if !ok {
		t.Fatalf("side-by-side delete row = %q, want leading word", uiDiffPainterText(p, pairRow))
	}
	if got := p.Cell(deleteOutCol, pairRow).Background; got != theme.Palette.Red.Tone950 {
		t.Fatalf("side-by-side delete out-of-span background = %v, want base delete %v", got, theme.Palette.Red.Tone950)
	}

	// The add pane sits to the right; search for the second 'V' (right of the first).
	addSpanCol := -1
	for col := deleteSpanCol + 1; col < p.Size().Width; col++ {
		if p.Cell(col, pairRow).Grapheme == "V" {
			addSpanCol = col
			break
		}
	}
	if addSpanCol < 0 {
		t.Fatalf("side-by-side add row = %q, want changed word on right pane", uiDiffPainterText(p, pairRow))
	}
	if got := p.Cell(addSpanCol, pairRow).Background; got != addEmphasis {
		t.Fatalf("side-by-side add in-span background = %v, want emphasis %v", got, addEmphasis)
	}
	// An out-of-span add cell ('f' of the right "foo") stays at the add base.
	addOutCol := -1
	for col := deleteOutCol + 1; col < p.Size().Width; col++ {
		if p.Cell(col, pairRow).Grapheme == "f" {
			addOutCol = col
			break
		}
	}
	if addOutCol < 0 {
		t.Fatalf("side-by-side add row = %q, want leading word on right pane", uiDiffPainterText(p, pairRow))
	}
	if got := p.Cell(addOutCol, pairRow).Background; got == addEmphasis {
		t.Fatalf("side-by-side add out-of-span background = %v, must not equal emphasis %v", got, addEmphasis)
	}
}

func TestUIDiffViewInlineSpansHandleTabsAndWideRunes(t *testing.T) {
	theme := uiDiffTestTheme()
	// Pin the byte->cell conversion: a naive byte-index implementation would land
	// the span on the wrong cell. Code "\t한aX" with tabWidth 4 (FileName f.txt):
	// \t=byte0 (1 byte, expands to cols 0-3), 한=bytes1-3 (3 bytes, width 2,
	// cols 4-5), a=byte4 (col 6), X=byte5 (col 7). InlineSpan{5,6} covers only 'X',
	// so the 'X' cell gets add emphasis and the preceding 'a' cell stays base.
	rows := []diff.Row{
		{Kind: diff.RowAdd, Gutter: "  1 + ", Marker: "+", FileName: "f.txt", Code: "\t한aX", InlineSpans: []diff.InlineSpan{{Start: 5, End: 6, Kind: diff.InlineChange}}},
	}
	app := newUIDiffTestApp(rows, false)
	size := vui.Size{Width: 40, Height: 1}
	app.Pump(size)
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)

	addEmphasis := uiDiffInlineSpanBackground(diff.RowAdd, theme)

	xCol, ok := uiDiffRowColumnOf(p, 0, "X")
	if !ok {
		t.Fatalf("tab/CJK row = %q, want trailing X", uiDiffPainterText(p, 0))
	}
	if got := p.Cell(xCol, 0).Background; got != addEmphasis {
		t.Fatalf("tab/CJK in-span 'X' background = %v, want emphasis %v", got, addEmphasis)
	}
	aCol, ok := uiDiffRowColumnOf(p, 0, "a")
	if !ok {
		t.Fatalf("tab/CJK row = %q, want 'a' before span", uiDiffPainterText(p, 0))
	}
	if got := p.Cell(aCol, 0).Background; got == addEmphasis {
		t.Fatalf("tab/CJK out-of-span 'a' background = %v, must not equal emphasis %v", got, addEmphasis)
	}
	// The wide '한' rendered before 'a' confirms tab expansion + width-2 handling.
	// It occupies two cells, so the painter places the grapheme at aCol-2 and a
	// blank continuation at aCol-1.
	if got := p.Cell(aCol-2, 0).Grapheme; got != "한" {
		t.Fatalf("cell two before 'a' = %q, want wide 한 (tab+wide expansion)", got)
	}
}

func TestUIDiffViewInlineSpanBoundariesAreExact(t *testing.T) {
	theme := uiDiffTestTheme()
	// Off-by-one guard: span bytes 7..15 cover exactly "oldValue". The cell just
	// before (':'/' ' at the 'e' of " := ") and just after (the space after the
	// word) must be base, while the first and last span cells are emphasized.
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "context"},
		{Kind: diff.RowDelete, Gutter: "2     ", Marker: "-", Code: "foo := oldValue + 1", InlineSpans: []diff.InlineSpan{{Start: 7, End: 15, Kind: diff.InlineChange}}},
	}
	app := newUIDiffTestApp(rows, false)
	size := vui.Size{Width: 40, Height: 2}
	app.Pump(size)
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)

	emphasis := uiDiffInlineSpanBackground(diff.RowDelete, theme)
	base := theme.Palette.Red.Tone950
	// All ASCII -> cell column == byte offset + code start. Locate code start by
	// finding 'o' of "oldValue" (the first 'o' after "foo " is the span start; use
	// the 'V' anchor instead to be unambiguous, then walk back to span start).
	vCol, ok := uiDiffRowColumnOf(p, 1, "V")
	if !ok {
		t.Fatalf("boundary row = %q, want changed word", uiDiffPainterText(p, 1))
	}
	spanStart := vCol - 3 // 'o','l','d' precede 'V' within "oldValue"
	spanEnd := vCol + 4   // 'a','l','u','e' follow 'V'; last span cell is 'e'
	if got := p.Cell(spanStart, 1).Background; got != emphasis {
		t.Fatalf("first span cell background = %v, want emphasis %v", got, emphasis)
	}
	if got := p.Cell(spanEnd, 1).Background; got != emphasis {
		t.Fatalf("last span cell background = %v, want emphasis %v", got, emphasis)
	}
	if got := p.Cell(spanStart-1, 1).Background; got != base {
		t.Fatalf("cell just before span = %v, want base %v", got, base)
	}
	if got := p.Cell(spanEnd+1, 1).Background; got != base {
		t.Fatalf("cell just after span = %v, want base %v", got, base)
	}
}

func TestUIDiffViewChangedRowWithoutSpansIsUniform(t *testing.T) {
	theme := uiDiffTestTheme()
	// A changed row carrying no InlineSpans must render a uniform base background;
	// none of its code cells should pick up the emphasis tone.
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "context"},
		{Kind: diff.RowDelete, Gutter: "2     ", Marker: "-", Code: "foo := oldValue + 1"},
	}
	app := newUIDiffTestApp(rows, false)
	size := vui.Size{Width: 40, Height: 2}
	app.Pump(size)
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)

	emphasis := uiDiffInlineSpanBackground(diff.RowDelete, theme)
	base := theme.Palette.Red.Tone950
	for col := 6; col < 6+len("foo := oldValue + 1"); col++ {
		got := p.Cell(col, 1).Background
		if got == emphasis {
			t.Fatalf("no-span row col %d background = emphasis %v, want uniform base", col, emphasis)
		}
		if got != base {
			t.Fatalf("no-span row col %d background = %v, want base %v", col, got, base)
		}
	}
}

func TestUIDiffViewHighlightsMultipleInlineSpans(t *testing.T) {
	theme := uiDiffTestTheme()
	// Two disjoint spans on one row: bytes 0..3 ("aaa") and bytes 8..11 ("ccc"),
	// with "bbb" (bytes 4..7, including the leading space) in the gap. Both windows
	// are emphasized; the gap is base.
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "context"},
		{Kind: diff.RowAdd, Gutter: "  2   ", Marker: "+", Code: "aaa bbb ccc", InlineSpans: []diff.InlineSpan{
			{Start: 0, End: 3, Kind: diff.InlineChange},
			{Start: 8, End: 11, Kind: diff.InlineChange},
		}},
	}
	app := newUIDiffTestApp(rows, false)
	size := vui.Size{Width: 40, Height: 2}
	app.Pump(size)
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)

	emphasis := uiDiffInlineSpanBackground(diff.RowAdd, theme)
	base := theme.Surface

	codeStart, ok := uiDiffRowColumnOf(p, 1, "a")
	if !ok {
		t.Fatalf("multi-span row = %q, want first word", uiDiffPainterText(p, 1))
	}
	// First span "aaa" at codeStart..codeStart+2.
	for col := codeStart; col < codeStart+3; col++ {
		if got := p.Cell(col, 1).Background; got != emphasis {
			t.Fatalf("first-span col %d background = %v, want emphasis %v", col, got, emphasis)
		}
	}
	// Gap " bbb " (cols codeStart+3 .. codeStart+7) is base.
	for col := codeStart + 3; col < codeStart+8; col++ {
		if got := p.Cell(col, 1).Background; got != base {
			t.Fatalf("gap col %d background = %v, want base %v", col, got, base)
		}
	}
	// Second span "ccc" at codeStart+8 .. codeStart+10.
	for col := codeStart + 8; col < codeStart+11; col++ {
		if got := p.Cell(col, 1).Background; got != emphasis {
			t.Fatalf("second-span col %d background = %v, want emphasis %v", col, got, emphasis)
		}
	}
}

func TestUIDiffViewStatusBar(t *testing.T) {
	app := newUIDiffTestAppWithBaseDraftsAndStatus([]diff.Row{{Kind: diff.RowContext, Text: "line"}}, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 20, Height: 2})
	app.Pump(vui.Size{Width: 20, Height: 2})

	p := vui.NewPainter(vui.Size{Width: 20, Height: 2})
	app.Paint(p)
	if got := uiDiffPainterText(p, 1); got != " NORMAL " {
		t.Fatalf("status bar = %q, want NORMAL segment", got)
	}
}

func TestUIDiffViewStatusBarShowsFileAndStats(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowFile, Text: "src/main.go"},
		{Kind: diff.RowAdd, Gutter: "1 1   ", Code: "new"},
		{Kind: diff.RowDelete, Gutter: "2 2   ", Code: "old"},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 40, Height: 3})
	app.Pump(vui.Size{Width: 40, Height: 3})

	p := vui.NewPainter(vui.Size{Width: 40, Height: 3})
	app.Paint(p)
	if got := uiDiffPainterText(p, 2); got != " NORMAL  src/main.go  +1 -1     +1 -1" {
		t.Fatalf("status bar = %q, want file context", got)
	}
	if got := p.Cell(8, 2).Background; got != uiDiffStatusBackground(uiDiffTestTheme()) {
		t.Fatalf("mode separator background = %v, want following status background", got)
	}
	if got := p.Cell(34, 2).Grapheme; got != "+" {
		t.Fatalf("right diffstat start = %q, want +", got)
	}
}

func TestRowsStatsPrefersDiffStatSummary(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowDiffStat, Stat: diff.Stat{Adds: 12, Deletes: 0}},
		{Kind: diff.RowDiffStat, Stat: diff.Stat{Adds: 10, Deletes: 8}},
		{Kind: diff.RowDiffStatSummary, Stat: diff.Stat{Adds: 100, Deletes: 80}},
	}

	if got, want := rowsStats(rows), (statusStats{Adds: 100, Deletes: 80}); got != want {
		t.Fatalf("stats = %s, want %s", got, want)
	}
}

func TestRowsStatsDoesNotDoubleCountStatSummaryAndPatchRows(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowDiffStat, Stat: diff.Stat{Adds: 1, Deletes: 1}},
		{Kind: diff.RowDiffStatSummary, Stat: diff.Stat{Adds: 1, Deletes: 1}},
		{Kind: diff.RowAdd},
		{Kind: diff.RowDelete},
	}

	if got, want := rowsStats(rows), (statusStats{Adds: 1, Deletes: 1}); got != want {
		t.Fatalf("stats = %s, want %s", got, want)
	}
}

func TestUIDiffViewFileFinderItemsIncludeDiffStatFiles(t *testing.T) {
	rows, err := rowsForInput(` README.md        |  1 +
 tui/app.go       | 12 ++++++------
 2 files changed, 7 insertions(+), 6 deletions(-)
`)
	if err != nil {
		t.Fatal(err)
	}

	items := uiDiffFileFinderItems(rows)
	if len(items) != 2 {
		t.Fatalf("items = %+v, want 2", items)
	}
	if items[1].Label != "tui/app.go" || items[1].Detail != "+6 -6" {
		t.Fatalf("second item = %+v", items[1])
	}
}

func TestUIDiffViewFileFinderJumpsToSelectedFile(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowFile, Text: "first.go"},
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "first"},
		{Kind: diff.RowFile, Text: "second.go"},
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "second"},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 60, Height: 10})
	app.Pump(vui.Size{Width: 60, Height: 10})

	app.Send(vaxis.Key{Text: " ", Keycode: vaxis.KeySpace})
	app.Send(vaxis.Key{Text: "e", Keycode: 'e'})
	app.Pump(vui.Size{Width: 60, Height: 10})
	p := vui.NewPainter(vui.Size{Width: 60, Height: 10})
	app.Paint(p)
	if _, _, ok := uiDiffFindText(p, "Find file…"); !ok {
		t.Fatal("file finder did not open")
	}

	app.Send(vaxis.Key{Text: "second"})
	app.Pump(vui.Size{Width: 60, Height: 10})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(vui.Size{Width: 60, Height: 10})
	app.Pump(vui.Size{Width: 60, Height: 10})
	p = vui.NewPainter(vui.Size{Width: 60, Height: 10})
	app.Paint(p)
	if got := uiDiffPainterText(p, 2); got != "second.go" {
		t.Fatalf("selected row = %q, want second.go", got)
	}
	if got := uiDiffPainterText(p, 9); got != " NORMAL  2/2 second.go  +0 -0" {
		t.Fatalf("status row = %q, want second file status", got)
	}
}

func TestUIDiffViewFileFinderEscapeKeepsCursor(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowFile, Text: "first.go"},
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "first"},
		{Kind: diff.RowFile, Text: "second.go"},
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "second"},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 60, Height: 10}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: " ", Keycode: vaxis.KeySpace})
	app.Send(vaxis.Key{Text: "e", Keycode: 'e'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "second"})
	app.Pump(size)
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	if _, _, ok := uiDiffFindText(p, "Find file…"); ok {
		t.Fatal("file finder stayed visible after escape")
	}
	if got := uiDiffPainterText(p, size.Height-1); got != " NORMAL  1/2 first.go  +0 -0" {
		t.Fatalf("status row = %q, want first file status", got)
	}
}

func TestUIDiffViewFileFinderDoesNotOpenWithoutFiles(t *testing.T) {
	app := newUIDiffTestAppWithBaseDraftsAndStatus([]diff.Row{{Kind: diff.RowContext, Text: "line"}}, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 40, Height: 4}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: " ", Keycode: vaxis.KeySpace})
	app.Send(vaxis.Key{Text: "e", Keycode: 'e'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if _, _, ok := uiDiffFindText(p, "Find file…"); ok {
		t.Fatal("file finder opened without file rows")
	}
}

func TestUIDiffViewFileFinderConsumesDiffKeys(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowFile, Text: "first.go"},
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "first"},
		{Kind: diff.RowFile, Text: "second.go"},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 60, Height: 8}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: " ", Keycode: vaxis.KeySpace})
	app.Send(vaxis.Key{Text: "e", Keycode: 'e'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "q"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(size)
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(size)

	if app.ShouldQuit() {
		t.Fatal("file finder leaked :q to diff view")
	}
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, size.Height-1); got != " NORMAL  1/2 first.go  +0 -0" {
		t.Fatalf("status row = %q, want cursor unchanged", got)
	}
}

func TestUIDiffViewCommentFinderOpensWithIndexedComments(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Gutter: "1 1   ", Code: "first", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "2 2   ", Code: "second", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
	}
	drafts := []review.CommentDraft{{Path: "main.go", Line: 2, Side: review.SideRight, Body: "  second body preview\nmore detail"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, drafts, true)
	size := vui.Size{Width: 80, Height: 8}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: " ", Keycode: vaxis.KeySpace})
	app.Send(vaxis.Key{Text: "n", Keycode: 'n'})
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	if _, _, ok := uiDiffFindText(p, "Find comment…"); !ok {
		t.Fatal("comment finder did not open")
	}
	if _, _, ok := uiDiffFindText(p, "main.go:2"); !ok {
		t.Fatal("comment finder item omitted comment location")
	}
	if _, _, ok := uiDiffFindText(p, "second body preview"); !ok {
		t.Fatal("comment finder item omitted body preview")
	}
}

func TestUIDiffViewCommentFinderJumpsToSelectedComment(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Gutter: "1 1   ", Code: "alpha", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "2 2   ", Code: "middle", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "3 3   ", Code: "beta", Review: review.Anchor{Path: "main.go", Line: 3, Side: review.SideRight}},
	}
	drafts := []review.CommentDraft{
		{Path: "main.go", Line: 1, Side: review.SideRight, Body: "alpha note"},
		{Path: "main.go", Line: 3, Side: review.SideRight, Body: "beta selected note"},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, drafts, true)
	size := vui.Size{Width: 80, Height: 8}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: " ", Keycode: vaxis.KeySpace})
	app.Send(vaxis.Key{Text: "n", Keycode: 'n'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "beta"})
	app.Pump(size)
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(size)
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	col, row, ok := uiDiffFindText(p, "3 3   beta")
	if !ok {
		t.Fatal("selected comment target row is not visible")
	}
	if got := p.Cell(col, row).Background; got != uiDiffCursorRowBackground(uiDiffTestTheme()) {
		t.Fatalf("selected comment target background = %v, want cursor row background", got)
	}
	if _, _, ok := uiDiffFindText(p, "Find comment…"); ok {
		t.Fatal("comment finder stayed visible after selection")
	}
}

func TestUIDiffViewCommentFinderDoesNotOpenWithoutIndexedComments(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Gutter: "1 1   ", Code: "first", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
	}
	drafts := []review.CommentDraft{{Path: "main.go", Line: 99, Side: review.SideRight, Body: "unmatched"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, drafts, true)
	size := vui.Size{Width: 60, Height: 6}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: " ", Keycode: vaxis.KeySpace})
	app.Send(vaxis.Key{Text: "n", Keycode: 'n'})
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	if _, _, ok := uiDiffFindText(p, "Find comment…"); ok {
		t.Fatal("comment finder opened without indexed comments")
	}
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 0 {
		t.Fatalf("cursor row = %d, want unchanged row 0", got)
	}
}

func TestUIDiffViewCommentFinderConsumesDiffKeys(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Gutter: "1 1   ", Code: "first", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "2 2   ", Code: "second", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
	}
	drafts := []review.CommentDraft{{Path: "main.go", Line: 1, Side: review.SideRight, Body: "first note"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, drafts, true)
	size := vui.Size{Width: 60, Height: 7}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: " ", Keycode: vaxis.KeySpace})
	app.Send(vaxis.Key{Text: "n", Keycode: 'n'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "q"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(size)
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(size)

	if app.ShouldQuit() {
		t.Fatal("comment finder leaked :q to diff view")
	}
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 0 {
		t.Fatalf("cursor row = %d, want unchanged row 0", got)
	}
}

func TestUIDiffViewFileFinderStatsAreColorized(t *testing.T) {
	theme := uiDiffTestTheme()
	app := vui.NewApp(vui.Provider[vui.Theme]{Value: theme, Child: uiDiffFileStatWidget("+1 -1", theme)})
	app.Pump(vui.Size{Width: 10, Height: 1})
	p := vui.NewPainter(vui.Size{Width: 10, Height: 1})
	app.Paint(p)

	if got := uiDiffPainterText(p, 0); got != "+1 -1" {
		t.Fatalf("stat text = %q, want +1 -1", got)
	}
	if got := p.Cell(0, 0).Foreground; got != theme.Palette.Green.Tone500 {
		t.Fatalf("add stat foreground = %v, want green tone500 %v", got, theme.Palette.Green.Tone500)
	}
	if got := p.Cell(3, 0).Foreground; got != theme.Palette.Red.Tone500 {
		t.Fatalf("delete stat foreground = %v, want red tone500 %v", got, theme.Palette.Red.Tone500)
	}
}

func TestUIDiffViewQuestionTogglesHelpOverlay(t *testing.T) {
	app := newUIDiffTestAppWithBaseDraftsAndStatus([]diff.Row{{Kind: diff.RowContext, Code: "line"}}, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 80, Height: len(helpKeybinds) + 6}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "/", Keycode: '/', Modifiers: vaxis.ModShift})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if _, _, ok := uiDiffFindText(p, "Keybinds"); !ok {
		t.Fatal("help overlay did not render")
	}
	if _, _, ok := uiDiffFindText(p, "Open cursor location in editor"); !ok {
		t.Fatal("help overlay did not include legacy keybinds")
	}
	if _, _, ok := uiDiffFindText(p, "╭"); ok {
		t.Fatal("help overlay rendered border chrome")
	}

	app.Send(vaxis.Key{Text: "?", Keycode: '?'})
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	if _, _, ok := uiDiffFindText(p, "Keybinds"); ok {
		t.Fatal("help overlay stayed visible after ?")
	}
}

func TestUIDiffViewHelpOverlayClosesWithEscapeAndQ(t *testing.T) {
	tests := []struct {
		name string
		key  vaxis.Key
	}{
		{name: "escape", key: vaxis.Key{Keycode: vaxis.KeyEsc}},
		{name: "q", key: vaxis.Key{Text: "q", Keycode: 'q'}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newUIDiffTestAppWithBaseDraftsAndStatus([]diff.Row{{Kind: diff.RowContext, Code: "line"}}, DefaultBaseColors(), false, nil, true)
			size := vui.Size{Width: 80, Height: len(helpKeybinds) + 6}
			app.Pump(size)
			app.Send(vaxis.Key{Text: "?", Keycode: '?'})
			app.Pump(size)
			app.Send(tt.key)
			app.Pump(size)

			p := vui.NewPainter(size)
			app.Paint(p)
			if _, _, ok := uiDiffFindText(p, "Keybinds"); ok {
				t.Fatal("help overlay stayed visible")
			}
		})
	}
}

func TestUIDiffViewThemePickerSelectsTheme(t *testing.T) {
	app := newUIDiffTestAppWithBaseDraftsAndStatus([]diff.Row{{Kind: diff.RowContext, Code: "line"}}, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 80, Height: 12}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "t", Keycode: 't'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if _, _, ok := uiDiffFindText(p, "Choose theme"); !ok {
		t.Fatal("theme picker did not render")
	}

	app.Send(vaxis.Key{Text: "latte"})
	app.Pump(size)
	app.Pump(size)
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(size)
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, size.Height-1); !strings.Contains(got, "Theme: Catppuccin Latte") {
		t.Fatalf("status = %q, want selected theme", got)
	}
	selected := uiThemeFromBaseColors(Themes[2].Colors)
	if got := p.Cell(1, 0).Background; got != uiDiffCursorRowBackground(selected) {
		t.Fatalf("cursor row background = %v, want selected theme cursor background %v", got, uiDiffCursorRowBackground(selected))
	}
	if got := p.Cell(size.Width-1, size.Height-2).Background; got != selected.Background {
		t.Fatalf("blank area background = %v, want selected theme background %v", got, selected.Background)
	}
	for _, pt := range []vui.Point{{X: size.Width - 1, Y: 0}, {X: size.Width - 1, Y: size.Height - 2}} {
		if got := p.Cell(pt.X, pt.Y).Background; got != selected.Background {
			t.Fatalf("root background at %#v = %v, want selected theme background %v", pt, got, selected.Background)
		}
	}
}

func TestUIDiffViewThemePickerPreviewsTheme(t *testing.T) {
	state := &uiDiffViewState{}
	items := state.themePreviewFilter("latte", Themes, uiThemeSelectItem)
	if len(items) == 0 {
		t.Fatal("theme filter returned no matches")
	}
	if got, want := state.themeName, "Catppuccin Latte"; got != want {
		t.Fatalf("preview theme = %q, want %q", got, want)
	}
}

func TestUIDiffViewThemePickerNoMatchRestoresPreview(t *testing.T) {
	state := &uiDiffViewState{themeNameBeforePick: "Default", themeName: "Catppuccin Latte"}
	items := state.themePreviewFilter("definitely-no-theme", Themes, uiThemeSelectItem)
	if len(items) != 0 {
		t.Fatalf("theme filter returned %d matches, want none", len(items))
	}
	if got, want := state.themeName, "Default"; got != want {
		t.Fatalf("preview theme = %q, want restored %q", got, want)
	}
}

func TestUIDiffViewThemePickerConsumesDiffKeys(t *testing.T) {
	app := newUIDiffTestAppWithBaseDraftsAndStatus([]diff.Row{{Kind: diff.RowContext, Code: "line"}}, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 80, Height: 12}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "t", Keycode: 't'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "q"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(size)
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(size)

	if app.ShouldQuit() {
		t.Fatal("theme picker leaked :q to diff view")
	}
	p := vui.NewPainter(size)
	app.Paint(p)
	if _, _, ok := uiDiffFindText(p, "Choose theme"); ok {
		t.Fatal("theme picker stayed visible after escape")
	}
}

func TestUIDiffViewThemePickerEscapeKeepsTheme(t *testing.T) {
	app := newUIDiffTestAppWithBaseDraftsAndStatus([]diff.Row{{Kind: diff.RowContext, Code: "line"}}, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 80, Height: 12}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "t", Keycode: 't'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "latte"})
	app.Pump(size)
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if _, _, ok := uiDiffFindText(p, "Choose theme"); ok {
		t.Fatal("theme picker stayed visible after escape")
	}
	if got := p.Cell(1, 0).Background; got != uiDiffCursorRowBackground(uiDiffTestTheme()) {
		t.Fatalf("cursor row background = %v, want original theme", got)
	}
}

func TestUIDiffViewEmptyStateUsesCustomMessage(t *testing.T) {
	root := uiDiffView{EmptyMessage: "No changes.", EmptyHint: "Watching: git diff", ShowStatus: true}
	app := vui.NewApp(root, vui.WithTheme(uiDiffTestTheme()))
	size := vui.Size{Width: 40, Height: 6}
	app.Pump(size)
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	if _, _, ok := uiDiffFindText(p, "No changes."); !ok {
		t.Fatal("empty message did not render")
	}
	if _, _, ok := uiDiffFindText(p, "Watching: git diff"); !ok {
		t.Fatal("empty hint did not render")
	}
}

func TestUIDiffViewShowsExplicitScrollbars(t *testing.T) {
	rows := make([]diff.Row, 20)
	for i := range rows {
		rows[i] = diff.Row{Kind: diff.RowContext, Gutter: "1 1   ", Code: strings.Repeat("x", 80)}
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 20, Height: 6}
	app.Pump(size)
	app.Pump(size)
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	theme := uiDiffTestTheme()
	if got := p.Cell(size.Width-1, 0).Background; got != theme.Border && got != theme.Surface && got != theme.AccentText && got != theme.SurfaceHovered {
		t.Fatalf("vertical scrollbar background = %v, want scrollbar style", got)
	}
	if row := uiDiffPainterText(p, size.Height-2); !strings.Contains(row, horizontalScrollbarThumb) {
		t.Fatalf("horizontal scrollbar row = %q, want thumb", row)
	}
}

func TestUIDiffViewCursorDoesNotShareHorizontalScrollbarRow(t *testing.T) {
	rows := make([]diff.Row, 20)
	for i := range rows {
		rows[i] = diff.Row{Kind: diff.RowContext, Gutter: "1 1   ", Code: strings.Repeat("x", 80)}
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 20, Height: 6}
	app.Pump(size)
	app.Pump(size)
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnd})
	app.Pump(size)
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	highlightRow := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme()))
	if highlightRow < 0 {
		t.Fatal("cursor row is not visible")
	}
	if row := uiDiffPainterText(p, highlightRow); strings.Contains(row, "▄") {
		t.Fatalf("cursor row = %q, want no horizontal scrollbar half cell", row)
	}
}

func TestUIDiffViewHorizontalScrollbarMovesWithXScroll(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Gutter: "1 1   ", Code: strings.Repeat("x", 80)}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 20, Height: 5}
	app.Pump(size)
	app.Pump(size)
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	initial := uiDiffPainterText(p, size.Height-2)
	if !strings.HasPrefix(initial, horizontalScrollbarThumb) {
		t.Fatalf("initial horizontal scrollbar row = %q, want thumb at start", initial)
	}

	app.Send(vaxis.Key{Text: "$", Keycode: '$', ShiftedCode: '4'})
	app.Pump(size)
	app.Pump(size)
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	after := uiDiffPainterText(p, size.Height-2)
	if strings.HasPrefix(after, horizontalScrollbarThumb) {
		t.Fatalf("horizontal scrollbar row after end = %q, want thumb moved right", after)
	}
	if !strings.Contains(after, horizontalScrollbarThumb) {
		t.Fatalf("horizontal scrollbar row after end = %q, want thumb", after)
	}
}

func TestUIDiffViewHorizontalScrollRevealsLastCellBeforeVerticalScrollbar(t *testing.T) {
	rows := make([]diff.Row, 20)
	for i := range rows {
		rows[i] = diff.Row{Kind: diff.RowContext, Gutter: "1 1   ", Code: strings.Repeat("x", 79) + "Z"}
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 20, Height: 6}
	app.Pump(size)
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "$", Keycode: '$', ShiftedCode: '4'})
	app.Pump(size)
	app.Pump(size)
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	if got := p.Cell(size.Width-2, 0).Grapheme; got != "Z" {
		t.Fatalf("last visible code cell = %q, want final cell before vertical scrollbar", got)
	}
	if got := p.Cell(size.Width-1, 0).Grapheme; got == "Z" {
		t.Fatal("final code cell rendered under vertical scrollbar")
	}
}

func TestUIDiffViewHorizontalScrollbarHiddenInWrapMode(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Gutter: "1 1   ", Code: strings.Repeat("x", 80)}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), true, nil, true)
	size := vui.Size{Width: 20, Height: 5}
	app.Pump(size)
	app.Pump(size)
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	if row := uiDiffPainterText(p, size.Height-2); strings.Contains(row, horizontalScrollbarThumb) {
		t.Fatalf("wrapped view horizontal scrollbar row = %q, want no thumb", row)
	}
}

func TestUIDiffViewSideBySideShowsHorizontalScrollbar(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowDelete, Gutter: "1   - ", Marker: "-", Code: strings.Repeat("old", 20)},
		{Kind: diff.RowAdd, Gutter: "  1 + ", Marker: "+", Code: strings.Repeat("new", 20)},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 40, Height: 5}
	app.Pump(size)
	app.Pump(size)
	app.Send(vaxis.Key{Text: "s", Keycode: 's'})
	app.Pump(size)
	app.Pump(size)
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	if row := uiDiffPainterText(p, size.Height-2); !strings.Contains(row, horizontalScrollbarThumb) {
		t.Fatalf("side-by-side horizontal scrollbar row = %q, want thumb", row)
	}
}

func TestUIDiffViewMovesCursorAndRevealsRows(t *testing.T) {
	rows := make([]diff.Row, 20)
	for i := range rows {
		rows[i] = diff.Row{Kind: diff.RowContext, Gutter: "1 1   ", Code: "line"}
	}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 20, Height: 4})
	app.Pump(vui.Size{Width: 20, Height: 4})

	app.Send(vaxis.Key{Text: "G", Keycode: 'G'})
	app.Pump(vui.Size{Width: 20, Height: 4})
	app.Pump(vui.Size{Width: 20, Height: 4})
	p := vui.NewPainter(vui.Size{Width: 20, Height: 4})
	app.Paint(p)
	if got := p.Cell(0, 3).Background; got != uiDiffCursorRowBackground(uiDiffTestTheme()) {
		t.Fatalf("bottom visible row background = %v, want active selection", got)
	}

	app.Send(vaxis.Key{Text: "k", Keycode: 'k'})
	app.Pump(vui.Size{Width: 20, Height: 4})
	p = vui.NewPainter(vui.Size{Width: 20, Height: 4})
	app.Paint(p)
	if got := p.Cell(0, 2).Background; got != uiDiffCursorRowBackground(uiDiffTestTheme()) {
		t.Fatalf("row above bottom background = %v, want active selection", got)
	}
}

func TestUIDiffViewHorizontalScrollKeepsGuttersFixed(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Gutter: "1 1   ", Code: "abcdefghijklmnop"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 14, Height: 3}
	app.Pump(size)
	app.Pump(size)

	for i := 0; i < 9; i++ {
		app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
		app.Pump(size)
	}
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, 0); !strings.HasPrefix(got, "1 1   ") {
		t.Fatalf("row = %q, want fixed gutter prefix", got)
	}
	if got := uiDiffPainterText(p, 0); !strings.Contains(got, "cdefghij") {
		t.Fatalf("row = %q, want horizontally scrolled code", got)
	}
}

func TestUIDiffViewSideBySideToggleRendersDeleteAddPair(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowDelete, Gutter: "1     - ", Marker: "-", Code: "old"},
		{Kind: diff.RowAdd, Gutter: "    1 + ", Marker: "+", Code: "new"},
	}
	app := newUIDiffTestApp(rows, false)
	size := vui.Size{Width: 30, Height: 2}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "s", Keycode: 's'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	row := uiDiffPainterText(p, 0)
	if !strings.Contains(row, "1 - old") {
		t.Fatalf("side-by-side row = %q, want left delete", row)
	}
	if !strings.Contains(row, "1 + new") {
		t.Fatalf("side-by-side row = %q, want right add", row)
	}
}

func TestUIDiffViewSideBySideRowsPairReplacementBlocks(t *testing.T) {
	rows := uiDiffSideBySideRows([]diff.Row{
		{Kind: diff.RowFile, Text: "main.go"},
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "same"},
		{Kind: diff.RowDelete, Gutter: "2     - ", Code: "old one"},
		{Kind: diff.RowDelete, Gutter: "3     - ", Code: "old two"},
		{Kind: diff.RowAdd, Gutter: "    2 + ", Code: "new one"},
		{Kind: diff.RowContext, Gutter: "4 3   ", Code: "after"},
	})

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

func TestUIDiffViewSideBySideRowsUseSimilarityPairing(t *testing.T) {
	rows := uiDiffSideBySideRows([]diff.Row{
		{Kind: diff.RowDelete, Gutter: "1     - ", Code: "foo := oldValue + 1"},
		{Kind: diff.RowDelete, Gutter: "2     - ", Code: "keep()"},
		{Kind: diff.RowAdd, Gutter: "    1 + ", Code: "inserted()"},
		{Kind: diff.RowAdd, Gutter: "    2 + ", Code: "foo := newValue + 1"},
		{Kind: diff.RowAdd, Gutter: "    3 + ", Code: "keep()"},
	})

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

func TestUIDiffViewSideBySideRowsPutAddOnlyHunkContextOnRight(t *testing.T) {
	rows := uiDiffSideBySideRows([]diff.Row{
		{Kind: diff.RowHunk, Text: "@@ -1,2 +1,3 @@"},
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "before"},
		{Kind: diff.RowAdd, Gutter: "    2 + ", Code: "added"},
		{Kind: diff.RowContext, Gutter: "2 3   ", Code: "after"},
	})

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

func TestUIDiffViewSideBySideRowsPutDeleteOnlyHunkContextOnLeft(t *testing.T) {
	rows := uiDiffSideBySideRows([]diff.Row{
		{Kind: diff.RowHunk, Text: "@@ -1,3 +1,2 @@"},
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "before"},
		{Kind: diff.RowDelete, Gutter: "2     - ", Code: "deleted"},
		{Kind: diff.RowContext, Gutter: "3 2   ", Code: "after"},
	})

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

func TestUIDiffViewSideBySideGutterUsesOneSideLineNumbers(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowDelete, Gutter: "10     - ", Code: "old"},
		{Kind: diff.RowAdd, Gutter: "    11 + ", Code: "new"},
		{Kind: diff.RowContext, Gutter: "12 13   ", Code: "same"},
	}

	if got, want := uiDiffSideBySideGutter(rows, rows[0], sideLeft), "10 - "; got != want {
		t.Fatalf("delete left gutter = %q, want %q", got, want)
	}
	if got, want := uiDiffSideBySideGutter(rows, rows[1], sideRight), "11 + "; got != want {
		t.Fatalf("add right gutter = %q, want %q", got, want)
	}
	if got, want := uiDiffSideBySideGutter(rows, rows[2], sideLeft), "12   "; got != want {
		t.Fatalf("context left gutter = %q, want %q", got, want)
	}
	if got, want := uiDiffSideBySideGutter(rows, rows[2], sideRight), "13   "; got != want {
		t.Fatalf("context right gutter = %q, want %q", got, want)
	}
}

func TestUIDiffViewSideBySideRendersAddOnlyHunkOnRight(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowHunk, Text: "@@ -1,2 +1,3 @@"},
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "before"},
		{Kind: diff.RowAdd, Gutter: "    2 + ", Code: "added"},
	}
	app := newUIDiffTestApp(rows, false)
	size := vui.Size{Width: 40, Height: 4}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "s", Keycode: 's'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	col, ok := uiDiffFindTextOnRow(p, 1, "1   before")
	if !ok || col < 20 {
		t.Fatalf("add-only context row = %q, want context on right", uiDiffPainterText(p, 1))
	}
}

func TestUIDiffViewSideBySideRendersDeleteOnlyHunkOnLeft(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowHunk, Text: "@@ -1,3 +1,2 @@"},
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "before"},
		{Kind: diff.RowDelete, Gutter: "2     - ", Code: "deleted"},
	}
	app := newUIDiffTestApp(rows, false)
	size := vui.Size{Width: 40, Height: 4}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "s", Keycode: 's'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	col, ok := uiDiffFindTextOnRow(p, 1, "1   before")
	if !ok || col >= 20 {
		t.Fatalf("delete-only context row = %q, want context on left", uiDiffPainterText(p, 1))
	}
}

func TestUIDiffViewSideBySideHonorsCustomToggleBinding(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowDelete, Gutter: "1     - ", Marker: "-", Code: "old"},
		{Kind: diff.RowAdd, Gutter: "    1 + ", Marker: "+", Code: "new"},
	}
	app := newUIDiffTestAppWithBindings(rows, map[string][]string{
		"toggle_layout": {"z"},
	})
	size := vui.Size{Width: 30, Height: 3}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "s", Keycode: 's'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if row := uiDiffPainterText(p, 0); strings.Contains(row, "new") {
		t.Fatalf("default toggle key switched layout: row = %q", row)
	}

	app.Send(vaxis.Key{Text: "z", Keycode: 'z'})
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	if row := uiDiffPainterText(p, 0); !strings.Contains(row, "old") || !strings.Contains(row, "new") {
		t.Fatalf("custom toggle key did not switch layout: row = %q", row)
	}
}

func TestUIDiffViewSideBySideHighlightsCursorOnOneSide(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Gutter: "1 1   ", Code: "same"}}
	app := newUIDiffTestApp(rows, false)
	size := vui.Size{Width: 30, Height: 2}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "s", Keycode: 's'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	cursorBackground := uiDiffCursorBackground(uiDiffTestTheme())
	if got := p.Cell(4, 0).Background; got == cursorBackground {
		t.Fatalf("left context cell background = cursor, want inactive side")
	}
	if got := p.Cell(19, 0).Background; got != cursorBackground {
		t.Fatalf("right context cell background = %v, want cursor", got)
	}
}

func TestUIDiffViewSideBySideShowsCommentEditor(t *testing.T) {
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
	app := newUIDiffTestApp(rows, false)
	size := vui.Size{Width: 50, Height: 6}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "s", Keycode: 's'})
	app.Pump(size)
	for rowIndex, row := range rows {
		if row.Kind == diff.RowAdd {
			for range rowIndex {
				app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
			}
			break
		}
	}
	app.Pump(size)
	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "Add comment") == -1 {
		t.Fatal("side-by-side comment editor not rendered")
	}
}

func TestUIDiffViewSideBySideHorizontalScrollUsesPaneWidth(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowDelete, Gutter: "1     - ", Code: "old"},
		{Kind: diff.RowAdd, Gutter: "    1 + ", Code: "abcdefghijklmnop"},
	}
	app := newUIDiffTestApp(rows, false)
	size := vui.Size{Width: 30, Height: 3}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "s", Keycode: 's'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: "$", Keycode: '$'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	row := uiDiffPainterText(p, 0)
	if strings.Contains(row, "abcdefghijklmnop") {
		t.Fatalf("side-by-side row = %q, want horizontally clipped code", row)
	}
	if !strings.Contains(row, "ghijklmnop") {
		t.Fatalf("side-by-side row = %q, want line end visible", row)
	}
}

func TestUIDiffViewSideBySideRevealUsesVisualRow(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowDelete, Gutter: "1     - ", Code: "old one"},
		{Kind: diff.RowAdd, Gutter: "    1 + ", Code: "new one"},
		{Kind: diff.RowDelete, Gutter: "2     - ", Code: "old two"},
		{Kind: diff.RowAdd, Gutter: "    2 + ", Code: "new two"},
		{Kind: diff.RowDelete, Gutter: "3     - ", Code: "old three"},
		{Kind: diff.RowAdd, Gutter: "    3 + ", Code: "new three"},
	}
	app := newUIDiffTestApp(rows, false)
	size := vui.Size{Width: 40, Height: 2}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "s", Keycode: 's'})
	app.Pump(size)
	for range 4 {
		app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	}
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if row := uiDiffPainterText(p, 1); !strings.Contains(row, "old three") || !strings.Contains(row, "new three") {
		t.Fatalf("bottom visible side-by-side row = %q, want cursor visual row", row)
	}
}

func TestUIDiffViewSideBySideMouseSelectionUsesPaneCoordinates(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowDelete, Gutter: "1     - ", Code: "old value"},
		{Kind: diff.RowAdd, Gutter: "    1 + ", Code: "new value"},
	}
	app := newUIDiffTestApp(rows, false)
	size := vui.Size{Width: 40, Height: 3}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "s", Keycode: 's'})
	app.Pump(size)
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: 0, Col: 26})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion, Row: 0, Col: 28})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease, Row: 0, Col: 28})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := p.Cell(26, 0).Background; got != uiDiffTestTheme().Selection {
		t.Fatalf("right pane selected cell background = %v, want selection", got)
	}
	if got := p.Cell(6, 0).Background; got == uiDiffTestTheme().Selection {
		t.Fatalf("left pane cell background = selection, want unselected")
	}
}

func TestUIDiffViewDollarAndZeroAdjustHorizontalScroll(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Gutter: "1 1   ", Code: "abcdefghijklmnop"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 14, Height: 3}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "$", Keycode: '$'})
	app.Pump(size)
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, 0); !strings.Contains(got, "ijklmnop") {
		t.Fatalf("row after $ = %q, want line end visible", got)
	}

	app.Send(vaxis.Key{Text: "0", Keycode: '0'})
	app.Pump(size)
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, 0); !strings.Contains(got, "abcdefgh") {
		t.Fatalf("row after 0 = %q, want line start visible", got)
	}
}

func TestUIDiffViewHorizontalMouseWheelScrollsCode(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Gutter: "1 1   ", Code: "abcdefghijklmnop"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 14, Height: 3}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Mouse{Button: mouseWheelRight, EventType: vaxis.EventPress})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, 0); !strings.Contains(got, "bcdefghi") {
		t.Fatalf("row after right wheel = %q, want horizontally scrolled code", got)
	}

	app.Send(vaxis.Mouse{Button: mouseWheelLeft, EventType: vaxis.EventPress})
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, 0); !strings.Contains(got, "abcdefgh") {
		t.Fatalf("row after left wheel = %q, want line start visible", got)
	}
}

func TestUIDiffViewExpandsGoModTabsToEightCells(t *testing.T) {
	segments := uiDiffExpandTabs([]vaxis.Segment{{Text: "\trequire", Style: vaxis.Style{}}}, tabWidthForFile("go.mod"))

	if got := segmentsText(segments); got != "        require" {
		t.Fatalf("expanded go.mod tab = %q, want eight spaces", got)
	}
}

func TestUIDiffViewExpandsGoTabsToEightCells(t *testing.T) {
	segments := uiDiffExpandTabs([]vaxis.Segment{{Text: "\treturn", Style: vaxis.Style{}}}, tabWidthForFile("main.go"))

	if got := segmentsText(segments); got != "        return" {
		t.Fatalf("expanded go tab = %q, want eight spaces", got)
	}
}

func TestUIDiffViewSkipsBlankRowsWhenMovingCursor(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "first"},
		{Kind: diff.RowBlank},
		{Kind: diff.RowContext, Gutter: "2 2   ", Code: "second"},
	}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 20, Height: 3})
	app.Pump(vui.Size{Width: 20, Height: 3})

	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(vui.Size{Width: 20, Height: 3})
	p := vui.NewPainter(vui.Size{Width: 20, Height: 3})
	app.Paint(p)
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 2 {
		t.Fatalf("highlight row after j = %d, want 2", got)
	}

	app.Send(vaxis.Key{Text: "k", Keycode: 'k'})
	app.Pump(vui.Size{Width: 20, Height: 3})
	p = vui.NewPainter(vui.Size{Width: 20, Height: 3})
	app.Paint(p)
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 0 {
		t.Fatalf("highlight row after k = %d, want 0", got)
	}
}

func TestUIDiffViewKeepsCursorVisibleWhenMovingDown(t *testing.T) {
	rows := make([]diff.Row, 12)
	for i := range rows {
		rows[i] = diff.Row{Kind: diff.RowContext, Gutter: "1 1   ", Code: "line"}
	}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 20, Height: 3})
	app.Pump(vui.Size{Width: 20, Height: 3})

	for i := 0; i < 8; i++ {
		app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
		app.Pump(vui.Size{Width: 20, Height: 3})
		app.Pump(vui.Size{Width: 20, Height: 3})
		p := vui.NewPainter(vui.Size{Width: 20, Height: 3})
		app.Paint(p)
		if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got == -1 {
			t.Fatalf("cursor not visible after %d j presses", i+1)
		}
	}
}

func TestUIDiffViewRepeatedCursorDownKeepsTopPainted(t *testing.T) {
	rows := make([]diff.Row, 40)
	for i := range rows {
		rows[i] = diff.Row{Kind: diff.RowContext, Gutter: "1 1   ", Code: fmt.Sprintf("line %02d", i+1)}
	}
	app := newUIDiffTestApp(rows, false)
	size := vui.Size{Width: 20, Height: 5}
	app.Pump(size)
	app.Pump(size)

	for i := 0; i < 20; i++ {
		app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	}
	app.Pump(size)
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	if got := strings.TrimSpace(uiDiffPainterText(p, 0)); got == "" {
		t.Fatal("top visible row is blank after repeated cursor down")
	}
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got == -1 {
		t.Fatal("cursor row is not visible after repeated cursor down")
	}
}

func TestUIDiffViewActiveCodeRowHighlightsToRightEdge(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Gutter: "1 1   ", Code: "short"}}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 20, Height: 1})
	app.Pump(vui.Size{Width: 20, Height: 1})

	p := vui.NewPainter(vui.Size{Width: 20, Height: 1})
	app.Paint(p)
	for col := 0; col < 20; col++ {
		if col == 6 {
			continue
		}
		if got := p.Cell(col, 0).Background; got != uiDiffCursorRowBackground(uiDiffTestTheme()) {
			t.Fatalf("active row background at col %d = %v, want selection", col, got)
		}
	}
}

func TestUIDiffViewChangedRowsHighlightToRightEdge(t *testing.T) {
	theme := uiDiffTestTheme()
	rows := []diff.Row{
		{Kind: diff.RowDelete, Gutter: "1     - ", Code: "old"},
		{Kind: diff.RowAdd, Gutter: "    1 + ", Code: "new"},
	}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 20, Height: 2})
	app.Pump(vui.Size{Width: 20, Height: 2})

	p := vui.NewPainter(vui.Size{Width: 20, Height: 2})
	app.Paint(p)
	if got := p.Cell(19, 0).Background; got != uiDiffCursorRowBackground(theme) {
		t.Fatalf("active delete row right edge background = %v, want selection", got)
	}
	if got := p.Cell(19, 1).Background; got != theme.Surface {
		t.Fatalf("add row right edge background = %v, want surface", got)
	}
}

func TestUIDiffViewUsesStableFixedGutterColumns(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "one"},
		{Kind: diff.RowContext, Gutter: "100 100   ", Code: "hundred"},
	}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 20, Height: 2})
	app.Pump(vui.Size{Width: 20, Height: 2})

	p := vui.NewPainter(vui.Size{Width: 20, Height: 2})
	app.Paint(p)
	if got := p.Cell(10, 0).Grapheme; got != "o" {
		t.Fatalf("first row code start = %q, want o at stable col 10", got)
	}
	if got := p.Cell(10, 1).Grapheme; got != "h" {
		t.Fatalf("second row code start = %q, want h at stable col 10", got)
	}
}

func TestUIDiffViewRendersMetadataOutsideDiffGrid(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowCommitHeader, Text: "commit abc123"},
		{Kind: diff.RowContext, Gutter: "12 34   ", Code: "code"},
	}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 20, Height: 2})
	app.Pump(vui.Size{Width: 20, Height: 2})

	p := vui.NewPainter(vui.Size{Width: 20, Height: 2})
	app.Paint(p)
	if got := p.Cell(0, 0).Grapheme; got != "c" {
		t.Fatalf("metadata row starts at col 0 with %q, want c", got)
	}
	if got := p.Cell(0, 1).Grapheme; got != "1" {
		t.Fatalf("diff row old gutter starts at col 0 with %q, want 1", got)
	}
	if got := p.Cell(8, 1).Grapheme; got != "c" {
		t.Fatalf("diff row code starts after gutter with %q, want c", got)
	}
}

func TestUIDiffViewRendersStructuredFullWidthRows(t *testing.T) {
	base := DefaultBaseColors()
	theme := uiThemeFromBaseColors(base)
	rows := []diff.Row{
		{Kind: diff.RowCommitHeader, Prefix: "commit ", Code: "abc123"},
		{Kind: diff.RowCommitMeta, Prefix: "Author: ", Code: "Example"},
		{Kind: diff.RowHunk, Prefix: "@@ -1 +1 @@", Code: " func"},
	}
	app := newUIDiffTestAppWithBase(rows, base, false)
	app.Pump(vui.Size{Width: 24, Height: 3})
	app.Pump(vui.Size{Width: 24, Height: 3})

	p := vui.NewPainter(vui.Size{Width: 24, Height: 3})
	app.Paint(p)
	if cell := p.Cell(0, 0); cell.Grapheme != "c" || cell.Foreground != uiDiffCursorForeground(theme) {
		t.Fatalf("commit prefix cursor cell = %q/%v, want c/cursor", cell.Grapheme, cell.Foreground)
	}
	if cell := p.Cell(1, 0); cell.Grapheme != "o" || cell.Foreground != theme.DisabledForeground {
		t.Fatalf("commit prefix cell = %q/%v, want o/dim", cell.Grapheme, cell.Foreground)
	}
	if cell := p.Cell(7, 0); cell.Grapheme != "a" || cell.Foreground != theme.Warning {
		t.Fatalf("commit hash cell = %q/%v, want a/yellow", cell.Grapheme, cell.Foreground)
	}
	if cell := p.Cell(0, 1); cell.Grapheme != "A" || cell.Foreground != theme.MutedForeground {
		t.Fatalf("commit meta label = %q/%v, want A/muted", cell.Grapheme, cell.Foreground)
	}
	if cell := p.Cell(8, 1); cell.Grapheme != "E" || cell.Foreground != theme.Palette.Cyan.Tone500 {
		t.Fatalf("commit meta value = %q/%v, want E/cyan", cell.Grapheme, cell.Foreground)
	}
	if cell := p.Cell(0, 2); cell.Grapheme != "@" || cell.Foreground != theme.Accent {
		t.Fatalf("hunk prefix = %q/%v, want @/hunk", cell.Grapheme, cell.Foreground)
	}
	if cell := p.Cell(11, 2); cell.Grapheme != " " || cell.Foreground != theme.DisabledForeground {
		t.Fatalf("hunk suffix = %q/%v, want space/dim", cell.Grapheme, cell.Foreground)
	}
}

func TestUIDiffViewDerivesDiffColorsFromUITheme(t *testing.T) {
	theme := uiThemeFromBaseColors(DefaultBaseColors())
	if style := uiStyleForDiffRow(diff.RowContext, theme); style.Background != theme.Background {
		t.Fatalf("context background = %v, want %v", style.Background, theme.Background)
	}
	if style := uiStyleForDiffRow(diff.RowContext, theme); style.Foreground != theme.MutedForeground {
		t.Fatalf("context foreground = %v, want muted %v", style.Foreground, theme.MutedForeground)
	}
	if style := uiGutterStyle(diff.RowContext, uiDiffCursorRowBackground(theme), theme); style.Background != uiDiffCursorRowBackground(theme) {
		t.Fatalf("active gutter background = %v, want %v", style.Background, uiDiffCursorRowBackground(theme))
	}
	if style := uiGutterStyle(diff.RowContext, 0, theme); style.Background != theme.Background {
		t.Fatalf("gutter background = %v, want %v", style.Background, theme.Background)
	}
	if style := uiStyleForDiffRow(diff.RowAdd, theme); style.Foreground != theme.Success {
		t.Fatalf("add foreground = %v, want %v", style.Foreground, theme.Success)
	}
	if style := uiStyleForDiffRow(diff.RowAdd, theme); style.Background != theme.Surface {
		t.Fatalf("add background = %v, want surface %v", style.Background, theme.Surface)
	}
	if style := uiStyleForDiffRow(diff.RowDelete, theme); style.Foreground != theme.Palette.Red.Tone500 {
		t.Fatalf("delete foreground = %v, want dim red %v", style.Foreground, theme.Palette.Red.Tone500)
	}
	if style := uiStyleForDiffRow(diff.RowDelete, theme); style.Background != theme.Palette.Red.Tone950 {
		t.Fatalf("delete background = %v, want dark red tone", style.Background)
	}
}

func TestUIDiffViewHighlightsCodeWithChroma(t *testing.T) {
	theme := uiDiffTestTheme()
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "    1 + ", Code: "package main", FileName: "main.go"}}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 24, Height: 1})
	app.Pump(vui.Size{Width: 24, Height: 1})

	p := vui.NewPainter(vui.Size{Width: 24, Height: 1})
	app.Paint(p)
	cell := p.Cell(6, 0)
	if cell.Grapheme != "a" {
		t.Fatalf("code glyph = %q, want a", cell.Grapheme)
	}
	if cell.Foreground != theme.Palette.Magenta.Tone500 {
		t.Fatalf("keyword foreground = %v, want magenta tone500 %v", cell.Foreground, theme.Palette.Magenta.Tone500)
	}
	if cell.Background != uiDiffCursorRowBackground(theme) {
		t.Fatalf("keyword background = %v, want selection %v", cell.Background, uiDiffCursorRowBackground(theme))
	}
}

func TestUIDiffViewDimsChromaCodeForContextAndDeletes(t *testing.T) {
	theme := uiDiffTestTheme()
	base := []vaxis.Segment{{Text: "package", Style: vaxis.Style{Foreground: theme.Palette.Magenta.Tone500}}}
	contextSegments := uiDiffToneCodeSegments(diff.RowContext, base, theme)
	deleteSegments := uiDiffToneCodeSegments(diff.RowDelete, base, theme)
	addSegments := uiDiffToneCodeSegments(diff.RowAdd, base, theme)
	if contextSegments[0].Style.Foreground != theme.Palette.Magenta.Tone600 {
		t.Fatalf("context syntax foreground = %v, want magenta tone600", contextSegments[0].Style.Foreground)
	}
	if deleteSegments[0].Style.Foreground != theme.Palette.Magenta.Tone600 {
		t.Fatalf("delete syntax foreground = %v, want magenta tone600", deleteSegments[0].Style.Foreground)
	}
	if addSegments[0].Style.Foreground != base[0].Style.Foreground {
		t.Fatal("add syntax foreground should remain unchanged")
	}
}

func TestUIDiffViewSyntaxColorsUseBrighterPaletteTones(t *testing.T) {
	theme := uiDiffTestTheme()
	colors := (uiSyntaxTheme{Theme: theme}).uiThemeColors()
	if colors.Magenta != theme.Palette.Magenta.Tone500 {
		t.Fatalf("syntax magenta = %v, want tone500 %v", colors.Magenta, theme.Palette.Magenta.Tone500)
	}
	if colors.Blue != theme.Palette.Blue.Tone500 {
		t.Fatalf("syntax blue = %v, want tone500 %v", colors.Blue, theme.Palette.Blue.Tone500)
	}
	if colors.Green != theme.Palette.Green.Tone500 {
		t.Fatalf("syntax green = %v, want tone500 %v", colors.Green, theme.Palette.Green.Tone500)
	}
}

func TestUIDiffViewCursorUsesThemeForeground(t *testing.T) {
	theme := uiDiffTestTheme()
	if got := uiDiffCursorBackground(theme); got != theme.Foreground {
		t.Fatalf("cursor background = %v, want foreground %v", got, theme.Foreground)
	}
	if got := uiDiffCursorForeground(theme); got != theme.Palette.Neutral.Tone950 {
		t.Fatalf("cursor foreground = %v, want dark neutral %v", got, theme.Palette.Neutral.Tone950)
	}
}

func TestUIDiffViewChangedGutterUsesBrighterTone(t *testing.T) {
	theme := uiDiffTestTheme()
	if got := uiGutterStyle(diff.RowAdd, 0, theme).Foreground; got != theme.Palette.Green.Tone400 {
		t.Fatalf("add marker foreground = %v, want green tone400 %v", got, theme.Palette.Green.Tone400)
	}
	if got := uiGutterStyle(diff.RowDelete, 0, theme).Foreground; got != theme.Palette.Red.Tone400 {
		t.Fatalf("delete marker foreground = %v, want red tone400 %v", got, theme.Palette.Red.Tone400)
	}
}

func TestUIDiffViewAddLineNumberGutterUsesSoftGreenForeground(t *testing.T) {
	theme := uiDiffTestTheme()
	if got := uiLineNumberGutterStyle(diff.RowAdd, 0, theme).Foreground; got != theme.Palette.Green.Tone300 {
		t.Fatalf("add line number foreground = %v, want soft green %v", got, theme.Palette.Green.Tone300)
	}
	if got := uiGutterStyle(diff.RowAdd, 0, theme).Foreground; got != theme.Palette.Green.Tone400 {
		t.Fatalf("add marker foreground = %v, want green tone400 %v", got, theme.Palette.Green.Tone400)
	}
}

func TestUIDiffViewVimNavigationKeys(t *testing.T) {
	tests := []struct {
		name          string
		prime         []vaxis.Key
		key           vaxis.Key
		wantHighlight int
	}{
		{
			name:          "Home moves cursor to top",
			prime:         []vaxis.Key{{Text: "G", Keycode: 'G'}},
			key:           vaxis.Key{Keycode: vaxis.KeyHome},
			wantHighlight: 0,
		},
		{
			name:          "End moves cursor to bottom",
			key:           vaxis.Key{Keycode: vaxis.KeyEnd},
			wantHighlight: 3,
		},
		{
			name:          "gg moves cursor to top",
			prime:         []vaxis.Key{{Text: "G", Keycode: 'G'}},
			key:           vaxis.Key{Text: "g", Keycode: 'g'},
			wantHighlight: 0,
		},
		{
			name:          "Ctrl+d moves cursor down half page",
			key:           vaxis.Key{Text: "d", Keycode: 'd', Modifiers: vaxis.ModCtrl},
			wantHighlight: 2,
		},
		{
			name:          "Page Down moves cursor down half page",
			key:           vaxis.Key{Keycode: vaxis.KeyPgDown},
			wantHighlight: 2,
		},
		{
			name:          "Ctrl+u moves cursor up half page",
			prime:         []vaxis.Key{{Text: "G", Keycode: 'G'}},
			key:           vaxis.Key{Text: "u", Keycode: 'u', Modifiers: vaxis.ModCtrl},
			wantHighlight: 1,
		},
		{
			name:          "Page Up moves cursor up half page",
			prime:         []vaxis.Key{{Text: "G", Keycode: 'G'}},
			key:           vaxis.Key{Keycode: vaxis.KeyPgUp},
			wantHighlight: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := make([]diff.Row, 20)
			for i := range rows {
				rows[i] = diff.Row{Kind: diff.RowContext, Gutter: "1 1   ", Code: "line"}
			}
			app := newUIDiffTestApp(rows, false)
			app.Pump(vui.Size{Width: 20, Height: 4})
			app.Pump(vui.Size{Width: 20, Height: 4})
			for _, key := range tt.prime {
				app.Send(key)
				app.Pump(vui.Size{Width: 20, Height: 4})
				app.Pump(vui.Size{Width: 20, Height: 4})
			}
			app.Send(tt.key)
			if tt.key.Text == "g" {
				app.Send(tt.key)
			}
			app.Pump(vui.Size{Width: 20, Height: 4})
			app.Pump(vui.Size{Width: 20, Height: 4})

			p := vui.NewPainter(vui.Size{Width: 20, Height: 4})
			app.Paint(p)
			if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != tt.wantHighlight {
				t.Fatalf("highlight row = %d, want %d", got, tt.wantHighlight)
			}
		})
	}
}

func TestUIDiffViewEditorTargetUsesCursorRow(t *testing.T) {
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
	target, ok := uiDiffEditorTarget(rows, selectionPoint{Row: 3, Col: 2})
	if !ok {
		t.Fatal("editor target not found")
	}
	if target.Path != "main.go" || target.Line != 11 || target.Column != 3 {
		t.Fatalf("target = %+v, want main.go:11:3", target)
	}
}

func TestUIDiffViewEditorTargetUsesTextColumnForTabs(t *testing.T) {
	row := diff.Row{
		Kind:     diff.RowAdd,
		FileName: "main.go",
		Code:     "\tfoo",
		Review:   review.Anchor{Line: 12},
	}
	tests := []struct {
		name       string
		cursorCell int
		wantColumn int
	}{
		{name: "inside tab", cursorCell: 4, wantColumn: 1},
		{name: "after tab", cursorCell: 8, wantColumn: 2},
		{name: "after first rune", cursorCell: 9, wantColumn: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, ok := uiDiffEditorTarget([]diff.Row{row}, selectionPoint{Row: 0, Col: tt.cursorCell})
			if !ok {
				t.Fatal("editor target not found")
			}
			if target.Column != tt.wantColumn {
				t.Fatalf("column = %d, want %d", target.Column, tt.wantColumn)
			}
		})
	}
}

func TestUIDiffViewEditorTargetUsesFourCellTabsForNonGoFiles(t *testing.T) {
	row := diff.Row{Kind: diff.RowAdd, FileName: "main.py", Code: "\tfoo", Review: review.Anchor{Line: 12}}
	target, ok := uiDiffEditorTarget([]diff.Row{row}, selectionPoint{Row: 0, Col: 4})
	if !ok {
		t.Fatal("editor target not found")
	}
	if target.Column != 2 {
		t.Fatalf("column = %d, want 2", target.Column)
	}
}

func TestUIDiffViewEditorTargetFallsBackToLineOne(t *testing.T) {
	target, ok := uiDiffEditorTarget([]diff.Row{{Kind: diff.RowFile, FileName: "main.go"}}, selectionPoint{})
	if !ok {
		t.Fatal("editor target not found")
	}
	if target.Path != "main.go" || target.Line != 1 || target.Column != 1 {
		t.Fatalf("target = %+v, want main.go:1:1", target)
	}
}

func TestEditorPathInRootUsesWorktreeRootForRelativePaths(t *testing.T) {
	root := t.TempDir()
	if got, want := editorPathInRoot("sub/main.go", root), filepath.Join(root, "sub/main.go"); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestEditorPathInRootLeavesAbsolutePathsAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.go")
	if got := editorPathInRoot(path, t.TempDir()); got != path {
		t.Fatalf("path = %q, want %q", got, path)
	}
}

func TestUIDiffViewOReportsMissingFile(t *testing.T) {
	app := newUIDiffTestAppWithBaseDraftsAndStatus([]diff.Row{{Kind: diff.RowBlank}}, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 40, Height: 3}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "o", Keycode: 'o'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, size.Height-1); !strings.Contains(got, "No file.") {
		t.Fatalf("status = %q, want no file", got)
	}
}

func TestUIDiffViewEditorTerminalReceivesCommandKeys(t *testing.T) {
	t.Setenv("GIT_EDITOR", "true")
	row := diff.Row{Kind: diff.RowAdd, FileName: "main.go", Code: "line", Review: review.Anchor{Line: 12}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus([]diff.Row{row}, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 40, Height: 5}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "o", Keycode: 'o'})
	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, size.Height-1); strings.HasPrefix(got, ":") {
		t.Fatalf("diff command mode intercepted terminal key: status = %q", got)
	}
}

func TestUIDiffViewBracketCJumpsBetweenChanges(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "same"},
		{Kind: diff.RowDelete, Gutter: "2     - ", Code: "old"},
		{Kind: diff.RowAdd, Gutter: "    2 + ", Code: "new"},
		{Kind: diff.RowContext, Gutter: "3 3   ", Code: "same"},
		{Kind: diff.RowAdd, Gutter: "    4 + ", Code: "other"},
	}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 24, Height: 5})
	app.Pump(vui.Size{Width: 24, Height: 5})

	app.Send(vaxis.Key{Text: "]", Keycode: ']'})
	app.Pump(vui.Size{Width: 24, Height: 5})
	app.Send(vaxis.Key{Text: "c", Keycode: 'c'})
	app.Pump(vui.Size{Width: 24, Height: 5})
	p := vui.NewPainter(vui.Size{Width: 24, Height: 5})
	app.Paint(p)
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 1 {
		t.Fatalf("]c highlight row = %d, want 1", got)
	}

	app.Send(vaxis.Key{Text: "]", Keycode: ']'})
	app.Send(vaxis.Key{Text: "c", Keycode: 'c'})
	app.Pump(vui.Size{Width: 24, Height: 5})
	p = vui.NewPainter(vui.Size{Width: 24, Height: 5})
	app.Paint(p)
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 4 {
		t.Fatalf("second ]c highlight row = %d, want 4", got)
	}

	app.Send(vaxis.Key{Text: "[", Keycode: '['})
	app.Send(vaxis.Key{Text: "c", Keycode: 'c'})
	app.Pump(vui.Size{Width: 24, Height: 5})
	p = vui.NewPainter(vui.Size{Width: 24, Height: 5})
	app.Paint(p)
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 1 {
		t.Fatalf("[c highlight row = %d, want 1", got)
	}
}

func TestUIDiffViewBracketNJumpsBetweenNotes(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Gutter: "1 1   ", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "2 2   ", Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "3 3   ", Code: "three", Review: review.Anchor{Path: "main.go", Line: 3, Side: review.SideRight}},
	}
	drafts := []review.CommentDraft{
		{Path: "main.go", Line: 2, Side: review.SideRight, Body: "two"},
		{Path: "main.go", Line: 3, Side: review.SideRight, Body: "three"},
	}
	app := newUIDiffTestAppWithBaseAndDrafts(rows, DefaultBaseColors(), false, drafts)
	size := vui.Size{Width: 24, Height: 7}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "]", Keycode: ']'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "n", Keycode: 'n'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 1 {
		t.Fatalf("]n highlight row = %d, want 1", got)
	}

	app.Send(vaxis.Key{Text: "]", Keycode: ']'})
	app.Send(vaxis.Key{Text: "n", Keycode: 'n'})
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 5 {
		t.Fatalf("second ]n highlight row = %d, want 5", got)
	}

	app.Send(vaxis.Key{Text: "[", Keycode: '['})
	app.Send(vaxis.Key{Text: "n", Keycode: 'n'})
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 1 {
		t.Fatalf("[n highlight row = %d, want 1", got)
	}
}

func TestUIDiffViewBracketNJumpsBetweenNewSessionNotes(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "2 2 + ", Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "3 3 + ", Code: "three", Review: review.Anchor{Path: "main.go", Line: 3, Side: review.SideRight}},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 80, Height: 12}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "first"})
	app.Send(vaxis.Key{Text: "s", Keycode: 's', Modifiers: vaxis.ModCtrl})
	app.Pump(size)

	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "second"})
	app.Send(vaxis.Key{Text: "s", Keycode: 's', Modifiers: vaxis.ModCtrl})
	app.Pump(size)

	app.Send(vaxis.Key{Text: "[", Keycode: '['})
	app.Send(vaxis.Key{Text: "n", Keycode: 'n'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 1 {
		t.Fatalf("[n newly-created note highlight row = %d, want first note row 1", got)
	}

	app.Send(vaxis.Key{Text: "]", Keycode: ']'})
	app.Send(vaxis.Key{Text: "n", Keycode: 'n'})
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 5 {
		t.Fatalf("]n newly-created note highlight row = %d, want second note row 5", got)
	}
}

func TestUIDiffViewBracketNJumpsToRangeCommentTargetOnce(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Gutter: "1 1   ", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "2 2   ", Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "3 3   ", Code: "three", Review: review.Anchor{Path: "main.go", Line: 3, Side: review.SideRight}},
	}
	drafts := []review.CommentDraft{{Path: "main.go", StartLine: 1, StartSide: review.SideRight, Line: 3, Side: review.SideRight, Body: "range"}}
	app := newUIDiffTestAppWithBaseAndDrafts(rows, DefaultBaseColors(), false, drafts)
	size := vui.Size{Width: 24, Height: 7}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "]", Keycode: ']'})
	app.Send(vaxis.Key{Text: "n", Keycode: 'n'})
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 2 {
		t.Fatalf("]n range highlight row = %d, want end row 2", got)
	}

	app.Send(vaxis.Key{Text: "]", Keycode: ']'})
	app.Send(vaxis.Key{Text: "n", Keycode: 'n'})
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 2 {
		t.Fatalf("second ]n range highlight row = %d, want unchanged row 2", got)
	}
}

func TestUIDiffViewSlashSearchMovesToMatch(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "alpha"},
		{Kind: diff.RowContext, Gutter: "2 2   ", Code: "needle"},
	}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 20, Height: 3})
	app.Pump(vui.Size{Width: 20, Height: 3})

	app.Send(vaxis.Key{Text: "/", Keycode: '/'})
	app.Send(vaxis.Key{Text: "needle"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(vui.Size{Width: 20, Height: 3})
	p := vui.NewPainter(vui.Size{Width: 20, Height: 3})
	app.Paint(p)
	if cell := p.Cell(6, 1); cell.Grapheme != "n" || cell.Background != uiDiffCursorBackground(uiDiffTestTheme()) {
		t.Fatalf("search cursor = %q/%v, want n/cursor", cell.Grapheme, cell.Background)
	}
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 1 {
		t.Fatalf("search highlight row = %d, want 1", got)
	}
}

func TestUIDiffViewUsesCustomNavigationKeybindings(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "one"},
		{Kind: diff.RowContext, Gutter: "2 2   ", Code: "two"},
	}
	app := newUIDiffTestAppWithBindings(rows, map[string][]string{
		"cursor_down": {"ctrl+n"},
	})
	size := vui.Size{Width: 20, Height: 3}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 0 {
		t.Fatalf("default cursor_down key moved to row %d, want 0", got)
	}

	app.Send(vaxis.Key{Keycode: 'n', Modifiers: vaxis.ModCtrl})
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 1 {
		t.Fatalf("custom cursor_down key moved to row %d, want 1", got)
	}
}

func TestUIDiffViewUsesCustomSearchKeybinding(t *testing.T) {
	app := newUIDiffTestAppWithBindings([]diff.Row{{Kind: diff.RowContext, Text: "needle"}}, map[string][]string{
		"search": {"f"},
	})
	size := vui.Size{Width: 20, Height: 2}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "/", Keycode: '/'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, 1); strings.HasPrefix(got, "/") {
		t.Fatalf("default search key entered search mode: status = %q", got)
	}

	app.Send(vaxis.Key{Text: "f", Keycode: 'f'})
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, 1); got != "/" {
		t.Fatalf("custom search status = %q, want /", got)
	}
}

func TestUIDiffViewSearchModeUsesStatusBar(t *testing.T) {
	app := newUIDiffTestAppWithBaseDraftsAndStatus([]diff.Row{{Kind: diff.RowContext, Text: "needle"}}, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 20, Height: 2})
	app.Pump(vui.Size{Width: 20, Height: 2})

	app.Send(vaxis.Key{Text: "/", Keycode: '/'})
	app.Pump(vui.Size{Width: 20, Height: 2})
	p := vui.NewPainter(vui.Size{Width: 20, Height: 2})
	app.Paint(p)
	if got := uiDiffPainterText(p, 1); got != "/" {
		t.Fatalf("empty search status = %q, want /", got)
	}

	app.Send(vaxis.Key{Text: "needle"})
	app.Pump(vui.Size{Width: 20, Height: 2})
	p = vui.NewPainter(vui.Size{Width: 20, Height: 2})
	app.Paint(p)
	if got := uiDiffPainterText(p, 1); got != "/needle" {
		t.Fatalf("search status = %q, want /needle", got)
	}
}

func TestUIDiffViewCommandModeUsesStatusBar(t *testing.T) {
	app := newUIDiffTestAppWithBaseDraftsAndStatus([]diff.Row{{Kind: diff.RowContext, Text: "line"}}, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 20, Height: 2})
	app.Pump(vui.Size{Width: 20, Height: 2})

	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "wq"})
	app.Pump(vui.Size{Width: 20, Height: 2})
	p := vui.NewPainter(vui.Size{Width: 20, Height: 2})
	app.Paint(p)
	if got := uiDiffPainterText(p, 1); got != ":wq" {
		t.Fatalf("command status = %q, want :wq", got)
	}
	cursor, ok := p.Cursor()
	if !ok {
		t.Fatal("command cursor was not rendered")
	}
	if cursor.Col != 3 || cursor.Row != 1 || cursor.Shape != vui.CursorBeam {
		t.Fatalf("command cursor = %+v, want beam at 3,1", cursor)
	}
}

func TestUIDiffViewCommandQQuits(t *testing.T) {
	app := newUIDiffTestAppWithBaseDraftsAndStatus([]diff.Row{{Kind: diff.RowContext, Text: "line"}}, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 20, Height: 2})

	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "q"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	if !app.ShouldQuit() {
		t.Fatal(":q did not quit")
	}
}

func TestUIDiffViewCommandQQuitsEmptyInput(t *testing.T) {
	app := vui.NewApp(uiDiffView{ShowStatus: true}, vui.WithTheme(uiDiffTestTheme()))
	app.Pump(vui.Size{Width: 40, Height: 4})
	app.Pump(vui.Size{Width: 40, Height: 4})

	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "q"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	if !app.ShouldQuit() {
		t.Fatal(":q did not quit with empty input")
	}
}

func TestUIDiffViewCommandQQuitsWithSavedComments(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "2 2 + ", Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
	}
	drafts := []review.CommentDraft{
		{Path: "main.go", Line: 1, Side: review.SideRight, Body: "saved one"},
		{Path: "main.go", Line: 2, Side: review.SideRight, Body: "saved two"},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, drafts, true)
	size := vui.Size{Width: 80, Height: 10}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "q"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	if !app.ShouldQuit() {
		p := vui.NewPainter(size)
		app.Paint(p)
		t.Fatalf(":q did not quit with saved comments; status = %q", uiDiffPainterText(p, size.Height-1))
	}
}

func TestUIDiffViewCommandQQuitsAfterNavigatingSavedComments(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "2 2 + ", Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
	}
	drafts := []review.CommentDraft{
		{Path: "main.go", Line: 1, Side: review.SideRight, Body: "saved one"},
		{Path: "main.go", Line: 2, Side: review.SideRight, Body: "saved two"},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, drafts, true)
	size := vui.Size{Width: 80, Height: 10}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "q"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	if !app.ShouldQuit() {
		p := vui.NewPainter(size)
		app.Paint(p)
		t.Fatalf(":q did not quit after navigating saved comments; status = %q", uiDiffPainterText(p, size.Height-1))
	}
}

func TestUIDiffViewCommandQWarnsWithUnsavedComments(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "line", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 60, Height: 6}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "draft"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(size)
	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "q"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(size)
	if app.ShouldQuit() {
		t.Fatal(":q quit with unsaved comment")
	}
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, size.Height-1); !strings.Contains(got, "Unsaved comments") {
		t.Fatalf("status message = %q, want unsaved warning", got)
	}
}

func TestUIDiffViewCommandWWritesCommentsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".comview", "comments.json")
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "line", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}}}
	app := newUIDiffTestAppWithReviewFile(rows, nil, path)
	size := vui.Size{Width: 60, Height: 6}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "saved"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(size)
	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "w"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(size)
	file, err := review.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Comments) != 1 || file.Comments[0].Body != "saved" {
		t.Fatalf("comments file = %+v", file)
	}
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, size.Height-1); !strings.Contains(got, "Comments saved.") {
		t.Fatalf("status message = %q, want save confirmation", got)
	}
}

func TestUIDiffViewCommandWWithoutCommentsShowsNoopStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".comview", "comments.json")
	app := newUIDiffTestAppWithReviewFile([]diff.Row{{Kind: diff.RowContext, Text: "line"}}, nil, path)
	size := vui.Size{Width: 60, Height: 3}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "w"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, size.Height-1); !strings.Contains(got, "No comments to save.") {
		t.Fatalf("status message = %q, want no comments status", got)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("created comments file for no-op save")
	}
}

func TestUIDiffViewCommandWSavesAfterDeletingAllComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".comview", "comments.json")
	draft := review.CommentDraft{Path: "main.go", Line: 1, Side: review.SideRight, Body: "delete me"}
	if err := review.SaveFile(path, review.CommentFile{Version: 1, Comments: []review.CommentDraft{draft}}); err != nil {
		t.Fatal(err)
	}
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "line", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}}}
	app := newUIDiffTestAppWithReviewFile(rows, []review.CommentDraft{draft}, path)
	size := vui.Size{Width: 60, Height: 5}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "x", Keycode: 'x'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "w"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(size)

	file, err := review.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Comments) != 0 {
		t.Fatalf("comments = %+v, want empty after deleting all", file.Comments)
	}
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, size.Height-1); !strings.Contains(got, "Comments saved.") {
		t.Fatalf("status message = %q, want save confirmation", got)
	}
}

func TestUIDiffViewCommandWQWritesAndQuits(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".comview", "comments.json")
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "line", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}}}
	app := newUIDiffTestAppWithReviewFile(rows, nil, path)
	app.Pump(vui.Size{Width: 60, Height: 6})

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 60, Height: 6})
	app.Send(vaxis.Key{Text: "saved"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(vui.Size{Width: 60, Height: 6})
	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "wq"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	if !app.ShouldQuit() {
		t.Fatal(":wq did not quit")
	}
	file, err := review.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Comments) != 1 || file.Comments[0].Body != "saved" {
		t.Fatalf("comments file = %+v", file)
	}
}

func TestUIDiffViewCommandWQAfterEditingSavedCommentWritesAndQuits(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".comview", "comments.json")
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "line", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}}}
	draft := review.CommentDraft{Path: "main.go", Line: 1, Side: review.SideRight, Body: "saved"}
	if err := review.SaveFile(path, review.CommentFile{Version: 1, Comments: []review.CommentDraft{draft}}); err != nil {
		t.Fatal(err)
	}
	app := newUIDiffTestAppWithReviewFile(rows, []review.CommentDraft{draft}, path)
	size := vui.Size{Width: 60, Height: 6}
	app.Pump(size)
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	col, row, ok := uiDiffFindText(p, "saved")
	if !ok {
		t.Fatal("saved comment was not rendered")
	}
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: row + 1, Col: col})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease, Row: row + 1, Col: col})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "!"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(size)
	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "wq"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	if !app.ShouldQuit() {
		p = vui.NewPainter(size)
		app.Paint(p)
		t.Fatalf(":wq did not quit after editing saved comment; status = %q", uiDiffPainterText(p, size.Height-1))
	}
	file, err := review.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Comments) != 1 || file.Comments[0].Body != "saved!" {
		t.Fatalf("comments file = %+v, want one edited saved comment", file)
	}
}

func TestUIDiffViewCommandWQDoesNotQuitAfterSaveFailure(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(blocker, "comments.json")
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "line", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}}}
	app := newUIDiffTestAppWithReviewFile(rows, nil, path)
	size := vui.Size{Width: 80, Height: 6}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "unsaved"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(size)
	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "wq"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(size)

	if app.ShouldQuit() {
		t.Fatal(":wq quit after failed save")
	}
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, size.Height-1); !strings.Contains(got, "Could not save comments") {
		t.Fatalf("status message = %q, want save failure", got)
	}

	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "q"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(size)
	if app.ShouldQuit() {
		t.Fatal(":q quit after failed save cleared dirty state")
	}
	p = vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, size.Height-1); !strings.Contains(got, "Unsaved comments") {
		t.Fatalf("status message after :q = %q, want unsaved warning", got)
	}
}

func TestUIDiffViewCommandWQAfterDeletionDoesNotQuitAfterSaveFailure(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(blocker, "comments.json")
	draft := review.CommentDraft{Path: "main.go", Line: 1, Side: review.SideRight, Body: "delete me"}
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "line", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}}}
	app := newUIDiffTestAppWithReviewFile(rows, []review.CommentDraft{draft}, path)
	size := vui.Size{Width: 80, Height: 5}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "x", Keycode: 'x'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "wq"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(size)

	if app.ShouldQuit() {
		t.Fatal(":wq quit after failed deletion save")
	}
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, size.Height-1); !strings.Contains(got, "Could not save comments") {
		t.Fatalf("status message = %q, want save failure", got)
	}
}

func TestUIDiffViewCommandQBangQuitsWithUnsavedComments(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "line", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 60, Height: 6})

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 60, Height: 6})
	app.Send(vaxis.Key{Text: "draft"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(vui.Size{Width: 60, Height: 6})
	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "q!"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	if !app.ShouldQuit() {
		t.Fatal(":q! did not quit")
	}
}

func TestUIDiffViewXDeletesNoteAtCursor(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "line", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	drafts := []review.CommentDraft{{Path: "main.go", Line: 12, Side: review.SideRight, Body: "comment"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, drafts, true)
	size := vui.Size{Width: 60, Height: 6}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "x", Keycode: 'x'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "comment") != -1 {
		t.Fatal("deleted note is still rendered")
	}
	if got := uiDiffPainterText(p, size.Height-1); !strings.Contains(got, "Note deleted.") {
		t.Fatalf("status message = %q, want note deleted", got)
	}

	path := filepath.Join(t.TempDir(), ".comview", "comments.json")
	app = newUIDiffTestAppWithReviewFile(rows, drafts, path)
	app.Pump(size)
	app.Pump(size)
	app.Send(vaxis.Key{Text: "x", Keycode: 'x'})
	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "w"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	file, err := review.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Comments) != 0 {
		t.Fatalf("saved comments = %+v, want none", file.Comments)
	}
}

func TestUIDiffViewDDDeletesNoteAtCursor(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "line", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	drafts := []review.CommentDraft{{Path: "main.go", Line: 12, Side: review.SideRight, Body: "comment"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, drafts, true)
	size := vui.Size{Width: 60, Height: 6}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "d", Keycode: 'd'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "comment") == -1 {
		t.Fatal("first d deleted note")
	}
	app.Send(vaxis.Key{Text: "d", Keycode: 'd'})
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "comment") != -1 {
		t.Fatal("dd did not delete note")
	}
}

func TestUIDiffViewCommentEditorXDeletesCharacter(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "line", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	drafts := []review.CommentDraft{{Path: "main.go", Line: 12, Side: review.SideRight, Body: "comment"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, drafts, true)
	size := vui.Size{Width: 60, Height: 6}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "x", Keycode: 'x'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "omment") == -1 {
		t.Fatal("x did not delete the focused comment character")
	}
}

func TestUIDiffViewCommentEditorDDDeletesLine(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "line", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	drafts := []review.CommentDraft{{Path: "main.go", Line: 12, Side: review.SideRight, Body: "one\ntwo"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, drafts, true)
	size := vui.Size{Width: 60, Height: 8}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "d", Keycode: 'd'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "one") == -1 {
		t.Fatal("first d deleted comment line")
	}
	app.Send(vaxis.Key{Text: "d", Keycode: 'd'})
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "one") != -1 || uiDiffPainterRowContaining(p, "two") == -1 {
		t.Fatal("dd did not delete only the focused comment line")
	}
}

func TestUIDiffViewCommentEditorDDeletesForward(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "line", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	drafts := []review.CommentDraft{{Path: "main.go", Line: 12, Side: review.SideRight, Body: "comment"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, drafts, true)
	size := vui.Size{Width: 60, Height: 6}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
	app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
	app.Send(vaxis.Key{Text: "D", Keycode: 'D'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "co") == -1 || uiDiffPainterRowContaining(p, "comment") != -1 {
		t.Fatal("D did not delete from cursor to end of line")
	}
}

func TestUIDiffViewCommentEditorVisualDDeletesSelection(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "line", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	drafts := []review.CommentDraft{{Path: "main.go", Line: 12, Side: review.SideRight, Body: "comment"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, drafts, true)
	size := vui.Size{Width: 60, Height: 6}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "v", Keycode: 'v'})
	app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
	app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
	app.Send(vaxis.Key{Text: "d", Keycode: 'd'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "ment") == -1 || uiDiffPainterRowContaining(p, "comment") != -1 {
		t.Fatal("visual d did not delete the selected comment text")
	}
}

func TestUIDiffViewCommentEditorVisualLineDDeletesSelection(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "line", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	drafts := []review.CommentDraft{{Path: "main.go", Line: 12, Side: review.SideRight, Body: "one\ntwo\nthree"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, drafts, true)
	size := vui.Size{Width: 60, Height: 9}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "V", Keycode: 'V'})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: "d", Keycode: 'd'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "one") != -1 || uiDiffPainterRowContaining(p, "two") != -1 || uiDiffPainterRowContaining(p, "three") == -1 {
		t.Fatal("visual line d did not delete selected comment lines")
	}
}

func TestUIDiffViewCommentEditorCChangesSelection(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "line", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	drafts := []review.CommentDraft{{Path: "main.go", Line: 12, Side: review.SideRight, Body: "comment"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, drafts, true)
	size := vui.Size{Width: 60, Height: 6}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "v", Keycode: 'v'})
	app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
	app.Send(vaxis.Key{Text: "c", Keycode: 'c'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "hi"})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "himment") == -1 {
		t.Fatal("c did not change selected comment text and enter insert mode")
	}
}

func TestUIDiffViewCommentEditorEscapeClosesEmptyUnfocusedEditor(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "line", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 60, Height: 6}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "x"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Send(vaxis.Key{Text: "x", Keycode: 'x'})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "Add comment") != -1 || uiDiffPainterRowContaining(p, "▄") != -1 {
		t.Fatal("empty comment editor stayed open after escape")
	}
}

func TestUIDiffViewXDeletesNoteOverlappingSelection(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "hello", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	drafts := []review.CommentDraft{{Path: "main.go", Line: 12, Side: review.SideRight, StartColumn: intPtr(2), EndColumn: intPtr(4), Body: "inline"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, drafts, true)
	size := vui.Size{Width: 60, Height: 6}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "v", Keycode: 'v'})
	app.Send(vaxis.Key{Text: "x", Keycode: 'x'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "inline") != -1 {
		t.Fatal("selection-overlapping note is still rendered")
	}
	if got := uiDiffPainterText(p, size.Height-1); !strings.Contains(got, "Note deleted.") {
		t.Fatalf("status message = %q, want note deleted", got)
	}
}

func TestUIDiffViewXShowsMessageWithoutNote(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "line", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 60, Height: 4}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "x", Keycode: 'x'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, size.Height-1); !strings.Contains(got, "No note.") {
		t.Fatalf("status message = %q, want no note", got)
	}
}

func TestUIDiffViewIncrementalSearchHighlightsMatches(t *testing.T) {
	theme := uiDiffTestTheme()
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "needle"},
		{Kind: diff.RowHunk, Prefix: "@@ -10 +10 @@", Code: " func"},
	}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 24, Height: 2})
	app.Pump(vui.Size{Width: 24, Height: 2})

	app.Send(vaxis.Key{Text: "/", Keycode: '/'})
	app.Send(vaxis.Key{Text: "needle"})
	app.Pump(vui.Size{Width: 24, Height: 2})
	p := vui.NewPainter(vui.Size{Width: 24, Height: 2})
	app.Paint(p)
	if got := p.Cell(7, 0).Background; got != uiDiffSearchHighlightStyle(theme).Background {
		t.Fatalf("code search highlight background = %v, want warning", got)
	}

	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Send(vaxis.Key{Text: "/", Keycode: '/'})
	app.Send(vaxis.Key{Text: "-10"})
	app.Pump(vui.Size{Width: 24, Height: 2})
	p = vui.NewPainter(vui.Size{Width: 24, Height: 2})
	app.Paint(p)
	if got := p.Cell(3, 1).Background; got == uiDiffSearchHighlightStyle(theme).Background {
		t.Fatalf("hunk metadata was highlighted by search")
	}
}

func TestUIDiffViewSearchesStructuredRowsExceptHunks(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "alpha"},
		{Kind: diff.RowHunk, Prefix: "@@ -10 +10 @@", Code: " func"},
		{Kind: diff.RowCommitHeader, Prefix: "commit ", Code: "abc123"},
	}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 24, Height: 3})
	app.Pump(vui.Size{Width: 24, Height: 3})

	app.Send(vaxis.Key{Text: "/", Keycode: '/'})
	app.Send(vaxis.Key{Text: "-10"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(vui.Size{Width: 24, Height: 3})
	p := vui.NewPainter(vui.Size{Width: 24, Height: 3})
	app.Paint(p)
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 0 {
		t.Fatalf("hunk search highlight row = %d, want unchanged cursor row 0", got)
	}

	app.Send(vaxis.Key{Text: "/", Keycode: '/'})
	app.Send(vaxis.Key{Text: "commit"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(vui.Size{Width: 24, Height: 3})
	p = vui.NewPainter(vui.Size{Width: 24, Height: 3})
	app.Paint(p)
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 2 {
		t.Fatalf("structured commit search highlight row = %d, want 2", got)
	}
}

func TestUIDiffViewIncrementalSearchUsesNextMatchFromStart(t *testing.T) {
	rows := make([]diff.Row, 10)
	for i := range rows {
		rows[i] = diff.Row{Kind: diff.RowContext, Text: "line"}
	}
	rows[2].Text = "alpha"
	rows[8].Text = "alpha"
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 20, Height: 4})
	app.Pump(vui.Size{Width: 20, Height: 4})
	for i := 0; i < 5; i++ {
		app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
		app.Pump(vui.Size{Width: 20, Height: 4})
	}

	app.Send(vaxis.Key{Text: "/", Keycode: '/'})
	app.Send(vaxis.Key{Text: "alpha"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(vui.Size{Width: 20, Height: 4})
	app.Pump(vui.Size{Width: 20, Height: 4})
	p := vui.NewPainter(vui.Size{Width: 20, Height: 4})
	app.Paint(p)
	if got := uiDiffPainterText(p, 3); got != "alpha" {
		t.Fatalf("search target text = %q, want alpha", got)
	}
	if got := p.Cell(1, 3).Background; got != uiDiffSearchHighlightStyle(uiDiffTestTheme()).Background {
		t.Fatalf("search target background = %v, want search highlight", got)
	}
}

func TestUIDiffViewSearchNextPrevious(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Text: "one needle"},
		{Kind: diff.RowContext, Text: "two needle"},
	}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 20, Height: 2})
	app.Pump(vui.Size{Width: 20, Height: 2})
	app.Send(vaxis.Key{Text: "/", Keycode: '/'})
	app.Send(vaxis.Key{Text: "needle"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(vui.Size{Width: 20, Height: 2})

	app.Send(vaxis.Key{Text: "n", Keycode: 'n'})
	app.Pump(vui.Size{Width: 20, Height: 2})
	p := vui.NewPainter(vui.Size{Width: 20, Height: 2})
	app.Paint(p)
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 1 {
		t.Fatalf("n highlight row = %d, want 1", got)
	}

	app.Send(vaxis.Key{Text: "N", Keycode: 'N'})
	app.Pump(vui.Size{Width: 20, Height: 2})
	p = vui.NewPainter(vui.Size{Width: 20, Height: 2})
	app.Paint(p)
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 0 {
		t.Fatalf("N highlight row = %d, want 0", got)
	}
}

func TestUIDiffViewEscapeClearsSearch(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Text: "needle"}}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 20, Height: 1})
	app.Pump(vui.Size{Width: 20, Height: 1})
	app.Send(vaxis.Key{Text: "/", Keycode: '/'})
	app.Send(vaxis.Key{Text: "needle"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(vui.Size{Width: 20, Height: 1})

	app.Send(vaxis.Key{Text: "n", Keycode: 'n'})
	app.Pump(vui.Size{Width: 20, Height: 1})
	p := vui.NewPainter(vui.Size{Width: 20, Height: 1})
	app.Paint(p)
	if cell := p.Cell(1, 0); cell.Background != uiDiffCursorRowBackground(uiDiffTestTheme()) {
		t.Fatalf("cursor moved after escaped search: first row bg = %v, want selection", cell.Background)
	}
}

func TestUIDiffViewEscapeClearsAcceptedSearch(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Text: "needle"}}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 20, Height: 1})
	app.Pump(vui.Size{Width: 20, Height: 1})
	app.Send(vaxis.Key{Text: "/", Keycode: '/'})
	app.Send(vaxis.Key{Text: "needle"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(vui.Size{Width: 20, Height: 1})
	p := vui.NewPainter(vui.Size{Width: 20, Height: 1})
	app.Paint(p)
	if got := p.Cell(1, 0).Background; got != uiDiffSearchHighlightStyle(uiDiffTestTheme()).Background {
		t.Fatalf("search highlight background = %v, want search highlight", got)
	}

	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(vui.Size{Width: 20, Height: 1})
	p = vui.NewPainter(vui.Size{Width: 20, Height: 1})
	app.Paint(p)
	if got := p.Cell(1, 0).Background; got == uiDiffSearchHighlightStyle(uiDiffTestTheme()).Background {
		t.Fatal("search highlight still visible after escape")
	}
}

func TestUIDiffViewLinewiseSelectionExtendsWithCursor(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "one"},
		{Kind: diff.RowContext, Gutter: "2 2   ", Code: "two"},
		{Kind: diff.RowContext, Gutter: "3 3   ", Code: "three"},
	}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 30, Height: 3})
	app.Pump(vui.Size{Width: 30, Height: 3})

	app.Send(vaxis.Key{Text: "V", Keycode: 'V'})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(vui.Size{Width: 30, Height: 3})
	p := vui.NewPainter(vui.Size{Width: 30, Height: 3})
	app.Paint(p)
	theme := uiDiffTestTheme()
	if got := p.Cell(6, 0).Background; got != theme.Selection {
		t.Fatalf("anchor text background = %v, want selection", got)
	}
	if got := p.Cell(7, 1).Background; got != theme.Selection {
		t.Fatalf("cursor text background = %v, want selection", got)
	}
	if got := p.Cell(29, 0).Background; got == theme.Selection {
		t.Fatal("anchor row selection extends past text")
	}
	if got := p.Cell(29, 1).Background; got == theme.Selection {
		t.Fatal("cursor row selection extends past text")
	}
	if got := p.Cell(29, 2).Background; got == theme.Selection {
		t.Fatal("unselected row has selection background")
	}
}

func TestUIDiffViewVisualLineSkipsHunkRows(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "one"},
		{Kind: diff.RowHunk, Text: "@@ -2 +2 @@"},
		{Kind: diff.RowContext, Gutter: "2 2   ", Code: "two"},
	}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 30, Height: 3})
	app.Pump(vui.Size{Width: 30, Height: 3})

	app.Send(vaxis.Key{Text: "V", Keycode: 'V'})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(vui.Size{Width: 30, Height: 3})
	p := vui.NewPainter(vui.Size{Width: 30, Height: 3})
	app.Paint(p)
	theme := uiDiffTestTheme()
	if got := p.Cell(6, 0).Background; got != theme.Selection {
		t.Fatalf("first code row background = %v, want selection", got)
	}
	if got := p.Cell(0, 1).Background; got == theme.Selection {
		t.Fatal("hunk row was selected")
	}
	if got := p.Cell(7, 2).Background; got != theme.Selection {
		t.Fatalf("second code row background = %v, want selection", got)
	}
}

func TestUIDiffViewSelectionTextLinewiseSkipsHunkRows(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "one"},
		{Kind: diff.RowHunk, Text: "@@ -2 +2 @@"},
		{Kind: diff.RowContext, Gutter: "2 2   ", Code: "two"},
	}
	state := &uiDiffViewState{
		selectionActive:   true,
		selectionLinewise: true,
		selectionAnchor:   selectionPoint{Row: 0},
		cursor:            selectionPoint{Row: 2},
	}
	if got, want := state.selectionText(rows), "one\ntwo"; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
}

func TestUIDiffViewSelectionTextSkipsNonSelectableRowsWithoutExtraNewline(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Code: "hello", Text: "hello"},
		{Kind: diff.RowHunk, Text: "@@ -1 +1 @@ func main()", Prefix: "@@ -1 +1 @@", Code: " func main()"},
		{Kind: diff.RowContext, Code: "world", Text: "world"},
	}
	state := &uiDiffViewState{
		selectionActive: true,
		selectionAnchor: selectionPoint{Row: 0, Col: 0},
		cursor:          selectionPoint{Row: 1, Col: textCellWidth(rows[1].Text) - 1},
	}
	if got, want := state.selectionText(rows), "hello"; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}

	state.cursor = selectionPoint{Row: 2, Col: textCellWidth(rows[2].Text) - 1}
	if got, want := state.selectionText(rows), "hello\nworld"; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
}

func TestUIDiffViewSelectionTextCharacterwise(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "abcd"},
		{Kind: diff.RowContext, Gutter: "2 2   ", Code: "wxyz"},
	}
	state := &uiDiffViewState{
		selectionActive: true,
		selectionAnchor: selectionPoint{Row: 0, Col: 1},
		cursor:          selectionPoint{Row: 1, Col: 2},
	}
	if got, want := state.selectionText(rows), "bcd\nwxy"; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
}

func TestUIDiffViewMouseDragSelectsCode(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "abcde"},
		{Kind: diff.RowContext, Gutter: "2 2   ", Code: "fghij"},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 30, Height: 3})
	app.Pump(vui.Size{Width: 30, Height: 3})
	codeOffset := uiDiffCodeOffset(rows)

	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: 0, Col: codeOffset + 1})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion, Row: 1, Col: codeOffset + 2})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease, Row: 1, Col: codeOffset + 2})
	app.Pump(vui.Size{Width: 30, Height: 3})

	p := vui.NewPainter(vui.Size{Width: 30, Height: 3})
	app.Paint(p)
	theme := uiDiffTestTheme()
	if got := p.Cell(codeOffset+1, 0).Background; got != theme.Selection {
		t.Fatalf("first selected cell background = %v, want selection", got)
	}
	if got := p.Cell(codeOffset+2, 1).Background; got != uiDiffCursorBackground(theme) {
		t.Fatalf("cursor cell background = %v, want cursor", got)
	}
	if got := p.Cell(0, 0).Background; got == theme.Selection {
		t.Fatal("gutter was selected")
	}
}

func TestUIDiffViewMouseDragSelectsCommitMessage(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowCommitMessage, Text: "    hello world"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 30, Height: 3}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: 0, Col: 4})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion, Row: 0, Col: 8})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease, Row: 0, Col: 8})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := p.Cell(4, 0).Background; got != uiDiffTestTheme().Selection {
		t.Fatalf("commit message selected background = %v, want selection", got)
	}
	if got := p.Cell(8, 0).Background; got != uiDiffCursorBackground(uiDiffTestTheme()) {
		t.Fatalf("commit message cursor background = %v, want cursor", got)
	}
}

func TestUIDiffViewMouseDragSelectsCommitHeader(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowCommitHeader, Text: "commit abc123", Prefix: "commit ", Code: "abc123"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 30, Height: 3}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: 0, Col: 0})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion, Row: 0, Col: 6})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease, Row: 0, Col: 6})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := p.Cell(0, 0).Background; got != uiDiffTestTheme().Selection {
		t.Fatalf("commit header selected background = %v, want selection", got)
	}
	if got := p.Cell(6, 0).Background; got != uiDiffCursorBackground(uiDiffTestTheme()) {
		t.Fatalf("commit header cursor background = %v, want cursor", got)
	}
}

func TestUIDiffViewMouseClickDoesNotSelect(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Gutter: "1 1   ", Code: "abcde"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 30, Height: 3})
	app.Pump(vui.Size{Width: 30, Height: 3})
	codeOffset := uiDiffCodeOffset(rows)

	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: 0, Col: codeOffset + 2})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease, Row: 0, Col: codeOffset + 2})
	app.Pump(vui.Size{Width: 30, Height: 3})

	p := vui.NewPainter(vui.Size{Width: 30, Height: 3})
	app.Paint(p)
	if got := p.Cell(codeOffset+2, 0).Background; got == uiDiffTestTheme().Selection {
		t.Fatal("single click selected text")
	}
	if got := uiDiffPainterText(p, 2); !strings.HasPrefix(got, " NORMAL ") {
		t.Fatalf("status bar = %q, want NORMAL", got)
	}
}

func TestUIDiffViewMousePressIgnoresStatusAndScrollbarRows(t *testing.T) {
	rows := make([]diff.Row, 12)
	for i := range rows {
		rows[i] = diff.Row{Kind: diff.RowContext, Gutter: "1 1   ", Code: strings.Repeat("x", 60)}
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 30, Height: 6}
	app.Pump(size)
	app.Pump(size)
	app.Pump(size)
	codeOffset := uiDiffCodeOffset(rows)

	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: size.Height - 2, Col: codeOffset})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion, Row: 1, Col: codeOffset + 2})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease, Row: 1, Col: codeOffset + 2})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: size.Height - 1, Col: codeOffset})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion, Row: 1, Col: codeOffset + 2})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease, Row: 1, Col: codeOffset + 2})
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, size.Height-1); !strings.HasPrefix(got, " NORMAL ") {
		t.Fatalf("status bar = %q, want NORMAL after ignored mouse rows", got)
	}
	if got := p.Cell(codeOffset+2, 1).Background; got == uiDiffTestTheme().Selection {
		t.Fatal("mouse drag from scrollbar/status row created a selection")
	}
}

func TestUIDiffViewMousePressIgnoresVerticalScrollbarColumn(t *testing.T) {
	rows := make([]diff.Row, 12)
	for i := range rows {
		rows[i] = diff.Row{Kind: diff.RowContext, Gutter: "1 1   ", Code: "abcdef"}
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 30, Height: 6}
	app.Pump(size)
	app.Pump(size)
	app.Pump(size)
	codeOffset := uiDiffCodeOffset(rows)

	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: 0, Col: size.Width - 1})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion, Row: 1, Col: codeOffset + 2})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease, Row: 1, Col: codeOffset + 2})
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, size.Height-1); !strings.HasPrefix(got, " NORMAL ") {
		t.Fatalf("status bar = %q, want NORMAL after scrollbar press", got)
	}
	if got := p.Cell(codeOffset+2, 1).Background; got == uiDiffTestTheme().Selection {
		t.Fatal("mouse drag from vertical scrollbar created a selection")
	}
}

func TestUIDiffViewVerticalScrollbarDragScrolls(t *testing.T) {
	rows := make([]diff.Row, 12)
	for i := range rows {
		rows[i] = diff.Row{Kind: diff.RowContext, Gutter: "1 1   ", Code: fmt.Sprintf("line %02d", i+1)}
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 30, Height: 6}
	app.Pump(size)
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: 0, Col: size.Width - 1})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion, Row: 3, Col: size.Width - 1})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease, Row: 3, Col: size.Width - 1})
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, 0); strings.Contains(got, "line 01") {
		t.Fatalf("first visible row = %q, want scrollbar drag to scroll down", got)
	}
}

func TestUIDiffViewHorizontalScrollbarDragScrolls(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Gutter: "1 1   ", Code: strings.Repeat("x", 60) + "Z"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 30, Height: 5}
	app.Pump(size)
	app.Pump(size)
	app.Pump(size)

	barRow := size.Height - 2
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: barRow, Col: 0})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion, Row: barRow, Col: size.Width - 1})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease, Row: barRow, Col: size.Width - 1})
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, 0); !strings.Contains(got, "Z") {
		t.Fatalf("visible row after horizontal scrollbar drag = %q, want line end", got)
	}
}

func TestUIDiffViewDoubleClickSelectsToken(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Gutter: "1 1   ", Code: "foo bar.baz"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 30, Height: 3})
	app.Pump(vui.Size{Width: 30, Height: 3})
	codeOffset := uiDiffCodeOffset(rows)

	for i := 0; i < 2; i++ {
		app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: 0, Col: codeOffset + 5})
		app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease, Row: 0, Col: codeOffset + 5})
	}
	app.Pump(vui.Size{Width: 30, Height: 3})

	p := vui.NewPainter(vui.Size{Width: 30, Height: 3})
	app.Paint(p)
	theme := uiDiffTestTheme()
	for col := codeOffset + 4; col <= codeOffset+6; col++ {
		if got := p.Cell(col, 0).Background; got != theme.Selection && got != uiDiffCursorBackground(theme) {
			t.Fatalf("token cell %d background = %v, want selected/cursor", col, got)
		}
	}
	if got := p.Cell(codeOffset+7, 0).Background; got == theme.Selection {
		t.Fatal("punctuation after token was selected")
	}
	if got := uiDiffPainterText(p, 2); !strings.HasPrefix(got, " VISUAL ") {
		t.Fatalf("status bar = %q, want VISUAL", got)
	}
}

func TestUIDiffViewTripleClickSelectsCodeRow(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "hello"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 30, Height: 3})
	app.Pump(vui.Size{Width: 30, Height: 3})
	codeOffset := uiDiffCodeOffset(rows)

	for i := 0; i < 3; i++ {
		app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: 0, Col: codeOffset + 1})
		app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease, Row: 0, Col: codeOffset + 1})
	}
	app.Pump(vui.Size{Width: 30, Height: 3})

	p := vui.NewPainter(vui.Size{Width: 30, Height: 3})
	app.Paint(p)
	for col := codeOffset; col < codeOffset+len("hello"); col++ {
		if got := p.Cell(col, 0).Background; got != uiDiffTestTheme().Selection && got != uiDiffCursorBackground(uiDiffTestTheme()) {
			t.Fatalf("row cell %d background = %v, want selected/cursor", col, got)
		}
	}
	if got := p.Cell(0, 0).Background; got == uiDiffTestTheme().Selection {
		t.Fatal("triple click selected gutter")
	}
	if got := uiDiffPainterText(p, 2); !strings.HasPrefix(got, " V-LINE ") {
		t.Fatalf("status bar = %q, want V-LINE", got)
	}
}

func TestUIDiffViewMouseSelectionSkipsHunkRows(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "hello"},
		{Kind: diff.RowHunk, Text: "@@ -1 +1 @@", Prefix: "@@ -1 +1 @@"},
		{Kind: diff.RowContext, Gutter: "2 2   ", Code: "world"},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 30, Height: 4})
	app.Pump(vui.Size{Width: 30, Height: 4})
	codeOffset := uiDiffCodeOffset(rows)

	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: 0, Col: codeOffset})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion, Row: 1, Col: codeOffset + 2})
	app.Pump(vui.Size{Width: 30, Height: 4})
	p := vui.NewPainter(vui.Size{Width: 30, Height: 4})
	app.Paint(p)
	if got := p.Cell(0, 1).Background; got == uiDiffTestTheme().Selection {
		t.Fatal("hunk row was selected")
	}

	app.Pump(vui.Size{Width: 30, Height: 4})

	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion, Row: 2, Col: codeOffset + 1})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease, Row: 2, Col: codeOffset + 1})
	app.Pump(vui.Size{Width: 30, Height: 4})
	p = vui.NewPainter(vui.Size{Width: 30, Height: 4})
	app.Paint(p)
	if got := p.Cell(codeOffset, 2).Background; got != uiDiffTestTheme().Selection {
		t.Fatalf("second code row selected cell background = %v, want selection", got)
	}
}

func TestUIDiffViewDragFromHunkIntoCodeStartsAtLineStart(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowHunk, Text: "@@ -1 +1 @@", Prefix: "@@ -1 +1 @@"},
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "abcdef"},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 30, Height: 4})
	app.Pump(vui.Size{Width: 30, Height: 4})
	codeOffset := uiDiffCodeOffset(rows)

	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: 0, Col: 2})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion, Row: 1, Col: codeOffset + 1})
	app.Pump(vui.Size{Width: 30, Height: 4})

	p := vui.NewPainter(vui.Size{Width: 30, Height: 4})
	app.Paint(p)
	theme := uiDiffTestTheme()
	if got := p.Cell(codeOffset, 1).Background; got != theme.Selection {
		t.Fatalf("line-start selected background = %v, want selection", got)
	}
	if got := p.Cell(codeOffset+1, 1).Background; got != uiDiffCursorBackground(theme) {
		t.Fatalf("drag cursor background = %v, want cursor", got)
	}
	if got := p.Cell(0, 0).Background; got == theme.Selection {
		t.Fatal("hunk row was selected")
	}
}

func TestUIDiffViewDragFromHunkUpIntoCodeStartsAtLineEnd(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "abcdef"},
		{Kind: diff.RowHunk, Text: "@@ -1 +1 @@", Prefix: "@@ -1 +1 @@"},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 30, Height: 4})
	app.Pump(vui.Size{Width: 30, Height: 4})
	codeOffset := uiDiffCodeOffset(rows)

	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: 1, Col: 2})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion, Row: 0, Col: codeOffset + 4})
	app.Pump(vui.Size{Width: 30, Height: 4})

	p := vui.NewPainter(vui.Size{Width: 30, Height: 4})
	app.Paint(p)
	theme := uiDiffTestTheme()
	if got := p.Cell(codeOffset+4, 0).Background; got != uiDiffCursorBackground(theme) {
		t.Fatalf("drag cursor background = %v, want cursor", got)
	}
	if got := p.Cell(codeOffset+5, 0).Background; got != theme.Selection {
		t.Fatalf("line-end selected background = %v, want selection", got)
	}
	if got := p.Cell(codeOffset+3, 0).Background; got == theme.Selection {
		t.Fatal("drag selection started before target cell")
	}
}

func TestUIDiffViewMouseWheelExtendsDraggingSelection(t *testing.T) {
	rows := make([]diff.Row, 12)
	for i := range rows {
		rows[i] = diff.Row{Kind: diff.RowContext, Gutter: "1 1   ", Code: "abcdef"}
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 30, Height: 4})
	app.Pump(vui.Size{Width: 30, Height: 4})
	codeOffset := uiDiffCodeOffset(rows)

	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: 0, Col: codeOffset})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion, Row: 1, Col: codeOffset})
	app.Send(vaxis.Mouse{Button: vaxis.MouseWheelDown, EventType: vaxis.EventPress, Row: 1, Col: codeOffset + 2})
	app.Pump(vui.Size{Width: 30, Height: 4})
	app.Pump(vui.Size{Width: 30, Height: 4})

	p := vui.NewPainter(vui.Size{Width: 30, Height: 4})
	app.Paint(p)
	theme := uiDiffTestTheme()
	if got := p.Cell(codeOffset+2, 1).Background; got != uiDiffCursorBackground(theme) {
		t.Fatalf("cursor after wheel drag background = %v, want cursor", got)
	}
	if got := p.Cell(codeOffset, 0).Background; got != theme.Selection {
		t.Fatalf("scrolled selection background = %v, want selection", got)
	}
}

func TestUIDiffViewMouseWheelDoesNotExtendFinishedSelection(t *testing.T) {
	rows := make([]diff.Row, 12)
	for i := range rows {
		rows[i] = diff.Row{Kind: diff.RowContext, Gutter: "1 1   ", Code: "abcdef"}
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 30, Height: 4})
	app.Pump(vui.Size{Width: 30, Height: 4})
	codeOffset := uiDiffCodeOffset(rows)

	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: 0, Col: codeOffset})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion, Row: 1, Col: codeOffset})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease, Row: 1, Col: codeOffset})
	app.Send(vaxis.Mouse{Button: vaxis.MouseWheelDown, EventType: vaxis.EventPress, Row: 1, Col: codeOffset + 2})
	app.Pump(vui.Size{Width: 30, Height: 4})
	app.Pump(vui.Size{Width: 30, Height: 4})

	p := vui.NewPainter(vui.Size{Width: 30, Height: 4})
	app.Paint(p)
	if got := p.Cell(codeOffset+2, 1).Background; got == uiDiffCursorBackground(uiDiffTestTheme()) {
		t.Fatal("finished selection cursor moved to mouse position after wheel")
	}
}

func TestUIDiffViewMouseWheelDoesNotExtendSelectionOverStatusOrScrollbar(t *testing.T) {
	rows := make([]diff.Row, 12)
	for i := range rows {
		rows[i] = diff.Row{Kind: diff.RowContext, Gutter: "1 1   ", Code: strings.Repeat("x", 60)}
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 30, Height: 6}
	app.Pump(size)
	app.Pump(size)
	app.Pump(size)
	codeOffset := uiDiffCodeOffset(rows)

	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: 0, Col: codeOffset})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion, Row: 1, Col: codeOffset})
	app.Send(vaxis.Mouse{Button: vaxis.MouseWheelDown, EventType: vaxis.EventPress, Row: size.Height - 1, Col: codeOffset + 2})
	app.Send(vaxis.Mouse{Button: vaxis.MouseWheelDown, EventType: vaxis.EventPress, Row: 1, Col: size.Width - 1})
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	if got := p.Cell(codeOffset+2, 1).Background; got == uiDiffCursorBackground(uiDiffTestTheme()) {
		t.Fatal("wheel over status/scrollbar moved drag cursor")
	}
}

func TestUIDiffViewSelectionTextPreservesTabs(t *testing.T) {
	row := diff.Row{Kind: diff.RowContext, Code: "a\tb", Text: "a\tb"}
	state := &uiDiffViewState{
		selectionActive: true,
		selectionAnchor: selectionPoint{Row: 0, Col: 1},
		cursor:          selectionPoint{Row: 0, Col: 1},
	}
	if got, want := state.selectionText([]diff.Row{row}), "\t"; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
}

func TestUIDiffViewTextObjectSelectsInnerWord(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Code: "foo bar.baz", Text: "foo bar.baz"}}
	state := &uiDiffViewState{
		cursor: selectionPoint{Row: 0, Col: 5},
	}
	if !state.selectWordTextObject(rows, textObjectInner) {
		t.Fatal("inner word text object failed")
	}
	if got, want := state.selectionText(rows), "bar"; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
	if got, want := state.cursor, (selectionPoint{Row: 0, Col: 6}); got != want {
		t.Fatalf("cursor = %+v, want %+v", got, want)
	}
}

func TestUIDiffViewTextObjectSelectsAroundWord(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Code: "foo bar baz", Text: "foo bar baz"}}
	state := &uiDiffViewState{
		cursor: selectionPoint{Row: 0, Col: 5},
	}
	if !state.selectWordTextObject(rows, textObjectAround) {
		t.Fatal("around word text object failed")
	}
	if got, want := state.selectionText(rows), "bar "; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
}

func TestUIDiffViewTextObjectSelectsPunctuationToken(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Code: "foo bar.baz", Text: "foo bar.baz"}}
	state := &uiDiffViewState{
		cursor: selectionPoint{Row: 0, Col: 7},
	}
	if !state.selectWordTextObject(rows, textObjectInner) {
		t.Fatal("punctuation token text object failed")
	}
	if got, want := state.selectionText(rows), "."; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
}

func TestUIDiffViewTextObjectKeysSelectInnerWord(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Gutter: "1 1   ", Code: "foo bar.baz"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 30, Height: 3})
	app.Pump(vui.Size{Width: 30, Height: 3})

	for i := 0; i < 5; i++ {
		app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
	}
	app.Send(vaxis.Key{Text: "v", Keycode: 'v'})
	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Send(vaxis.Key{Text: "w", Keycode: 'w'})
	app.Pump(vui.Size{Width: 30, Height: 3})

	p := vui.NewPainter(vui.Size{Width: 30, Height: 3})
	app.Paint(p)
	codeOffset := uiDiffCodeOffset(rows)
	theme := uiDiffTestTheme()
	for col := codeOffset + 4; col <= codeOffset+6; col++ {
		if got := p.Cell(col, 0).Background; got != theme.Selection && got != uiDiffCursorBackground(theme) {
			t.Fatalf("word cell %d background = %v, want selected/cursor", col, got)
		}
	}
	if got := p.Cell(codeOffset+7, 0).Background; got == theme.Selection {
		t.Fatal("punctuation after word was selected")
	}
}

func TestUIDiffViewTextObjectIgnoresShiftModifierPress(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Gutter: "1 1   ", Code: "foo bar.baz"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 30, Height: 3})
	app.Pump(vui.Size{Width: 30, Height: 3})

	for i := 0; i < 5; i++ {
		app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
	}
	app.Send(vaxis.Key{Text: "v", Keycode: 'v'})
	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Send(vaxis.Key{Keycode: vaxis.KeyLeftShift})
	app.Send(vaxis.Key{Text: "w", Keycode: 'w'})
	app.Pump(vui.Size{Width: 30, Height: 3})

	p := vui.NewPainter(vui.Size{Width: 30, Height: 3})
	app.Paint(p)
	codeOffset := uiDiffCodeOffset(rows)
	theme := uiDiffTestTheme()
	for col := codeOffset + 4; col <= codeOffset+6; col++ {
		if got := p.Cell(col, 0).Background; got != theme.Selection && got != uiDiffCursorBackground(theme) {
			t.Fatalf("word cell %d background = %v, want selected/cursor", col, got)
		}
	}
}

func TestUIDiffViewTextObjectUsesShiftedPunctuation(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Gutter: "1 1   ", Code: "call(foo)"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 30, Height: 3})
	app.Pump(vui.Size{Width: 30, Height: 3})

	for i := 0; i < strings.Index(rows[0].Code, "foo"); i++ {
		app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
	}
	app.Send(vaxis.Key{Text: "v", Keycode: 'v'})
	app.Send(vaxis.Key{Text: "a", Keycode: 'a'})
	app.Send(vaxis.Key{Text: ")", Keycode: '0', Modifiers: vaxis.ModShift})
	app.Pump(vui.Size{Width: 30, Height: 3})

	p := vui.NewPainter(vui.Size{Width: 30, Height: 3})
	app.Paint(p)
	codeOffset := uiDiffCodeOffset(rows)
	theme := uiDiffTestTheme()
	if got := p.Cell(codeOffset+4, 0).Background; got != theme.Selection {
		t.Fatalf("opening paren background = %v, want selection", got)
	}
	if got := p.Cell(codeOffset+8, 0).Background; got != uiDiffCursorBackground(theme) {
		t.Fatalf("closing paren cursor background = %v, want cursor", got)
	}
	if got := p.Cell(codeOffset, 0).Background; got == uiDiffCursorBackground(theme) {
		t.Fatal("shifted punctuation fell through to 0 cursor movement")
	}
}

func TestUIDiffViewTextObjectSelectsMultilineBraces(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Code: "func main() {", Text: "func main() {"},
		{Kind: diff.RowContext, Code: "\tcall()", Text: "\tcall()"},
		{Kind: diff.RowContext, Code: "}", Text: "}"},
	}
	state := &uiDiffViewState{cursor: selectionPoint{Row: 1, Col: 2}}
	open, close, ok := textObjectDelimiters('{')
	if !ok {
		t.Fatal("brace delimiter missing")
	}
	if !state.selectDelimitedTextObject(rows, textObjectAround, open, close) {
		t.Fatal("around brace text object failed")
	}
	if got, want := state.selectionText(rows), "{\n\tcall()\n}"; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
}

func TestUIDiffViewInnerTextObjectExcludesBoundaryNewlines(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Code: "foo{", Text: "foo{"},
		{Kind: diff.RowContext, Code: "  foo,", Text: "  foo,"},
		{Kind: diff.RowContext, Code: "}", Text: "}"},
	}
	state := &uiDiffViewState{cursor: selectionPoint{Row: 1, Col: 0}}
	open, close, ok := textObjectDelimiters('{')
	if !ok {
		t.Fatal("brace delimiter missing")
	}
	if !state.selectDelimitedTextObject(rows, textObjectInner, open, close) {
		t.Fatal("inner brace text object failed")
	}
	if got, want := state.selectionText(rows), "  foo,"; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
}

func TestUIDiffViewInnerBracketTextObjectsSelectOnlyInsideLine(t *testing.T) {
	tests := []struct {
		name   string
		object rune
		open   string
		close  string
	}{
		{name: "brace", object: '{', open: "foo() {", close: "}"},
		{name: "paren", object: '(', open: "foo(", close: ")"},
		{name: "square", object: '[', open: "items := [", close: "]"},
		{name: "angle", object: '<', open: "Vec<", close: ">"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := []diff.Row{
				{Kind: diff.RowContext, Code: tt.open, Text: tt.open},
				{Kind: diff.RowContext, Code: "  bar()", Text: "  bar()"},
				{Kind: diff.RowContext, Code: tt.close, Text: tt.close},
			}
			state := &uiDiffViewState{cursor: selectionPoint{Row: 1, Col: 2}}
			open, close, ok := textObjectDelimiters(tt.object)
			if !ok {
				t.Fatalf("%q delimiter missing", tt.object)
			}
			if !state.selectDelimitedTextObject(rows, textObjectInner, open, close) {
				t.Fatalf("inner %q text object failed", tt.object)
			}
			if got, want := state.selectionText(rows), "  bar()"; got != want {
				t.Fatalf("selection text = %q, want %q", got, want)
			}
		})
	}
}

func TestUIDiffViewTextObjectStopsAtHunkBoundary(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Code: "func main() {", Text: "func main() {"},
		{Kind: diff.RowContext, Code: "\tcall()", Text: "\tcall()"},
		{Kind: diff.RowContext, Code: "}", Text: "}"},
		{Kind: diff.RowHunk, Text: "@@ -10 +10 @@"},
		{Kind: diff.RowContext, Code: "other {}", Text: "other {}"},
	}
	state := &uiDiffViewState{cursor: selectionPoint{Row: 1, Col: 2}}
	open, close, ok := textObjectDelimiters('{')
	if !ok {
		t.Fatal("brace delimiter missing")
	}
	if !state.selectDelimitedTextObject(rows, textObjectAround, open, close) {
		t.Fatal("around brace text object failed")
	}
	if got, want := state.selectionText(rows), "{\n\tcall()\n}"; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
}

func TestUIDiffViewTextObjectSkipsOppositeSideRows(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Code: "if ok {", Text: "if ok {", FileName: "main.go"},
		{Kind: diff.RowDelete, Code: "old()", Text: "old()", FileName: "main.go"},
		{Kind: diff.RowAdd, Code: "new()", Text: "new()", FileName: "main.go"},
		{Kind: diff.RowContext, Code: "}", Text: "}", FileName: "main.go"},
	}
	state := &uiDiffViewState{cursor: selectionPoint{Row: 2, Col: 0}}
	open, close, ok := textObjectDelimiters('{')
	if !ok {
		t.Fatal("brace delimiter missing")
	}
	if !state.selectDelimitedTextObject(rows, textObjectAround, open, close) {
		t.Fatal("around brace text object failed")
	}
	if got, want := state.selectionText(rows), "{\nnew()\n}"; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
}

func TestUIDiffViewOpensCommentEditor(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "new", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Pump(vui.Size{Width: 40, Height: 6})

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Pump(vui.Size{Width: 40, Height: 6})
	p := vui.NewPainter(vui.Size{Width: 40, Height: 6})
	app.Paint(p)
	if got := uiDiffPainterText(p, 1); !strings.Contains(got, "▄") {
		t.Fatalf("editor row = %q, want top half-block padding", got)
	}
	codeOffset := uiDiffCodeOffset(rows)
	if cursor, ok := p.Cursor(); !ok || cursor.Col != codeOffset+2 || cursor.Row != 2 || cursor.Shape != vui.CursorBeam {
		t.Fatalf("cursor = %+v, want editor row beam cursor after two left padding cells", cursor)
	}
	if got := p.Cell(0, 0).Background; got == uiDiffCursorRowBackground(uiDiffTestTheme()) {
		t.Fatal("diff cursor row is highlighted while textarea is focused")
	}
	if got := uiDiffPainterText(p, 5); !strings.HasPrefix(got, " INSERT ") {
		t.Fatalf("status bar = %q, want INSERT", got)
	}
}

func TestUIDiffViewCommentEditorGrowsVertically(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "new", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 80, Height: 10}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "one\ntwo\nthree"})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, 2); !strings.Contains(got, "one") {
		t.Fatalf("first editor line = %q, want one", got)
	}
	if got := uiDiffPainterText(p, 3); !strings.Contains(got, "two") {
		t.Fatalf("second editor line = %q, want two", got)
	}
	if got := uiDiffPainterText(p, 4); !strings.Contains(got, "three") {
		t.Fatalf("third editor line = %q, want three", got)
	}
	if got := uiDiffPainterText(p, 5); !strings.Contains(got, "▀") {
		t.Fatalf("editor bottom row = %q, want box to grow instead of scrolling", got)
	}
}

func TestUIDiffViewCommentEditorGrowsVerticallyForWrappedText(t *testing.T) {
	rows := make([]diff.Row, 8)
	for i := range rows {
		rows[i] = diff.Row{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: fmt.Sprintf("line %d", i), Review: review.Anchor{Path: "main.go", Line: i + 1, Side: review.SideRight}}
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 50, Height: 15}
	app.Pump(size)
	app.Pump(size)
	for range 3 {
		app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	}
	app.Pump(size)

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: strings.Repeat("word ", 10)})
	app.Pump(size)
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	visible := ""
	firstBodyRow := ""
	for row := 0; row < size.Height; row++ {
		text := uiDiffPainterText(p, row)
		visible += text
		if firstBodyRow == "" && strings.Contains(text, "word") {
			firstBodyRow = text
		}
	}
	if !strings.Contains(firstBodyRow, "word word word") {
		t.Fatalf("first visible comment body row = %q, want beginning of comment", firstBodyRow)
	}
	if got := strings.Count(visible, "w"); got != 10 {
		t.Fatalf("visible wrapped word starts = %d, want all 10 typed words", got)
	}
	if !strings.Contains(visible, "▄") || !strings.Contains(visible, "▀") {
		t.Fatal("comment editor chrome was not fully visible")
	}
}

func TestUIDiffViewCommentEditorTypingSingleLineGrowsForSoftWrap(t *testing.T) {
	tests := []struct {
		name string
		text string
		rows int
	}{
		{name: "two rows", text: strings.Repeat("a", 69), rows: 2},
		{name: "three rows", text: strings.Repeat("a", 137), rows: 3},
		{name: "four rows", text: strings.Repeat("a", 205), rows: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "new", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
			app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
			size := vui.Size{Width: 80, Height: 8}
			app.Pump(size)
			app.Pump(size)

			app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
			app.Pump(size)
			app.Send(vaxis.Key{Text: tt.text, Keycode: []rune(tt.text)[0]})
			app.Pump(size)
			app.Pump(size)

			p := vui.NewPainter(size)
			app.Paint(p)
			bodyRows := uiDiffCommentEditorBodyTextRows(p, size)
			if len(bodyRows) != tt.rows {
				t.Fatalf("comment body rows = %d, want %d; rows=%q", len(bodyRows), tt.rows, bodyRows)
			}
			if !strings.Contains(bodyRows[0], "aaa") {
				t.Fatalf("first visible comment body row = %q, want beginning of typed text", bodyRows[0])
			}
			visible := strings.Join(bodyRows, "")
			if got := strings.Count(visible, "a"); got != len(tt.text) {
				t.Fatalf("visible a count = %d, want %d", got, len(tt.text))
			}
			if uiDiffPainterRowContaining(p, "▄") == -1 || uiDiffPainterRowContaining(p, "▀") == -1 {
				t.Fatal("comment editor chrome was not fully visible")
			}
		})
	}
}

func TestUIDiffViewCommentEditorTypingOneRuneAtATimeKeepsTopVisibleWhenGrowing(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "new", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 80, Height: 8}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(size)
	text := "lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore"
	for _, r := range text {
		app.Send(vaxis.Key{Text: string(r), Keycode: r})
		app.Pump(size)
	}
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	bodyRows := uiDiffCommentEditorBodyTextRows(p, size)
	if len(bodyRows) < 2 {
		t.Fatalf("comment body rows = %d, want wrapped text; rows=%q", len(bodyRows), bodyRows)
	}
	if !strings.Contains(bodyRows[0], "lorem") {
		t.Fatalf("first visible comment body row = %q, want beginning of typed text", bodyRows[0])
	}
	visible := strings.Join(bodyRows, "")
	if !strings.Contains(visible, "labore") {
		t.Fatalf("visible comment body = %q, want end of typed text", visible)
	}
}

func uiDiffCommentEditorBodyTextRows(p *vui.Painter, size vui.Size) []string {
	var rows []string
	for row := 0; row < size.Height; row++ {
		text := uiDiffPainterText(p, row)
		if strings.Contains(text, "▄") || strings.Contains(text, "▀") || !strings.HasPrefix(text, "        ") || !strings.ContainsAny(text, "abcdefghijklmnopqrstuvwxyz") {
			continue
		}
		rows = append(rows, text)
	}
	return rows
}

func TestUIDiffViewOpeningCommentEditorScrollsIntoView(t *testing.T) {
	rows := make([]diff.Row, 8)
	for i := range rows {
		rows[i] = diff.Row{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: fmt.Sprintf("line %d", i), Review: review.Anchor{Path: "main.go", Line: i + 1, Side: review.SideRight}}
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 80, Height: 6}
	app.Pump(size)
	app.Pump(size)
	for range 4 {
		app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	}
	app.Pump(size)

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(size)
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "▄") == -1 || uiDiffPainterRowContaining(p, "▀") == -1 {
		t.Fatal("opening comment editor did not scroll the text field into view")
	}
}

func TestUIDiffViewOpeningCommentEditorForVisibleLineSelectionDoesNotScroll(t *testing.T) {
	rows := make([]diff.Row, 50)
	for i := range rows {
		rows[i] = diff.Row{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: fmt.Sprintf("line %d", i), Review: review.Anchor{Path: "main.go", Line: i + 1, Side: review.SideRight}}
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 80, Height: 12}
	app.Pump(size)
	app.Pump(size)

	for range 20 {
		app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
		app.Pump(size)
	}
	for range 5 {
		app.Send(vaxis.Key{Text: "k", Keycode: 'k'})
		app.Pump(size)
	}
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, 0); !strings.Contains(got, "line 10") {
		t.Fatalf("top visible row before opening selected-line comment = %q, want scrolled viewport", got)
	}

	app.Send(vaxis.Key{Text: "V", Keycode: 'V'})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(size)
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, 0); !strings.Contains(got, "line 10") {
		t.Fatalf("top visible row after opening selected-line comment = %q, want unchanged viewport", got)
	}
	if uiDiffPainterRowContaining(p, "▄") == -1 || uiDiffPainterRowContaining(p, "▀") == -1 {
		t.Fatal("comment editor was not visible in unchanged viewport")
	}
}

func TestUIDiffViewCommentEditorEscapeReturnsToNormal(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "new", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Pump(vui.Size{Width: 40, Height: 6})

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Send(vaxis.Key{Text: "draft"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(vui.Size{Width: 40, Height: 6})
	p := vui.NewPainter(vui.Size{Width: 40, Height: 6})
	app.Paint(p)
	if got := uiDiffPainterText(p, 2); !strings.Contains(got, "draft") {
		t.Fatalf("editor body after escape = %q, want draft", got)
	}
	if got := uiDiffPainterText(p, 5); !strings.HasPrefix(got, " NORMAL ") {
		t.Fatalf("status bar = %q, want NORMAL", got)
	}
	if _, ok := p.Cursor(); ok {
		t.Fatal("textarea cursor is still visible after escape returned focus to diff")
	}
	if got := p.Cell(0, 0).Background; got == uiDiffCursorRowBackground(uiDiffTestTheme()) {
		t.Fatal("diff cursor row is highlighted while comment editor is in normal mode")
	}
}

func TestUIDiffViewCommentEditorNormalModeAppendAfterCursor(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "new", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Pump(vui.Size{Width: 40, Height: 6})

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Send(vaxis.Key{Text: "abc"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Send(vaxis.Key{Text: "h", Keycode: 'h'})
	app.Send(vaxis.Key{Text: "a", Keycode: 'a'})
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Send(vaxis.Key{Text: "X"})
	app.Pump(vui.Size{Width: 40, Height: 6})
	p := vui.NewPainter(vui.Size{Width: 40, Height: 6})
	app.Paint(p)
	if got := uiDiffPainterText(p, 2); !strings.Contains(got, "abXc") {
		t.Fatalf("editor body = %q, want append after normal cursor", got)
	}
}

func TestUIDiffViewCommentEditorNormalModeMovesBetweenLines(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "new", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 40, Height: 8})
	app.Pump(vui.Size{Width: 40, Height: 8})

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 40, Height: 8})
	app.Send(vaxis.Key{Text: "ab\ncd"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Send(vaxis.Key{Text: "k", Keycode: 'k'})
	app.Send(vaxis.Key{Text: "a", Keycode: 'a'})
	app.Pump(vui.Size{Width: 40, Height: 8})
	app.Send(vaxis.Key{Text: "X"})
	app.Pump(vui.Size{Width: 40, Height: 8})
	p := vui.NewPainter(vui.Size{Width: 40, Height: 8})
	app.Paint(p)
	if got := uiDiffPainterText(p, 2); !strings.Contains(got, "abX") {
		t.Fatalf("first editor line = %q, want normal-mode k to move up", got)
	}
}

func TestUIDiffViewCursorUpIntoCommentStartsAtLastLine(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "2 2 + ", Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 40, Height: 10}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "first\nsecond"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: "k", Keycode: 'k'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	secondRow := uiDiffPainterRowContaining(p, "second")
	if secondRow == -1 {
		t.Fatal("second comment line was not rendered")
	}
	codeOffset := uiDiffCodeOffset(rows)
	if cell := p.Cell(codeOffset+2, secondRow); cell.Background != uiDiffCursorBackground(uiDiffTestTheme()) {
		t.Fatalf("cursor after moving up into comment = %v, want last comment line cursor", cell.Background)
	}

	app.Send(vaxis.Key{Text: "k", Keycode: 'k'})
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	firstRow := uiDiffPainterRowContaining(p, "first")
	if firstRow == -1 {
		t.Fatal("first comment line was not rendered")
	}
	if cell := p.Cell(codeOffset+2, firstRow); cell.Background != uiDiffCursorBackground(uiDiffTestTheme()) {
		t.Fatalf("cursor after moving within comment = %v, want first comment line cursor", cell.Background)
	}
}

func TestUIDiffViewCommentEditorNormalModeOpenLineBelow(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "new", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 40, Height: 8})
	app.Pump(vui.Size{Width: 40, Height: 8})

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 40, Height: 8})
	app.Send(vaxis.Key{Text: "one"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Send(vaxis.Key{Text: "o", Keycode: 'o'})
	app.Pump(vui.Size{Width: 40, Height: 8})
	app.Send(vaxis.Key{Text: "two"})
	app.Pump(vui.Size{Width: 40, Height: 8})
	p := vui.NewPainter(vui.Size{Width: 40, Height: 8})
	app.Paint(p)
	if got := uiDiffPainterText(p, 2); !strings.Contains(got, "one") {
		t.Fatalf("first editor line = %q, want one", got)
	}
	if got := uiDiffPainterText(p, 3); !strings.Contains(got, "two") {
		t.Fatalf("opened editor line = %q, want two", got)
	}
}

func TestUIDiffViewCommentEditorNormalModeOpenLineAbove(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "new", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 40, Height: 8})
	app.Pump(vui.Size{Width: 40, Height: 8})

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 40, Height: 8})
	app.Send(vaxis.Key{Text: "two"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Send(vaxis.Key{Text: "O", Keycode: 'O'})
	app.Pump(vui.Size{Width: 40, Height: 8})
	app.Send(vaxis.Key{Text: "one"})
	app.Pump(vui.Size{Width: 40, Height: 8})
	p := vui.NewPainter(vui.Size{Width: 40, Height: 8})
	app.Paint(p)
	if got := uiDiffPainterText(p, 2); !strings.Contains(got, "one") {
		t.Fatalf("opened editor line = %q, want one", got)
	}
	if got := uiDiffPainterText(p, 3); !strings.Contains(got, "two") {
		t.Fatalf("second editor line = %q, want two", got)
	}
}

func TestUIDiffViewCommentEditorNormalModeLineStartAndEnd(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "new", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Pump(vui.Size{Width: 40, Height: 6})

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Send(vaxis.Key{Text: "abc"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Send(vaxis.Key{Text: "0", Keycode: '0'})
	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Send(vaxis.Key{Text: "X"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Send(vaxis.Key{Text: "$", Keycode: '$'})
	app.Send(vaxis.Key{Text: "a", Keycode: 'a'})
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Send(vaxis.Key{Text: "Y"})
	app.Pump(vui.Size{Width: 40, Height: 6})
	p := vui.NewPainter(vui.Size{Width: 40, Height: 6})
	app.Paint(p)
	if got := uiDiffPainterText(p, 2); !strings.Contains(got, "XabcY") {
		t.Fatalf("editor body = %q, want line start/end edits", got)
	}
}

func TestUIDiffViewCommentEditorNormalModeShowsCursorOnEmptyLine(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "new", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 40, Height: 8})
	app.Pump(vui.Size{Width: 40, Height: 8})

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 40, Height: 8})
	app.Send(vaxis.Key{Text: "ab\n"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(vui.Size{Width: 40, Height: 8})
	p := vui.NewPainter(vui.Size{Width: 40, Height: 8})
	app.Paint(p)
	if cell := p.Cell(uiDiffCodeOffset(rows)+2, 3); cell.Background != uiDiffCursorBackground(uiDiffTestTheme()) {
		t.Fatalf("empty line cursor background = %v, want cursor background", cell.Background)
	}
}

func TestUIDiffViewCommentEditorNormalModeHidesCursorWhenUnfocused(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "new", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Pump(vui.Size{Width: 40, Height: 6})

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Send(vaxis.Key{Text: "abc"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(vui.Size{Width: 40, Height: 6})
	p := vui.NewPainter(vui.Size{Width: 40, Height: 6})
	app.Paint(p)
	for col := 2; col < 5; col++ {
		if cell := p.Cell(col, 2); cell.Background == uiDiffCursorBackground(uiDiffTestTheme()) {
			t.Fatalf("unfocused comment cursor still visible at col %d", col)
		}
	}
}

func TestUIDiffViewCommentEditorEscapeClosesEmptyEditor(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "new", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Pump(vui.Size{Width: 40, Height: 6})

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(vui.Size{Width: 40, Height: 6})
	p := vui.NewPainter(vui.Size{Width: 40, Height: 6})
	app.Paint(p)
	if got := uiDiffPainterText(p, 1); strings.Contains(got, "▄") || strings.Contains(got, "Add comment") {
		t.Fatalf("editor row = %q, want empty editor closed", got)
	}
	if _, ok := p.Cursor(); ok {
		t.Fatal("textarea cursor is still visible after empty editor closed")
	}
	if got := p.Cell(0, 0).Background; got != uiDiffCursorRowBackground(uiDiffTestTheme()) {
		t.Fatalf("diff cursor row background = %v, want cursor row", got)
	}
}

func TestUIDiffViewCommentEditorCanCursorInAndOut(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "2 2 + ", Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 40, Height: 8})
	app.Pump(vui.Size{Width: 40, Height: 8})

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 40, Height: 8})
	app.Send(vaxis.Key{Text: "draft"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(vui.Size{Width: 40, Height: 8})

	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(vui.Size{Width: 40, Height: 8})
	p := vui.NewPainter(vui.Size{Width: 40, Height: 8})
	app.Paint(p)
	if got := p.Cell(39, 0).Background; got == uiDiffCursorRowBackground(uiDiffTestTheme()) {
		t.Fatal("diff row stayed highlighted after cursoring into comment box")
	}
	if got := p.Cell(uiDiffCodeOffset(rows), 2).Background; got != uiDiffTestTheme().SurfaceHovered {
		t.Fatalf("focused comment body background = %v, want hovered surface", got)
	}

	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(vui.Size{Width: 40, Height: 8})
	p = vui.NewPainter(vui.Size{Width: 40, Height: 8})
	app.Paint(p)
	secondRow := uiDiffPainterRowContaining(p, "two")
	if secondRow == -1 {
		t.Fatal("second diff row was not rendered")
	}
	if got := p.Cell(39, secondRow).Background; got != uiDiffCursorRowBackground(uiDiffTestTheme()) {
		t.Fatalf("second diff row background = %v, want cursor row after moving out of comment", got)
	}

	app.Send(vaxis.Key{Text: "k", Keycode: 'k'})
	app.Send(vaxis.Key{Text: "k", Keycode: 'k'})
	app.Pump(vui.Size{Width: 40, Height: 8})
	p = vui.NewPainter(vui.Size{Width: 40, Height: 8})
	app.Paint(p)
	if got := p.Cell(39, 0).Background; got != uiDiffCursorRowBackground(uiDiffTestTheme()) {
		t.Fatalf("first diff row background = %v, want cursor row after moving back out of comment", got)
	}
}

func uiDiffPainterRowContaining(p *vui.Painter, text string) int {
	for row := 0; row < p.Size().Height; row++ {
		if strings.Contains(uiDiffPainterText(p, row), text) {
			return row
		}
	}
	return -1
}

func uiDiffPainterRowCountContaining(p *vui.Painter, text string) int {
	count := 0
	for row := 0; row < p.Size().Height; row++ {
		if strings.Contains(uiDiffPainterText(p, row), text) {
			count++
		}
	}
	return count
}

func TestUIDiffViewCommentEditorEscapeKeepsEditorOpen(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "new", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Pump(vui.Size{Width: 40, Height: 6})

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Send(vaxis.Key{Text: "draft"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(vui.Size{Width: 40, Height: 6})
	p := vui.NewPainter(vui.Size{Width: 40, Height: 6})
	app.Paint(p)
	if got := uiDiffPainterText(p, 1); !strings.Contains(got, "▄") {
		t.Fatalf("editor row = %q, want editor to remain open", got)
	}
	if got := uiDiffPainterText(p, 2); !strings.Contains(got, "draft") {
		t.Fatalf("editor body = %q, want draft", got)
	}
	codeOffset := uiDiffCodeOffset(rows)
	if got := p.Cell(0, 2).Background; got == uiDiffTestTheme().Surface {
		t.Fatalf("editor body left edge background = %v, want gutter space before comment surface", got)
	}
	if got := p.Cell(codeOffset, 2).Background; got != uiDiffTestTheme().Surface {
		t.Fatalf("editor body code edge background = %v, want comment surface", got)
	}
}

func TestUIDiffViewCommentEditorNormalIReentersInsert(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "new", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Pump(vui.Size{Width: 40, Height: 6})

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Send(vaxis.Key{Text: "a"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Send(vaxis.Key{Text: "a", Keycode: 'a'})
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Send(vaxis.Key{Text: "b"})
	app.Send(vaxis.Key{Text: "s", Keycode: 's', Modifiers: vaxis.ModCtrl})
	app.Pump(vui.Size{Width: 40, Height: 6})
	p := vui.NewPainter(vui.Size{Width: 40, Height: 6})
	app.Paint(p)
	if got := uiDiffPainterText(p, 2); !strings.Contains(got, "ab") {
		t.Fatalf("draft row = %q, want edited body", got)
	}
}

func TestUIDiffViewCommentEditorSubmitCreatesDraft(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "new", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Pump(vui.Size{Width: 40, Height: 6})

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 40, Height: 6})
	app.Send(vaxis.Key{Text: "looks good"})
	app.Send(vaxis.Key{Text: "s", Keycode: 's', Modifiers: vaxis.ModCtrl})
	app.Pump(vui.Size{Width: 40, Height: 6})
	p := vui.NewPainter(vui.Size{Width: 40, Height: 6})
	app.Paint(p)
	if got := uiDiffPainterText(p, 1); !strings.Contains(got, "▄") || strings.ContainsAny(got, "┌╭─") {
		t.Fatalf("submitted draft top row = %q, want borderless comment chrome", got)
	}
	if got := uiDiffPainterText(p, 2); !strings.Contains(got, "looks good") {
		t.Fatalf("draft row = %q, want submitted body", got)
	}
	if got := uiDiffPainterText(p, 5); !strings.HasPrefix(got, " NORMAL ") {
		t.Fatalf("status bar = %q, want NORMAL", got)
	}
}

func TestUIDiffViewVisualLineIOpensCommentEditor(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".comview", "comments.json")
	rows := []diff.Row{
		{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "first", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "2 2 + ", Code: "second", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
	}
	app := newUIDiffTestAppWithReviewFile(rows, nil, path)
	size := vui.Size{Width: 80, Height: 8}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "V", Keycode: 'V'})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: "I", Keycode: 'I'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, size.Height-1); !strings.HasPrefix(got, " INSERT ") {
		t.Fatalf("status = %q, want insert", got)
	}

	app.Send(vaxis.Key{Text: "range"})
	app.Send(vaxis.Key{Text: "s", Keycode: 's', Modifiers: vaxis.ModCtrl})
	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "w"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	file, err := review.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Comments) != 1 {
		t.Fatalf("comments = %+v, want one", file.Comments)
	}
	draft := file.Comments[0]
	if draft.Body != "range" || draft.StartLine != 1 || draft.Line != 2 || draft.StartColumn != nil || draft.EndColumn != nil {
		t.Fatalf("draft = %+v, want line range 1-2 without columns", draft)
	}
}

func TestUIDiffViewVisualIOpensColumnCommentEditor(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".comview", "comments.json")
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "hello", Review: review.Anchor{Path: "main.go", Line: 12, Side: review.SideRight}}}
	app := newUIDiffTestAppWithReviewFile(rows, nil, path)
	size := vui.Size{Width: 80, Height: 7}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
	app.Send(vaxis.Key{Text: "v", Keycode: 'v'})
	app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
	app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
	app.Send(vaxis.Key{Text: "I", Keycode: 'I'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, size.Height-1); !strings.HasPrefix(got, " INSERT ") {
		t.Fatalf("status = %q, want insert", got)
	}

	app.Send(vaxis.Key{Text: "inline"})
	app.Send(vaxis.Key{Text: "s", Keycode: 's', Modifiers: vaxis.ModCtrl})
	app.Send(vaxis.Key{Text: ":", Keycode: ':'})
	app.Send(vaxis.Key{Text: "w"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	file, err := review.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Comments) != 1 {
		t.Fatalf("comments = %+v, want one", file.Comments)
	}
	draft := file.Comments[0]
	if draft.Body != "inline" || draft.Line != 12 || draft.StartLine != 0 || draft.StartColumn == nil || draft.EndColumn == nil || *draft.StartColumn != 2 || *draft.EndColumn != 3 {
		t.Fatalf("draft = %+v, want columns 2-3", draft)
	}
}

func TestUIDiffViewOpeningSecondCommentKeepsFirstDraft(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "2 2 + ", Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 80, Height: 12})
	app.Pump(vui.Size{Width: 80, Height: 12})

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 80, Height: 12})
	app.Send(vaxis.Key{Text: "first"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(vui.Size{Width: 80, Height: 12})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 80, Height: 12})
	p := vui.NewPainter(vui.Size{Width: 80, Height: 12})
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "first") == -1 {
		t.Fatal("first draft disappeared after opening second comment editor")
	}
	if uiDiffPainterRowContaining(p, "Add comment") == -1 {
		t.Fatal("second comment editor was not opened")
	}
}

func TestUIDiffViewOpeningCommentBelowSavedCommentDoesNotDuplicateSavedComment(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "2 2 + ", Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
	}
	drafts := []review.CommentDraft{{Path: "main.go", Line: 1, Side: review.SideRight, Body: "saved comment"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, drafts, true)
	size := vui.Size{Width: 80, Height: 12}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterRowCountContaining(p, "saved comment"); got != 1 {
		t.Fatalf("saved comment render count = %d, want 1", got)
	}
	if uiDiffPainterRowContaining(p, "Add comment") == -1 {
		t.Fatal("new comment editor was not opened below saved comment")
	}
}

func TestUIDiffViewCursorUpStopsAtAdjacentStoredComments(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "2 2 + ", Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "3 3 + ", Code: "three", Review: review.Anchor{Path: "main.go", Line: 3, Side: review.SideRight}},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, false)
	size := vui.Size{Width: 80, Height: 12}
	app.Pump(size)
	app.Pump(size)

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "comment 1"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "comment 2"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(size)

	for range 3 {
		app.Send(vaxis.Key{Text: "k", Keycode: 'k'})
		app.Pump(size)
	}
	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "!"})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "!comment 1") == -1 {
		t.Fatal("cursor skipped first adjacent comment while moving up")
	}
}

func TestUIDiffViewCursorStopsAtSubmittedDraftComments(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "2 2 + ", Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "3 3 + ", Code: "three", Review: review.Anchor{Path: "main.go", Line: 3, Side: review.SideRight}},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 80, Height: 14})
	app.Pump(vui.Size{Width: 80, Height: 14})

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 80, Height: 14})
	app.Send(vaxis.Key{Text: "submitted 1"})
	app.Send(vaxis.Key{Text: "s", Keycode: 's', Modifiers: vaxis.ModCtrl})
	app.Pump(vui.Size{Width: 80, Height: 14})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 80, Height: 14})
	app.Send(vaxis.Key{Text: "submitted 2"})
	app.Send(vaxis.Key{Text: "s", Keycode: 's', Modifiers: vaxis.ModCtrl})
	app.Pump(vui.Size{Width: 80, Height: 14})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(vui.Size{Width: 80, Height: 14})

	for range 5 {
		app.Send(vaxis.Key{Text: "k", Keycode: 'k'})
	}
	app.Pump(vui.Size{Width: 80, Height: 14})
	p := vui.NewPainter(vui.Size{Width: 80, Height: 14})
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "submitted 1") == -1 {
		t.Fatal("moving up through submitted draft comments skipped the first draft")
	}
}

func TestUIDiffViewCursorStopsAtProvidedDraftComments(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "2 2 + ", Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "3 3 + ", Code: "three", Review: review.Anchor{Path: "main.go", Line: 3, Side: review.SideRight}},
	}
	drafts := []review.CommentDraft{
		{Path: "main.go", Line: 1, Side: review.SideRight, Body: "provided 1"},
		{Path: "main.go", Line: 2, Side: review.SideRight, Body: "provided 2"},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, drafts, true)
	app.Pump(vui.Size{Width: 80, Height: 14})
	app.Pump(vui.Size{Width: 80, Height: 14})

	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(vui.Size{Width: 80, Height: 14})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(vui.Size{Width: 80, Height: 14})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(vui.Size{Width: 80, Height: 14})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(vui.Size{Width: 80, Height: 14})
	app.Send(vaxis.Key{Text: "k", Keycode: 'k'})
	app.Pump(vui.Size{Width: 80, Height: 14})
	p := vui.NewPainter(vui.Size{Width: 80, Height: 14})
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "provided 2") == -1 {
		t.Fatal("first k from code 3 did not show provided draft 2")
	}
	app.Send(vaxis.Key{Text: "k", Keycode: 'k'})
	app.Pump(vui.Size{Width: 80, Height: 14})
	p = vui.NewPainter(vui.Size{Width: 80, Height: 14})
	app.Paint(p)
	code2Row := uiDiffPainterRowContaining(p, "2 2 + two")
	if code2Row == -1 || p.Cell(79, code2Row).Background != uiDiffCursorRowBackground(uiDiffTestTheme()) {
		t.Fatal("second k from code 3 did not hover code 2")
	}
	app.Send(vaxis.Key{Text: "k", Keycode: 'k'})
	app.Pump(vui.Size{Width: 80, Height: 14})
	p = vui.NewPainter(vui.Size{Width: 80, Height: 14})
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "provided 1") == -1 {
		t.Fatal("third k from code 3 did not show provided draft 1")
	}
	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 80, Height: 14})
	app.Send(vaxis.Key{Text: "!"})
	app.Pump(vui.Size{Width: 80, Height: 14})
	p = vui.NewPainter(vui.Size{Width: 80, Height: 14})
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "!provided 1") == -1 {
		t.Fatal("moving up through provided draft comments skipped the first draft")
	}
}

func TestUIDiffViewMouseClickSelectsProvidedDraftComment(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "2 2 + ", Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
	}
	drafts := []review.CommentDraft{{Path: "main.go", Line: 2, Side: review.SideRight, Body: "provided comment"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, drafts, true)
	size := vui.Size{Width: 80, Height: 10}
	app.Pump(size)
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	col, row, ok := uiDiffFindText(p, "provided comment")
	if !ok {
		t.Fatal("provided comment was not rendered")
	}

	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: row, Col: col})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease, Row: row, Col: col})
	app.Pump(size)
	app.Pump(size)
	app.Send(vaxis.Key{Text: "!"})
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "!provided comment") == -1 {
		t.Fatalf("mouse-selected draft comment did not enter insert mode at click position; rendered row = %q", uiDiffPainterText(p, row))
	}
}

func TestUIDiffViewCommentEditorChromeClickMovesCursor(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 80, Height: 8}
	app.Pump(size)
	app.Pump(size)
	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "hello"})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	col, row, ok := uiDiffFindText(p, "hello")
	if !ok {
		t.Fatal("comment editor body was not rendered")
	}

	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: row + 1, Col: col})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease, Row: row + 1, Col: col})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "!"})
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	if uiDiffPainterRowContaining(p, "hello!") == -1 {
		t.Fatal("clicking comment chrome did not move cursor to nearest text position")
	}
}

func TestUIDiffViewCommentEditorSupportsMouseTextSelection(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 80, Height: 8}
	app.Pump(size)
	app.Pump(size)
	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(size)
	app.Send(vaxis.Key{Text: "hello world"})
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	col, row, ok := uiDiffFindText(p, "hello world")
	if !ok {
		t.Fatal("comment editor body was not rendered")
	}

	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: row, Col: col})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion, Row: row, Col: col + len("hello")})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease, Row: row, Col: col + len("hello")})
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	selectedBackground := vaxis.Color(0)
	for offset := range len("hello") {
		if bg := p.Cell(col+offset, row).Background; bg != uiDiffTestTheme().SurfaceHovered {
			selectedBackground = bg
			break
		}
	}
	if selectedBackground == 0 || selectedBackground == uiDiffTestTheme().SurfaceHovered {
		t.Fatalf("selected comment background = %v, want text selection background", selectedBackground)
	}
	for offset := range len("hello") {
		if got := p.Cell(col+offset, row).Background; got != selectedBackground {
			t.Fatalf("selected comment char %d background = %v, want %v", offset, got, selectedBackground)
		}
	}
}

func TestUIDiffViewMouseSelectionAccountsForCommentRows(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Gutter: "1 1 + ", Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Gutter: "2 2 + ", Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 40, Height: 8})
	app.Pump(vui.Size{Width: 40, Height: 8})
	codeOffset := uiDiffCodeOffset(rows)

	app.Send(vaxis.Key{Text: "i", Keycode: 'i'})
	app.Pump(vui.Size{Width: 40, Height: 8})
	app.Send(vaxis.Key{Text: "draft"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(vui.Size{Width: 40, Height: 8})
	app.Pump(vui.Size{Width: 40, Height: 8})

	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Row: 4, Col: codeOffset})
	app.Send(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion, Row: 4, Col: codeOffset + 1})
	app.Pump(vui.Size{Width: 40, Height: 8})
	p := vui.NewPainter(vui.Size{Width: 40, Height: 8})
	app.Paint(p)
	if got := p.Cell(codeOffset, 4).Background; got != uiDiffTestTheme().Selection {
		t.Fatalf("second diff row selected background = %v, want selection", got)
	}
	if got := p.Cell(codeOffset, 0).Background; got == uiDiffTestTheme().Selection {
		t.Fatal("mouse selection hit first row instead of row after comment box")
	}
}

func TestUIDiffViewSelectionTextIncludesCommitMessages(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Code: "selectable", Text: "selectable"},
		{Kind: diff.RowCommitHeader, Text: "commit abc123"},
		{Kind: diff.RowCommitMeta, Text: "Author: Example <example@example.com>"},
		{Kind: diff.RowCommitMessage, Text: "    message"},
	}
	state := &uiDiffViewState{
		selectionActive: true,
		selectionAnchor: selectionPoint{Row: 0, Col: 0},
		cursor:          selectionPoint{Row: 3, Col: textCellWidth(rows[3].Text) - 1},
	}
	if got, want := state.selectionText(rows), "selectable\ncommit abc123\nAuthor: Example <example@example.com>\n    message"; got != want {
		t.Fatalf("selection text = %q, want %q", got, want)
	}
}

func TestUIDiffViewCanCursorToCommitHeaderRows(t *testing.T) {
	doc, err := diff.Parse("commit abc123\nAuthor: Example <example@example.com>\nDate:   Thu May 14 12:00:00 2026 -0500\n\n    hello world\n")
	if err != nil {
		t.Fatal(err)
	}
	rows := doc.Rows()
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 64, Height: 7}
	app.Pump(size)
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	for _, text := range []string{"commit abc123", "Author: Example", "Date:"} {
		row := uiDiffPainterRowContaining(p, text)
		if row == -1 {
			t.Fatalf("%q was not rendered", text)
		}
	}
	if row := uiDiffPainterRowContaining(p, "commit abc123"); p.Cell(size.Width-1, row).Background != uiDiffCursorRowBackground(uiDiffTestTheme()) {
		t.Fatal("initial cursor is not on commit header")
	}

	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	if row := uiDiffPainterRowContaining(p, "Author: Example"); p.Cell(size.Width-1, row).Background != uiDiffCursorRowBackground(uiDiffTestTheme()) {
		t.Fatal("cursor did not move to commit author row")
	}

	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(size)
	p = vui.NewPainter(size)
	app.Paint(p)
	if row := uiDiffPainterRowContaining(p, "Date:"); p.Cell(size.Width-1, row).Background != uiDiffCursorRowBackground(uiDiffTestTheme()) {
		t.Fatal("cursor did not move to commit date row")
	}
}

func TestUIDiffViewCursorSkipsFileAndHunkRows(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowFile, Text: "main.go"},
		{Kind: diff.RowHunk, Text: "@@ -1 +1 @@"},
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "one"},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 30, Height: 4}
	app.Pump(size)
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := p.Cell(29, 2).Background; got != uiDiffCursorRowBackground(uiDiffTestTheme()) {
		t.Fatalf("initial cursor background = %v, want code row", got)
	}
}

func TestUIDiffViewCanCursorToRenderedCommitMessage(t *testing.T) {
	doc, err := diff.Parse("commit abc123\nAuthor: Example <example@example.com>\n\n    hello world\n")
	if err != nil {
		t.Fatal(err)
	}
	rows := doc.Rows()
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 40, Height: 6}
	app.Pump(size)
	app.Pump(size)

	for range 3 {
		app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	}
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	row := uiDiffPainterRowContaining(p, "hello world")
	if row == -1 {
		t.Fatal("commit message was not rendered")
	}
	if got := p.Cell(size.Width-1, row).Background; got != uiDiffCursorRowBackground(uiDiffTestTheme()) {
		t.Fatalf("commit message cursor background = %v, want cursor row", got)
	}
	if got := p.Cell(0, row).Background; got != uiDiffCursorBackground(uiDiffTestTheme()) {
		t.Fatalf("commit message cursor cell background = %v, want cursor", got)
	}
}

func TestUIDiffViewCommitMessageCursorFillsRow(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowCommitMessage, Text: "    message"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 30, Height: 3}
	app.Pump(size)
	app.Pump(size)
	p := vui.NewPainter(size)
	app.Paint(p)
	if got := p.Cell(size.Width-1, 0).Background; got != uiDiffCursorRowBackground(uiDiffTestTheme()) {
		t.Fatalf("commit message row edge background = %v, want cursor row", got)
	}
}

func TestUIDiffViewCommitMessageWraps(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowCommitMessage, Text: "    this is a long commit message that should wrap"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	size := vui.Size{Width: 24, Height: 5}
	app.Pump(size)
	app.Pump(size)

	p := vui.NewPainter(size)
	app.Paint(p)
	if got := uiDiffPainterText(p, 0); !strings.Contains(got, "this is a long") {
		t.Fatalf("first commit message row = %q, want wrapped prefix", got)
	}
	if got := uiDiffPainterText(p, 1); !strings.Contains(got, "commit message") {
		t.Fatalf("second commit message row = %q, want wrapped continuation", got)
	}
}

func TestUIDiffViewYankClearsSelectionAndHighlightsText(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Gutter: "1 1   ", Code: "one"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 30, Height: 3})
	app.Pump(vui.Size{Width: 30, Height: 3})

	app.Send(vaxis.Key{Text: "V", Keycode: 'V'})
	app.Send(vaxis.Key{Text: "y", Keycode: 'y'})
	app.Pump(vui.Size{Width: 30, Height: 3})
	p := vui.NewPainter(vui.Size{Width: 30, Height: 3})
	app.Paint(p)
	if got := uiDiffPainterText(p, 2); !strings.HasPrefix(got, " NORMAL ") {
		t.Fatalf("status bar = %q, want NORMAL after yank", got)
	}
	if got := p.Cell(7, 0).Background; got != uiDiffYankBackground(uiDiffTestTheme()) {
		t.Fatalf("yank background = %v, want yank", got)
	}
	if got := p.Cell(29, 0).Background; got == uiDiffYankBackground(uiDiffTestTheme()) {
		t.Fatal("yank highlight extends past text")
	}
}

func TestUIDiffViewYankHighlightExpires(t *testing.T) {
	state := &uiDiffViewState{
		yankActive:   true,
		yankLinewise: true,
		yankAnchor:   selectionPoint{Row: 0},
		yankCursor:   selectionPoint{Row: 1},
		yankUntil:    time.Unix(10, 0),
	}
	state.clearExpiredYank(time.Unix(9, 0))
	if !state.yankActive {
		t.Fatal("yank highlight expired early")
	}
	state.clearExpiredYank(time.Unix(10, 0))
	if state.yankActive || state.yankLinewise || !state.yankUntil.IsZero() {
		t.Fatalf("yank highlight still active: active=%v linewise=%v until=%v", state.yankActive, state.yankLinewise, state.yankUntil)
	}
}

func TestUIDiffViewVisualLineStatusMode(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Gutter: "1 1   ", Code: "one"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 40, Height: 3})
	app.Pump(vui.Size{Width: 40, Height: 3})

	app.Send(vaxis.Key{Text: "V", Keycode: 'V'})
	app.Pump(vui.Size{Width: 40, Height: 3})
	p := vui.NewPainter(vui.Size{Width: 40, Height: 3})
	app.Paint(p)
	if got := uiDiffPainterText(p, 2); !strings.HasPrefix(got, " V-LINE ") {
		t.Fatalf("status bar = %q, want V-LINE mode", got)
	}
}

func TestUIDiffViewVisualCharDoesNotEnterLinewiseSelection(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "one"},
		{Kind: diff.RowContext, Gutter: "2 2   ", Code: "two"},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 30, Height: 4})
	app.Pump(vui.Size{Width: 30, Height: 4})

	app.Send(vaxis.Key{Text: "v", Keycode: 'v'})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Pump(vui.Size{Width: 30, Height: 4})
	p := vui.NewPainter(vui.Size{Width: 30, Height: 4})
	app.Paint(p)
	theme := uiDiffTestTheme()
	if got := p.Cell(29, 0).Background; got == theme.Selection {
		t.Fatal("visual character mode selected the full anchor row")
	}
	if got := uiDiffPainterText(p, 3); !strings.HasPrefix(got, " VISUAL ") {
		t.Fatalf("status bar = %q, want VISUAL mode", got)
	}
}

func TestUIDiffViewVisualCharSelectsSameLineRange(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Gutter: "1 1   ", Code: "abcdef"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 20, Height: 3})
	app.Pump(vui.Size{Width: 20, Height: 3})

	app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
	app.Send(vaxis.Key{Text: "v", Keycode: 'v'})
	app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
	app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
	app.Pump(vui.Size{Width: 20, Height: 3})
	p := vui.NewPainter(vui.Size{Width: 20, Height: 3})
	app.Paint(p)
	theme := uiDiffTestTheme()
	for col := 7; col <= 9; col++ {
		if col == 9 {
			continue
		}
		if got := p.Cell(col, 0).Background; got != theme.Selection {
			t.Fatalf("selected char col %d background = %v, want selection", col, got)
		}
	}
	if got := p.Cell(9, 0).Background; got != uiDiffCursorBackground(theme) {
		t.Fatalf("selected cursor background = %v, want cursor", got)
	}
	if got := p.Cell(6, 0).Background; got == theme.Selection {
		t.Fatal("character before selection is highlighted")
	}
	if got := p.Cell(10, 0).Background; got == theme.Selection {
		t.Fatal("character after selection is highlighted")
	}
}

func TestUIDiffViewVisualCharStillRendersCursor(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Gutter: "1 1   ", Code: "abcdef"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 20, Height: 3})
	app.Pump(vui.Size{Width: 20, Height: 3})

	app.Send(vaxis.Key{Text: "v", Keycode: 'v'})
	app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
	app.Pump(vui.Size{Width: 20, Height: 3})
	p := vui.NewPainter(vui.Size{Width: 20, Height: 3})
	app.Paint(p)
	cell := p.Cell(7, 0)
	if cell.Grapheme != "b" {
		t.Fatalf("cursor grapheme = %q, want b", cell.Grapheme)
	}
	if cell.Background != uiDiffCursorBackground(uiDiffTestTheme()) {
		t.Fatalf("cursor background = %v, want cursor", cell.Background)
	}
	if cell.Foreground != uiDiffCursorForeground(uiDiffTestTheme()) {
		t.Fatalf("cursor foreground = %v, want cursor contrast", cell.Foreground)
	}
}

func TestUIDiffViewVisualCharTabCursorUsesLastTabCell(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Gutter: "1 1   ", Code: "a\tb"}}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 20, Height: 3})
	app.Pump(vui.Size{Width: 20, Height: 3})

	app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
	app.Send(vaxis.Key{Text: "v", Keycode: 'v'})
	app.Pump(vui.Size{Width: 20, Height: 3})
	p := vui.NewPainter(vui.Size{Width: 20, Height: 3})
	app.Paint(p)
	for col := 7; col < 14; col++ {
		if got := p.Cell(col, 0).Background; got == uiDiffCursorBackground(uiDiffTestTheme()) {
			t.Fatalf("tab cursor rendered too early at col %d", col)
		}
	}
	if got := p.Cell(14, 0).Background; got != uiDiffCursorBackground(uiDiffTestTheme()) {
		t.Fatalf("tab cursor background = %v, want cursor on last tab cell", got)
	}
}

func TestUIDiffViewVisualCharSelectsPartialCrossLineRange(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "abcd"},
		{Kind: diff.RowContext, Gutter: "2 2   ", Code: "wxyz"},
	}
	app := newUIDiffTestAppWithBaseDraftsAndStatus(rows, DefaultBaseColors(), false, nil, true)
	app.Pump(vui.Size{Width: 20, Height: 4})
	app.Pump(vui.Size{Width: 20, Height: 4})

	app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
	app.Send(vaxis.Key{Text: "v", Keycode: 'v'})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
	app.Pump(vui.Size{Width: 20, Height: 4})
	p := vui.NewPainter(vui.Size{Width: 20, Height: 4})
	app.Paint(p)
	theme := uiDiffTestTheme()
	for col := 7; col <= 9; col++ {
		if got := p.Cell(col, 0).Background; got != theme.Selection {
			t.Fatalf("first row selected col %d background = %v, want selection", col, got)
		}
	}
	if got := p.Cell(6, 0).Background; got == theme.Selection {
		t.Fatal("first row character before selection is highlighted")
	}
	for col := 6; col <= 8; col++ {
		if col == 8 {
			continue
		}
		if got := p.Cell(col, 1).Background; got != theme.Selection {
			t.Fatalf("second row selected col %d background = %v, want selection", col, got)
		}
	}
	if got := p.Cell(8, 1).Background; got != uiDiffCursorBackground(theme) {
		t.Fatalf("second row cursor background = %v, want cursor", got)
	}
	if got := p.Cell(9, 1).Background; got == theme.Selection {
		t.Fatal("second row character after selection is highlighted")
	}
}

func TestUIDiffViewEscapeClearsLinewiseSelection(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "one"},
		{Kind: diff.RowContext, Gutter: "2 2   ", Code: "two"},
	}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 30, Height: 2})
	app.Pump(vui.Size{Width: 30, Height: 2})

	app.Send(vaxis.Key{Text: "V", Keycode: 'V'})
	app.Send(vaxis.Key{Text: "j", Keycode: 'j'})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(vui.Size{Width: 30, Height: 2})
	p := vui.NewPainter(vui.Size{Width: 30, Height: 2})
	app.Paint(p)
	theme := uiDiffTestTheme()
	if got := p.Cell(29, 0).Background; got == theme.Selection {
		t.Fatal("anchor row still selected after escape")
	}
	if got := p.Cell(29, 1).Background; got != uiDiffCursorRowBackground(theme) {
		t.Fatalf("cursor row background = %v, want selection", got)
	}
}

func TestUIDiffViewLineBoundaryKeys(t *testing.T) {
	tests := []struct {
		name      string
		keys      []vaxis.Key
		wantCol   int
		wantGlyph string
	}{
		{
			name:      "l moves cursor right",
			keys:      []vaxis.Key{{Text: "l", Keycode: 'l'}},
			wantCol:   7,
			wantGlyph: "b",
		},
		{
			name:      "h moves cursor left",
			keys:      []vaxis.Key{{Text: "l", Keycode: 'l'}, {Text: "l", Keycode: 'l'}, {Text: "h", Keycode: 'h'}},
			wantCol:   7,
			wantGlyph: "b",
		},
		{
			name:      "0 moves to code start",
			keys:      []vaxis.Key{{Text: "l", Keycode: 'l'}, {Text: "l", Keycode: 'l'}, {Text: "0", Keycode: '0'}},
			wantCol:   6,
			wantGlyph: "a",
		},
		{
			name:      "$ moves to code end",
			keys:      []vaxis.Key{{Text: "$", Keycode: '$'}},
			wantCol:   11,
			wantGlyph: "f",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := []diff.Row{{Kind: diff.RowAdd, Gutter: "1 1   ", Marker: "+", Code: "abcdef"}}
			app := newUIDiffTestApp(rows, false)
			app.Pump(vui.Size{Width: 20, Height: 3})
			app.Pump(vui.Size{Width: 20, Height: 3})
			for _, key := range tt.keys {
				app.Send(key)
				app.Pump(vui.Size{Width: 20, Height: 3})
			}

			p := vui.NewPainter(vui.Size{Width: 20, Height: 3})
			app.Paint(p)
			cell := p.Cell(tt.wantCol, 0)
			if cell.Grapheme != tt.wantGlyph {
				t.Fatalf("cursor glyph = %q, want %q", cell.Grapheme, tt.wantGlyph)
			}
			if cell.Background != uiDiffCursorBackground(uiDiffTestTheme()) {
				t.Fatalf("cursor background = %v, want yank", cell.Background)
			}
			if cell.Foreground != uiDiffCursorForeground(uiDiffTestTheme()) {
				t.Fatalf("cursor foreground = %v, want contrast foreground", cell.Foreground)
			}
		})
	}
}

func TestUIDiffViewHorizontalMovementUsesTabStops(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Gutter: "1 1   ", Code: "a\tb"}}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 20, Height: 3})
	app.Pump(vui.Size{Width: 20, Height: 3})

	app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
	app.Pump(vui.Size{Width: 20, Height: 3})
	p := vui.NewPainter(vui.Size{Width: 20, Height: 3})
	app.Paint(p)
	if got := p.Cell(7, 0).Background; got != uiDiffCursorBackground(uiDiffTestTheme()) {
		t.Fatalf("tab cursor first cell background = %v, want cursor", got)
	}
	for col := 8; col < 15; col++ {
		if got := p.Cell(col, 0).Background; got != uiDiffCursorRowBackground(uiDiffTestTheme()) {
			t.Fatalf("tab cursor remainder background at col %d = %v, want row selection", col, got)
		}
	}

	app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
	app.Pump(vui.Size{Width: 20, Height: 3})
	p = vui.NewPainter(vui.Size{Width: 20, Height: 3})
	app.Paint(p)
	if cell := p.Cell(15, 0); cell.Grapheme != "b" || cell.Background != uiDiffCursorBackground(uiDiffTestTheme()) {
		t.Fatalf("cursor after tab = %q/%v, want b/yank", cell.Grapheme, cell.Background)
	}

	app.Send(vaxis.Key{Text: "h", Keycode: 'h'})
	app.Pump(vui.Size{Width: 20, Height: 3})
	p = vui.NewPainter(vui.Size{Width: 20, Height: 3})
	app.Paint(p)
	if got := p.Cell(7, 0).Background; got != uiDiffCursorBackground(uiDiffTestTheme()) {
		t.Fatalf("tab cursor after h first cell background = %v, want cursor", got)
	}
	for col := 8; col < 15; col++ {
		if got := p.Cell(col, 0).Background; got != uiDiffCursorRowBackground(uiDiffTestTheme()) {
			t.Fatalf("tab cursor after h remainder background at col %d = %v, want row selection", col, got)
		}
	}
}

func TestUIDiffViewHorizontalMovementStopsAtLineEnd(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowContext, Gutter: "1 1   ", Code: "abc"}}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 20, Height: 3})
	app.Pump(vui.Size{Width: 20, Height: 3})

	for i := 0; i < 10; i++ {
		app.Send(vaxis.Key{Text: "l", Keycode: 'l'})
		app.Pump(vui.Size{Width: 20, Height: 3})
	}

	p := vui.NewPainter(vui.Size{Width: 20, Height: 3})
	app.Paint(p)
	cell := p.Cell(8, 0)
	if cell.Grapheme != "c" || cell.Background != uiDiffCursorBackground(uiDiffTestTheme()) {
		t.Fatalf("cursor after repeated l = %q/%v, want c/yank", cell.Grapheme, cell.Background)
	}
	if got := p.Cell(9, 0).Background; got == uiDiffCursorBackground(uiDiffTestTheme()) {
		t.Fatal("cursor highlighted past end of line")
	}
}

func TestUIDiffViewJumpCommitScrollsTargetToTop(t *testing.T) {
	rows := make([]diff.Row, 30)
	for i := range rows {
		rows[i] = diff.Row{Kind: diff.RowContext, Gutter: "1 1   ", Code: "line"}
	}
	rows[0] = diff.Row{Kind: diff.RowCommitHeader, Text: "commit one"}
	rows[12] = diff.Row{Kind: diff.RowCommitHeader, Text: "commit two"}
	rows[28] = diff.Row{Kind: diff.RowCommitHeader, Text: "commit three"}
	app := newUIDiffTestApp(rows, false)
	app.Pump(vui.Size{Width: 20, Height: 4})
	app.Pump(vui.Size{Width: 20, Height: 4})

	app.Send(vaxis.Key{Text: "J", Keycode: 'J'})
	app.Pump(vui.Size{Width: 20, Height: 4})
	app.Pump(vui.Size{Width: 20, Height: 4})
	p := vui.NewPainter(vui.Size{Width: 20, Height: 4})
	app.Paint(p)
	if got := uiDiffPainterText(p, 0); got != "commit two" {
		t.Fatalf("top row after J = %q, want commit two", got)
	}
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 0 {
		t.Fatalf("highlight row after J = %d, want 0", got)
	}

	app.Send(vaxis.Key{Text: "J", Keycode: 'J'})
	app.Pump(vui.Size{Width: 20, Height: 4})
	app.Pump(vui.Size{Width: 20, Height: 4})
	p = vui.NewPainter(vui.Size{Width: 20, Height: 4})
	app.Paint(p)
	if got := uiDiffPainterText(p, 2); got != "commit three" {
		t.Fatalf("visible row after final J = %q, want commit three", got)
	}
	if got := uiDiffHighlightedScreenRow(p, uiDiffCursorRowBackground(uiDiffTestTheme())); got != 2 {
		t.Fatalf("highlight row after final J = %d, want 2", got)
	}

	app.Send(vaxis.Key{Text: "K", Keycode: 'K'})
	app.Pump(vui.Size{Width: 20, Height: 4})
	app.Pump(vui.Size{Width: 20, Height: 4})
	p = vui.NewPainter(vui.Size{Width: 20, Height: 4})
	app.Paint(p)
	if got := uiDiffPainterText(p, 0); got != "commit two" {
		t.Fatalf("top row after K = %q, want commit two", got)
	}
}

func uiDiffHighlightedScreenRow(p *vui.Painter, bg vaxis.Color) int {
	size := p.Size()
	for row := 0; row < size.Height; row++ {
		for col := 0; col < size.Width; col++ {
			if p.Cell(col, row).Background == bg {
				return row
			}
		}
	}
	return -1
}

func uiDiffPainterText(p *vui.Painter, row int) string {
	size := p.Size()
	text := ""
	for col := 0; col < size.Width; col++ {
		text += p.Cell(col, row).Grapheme
	}
	return strings.TrimRight(text, " ")
}

func uiDiffFindText(p *vui.Painter, text string) (int, int, bool) {
	size := p.Size()
	for row := 0; row < size.Height; row++ {
		if col, ok := uiDiffFindTextOnRow(p, row, text); ok {
			return col, row, true
		}
	}
	return 0, 0, false
}

func uiDiffFindTextOnRow(p *vui.Painter, row int, text string) (int, bool) {
	size := p.Size()
	line := ""
	for col := 0; col < size.Width; col++ {
		line += p.Cell(col, row).Grapheme
	}
	col := strings.Index(line, text)
	return col, col >= 0
}

func TestUIDiffViewAltPTogglesProfileOverlay(t *testing.T) {
	app := newUIDiffTestApp([]diff.Row{{Kind: diff.RowContext, Code: "line"}}, false)
	app.Pump(vui.Size{Width: 20, Height: 4})
	app.Send(vaxis.Key{Text: "p", Keycode: 'p', Modifiers: vaxis.ModAlt})
	if !app.ProfileOverlay() {
		t.Fatal("profile overlay not enabled")
	}
	app.Send(vaxis.Key{Text: "p", Keycode: 'p', Modifiers: vaxis.ModAlt})
	if app.ProfileOverlay() {
		t.Fatal("profile overlay not disabled")
	}
}

func TestUIDiffViewWrapsCodeThroughMeasuredRows(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowContext, Gutter: "1 1   ", Code: "abcdefghij"},
		{Kind: diff.RowContext, Gutter: "2 2   ", Code: "next"},
	}
	app := newUIDiffTestApp(rows, true)
	app.Pump(vui.Size{Width: 11, Height: 4})
	app.Pump(vui.Size{Width: 11, Height: 4})

	p := vui.NewPainter(vui.Size{Width: 11, Height: 4})
	app.Paint(p)
	if got := p.Cell(6, 0).Grapheme; got != "a" {
		t.Fatalf("wrapped first line start = %q, want a", got)
	}
	if got := p.Cell(6, 1).Grapheme; got != "f" {
		t.Fatalf("wrapped second line start = %q, want f", got)
	}
	if got := p.Cell(6, 2).Grapheme; got != "n" {
		t.Fatalf("next row after wrapped row = %q, want n", got)
	}
}
