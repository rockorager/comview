package tui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rockorager/go-uucode"
	"go.rockorager.dev/vaxis"
	vui "go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/widgets/term"

	"go.rockorager.dev/comview/diff"
	"go.rockorager.dev/comview/review"
)

type uiDiffView struct {
	Rows          []diff.Row
	Wrap          bool
	ReviewDrafts  []review.CommentDraft
	ReviewFile    string
	WorkTreeRoot  string
	ShowStatus    bool
	Binds         Bindings
	EmptyMessage  string
	EmptyHint     string
	InitialStatus string
}

const uiDiffSyntaxHighlightRowLimit = 5000

func uiDiffRootWithStatus(rows []diff.Row, wrap bool, drafts []review.CommentDraft, showStatus bool) vui.Widget {
	return uiDiffRootWithReviewFile(rows, wrap, drafts, "", showStatus)
}

func uiDiffRootWithReviewFile(rows []diff.Row, wrap bool, drafts []review.CommentDraft, reviewFile string, showStatus bool) vui.Widget {
	return uiDiffRootWithReviewFileAndBindings(rows, wrap, drafts, reviewFile, showStatus, Bindings{})
}

func uiDiffRootWithReviewFileAndBindings(rows []diff.Row, wrap bool, drafts []review.CommentDraft, reviewFile string, showStatus bool, binds Bindings) vui.Widget {
	return uiDiffView{Rows: rows, Wrap: wrap, ReviewDrafts: drafts, ReviewFile: reviewFile, ShowStatus: showStatus, Binds: binds}
}

func uiThemeFromBaseColors(base BaseColors) vui.Theme {
	return vui.ThemeFromPalette(vui.PaletteFromBaseColors(vui.BaseColors{
		Black:   base.Background,
		Red:     base.Red,
		Green:   base.Green,
		Yellow:  base.Yellow,
		Blue:    base.Blue,
		Magenta: base.Magenta,
		Cyan:    base.Cyan,
		White:   base.Foreground,
	}), vui.DarkTheme)
}

func (w uiDiffView) CreateState() vui.State {
	return &uiDiffViewState{}
}

type uiDiffViewState struct {
	vui.StateBase
	scroll                    vui.ScrollController
	list                      vui.SliverListController
	cursor                    selectionPoint
	cursorCol                 int
	pendingG                  bool
	pendingBracket            rune
	pendingSpace              bool
	pendingD                  bool
	fileFinder                bool
	themeFinder               bool
	themeName                 string
	themeNameBeforePick       string
	helpVisible               bool
	sideBySide                bool
	xScroll                   int
	textObject                textObjectState
	commentEditorActive       bool
	commentEditorFocused      bool
	commentEditorInsert       bool
	commentEditorRow          int
	commentEditorTarget       uiDiffCommentTarget
	commentEditorBody         string
	commentEditorCursor       *int
	commentEditorNormalCursor int
	commentEditorSelection    bool
	commentEditorLinewise     bool
	commentEditorAnchor       int
	commentEditorBodies       map[int]string
	reviewDrafts              []review.CommentDraft
	deletedReviewDrafts       map[review.CommentDraft]bool
	selectionAnchor           selectionPoint
	selectionActive           bool
	selectionLinewise         bool
	selectionInitialNewline   bool
	selectionFinalNewline     bool
	selectionSideFiltered     bool
	selectionSide             diffSide
	mouseSelecting            bool
	hScrollbarDragging        bool
	hScrollbarGrab            int
	mouseAnchor               selectionPoint
	mouseHasAnchor            bool
	mouseStartRow             int
	clicks                    clickState
	yankAnchor                selectionPoint
	yankCursor                selectionPoint
	yankActive                bool
	yankLinewise              bool
	yankUntil                 time.Time
	searchMode                bool
	searchQuery               string
	searchMatches             []searchMatch
	searchIndex               int
	searchStart               selectionPoint
	commandMode               bool
	commandLine               string
	reviewDirty               bool
	statusMessage             string
	statusMessageUntil        time.Time
	editorCommand             *exec.Cmd
	syntaxTheme               vui.Theme
	highlighter               *SyntaxHighlighter
	highlightedTheme          vui.Theme
	highlightedRows           map[int][]vaxis.Segment
	metricsRows               []diff.Row
	metrics                   uiDiffDocumentMetrics
	sideRowsRows              []diff.Row
	sideRows                  []sideBySideRow
}

type uiDiffDocumentMetrics struct {
	HasCommitMessage bool
	OldGutterWidth   int
	NewGutterWidth   int
	ContentWidth     int
}

type uiDiffCommentTarget struct {
	Draft review.CommentDraft
	Row   int
}

func (s *uiDiffViewState) Build(ctx vui.BuildContext) vui.Widget {
	w := s.Widget().(uiDiffView)
	if s.statusMessage == "" && w.InitialStatus != "" {
		s.statusMessage = w.InitialStatus
		s.statusMessageUntil = time.Now().Add(statusMessageTimeout)
	}
	s.clearExpiredYank(time.Now())
	s.clearExpiredStatusMessage(time.Now())
	s.clampCursor(w.Rows)
	theme := vui.MustDepend[vui.Theme](ctx)
	if s.themeName != "" {
		if selected, ok := ThemeByName(s.themeName); ok {
			theme = uiThemeFromBaseColors(selected.Colors)
		}
	}
	highlightedRows := s.highlightedCodeRows(w.Rows, theme)
	metrics := s.documentMetrics(w.Rows)
	drafts := s.allReviewDrafts(w.ReviewDrafts)
	sliver := vui.Widget(vui.SliverListBuilder{
		Controller:          &s.list,
		Count:               len(w.Rows),
		ItemExtent:          uiDiffItemExtent(w.Wrap || metrics.HasCommitMessage || s.commentEditorActive || len(drafts) > 0),
		EstimatedItemExtent: 1,
		Overscan:            8,
		Builder: func(ctx vui.BuildContext, row int) vui.Widget {
			return s.buildItem(w.Rows, row, theme, highlightedRows, drafts, w.Wrap, metrics)
		},
	})
	if s.sideBySide {
		sideRows := s.sideBySideRows(w.Rows)
		sliver = vui.SliverListBuilder{
			Controller:          &s.list,
			Count:               len(sideRows),
			ItemExtent:          uiDiffItemExtent(s.commentEditorActive || len(s.commentEditorBodies) > 0 || len(drafts) > 0),
			EstimatedItemExtent: 1,
			Overscan:            8,
			Builder: func(ctx vui.BuildContext, row int) vui.Widget {
				return s.buildSideBySideItem(w.Rows, sideRows[row], theme, highlightedRows, drafts, w.Wrap, metrics)
			},
		}
	}
	scrollView := vui.Widget(vui.CustomScrollView{
		Controller: &s.scroll,
		Slivers:    []vui.Widget{sliver},
	})
	if len(w.Rows) > 0 && w.ShowStatus {
		scrollView = vui.Scrollbar{Child: scrollView}
	}
	if len(w.Rows) == 0 {
		message := w.EmptyMessage
		if message == "" {
			message = "Pipe git diff or git show into comview."
		}
		hint := w.EmptyHint
		if hint == "" {
			hint = "Run comview watch to refresh git diff as files change."
		}
		scrollView = vui.Padding(vui.All(1), vui.Flex{
			Axis: vui.Vertical,
			Children: []vui.Widget{
				vui.Text{Value: message, Style: vui.Style{Foreground: theme.Foreground, Background: theme.Background}},
				vui.SizedBox{Height: 1},
				vui.Text{Value: hint, Style: vui.Style{Foreground: theme.MutedForeground, Background: theme.Background}},
				vui.Text{Value: "Use :q to quit.", Style: vui.Style{Foreground: theme.MutedForeground, Background: theme.Background}},
			},
		})
	}
	content := vui.Widget(scrollView)
	if w.ShowStatus {
		children := []vui.Widget{vui.Expanded(scrollView)}
		if bar, ok := s.horizontalScrollbar(w.Rows, w.Wrap, theme, metrics); ok {
			children = append(children, bar)
		}
		children = append(children, s.buildStatusBar(w.Rows, theme))
		content = vui.Flex{
			Axis:               vui.Vertical,
			CrossAxisAlignment: vui.CrossAxisStretch,
			Children:           children,
		}
	}
	content = vui.DecoratedBox(
		vui.Decoration{Style: vui.Style{Background: theme.Background}},
		vui.SizedBox{Width: 10000, Height: 10000, Child: content},
	)
	entries := []vui.OverlayEntry{}
	if s.fileFinder {
		entries = append(entries, vui.OverlayEntry{Modal: true, Child: s.buildFileFinder(w.Rows, theme)})
	}
	if s.themeFinder {
		entries = append(entries, vui.OverlayEntry{Modal: true, Child: s.buildThemeFinder()})
	}
	if s.helpVisible {
		entries = append(entries, vui.OverlayEntry{Modal: true, Child: s.buildHelpOverlay(theme)})
	}
	if s.editorCommand != nil {
		entries = append(entries, vui.OverlayEntry{Modal: true, Child: s.buildEditorTerminal(theme)})
	}
	root := vui.Widget(vui.Overlay{Child: content, Entries: entries})
	if s.themeName != "" {
		root = vui.Provider[vui.Theme]{Value: theme, Child: root}
	}
	return root
}

func (s *uiDiffViewState) buildHelpOverlay(theme vui.Theme) vui.Widget {
	style := vui.Style{Foreground: theme.Foreground, Background: theme.SurfaceRaised}
	keyStyle := vui.Style{Foreground: theme.MutedForeground, Background: theme.SurfaceRaised}
	titleStyle := vui.Style{Foreground: theme.Palette.Yellow.Tone500, Background: theme.SurfaceRaised, Attribute: vaxis.AttrBold}
	keyWidth := helpKeybindWidth()
	children := []vui.Widget{
		vui.Text{Value: "Keybinds", Style: titleStyle},
		vui.SizedBox{Height: 1},
	}
	for _, binding := range helpKeybinds {
		children = append(children, vui.RichText{Spans: []vui.TextSpan{
			{Text: fmt.Sprintf("%-*s", keyWidth, binding.Key), Style: keyStyle},
			{Text: "  " + binding.Action, Style: style},
		}, MaxLines: 1, Overflow: vui.TextOverflowEllipsis})
	}
	return vui.ConstrainedBox{
		Constraints: vui.Constraints{MaxWidth: uiDiffHelpOverlayWidth(), MaxHeight: len(helpKeybinds) + 4},
		Child: vui.DecoratedBox(
			vui.Decoration{Style: style},
			vui.Padding(vui.Symmetric(2, 1), vui.Flex{
				Axis:               vui.Vertical,
				MainAxisSize:       vui.MainAxisSizeMin,
				CrossAxisAlignment: vui.CrossAxisStretch,
				Children:           children,
			}),
		),
	}
}

func uiDiffHelpOverlayWidth() int {
	innerWidth := len("Keybinds")
	keyWidth := helpKeybindWidth()
	for _, binding := range helpKeybinds {
		innerWidth = maxInt(innerWidth, keyWidth+2+textCellWidth(binding.Action))
	}
	return innerWidth + 4
}

func (s *uiDiffViewState) buildThemeFinder() vui.Widget {
	return vui.FuzzySelect[Theme]{
		Items:          Themes,
		Item:           uiThemeSelectItem,
		Filter:         s.themePreviewFilter,
		Placeholder:    "Choose theme…",
		EmptyText:      "No matching themes",
		MaxVisibleRows: 8,
		RowStyle:       vui.FuzzySelectOneLine,
		OnDismiss: func(vui.EventContext) {
			s.themeFinder = false
			s.themeName = s.themeNameBeforePick
			s.themeNameBeforePick = ""
			s.SetState(func() {})
		},
		OnSelected: func(_ vui.EventContext, theme Theme) {
			s.themeFinder = false
			s.themeName = theme.Name
			s.themeNameBeforePick = ""
			s.setStatusMessage("Theme: " + theme.Name)
		},
	}
}

func (s *uiDiffViewState) themePreviewFilter(query string, items []Theme, item vui.FuzzySelectItemFunc[Theme]) []Theme {
	filtered := vui.DefaultFuzzySelectFilter(query, items, item)
	if len(filtered) > 0 {
		s.themeName = filtered[0].Name
	} else {
		s.themeName = s.themeNameBeforePick
	}
	return filtered
}

func uiThemeSelectItem(theme Theme) vui.FuzzySelectItem {
	return vui.FuzzySelectItem{Title: theme.Name, Aliases: []string{theme.Name}}
}

func (s *uiDiffViewState) horizontalScrollbar(rows []diff.Row, wrap bool, theme vui.Theme, metrics uiDiffDocumentMetrics) (vui.Widget, bool) {
	if wrap || len(rows) == 0 {
		return nil, false
	}
	verticalVisible := false
	if s.scroll.Attached() {
		verticalVisible = s.scroll.Metrics().MaxScrollOffset > 0
	}
	contentWidth := metrics.ContentWidth
	if contentWidth == 0 {
		return nil, false
	}
	return uiDiffHorizontalScrollbar{ContentWidth: contentWidth, XScroll: s.xScroll, SideBySide: s.sideBySide, VerticalVisible: verticalVisible, Theme: theme}, true
}

func (s *uiDiffViewState) horizontalContentWidthForGutterWidths(rows []diff.Row, oldWidth int, newWidth int) int {
	width := 0
	if s.sideBySide {
		gutterWidth := maxInt(oldWidth, newWidth)
		if gutterWidth < 1 {
			gutterWidth = 1
		}
		for _, row := range rows {
			if selectableDiffRow(row.Kind) {
				width = maxInt(width, gutterWidth+3+codeCellWidth(row))
			}
		}
		return width
	}
	codeOffset := uiDiffCodeOffsetForGutterWidths(oldWidth, newWidth)
	for _, row := range rows {
		rowWidth := textCellWidth(row.Text)
		if selectableDiffRow(row.Kind) && (row.Code != "" || row.Gutter != "" || row.Marker != "") {
			rowWidth = codeOffset + codeCellWidth(row)
		}
		width = maxInt(width, rowWidth)
	}
	return width
}

type uiDiffHorizontalScrollbar struct {
	ContentWidth    int
	XScroll         int
	SideBySide      bool
	VerticalVisible bool
	Theme           vui.Theme
}

func (w uiDiffHorizontalScrollbar) CreateRenderObject(vui.BuildContext) vui.RenderObject {
	return &uiDiffRenderHorizontalScrollbar{ContentWidth: w.ContentWidth, XScroll: w.XScroll, SideBySide: w.SideBySide, VerticalVisible: w.VerticalVisible, Theme: w.Theme}
}

func (w uiDiffHorizontalScrollbar) UpdateRenderObject(_ vui.BuildContext, ro vui.RenderObject) {
	r := ro.(*uiDiffRenderHorizontalScrollbar)
	if r.ContentWidth != w.ContentWidth || r.XScroll != w.XScroll || r.SideBySide != w.SideBySide || r.VerticalVisible != w.VerticalVisible || r.Theme != w.Theme {
		r.ContentWidth = w.ContentWidth
		r.XScroll = w.XScroll
		r.SideBySide = w.SideBySide
		r.VerticalVisible = w.VerticalVisible
		r.Theme = w.Theme
		r.MarkNeedsLayout()
	}
}

type uiDiffRenderHorizontalScrollbar struct {
	vui.LeafRenderObject
	ContentWidth    int
	XScroll         int
	SideBySide      bool
	VerticalVisible bool
	Theme           vui.Theme
}

func (r *uiDiffRenderHorizontalScrollbar) Layout(_ vui.LayoutContext, c vui.Constraints) {
	r.SetSize(r.size(c))
}

func (r *uiDiffRenderHorizontalScrollbar) DryLayout(_ vui.LayoutContext, c vui.Constraints) vui.Size {
	return r.size(c)
}

func (r *uiDiffRenderHorizontalScrollbar) size(c vui.Constraints) vui.Size {
	width := c.MaxWidth
	if width < 0 || width == vui.Unbounded {
		width = r.ContentWidth
	}
	if r.scrollbar(width).Length == 0 {
		return c.Constrain(vui.Size{Width: width, Height: 0})
	}
	return c.Constrain(vui.Size{Width: width, Height: 1})
}

func (r *uiDiffRenderHorizontalScrollbar) Paint(p *vui.Painter, off vui.Offset) {
	bar := r.scrollbar(r.Size().Width)
	if bar.Length == 0 {
		return
	}
	trackStyle := vui.Style{Foreground: r.Theme.MutedForeground, Background: r.Theme.Background}
	thumbStyle := vui.Style{Foreground: r.Theme.MutedForeground, Background: r.Theme.Background}
	for col := 0; col < bar.Length; col++ {
		cell := vaxis.Cell{Character: vaxis.Character{Grapheme: " ", Width: 1}, Style: trackStyle}
		if col >= bar.Thumb && col < bar.Thumb+bar.Size {
			cell = vaxis.Cell{Character: vaxis.Character{Grapheme: horizontalScrollbarThumb, Width: 1}, Style: thumbStyle}
		}
		p.DrawCell(vui.Point{X: off.X + col, Y: off.Y}, cell)
	}
}

func (r *uiDiffRenderHorizontalScrollbar) scrollbar(width int) uiDiffScrollbar {
	if r.VerticalVisible {
		width--
	}
	viewportWidth := width
	if r.SideBySide {
		leftWidth, _, rightWidth := uiDiffSideBySidePaneGeometry(width)
		viewportWidth = maxInt(leftWidth, rightWidth)
	}
	if width <= 0 || viewportWidth <= 0 || r.ContentWidth <= viewportWidth {
		return uiDiffScrollbar{}
	}
	thumbSize := (viewportWidth * width) / r.ContentWidth
	if thumbSize < 1 {
		thumbSize = 1
	}
	if thumbSize > width {
		thumbSize = width
	}
	maxThumbLeft := width - thumbSize
	thumbLeft := 0
	if maxScroll := r.ContentWidth - viewportWidth; maxScroll > 0 {
		thumbLeft = (minInt(r.XScroll, maxScroll) * maxThumbLeft) / maxScroll
	}
	return uiDiffScrollbar{Length: width, Thumb: thumbLeft, Size: thumbSize, Viewport: viewportWidth}
}

type uiDiffScrollbar struct {
	Length   int
	Thumb    int
	Size     int
	Viewport int
}

func (s *uiDiffViewState) buildEditorTerminal(theme vui.Theme) vui.Widget {
	border := vui.BorderLine(theme.Border)
	border.Chars = vui.BorderChars{
		TopLeft:     vui.Character{Grapheme: "╭", Width: 1},
		TopRight:    vui.Character{Grapheme: "╮", Width: 1},
		BottomLeft:  vui.Character{Grapheme: "╰", Width: 1},
		BottomRight: vui.Character{Grapheme: "╯", Width: 1},
	}
	terminal := vui.Padding(vui.All(2), vui.DecoratedBox(
		vui.Decoration{Style: vui.Style{Background: theme.Background}, Border: border},
		vui.Padding(vui.All(1), vui.SizedBox{Width: 10000, Height: 10000, Child: term.Terminal{
			Command: s.editorCommand,
			Options: []term.Option{term.WithKittyKeyboard(true)},
			OnEvent: func(_ vui.EventContext, ev vui.Event) vui.EventResult {
				closed, ok := ev.(term.EventClosed)
				if !ok {
					return vui.EventIgnored
				}
				s.editorCommand = nil
				if closed.Error != nil {
					s.setStatusMessage(fmt.Sprintf("Editor exited: %v", closed.Error))
				}
				s.SetState(func() {})
				return vui.EventHandled
			},
		}}),
	))
	return vui.FocusScope{Trap: true, AutoFocus: true, Child: vui.Stack{Children: []vui.Widget{terminal, uiFocusNextOnce{}}}}
}

type uiFocusNextOnce struct{}

func (uiFocusNextOnce) CreateState() vui.State {
	return &uiFocusNextOnceState{}
}

type uiFocusNextOnceState struct {
	vui.StateBase
	dispatched bool
}

func (s *uiFocusNextOnceState) Build(ctx vui.BuildContext) vui.Widget {
	if !s.dispatched {
		s.dispatched = true
		eventCtx := ctx.EventContext()
		ctx.Runtime().Dispatch(func() { eventCtx.FocusNext() })
	}
	return vui.SizedBox{}
}

func (s *uiDiffViewState) allReviewDrafts(base []review.CommentDraft) []review.CommentDraft {
	drafts := make([]review.CommentDraft, 0, len(base)+len(s.reviewDrafts))
	for _, draft := range base {
		if !s.deletedReviewDrafts[draft] {
			drafts = append(drafts, draft)
		}
	}
	if len(s.reviewDrafts) == 0 {
		return drafts
	}
	for _, draft := range s.reviewDrafts {
		if !s.deletedReviewDrafts[draft] {
			drafts = append(drafts, draft)
		}
	}
	return drafts
}

type uiDiffFileItem struct {
	Label  string
	Detail string
	Row    int
}

func (s *uiDiffViewState) buildFileFinder(rows []diff.Row, theme vui.Theme) vui.Widget {
	return vui.FuzzySelect[uiDiffFileItem]{
		Items:          uiDiffFileFinderItems(rows),
		Item:           func(item uiDiffFileItem) vui.FuzzySelectItem { return uiDiffFileSelectItem(item, theme) },
		Placeholder:    "Find file…",
		EmptyText:      "No matching files",
		MaxVisibleRows: 8,
		RowStyle:       vui.FuzzySelectOneLine,
		OnDismiss: func(vui.EventContext) {
			s.fileFinder = false
			s.SetState(func() {})
		},
		OnSelected: func(_ vui.EventContext, item uiDiffFileItem) {
			s.fileFinder = false
			s.setCursorRowAtStart(rows, item.Row)
		},
	}
}

func uiDiffFileSelectItem(item uiDiffFileItem, theme vui.Theme) vui.FuzzySelectItem {
	return vui.FuzzySelectItem{
		Title:       item.Label,
		Description: item.Detail,
		Aliases:     []string{item.Label},
		Trailing:    uiDiffFileStatWidget(item.Detail, theme),
	}
}

