//nolint:unused
package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rockorager/go-uucode"
	"go.rockorager.dev/vaxis"

	"go.rockorager.dev/comview/diff"
	"go.rockorager.dev/comview/review"
)

const (
	pendingKeyTimeout        = 800 * time.Millisecond
	multiClickTimeout        = 500 * time.Millisecond
	yankHighlightDuration    = 180 * time.Millisecond
	statusMessageTimeout     = 2 * time.Second
	mouseWheelScrollLines    = 1
	mouseWheelScrollColumns  = 1
	horizontalScrollbarThumb = "\U0001FB0B"
)

const (
	mouseWheelLeft  vaxis.MouseButton = 66
	mouseWheelRight vaxis.MouseButton = 67
)

type selectionPoint struct {
	Row int
	Col int
}

type searchMatch struct {
	Row   int
	Start int
	End   int
}

type textObjectKind int

const (
	textObjectInner textObjectKind = iota
	textObjectAround
)

type textObjectState struct {
	active bool
	kind   textObjectKind
	at     time.Time
}

type clickState struct {
	Point selectionPoint
	At    time.Time
	Count int
}

type sideBySideRow struct {
	Full  int
	Left  int
	Right int
}

type diffSide int

const (
	sideLeft diffSide = iota
	sideRight
)

type statusStats struct {
	Adds    int
	Deletes int
}

type statusContext struct {
	CommitIndex int
	Commits     int
	Commit      string
	FileIndex   int
	Files       int
	File        string
	FileStats   statusStats
	TotalStats  statusStats
}

type textObjectBounds struct {
	Start     int
	End       int
	Side      diffSide
	Code      map[int]string
	CodeStart map[int]int
	CodeWidth map[int]int
	TabWidth  map[int]int
}

type textObjectPosition struct {
	Row int
	Col int
}

type helpKeybind struct {
	Key       string
	READMEKey string
	Action    string
}

var helpKeybinds = []helpKeybind{
	{Key: "j/k, arrows", READMEKey: "`j`/`k`, arrows", Action: "Move"},
	{Key: "h/l", READMEKey: "`h`/`l`", Action: "Move horizontally"},
	{Key: "gg / G", READMEKey: "`gg` / `G`", Action: "Top / bottom"},
	{Key: "Ctrl-d / Ctrl-u", READMEKey: "`Ctrl-d` / `Ctrl-u`", Action: "Half-page down / up"},
	{Key: "J / K", READMEKey: "`J` / `K`", Action: "Next / previous commit"},
	{Key: "]c / [c", READMEKey: "`]c` / `[c`", Action: "Next / previous change"},
	{Key: "]n / [n", READMEKey: "`]n` / `[n`", Action: "Next / previous note"},
	{Key: "s", READMEKey: "`s`", Action: "Toggle side-by-side view"},
	{Key: "t", READMEKey: "`t`", Action: "Choose theme"},
	{Key: "Space e", READMEKey: "`<space>e`", Action: "Find file in diff"},
	{Key: "/", READMEKey: "`/`", Action: "Search"},
	{Key: "n / N", READMEKey: "`n` / `N`", Action: "Next / previous search result"},
	{Key: "o", READMEKey: "`o`", Action: "Open cursor location in editor"},
	{Key: "v / V", READMEKey: "`v` / `V`", Action: "Visual / visual-line selection"},
	{Key: "iw, aw, i{, a\", etc.", READMEKey: "`iw`, `aw`, `i{`, `a\"`, etc.", Action: "Text objects, naturally flawless"},
	{Key: "y", READMEKey: "`y`", Action: "Copy selection"},
	{Key: "i or I", READMEKey: "`i` or `I`", Action: "Add/edit comment"},
	{Key: "x / dd", READMEKey: "`x` / `dd`", Action: "Delete note under cursor"},
	{Key: ":w", READMEKey: "`:w`", Action: "Save comments"},
	{Key: ":q / :q!", READMEKey: "`:q` / `:q!`", Action: "Quit / force quit"},
	{Key: "?", READMEKey: "`?`", Action: "Show this help"},
	{Key: "Esc", READMEKey: "`Esc`", Action: "Cancel"},
}

func helpKeybindWidth() int {
	width := 0
	for _, binding := range helpKeybinds {
		width = maxInt(width, textCellWidth(binding.Key))
	}
	return width
}

