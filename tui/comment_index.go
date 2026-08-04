package tui

import (
	"sort"
	"strings"

	"go.rockorager.dev/comview/diff"
	"go.rockorager.dev/comview/review"
)

type commentIndex struct {
	entries []commentIndexEntry
}

type commentIndexEntry struct {
	Draft     review.CommentDraft
	Row       int
	Anchor    review.Anchor
	Path      string
	Side      review.Side
	Line      int
	StartLine int
	StartSide review.Side
	Preview   string
}

func buildCommentIndex(rows []diff.Row, drafts []review.CommentDraft) commentIndex {
	entries := make([]commentIndexEntry, 0, len(drafts))
	for _, draft := range drafts {
		row, ok := commentIndexTargetRow(rows, draft)
		if !ok {
			continue
		}
		anchor := rows[row].Review
		entries = append(entries, commentIndexEntry{
			Draft:     draft,
			Row:       row,
			Anchor:    anchor,
			Path:      draft.Path,
			Side:      draft.Side,
			Line:      draft.Line,
			StartLine: draft.StartLine,
			StartSide: draft.StartSide,
			Preview:   commentPreview(draft.Body),
		})
	}
	return commentIndex{entries: entries}
}

func commentIndexTargetRow(rows []diff.Row, draft review.CommentDraft) (int, bool) {
	for rowIndex, row := range rows {
		if reviewDraftEndsAt(draft, row.Review) {
			return rowIndex, true
		}
	}
	for rowIndex, row := range rows {
		if reviewDraftContains(draft, row.Review) {
			return rowIndex, true
		}
	}
	return 0, false
}

func (idx commentIndex) Entries() []commentIndexEntry {
	entries := make([]commentIndexEntry, len(idx.entries))
	copy(entries, idx.entries)
	return entries
}

func (idx commentIndex) EntriesForRow(row int) []commentIndexEntry {
	entries := make([]commentIndexEntry, 0, 1)
	for _, entry := range idx.entries {
		if entry.Row == row {
			entries = append(entries, entry)
		}
	}
	return entries
}

func (idx commentIndex) DraftsForRow(row int) []review.CommentDraft {
	entries := idx.EntriesForRow(row)
	if len(entries) == 0 {
		return nil
	}
	drafts := make([]review.CommentDraft, 0, len(entries))
	for _, entry := range entries {
		drafts = append(drafts, entry.Draft)
	}
	return drafts
}

func (idx commentIndex) TargetRows() []int {
	seen := make(map[int]bool, len(idx.entries))
	targets := make([]int, 0, len(idx.entries))
	for _, entry := range idx.entries {
		if seen[entry.Row] {
			continue
		}
		seen[entry.Row] = true
		targets = append(targets, entry.Row)
	}
	sort.Ints(targets)
	return targets
}

func commentPreview(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			return line
		}
	}
	return ""
}