func uiDiffFileStatWidget(detail string, theme vui.Theme) vui.Widget {
	if detail == "" {
		return nil
	}
	parts := strings.Split(detail, " ")
	if len(parts) != 2 {
		return vui.Text{Value: detail, Style: vui.Style{Foreground: theme.MutedForeground}}
	}
	return vui.RichText{Spans: []vui.TextSpan{
		{Text: parts[0], Style: vui.Style{Foreground: theme.Palette.Green.Tone500}},
		{Text: " ", Style: vui.Style{Foreground: theme.MutedForeground}},
		{Text: parts[1], Style: vui.Style{Foreground: theme.Palette.Red.Tone500}},
	}, MaxLines: 1, Overflow: vui.TextOverflowEllipsis}
}

func uiDiffFileFinderItems(rows []diff.Row) []uiDiffFileItem {
	items := make([]uiDiffFileItem, 0)
	for rowIndex, row := range rows {
		switch row.Kind {
		case diff.RowFile:
			items = append(items, uiDiffFileItem{Label: row.Text, Detail: uiDiffFileStatsFromRow(rows, rowIndex).String(), Row: rowIndex})
		case diff.RowDiffStat:
			if row.FileName != "" {
				items = append(items, uiDiffFileItem{Label: row.FileName, Detail: statDetail(row.Stat), Row: rowIndex})
			}
		}
	}
	return items
}

func uiDiffFileStatsFromRow(rows []diff.Row, fileRow int) statusStats {
	if fileRow < 0 || fileRow >= len(rows) || rows[fileRow].Kind != diff.RowFile {
		return statusStats{}
	}
	fileEnd := len(rows)
	for rowIndex := fileRow + 1; rowIndex < len(rows); rowIndex++ {
		switch rows[rowIndex].Kind {
		case diff.RowFile, diff.RowCommitHeader:
			fileEnd = rowIndex
		}
		if fileEnd == rowIndex {
			break
		}
	}
	return rowsStats(rows[fileRow:fileEnd])
}

func (s *uiDiffViewState) buildStatusBar(rows []diff.Row, theme vui.Theme) vui.Widget {
	style := uiDiffStatusStyle(theme)
	if s.commandMode {
		text := ":" + s.commandLine
		return vui.Cursor{Col: textCellWidth(text), Shape: vui.CursorBeam, Child: uiDiffStatusText(text, style)}
	}
	if s.searchMode {
		return uiDiffStatusText("/"+s.searchQuery, style)
	}
	if s.statusMessage != "" {
		return uiDiffStatusText(" "+s.statusMessage, style)
	}
	return uiDiffStatusSegments(s.statusSegments(rows, theme), s.statusRightSegments(rows, theme), style)
}

func (s *uiDiffViewState) statusSegments(rows []diff.Row, theme vui.Theme) []vaxis.Segment {
	leftSegments := s.statusLeftSegments(rows, theme)
	separatorBackground := uiDiffStatusBackground(theme)
	if len(leftSegments) > 0 {
		separatorBackground = leftSegments[0].Style.Background
	}
	segments := []vaxis.Segment{
		{Text: " " + s.statusModeLabel() + " ", Style: uiDiffStatusModeStyle(theme)},
		{Text: "", Style: vaxis.Style{Foreground: theme.Primary, Background: separatorBackground}},
	}
	segments = append(segments, leftSegments...)
	return segments
}

func (s *uiDiffViewState) statusModeLabel() string {
	if s.commentEditorActive {
		if s.commentEditorInsert {
			return "INSERT"
		}
		return "NORMAL"
	}
	if !s.selectionActive {
		return "NORMAL"
	}
	if s.selectionLinewise {
		return "V-LINE"
	}
	return "VISUAL"
}

func (s *uiDiffViewState) statusLeftSegments(rows []diff.Row, theme vui.Theme) []vaxis.Segment {
	context := s.statusContext(rows)
	sections := make([]uiDiffStatusSection, 0, 2)
	if context.Commits > 0 && context.CommitIndex > 0 && context.Commit != "" {
		commit := context.Commit
		if len(commit) > 12 {
			commit = commit[:12]
		}
		if context.Commits > 1 {
			commit = fmt.Sprintf("%d/%d %s", context.CommitIndex, context.Commits, commit)
		}
		sections = append(sections, uiDiffStatusSection{Text: commit, Foreground: theme.Palette.Blue.Tone500, Background: uiDiffStatusCommitBackground(theme)})
	}
	if context.Files > 0 && context.File != "" {
		file := context.File
		if context.Files > 1 {
			file = fmt.Sprintf("%d/%d %s", context.FileIndex, context.Files, file)
		}
		sections = append(sections, uiDiffStatusSection{Text: file, Foreground: theme.Foreground, Background: uiDiffStatusBackground(theme), Separator: "", PathBase: true})
	}
	segments := uiDiffStatusSectionSegments(sections, theme)
	if context.Files > 0 && context.File != "" {
		segments = append(segments, uiDiffStatusStatsSegments(context.FileStats, theme)...)
	}
	return segments
}

func (s *uiDiffViewState) statusRightSegments(rows []diff.Row, theme vui.Theme) []vaxis.Segment {
	context := s.statusContext(rows)
	if context.TotalStats.Adds == 0 && context.TotalStats.Deletes == 0 {
		return nil
	}
	segments := []vaxis.Segment{{Text: " ", Style: uiDiffStatusStyle(theme)}}
	segments = append(segments, uiDiffStatusStatsSegments(context.TotalStats, theme)...)
	segments = append(segments, vaxis.Segment{Text: " ", Style: uiDiffStatusStyle(theme)})
	return segments
}

type uiDiffStatusSection struct {
	Text       string
	Foreground vaxis.Color
	Background vaxis.Color
	Separator  string
	PathBase   bool
}

func uiDiffStatusText(text string, style vaxis.Style) vui.Widget {
	return vui.DecoratedBox(
		vui.Decoration{Style: style},
		vui.SizedBox{Height: 1, Child: vui.Text{Value: text, Style: style}},
	)
}

func uiDiffStatusSegments(left []vaxis.Segment, right []vaxis.Segment, fillStyle vaxis.Style) vui.Widget {
	children := []vui.Widget{
		vui.Expanded(vui.RichText{Spans: uiTextSpans(left), MaxLines: 1, Overflow: vui.TextOverflowClip}),
	}
	if len(right) > 0 {
		children = append(children, vui.RichText{Spans: uiTextSpans(right), MaxLines: 1, Overflow: vui.TextOverflowClip})
	}
	return vui.DecoratedBox(
		vui.Decoration{Style: fillStyle},
		vui.SizedBox{Height: 1, Child: vui.Row(children...)},
	)
}

func uiDiffStatusStyle(theme vui.Theme) vaxis.Style {
	return vaxis.Style{Foreground: theme.Foreground, Background: uiDiffStatusBackground(theme)}
}

func uiDiffStatusModeStyle(theme vui.Theme) vaxis.Style {
	return vaxis.Style{Foreground: uiDiffStatusBackground(theme), Background: theme.Primary, Attribute: vaxis.AttrBold}
}

func uiDiffStatusAddStyle(theme vui.Theme) vaxis.Style {
	return vaxis.Style{Foreground: theme.Success, Background: uiDiffStatusBackground(theme), Attribute: vaxis.AttrBold}
}

func uiDiffStatusDeleteStyle(theme vui.Theme) vaxis.Style {
	return vaxis.Style{Foreground: theme.Danger, Background: uiDiffStatusBackground(theme), Attribute: vaxis.AttrBold}
}

func uiDiffStatusBackground(theme vui.Theme) vaxis.Color {
	return theme.Surface
}

func uiDiffStatusCommitBackground(theme vui.Theme) vaxis.Color {
	if theme.Mode == vui.LightTheme {
		return theme.Palette.Blue.Tone100
	}
	return theme.Palette.Blue.Tone950
}

func uiDiffStatusSectionSegments(sections []uiDiffStatusSection, theme vui.Theme) []vaxis.Segment {
	segments := make([]vaxis.Segment, 0, len(sections)*2)
	for index, section := range sections {
		if section.Text == "" {
			continue
		}
		nextBackground := uiDiffStatusBackground(theme)
		if index+1 < len(sections) {
			nextBackground = sections[index+1].Background
		}
		separator := section.Separator
		if separator == "" {
			separator = ""
		}
		separatorStyle := vaxis.Style{Foreground: section.Background, Background: nextBackground}
		if separator == "" {
			separatorStyle = vaxis.Style{Foreground: section.Foreground, Background: section.Background}
		}
		segments = append(segments, uiDiffStatusSectionTextSegments(section)...)
		segments = append(segments, vaxis.Segment{Text: separator, Style: separatorStyle})
	}
	return segments
}

func uiDiffStatusSectionTextSegments(section uiDiffStatusSection) []vaxis.Segment {
	style := vaxis.Style{Foreground: section.Foreground, Background: section.Background, Attribute: vaxis.AttrBold}
	if !section.PathBase {
		return []vaxis.Segment{{Text: " " + section.Text + " ", Style: style}}
	}
	regularStyle := style
	regularStyle.Attribute = 0
	prefix, base := splitStatusPathBase(section.Text)
	segments := make([]vaxis.Segment, 0, 3)
	segments = append(segments, vaxis.Segment{Text: " " + prefix, Style: regularStyle})
	segments = append(segments, vaxis.Segment{Text: base, Style: style})
	segments = append(segments, vaxis.Segment{Text: " ", Style: regularStyle})
	return segments
}

func uiDiffStatusStatsSegments(stats statusStats, theme vui.Theme) []vaxis.Segment {
	return []vaxis.Segment{
		{Text: " ", Style: uiDiffStatusStyle(theme)},
		{Text: fmt.Sprintf("+%d", stats.Adds), Style: uiDiffStatusAddStyle(theme)},
		{Text: " ", Style: uiDiffStatusStyle(theme)},
		{Text: fmt.Sprintf("-%d", stats.Deletes), Style: uiDiffStatusDeleteStyle(theme)},
	}
}

func (s *uiDiffViewState) statusContext(rows []diff.Row) statusContext {
	var context statusContext
	context.Commits = uiDiffCountRows(rows, diff.RowCommitHeader)
	context.Files = uiDiffCountFiles(rows)
	context.TotalStats = rowsStats(rows)
	context.CommitIndex, context.Commit = uiDiffCurrentCommitContext(rows, s.cursor.Row)
	context.FileIndex, context.File, context.FileStats = uiDiffCurrentFileContext(rows, s.cursor.Row)
	return context
}

func uiDiffCountRows(rows []diff.Row, kind diff.RowKind) int {
	count := 0
	for _, row := range rows {
		if row.Kind == kind {
			count++
		}
	}
	return count
}

func uiDiffCountFiles(rows []diff.Row) int {
	count := 0
	for _, row := range rows {
		switch row.Kind {
		case diff.RowFile, diff.RowDiffStat:
			count++
		}
	}
	return count
}

func uiDiffCurrentCommitContext(rows []diff.Row, cursorRow int) (int, string) {
	index := 0
	currentIndex := 0
	currentCommit := ""
	for rowIndex, row := range rows {
		if row.Kind != diff.RowCommitHeader {
			continue
		}
		index++
		if rowIndex <= cursorRow {
			currentIndex = index
			currentCommit = row.Code
			if currentCommit == "" {
				currentCommit = strings.TrimPrefix(row.Text, "commit ")
			}
		}
	}
	return currentIndex, currentCommit
}

func uiDiffCurrentFileContext(rows []diff.Row, cursorRow int) (int, string, statusStats) {
	fileStart := -1
	fileIndex := 0
	currentIndex := 0
	fileName := ""
	for rowIndex, row := range rows {
		switch row.Kind {
		case diff.RowCommitHeader:
			if rowIndex <= cursorRow {
				fileStart = -1
				currentIndex = 0
				fileName = ""
			}
		case diff.RowFile:
			fileIndex++
			if rowIndex <= cursorRow {
				fileStart = rowIndex
				currentIndex = fileIndex
				fileName = row.Text
			}
		case diff.RowDiffStat:
			fileIndex++
			if rowIndex <= cursorRow {
				fileStart = rowIndex
				currentIndex = fileIndex
				fileName = row.FileName
			}
		}
	}
	if fileStart < 0 {
		return 0, "", statusStats{}
	}
	fileEnd := len(rows)
	for rowIndex := fileStart + 1; rowIndex < len(rows); rowIndex++ {
		switch rows[rowIndex].Kind {
		case diff.RowFile, diff.RowDiffStat, diff.RowCommitHeader:
			fileEnd = rowIndex
		}
		if fileEnd == rowIndex {
			break
		}
	}
	return currentIndex, fileName, rowsStats(rows[fileStart:fileEnd])
}

func uiDiffItemExtent(wrap bool) int {
	if wrap {
		return 0
	}
	return 1
}

func uiDiffRowsIncludeCommitMessage(rows []diff.Row) bool {
	for _, row := range rows {
		if row.Kind == diff.RowCommitMessage {
			return true
		}
	}
	return false
}

func (s *uiDiffViewState) DidUpdateWidget(old vui.Widget) {
	oldView, ok := old.(uiDiffView)
	if !ok {
		s.highlightedRows = nil
		return
	}
	view := s.Widget().(uiDiffView)
	if !uiDiffRowsShareBacking(oldView.Rows, view.Rows) {
		s.highlightedRows = nil
	}
}

func uiDiffRowsShareBacking(a []diff.Row, b []diff.Row) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	return &a[0] == &b[0]
}

func (s *uiDiffViewState) documentMetrics(rows []diff.Row) uiDiffDocumentMetrics {
	if uiDiffRowsShareBacking(s.metricsRows, rows) {
		return s.metrics
	}
	oldWidth, newWidth := uiDiffGutterWidths(rows)
	s.metricsRows = rows
	s.metrics = uiDiffDocumentMetrics{
		HasCommitMessage: uiDiffRowsIncludeCommitMessage(rows),
		OldGutterWidth:   oldWidth,
		NewGutterWidth:   newWidth,
		ContentWidth:     s.horizontalContentWidthForGutterWidths(rows, oldWidth, newWidth),
	}
	return s.metrics
}

func (s *uiDiffViewState) sideBySideRows(rows []diff.Row) []sideBySideRow {
	if uiDiffRowsShareBacking(s.sideRowsRows, rows) {
		return s.sideRows
	}
	s.sideRowsRows = rows
	s.sideRows = uiDiffSideBySideRowsForRows(rows)
	return s.sideRows
}

func (s *uiDiffViewState) syntaxHighlighter(theme vui.Theme) *SyntaxHighlighter {
	if s.highlighter == nil || s.syntaxTheme != theme {
		s.syntaxTheme = theme
		s.highlighter = NewSyntaxHighlighterWithUITheme(uiSyntaxTheme{Theme: theme})
	}
	return s.highlighter
}

func (s *uiDiffViewState) highlightedCodeRows(rows []diff.Row, theme vui.Theme) map[int][]vaxis.Segment {
	if uiDiffHighlightableRowCount(rows) > uiDiffSyntaxHighlightRowLimit {
		s.highlightedRows = nil
		return nil
	}
	if s.highlightedRows != nil && s.highlightedTheme == theme {
		return s.highlightedRows
	}
	highlighter := s.syntaxHighlighter(theme)
	s.highlightedTheme = theme
	s.highlightedRows = highlighter.HighlightRows(rows, func(kind diff.RowKind) vaxis.Style {
		return uiStyleForDiffRow(kind, theme)
	})
	return s.highlightedRows
}

func uiDiffHighlightableRowCount(rows []diff.Row) int {
	count := 0
	for _, row := range rows {
		switch row.Kind {
		case diff.RowContext, diff.RowAdd, diff.RowDelete:
			if row.Code != "" {
				count++
			}
		}
	}
	return count
}

type uiSyntaxTheme struct {
	Theme vui.Theme
}

func (t uiSyntaxTheme) uiThemeColors() uiThemeColors {
	if t.Theme.Mode == vui.LightTheme {
		return uiThemeColors{
			Foreground: t.Theme.Foreground,
			Muted:      t.Theme.MutedForeground,
			Blue:       t.Theme.Palette.Blue.Tone700,
			Cyan:       t.Theme.Palette.Cyan.Tone700,
			Green:      t.Theme.Palette.Green.Tone700,
			Magenta:    t.Theme.Palette.Magenta.Tone700,
			Yellow:     t.Theme.Palette.Yellow.Tone800,
			Red:        t.Theme.Palette.Red.Tone700,
			Header:     t.Theme.Primary,
			Hunk:       t.Theme.Accent,
		}
	}
	return uiThemeColors{
		Foreground: t.Theme.Foreground,
		Muted:      t.Theme.MutedForeground,
		Blue:       t.Theme.Palette.Blue.Tone500,
		Cyan:       t.Theme.Palette.Cyan.Tone500,
		Green:      t.Theme.Palette.Green.Tone500,
		Magenta:    t.Theme.Palette.Magenta.Tone500,
		Yellow:     t.Theme.Palette.Yellow.Tone500,
		Red:        t.Theme.Palette.Red.Tone500,
		Header:     t.Theme.Primary,
		Hunk:       t.Theme.Accent,
	}
}

func (s *uiDiffViewState) HandleEvent(ctx vui.EventContext, ev vui.Event) vui.EventResult {
	if ctx.Phase() != vui.CapturePhase {
		return vui.EventIgnored
	}
	if s.editorCommand != nil {
		return vui.EventIgnored
	}
	if mouse, ok := ev.(vaxis.Mouse); ok {
		return s.handleMouse(ctx, mouse)
	}
	key, ok := ev.(vaxis.Key)
	if !ok || key.EventType == vaxis.EventRelease || pureModifierKey(key) {
		return vui.EventIgnored
	}
	if s.fileFinder {
		return vui.EventIgnored
	}
	if s.themeFinder {
		return vui.EventIgnored
	}
	if s.helpVisible {
		if keyQuestionMark(key) || key.Matches('q') || keyEscape(key) {
			s.helpVisible = false
			s.SetState(func() {})
			return vui.EventHandled
		}
		return vui.EventIgnored
	}
	w := s.Widget().(uiDiffView)
	rows := w.Rows
	if key.MatchString("Ctrl+c") {
		ctx.Quit()
		return vui.EventHandled
	}
	if s.commandMode {
		return s.handleCommandKey(ctx, rows, key)
	}
	if key.Matches(':') {
		s.clearPendingKeys()
		s.enterCommandMode()
		return vui.EventHandled
	}
	if len(rows) == 0 {
		return vui.EventIgnored
	}
	if s.commentEditorActive && s.commentEditorFocused && !s.commentEditorInsert && key.Matches(':') {
		s.enterCommandMode()
		return vui.EventHandled
	}
	if s.commentEditorActive && strings.TrimSpace(s.commentEditorBody) == "" && keyEscape(key) {
		s.closeCommentEditor()
		s.SetState(func() {})
		return vui.EventHandled
	}
	if s.commentEditorActive && (s.commentEditorFocused || s.commentEditorInsert) {
		return s.handleCommentEditorKey(rows, key)
	}
	if s.searchMode {
		return s.handleSearchKey(rows, key)
	}
	s.clearExpiredTextObject(time.Now())
	if s.textObject.active {
		return s.handleTextObjectKey(rows, key)
	}
	switch {
	case key.MatchString("Alt+p"):
		ctx.ToggleProfileOverlay()
		return vui.EventHandled
	case keyQuestionMark(key):
		s.clearPendingKeys()
		s.helpVisible = true
		s.SetState(func() {})
		return vui.EventHandled
	case key.Matches('t'):
		s.clearPendingKeys()
		s.themeFinder = true
		s.themeNameBeforePick = s.themeName
		s.statusMessage = ""
		s.statusMessageUntil = time.Time{}
		s.SetState(func() {})
		return vui.EventHandled
	case key.Matches('x'):
		s.clearPendingKeys()
		s.deleteReviewDraftCommand(rows, w.ReviewDrafts)
		return vui.EventHandled
	case key.Matches('d') && s.pendingD:
		s.clearPendingKeys()
		s.deleteReviewDraftCommand(rows, w.ReviewDrafts)
		return vui.EventHandled
	case key.Matches('d'):
		s.pendingG = false
		s.pendingBracket = 0
		s.pendingSpace = false
		s.pendingD = true
		return vui.EventHandled
	case w.Binds.Matches(key, "open_editor"):
		s.clearPendingKeys()
		s.openEditor()
		return vui.EventHandled
	case key.Matches('c') && s.pendingBracket == ']':
		s.clearPendingKeys()
		s.jumpChange(rows, 1)
		return vui.EventHandled
	case key.Matches('c') && s.pendingBracket == '[':
		s.clearPendingKeys()
		s.jumpChange(rows, -1)
		return vui.EventHandled
	case key.Matches('n') && s.pendingBracket == ']':
		s.clearPendingKeys()
		s.jumpNote(w.Rows, w.ReviewDrafts, 1)
		return vui.EventHandled
	case key.Matches('n') && s.pendingBracket == '[':
		s.clearPendingKeys()
		s.jumpNote(w.Rows, w.ReviewDrafts, -1)
		return vui.EventHandled
	case w.Binds.Matches(key, "search"):
		s.clearPendingKeys()
		s.enterSearchMode()
		return vui.EventHandled
	case w.Binds.Matches(key, "toggle_layout"):
		s.clearPendingKeys()
		s.sideBySide = !s.sideBySide
		s.SetState(func() {})
		return vui.EventHandled
	case key.Matches('v'):
		s.clearPendingKeys()
		s.toggleSelection(false)
		return vui.EventHandled
	case key.Matches('a') && s.selectionActive:
		s.clearPendingKeys()
		s.startTextObject(textObjectAround)
		return vui.EventHandled
	case key.Matches('V'):
		s.clearPendingKeys()
		s.toggleSelection(true)
		return vui.EventHandled
	case key.Matches('I') && s.selectionActive:
		s.clearPendingKeys()
		s.openCommentEditor(rows)
		return vui.EventHandled
	case key.Matches('i') && s.selectionActive && !s.selectionLinewise:
		s.clearPendingKeys()
		s.startTextObject(textObjectInner)
		return vui.EventHandled
	case key.Matches('i'):
		s.clearPendingKeys()
		s.openCommentEditor(rows)
		return vui.EventHandled
	case w.Binds.Matches(key, "yank"):
		s.clearPendingKeys()
		s.yankSelection(ctx, rows)
		return vui.EventHandled
	case key.Matches(vaxis.KeySpace):
		s.pendingG = false
		s.pendingBracket = 0
		s.pendingSpace = true
		return vui.EventHandled
	case key.Matches('e') && s.pendingSpace:
		s.clearPendingKeys()
		if len(uiDiffFileFinderItems(rows)) == 0 {
			return vui.EventHandled
		}
		s.fileFinder = true
		s.SetState(func() {})
		return vui.EventHandled
	case w.Binds.Matches(key, "next_result"):
		s.clearPendingKeys()
		s.moveSearchMatch(rows, 1)
		return vui.EventHandled
	case w.Binds.Matches(key, "prev_result"):
		s.clearPendingKeys()
		s.moveSearchMatch(rows, -1)
		return vui.EventHandled
	case key.Matches(vaxis.KeyEsc):
		s.clearPendingKeys()
		if s.selectionActive {
			s.clearLineSelection()
			s.SetState(func() {})
			return vui.EventHandled
		}
		if s.searchQuery != "" || len(s.searchMatches) > 0 || s.searchIndex != -1 {
			s.clearSearch()
			s.SetState(func() {})
			return vui.EventHandled
		}
		return vui.EventIgnored
	case key.Matches(']'):
		s.pendingG = false
		s.pendingBracket = ']'
		return vui.EventHandled
	case key.Matches('['):
		s.pendingG = false
		s.pendingBracket = '['
		return vui.EventHandled
	case key.Matches('g') && s.pendingG:
		s.clearPendingKeys()
		s.setCursorRow(rows, 0)
		return vui.EventHandled
	case key.Matches('g'):
		s.pendingBracket = 0
		s.pendingG = true
		return vui.EventHandled
	case key.Matches(vaxis.KeyHome):
		s.clearPendingKeys()
		s.setCursorRow(rows, 0)
		return vui.EventHandled
	case w.Binds.Matches(key, "cursor_bottom"):
		s.clearPendingKeys()
		s.setCursorRow(rows, len(rows)-1)
		return vui.EventHandled
	case w.Binds.Matches(key, "half_page_down"):
		s.clearPendingKeys()
		s.moveCursorRows(rows, s.halfPageRows())
		return vui.EventHandled
	case w.Binds.Matches(key, "half_page_up"):
		s.clearPendingKeys()
		s.moveCursorRows(rows, -s.halfPageRows())
		return vui.EventHandled
	case w.Binds.Matches(key, "next_commit"):
		s.clearPendingKeys()
		s.jumpCommit(rows, 1)
		return vui.EventHandled
	case w.Binds.Matches(key, "prev_commit"):
		s.clearPendingKeys()
		s.jumpCommit(rows, -1)
		return vui.EventHandled
	case w.Binds.Matches(key, "cursor_down"):
		s.clearPendingKeys()
		if s.moveIntoCommentEditor(rows, 1) {
			return vui.EventHandled
		}
		s.moveCursorRows(rows, 1)
		return vui.EventHandled
	case w.Binds.Matches(key, "cursor_up"):
		s.clearPendingKeys()
		if s.moveIntoCommentEditor(rows, -1) {
			return vui.EventHandled
		}
		s.moveCursorRows(rows, -1)
		return vui.EventHandled
	case w.Binds.Matches(key, "cursor_left"):
		s.clearPendingKeys()
		s.moveCursorCols(rows, -1)
		return vui.EventHandled
	case w.Binds.Matches(key, "cursor_right"):
		s.clearPendingKeys()
		s.moveCursorCols(rows, 1)
		return vui.EventHandled
	case key.Matches('0'):
		s.clearPendingKeys()
		s.moveCursorLineStart(rows)
		return vui.EventHandled
	case key.Matches('$'):
		s.clearPendingKeys()
		s.moveCursorLineEnd(rows)
		return vui.EventHandled
	default:
		s.clearPendingKeys()
		return vui.EventIgnored
	}
}