func rowsForInput(input string) ([]diff.Row, error) {
	doc, err := diff.Parse(input)
	if err != nil {
		return nil, err
	}
	return doc.RowsWithOptions(diff.DefaultRenderOptions()), nil
}

func mouseWheelButton(button vaxis.MouseButton) bool {
	return button == vaxis.MouseWheelDown ||
		button == vaxis.MouseWheelUp ||
		button == mouseWheelLeft ||
		button == mouseWheelRight
}

func keyEscape(key vaxis.Key) bool {
	return key.Matches(vaxis.KeyEsc) || key.MatchString("Escape")
}

func keyQuestionMark(key vaxis.Key) bool {
	if key.Matches('?') {
		return true
	}
	return key.Modifiers&vaxis.ModShift != 0 && (key.Keycode == '/' || key.Text == "/")
}

func pureModifierKey(key vaxis.Key) bool {
	if key.Text != "" {
		return false
	}
	switch key.Keycode {
	case vaxis.KeyLeftShift, vaxis.KeyRightShift, vaxis.KeyL3Shift, vaxis.KeyL5Shift,
		vaxis.KeyLeftControl, vaxis.KeyRightControl,
		vaxis.KeyLeftAlt, vaxis.KeyRightAlt,
		vaxis.KeyLeftSuper, vaxis.KeyRightSuper,
		vaxis.KeyLeftHyper, vaxis.KeyRightHyper,
		vaxis.KeyLeftMeta, vaxis.KeyRightMeta:
		return true
	default:
		return false
	}
}

func selectionPointLess(a selectionPoint, b selectionPoint) bool {
	if a.Row != b.Row {
		return a.Row < b.Row
	}
	return a.Col < b.Col
}

func statDetail(stat diff.Stat) string {
	return fmt.Sprintf("+%d -%d", stat.Adds, stat.Deletes)
}

func rowsStats(rows []diff.Row) statusStats {
	var stats statusStats
	foundSummary := false
	for _, row := range rows {
		if row.Kind != diff.RowDiffStatSummary {
			continue
		}
		foundSummary = true
		stats.Adds += row.Stat.Adds
		stats.Deletes += row.Stat.Deletes
	}
	if foundSummary {
		return stats
	}

	for _, row := range rows {
		switch row.Kind {
		case diff.RowAdd:
			stats.Adds++
		case diff.RowDelete:
			stats.Deletes++
		case diff.RowDiffStat:
			stats.Adds += row.Stat.Adds
			stats.Deletes += row.Stat.Deletes
		}
	}
	return stats
}

func (s statusStats) String() string {
	return fmt.Sprintf("+%d -%d", s.Adds, s.Deletes)
}

func pluralize(count int, singular string) string {
	if count == 1 {
		return singular
	}
	return singular + "s"
}

