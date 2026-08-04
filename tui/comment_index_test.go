package tui

import (
	"reflect"
	"testing"

	"go.rockorager.dev/comview/diff"
	"go.rockorager.dev/comview/review"
)

func TestBuildCommentIndexResolvesSingleLineComment(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
	}
	drafts := []review.CommentDraft{{Path: "main.go", Line: 2, Side: review.SideRight, Body: "  looks good\nwith detail  "}}

	idx := buildCommentIndex(rows, drafts)
	entries := idx.entries
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.row != 1 {
		t.Fatalf("entry row = %d, want 1", entry.row)
	}
	if entry.draft.Path != "main.go" || entry.draft.Line != 2 || entry.draft.Side != review.SideRight {
		t.Fatalf("entry draft location = %q line %d side %q, want main.go line 2 RIGHT", entry.draft.Path, entry.draft.Line, entry.draft.Side)
	}
	if entry.anchor != rows[1].Review {
		t.Fatalf("entry anchor = %+v, want row review anchor %+v", entry.anchor, rows[1].Review)
	}
	if got := commentPreview(entry.draft.Body); got != "looks good" {
		t.Fatalf("preview = %q, want first trimmed line", got)
	}
	if !reflect.DeepEqual(idx.targetRows(), []int{1}) {
		t.Fatalf("target rows = %#v, want []int{1}", idx.targetRows())
	}
}

func TestBuildCommentIndexResolvesLeftSideComment(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowDelete, Code: "old", Review: review.Anchor{Path: "main.go", Line: 7, Side: review.SideLeft}},
		{Kind: diff.RowAdd, Code: "new", Review: review.Anchor{Path: "main.go", Line: 8, Side: review.SideRight}},
	}
	drafts := []review.CommentDraft{{Path: "main.go", Line: 7, Side: review.SideLeft, Body: "left note"}}

	idx := buildCommentIndex(rows, drafts)
	entries := idx.entries
	if len(entries) != 1 || entries[0].row != 0 || entries[0].draft.Side != review.SideLeft {
		t.Fatalf("entries = %+v, want one left-side entry at row 0", entries)
	}
}

func TestBuildCommentIndexResolvesRangeToEndRowOnce(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}},
		{Kind: diff.RowAdd, Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
		{Kind: diff.RowAdd, Code: "three", Review: review.Anchor{Path: "main.go", Line: 3, Side: review.SideRight}},
	}
	drafts := []review.CommentDraft{{Path: "main.go", StartLine: 1, StartSide: review.SideRight, Line: 3, Side: review.SideRight, Body: "range"}}

	idx := buildCommentIndex(rows, drafts)
	entries := idx.entries
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].row != 2 || entries[0].draft.StartLine != 1 || entries[0].draft.Line != 3 {
		t.Fatalf("entry = %+v, want range targeted to end row 2 with draft line metadata", entries[0])
	}
	if !reflect.DeepEqual(idx.targetRows(), []int{2}) {
		t.Fatalf("target rows = %#v, want []int{2}", idx.targetRows())
	}
}

func TestBuildCommentIndexFallsBackToContainedRangeRow(t *testing.T) {
	rows := []diff.Row{
		{Kind: diff.RowAdd, Code: "two", Review: review.Anchor{Path: "main.go", Line: 2, Side: review.SideRight}},
	}
	drafts := []review.CommentDraft{{Path: "main.go", StartLine: 1, StartSide: review.SideRight, Line: 3, Side: review.SideRight, Body: "range"}}

	idx := buildCommentIndex(rows, drafts)
	entries := idx.entries
	if len(entries) != 1 || entries[0].row != 0 || entries[0].draft.Line != 3 || entries[0].draft.StartLine != 1 {
		t.Fatalf("entries = %+v, want fallback to visible contained row while preserving draft line metadata", entries)
	}
}

func TestBuildCommentIndexSkipsUnmatchedComments(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}}}
	drafts := []review.CommentDraft{
		{Path: "other.go", Line: 1, Side: review.SideRight, Body: "wrong path"},
		{Path: "main.go", Line: 2, Side: review.SideRight, Body: "wrong line"},
	}

	idx := buildCommentIndex(rows, drafts)
	if len(idx.entries) != 0 {
		t.Fatalf("entries = %+v, want unmatched comments skipped", idx.entries)
	}
	if len(idx.targetRows()) != 0 {
		t.Fatalf("target rows = %+v, want none", idx.targetRows())
	}
}

func TestBuildCommentIndexKeepsInputOrderForSameRow(t *testing.T) {
	rows := []diff.Row{{Kind: diff.RowAdd, Code: "one", Review: review.Anchor{Path: "main.go", Line: 1, Side: review.SideRight}}}
	drafts := []review.CommentDraft{
		{Path: "main.go", Line: 1, Side: review.SideRight, Body: "first"},
		{Path: "main.go", Line: 1, Side: review.SideRight, Body: "second"},
	}

	idx := buildCommentIndex(rows, drafts)
	gotDrafts := idx.draftsForRow(0)
	if len(gotDrafts) != 2 {
		t.Fatalf("same-row drafts = %d, want 2", len(gotDrafts))
	}
	if commentPreview(gotDrafts[0].Body) != "first" || commentPreview(gotDrafts[1].Body) != "second" {
		t.Fatalf("same-row preview order = %q, %q; want first, second", commentPreview(gotDrafts[0].Body), commentPreview(gotDrafts[1].Body))
	}
	if !reflect.DeepEqual(idx.targetRows(), []int{0}) {
		t.Fatalf("target rows = %#v, want one deduped row", idx.targetRows())
	}
}

func TestCommentPreviewCompactsWhitespace(t *testing.T) {
	if got := commentPreview("  first\t line  \nsecond line  "); got != "first line" {
		t.Fatalf("preview = %q, want compact first line", got)
	}
	if got := commentPreview("\n\t second line "); got != "second line" {
		t.Fatalf("preview = %q, want first non-empty trimmed line", got)
	}
}