func (s *uiDiffViewState) handleMouse(_ vui.EventContext, mouse vaxis.Mouse) vui.EventResult {
	if s.fileFinder {
		return vui.EventIgnored
	}
	w := s.Widget().(uiDiffView)
	rows := w.Rows
	if len(rows) == 0 {
		return vui.EventIgnored
	}
	metrics := s.documentMetrics(rows)
	if s.handleActiveCommentEditorChromeMouse(rows, mouse) {
		return vui.EventHandled
	}
	if s.mouseInActiveCommentTextField(mouse) {
		return vui.EventIgnored
	}
	if s.handleHorizontalScrollbarMouse(w, mouse, metrics) {
		return vui.EventHandled
	}
	if mouseWheelButton(mouse.Button) {
		return s.handleMouseWheel(mouse)
	}
	switch mouse.EventType {
	case vaxis.EventPress:
		if mouse.Button != vaxis.MouseLeftButton {
			return vui.EventIgnored
		}
		if row, ok := s.commentRowForMouse(rows, mouse); ok {
			cursor := s.commentCursorOffsetForMouse(rows, row, mouse)
			s.focusCommentEditorRow(rows, row)
			s.commentEditorCursor = &cursor
			s.commentEditorInsert = true
			s.mouseSelecting = false
			s.mouseHasAnchor = false
			s.clearLineSelection()
			s.yankActive = false
			s.clearPendingKeys()
			s.SetState(func() {})
			return vui.EventHandled
		}
		point, ok := s.selectionPointForMouse(rows, mouse, metrics)
		if !ok {
			if !s.mouseInDiffViewport(mouse) {
				return vui.EventIgnored
			}
			s.mouseSelecting = true
			s.mouseHasAnchor = false
			s.mouseStartRow = s.documentRowForMouse(mouse)
			s.clearLineSelection()
			s.clearPendingKeys()
			s.SetState(func() {})
			return vui.EventHandled
		}
		s.setCursorPointWithoutReveal(rows, point)
		s.clearLineSelection()
		s.yankActive = false
		s.clearPendingKeys()
		switch s.registerClick(point, time.Now()) {
		case 2:
			s.mouseSelecting = false
			s.selectTokenAt(rows, point)
			s.SetState(func() {})
			return vui.EventHandled
		case 3:
			s.mouseSelecting = false
			s.selectRowAt(rows, point)
			s.SetState(func() {})
			return vui.EventHandled
		}
		s.mouseSelecting = true
		s.mouseAnchor = point
		s.mouseHasAnchor = true
		s.mouseStartRow = point.Row
		s.SetState(func() {})
		return vui.EventHandled
	case vaxis.EventMotion:
		if !s.mouseSelecting || mouse.Button == vaxis.MouseNoButton {
			return vui.EventIgnored
		}
		point, ok := s.selectionPointForMouse(rows, mouse, metrics)
		if !ok {
			return vui.EventIgnored
		}
		if !s.mouseHasAnchor {
			s.mouseAnchor = s.dragAnchor(rows, point)
			s.mouseHasAnchor = true
		}
		if !s.selectionActive && point == s.mouseAnchor {
			return vui.EventIgnored
		}
		if !s.selectionActive {
			s.selectionActive = true
			s.selectionLinewise = false
			s.selectionAnchor = s.mouseAnchor
		}
		s.setCursorPointWithoutReveal(rows, point)
		s.SetState(func() {})
		return vui.EventHandled
	case vaxis.EventRelease:
		if !s.mouseSelecting {
			return vui.EventIgnored
		}
		s.mouseSelecting = false
		s.mouseHasAnchor = false
		if point, ok := s.selectionPointForMouse(rows, mouse, metrics); ok {
			s.setCursorPointWithoutReveal(rows, point)
		}
		s.SetState(func() {})
		return vui.EventHandled
	default:
		return vui.EventIgnored
	}
}

func (s *uiDiffViewState) mouseInActiveCommentTextField(mouse vaxis.Mouse) bool {
	if !s.commentEditorActive || !s.commentEditorInsert {
		return false
	}
	row, offset, ok := s.commentItemRowAndOffsetForMouse(s.Widget().(uiDiffView).Rows, mouse)
	bodyRows := uiDiffCommentEditorBodyRows(s.commentEditorBody)
	return ok && row == s.commentEditorRow && offset >= 2 && offset < 2+bodyRows
}

func (s *uiDiffViewState) handleActiveCommentEditorChromeMouse(rows []diff.Row, mouse vaxis.Mouse) bool {
	if !s.commentEditorActive || !s.commentEditorInsert || mouse.EventType != vaxis.EventPress || mouse.Button != vaxis.MouseLeftButton {
		return false
	}
	row, offset, ok := s.commentItemRowAndOffsetForMouse(rows, mouse)
	bodyRows := uiDiffCommentEditorBodyRows(s.commentEditorBody)
	if !ok || row != s.commentEditorRow || offset <= 0 || (offset >= 2 && offset < 2+bodyRows) {
		return false
	}
	cursor := 0
	if offset >= 2+bodyRows {
		cursor = len([]rune(s.commentEditorBody))
	}
	s.commentEditorCursor = &cursor
	s.focusCommentEditorRow(rows, row)
	s.commentEditorInsert = true
	s.SetState(func() {})
	return true
}

func (s *uiDiffViewState) commentRowForMouse(rows []diff.Row, mouse vaxis.Mouse) (int, bool) {
	row, offset, ok := s.commentItemRowAndOffsetForMouse(rows, mouse)
	if !ok || row < 0 || row >= len(rows) || offset <= 0 {
		return 0, false
	}
	if strings.TrimSpace(s.commentEditorBodyForRow(rows, row)) == "" {
		return 0, false
	}
	return row, true
}

func (s *uiDiffViewState) commentCursorOffsetForMouse(rows []diff.Row, row int, mouse vaxis.Mouse) int {
	body := s.commentEditorBodyForRow(rows, row)
	_, offset, ok := s.commentItemRowAndOffsetForMouse(rows, mouse)
	if !ok || offset <= 1 {
		return 0
	}
	bodyRows := uiDiffCommentEditorBodyRows(body)
	if offset >= 2+bodyRows {
		return len([]rune(body))
	}
	col := mouse.Col
	if uiDiffRowUsesGrid(rows[row]) {
		col -= uiDiffCodeOffset(rows)
	}
	col -= 2
	return clampUIDiffInt(col, 0, len([]rune(body)))
}

func (s *uiDiffViewState) commentItemRowAndOffsetForMouse(rows []diff.Row, mouse vaxis.Mouse) (int, int, bool) {
	row, offset, ok := s.itemRowAndOffsetForMouse(mouse)
	if ok && offset > 0 {
		return row, offset, true
	}
	logicalY := mouse.Row
	if s.scroll.Attached() {
		metrics := s.scroll.Metrics()
		if mouse.Row < 0 || mouse.Row >= metrics.ViewportHeight {
			return 0, 0, false
		}
		logicalY = metrics.ScrollOffset + mouse.Row
	}
	visualY := 0
	for rowIndex := range rows {
		height := 1 + s.commentRowsForMouse(rows, rowIndex)
		if logicalY >= visualY && logicalY < visualY+height {
			return rowIndex, logicalY - visualY, true
		}
		visualY += height
	}
	return 0, 0, false
}

func (s *uiDiffViewState) commentRowsForMouse(rows []diff.Row, row int) int {
	if row < 0 || row >= len(rows) {
		return 0
	}
	if s.commentEditorActive && s.commentEditorRow == row {
		return uiDiffCommentEditorRows(s.commentEditorBody)
	}
	if strings.TrimSpace(s.commentEditorBodies[row]) != "" {
		return uiDiffCommentEditorRows(s.commentEditorBodies[row])
	}
	w := s.Widget().(uiDiffView)
	count := 0
	for _, draft := range reviewDraftsForRow(rows[row], s.allReviewDrafts(w.ReviewDrafts)) {
		count += uiDiffCommentEditorRows(draft.Body)
	}
	return count
}

func (s *uiDiffViewState) itemRowAndOffsetForMouse(mouse vaxis.Mouse) (int, int, bool) {
	row, ok := s.rowForMouse(mouse)
	if !ok {
		return 0, 0, false
	}
	itemOffset, ok := s.list.OffsetForIndex(row)
	if !ok {
		return 0, 0, false
	}
	logicalY := mouse.Row
	if s.scroll.Attached() {
		metrics := s.scroll.Metrics()
		if mouse.Row < 0 || mouse.Row >= metrics.ViewportHeight {
			return 0, 0, false
		}
		logicalY = metrics.ScrollOffset + mouse.Row
	}
	return row, logicalY - itemOffset, true
}

func (s *uiDiffViewState) handleHorizontalScrollbarMouse(w uiDiffView, mouse vaxis.Mouse, metrics uiDiffDocumentMetrics) bool {
	if w.Wrap || !w.ShowStatus || !s.scroll.Attached() {
		return false
	}
	scrollMetrics := s.scroll.Metrics()
	barRow := scrollMetrics.ViewportHeight
	verticalVisible := scrollMetrics.MaxScrollOffset > 0
	bar := s.horizontalScrollbarModelForContentWidth(metrics.ContentWidth, scrollMetrics.ViewportWidth, verticalVisible)
	if bar.Length == 0 {
		return false
	}
	switch mouse.EventType {
	case vaxis.EventPress:
		if mouse.Button != vaxis.MouseLeftButton || mouse.Row != barRow || mouse.Col < 0 || mouse.Col >= bar.Length {
			return false
		}
		s.hScrollbarDragging = true
		s.hScrollbarGrab = 0
		if mouse.Col >= bar.Thumb && mouse.Col < bar.Thumb+bar.Size {
			s.hScrollbarGrab = mouse.Col - bar.Thumb
		}
		s.scrollHorizontalScrollbarToContentWidth(metrics.ContentWidth, scrollMetrics.ViewportWidth, mouse.Col-s.hScrollbarGrab, verticalVisible)
		s.SetState(func() {})
		return true
	case vaxis.EventMotion:
		if !s.hScrollbarDragging || mouse.Button == vaxis.MouseNoButton {
			return false
		}
		s.scrollHorizontalScrollbarToContentWidth(metrics.ContentWidth, scrollMetrics.ViewportWidth, mouse.Col-s.hScrollbarGrab, verticalVisible)
		s.SetState(func() {})
		return true
	case vaxis.EventRelease:
		if !s.hScrollbarDragging {
			return false
		}
		s.hScrollbarDragging = false
		s.scrollHorizontalScrollbarToContentWidth(metrics.ContentWidth, scrollMetrics.ViewportWidth, mouse.Col-s.hScrollbarGrab, verticalVisible)
		s.SetState(func() {})
		return true
	default:
		return false
	}
}

func (s *uiDiffViewState) horizontalScrollbarModelForContentWidth(contentWidth int, width int, verticalVisible bool) uiDiffScrollbar {
	return (&uiDiffRenderHorizontalScrollbar{
		ContentWidth:    contentWidth,
		XScroll:         s.xScroll,
		SideBySide:      s.sideBySide,
		VerticalVisible: verticalVisible,
	}).scrollbar(width)
}

func (s *uiDiffViewState) scrollHorizontalScrollbarToContentWidth(contentWidth int, width int, thumbLeft int, verticalVisible bool) {
	bar := s.horizontalScrollbarModelForContentWidth(contentWidth, width, verticalVisible)
	if bar.Length == 0 {
		return
	}
	maxScroll := contentWidth - bar.Viewport
	maxThumbLeft := bar.Length - bar.Size
	if maxScroll <= 0 || maxThumbLeft <= 0 {
		s.xScroll = 0
		return
	}
	thumbLeft = clampUIDiffInt(thumbLeft, 0, maxThumbLeft)
	s.xScroll = (thumbLeft * maxScroll) / maxThumbLeft
}

func (s *uiDiffViewState) handleMouseWheel(mouse vaxis.Mouse) vui.EventResult {
	switch mouse.Button {
	case mouseWheelLeft:
		return s.scrollHorizontallyBy(-mouseWheelScrollColumns)
	case mouseWheelRight:
		return s.scrollHorizontallyBy(mouseWheelScrollColumns)
	}
	if !s.mouseSelecting || !s.selectionActive {
		return vui.EventIgnored
	}
	if !s.mouseInDiffViewport(mouse) {
		return vui.EventIgnored
	}
	var lines int
	switch mouse.Button {
	case vaxis.MouseWheelDown:
		lines = mouseWheelScrollLines
	case vaxis.MouseWheelUp:
		lines = -mouseWheelScrollLines
	default:
		return vui.EventIgnored
	}
	if lines == 0 {
		return vui.EventIgnored
	}
	s.scroll.ScrollByLines(lines)
	w := s.Widget().(uiDiffView)
	if point, ok := s.selectionPointForMouse(w.Rows, mouse, s.documentMetrics(w.Rows)); ok {
		s.setCursorPointWithoutReveal(w.Rows, point)
	}
	s.SetState(func() {})
	return vui.EventHandled
}

func (s *uiDiffViewState) mouseInDiffViewport(mouse vaxis.Mouse) bool {
	if mouse.Row < 0 || mouse.Col < 0 {
		return false
	}
	if !s.scroll.Attached() {
		return true
	}
	metrics := s.scroll.Metrics()
	if mouse.Row >= metrics.ViewportHeight || mouse.Col >= metrics.ViewportWidth {
		return false
	}
	if metrics.MaxScrollOffset > 0 && mouse.Col == metrics.ViewportWidth-1 {
		return false
	}
	return true
}

func (s *uiDiffViewState) scrollHorizontallyBy(cols int) vui.EventResult {
	if cols == 0 {
		return vui.EventIgnored
	}
	next := s.xScroll + cols
	if next < 0 {
		next = 0
	}
	if next == s.xScroll {
		return vui.EventIgnored
	}
	s.xScroll = next
	s.SetState(func() {})
	return vui.EventHandled
}

func (s *uiDiffViewState) dragAnchor(rows []diff.Row, point selectionPoint) selectionPoint {
	_, end, ok := uiDiffCodeRange(rows, point.Row)
	if !ok {
		return point
	}
	switch {
	case point.Row > s.mouseStartRow:
		return selectionPoint{Row: point.Row, Col: 0}
	case point.Row < s.mouseStartRow:
		return selectionPoint{Row: point.Row, Col: maxInt(0, end-1)}
	default:
		return point
	}
}

func (s *uiDiffViewState) registerClick(point selectionPoint, now time.Time) int {
	if s.clicks.Point == point && now.Sub(s.clicks.At) <= multiClickTimeout {
		s.clicks.Count++
	} else {
		s.clicks.Count = 1
	}
	if s.clicks.Count > 3 {
		s.clicks.Count = 1
	}
	s.clicks.Point = point
	s.clicks.At = now
	return s.clicks.Count
}

func (s *uiDiffViewState) selectTokenAt(rows []diff.Row, point selectionPoint) {
	if point.Row < 0 || point.Row >= len(rows) || !selectableDiffRow(rows[point.Row].Kind) {
		return
	}
	row := rows[point.Row]
	start, end := tokenRangeAtWithTabWidth(row.Code, point.Col, tabWidthForFile(row.FileName))
	_, codeEnd, ok := uiDiffCodeRange(rows, point.Row)
	if !ok {
		return
	}
	start = clampUIDiffInt(start, 0, maxInt(0, codeEnd-1))
	end = clampUIDiffInt(end, start+1, codeEnd)
	s.selectionActive = true
	s.selectionLinewise = false
	s.selectionAnchor = selectionPoint{Row: point.Row, Col: start}
	s.setCursorPointWithoutReveal(rows, selectionPoint{Row: point.Row, Col: maxInt(start, end-1)})
}

func (s *uiDiffViewState) selectRowAt(rows []diff.Row, point selectionPoint) {
	if point.Row < 0 || point.Row >= len(rows) || !selectableDiffRow(rows[point.Row].Kind) {
		return
	}
	_, end, ok := uiDiffCodeRange(rows, point.Row)
	if !ok {
		return
	}
	s.selectionActive = true
	s.selectionLinewise = true
	s.selectionAnchor = selectionPoint{Row: point.Row, Col: 0}
	s.setCursorPointWithoutReveal(rows, selectionPoint{Row: point.Row, Col: maxInt(0, end-1)})
}

func (s *uiDiffViewState) selectionPointForMouse(rows []diff.Row, mouse vaxis.Mouse, metrics uiDiffDocumentMetrics) (selectionPoint, bool) {
	if !s.mouseInDiffViewport(mouse) {
		return selectionPoint{}, false
	}
	if s.sideBySide {
		return s.sideBySideSelectionPointForMouse(rows, mouse, metrics)
	}
	rowIndex, ok := s.rowForMouse(mouse)
	if !ok || rowIndex < 0 || rowIndex >= len(rows) || !selectableDiffRow(rows[rowIndex].Kind) {
		return selectionPoint{}, false
	}
	col := mouse.Col
	if uiDiffRowUsesGrid(rows[rowIndex]) {
		col -= uiDiffCodeOffsetForGutterWidths(metrics.OldGutterWidth, metrics.NewGutterWidth)
	}
	_, end, ok := uiDiffCodeRange(rows, rowIndex)
	if !ok {
		return selectionPoint{}, false
	}
	col = clampUIDiffInt(col, 0, maxInt(0, end-1))
	return selectionPoint{Row: rowIndex, Col: col}, true
}

func (s *uiDiffViewState) sideBySideSelectionPointForMouse(rows []diff.Row, mouse vaxis.Mouse, metrics uiDiffDocumentMetrics) (selectionPoint, bool) {
	visualRow, ok := s.rowForMouse(mouse)
	if !ok {
		return selectionPoint{}, false
	}
	sideRows := s.sideBySideRows(rows)
	if visualRow < 0 || visualRow >= len(sideRows) {
		return selectionPoint{}, false
	}
	viewportWidth := 0
	if s.scroll.Attached() {
		viewportWidth = s.scroll.Metrics().ViewportWidth
	}
	if viewportWidth <= 0 {
		viewportWidth = mouse.Col + 1
	}
	leftWidth, rightStart, rightWidth := uiDiffSideBySidePaneGeometry(viewportWidth)
	side := sideLeft
	localCol := mouse.Col
	if mouse.Col >= rightStart {
		side = sideRight
		localCol = mouse.Col - rightStart
	} else if mouse.Col >= leftWidth {
		return selectionPoint{}, false
	}
	if localCol < 0 {
		return selectionPoint{}, false
	}
	if side == sideLeft && localCol >= leftWidth {
		return selectionPoint{}, false
	}
	if side == sideRight && localCol >= rightWidth {
		return selectionPoint{}, false
	}
	rowIndex := sideBySideDocRowForSide(sideRows[visualRow], side)
	if rowIndex < 0 || rowIndex >= len(rows) || !selectableDiffRow(rows[rowIndex].Kind) {
		return selectionPoint{}, false
	}
	gutterWidth := metrics.SideBySideGutterWidth() + 3
	col := localCol - gutterWidth + s.xScroll
	_, end, ok := uiDiffCodeRange(rows, rowIndex)
	if !ok {
		return selectionPoint{}, false
	}
	col = clampUIDiffInt(col, 0, maxInt(0, end-1))
	return selectionPoint{Row: rowIndex, Col: col}, true
}