func commentLines(body string) []string {
	if body == "" {
		return []string{""}
	}
	lines := strings.Split(body, "\n")
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func textObjectKeyRune(key vaxis.Key) rune {
	if key.ShiftedCode != 0 {
		return key.ShiftedCode
	}
	if key.Modifiers&vaxis.ModShift != 0 {
		if shifted, ok := shiftedTextObjectRune(key.Keycode); ok {
			return shifted
		}
		if r, _ := utf8.DecodeRuneInString(key.Text); r != utf8.RuneError {
			if shifted, ok := shiftedTextObjectRune(r); ok {
				return shifted
			}
		}
	}
	if key.Text != "" {
		r, _ := utf8.DecodeRuneInString(key.Text)
		return r
	}
	return key.Keycode
}

func shiftedTextObjectRune(r rune) (rune, bool) {
	switch r {
	case '9':
		return '(', true
	case '0':
		return ')', true
	case '[':
		return '{', true
	case ']':
		return '}', true
	case '\'':
		return '"', true
	case '`':
		return '~', true
	case ',':
		return '<', true
	case '.':
		return '>', true
	default:
		return 0, false
	}
}

func selectableDiffRow(kind diff.RowKind) bool {
	switch kind {
	case diff.RowContext, diff.RowAdd, diff.RowDelete, diff.RowCommitHeader, diff.RowCommitMeta, diff.RowCommitMessage, diff.RowCommitTrailer:
		return true
	default:
		return false
	}
}

func textObjectRowsContiguous(row diff.Row) bool {
	return selectableDiffRow(row.Kind)
}

func rowOnTextObjectSide(row diff.Row, side diffSide) bool {
	switch row.Kind {
	case diff.RowContext:
		return true
	case diff.RowDelete:
		return side == sideLeft
	case diff.RowAdd:
		return side == sideRight
	default:
		return false
	}
}

func uiDiffSideBySideRowsForRows(rows []diff.Row) []sideBySideRow {
	out := make([]sideBySideRow, 0, len(rows))
	contextLeft := true
	contextRight := true
	for i := 0; i < len(rows); {
		if rows[i].Kind == diff.RowDelete {
			deleteStart := i
			for i < len(rows) && rows[i].Kind == diff.RowDelete {
				i++
			}
			addStart := i
			for i < len(rows) && rows[i].Kind == diff.RowAdd {
				i++
			}
			for deleteOffset, addOffset := 0, 0; deleteOffset < addStart-deleteStart || addOffset < i-addStart; {
				row := sideBySideRow{Full: -1, Left: -1, Right: -1}
				if deleteOffset < addStart-deleteStart {
					row.Left = deleteStart + deleteOffset
					deleteOffset++
				}
				if addOffset < i-addStart {
					row.Right = addStart + addOffset
					addOffset++
				}
				out = append(out, row)
			}
			continue
		}
		switch rows[i].Kind {
		case diff.RowHunk:
			contextLeft, contextRight = sideBySideHunkContextSides(rows, i)
			out = append(out, sideBySideRow{Full: i, Left: -1, Right: -1})
		case diff.RowAdd:
			out = append(out, sideBySideRow{Full: -1, Left: -1, Right: i})
		case diff.RowContext:
			row := sideBySideRow{Full: -1, Left: -1, Right: -1}
			if contextLeft {
				row.Left = i
			}
			if contextRight {
				row.Right = i
			}
			out = append(out, row)
		default:
			contextLeft = true
			contextRight = true
			out = append(out, sideBySideRow{Full: i, Left: -1, Right: -1})
		}
		i++
	}
	return out
}

func sideBySideHunkContextSides(rows []diff.Row, hunk int) (bool, bool) {
	hasDeletes := false
	hasAdds := false
	for i := hunk + 1; i < len(rows); i++ {
		switch rows[i].Kind {
		case diff.RowDelete:
			hasDeletes = true
		case diff.RowAdd:
			hasAdds = true
		case diff.RowHunk, diff.RowFile, diff.RowMeta, diff.RowCommitHeader, diff.RowCommitMeta, diff.RowCommitMessage, diff.RowCommitTrailer, diff.RowDiffStat, diff.RowDiffStatSummary, diff.RowBlank:
			return sideBySideContextSides(hasDeletes, hasAdds)
		}
	}
	return sideBySideContextSides(hasDeletes, hasAdds)
}

func sideBySideContextSides(hasDeletes bool, hasAdds bool) (bool, bool) {
	switch {
	case hasAdds && !hasDeletes:
		return false, true
	case hasDeletes && !hasAdds:
		return true, false
	default:
		return true, true
	}
}

func rowContainsDocRow(row sideBySideRow, docRow int) bool {
	return row.Full == docRow || row.Left == docRow || row.Right == docRow
}

func sideBySideRowCommentDocRows(row sideBySideRow) []int {
	rows := make([]int, 0, 2)
	for _, docRow := range []int{row.Full, row.Left, row.Right} {
		if docRow < 0 {
			continue
		}
		seen := false
		for _, existing := range rows {
			if existing == docRow {
				seen = true
				break
			}
		}
		if !seen {
			rows = append(rows, docRow)
		}
	}
	return rows
}

func sideBySideRowFirstDoc(row sideBySideRow) int {
	first := -1
	for _, docRow := range []int{row.Full, row.Left, row.Right} {
		if docRow >= 0 && (first < 0 || docRow < first) {
			first = docRow
		}
	}
	return first
}

func sideBySideDocRowForSide(row sideBySideRow, side diffSide) int {
	if row.Full >= 0 {
		return row.Full
	}
	if side == sideLeft {
		return firstAvailableDocRow(row.Left, row.Right)
	}
	return firstAvailableDocRow(row.Right, row.Left)
}

func firstAvailableDocRow(preferred int, fallback int) int {
	if preferred >= 0 {
		return preferred
	}
	return fallback
}

func sideBySideCursorDocRowForSide(row sideBySideRow, side diffSide) int {
	if row.Full >= 0 {
		return row.Full
	}
	if side == sideLeft {
		return row.Left
	}
	return row.Right
}

func sideForRow(row diff.Row) diffSide {
	if row.Kind == diff.RowDelete {
		return sideLeft
	}
	return sideRight
}

func uiDiffSideBySideGutterForRows(rows []diff.Row, row diff.Row, side diffSide) string {
	width := sideBySideLineNumberWidth(rows)
	oldNumber, newNumber := splitGutterNumbers(row)
	number := ""
	marker := " "
	if side == sideLeft {
		number = oldNumber
		if row.Kind == diff.RowDelete {
			marker = "-"
		}
	} else {
		number = newNumber
		if row.Kind == diff.RowAdd {
			marker = "+"
		}
	}
	return fmt.Sprintf("%*s %s ", width, number, marker)
}

func sideBySideLineNumberWidth(rows []diff.Row) int {
	width := 1
	for _, row := range rows {
		oldNumber, newNumber := splitGutterNumbers(row)
		width = maxInt(width, len(oldNumber))
		width = maxInt(width, len(newNumber))
	}
	return width
}

func splitGutterNumbers(row diff.Row) (string, string) {
	fields := strings.Fields(row.Gutter)
	switch row.Kind {
	case diff.RowContext:
		if len(fields) >= 2 {
			return fields[0], fields[1]
		}
	case diff.RowDelete:
		if len(fields) >= 1 {
			return fields[0], ""
		}
	case diff.RowAdd:
		if len(fields) >= 1 {
			return "", fields[0]
		}
	}
	return "", ""
}

func reviewAnchorValid(anchor review.Anchor) bool {
	return anchor.Path != "" && anchor.Line > 0 && anchor.Side != ""
}

func reviewDraftMatchesTarget(draft review.CommentDraft, target review.CommentDraft) bool {
	if draft.Path != target.Path ||
		draft.Line != target.Line ||
		draft.Side != target.Side ||
		draft.CommitID != target.CommitID ||
		draft.OriginalCommitID != target.OriginalCommitID ||
		draft.StartLine != target.StartLine ||
		draft.StartSide != target.StartSide {
		return false
	}
	return optionalIntEqual(draft.StartColumn, target.StartColumn) &&
		optionalIntEqual(draft.EndColumn, target.EndColumn)
}

func optionalIntEqual(a *int, b *int) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return *a == *b
	}
}

