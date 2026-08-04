package tui

import (
	"sort"
	"strings"

	"go.rockorager.dev/comview/diff"
	"go.rockorager.dev/comview/review"
)

// commentIndex maps visible draft comments to the diff rows that should render or jump to them.
type commentIndex struct {
	entries []commentIndexEntry
}

// commentIndexEntry keeps the original draft plus its resolved visible target row.
type commentIndexEntry struct {
	draft  review.CommentDraft
	row    int
	anchor review.Anchor
}

// buildCommentIndex indexes drafts that resolve against the current visible rows.
// Drafts with no visible matching anchor are skipped.
func buildCommentIndex(rows []diff.Row, drafts []review.CommentDraft) commentIndex {
	entries := make([]commentIndexEntry, 0, len(drafts))
	for _, draft := range drafts {
		row, ok := commentIndexTargetRow(rows, draft)
		if !ok {
			continue
		}
		entries = append(entries, commentIndexEntry{
			draft:  draft,
			row:    row,
			anchor: rows[row].Review,
		})
	}
	return commentIndex{entries: entries}
}

// commentIndexTargetRow prefers an exact end-anchor match, then any row contained in the draft range.
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

func (idx commentIndex) draftsForRow(row int) []review.CommentDraft {
	drafts := make([]review.CommentDraft, 0, 1)
	for _, entry := range idx.entries {
		if entry.row == row {
			drafts = append(drafts, entry.draft)
		}
	}
	if len(drafts) == 0 {
		return nil
	}
	return drafts
}

func (idx commentIndex) targetRows() []int {
	seen := make(map[int]bool, len(idx.entries))
	targets := make([]int, 0, len(idx.entries))
	for _, entry := range idx.entries {
		if seen[entry.row] {
			continue
		}
		seen[entry.row] = true
		targets = append(targets, entry.row)
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