func uiDiffSideBySidePaneGeometry(width int) (leftWidth int, rightStart int, rightWidth int) {
	leftWidth = width / 2
	if width > 1 {
		leftWidth = (width - 1) / 2
	}
	rightStart = leftWidth
	if width > 1 {
		rightStart++
	}
	rightWidth = width - rightStart
	if rightWidth < 0 {
		rightWidth = 0
	}
	return leftWidth, rightStart, rightWidth
}

func (s *uiDiffViewState) rowForMouse(mouse vaxis.Mouse) (int, bool) {
	first, _, ok := s.list.VisibleRange()
	if !ok || mouse.Row < 0 {
		return 0, false
	}
	w := s.Widget().(uiDiffView)
	count := len(w.Rows)
	if s.sideBySide {
		count = len(s.sideBySideRows(w.Rows))
	}
	if count <= 0 {
		return 0, false
	}
	logicalY := mouse.Row
	if s.scroll.Attached() {
		metrics := s.scroll.Metrics()
		if mouse.Row >= metrics.ViewportHeight {
			return 0, false
		}
		logicalY = metrics.ScrollOffset + mouse.Row
	}
	last := count
	for row := first; row < last; row++ {
		offset, ok := s.list.OffsetForIndex(row)
		if !ok {
			continue
		}
		nextOffset := offset + 1
		if row+1 < count {
			if next, ok := s.list.OffsetForIndex(row + 1); ok {
				nextOffset = next
			}
		} else if s.scroll.Attached() {
			nextOffset = maxInt(offset+1, s.scroll.Metrics().ContentHeight)
		} else {
			nextOffset = 1 << 30
		}
		if logicalY >= offset && logicalY < nextOffset {
			return row, true
		}
	}
	return 0, false
}

func (s *uiDiffViewState) documentRowForMouse(mouse vaxis.Mouse) int {
	row, ok := s.rowForMouse(mouse)
	if !ok {
		return -1
	}
	return row
}

func uiDiffCodeOffset(rows []diff.Row) int {
	oldWidth, newWidth := uiDiffGutterWidths(rows)
	return uiDiffCodeOffsetForGutterWidths(oldWidth, newWidth)
}

func uiDiffCodeOffsetForGutterWidths(oldWidth int, newWidth int) int {
	if oldWidth == 0 && newWidth == 0 {
		return 0
	}
	return oldWidth + 1 + newWidth + 1 + 1 + 1
}

func (s *uiDiffViewState) clearPendingKeys() {
	s.pendingG = false
	s.pendingBracket = 0
	s.pendingSpace = false
	s.pendingD = false
}

func (s *uiDiffViewState) openCommentEditor(rows []diff.Row) {
	if s.selectionActive {
		s.openCommentEditorForSelection(rows)
		return
	}
	if s.cursor.Row < 0 || s.cursor.Row >= len(rows) || !reviewAnchorValid(rows[s.cursor.Row].Review) {
		return
	}
	s.storeCommentEditorBody()
	if s.commentEditorActive && s.commentEditorRow == s.cursor.Row {
		s.commentEditorFocused = true
		s.commentEditorInsert = true
		s.clearLineSelection()
		s.revealCommentEditor(rows)
		s.SetState(func() {})
		return
	}
	s.commentEditorActive = true
	s.commentEditorFocused = true
	s.commentEditorInsert = true
	s.commentEditorRow = s.cursor.Row
	s.commentEditorTarget = uiDiffCommentTarget{Draft: commentDraftForAnchor(rows[s.cursor.Row].Review), Row: s.cursor.Row}
	s.commentEditorBody = s.commentEditorBodies[s.cursor.Row]
	s.clearLineSelection()
	s.revealCommentEditor(rows)
	s.SetState(func() {})
}

func (s *uiDiffViewState) openCommentEditorForSelection(rows []diff.Row) {
	draft, ok := s.reviewDraftForSelection(rows)
	if !ok {
		return
	}
	s.storeCommentEditorBody()
	s.commentEditorActive = true
	s.commentEditorFocused = true
	s.commentEditorInsert = true
	s.commentEditorRow = s.reviewDraftTargetRow(rows, draft)
	s.commentEditorTarget = uiDiffCommentTarget{Draft: draft, Row: s.commentEditorRow}
	s.commentEditorBody = s.commentEditorBodies[s.commentEditorRow]
	s.clearLineSelection()
	s.revealCommentEditor(rows)
	s.SetState(func() {})
}

func (s *uiDiffViewState) reviewDraftForSelection(rows []diff.Row) (review.CommentDraft, bool) {
	start := s.selectionAnchor
	end := s.cursor
	if selectionPointLess(end, start) {
		start, end = end, start
	}
	startAnchor, ok := firstReviewAnchor(rows, start.Row, end.Row)
	if !ok {
		return review.CommentDraft{}, false
	}
	endAnchor, ok := lastReviewAnchor(rows, start.Row, end.Row)
	if !ok {
		return review.CommentDraft{}, false
	}
	if startAnchor.Path != endAnchor.Path || startAnchor.CommitID != endAnchor.CommitID || startAnchor.OriginalCommitID != endAnchor.OriginalCommitID {
		return review.CommentDraft{}, false
	}
	draft := review.CommentDraft{
		Path:             startAnchor.Path,
		Line:             endAnchor.Line,
		Side:             endAnchor.Side,
		CommitID:         endAnchor.CommitID,
		OriginalCommitID: endAnchor.OriginalCommitID,
	}
	if startAnchor.Line != endAnchor.Line || startAnchor.Side != endAnchor.Side {
		draft.StartLine = startAnchor.Line
		draft.StartSide = startAnchor.Side
	}
	if !s.selectionLinewise && start.Row == end.Row && startAnchor.Line == endAnchor.Line && startAnchor.Side == endAnchor.Side {
		startColumn, endColumn, ok := uiDiffReviewColumnsForSelection(rows, start.Row, start.Col, end.Col)
		if ok {
			draft.StartColumn = &startColumn
			draft.EndColumn = &endColumn
		}
	}
	return draft, true
}

func (s *uiDiffViewState) reviewDraftTargetRow(rows []diff.Row, draft review.CommentDraft) int {
	for rowIndex, row := range rows {
		if reviewDraftEndsAt(draft, row.Review) {
			return rowIndex
		}
	}
	return s.cursor.Row
}

func firstReviewAnchor(rows []diff.Row, start int, end int) (review.Anchor, bool) {
	if start < 0 {
		start = 0
	}
	if end >= len(rows) {
		end = len(rows) - 1
	}
	for row := start; row <= end; row++ {
		if reviewAnchorValid(rows[row].Review) {
			return rows[row].Review, true
		}
	}
	return review.Anchor{}, false
}

func lastReviewAnchor(rows []diff.Row, start int, end int) (review.Anchor, bool) {
	if start < 0 {
		start = 0
	}
	if end >= len(rows) {
		end = len(rows) - 1
	}
	for row := end; row >= start; row-- {
		if reviewAnchorValid(rows[row].Review) {
			return rows[row].Review, true
		}
	}
	return review.Anchor{}, false
}

func uiDiffReviewColumnsForSelection(rows []diff.Row, rowIndex int, startCol int, endCol int) (int, int, bool) {
	codeStart, codeEnd, ok := uiDiffCodeRange(rows, rowIndex)
	if !ok {
		return 0, 0, false
	}
	startCol = maxInt(startCol, codeStart)
	endCol = minInt(endCol, codeEnd)
	if startCol >= endCol {
		return 0, 0, false
	}
	return startCol - codeStart + 1, endCol - codeStart, true
}

func (s *uiDiffViewState) handleCommentEditorKey(rows []diff.Row, key vaxis.Key) vui.EventResult {
	if !s.commentEditorInsert {
		switch {
		case key.Matches(vaxis.KeyEsc):
			if s.commentEditorSelection {
				s.commentEditorSelection = false
				s.SetState(func() {})
				return vui.EventHandled
			}
			if strings.TrimSpace(s.commentEditorBody) == "" {
				s.closeCommentEditor()
				s.SetState(func() {})
				return vui.EventHandled
			}
			s.commentEditorFocused = false
			s.SetState(func() {})
			return vui.EventHandled
		case key.Matches('v'):
			s.clearPendingKeys()
			s.commentEditorSelection = true
			s.commentEditorLinewise = false
			s.commentEditorAnchor = s.commentEditorNormalCursor
			s.SetState(func() {})
			return vui.EventHandled
		case key.Matches('V'):
			s.clearPendingKeys()
			s.commentEditorSelection = true
			s.commentEditorLinewise = true
			s.commentEditorAnchor = s.commentEditorNormalCursor
			s.SetState(func() {})
			return vui.EventHandled
		case key.Matches('x'):
			s.clearPendingKeys()
			s.deleteCommentEditorSelectionOrRange(rows, false, s.commentEditorNormalCursor, minInt(s.commentEditorNormalCursor+1, len([]rune(s.commentEditorBody))), false)
			return vui.EventHandled
		case key.Matches('c'):
			s.clearPendingKeys()
			if s.commentEditorSelection {
				s.deleteCommentEditorSelectionOrRange(rows, true, 0, 0, false)
			}
			return vui.EventHandled
		case key.Matches('D'):
			s.clearPendingKeys()
			start := s.commentEditorNormalCursor
			end := commentEditorLineEnd(s.commentEditorBody, start)
			if end == start {
				end = minInt(start+1, len([]rune(s.commentEditorBody)))
			}
			s.deleteCommentEditorSelectionOrRange(rows, false, start, end, false)
			return vui.EventHandled
		case key.Matches('d') && s.pendingD:
			s.clearPendingKeys()
			start := commentEditorLineStart(s.commentEditorBody, s.commentEditorNormalCursor)
			end := commentEditorNextLineStart(s.commentEditorBody, s.commentEditorNormalCursor)
			s.deleteCommentEditorSelectionOrRange(rows, false, start, end, true)
			return vui.EventHandled
		case key.Matches('d') && s.commentEditorSelection:
			s.clearPendingKeys()
			s.deleteCommentEditorSelectionOrRange(rows, false, 0, 0, false)
			return vui.EventHandled
		case key.Matches('d'):
			s.pendingD = true
			s.SetState(func() {})
			return vui.EventHandled
		case key.Matches('i'):
			s.clearPendingKeys()
			s.commentEditorSelection = false
			cursor := s.commentEditorNormalCursor
			s.commentEditorCursor = &cursor
			s.commentEditorInsert = true
			s.SetState(func() {})
			return vui.EventHandled
		case key.Matches('I'):
			s.clearPendingKeys()
			s.commentEditorSelection = false
			cursor := commentEditorLineStart(s.commentEditorBody, s.commentEditorNormalCursor)
			s.commentEditorCursor = &cursor
			s.commentEditorNormalCursor = cursor
			s.commentEditorInsert = true
			s.SetState(func() {})
			return vui.EventHandled
		case key.Matches('a'):
			s.clearPendingKeys()
			s.commentEditorSelection = false
			cursor := minInt(s.commentEditorNormalCursor+1, len([]rune(s.commentEditorBody)))
			s.commentEditorCursor = &cursor
			s.commentEditorNormalCursor = cursor
			s.commentEditorInsert = true
			s.SetState(func() {})
			return vui.EventHandled
		case key.Matches('A'):
			s.clearPendingKeys()
			s.commentEditorSelection = false
			cursor := commentEditorLineEnd(s.commentEditorBody, s.commentEditorNormalCursor)
			s.commentEditorCursor = &cursor
			s.commentEditorNormalCursor = cursor
			s.commentEditorInsert = true
			s.SetState(func() {})
			return vui.EventHandled
		case key.Matches('o'):
			s.clearPendingKeys()
			cursor := commentEditorLineEnd(s.commentEditorBody, s.commentEditorNormalCursor)
			s.commentEditorBody = commentEditorInsertRunes(s.commentEditorBody, cursor, "\n")
			cursor++
			s.commentEditorCursor = &cursor
			s.commentEditorNormalCursor = cursor
			s.commentEditorInsert = true
			s.storeCommentEditorBody()
			s.revealCommentEditor(rows)
			s.SetState(func() {})
			return vui.EventHandled
		case key.Matches('O'):
			s.clearPendingKeys()
			cursor := commentEditorLineStart(s.commentEditorBody, s.commentEditorNormalCursor)
			s.commentEditorBody = commentEditorInsertRunes(s.commentEditorBody, cursor, "\n")
			s.commentEditorCursor = &cursor
			s.commentEditorNormalCursor = cursor
			s.commentEditorInsert = true
			s.storeCommentEditorBody()
			s.revealCommentEditor(rows)
			s.SetState(func() {})
			return vui.EventHandled
		case key.Matches('0'):
			s.clearPendingKeys()
			s.commentEditorNormalCursor = commentEditorLineStart(s.commentEditorBody, s.commentEditorNormalCursor)
			s.SetState(func() {})
			return vui.EventHandled
		case key.Matches('$'):
			s.clearPendingKeys()
			s.commentEditorNormalCursor = maxInt(commentEditorLineStart(s.commentEditorBody, s.commentEditorNormalCursor), commentEditorLineEnd(s.commentEditorBody, s.commentEditorNormalCursor)-1)
			s.SetState(func() {})
			return vui.EventHandled
		case key.Matches('h'), key.Matches(vaxis.KeyLeft), key.MatchString("Left"):
			s.clearPendingKeys()
			s.commentEditorNormalCursor = maxInt(0, s.commentEditorNormalCursor-1)
			s.SetState(func() {})
			return vui.EventHandled
		case key.Matches('l'), key.Matches(vaxis.KeyRight), key.MatchString("Right"):
			s.clearPendingKeys()
			s.commentEditorNormalCursor = minInt(s.commentEditorNormalCursor+1, maxInt(0, len([]rune(s.commentEditorBody))-1))
			s.SetState(func() {})
			return vui.EventHandled
		case key.Matches('j'), key.Matches(vaxis.KeyDown), key.MatchString("Down"):
			s.clearPendingKeys()
			cursor := commentEditorMoveLine(s.commentEditorBody, s.commentEditorNormalCursor, 1)
			if cursor == s.commentEditorNormalCursor {
				s.commentEditorFocused = false
				s.setCursorRow(rows, s.commentEditorRow+1)
				return vui.EventHandled
			}
			s.commentEditorNormalCursor = cursor
			s.SetState(func() {})
			return vui.EventHandled
		case key.Matches('k'), key.Matches(vaxis.KeyUp), key.MatchString("Up"):
			s.clearPendingKeys()
			cursor := commentEditorMoveLine(s.commentEditorBody, s.commentEditorNormalCursor, -1)
			if cursor == s.commentEditorNormalCursor {
				s.commentEditorFocused = false
				s.setCursorRow(rows, s.commentEditorRow)
				return vui.EventHandled
			}
			s.commentEditorNormalCursor = cursor
			s.SetState(func() {})
			return vui.EventHandled
		default:
			return vui.EventIgnored
		}
	}
	switch {
	case key.Matches(vaxis.KeyEsc):
		if strings.TrimSpace(s.commentEditorBody) == "" {
			s.closeCommentEditor()
			s.SetState(func() {})
			return vui.EventHandled
		}
		s.commentEditorFocused = true
		s.commentEditorInsert = false
		s.commentEditorNormalCursor = maxInt(0, len([]rune(s.commentEditorBody))-1)
		s.SetState(func() {})
		return vui.EventHandled
	case key.MatchString("Ctrl+s"):
		s.submitCommentEditor(rows)
		s.SetState(func() {})
		return vui.EventHandled
	default:
		return vui.EventIgnored
	}
}

func (s *uiDiffViewState) deleteCommentEditorSelectionOrRange(rows []diff.Row, insert bool, start int, end int, linewise bool) {
	if s.commentEditorSelection {
		start, end = commentEditorSelectionRange(s.commentEditorBody, s.commentEditorAnchor, s.commentEditorNormalCursor, s.commentEditorLinewise)
		linewise = s.commentEditorLinewise
	}
	s.commentEditorBody, s.commentEditorNormalCursor = commentEditorDeleteRange(s.commentEditorBody, start, end)
	if linewise && s.commentEditorBody != "" {
		s.commentEditorNormalCursor = commentEditorLineStart(s.commentEditorBody, s.commentEditorNormalCursor)
	}
	s.commentEditorSelection = false
	s.commentEditorLinewise = false
	s.commentEditorCursor = nil
	if insert {
		cursor := s.commentEditorNormalCursor
		s.commentEditorCursor = &cursor
		s.commentEditorInsert = true
	}
	s.storeCommentEditorBody()
	s.revealCommentEditor(rows)
	s.SetState(func() {})
}

func (s *uiDiffViewState) submitCommentEditor(rows []diff.Row) {
	if !s.commentEditorActive || s.commentEditorRow < 0 || s.commentEditorRow >= len(rows) {
		return
	}
	body := strings.TrimSpace(s.commentEditorBody)
	if body != "" {
		draft := s.commentEditorTarget.Draft
		if draft.Path != "" {
			s.removeReviewDraft(draft)
		}
		if draft.Path == "" {
			draft = commentDraftForAnchor(rows[s.commentEditorRow].Review)
		}
		draft.Body = s.commentEditorBody
		s.reviewDrafts = append(s.reviewDrafts, draft)
		s.reviewDirty = true
	}
	delete(s.commentEditorBodies, s.commentEditorRow)
	s.commentEditorActive = false
	s.closeCommentEditor()
}

func (s *uiDiffViewState) removeReviewDraft(draft review.CommentDraft) {
	for index, local := range s.reviewDrafts {
		if local == draft {
			s.reviewDrafts = append(s.reviewDrafts[:index], s.reviewDrafts[index+1:]...)
			return
		}
	}
	if s.deletedReviewDrafts == nil {
		s.deletedReviewDrafts = make(map[review.CommentDraft]bool)
	}
	s.deletedReviewDrafts[draft] = true
}

func (s *uiDiffViewState) submitActiveCommentEditor(rows []diff.Row) {
	if !s.commentEditorActive {
		return
	}
	if strings.TrimSpace(s.commentEditorBody) == "" {
		s.closeCommentEditor()
		return
	}
	s.submitCommentEditor(rows)
}

func (s *uiDiffViewState) enterCommandMode() {
	s.commandMode = true
	s.commandLine = ""
	s.SetState(func() {})
}

func (s *uiDiffViewState) handleCommandKey(ctx vui.EventContext, rows []diff.Row, key vaxis.Key) vui.EventResult {
	switch {
	case key.Matches(vaxis.KeyEsc):
		s.commandMode = false
		s.commandLine = ""
		s.SetState(func() {})
		return vui.EventHandled
	case key.Matches(vaxis.KeyEnter):
		s.executeCommand(ctx, rows, strings.TrimSpace(s.commandLine))
		s.SetState(func() {})
		return vui.EventHandled
	case key.Matches(vaxis.KeyBackspace), key.Matches('h', vaxis.ModCtrl):
		if s.commandLine != "" {
			runes := []rune(s.commandLine)
			s.commandLine = string(runes[:len(runes)-1])
			s.SetState(func() {})
		}
		return vui.EventHandled
	case key.Text != "" && key.Modifiers&(vaxis.ModCtrl|vaxis.ModAlt|vaxis.ModSuper) == 0:
		for _, r := range key.Text {
			if r >= ' ' {
				s.commandLine += string(r)
			}
		}
		s.SetState(func() {})
		return vui.EventHandled
	default:
		return vui.EventIgnored
	}
}

func (s *uiDiffViewState) executeCommand(ctx vui.EventContext, rows []diff.Row, command string) {
	s.commandMode = false
	s.commandLine = ""
	if command == "" {
		return
	}
	for len(command) > 0 {
		switch {
		case strings.HasPrefix(command, "q!"):
			ctx.Quit()
			return
		case strings.HasPrefix(command, "q"):
			if s.hasUnsavedReviewChanges(rows) {
				s.setStatusMessage("Unsaved comments. Use :w to save or :q! to quit.")
				return
			}
			ctx.Quit()
			return
		case strings.HasPrefix(command, "w"):
			if !s.writeReviewCommand(rows) {
				return
			}
			command = command[1:]
		default:
			return
		}
	}
}

func (s *uiDiffViewState) hasUnsavedReviewChanges(rows []diff.Row) bool {
	if s.reviewDirty {
		return true
	}
	if s.commentEditorActive && strings.TrimSpace(s.commentEditorBody) != "" {
		if s.commentEditorBody != s.commentEditorTarget.Draft.Body {
			return true
		}
	}
	for row, body := range s.commentEditorBodies {
		if strings.TrimSpace(body) == "" {
			continue
		}
		if row < 0 || row >= len(rows) {
			return true
		}
		w := s.Widget().(uiDiffView)
		drafts := reviewDraftsForRow(rows[row], s.allReviewDrafts(w.ReviewDrafts))
		if len(drafts) == 0 || body != drafts[0].Body {
			return true
		}
	}
	return false
}

func (s *uiDiffViewState) writeReviewCommand(rows []diff.Row) bool {
	s.submitActiveCommentEditor(rows)
	w := s.Widget().(uiDiffView)
	drafts := s.allReviewDrafts(w.ReviewDrafts)
	if len(drafts) == 0 {
		if s.reviewDirty {
			if w.ReviewFile != "" {
				if err := review.SaveFile(w.ReviewFile, review.CommentFile{Version: 1}); err != nil {
					s.setStatusMessage(fmt.Sprintf("Could not save comments: %v", err))
					return false
				}
			}
			s.reviewDirty = false
			s.setStatusMessage("Comments saved.")
			return true
		}
		s.reviewDirty = false
		s.setStatusMessage("No comments to save.")
		return true
	}
	if w.ReviewFile != "" {
		if err := review.SaveFile(w.ReviewFile, review.CommentFile{Version: 1, Comments: drafts}); err != nil {
			s.setStatusMessage(fmt.Sprintf("Could not save comments: %v", err))
			return false
		}
	}
	s.reviewDirty = false
	s.setStatusMessage("Comments saved.")
	return true
}