func reviewDraftEndsAt(draft review.CommentDraft, anchor review.Anchor) bool {
	return draft.Path == anchor.Path &&
		draft.Line == anchor.Line &&
		draft.Side == anchor.Side &&
		draft.CommitID == anchor.CommitID &&
		draft.OriginalCommitID == anchor.OriginalCommitID
}

func reviewDraftContains(draft review.CommentDraft, anchor review.Anchor) bool {
	if draft.Path != anchor.Path ||
		draft.CommitID != anchor.CommitID ||
		draft.OriginalCommitID != anchor.OriginalCommitID {
		return false
	}
	if draft.StartLine == 0 {
		return draft.Line == anchor.Line && draft.Side == anchor.Side
	}
	if draft.StartSide != anchor.Side || draft.Side != anchor.Side {
		return draft.Line == anchor.Line && draft.Side == anchor.Side ||
			draft.StartLine == anchor.Line && draft.StartSide == anchor.Side
	}
	start := minInt(draft.StartLine, draft.Line)
	end := maxInt(draft.StartLine, draft.Line)
	return anchor.Side == draft.Side && anchor.Line >= start && anchor.Line <= end
}

func noteTargetRows(rows []diff.Row, drafts []review.CommentDraft) []int {
	return buildCommentIndex(rows, drafts).targetRows()
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

func splitStatusPathBase(text string) (string, string) {
	if text == "" {
		return "", ""
	}

	countPrefix := ""
	path := text
	if space := strings.Index(text, " "); space >= 0 && isStatusCountPrefix(text[:space]) && space+1 < len(text) {
		countPrefix = text[:space+1]
		path = text[space+1:]
	}
	if slash := strings.LastIndex(path, "/"); slash >= 0 && slash+1 < len(path) {
		return countPrefix + path[:slash+1], path[slash+1:]
	}
	return countPrefix, path
}

func isStatusCountPrefix(text string) bool {
	before, after, ok := strings.Cut(text, "/")
	if !ok || before == "" || after == "" {
		return false
	}
	for _, r := range before + after {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func textCellWidth(text string) int {
	width := 0
	for _, char := range vaxis.Characters(text) {
		width += char.Width
	}
	return width
}

func editorColumnAtCell(text string, target int, tabWidth int) int {
	if target < 0 {
		return 1
	}

	column := 1
	cell := 0
	it := uucode.NewGraphemeIterator(text)
	for g, ok := it.Next(); ok; g, ok = it.Next() {
		next := cell + graphemeCellWidthWithTabWidth(text[g.Start:g.End], tabWidth)
		if target < next {
			return column
		}
		cell = next
		column++
	}
	return column
}

func characterAtCell(text string, target int) vaxis.Character {
	return characterAtCellWithTabWidth(text, target, defaultTabWidth)
}

func characterAtCellWithTabWidth(text string, target int, tabWidth int) vaxis.Character {
	if target < 0 {
		target = 0
	}

	col := 0
	it := uucode.NewGraphemeIterator(text)
	for g, ok := it.Next(); ok; g, ok = it.Next() {
		char := characterForGraphemeWithTabWidth(text[g.Start:g.End], tabWidth)
		next := col + char.Width
		if target >= col && target < next {
			if target == col && char.Width == 1 {
				return char
			}
			break
		}
		col = next
	}
	return vaxis.Character{
		Grapheme: " ",
		Width:    1,
	}
}

func runeAtCellWithTabWidth(text string, target int, tabWidth int) rune {
	return []rune(characterAtCellWithTabWidth(text, target, tabWidth).Grapheme)[0]
}

func runeColumnAtCell(text string, target int) int {
	col := 0
	index := 0
	it := uucode.NewGraphemeIterator(text)
	for g, ok := it.Next(); ok; g, ok = it.Next() {
		cluster := text[g.Start:g.End]
		next := col + graphemeCellWidth(cluster)
		if target < next {
			return index
		}
		col = next
		index += utf8.RuneCountInString(cluster)
	}
	return index
}

func rowRuneAtCell(row diff.Row, target int) rune {
	return []rune(rowCharacterAtCell(row, target).Grapheme)[0]
}

func rowCharacterAtCell(row diff.Row, target int) vaxis.Character {
	codeStart := textCellWidth(row.Gutter + row.Marker)
	if row.Code != "" && (row.Gutter != "" || row.Marker != "") && target >= codeStart {
		return characterAtCellWithTabWidth(row.Code, target-codeStart, tabWidthForFile(row.FileName))
	}
	if row.Code != "" && row.Text == row.Code {
		return characterAtCellWithTabWidth(row.Code, target, tabWidthForFile(row.FileName))
	}
	return characterAtCell(row.Text, target)
}

func isSpaceRune(r rune) bool {
	return uucode.IsSpace(r)
}

func cellTextRange(text string, start int, end int) string {
	if start < 0 {
		start = 0
	}
	if end <= start {
		return ""
	}

	var out strings.Builder
	col := 0
	it := uucode.NewGraphemeIterator(text)
	for g, ok := it.Next(); ok; g, ok = it.Next() {
		cluster := text[g.Start:g.End]
		next := col + graphemeCellWidth(cluster)
		if next > start && col < end {
			out.WriteString(cluster)
		}
		col = next
		if col >= end {
			break
		}
	}
	return out.String()
}

func rowCellTextRange(row diff.Row, start int, end int) string {
	codeStart := textCellWidth(row.Gutter + row.Marker)
	if row.Code == "" || ((row.Gutter == "" && row.Marker == "") && row.Text != row.Code) {
		return cellTextRange(row.Text, start, end)
	}

	var out strings.Builder
	if start < codeStart {
		out.WriteString(cellTextRange(row.Gutter+row.Marker, start, minInt(end, codeStart)))
	}
	if end > codeStart {
		out.WriteString(cellTextRangeWithTabWidth(row.Code, maxInt(0, start-codeStart), end-codeStart, tabWidthForFile(row.FileName)))
	}
	return out.String()
}

func cellTextRangeWithTabWidth(text string, start int, end int, tabWidth int) string {
	if start < 0 {
		start = 0
	}
	if end <= start {
		return ""
	}

	var out strings.Builder
	col := 0
	it := uucode.NewGraphemeIterator(text)
	for g, ok := it.Next(); ok; g, ok = it.Next() {
		cluster := text[g.Start:g.End]
		next := col + graphemeCellWidthWithTabWidth(cluster, tabWidth)
		if next > start && col < end {
			out.WriteString(cluster)
		}
		col = next
		if col >= end {
			break
		}
	}
	return out.String()
}

func graphemeCellWidth(grapheme string) int {
	return graphemeCellWidthWithTabWidth(grapheme, defaultTabWidth)
}

const defaultTabWidth = 8

func graphemeCellWidthWithTabWidth(grapheme string, tabWidth int) int {
	if grapheme == "\t" {
		return tabWidth
	}
	return uucode.StringWidth(grapheme)
}

func tabWidthForFile(fileName string) int {
	if fileName == "" {
		return defaultTabWidth
	}
	if strings.HasSuffix(fileName, ".go") || fileName == "go.mod" {
		return defaultTabWidth
	}
	return 4
}

func codeCellWidth(row diff.Row) int {
	return textCellWidthWithTabWidth(row.Code, tabWidthForFile(row.FileName))
}

func textCellWidthWithTabWidth(text string, tabWidth int) int {
	width := 0
	it := uucode.NewGraphemeIterator(text)
	for g, ok := it.Next(); ok; g, ok = it.Next() {
		width += graphemeCellWidthWithTabWidth(text[g.Start:g.End], tabWidth)
	}
	return width
}

func rowTextCellWidth(row diff.Row) int {
	if row.Code != "" && (row.Gutter != "" || row.Marker != "") {
		return textCellWidth(row.Gutter+row.Marker) + codeCellWidth(row)
	}
	if row.Code != "" && row.Text == row.Code {
		return codeCellWidth(row)
	}
	return textCellWidth(row.Text)
}

func rowTokenRangeAt(row diff.Row, col int) (int, int) {
	codeStart := textCellWidth(row.Gutter + row.Marker)
	if row.Code != "" && (row.Gutter != "" || row.Marker != "") && col >= codeStart {
		start, end := tokenRangeAtWithTabWidth(row.Code, col-codeStart, tabWidthForFile(row.FileName))
		return codeStart + start, codeStart + end
	}
	if row.Code != "" && row.Text == row.Code {
		return tokenRangeAtWithTabWidth(row.Code, col, tabWidthForFile(row.FileName))
	}
	return tokenRangeAt(row.Text, col)
}

func tokenRangeAtWithTabWidth(text string, col int, tabWidth int) (int, int) {
	cells := textCellsWithTabWidth(text, tabWidth)
	if len(cells) == 0 {
		return 0, 0
	}

	index := len(cells) - 1
	for i, cell := range cells {
		if col < cell.End {
			index = i
			break
		}
	}

	kind := cells[index].Kind
	start := index
	for start > 0 && cells[start-1].Kind == kind {
		start--
	}
	end := index + 1
	for end < len(cells) && cells[end].Kind == kind {
		end++
	}
	return cells[start].Start, cells[end-1].End
}

func textCellsWithTabWidth(text string, tabWidth int) []textCell {
	cells := make([]textCell, 0, utf8.RuneCountInString(text))
	col := 0
	it := uucode.NewGraphemeIterator(text)
	for g, ok := it.Next(); ok; g, ok = it.Next() {
		cluster := text[g.Start:g.End]
		start := col
		end := start + graphemeCellWidthWithTabWidth(cluster, tabWidth)
		col = end
		if end <= start {
			continue
		}
		cells = append(cells, textCell{Start: start, End: end, Kind: selectionTokenKind(cluster)})
	}
	return cells
}

func characterForGraphemeWithTabWidth(grapheme string, tabWidth int) vaxis.Character {
	if grapheme == "\t" {
		return vaxis.Character{Grapheme: "\t", Width: tabWidth}
	}
	chars := vaxis.Characters(grapheme)
	if len(chars) == 0 {
		return vaxis.Character{}
	}
	return chars[0]
}

type textCell struct {
	Start int
	End   int
	Kind  int
}

const (
	spaceSelectionToken = iota + 1
	wordSelectionToken
	punctuationSelectionToken
	symbolSelectionToken
)

func tokenRangeAt(text string, col int) (int, int) {
	cells := textCells(text)
	if len(cells) == 0 {
		return 0, 0
	}

	index := len(cells) - 1
	for i, cell := range cells {
		if col < cell.End {
			index = i
			break
		}
	}

	kind := cells[index].Kind
	start := index
	for start > 0 && cells[start-1].Kind == kind {
		start--
	}
	end := index + 1
	for end < len(cells) && cells[end].Kind == kind {
		end++
	}
	return cells[start].Start, cells[end-1].End
}

func textCells(text string) []textCell {
	chars := vaxis.Characters(text)
	cells := make([]textCell, 0, len(chars))
	col := 0
	for _, char := range chars {
		start := col
		end := start + char.Width
		col = end
		if char.Width <= 0 {
			continue
		}
		cells = append(cells, textCell{
			Start: start,
			End:   end,
			Kind:  selectionTokenKind(char.Grapheme),
		})
	}
	return cells
}

func findDelimitedTextObject(bounds textObjectBounds, cursor textObjectPosition, open rune, close rune) (textObjectPosition, textObjectPosition, bool) {
	if open == close {
		return findQuoteTextObject(bounds, cursor, open)
	}
	return findBracketTextObject(bounds, cursor, open, close)
}

type textObjectPair struct {
	Open  textObjectPosition
	Close textObjectPosition
}

func findBracketTextObject(bounds textObjectBounds, cursor textObjectPosition, open rune, close rune) (textObjectPosition, textObjectPosition, bool) {
	pairs := make([]textObjectPair, 0)
	stack := make([]textObjectPosition, 0)
	for pos, ok := nextTextObjectScanPosition(bounds, textObjectPosition{Row: bounds.Start, Col: -1}); ok; pos, ok = nextTextObjectScanPosition(bounds, pos) {
		r := runeAtCellWithTabWidth(rowCode(bounds, pos.Row), pos.Col, bounds.TabWidth[pos.Row])
		switch r {
		case open:
			stack = append(stack, pos)
		case close:
			if len(stack) == 0 {
				continue
			}
			openPos := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			pairs = append(pairs, textObjectPair{Open: openPos, Close: pos})
		}
	}
	return chooseTextObjectPair(pairs, cursor)
}

func chooseTextObjectPair(pairs []textObjectPair, cursor textObjectPosition) (textObjectPosition, textObjectPosition, bool) {
	best, ok := innermostContainingTextObjectPair(pairs, cursor)
	if ok {
		return best.Open, best.Close, true
	}
	best, ok = nextTextObjectPair(pairs, cursor)
	if ok {
		return best.Open, best.Close, true
	}
	return textObjectPosition{}, textObjectPosition{}, false
}

func innermostContainingTextObjectPair(pairs []textObjectPair, cursor textObjectPosition) (textObjectPair, bool) {
	var best textObjectPair
	found := false
	for _, pair := range pairs {
		if textObjectPositionLess(cursor, pair.Open) || textObjectPositionLess(pair.Close, cursor) {
			continue
		}
		if !found || textObjectPositionLess(best.Open, pair.Open) {
			best = pair
			found = true
		}
	}
	return best, found
}

func nextTextObjectPair(pairs []textObjectPair, cursor textObjectPosition) (textObjectPair, bool) {
	var best textObjectPair
	found := false
	for _, pair := range pairs {
		if !textObjectPositionLess(cursor, pair.Open) {
			continue
		}
		if !found || textObjectPositionLess(pair.Open, best.Open) {
			best = pair
			found = true
		}
	}
	return best, found
}

func findQuoteTextObject(bounds textObjectBounds, cursor textObjectPosition, delimiter rune) (textObjectPosition, textObjectPosition, bool) {
	positions := make([]textObjectPosition, 0)
	for row := bounds.Start; row <= bounds.End; row++ {
		width, ok := bounds.CodeWidth[row]
		if !ok {
			continue
		}
		for col := 0; col < width; col++ {
			if runeAtCellWithTabWidth(rowCode(bounds, row), col, bounds.TabWidth[row]) == delimiter {
				positions = append(positions, textObjectPosition{Row: row, Col: col})
			}
		}
	}
	pairs := make([]textObjectPair, 0, len(positions)/2)
	for i := 0; i+1 < len(positions); i += 2 {
		pairs = append(pairs, textObjectPair{Open: positions[i], Close: positions[i+1]})
	}
	return chooseTextObjectPair(pairs, cursor)
}

func previousTextObjectScanPosition(bounds textObjectBounds, pos textObjectPosition) (textObjectPosition, bool) {
	for row := pos.Row; row >= bounds.Start; row-- {
		width, ok := bounds.CodeWidth[row]
		if !ok || width == 0 {
			continue
		}
		col := width - 1
		if row == pos.Row {
			col = minInt(pos.Col, width-1)
		}
		if col >= 0 {
			return textObjectPosition{Row: row, Col: col}, true
		}
	}
	return textObjectPosition{}, false
}

func nextTextObjectScanPosition(bounds textObjectBounds, pos textObjectPosition) (textObjectPosition, bool) {
	for row := pos.Row; row <= bounds.End; row++ {
		width, ok := bounds.CodeWidth[row]
		if !ok || width == 0 {
			continue
		}
		col := 0
		if row == pos.Row {
			col = pos.Col + 1
		}
		if col < 0 {
			col = 0
		}
		if col < width {
			return textObjectPosition{Row: row, Col: col}, true
		}
	}
	return textObjectPosition{}, false
}

func advanceTextObjectPosition(bounds textObjectBounds, pos textObjectPosition) textObjectPosition {
	next, ok := nextTextObjectScanPosition(bounds, pos)
	if !ok {
		return pos
	}
	return next
}

func previousTextObjectPosition(bounds textObjectBounds, pos textObjectPosition) textObjectPosition {
	prev, ok := previousTextObjectScanPosition(bounds, textObjectPosition{Row: pos.Row, Col: pos.Col - 1})
	if !ok {
		return pos
	}
	return prev
}

func textObjectPositionLess(a textObjectPosition, b textObjectPosition) bool {
	if a.Row != b.Row {
		return a.Row < b.Row
	}
	return a.Col < b.Col
}

func textObjectDelimiters(object rune) (rune, rune, bool) {
	switch object {
	case '\'', '"', '`':
		return object, object, true
	case '(', ')':
		return '(', ')', true
	case '[', ']':
		return '[', ']', true
	case '{', '}':
		return '{', '}', true
	case '<', '>':
		return '<', '>', true
	default:
		return 0, 0, false
	}
}

func rowCode(bounds textObjectBounds, row int) string {
	return bounds.Code[row]
}

func selectionTokenKind(text string) int {
	r, _ := utf8.DecodeRuneInString(text)
	switch {
	case uucode.IsSpace(r):
		return spaceSelectionToken
	case uucode.IsLetter(r) || uucode.IsDigit(r) || r == '_':
		return wordSelectionToken
	case uucode.IsPunct(r):
		return punctuationSelectionToken
	default:
		return symbolSelectionToken
	}
}

func styleSegmentsRangeFull(segments []vaxis.Segment, start int, end int, style vaxis.Style) []vaxis.Segment {
	return styleSegmentsRangeFullWithTabWidth(segments, start, end, style, defaultTabWidth)
}

func styleSegmentsRangeFullWithTabWidth(segments []vaxis.Segment, start int, end int, style vaxis.Style, tabWidth int) []vaxis.Segment {
	if start >= end {
		return segments
	}

	var styled []vaxis.Segment
	col := 0
	for _, segment := range segments {
		it := uucode.NewGraphemeIterator(segment.Text)
		for g, ok := it.Next(); ok; g, ok = it.Next() {
			grapheme := segment.Text[g.Start:g.End]
			char := characterForGraphemeWithTabWidth(grapheme, tabWidth)
			next := col + char.Width
			charStyle := segment.Style
			if next > start && col < end {
				if style.Foreground != vaxis.ColorDefault {
					charStyle.Foreground = style.Foreground
				}
				if style.Background != vaxis.ColorDefault {
					charStyle.Background = style.Background
				}
				if style.UnderlineColor != vaxis.ColorDefault {
					charStyle.UnderlineColor = style.UnderlineColor
				}
				if style.UnderlineStyle != vaxis.UnderlineOff {
					charStyle.UnderlineStyle = style.UnderlineStyle
				}
				charStyle.Attribute |= style.Attribute
			}
			styled = appendSegment(styled, vaxis.Segment{Text: grapheme, Style: charStyle})
			col = next
		}
	}
	return styled
}

func appendSegment(segments []vaxis.Segment, segment vaxis.Segment) []vaxis.Segment {
	if segment.Text == "" {
		return segments
	}
	last := len(segments) - 1
	if last >= 0 && segments[last].Style == segment.Style {
		segments[last].Text += segment.Text
		return segments
	}
	return append(segments, segment)
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