func (s *uiDiffViewState) setStatusMessage(message string) {
	s.statusMessage = message
	s.statusMessageUntil = time.Now().Add(statusMessageTimeout)
}

func (s *uiDiffViewState) clearExpiredStatusMessage(now time.Time) bool {
	if s.statusMessage == "" || s.statusMessageUntil.IsZero() || now.Before(s.statusMessageUntil) {
		return false
	}
	s.statusMessage = ""
	s.statusMessageUntil = time.Time{}
	return true
}

func (s *uiDiffViewState) openEditor() {
	target, ok := s.editorTarget()
	if !ok || target.Path == "" {
		s.SetState(func() {})
		return
	}
	name, args, err := editorCommand(configuredEditor(), target)
	if err != nil {
		s.setStatusMessage(fmt.Sprintf("Could not open editor: %v", err))
		s.SetState(func() {})
		return
	}
	s.editorCommand = exec.Command(name, args...)
	s.SetState(func() {})
}

func (s *uiDiffViewState) editorTarget() (EditorTarget, bool) {
	w := s.Widget().(uiDiffView)
	target, ok := uiDiffEditorTarget(w.Rows, s.cursor)
	if !ok {
		s.setStatusMessage("No file.")
		return EditorTarget{}, false
	}
	target.Path = editorPathInRoot(target.Path, w.WorkTreeRoot)
	return target, true
}

func uiDiffEditorTarget(rows []diff.Row, cursor selectionPoint) (EditorTarget, bool) {
	if cursor.Row < 0 || cursor.Row >= len(rows) {
		return EditorTarget{}, false
	}
	row := rows[cursor.Row]
	if row.FileName == "" {
		return EditorTarget{}, false
	}
	line := row.Review.Line
	if line <= 0 {
		line = 1
	}
	column := 1
	if row.Code != "" {
		column = editorColumnAtCell(row.Code, cursor.Col, tabWidthForFile(row.FileName))
	}
	return EditorTarget{Path: row.FileName, Line: line, Column: column}, true
}

func editorPathInRoot(path string, root string) string {
	if path == "" || root == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func (s *uiDiffViewState) deleteReviewDraftCommand(rows []diff.Row, base []review.CommentDraft) {
	if !s.deleteReviewDraftAtTarget(rows, base) {
		s.setStatusMessage("No note.")
		s.SetState(func() {})
		return
	}
	s.SetState(func() {})
}

func (s *uiDiffViewState) deleteReviewDraftAtTarget(rows []diff.Row, base []review.CommentDraft) bool {
	draft, ok := s.findReviewDraftAtTarget(rows, base)
	if !ok {
		return false
	}
	for index, local := range s.reviewDrafts {
		if local == draft {
			s.reviewDrafts = append(s.reviewDrafts[:index], s.reviewDrafts[index+1:]...)
			s.reviewDirty = true
			s.clearLineSelection()
			s.closeCommentEditorForDraft(rows, draft)
			s.setStatusMessage("Note deleted.")
			return true
		}
	}
	if s.deletedReviewDrafts == nil {
		s.deletedReviewDrafts = make(map[review.CommentDraft]bool)
	}
	s.deletedReviewDrafts[draft] = true
	s.reviewDirty = true
	s.clearLineSelection()
	s.closeCommentEditorForDraft(rows, draft)
	s.setStatusMessage("Note deleted.")
	return true
}

func (s *uiDiffViewState) closeCommentEditorForDraft(rows []diff.Row, draft review.CommentDraft) {
	if !s.commentEditorActive || s.commentEditorRow < 0 || s.commentEditorRow >= len(rows) {
		return
	}
	if reviewDraftContains(draft, rows[s.commentEditorRow].Review) {
		s.closeCommentEditor()
	}
}

func (s *uiDiffViewState) findReviewDraftAtTarget(rows []diff.Row, base []review.CommentDraft) (review.CommentDraft, bool) {
	drafts := s.allReviewDrafts(base)
	if s.selectionActive {
		start, end := orderedUIDiffInts(s.selectionAnchor.Row, s.cursor.Row)
		if start < 0 {
			start = 0
		}
		if end >= len(rows) {
			end = len(rows) - 1
		}
		for row := start; row <= end; row++ {
			if !selectableDiffRow(rows[row].Kind) {
				continue
			}
			if draft, ok := findReviewDraftContainingAnchor(drafts, rows[row].Review); ok {
				return draft, true
			}
		}
		return review.CommentDraft{}, false
	}
	if s.cursor.Row < 0 || s.cursor.Row >= len(rows) {
		return review.CommentDraft{}, false
	}
	return findReviewDraftContainingAnchor(drafts, rows[s.cursor.Row].Review)
}

func findReviewDraftContainingAnchor(drafts []review.CommentDraft, anchor review.Anchor) (review.CommentDraft, bool) {
	if !reviewAnchorValid(anchor) {
		return review.CommentDraft{}, false
	}
	for _, draft := range drafts {
		if reviewDraftContains(draft, anchor) {
			return draft, true
		}
	}
	return review.CommentDraft{}, false
}

func commentDraftForAnchor(anchor review.Anchor) review.CommentDraft {
	return review.CommentDraft{
		Path:             anchor.Path,
		Line:             anchor.Line,
		Side:             anchor.Side,
		CommitID:         anchor.CommitID,
		OriginalCommitID: anchor.OriginalCommitID,
	}
}

func (s *uiDiffViewState) closeCommentEditor() {
	delete(s.commentEditorBodies, s.commentEditorRow)
	s.commentEditorActive = false
	s.commentEditorFocused = false
	s.commentEditorInsert = false
	s.commentEditorTarget = uiDiffCommentTarget{}
	s.commentEditorBody = ""
	s.commentEditorCursor = nil
	s.commentEditorNormalCursor = 0
	s.commentEditorSelection = false
	s.commentEditorLinewise = false
	s.commentEditorAnchor = 0
}

func (s *uiDiffViewState) storeCommentEditorBody() {
	if !s.commentEditorActive || s.commentEditorRow < 0 {
		return
	}
	if strings.TrimSpace(s.commentEditorBody) == "" {
		delete(s.commentEditorBodies, s.commentEditorRow)
		return
	}
	if s.commentEditorBodies == nil {
		s.commentEditorBodies = make(map[int]string)
	}
	s.commentEditorBodies[s.commentEditorRow] = s.commentEditorBody
}

func (s *uiDiffViewState) moveIntoCommentEditor(rows []diff.Row, delta int) bool {
	if s.commentEditorInsert || s.commentEditorFocused {
		return false
	}
	if delta > 0 && s.commentEditorBodyForRow(rows, s.cursor.Row) != "" {
		s.focusCommentEditorRow(rows, s.cursor.Row)
		return true
	}
	if delta < 0 {
		start := s.cursor.Row - 1
		for row := start; row >= 0; row-- {
			if !uiDiffCursorableRow(rows[row]) {
				continue
			}
			if s.commentEditorBodyForRow(rows, row) == "" {
				return false
			}
			s.cursor.Row = row
			s.cursor.Col = s.clampCursorCol(rows, s.cursor.Row, s.cursorCol)
			s.cursorCol = s.cursor.Col
			s.focusCommentEditorRow(rows, row)
			s.commentEditorNormalCursor = commentEditorLastLineCursor(s.commentEditorBody)
			return true
		}
	}
	return false
}

func (s *uiDiffViewState) commentEditorBodyForRow(rows []diff.Row, row int) string {
	if s.commentEditorActive && s.commentEditorRow == row && strings.TrimSpace(s.commentEditorBody) != "" {
		return s.commentEditorBody
	}
	if body := s.commentEditorBodies[row]; strings.TrimSpace(body) != "" {
		return body
	}
	if row < 0 || row >= len(rows) {
		return ""
	}
	w := s.Widget().(uiDiffView)
	drafts := reviewDraftsForRow(rows[row], s.allReviewDrafts(w.ReviewDrafts))
	if len(drafts) == 0 {
		return ""
	}
	return drafts[0].Body
}

func (s *uiDiffViewState) focusCommentEditorRow(rows []diff.Row, row int) {
	s.storeCommentEditorBody()
	body := s.commentEditorBodyForRow(rows, row)
	s.commentEditorActive = true
	s.commentEditorFocused = true
	s.commentEditorInsert = false
	s.commentEditorRow = row
	s.commentEditorCursor = nil
	s.commentEditorTarget = uiDiffCommentTarget{Row: row}
	if row >= 0 && row < len(rows) {
		w := s.Widget().(uiDiffView)
		if drafts := reviewDraftsForRow(rows[row], s.allReviewDrafts(w.ReviewDrafts)); len(drafts) > 0 {
			s.commentEditorTarget.Draft = drafts[0]
		}
	}
	s.commentEditorBody = body
	s.commentEditorNormalCursor = 0
	s.revealCursorRow()
	s.SetState(func() {})
}

func (s *uiDiffViewState) startTextObject(kind textObjectKind) {
	s.textObject = textObjectState{active: true, kind: kind, at: time.Now()}
	s.SetState(func() {})
}

func (s *uiDiffViewState) clearExpiredTextObject(now time.Time) {
	if s.textObject.active && now.Sub(s.textObject.at) > pendingKeyTimeout {
		s.textObject = textObjectState{}
	}
}

func (s *uiDiffViewState) handleTextObjectKey(rows []diff.Row, key vaxis.Key) vui.EventResult {
	state := s.textObject
	s.textObject = textObjectState{}
	if key.Matches(vaxis.KeyEsc) {
		s.SetState(func() {})
		return vui.EventHandled
	}
	object := textObjectKeyRune(key)
	if object == 'w' {
		if s.selectWordTextObject(rows, state.kind) {
			s.SetState(func() {})
			return vui.EventHandled
		}
		s.SetState(func() {})
		return vui.EventHandled
	}
	open, close, ok := textObjectDelimiters(object)
	if !ok || !s.selectDelimitedTextObject(rows, state.kind, open, close) {
		s.SetState(func() {})
		return vui.EventHandled
	}
	s.SetState(func() {})
	return vui.EventHandled
}

func (s *uiDiffViewState) selectWordTextObject(rows []diff.Row, kind textObjectKind) bool {
	if s.cursor.Row < 0 || s.cursor.Row >= len(rows) || !selectableDiffRow(rows[s.cursor.Row].Kind) {
		return false
	}
	row := rows[s.cursor.Row]
	start, end := tokenRangeAtWithTabWidth(row.Code, s.cursor.Col, tabWidthForFile(row.FileName))
	_, codeEnd, ok := uiDiffCodeRange(rows, s.cursor.Row)
	if !ok {
		return false
	}
	start = clampUIDiffInt(start, 0, maxInt(0, codeEnd-1))
	end = clampUIDiffInt(end, start+1, codeEnd)
	if kind == textObjectAround {
		end = uiDiffExtendAroundWord(row, start, end, codeEnd)
		if end == start {
			end = minInt(codeEnd, start+1)
		}
	}
	s.selectionActive = true
	s.selectionLinewise = false
	s.selectionInitialNewline = false
	s.selectionFinalNewline = false
	s.selectionSideFiltered = false
	s.selectionAnchor = selectionPoint{Row: s.cursor.Row, Col: start}
	s.setCursorPointWithoutReveal(rows, selectionPoint{Row: s.cursor.Row, Col: maxInt(start, end-1)})
	return true
}

func uiDiffExtendAroundWord(row diff.Row, start int, end int, codeEnd int) int {
	for end < codeEnd && isSpaceRune(runeAtCellWithTabWidth(row.Code, end, tabWidthForFile(row.FileName))) {
		end++
	}
	if end == codeEnd {
		for start > 0 && isSpaceRune(runeAtCellWithTabWidth(row.Code, start-1, tabWidthForFile(row.FileName))) {
			start--
		}
	}
	return end
}

func (s *uiDiffViewState) selectDelimitedTextObject(rows []diff.Row, kind textObjectKind, open rune, close rune) bool {
	bounds, ok := s.textObjectSearchBounds(rows)
	if !ok {
		return false
	}
	cursor := textObjectPosition{Row: s.cursor.Row, Col: s.cursor.Col - bounds.CodeStart[s.cursor.Row]}
	if cursor.Col < 0 {
		cursor.Col = 0
	}
	if width := bounds.CodeWidth[s.cursor.Row]; cursor.Col >= width {
		cursor.Col = maxInt(0, width-1)
	}
	openPos, closePos, ok := findDelimitedTextObject(bounds, cursor, open, close)
	if !ok {
		return false
	}
	start := openPos
	end := closePos
	includeInitialNewline := false
	includeFinalNewline := false
	if kind == textObjectInner {
		start = advanceTextObjectPosition(bounds, openPos)
		end = previousTextObjectPosition(bounds, closePos)
	}
	if textObjectPositionLess(end, start) {
		return false
	}
	anchor := selectionPoint{Row: start.Row, Col: bounds.CodeStart[start.Row] + start.Col}
	if includeInitialNewline {
		anchor = selectionPoint{Row: openPos.Row, Col: bounds.CodeStart[openPos.Row] + bounds.CodeWidth[openPos.Row]}
	}
	s.selectionActive = true
	s.selectionLinewise = false
	s.selectionInitialNewline = includeInitialNewline
	s.selectionFinalNewline = includeFinalNewline
	s.selectionSideFiltered = false
	if start.Row != end.Row && s.cursor.Row >= 0 && s.cursor.Row < len(rows) {
		s.selectionSideFiltered = true
		s.selectionSide = sideForRow(rows[s.cursor.Row])
	}
	s.selectionAnchor = anchor
	s.setCursorPointWithoutReveal(rows, selectionPoint{Row: end.Row, Col: bounds.CodeStart[end.Row] + end.Col})
	return true
}

func (s *uiDiffViewState) textObjectSearchBounds(rows []diff.Row) (textObjectBounds, bool) {
	if s.cursor.Row < 0 || s.cursor.Row >= len(rows) || !selectableDiffRow(rows[s.cursor.Row].Kind) {
		return textObjectBounds{}, false
	}
	side := sideForRow(rows[s.cursor.Row])
	start := s.cursor.Row
	for start > 0 && textObjectRowsContiguous(rows[start-1]) {
		start--
	}
	end := s.cursor.Row
	for end+1 < len(rows) && textObjectRowsContiguous(rows[end+1]) {
		end++
	}
	bounds := textObjectBounds{
		Start:     start,
		End:       end,
		Side:      side,
		Code:      make(map[int]string, end-start+1),
		CodeStart: make(map[int]int, end-start+1),
		CodeWidth: make(map[int]int, end-start+1),
		TabWidth:  make(map[int]int, end-start+1),
	}
	for rowIndex := start; rowIndex <= end; rowIndex++ {
		if !rowOnTextObjectSide(rows[rowIndex], side) {
			continue
		}
		bounds.Code[rowIndex] = rows[rowIndex].Code
		bounds.CodeStart[rowIndex] = 0
		bounds.CodeWidth[rowIndex] = codeCellWidth(rows[rowIndex])
		bounds.TabWidth[rowIndex] = tabWidthForFile(rows[rowIndex].FileName)
	}
	if _, ok := bounds.CodeStart[s.cursor.Row]; !ok {
		return textObjectBounds{}, false
	}
	return bounds, true
}

func (s *uiDiffViewState) toggleSelection(linewise bool) {
	if s.selectionActive {
		s.clearLineSelection()
	} else {
		w := s.Widget().(uiDiffView)
		if s.cursor.Row < 0 || s.cursor.Row >= len(w.Rows) || !selectableDiffRow(w.Rows[s.cursor.Row].Kind) {
			return
		}
		s.selectionActive = true
		s.selectionLinewise = linewise
		s.selectionAnchor = s.cursor
		s.yankActive = false
	}
	s.SetState(func() {})
}

func (s *uiDiffViewState) clearLineSelection() {
	s.selectionActive = false
	s.selectionLinewise = false
	s.selectionInitialNewline = false
	s.selectionFinalNewline = false
	s.selectionSideFiltered = false
	s.selectionAnchor = selectionPoint{}
}

func (s *uiDiffViewState) yankSelection(ctx vui.EventContext, rows []diff.Row) {
	if !s.selectionActive {
		return
	}
	text := s.selectionText(rows)
	if text == "" {
		return
	}
	ctx.Copy(text)
	s.yankActive = true
	s.yankLinewise = s.selectionLinewise
	s.yankAnchor = s.selectionAnchor
	s.yankCursor = s.cursor
	s.yankUntil = time.Now().Add(yankHighlightDuration)
	s.clearLineSelection()
	s.SetState(func() {})
	runtime := ctx.Runtime()
	time.AfterFunc(yankHighlightDuration, func() {
		runtime.Dispatch(func() {
			s.SetState(func() { s.clearExpiredYank(time.Now()) })
		})
	})
}

func (s *uiDiffViewState) clearExpiredYank(now time.Time) {
	if s.yankActive && !s.yankUntil.IsZero() && !now.Before(s.yankUntil) {
		s.yankActive = false
		s.yankLinewise = false
		s.yankAnchor = selectionPoint{}
		s.yankCursor = selectionPoint{}
		s.yankUntil = time.Time{}
	}
}

func (s *uiDiffViewState) selectionText(rows []diff.Row) string {
	if !s.selectionActive {
		return ""
	}
	start := s.selectionAnchor
	end := s.cursor
	if selectionPointLess(end, start) {
		start, end = end, start
	}
	var text strings.Builder
	wroteRow := false
	for rowIndex := start.Row; rowIndex <= end.Row && rowIndex < len(rows); rowIndex++ {
		if rowIndex < 0 || !s.selectionIncludesRow(rows[rowIndex]) {
			continue
		}
		row := rows[rowIndex]
		rowStart, rowEnd := 0, codeCellWidth(row)
		if s.selectionInitialNewline && rowIndex == start.Row {
			wroteRow = true
			continue
		}
		rowCode := row.Code
		if !uiDiffRowUsesGrid(row) {
			rowCode = uiDiffRowCode(row)
			rowEnd = textCellWidth(rowCode)
		}
		if !s.selectionLinewise {
			if rowIndex == start.Row {
				rowStart = maxInt(rowStart, start.Col)
			}
			if rowIndex == end.Row {
				rowEnd = minInt(rowEnd, end.Col+1)
			}
		}
		rowText := cellTextRangeWithTabWidth(rowCode, rowStart, rowEnd, tabWidthForFile(row.FileName))
		if rowText == "" {
			continue
		}
		if wroteRow {
			text.WriteByte('\n')
		}
		text.WriteString(rowText)
		wroteRow = true
	}
	if s.selectionFinalNewline && wroteRow {
		text.WriteByte('\n')
	}
	return text.String()
}

func (s *uiDiffViewState) selectionIncludesRow(row diff.Row) bool {
	if !selectableDiffRow(row.Kind) {
		return false
	}
	return !s.selectionSideFiltered || rowOnTextObjectSide(row, s.selectionSide)
}

func (s *uiDiffViewState) lineSelected(row int) bool {
	if !s.selectionActive || !s.selectionLinewise {
		return false
	}
	return s.lineInRange(row, s.selectionAnchor, s.cursor)
}

func (s *uiDiffViewState) lineYanked(row int) bool {
	if !s.yankActive || !s.yankLinewise {
		return false
	}
	return s.lineInRange(row, s.yankAnchor, s.yankCursor)
}

func (s *uiDiffViewState) lineInRange(row int, anchor selectionPoint, cursor selectionPoint) bool {
	w := s.Widget().(uiDiffView)
	if row < 0 || row >= len(w.Rows) || !selectableDiffRow(w.Rows[row].Kind) {
		return false
	}
	start, end := orderedUIDiffInts(anchor.Row, cursor.Row)
	return row >= start && row <= end
}

func (s *uiDiffViewState) charSelectionRange(rowIndex int, row diff.Row) (int, int, bool) {
	if !s.selectionActive || s.selectionLinewise {
		return 0, 0, false
	}
	if !s.selectionIncludesRow(row) {
		return 0, 0, false
	}
	return uiDiffSelectionRange(rowIndex, row, s.selectionAnchor, s.cursor)
}

func uiDiffSelectionRange(rowIndex int, row diff.Row, anchor selectionPoint, cursor selectionPoint) (int, int, bool) {
	if !selectableDiffRow(row.Kind) {
		return 0, 0, false
	}
	start := anchor
	end := cursor
	if selectionPointLess(end, start) {
		start, end = end, start
	}
	if rowIndex < start.Row || rowIndex > end.Row {
		return 0, 0, false
	}
	_, codeEnd, ok := uiDiffCodeRangeForRow(row)
	if !ok {
		return 0, 0, false
	}
	from, to := 0, maxInt(0, codeEnd-1)
	if rowIndex == start.Row {
		from = start.Col
	}
	if rowIndex == end.Row {
		to = end.Col
	}
	from = clampUIDiffInt(from, 0, maxInt(0, codeEnd-1))
	to = clampUIDiffInt(to, 0, maxInt(0, codeEnd-1))
	if to < from {
		return 0, 0, false
	}
	return from, to + 1, true
}

func (s *uiDiffViewState) charYankRange(rowIndex int, row diff.Row) (int, int, bool) {
	if !s.yankActive || s.yankLinewise {
		return 0, 0, false
	}
	return uiDiffSelectionRange(rowIndex, row, s.yankAnchor, s.yankCursor)
}

func (s *uiDiffViewState) enterSearchMode() {
	s.searchMode = true
	s.searchQuery = ""
	s.searchMatches = nil
	s.searchIndex = -1
	s.searchStart = s.cursor
	s.SetState(func() {})
}

func (s *uiDiffViewState) handleSearchKey(rows []diff.Row, key vaxis.Key) vui.EventResult {
	switch {
	case key.Matches(vaxis.KeyEsc):
		s.searchMode = false
		s.clearSearch()
		s.SetState(func() {})
		return vui.EventHandled
	case key.Matches(vaxis.KeyEnter):
		s.searchMode = false
		s.updateSearchMatches(rows)
		if len(s.searchMatches) == 0 {
			s.SetState(func() {})
			return vui.EventHandled
		}
		s.searchIndex = s.nextSearchIndexFromPoint(s.searchStart, 1)
		s.applySearchMatch(rows)
		s.SetState(func() {})
		return vui.EventHandled
	case key.Matches(vaxis.KeyBackspace), key.Matches('h', vaxis.ModCtrl):
		if s.searchQuery != "" {
			runes := []rune(s.searchQuery)
			s.searchQuery = string(runes[:len(runes)-1])
			s.updateSearchMatches(rows)
			s.SetState(func() {})
		}
		return vui.EventHandled
	case key.Text != "" && key.Modifiers&(vaxis.ModCtrl|vaxis.ModAlt|vaxis.ModSuper) == 0:
		for _, r := range key.Text {
			if r >= ' ' {
				s.searchQuery += string(r)
			}
		}
		s.updateSearchMatches(rows)
		s.SetState(func() {})
		return vui.EventHandled
	default:
		return vui.EventIgnored
	}
}

func (s *uiDiffViewState) clearSearch() {
	s.searchQuery = ""
	s.searchMatches = nil
	s.searchIndex = -1
}

func (s *uiDiffViewState) buildItem(rows []diff.Row, rowIndex int, theme vui.Theme, highlightedRows map[int][]vaxis.Segment, drafts []review.CommentDraft, wrap bool, metrics uiDiffDocumentMetrics) vui.Widget {
	row := rows[rowIndex]
	active := rowIndex == s.cursor.Row && !s.commentEditorInsert && !s.commentEditorFocused
	selected := s.lineSelected(rowIndex)
	yanked := s.lineYanked(rowIndex)
	var item vui.Widget
	if !uiDiffRowUsesGrid(row) {
		item = s.buildFullWidthRow(row, rowIndex, uiDiffRowBackground(active, selected, yanked, theme), active, s.cursor.Col, selected, yanked, theme, wrap)
	} else {
		item = s.buildRow(row, rowIndex, active, selected, yanked, s.cursor.Col, theme, highlightedRows, s.searchMatches, wrap, metrics)
	}
	children := []vui.Widget{item}
	showDrafts := true
	commentIndent := uiDiffCodeOffsetForGutterWidths(metrics.OldGutterWidth, metrics.NewGutterWidth)
	commentBodyWidth := s.commentEditorBodyWidth(commentIndent)
	if s.commentEditorActive && s.commentEditorRow == rowIndex {
		children = append(children, uiDiffIndentedComment(commentIndent, s.buildCommentEditor(rows, theme, commentBodyWidth), theme))
		showDrafts = false
	} else if body := s.commentEditorBodies[rowIndex]; strings.TrimSpace(body) != "" {
		children = append(children, uiDiffIndentedComment(commentIndent, uiDiffCommentEditorBox(body, nil, false, false, nil, 0, false, false, 0, commentBodyWidth, theme), theme))
		showDrafts = false
	}
	if showDrafts {
		for _, draft := range reviewDraftsForRow(row, drafts) {
			children = append(children, uiDiffIndentedComment(commentIndent, uiDiffReviewDraft(draft, theme, func(vui.EventContext) {
				s.focusCommentEditorRow(rows, rowIndex)
				s.commentEditorInsert = true
			}), theme))
		}
	}
	if len(children) == 1 {
		return item
	}
	return vui.Column(children...)
}

func (s *uiDiffViewState) buildSideBySideItem(rows []diff.Row, sideRow sideBySideRow, theme vui.Theme, highlightedRows map[int][]vaxis.Segment, drafts []review.CommentDraft, wrap bool, metrics uiDiffDocumentMetrics) vui.Widget {
	if sideRow.Full >= 0 {
		return s.buildItem(rows, sideRow.Full, theme, highlightedRows, drafts, wrap, metrics)
	}
	separatorStyle := vaxis.Style{Foreground: theme.MutedForeground, Background: theme.Background}
	row := vui.Row(
		vui.Expanded(s.buildSideBySideCell(rows, sideRow.Left, sideLeft, theme, highlightedRows, wrap, metrics)),
		uiDiffFixedCell(1, separatorStyle, vui.Text{Value: " ", Style: separatorStyle}),
		vui.Expanded(s.buildSideBySideCell(rows, sideRow.Right, sideRight, theme, highlightedRows, wrap, metrics)),
	)
	children := []vui.Widget{row}
	for _, docRow := range sideBySideRowCommentDocRows(sideRow) {
		children = append(children, s.buildSideBySideCommentRows(rows, docRow, drafts, theme, metrics)...)
	}
	if len(children) == 1 {
		return row
	}
	return vui.Column(children...)
}

func (s *uiDiffViewState) buildSideBySideCell(rows []diff.Row, rowIndex int, side diffSide, theme vui.Theme, highlightedRows map[int][]vaxis.Segment, wrap bool, metrics uiDiffDocumentMetrics) vui.Widget {
	if rowIndex < 0 || rowIndex >= len(rows) {
		return vui.DecoratedBox(vui.Decoration{Style: vaxis.Style{Background: theme.Background}}, vui.SizedBox{Height: 1})
	}
	row := rows[rowIndex]
	active := rowIndex == s.cursor.Row && side == sideForRow(row) && !s.commentEditorInsert && !s.commentEditorFocused
	selected := s.lineSelected(rowIndex)
	yanked := s.lineYanked(rowIndex)
	style := uiStyleForDiffRow(row.Kind, theme)
	rowBackground := vaxis.Color(0)
	if active {
		rowBackground = uiDiffCursorRowBackground(theme)
	}
	fillBackground := style.Background
	if rowBackground != 0 {
		fillBackground = rowBackground
	}
	textBackground := rowBackground
	if selected {
		textBackground = theme.Selection
	} else if yanked {
		textBackground = uiDiffYankBackground(theme)
	}
	if textBackground != 0 {
		style.Background = textBackground
	}
	codeSegments := highlightedRows[rowIndex]
	if len(codeSegments) == 0 {
		codeSegments = []vaxis.Segment{{Text: row.Code, Style: style}}
	}
	codeSegments = uiDiffToneCodeSegments(row.Kind, codeSegments, theme)
	if textBackground != 0 {
		codeSegments = uiDiffApplyBackground(codeSegments, textBackground)
	}
	if start, end, ok := s.charSelectionRange(rowIndex, row); ok {
		codeSegments = uiDiffApplySegmentBackgroundRange(codeSegments, start, end, theme.Selection, tabWidthForFile(row.FileName))
	}
	if start, end, ok := s.charYankRange(rowIndex, row); ok {
		codeSegments = uiDiffApplySegmentBackgroundRange(codeSegments, start, end, uiDiffYankBackground(theme), tabWidthForFile(row.FileName))
	}
	codeSegments = uiDiffSearchSegments(rowIndex, row, codeSegments, s.searchMatches, theme)
	gutterStyle := uiGutterStyle(row.Kind, rowBackground, theme)
	lineNumberStyle := uiLineNumberGutterStyle(row.Kind, rowBackground, theme)
	gutter := uiDiffSideBySideGutterForWidth(row, side, metrics.SideBySideGutterWidth())
	lineNumber, marker := uiDiffSplitSideBySideGutter(gutter)
	return vui.Row(
		uiDiffFixedCell(len(lineNumber), lineNumberStyle, vui.Text{Value: lineNumber, Style: lineNumberStyle, Align: vui.TextAlignRight}),
		uiDiffFixedCell(3, gutterStyle, vui.Text{Value: marker, Style: gutterStyle}),
		vui.Expanded(uiDiffCodeWidget(row, row.Code, codeSegments, active, s.selectionActive && !s.selectionLinewise, s.cursor.Col, theme, fillBackground, wrap, s.xScroll)),
	)
}

func uiDiffSideBySideRows(rows []diff.Row) []sideBySideRow {
	return uiDiffSideBySideRowsForRows(rows)
}

func uiDiffSideBySideGutter(rows []diff.Row, row diff.Row, side diffSide) string {
	return uiDiffSideBySideGutterForRows(rows, row, side)
}

func (m uiDiffDocumentMetrics) SideBySideGutterWidth() int {
	width := maxInt(m.OldGutterWidth, m.NewGutterWidth)
	if width < 1 {
		return 1
	}
	return width
}

func uiDiffSideBySideGutterForWidth(row diff.Row, side diffSide, width int) string {
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

func uiDiffSplitSideBySideGutter(gutter string) (string, string) {
	if len(gutter) < 3 {
		return gutter, ""
	}
	return gutter[:len(gutter)-3], gutter[len(gutter)-3:]
}

func (s *uiDiffViewState) buildSideBySideCommentRows(rows []diff.Row, rowIndex int, drafts []review.CommentDraft, theme vui.Theme, metrics uiDiffDocumentMetrics) []vui.Widget {
	if rowIndex < 0 || rowIndex >= len(rows) {
		return nil
	}
	side := uiDiffCommentSideForRow(rows[rowIndex])
	commentIndent := metrics.SideBySideGutterWidth() + 3
	commentBodyWidth := s.commentEditorBodyWidth(commentIndent)
	widgets := make([]vui.Widget, 0, 1)
	addCommentWidget := func(widget vui.Widget) {
		widgets = append(widgets, uiDiffSideBySideCommentRow(uiDiffIndentedComment(commentIndent, widget, theme), side, theme))
	}
	showDrafts := true
	if s.commentEditorActive && s.commentEditorRow == rowIndex {
		addCommentWidget(s.buildCommentEditor(rows, theme, commentBodyWidth))
		showDrafts = false
	} else if body := s.commentEditorBodies[rowIndex]; strings.TrimSpace(body) != "" {
		addCommentWidget(uiDiffCommentEditorBox(body, nil, false, false, nil, 0, false, false, 0, commentBodyWidth, theme))
		showDrafts = false
	}
	if showDrafts {
		for _, draft := range reviewDraftsForRow(rows[rowIndex], drafts) {
			addCommentWidget(uiDiffReviewDraft(draft, theme, func(vui.EventContext) {
				s.focusCommentEditorRow(rows, rowIndex)
				s.commentEditorInsert = true
			}))
		}
	}
	return widgets
}

func uiDiffSideBySideCommentRow(comment vui.Widget, side diffSide, theme vui.Theme) vui.Widget {
	spacer := vui.DecoratedBox(vui.Decoration{Style: vaxis.Style{Background: theme.Background}}, vui.SizedBox{Height: 1})
	separatorStyle := vaxis.Style{Foreground: theme.MutedForeground, Background: theme.Background}
	left := spacer
	right := spacer
	if side == sideLeft {
		left = comment
	} else {
		right = comment
	}
	return vui.Row(
		vui.Expanded(left),
		uiDiffFixedCell(1, separatorStyle, vui.Text{Value: " ", Style: separatorStyle}),
		vui.Expanded(right),
	)
}

func uiDiffCommentSideForRow(row diff.Row) diffSide {
	if row.Review.Side == review.SideLeft {
		return sideLeft
	}
	if row.Review.Side == review.SideRight {
		return sideRight
	}
	return sideForRow(row)
}

func (s *uiDiffViewState) buildCommentEditor(rows []diff.Row, theme vui.Theme, bodyWidth int) vui.Widget {
	cursor := s.commentEditorCursor
	s.commentEditorCursor = nil
	return uiDiffCommentEditorBox(s.commentEditorBody, cursor, s.commentEditorFocused, s.commentEditorInsert, func(_ vui.EventContext, value string) {
		delta := len([]rune(value)) - len([]rune(s.commentEditorBody))
		s.commentEditorBody = value
		if s.commentEditorInsert {
			s.commentEditorNormalCursor = maxInt(0, minInt(s.commentEditorNormalCursor+delta, len([]rune(s.commentEditorBody))))
		}
		s.storeCommentEditorBody()
		s.revealCommentEditor(rows)
		s.SetState(func() {})
	}, s.commentEditorNormalCursor, s.commentEditorSelection, s.commentEditorLinewise, s.commentEditorAnchor, bodyWidth, theme)
}

func uiDiffCommentEditorBox(body string, cursorOffset *int, focused bool, insert bool, onChanged func(vui.EventContext, string), normalCursor int, selection bool, linewise bool, anchor int, bodyWidth int, theme vui.Theme) vui.Widget {
	background := theme.Surface
	if focused || insert {
		background = theme.SurfaceHovered
	}
	if bodyWidth <= 0 {
		bodyWidth = uiDiffCommentEditorDefaultBodyWidth
	}
	boxStyle := vaxis.Style{Foreground: theme.Foreground, Background: background}
	if !insert {
		if body == "" {
			body = "Add comment…"
		}
		return uiDiffCommentBoxForWidth(uiDiffCommentColumn(
			uiDiffCommentHalfBlock("▄", background, theme),
			vui.DecoratedBox(
				vui.Decoration{Style: boxStyle},
				vui.Padding(vui.Symmetric(2, 0), vui.RichText{Spans: commentEditorNormalSpans(body, normalCursor, focused, selection, linewise, anchor, boxStyle, theme), SoftWrap: true}),
			),
			uiDiffCommentHalfBlock("▀", background, theme),
		), bodyWidth+4)
	}
	return uiDiffCommentBoxForWidth(uiDiffCommentColumn(
		uiDiffCommentHalfBlock("▄", background, theme),
		vui.DecoratedBox(
			vui.Decoration{Style: boxStyle},
			vui.FocusScope{AutoFocus: true, Child: vui.TextArea{
				Value:        body,
				CursorOffset: cursorOffset,
				Placeholder:  "Add comment…",
				MinHeight:    1,
				SoftWrap:     true,
				Padding:      vui.Symmetric(2, 0),
				OnChanged:    onChanged,
			}},
		),
		uiDiffCommentHalfBlock("▀", background, theme),
	), bodyWidth+4)
}

func commentEditorNormalSpans(body string, cursor int, focused bool, selection bool, linewise bool, anchor int, style vaxis.Style, theme vui.Theme) []vui.TextSpan {
	runes := []rune(body)
	if len(runes) == 0 || !focused {
		return []vui.TextSpan{{Text: body, Style: style}}
	}
	cursor = minInt(maxInt(0, cursor), len(runes))
	if selection {
		start, end := commentEditorSelectionRange(body, anchor, cursor, linewise)
		if start < end {
			selectionStyle := style
			selectionStyle.Background = theme.Selection
			spans := make([]vui.TextSpan, 0, 3)
			if start > 0 {
				spans = append(spans, vui.TextSpan{Text: string(runes[:start]), Style: style})
			}
			spans = append(spans, vui.TextSpan{Text: string(runes[start:end]), Style: selectionStyle})
			if end < len(runes) {
				spans = append(spans, vui.TextSpan{Text: string(runes[end:]), Style: style})
			}
			return spans
		}
	}
	spans := make([]vui.TextSpan, 0, 3)
	if cursor > 0 {
		spans = append(spans, vui.TextSpan{Text: string(runes[:cursor]), Style: style})
	}
	cursorStyle := vaxis.Style{Foreground: uiDiffCursorForeground(theme), Background: uiDiffCursorBackground(theme)}
	if cursor == len(runes) || runes[cursor] == '\n' {
		spans = append(spans, vui.TextSpan{Text: " ", Style: cursorStyle})
		if cursor < len(runes) {
			spans = append(spans, vui.TextSpan{Text: string(runes[cursor:]), Style: style})
		}
		return spans
	}
	spans = append(spans, vui.TextSpan{Text: string(runes[cursor : cursor+1]), Style: cursorStyle})
	if cursor+1 < len(runes) {
		spans = append(spans, vui.TextSpan{Text: string(runes[cursor+1:]), Style: style})
	}
	return spans
}

func commentEditorLineEnd(body string, cursor int) int {
	runes := []rune(body)
	cursor = minInt(maxInt(0, cursor), len(runes))
	for cursor < len(runes) && runes[cursor] != '\n' {
		cursor++
	}
	return cursor
}

func commentEditorNextLineStart(body string, cursor int) int {
	runes := []rune(body)
	cursor = commentEditorLineEnd(body, cursor)
	if cursor < len(runes) {
		return cursor + 1
	}
	return cursor
}

func commentEditorSelectionRange(body string, anchor int, cursor int, linewise bool) (int, int) {
	runes := []rune(body)
	anchor = minInt(maxInt(0, anchor), len(runes))
	cursor = minInt(maxInt(0, cursor), len(runes))
	start := minInt(anchor, cursor)
	end := maxInt(anchor, cursor)
	if !linewise {
		if end < len(runes) {
			end++
		}
		return start, end
	}
	return commentEditorLineStart(body, start), commentEditorNextLineStart(body, end)
}

func commentEditorDeleteRange(body string, start int, end int) (string, int) {
	runes := []rune(body)
	start = minInt(maxInt(0, start), len(runes))
	end = minInt(maxInt(start, end), len(runes))
	next := string(runes[:start]) + string(runes[end:])
	cursor := minInt(start, maxInt(0, len([]rune(next))-1))
	return next, cursor
}

func commentEditorLineStart(body string, cursor int) int {
	runes := []rune(body)
	cursor = minInt(maxInt(0, cursor), len(runes))
	for cursor > 0 && runes[cursor-1] != '\n' {
		cursor--
	}
	return cursor
}

func commentEditorInsertRunes(body string, cursor int, insert string) string {
	runes := []rune(body)
	cursor = minInt(maxInt(0, cursor), len(runes))
	return string(runes[:cursor]) + insert + string(runes[cursor:])
}

func commentEditorLastLineCursor(body string) int {
	runes := []rune(body)
	if len(runes) == 0 {
		return 0
	}
	cursor := len(runes) - 1
	for cursor > 0 && runes[cursor-1] != '\n' {
		cursor--
	}
	return cursor
}

func commentEditorMoveLine(body string, cursor int, delta int) int {
	runes := []rune(body)
	if len(runes) == 0 {
		return 0
	}
	cursor = minInt(maxInt(0, cursor), len(runes)-1)
	start := cursor
	for start > 0 && runes[start-1] != '\n' {
		start--
	}
	col := cursor - start
	if delta < 0 {
		if start == 0 {
			return cursor
		}
		prevEnd := start - 1
		prevStart := prevEnd
		for prevStart > 0 && runes[prevStart-1] != '\n' {
			prevStart--
		}
		return minInt(prevStart+col, prevEnd)
	}
	end := start
	for end < len(runes) && runes[end] != '\n' {
		end++
	}
	if end >= len(runes) {
		return cursor
	}
	nextStart := end + 1
	nextEnd := nextStart
	for nextEnd < len(runes) && runes[nextEnd] != '\n' {
		nextEnd++
	}
	return minInt(nextStart+col, maxInt(nextStart, nextEnd-1))
}

func uiDiffCommentColumn(children ...vui.Widget) vui.Widget {
	return vui.Flex{Axis: vui.Vertical, CrossAxisAlignment: vui.CrossAxisStretch, Children: children}
}

func uiDiffCommentHalfBlock(block string, foreground vaxis.Color, theme vui.Theme) vui.Widget {
	style := vaxis.Style{Foreground: foreground, Background: theme.Background}
	return vui.Text{Value: strings.Repeat(block, 72), Style: style, Overflow: vui.TextOverflowClip}
}

func uiDiffReviewDraft(draft review.CommentDraft, theme vui.Theme, _ func(vui.EventContext)) vui.Widget {
	background := theme.Surface
	boxStyle := vaxis.Style{Foreground: theme.Foreground, Background: background}
	body := draft.Body
	if body == "" {
		body = "Add comment…"
	}
	return uiDiffCommentBox(uiDiffCommentColumn(
		uiDiffCommentHalfBlock("▄", background, theme),
		vui.DecoratedBox(
			vui.Decoration{Style: boxStyle},
			vui.Padding(vui.Symmetric(2, 0), vui.RichText{Spans: []vui.TextSpan{{Text: body, Style: boxStyle}}, SoftWrap: true}),
		),
		uiDiffCommentHalfBlock("▀", background, theme),
	))
}

func uiDiffCommentBox(child vui.Widget) vui.Widget {
	return uiDiffCommentBoxForWidth(child, 72)
}

func uiDiffCommentBoxForWidth(child vui.Widget, width int) vui.Widget {
	return vui.ConstrainedBox{Constraints: vui.Constraints{MaxWidth: minInt(72, maxInt(1, width))}, Child: child}
}

const uiDiffCommentEditorDefaultBodyWidth = 40

func uiDiffCommentEditorRows(body string) int {
	return uiDiffCommentEditorBodyRows(body) + 2
}

func uiDiffCommentEditorBodyRows(body string) int {
	return uiDiffCommentEditorBodyRowsForWidth(body, uiDiffCommentEditorDefaultBodyWidth)
}

func uiDiffCommentEditorRowsForWidth(body string, width int) int {
	return uiDiffCommentEditorBodyRowsForWidth(body, width) + 2
}

func uiDiffCommentEditorBodyRowsForWidth(body string, width int) int {
	if body == "" {
		return 1
	}
	width = maxInt(1, width)
	rows := 0
	for _, line := range strings.Split(body, "\n") {
		lineWidth := textCellWidth(line)
		rows += maxInt(1, (lineWidth+width-1)/width)
	}
	return rows
}

func (s *uiDiffViewState) commentEditorBodyWidth(commentIndent int) int {
	if !s.scroll.Attached() {
		return uiDiffCommentEditorDefaultBodyWidth
	}
	viewportWidth := s.scroll.Metrics().ViewportWidth
	editorWidth := minInt(72, maxInt(1, viewportWidth-commentIndent))
	return maxInt(1, editorWidth-4)
}

func uiDiffIndentedComment(indent int, child vui.Widget, theme vui.Theme) vui.Widget {
	if indent <= 0 {
		return child
	}
	style := vaxis.Style{Background: theme.Background}
	return vui.Row(
		uiDiffFixedCell(indent, style, vui.Text{Value: strings.Repeat(" ", indent), Style: style}),
		child,
	)
}

func reviewDraftsForRow(row diff.Row, drafts []review.CommentDraft) []review.CommentDraft {
	if !reviewAnchorValid(row.Review) {
		return nil
	}
	matches := make([]review.CommentDraft, 0, 1)
	for _, draft := range drafts {
		if reviewDraftEndsAt(draft, row.Review) {
			matches = append(matches, draft)
		}
	}
	return matches
}

func uiDiffRowBackground(active bool, selected bool, yanked bool, theme vui.Theme) vaxis.Color {
	if selected {
		return theme.Selection
	}
	if yanked {
		return uiDiffYankBackground(theme)
	}
	if active {
		return uiDiffCursorRowBackground(theme)
	}
	return 0
}

func (s *uiDiffViewState) buildFullWidthRow(row diff.Row, rowIndex int, background vaxis.Color, active bool, cursorCol int, selected bool, yanked bool, theme vui.Theme, wrap bool) vui.Widget {
	if row.Kind == diff.RowCommitMessage {
		wrap = true
	}
	if segments, ok := uiDiffStructuredSegments(row, theme); ok {
		if background != 0 {
			segments = uiDiffApplyBackground(segments, background)
		}
		if start, end, ok := s.charSelectionRange(rowIndex, row); ok {
			segments = uiDiffApplySegmentBackgroundRange(segments, start, end, theme.Selection, tabWidthForFile(row.FileName))
		}
		if start, end, ok := s.charYankRange(rowIndex, row); ok {
			segments = uiDiffApplySegmentBackgroundRange(segments, start, end, uiDiffYankBackground(theme), tabWidthForFile(row.FileName))
		}
		segments = uiDiffSearchSegments(rowIndex, row, segments, s.searchMatches, theme)
		if active {
			segments = uiDiffCursorSegments(segments, cursorCol, vaxis.Style{Foreground: uiDiffCursorForeground(theme), Background: uiDiffCursorBackground(theme)}, tabWidthForFile(row.FileName), false)
		}
		return uiDiffFullWidthRowBox(segments, background, wrap, uiDiffFullWidthRowFills(row))
	}
	style := uiStyleForDiffRow(row.Kind, theme)
	textBackground := background
	if selected {
		textBackground = theme.Selection
	} else if yanked {
		textBackground = uiDiffYankBackground(theme)
	}
	if background != 0 {
		style.Background = background
	}
	segments := []vaxis.Segment{{Text: uiDiffRowCode(row), Style: style}}
	if textBackground != 0 {
		segments = uiDiffApplyBackground(segments, textBackground)
	}
	if start, end, ok := s.charSelectionRange(rowIndex, row); ok {
		segments = uiDiffApplySegmentBackgroundRange(segments, start, end, theme.Selection, tabWidthForFile(row.FileName))
	}
	if start, end, ok := s.charYankRange(rowIndex, row); ok {
		segments = uiDiffApplySegmentBackgroundRange(segments, start, end, uiDiffYankBackground(theme), tabWidthForFile(row.FileName))
	}
	segments = uiDiffSearchSegments(rowIndex, row, segments, s.searchMatches, theme)
	if active {
		segments = uiDiffCursorSegments(segments, cursorCol, vaxis.Style{Foreground: uiDiffCursorForeground(theme), Background: uiDiffCursorBackground(theme)}, tabWidthForFile(row.FileName), false)
	}
	return uiDiffFullWidthRowBox(segments, background, wrap, uiDiffFullWidthRowFills(row))
}

func uiDiffFullWidthRowFills(row diff.Row) bool {
	switch row.Kind {
	case diff.RowCommitHeader, diff.RowCommitMeta, diff.RowCommitMessage, diff.RowCommitTrailer:
		return true
	default:
		return false
	}
}

func uiDiffFullWidthRowBox(segments []vaxis.Segment, background vaxis.Color, wrap bool, fillRow bool) vui.Widget {
	text := vui.RichText{Spans: uiTextSpans(segments), SoftWrap: wrap}
	if background == 0 || !fillRow {
		return text
	}
	if wrap {
		return vui.DecoratedBox(
			vui.Decoration{Style: vaxis.Style{Background: background}},
			text,
		)
	}
	return vui.DecoratedBox(
		vui.Decoration{Style: vaxis.Style{Background: background}},
		vui.SizedBox{Width: 10000, Child: text},
	)
}

func uiDiffStructuredSegments(row diff.Row, theme vui.Theme) ([]vaxis.Segment, bool) {
	switch row.Kind {
	case diff.RowHunk:
		if row.Prefix == "" && row.Code == "" {
			return nil, false
		}
		return []vaxis.Segment{
			{Text: row.Prefix, Style: uiStyleForDiffRow(diff.RowHunk, theme)},
			{Text: row.Code, Style: uiDimStyle(theme)},
		}, true
	case diff.RowCommitHeader:
		if row.Prefix == "" && row.Code == "" {
			return nil, false
		}
		return []vaxis.Segment{
			{Text: row.Prefix, Style: uiDimStyle(theme)},
			{Text: row.Code, Style: uiCommitHashStyle(theme)},
		}, true
	case diff.RowCommitMeta:
		if row.Prefix == "" && row.Code == "" {
			return nil, false
		}
		return []vaxis.Segment{
			{Text: row.Prefix, Style: uiCommitLabelStyle(theme)},
			{Text: row.Code, Style: uiCommitMetaStyle(theme)},
		}, true
	case diff.RowCommitTrailer:
		if row.Prefix == "" && row.Code == "" {
			return nil, false
		}
		return []vaxis.Segment{
			{Text: row.Prefix, Style: uiCommitTrailerLabelStyle(theme)},
			{Text: row.Code, Style: uiCommitTrailerValueStyle(theme)},
		}, true
	case diff.RowDiffStat:
		return uiDiffStatSegments(row, theme), true
	case diff.RowDiffStatSummary:
		return uiDiffStatSummarySegments(row, theme), true
	default:
		return nil, false
	}
}

func uiDiffApplyBackground(segments []vaxis.Segment, background vaxis.Color) []vaxis.Segment {
	styled := make([]vaxis.Segment, len(segments))
	for i, segment := range segments {
		segment.Style.Background = background
		styled[i] = segment
	}
	return styled
}

func uiDiffSearchSegments(rowIndex int, row diff.Row, segments []vaxis.Segment, matches []searchMatch, theme vui.Theme) []vaxis.Segment {
	if len(matches) == 0 || rowIndex < 0 {
		return segments
	}
	style := uiDiffSearchHighlightStyle(theme)
	for _, match := range matches {
		if match.Row != rowIndex {
			continue
		}
		start := match.Start
		end := match.End
		if row.Code != "" && uiDiffRowUsesGrid(row) {
			segments = styleSegmentsRangeFullWithTabWidth(segments, start, end, style, tabWidthForFile(row.FileName))
		} else {
			segments = styleSegmentsRangeFull(segments, start, end, style)
		}
	}
	return segments
}

func uiDiffSearchHighlightStyle(theme vui.Theme) vaxis.Style {
	return vaxis.Style{Foreground: theme.Background, Background: theme.Warning}
}

func uiDiffCursorRowBackground(theme vui.Theme) vaxis.Color {
	return theme.SurfaceHovered
}

func uiDiffYankBackground(theme vui.Theme) vaxis.Color {
	return theme.Warning
}

func uiDiffLineBackground(theme vui.Theme, scale vui.ColorScale) vaxis.Color {
	if theme.Mode == vui.LightTheme {
		return scale.Tone50
	}
	return scale.Tone950
}

func uiDiffCursorBackground(theme vui.Theme) vaxis.Color {
	return theme.Foreground
}

func uiDiffCursorForeground(theme vui.Theme) vaxis.Color {
	if theme.Mode == vui.LightTheme {
		return theme.Palette.Neutral.Tone50
	}
	return theme.Palette.Neutral.Tone950
}

func uiDiffChangedGutterForeground(theme vui.Theme, scale vui.ColorScale) vaxis.Color {
	if theme.Mode == vui.LightTheme {
		return scale.Tone700
	}
	return scale.Tone400
}

func uiDiffStatSegments(row diff.Row, theme vui.Theme) []vaxis.Segment {
	baseStyle := vaxis.Style{Foreground: theme.Foreground, Background: theme.Background}
	barStyle := uiDimStyle(theme)
	addStyle := baseStyle
	addStyle.Foreground = theme.Success
	deleteStyle := baseStyle
	deleteStyle.Foreground = theme.Danger

	segments := []vaxis.Segment{
		{Text: " " + row.Stat.Path, Style: baseStyle},
		{Text: " | ", Style: uiDimStyle(theme)},
	}
	if row.Stat.Changed > 0 {
		segments = append(segments, vaxis.Segment{Text: fmt.Sprintf("%d ", row.Stat.Changed), Style: barStyle})
	}
	for _, r := range row.Stat.Bar {
		style := barStyle
		switch r {
		case '+':
			style = addStyle
		case '-':
			style = deleteStyle
		}
		segments = append(segments, vaxis.Segment{Text: string(r), Style: style})
	}
	return segments
}

func uiDiffStatSummarySegments(row diff.Row, theme vui.Theme) []vaxis.Segment {
	baseStyle := uiDimStyle(theme)
	addStyle := baseStyle
	addStyle.Foreground = theme.Success
	deleteStyle := baseStyle
	deleteStyle.Foreground = theme.Danger
	return []vaxis.Segment{
		{Text: fmt.Sprintf(" %d %s changed", row.Stat.Files, pluralize(row.Stat.Files, "file")), Style: baseStyle},
		{Text: fmt.Sprintf(", +%d", row.Stat.Adds), Style: addStyle},
		{Text: fmt.Sprintf(" -%d", row.Stat.Deletes), Style: deleteStyle},
	}
}

func uiDimStyle(theme vui.Theme) vaxis.Style {
	return vaxis.Style{Foreground: theme.DisabledForeground, Background: theme.Background}
}

func uiCommitHashStyle(theme vui.Theme) vaxis.Style {
	return vaxis.Style{Foreground: theme.Warning, Background: theme.Background, Attribute: vaxis.AttrBold}
}

func uiCommitLabelStyle(theme vui.Theme) vaxis.Style {
	return vaxis.Style{Foreground: theme.MutedForeground, Background: theme.Background}
}

func uiCommitMetaStyle(theme vui.Theme) vaxis.Style {
	return vaxis.Style{Foreground: theme.Palette.Cyan.Tone500, Background: theme.Background}
}

func uiCommitTrailerLabelStyle(theme vui.Theme) vaxis.Style {
	return vaxis.Style{Foreground: theme.Primary, Background: theme.Background, Attribute: vaxis.AttrBold}
}

func uiCommitTrailerValueStyle(theme vui.Theme) vaxis.Style {
	return vaxis.Style{Foreground: theme.DisabledForeground, Background: theme.Background}
}

func (s *uiDiffViewState) buildRow(row diff.Row, rowIndex int, active bool, selected bool, yanked bool, cursorCol int, theme vui.Theme, highlightedRows map[int][]vaxis.Segment, searchMatches []searchMatch, wrap bool, metrics uiDiffDocumentMetrics) vui.Widget {
	style := uiStyleForDiffRow(row.Kind, theme)
	rowBackground := vaxis.Color(0)
	if active {
		rowBackground = uiDiffCursorRowBackground(theme)
	}
	fillBackground := style.Background
	if rowBackground != 0 {
		fillBackground = rowBackground
	}
	textBackground := rowBackground
	if selected {
		textBackground = theme.Selection
	} else if yanked {
		textBackground = uiDiffYankBackground(theme)
	}
	if textBackground != 0 {
		style.Background = textBackground
	}
	code := uiDiffRowCode(row)
	codeSegments := highlightedRows[rowIndex]
	if len(codeSegments) == 0 {
		codeSegments = []vaxis.Segment{{Text: code, Style: style}}
	}
	codeSegments = uiDiffToneCodeSegments(row.Kind, codeSegments, theme)
	if textBackground != 0 {
		codeSegments = uiDiffApplyBackground(codeSegments, textBackground)
	}
	if start, end, ok := s.charSelectionRange(rowIndex, row); ok {
		codeSegments = uiDiffApplySegmentBackgroundRange(codeSegments, start, end, theme.Selection, tabWidthForFile(row.FileName))
	}
	if start, end, ok := s.charYankRange(rowIndex, row); ok {
		codeSegments = uiDiffApplySegmentBackgroundRange(codeSegments, start, end, uiDiffYankBackground(theme), tabWidthForFile(row.FileName))
	}
	codeSegments = uiDiffSearchSegments(rowIndex, row, codeSegments, searchMatches, theme)
	oldLine, newLine, marker := splitDiffGutter(row)
	oldWidth, newWidth := metrics.OldGutterWidth, metrics.NewGutterWidth
	gutterStyle := uiGutterStyle(row.Kind, rowBackground, theme)
	lineNumberStyle := uiLineNumberGutterStyle(row.Kind, rowBackground, theme)
	return vui.Row(
		uiDiffFixedCell(oldWidth, lineNumberStyle, vui.Text{Value: oldLine, Style: lineNumberStyle, Align: vui.TextAlignRight}),
		uiDiffFixedCell(1, gutterStyle, vui.Text{Value: " ", Style: gutterStyle}),
		uiDiffFixedCell(newWidth, lineNumberStyle, vui.Text{Value: newLine, Style: lineNumberStyle, Align: vui.TextAlignRight}),
		uiDiffFixedCell(1, gutterStyle, vui.Text{Value: " ", Style: gutterStyle}),
		uiDiffFixedCell(1, gutterStyle, vui.Text{Value: marker, Style: gutterStyle}),
		uiDiffFixedCell(1, gutterStyle, vui.Text{Value: " ", Style: gutterStyle}),
		vui.Expanded(uiDiffCodeWidget(row, code, codeSegments, active, s.selectionActive && !s.selectionLinewise, cursorCol, theme, fillBackground, wrap, s.xScroll)),
	)
}

func uiDiffFixedCell(width int, style vaxis.Style, child vui.Widget) vui.Widget {
	return vui.DecoratedBox(
		vui.Decoration{Style: style},
		vui.SizedBox{Width: width, Height: 1, Child: child},
	)
}

func uiDiffCodeWidget(row diff.Row, code string, segments []vaxis.Segment, active bool, cursorTabEnd bool, cursorCol int, theme vui.Theme, background vaxis.Color, wrap bool, xScroll int) vui.Widget {
	tabWidth := tabWidthForFile(row.FileName)
	if wrap {
		xScroll = 0
	}
	if !active || code == "" {
		segments = uiDiffClipSegments(segments, xScroll, tabWidth)
		segments = uiDiffExpandTabs(segments, tabWidth)
		return vui.DecoratedBox(
			vui.Decoration{Style: vaxis.Style{Background: background}},
			vui.RichText{Spans: uiTextSpans(segments), SoftWrap: wrap, Overflow: vui.TextOverflowClip},
		)
	}
	cursorStyle := vaxis.Style{Foreground: uiDiffCursorForeground(theme), Background: uiDiffCursorBackground(theme)}
	segments = uiDiffCursorSegments(segments, cursorCol, cursorStyle, tabWidth, cursorTabEnd)
	segments = uiDiffClipSegments(segments, xScroll, tabWidth)
	segments = uiDiffExpandTabs(segments, tabWidth)
	return vui.DecoratedBox(
		vui.Decoration{Style: vaxis.Style{Background: background}},
		vui.RichText{Spans: uiTextSpans(segments), SoftWrap: wrap, Overflow: vui.TextOverflowClip},
	)
}

func uiDiffExpandTabs(segments []vaxis.Segment, tabWidth int) []vaxis.Segment {
	if tabWidth <= 0 {
		tabWidth = defaultTabWidth
	}
	expanded := make([]vaxis.Segment, 0, len(segments))
	for _, segment := range segments {
		if !strings.Contains(segment.Text, "\t") {
			expanded = appendSegment(expanded, segment)
			continue
		}
		segment.Text = strings.ReplaceAll(segment.Text, "\t", strings.Repeat(" ", tabWidth))
		expanded = appendSegment(expanded, segment)
	}
	return expanded
}

func uiDiffCursorSegments(segments []vaxis.Segment, cursorCol int, cursorStyle vaxis.Style, tabWidth int, cursorTabEnd bool) []vaxis.Segment {
	var styled []vaxis.Segment
	col := 0
	for _, segment := range segments {
		it := uucode.NewGraphemeIterator(segment.Text)
		for g, ok := it.Next(); ok; g, ok = it.Next() {
			grapheme := segment.Text[g.Start:g.End]
			char := characterForGraphemeWithTabWidth(grapheme, tabWidth)
			next := col + char.Width
			if cursorCol >= col && cursorCol < next {
				if grapheme == "\t" && char.Width > 1 {
					if cursorTabEnd {
						styled = appendSegment(styled, vaxis.Segment{Text: strings.Repeat(" ", char.Width-1), Style: segment.Style})
						styled = appendSegment(styled, vaxis.Segment{Text: " ", Style: cursorStyle})
					} else {
						styled = appendSegment(styled, vaxis.Segment{Text: " ", Style: cursorStyle})
						styled = appendSegment(styled, vaxis.Segment{Text: strings.Repeat(" ", char.Width-1), Style: segment.Style})
					}
				} else {
					styled = appendSegment(styled, vaxis.Segment{Text: grapheme, Style: cursorStyle})
				}
			} else {
				styled = appendSegment(styled, vaxis.Segment{Text: grapheme, Style: segment.Style})
			}
			col = next
		}
	}
	return styled
}

func uiDiffClipSegments(segments []vaxis.Segment, xScroll int, tabWidth int) []vaxis.Segment {
	if xScroll <= 0 {
		return segments
	}
	clipped := make([]vaxis.Segment, 0, len(segments))
	col := 0
	for _, segment := range segments {
		it := uucode.NewGraphemeIterator(segment.Text)
		text := ""
		for g, ok := it.Next(); ok; g, ok = it.Next() {
			grapheme := segment.Text[g.Start:g.End]
			char := characterForGraphemeWithTabWidth(grapheme, tabWidth)
			next := col + char.Width
			if next > xScroll {
				if col < xScroll && char.Width > 1 {
					text += strings.Repeat(" ", next-xScroll)
				} else {
					text += grapheme
				}
			}
			col = next
		}
		if text != "" {
			segment.Text = text
			clipped = append(clipped, segment)
		}
	}
	return clipped
}

func uiDiffApplySegmentBackgroundRange(segments []vaxis.Segment, start int, end int, background vaxis.Color, tabWidth int) []vaxis.Segment {
	if end <= start {
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
			style := segment.Style
			if next > start && col < end {
				style.Background = background
			}
			styled = appendSegment(styled, vaxis.Segment{Text: grapheme, Style: style})
			col = next
		}
	}
	return styled
}

func uiTextSpans(segments []vaxis.Segment) []vui.TextSpan {
	spans := make([]vui.TextSpan, 0, len(segments))
	for _, segment := range segments {
		spans = append(spans, vui.TextSpan{Text: segment.Text, Style: segment.Style})
	}
	return spans
}

func uiDiffToneCodeSegments(kind diff.RowKind, segments []vaxis.Segment, theme vui.Theme) []vaxis.Segment {
	switch kind {
	case diff.RowContext:
		return uiDiffDimSegments(segments, theme, 1)
	case diff.RowDelete:
		return uiDiffDimSegments(segments, theme, 1)
	default:
		return segments
	}
}

func uiDiffDimSegments(segments []vaxis.Segment, theme vui.Theme, steps int) []vaxis.Segment {
	styled := make([]vaxis.Segment, len(segments))
	for i, segment := range segments {
		if segment.Style.Foreground != vaxis.ColorDefault {
			segment.Style.Foreground = uiDiffDimSyntaxColor(segment.Style.Foreground, theme, steps)
		}
		styled[i] = segment
	}
	return styled
}

func uiDiffDimSyntaxColor(color vaxis.Color, theme vui.Theme, steps int) vaxis.Color {
	if color == theme.Foreground {
		return theme.MutedForeground
	}
	if dimmed, ok := uiDiffDimScaleColor(color, theme.Palette.Blue, theme.Mode, steps); ok {
		return dimmed
	}
	if dimmed, ok := uiDiffDimScaleColor(color, theme.Palette.Cyan, theme.Mode, steps); ok {
		return dimmed
	}
	if dimmed, ok := uiDiffDimScaleColor(color, theme.Palette.Green, theme.Mode, steps); ok {
		return dimmed
	}
	if dimmed, ok := uiDiffDimScaleColor(color, theme.Palette.Magenta, theme.Mode, steps); ok {
		return dimmed
	}
	if dimmed, ok := uiDiffDimScaleColor(color, theme.Palette.Yellow, theme.Mode, steps); ok {
		return dimmed
	}
	if dimmed, ok := uiDiffDimScaleColor(color, theme.Palette.Red, theme.Mode, steps); ok {
		return dimmed
	}
	return color
}

func uiDiffDimScaleColor(color vaxis.Color, scale vui.ColorScale, mode vui.ThemeMode, steps int) (vaxis.Color, bool) {
	tone := []vaxis.Color{
		scale.Tone50,
		scale.Tone100,
		scale.Tone200,
		scale.Tone300,
		scale.Tone400,
		scale.Tone500,
		scale.Tone600,
		scale.Tone700,
		scale.Tone800,
		scale.Tone900,
		scale.Tone950,
	}
	for index, candidate := range tone {
		if color != candidate {
			continue
		}
		if mode == vui.LightTheme {
			return tone[clampUIDiffInt(index-steps, 0, len(tone)-1)], true
		}
		return tone[clampUIDiffInt(index+steps, 0, len(tone)-1)], true
	}
	return color, false
}

func uiDiffGutterWidths(rows []diff.Row) (int, int) {
	oldWidth := 0
	newWidth := 0
	for _, row := range rows {
		if !uiDiffRowUsesGrid(row) {
			continue
		}
		oldLine, newLine, _ := splitDiffGutter(row)
		oldWidth = maxInt(oldWidth, textCellWidth(oldLine))
		newWidth = maxInt(newWidth, textCellWidth(newLine))
	}
	return oldWidth, newWidth
}

func uiDiffRowUsesGrid(row diff.Row) bool {
	return row.Gutter != "" || row.Marker != ""
}

func (s *uiDiffViewState) setCursorRow(rows []diff.Row, row int) {
	if len(rows) == 0 {
		s.cursor = selectionPoint{}
		return
	}
	if s.selectionActive {
		s.cursor.Row = uiDiffSelectableTargetRow(rows, row, signUIDiffInt(row-s.cursor.Row))
	} else {
		s.cursor.Row = uiDiffCursorTargetRow(rows, row, signUIDiffInt(row-s.cursor.Row))
	}
	s.cursor.Col = s.clampCursorCol(rows, s.cursor.Row, s.cursorCol)
	s.cursorCol = s.cursor.Col
	s.revealCursorRow()
	s.SetState(func() {})
}

func (s *uiDiffViewState) moveCursorRows(rows []diff.Row, delta int) {
	if delta == 0 {
		return
	}
	if absInt(delta) == 1 && s.cursorStepScroll(rows, delta) {
		return
	}
	s.setCursorRow(rows, s.cursor.Row+delta)
}

func (s *uiDiffViewState) cursorStepScroll(rows []diff.Row, delta int) bool {
	if !s.list.Attached() || !s.scroll.Attached() {
		return false
	}
	oldRow := s.cursor.Row
	target := oldRow + delta
	if s.selectionActive {
		target = uiDiffSelectableTargetRow(rows, target, delta)
	} else {
		target = uiDiffCursorTargetRow(rows, target, delta)
	}
	if target == oldRow {
		return false
	}
	s.cursor.Row = target
	s.cursor.Col = s.clampCursorCol(rows, s.cursor.Row, s.cursorCol)
	s.cursorCol = s.cursor.Col

	visualRow := target
	if s.sideBySide {
		if row, ok := uiDiffSideBySideVisualIndex(rows, target); ok {
			visualRow = row
		}
	}
	s.revealVisualCursorRow(visualRow)
	s.SetState(func() {})
	return true
}

func (s *uiDiffViewState) revealVisualCursorRow(visualRow int) {
	offset, ok := s.list.OffsetForIndex(visualRow)
	if !ok {
		s.revealCursorRow()
		return
	}
	metrics := s.scroll.Metrics()
	viewportHeight := metrics.ViewportHeight
	if viewportHeight <= 0 {
		s.revealCursorRow()
		return
	}
	extent := 1
	if next, ok := s.list.OffsetForIndex(visualRow + 1); ok && next > offset {
		extent = next - offset
	}
	switch {
	case offset < metrics.ScrollOffset:
		s.scroll.ScrollToOffset(offset)
	case offset+extent > metrics.ScrollOffset+viewportHeight:
		s.scroll.ScrollToOffset(offset + extent - viewportHeight)
	}
}

func uiDiffCursorTargetRow(rows []diff.Row, row int, direction int) int {
	if len(rows) == 0 {
		return 0
	}
	row = clampUIDiffInt(row, 0, len(rows)-1)
	if uiDiffCursorableRow(rows[row]) {
		return row
	}
	if direction == 0 {
		direction = 1
	}
	for next := row + direction; next >= 0 && next < len(rows); next += direction {
		if uiDiffCursorableRow(rows[next]) {
			return next
		}
	}
	for next := row - direction; next >= 0 && next < len(rows); next -= direction {
		if uiDiffCursorableRow(rows[next]) {
			return next
		}
	}
	return row
}

func uiDiffSelectableTargetRow(rows []diff.Row, row int, direction int) int {
	if len(rows) == 0 {
		return 0
	}
	row = clampUIDiffInt(row, 0, len(rows)-1)
	if selectableDiffRow(rows[row].Kind) {
		return row
	}
	if direction >= 0 {
		for next := row + 1; next < len(rows); next++ {
			if selectableDiffRow(rows[next].Kind) {
				return next
			}
		}
	}
	if direction <= 0 {
		for next := row - 1; next >= 0; next-- {
			if selectableDiffRow(rows[next].Kind) {
				return next
			}
		}
	}
	return row
}

func uiDiffCursorableRow(row diff.Row) bool {
	switch row.Kind {
	case diff.RowBlank, diff.RowFile, diff.RowHunk:
		return false
	default:
		return true
	}
}

func signUIDiffInt(v int) int {
	if v < 0 {
		return -1
	}
	if v > 0 {
		return 1
	}
	return 0
}

func (s *uiDiffViewState) jumpCommit(rows []diff.Row, direction int) {
	if len(rows) == 0 {
		return
	}
	if direction < 0 {
		for row := s.cursor.Row - 1; row >= 0; row-- {
			if rows[row].Kind == diff.RowCommitHeader {
				s.setCursorRowAtStart(rows, row)
				return
			}
		}
		return
	}
	for row := s.cursor.Row + 1; row < len(rows); row++ {
		if rows[row].Kind == diff.RowCommitHeader {
			s.setCursorRowAtStart(rows, row)
			return
		}
	}
}

func (s *uiDiffViewState) setCursorRowAtStart(rows []diff.Row, row int) {
	s.setCursorRow(rows, row)
	s.list.ScrollToIndex(s.cursor.Row, vui.ScrollAlignStart)
}

func (s *uiDiffViewState) jumpChange(rows []diff.Row, direction int) {
	targets := uiDiffChangeTargetRows(rows)
	s.jumpTargetRow(rows, targets, direction)
}

func (s *uiDiffViewState) jumpNote(rows []diff.Row, drafts []review.CommentDraft, direction int) {
	targets := noteTargetRows(rows, drafts)
	s.jumpTargetRow(rows, targets, direction)
}

func (s *uiDiffViewState) jumpTargetRow(rows []diff.Row, targets []int, direction int) {
	if len(targets) == 0 {
		return
	}
	if direction < 0 {
		for index := len(targets) - 1; index >= 0; index-- {
			if targets[index] < s.cursor.Row {
				s.setCursorRow(rows, targets[index])
				return
			}
		}
		return
	}
	for _, target := range targets {
		if target > s.cursor.Row {
			s.setCursorRow(rows, target)
			return
		}
	}
}

func (s *uiDiffViewState) updateSearchMatches(rows []diff.Row) {
	if s.searchQuery == "" {
		s.searchMatches = nil
		s.searchIndex = -1
		return
	}
	query := strings.ToLower(s.searchQuery)
	matches := make([]searchMatch, 0)
	for rowIndex, row := range rows {
		searchText, offset := uiDiffSearchableText(row)
		text := strings.ToLower(searchText)
		for start := 0; ; {
			index := strings.Index(text[start:], query)
			if index < 0 {
				break
			}
			matchStart := start + index
			matchEnd := matchStart + len(query)
			matchStartCol := textCellWidth(searchText[:matchStart])
			matchEndCol := textCellWidth(searchText[:matchEnd])
			if uiDiffRowUsesGrid(row) && row.Code != "" {
				matchStartCol = textCellWidthWithTabWidth(searchText[:matchStart], tabWidthForFile(row.FileName))
				matchEndCol = textCellWidthWithTabWidth(searchText[:matchEnd], tabWidthForFile(row.FileName))
			}
			matches = append(matches, searchMatch{Row: rowIndex, Start: offset + matchStartCol, End: offset + matchEndCol})
			start = matchEnd
		}
	}
	s.searchMatches = matches
	s.searchIndex = -1
}

func uiDiffSearchableText(row diff.Row) (string, int) {
	if row.Kind == diff.RowHunk {
		return "", 0
	}
	if uiDiffRowUsesGrid(row) && row.Code != "" {
		return row.Code, 0
	}
	if row.Prefix != "" || row.Code != "" {
		return row.Prefix + row.Code, 0
	}
	return uiDiffRowCode(row), 0
}

func (s *uiDiffViewState) moveSearchMatch(rows []diff.Row, delta int) {
	if len(s.searchMatches) == 0 {
		return
	}
	if s.searchIndex < 0 || s.searchIndex >= len(s.searchMatches) {
		s.searchIndex = s.nextSearchIndexFromPoint(s.cursor, delta)
	} else {
		s.searchIndex = (s.searchIndex + delta + len(s.searchMatches)) % len(s.searchMatches)
	}
	s.applySearchMatch(rows)
	s.SetState(func() {})
}

func (s *uiDiffViewState) nextSearchIndexFromPoint(origin selectionPoint, direction int) int {
	if len(s.searchMatches) == 0 {
		return -1
	}
	if direction < 0 {
		for index := len(s.searchMatches) - 1; index >= 0; index-- {
			point := selectionPoint{Row: s.searchMatches[index].Row, Col: s.searchMatches[index].Start}
			if selectionPointLess(point, origin) {
				return index
			}
		}
		return len(s.searchMatches) - 1
	}
	for index, match := range s.searchMatches {
		point := selectionPoint{Row: match.Row, Col: match.Start}
		if selectionPointLess(origin, point) || origin == point {
			return index
		}
	}
	return 0
}

func (s *uiDiffViewState) applySearchMatch(rows []diff.Row) {
	if s.searchIndex < 0 || s.searchIndex >= len(s.searchMatches) {
		return
	}
	match := s.searchMatches[s.searchIndex]
	if s.searchMode {
		s.setCursorPointWithoutReveal(rows, selectionPoint{Row: match.Row, Col: match.Start})
		return
	}
	s.setCursorPoint(rows, selectionPoint{Row: match.Row, Col: match.Start})
}

func (s *uiDiffViewState) setCursorPoint(rows []diff.Row, point selectionPoint) {
	if len(rows) == 0 {
		return
	}
	s.setCursorPointWithoutReveal(rows, point)
	s.revealCursorRow()
}

func (s *uiDiffViewState) setCursorPointWithoutReveal(rows []diff.Row, point selectionPoint) {
	if len(rows) == 0 {
		return
	}
	s.cursor.Row = clampUIDiffInt(point.Row, 0, len(rows)-1)
	s.cursor.Col = s.clampCursorCol(rows, s.cursor.Row, point.Col)
	s.cursorCol = s.cursor.Col
}

func uiDiffChangeTargetRows(rows []diff.Row) []int {
	targets := make([]int, 0)
	inChange := false
	for index, row := range rows {
		if row.Kind == diff.RowAdd || row.Kind == diff.RowDelete {
			if !inChange {
				targets = append(targets, index)
			}
			inChange = true
			continue
		}
		inChange = false
	}
	return targets
}

func (s *uiDiffViewState) halfPageRows() int {
	first, last, ok := s.list.VisibleRange()
	if !ok || last <= first+1 {
		return 1
	}
	return maxInt(1, (last-first)/2)
}

func (s *uiDiffViewState) revealCursorRow() {
	first, last, ok := s.list.VisibleRange()
	if !ok {
		return
	}
	cursorRow := s.cursor.Row
	if s.sideBySide {
		w := s.Widget().(uiDiffView)
		if visualRow, ok := uiDiffSideBySideVisualIndex(w.Rows, s.cursor.Row); ok {
			cursorRow = visualRow
		}
	}
	if cursorRow < first {
		s.list.ScrollToIndex(cursorRow, vui.ScrollAlignStart)
		return
	}
	if cursorRow >= last {
		s.list.ScrollToIndex(cursorRow, vui.ScrollAlignEnd)
		return
	}
	if s.scroll.Attached() {
		metrics := s.scroll.Metrics()
		if offset, ok := s.list.OffsetForIndex(cursorRow); ok {
			if offset < metrics.ScrollOffset {
				s.list.ScrollToIndex(cursorRow, vui.ScrollAlignStart)
				return
			}
			if offset >= metrics.ScrollOffset+metrics.ViewportHeight {
				s.list.ScrollToIndex(cursorRow, vui.ScrollAlignEnd)
			}
		}
	}
}

func (s *uiDiffViewState) revealCommentEditor(rows []diff.Row) {
	if !s.commentEditorActive || s.commentEditorRow < 0 || s.commentEditorRow >= len(rows) {
		return
	}
	row := s.commentEditorRow
	if s.sideBySide {
		visualRow, ok := uiDiffSideBySideVisualIndex(rows, row)
		if !ok {
			return
		}
		row = visualRow
	}
	if s.list.Attached() && s.scroll.Attached() {
		if offset, ok := s.list.OffsetForIndex(row); ok {
			metrics := s.scroll.Metrics()
			commentIndent := uiDiffCodeOffset(rows)
			if s.sideBySide {
				commentIndent = s.documentMetrics(rows).SideBySideGutterWidth() + 3
			}
			bodyWidth := s.commentEditorBodyWidth(commentIndent)
			extent := 1 + uiDiffCommentEditorRowsForWidth(s.commentEditorBody, bodyWidth)
			if offset < metrics.ScrollOffset {
				s.scroll.ScrollToOffset(offset)
				return
			}
			if offset+extent > metrics.ScrollOffset+metrics.ViewportHeight {
				s.scroll.ScrollToOffset(offset + extent - metrics.ViewportHeight)
				return
			}
			return
		}
	}
	s.list.ScrollToIndex(row, vui.ScrollAlignEnd)
}

func uiDiffSideBySideVisualIndex(rows []diff.Row, docRow int) (int, bool) {
	for index, row := range uiDiffSideBySideRows(rows) {
		if rowContainsDocRow(row, docRow) {
			return index, true
		}
	}
	return 0, false
}

func (s *uiDiffViewState) moveCursorCols(rows []diff.Row, delta int) {
	if s.cursor.Row < 0 || s.cursor.Row >= len(rows) {
		return
	}
	s.cursor.Col = uiDiffMoveCursorCol(rows[s.cursor.Row], s.cursor.Col, delta)
	s.cursorCol = s.cursor.Col
	s.revealCursorRow()
	s.revealCursorCol(rows)
	s.SetState(func() {})
}

func uiDiffMoveCursorCol(row diff.Row, col int, delta int) int {
	start, end, ok := uiDiffCodeRangeForRow(row)
	if !ok || delta == 0 {
		return col
	}
	col = uiDiffClampCursorCol(row, col, start, end)
	if delta > 0 {
		for ; delta > 0; delta-- {
			col = uiDiffNextCursorCol(row, col, end)
		}
		return col
	}
	for ; delta < 0; delta++ {
		col = uiDiffPrevCursorCol(row, col, start)
	}
	return col
}

func uiDiffNextCursorCol(row diff.Row, col int, end int) int {
	last := minInt(col, end)
	for _, pos := range uiDiffCursorPositions(row) {
		if pos > col {
			return minInt(pos, end)
		}
		last = minInt(pos, end)
	}
	return last
}

func uiDiffPrevCursorCol(row diff.Row, col int, start int) int {
	prev := start
	for _, pos := range uiDiffCursorPositions(row) {
		if pos >= col {
			return prev
		}
		prev = pos
	}
	return prev
}

func uiDiffClampCursorCol(row diff.Row, col int, start int, end int) int {
	positions := uiDiffCursorPositions(row)
	if len(positions) == 0 {
		return start
	}
	if col <= start {
		return start
	}
	last := start
	for _, pos := range positions {
		if pos > col || pos > end {
			return last
		}
		last = pos
	}
	return last
}

func uiDiffCursorPositions(row diff.Row) []int {
	code := uiDiffRowCode(row)
	if code == "" {
		return []int{0}
	}
	positions := make([]int, 0)
	col := 0
	tabWidth := tabWidthForFile(row.FileName)
	it := uucode.NewGraphemeIterator(code)
	for g, ok := it.Next(); ok; g, ok = it.Next() {
		positions = append(positions, col)
		col += graphemeCellWidthWithTabWidth(code[g.Start:g.End], tabWidth)
	}
	return positions
}

func (s *uiDiffViewState) moveCursorLineStart(rows []diff.Row) {
	if start, _, ok := uiDiffCodeRange(rows, s.cursor.Row); ok {
		s.cursor.Col = start
		s.cursorCol = s.cursor.Col
		s.xScroll = 0
		s.SetState(func() {})
	}
}

func (s *uiDiffViewState) moveCursorLineEnd(rows []diff.Row) {
	if start, end, ok := uiDiffCodeRange(rows, s.cursor.Row); ok {
		s.cursor.Col = maxInt(start, end-1)
		s.cursorCol = s.cursor.Col
		s.revealCursorCol(rows)
		s.SetState(func() {})
	}
}

func (s *uiDiffViewState) revealCursorCol(rows []diff.Row) {
	if s.cursor.Row < 0 || s.cursor.Row >= len(rows) {
		return
	}
	codeWidth := s.codeViewportWidth(rows)
	if codeWidth <= 0 {
		return
	}
	if s.cursor.Col < s.xScroll {
		s.xScroll = s.cursor.Col
	}
	if s.cursor.Col >= s.xScroll+codeWidth {
		s.xScroll = s.cursor.Col - codeWidth + 1
	}
	if s.xScroll < 0 {
		s.xScroll = 0
	}
}

func (s *uiDiffViewState) codeViewportWidth(rows []diff.Row) int {
	if !s.scroll.Attached() {
		return 0
	}
	viewportWidth := s.scroll.Metrics().ViewportWidth
	if s.scroll.Metrics().MaxScrollOffset > 0 {
		viewportWidth--
	}
	if s.sideBySide && s.cursor.Row >= 0 && s.cursor.Row < len(rows) {
		leftWidth, rightStart, _ := uiDiffSideBySidePaneGeometry(viewportWidth)
		paneWidth := leftWidth
		if sideForRow(rows[s.cursor.Row]) == sideRight {
			paneWidth = viewportWidth - rightStart
		}
		return maxInt(0, paneWidth-textCellWidth(uiDiffSideBySideGutter(rows, rows[s.cursor.Row], sideForRow(rows[s.cursor.Row]))))
	}
	return maxInt(0, viewportWidth-uiDiffCodeOffset(rows))
}

func (s *uiDiffViewState) clampCursor(rows []diff.Row) {
	if len(rows) == 0 {
		s.cursor = selectionPoint{}
		s.cursorCol = 0
		return
	}
	s.cursor.Row = uiDiffCursorTargetRow(rows, s.cursor.Row, 1)
	s.cursor.Col = s.clampCursorCol(rows, s.cursor.Row, s.cursor.Col)
	s.cursorCol = s.cursor.Col
}

func (s *uiDiffViewState) clampCursorCol(rows []diff.Row, row int, col int) int {
	start, end, ok := uiDiffCodeRange(rows, row)
	if !ok {
		return 0
	}
	return uiDiffClampCursorCol(rows[row], col, start, end)
}

func clampUIDiffInt(v int, lo int, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func orderedUIDiffInts(a int, b int) (int, int) {
	if a <= b {
		return a, b
	}
	return b, a
}

func uiDiffCodeRange(rows []diff.Row, rowIndex int) (int, int, bool) {
	if rowIndex < 0 || rowIndex >= len(rows) {
		return 0, 0, false
	}
	row := rows[rowIndex]
	return uiDiffCodeRangeForRow(row)
}

func uiDiffCodeRangeForRow(row diff.Row) (int, int, bool) {
	if !selectableDiffRow(row.Kind) {
		return 0, textCellWidth(uiDiffRowCode(row)), true
	}
	if !uiDiffRowUsesGrid(row) {
		return 0, textCellWidth(uiDiffRowCode(row)), true
	}
	return 0, codeCellWidth(row), true
}

func uiDiffRowCode(row diff.Row) string {
	switch row.Kind {
	case diff.RowCommitHeader, diff.RowCommitMeta, diff.RowCommitTrailer:
		if row.Text != "" {
			return row.Text
		}
	}
	if row.Code != "" {
		return row.Code
	}
	return row.Text
}

func splitDiffGutter(row diff.Row) (string, string, string) {
	if row.Gutter == "" && row.Marker == "" {
		return "", "", ""
	}
	fields := stringsFields(row.Gutter)
	marker := row.Marker
	if len(fields) > 0 && isDiffMarker(fields[len(fields)-1]) {
		marker = fields[len(fields)-1]
		fields = fields[:len(fields)-1]
	}
	switch len(fields) {
	case 0:
		return "", "", marker
	case 1:
		if row.Kind == diff.RowDelete {
			return fields[0], "", marker
		}
		return "", fields[0], marker
	default:
		return fields[0], fields[1], marker
	}
}

func isDiffMarker(s string) bool {
	return s == "+" || s == "-"
}

func stringsFields(s string) []string {
	fields := make([]string, 0, 2)
	start := -1
	for i, r := range s {
		if r == ' ' || r == '\t' {
			if start >= 0 {
				fields = append(fields, s[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		fields = append(fields, s[start:])
	}
	return fields
}

func uiGutterStyle(kind diff.RowKind, background vaxis.Color, theme vui.Theme) vaxis.Style {
	style := vaxis.Style{Foreground: theme.MutedForeground, Background: theme.Background}
	if background != 0 {
		style.Background = background
	}
	switch kind {
	case diff.RowAdd:
		style.Foreground = uiDiffChangedGutterForeground(theme, theme.Palette.Green)
	case diff.RowDelete:
		style.Foreground = uiDiffChangedGutterForeground(theme, theme.Palette.Red)
	}
	return style
}

func uiLineNumberGutterStyle(kind diff.RowKind, background vaxis.Color, theme vui.Theme) vaxis.Style {
	style := uiGutterStyle(kind, background, theme)
	if kind == diff.RowAdd {
		style.Foreground = uiDiffAddedLineNumberForeground(theme)
	}
	return style
}

func uiDiffAddedLineNumberForeground(theme vui.Theme) vaxis.Color {
	if theme.Mode == vui.LightTheme {
		return theme.Palette.Green.Tone600
	}
	return theme.Palette.Green.Tone300
}

func uiStyleForDiffRow(kind diff.RowKind, theme vui.Theme) vaxis.Style {
	switch kind {
	case diff.RowFile:
		return vaxis.Style{Foreground: theme.Primary, Background: theme.Background, Attribute: vaxis.AttrBold}
	case diff.RowHunk:
		return vaxis.Style{Foreground: theme.Accent, Background: theme.Background}
	case diff.RowAdd:
		return vaxis.Style{Foreground: theme.Success, Background: theme.Surface}
	case diff.RowDelete:
		return vaxis.Style{Foreground: uiDiffDimChangedForeground(theme, theme.Palette.Red), Background: uiDiffLineBackground(theme, theme.Palette.Red)}
	case diff.RowMeta, diff.RowPreamble, diff.RowNoNewline:
		return vaxis.Style{Foreground: theme.MutedForeground, Background: theme.Background}
	case diff.RowCommitHeader:
		return vaxis.Style{Foreground: theme.Warning, Background: theme.Background, Attribute: vaxis.AttrBold}
	case diff.RowCommitMeta:
		return vaxis.Style{Foreground: theme.Palette.Cyan.Tone500, Background: theme.Background}
	default:
		return vaxis.Style{Foreground: theme.MutedForeground, Background: theme.Background}
	}
}

func uiDiffDimChangedForeground(theme vui.Theme, scale vui.ColorScale) vaxis.Color {
	if theme.Mode == vui.LightTheme {
		return scale.Tone800
	}
	return scale.Tone500
}
