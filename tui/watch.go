package tui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	vui "go.rockorager.dev/vaxis/ui"

	"go.rockorager.dev/comview/diff"
	"go.rockorager.dev/comview/internal/terminal"
	"go.rockorager.dev/comview/review"
)

const defaultWatchInterval = 750 * time.Millisecond

// RunWatch starts comview in watch mode. The command is rerun periodically and
// the displayed diff is refreshed whenever the command output changes.
func RunWatch(command []string) error {
	if len(command) == 0 {
		command = []string{"git", "diff"}
	}
	return runUIWatch(command)
}

type uiWatchView struct {
	Command      []string
	Config       Config
	Review       review.CommentFile
	File         string
	WorkTreeRoot string
}

func (w uiWatchView) CreateState() vui.State {
	return &uiWatchViewState{}
}

type uiWatchViewState struct {
	vui.StateBase
	started  bool
	rows     []diff.Row
	message  string
	lastHash [sha256.Size]byte
	haveHash bool
}

func (s *uiWatchViewState) Build(ctx vui.BuildContext) vui.Widget {
	w := s.Widget().(uiWatchView)
	if !s.started {
		s.started = true
		s.message = fmt.Sprintf("Watching: %s", strings.Join(w.Command, " "))
		runtime := ctx.Runtime()
		go s.watch(runtime, w.Command, defaultWatchInterval)
	}
	return s.diffRoot(w)
}

func (s *uiWatchViewState) diffRoot(w uiWatchView) uiDiffView {
	root := uiDiffRootWithReviewFileAndBindings(s.rows, w.Config.Wrap, w.Review.Comments, w.File, true, newBindings(w.Config.Keybindings)).(uiDiffView)
	root.WorkTreeRoot = w.WorkTreeRoot
	root.EmptyMessage = "No changes."
	root.EmptyHint = fmt.Sprintf("Watching: %s", strings.Join(w.Command, " "))
	root.InitialStatus = s.message
	return root
}

func (s *uiWatchViewState) watch(runtime vui.Runtime, command []string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		output, err := runWatchCommand(context.Background(), command)
		if s.applyWatchResult(output, err, time.Now()) {
			runtime.Dispatch(func() {
				s.SetState(func() {})
			})
		}
		<-ticker.C
	}
}

func (s *uiWatchViewState) applyWatchResult(output string, err error, now time.Time) bool {
	message := ""
	rows := s.rows
	hashInput := ""
	if err != nil {
		message = fmt.Sprintf("Watch command failed: %v", err)
		hashInput = "error:" + message
	} else if parsed, parseErr := rowsForInput(output); parseErr != nil {
		message = fmt.Sprintf("Could not parse diff: %v", parseErr)
		hashInput = "parse:" + message
	} else {
		rows = parsed
		message = fmt.Sprintf("Updated %s", now.Format("15:04:05"))
		if len(rows) == 0 {
			message = fmt.Sprintf("No changes %s", now.Format("15:04:05"))
		}
		hashInput = "output:" + output
	}
	hash := sha256.Sum256([]byte(hashInput))
	if s.haveHash && hash == s.lastHash {
		return false
	}
	s.lastHash = hash
	s.haveHash = true
	s.rows = rows
	s.message = message
	return true
}

func runUIWatch(command []string) error {
	cfg := loadConfig()
	commentPath := cfg.CommentFile
	if commentPath == "" {
		commentPath = review.DefaultFilePath
	}
	commentFile, err := review.LoadFile(commentPath)
	if err != nil {
		return err
	}
	root := uiWatchView{Command: command, Config: cfg, Review: commentFile, File: commentPath, WorkTreeRoot: gitWorkTreeRoot()}
	if cfg.Theme != "" {
		if t, ok := ThemeByName(cfg.Theme); ok {
			return vui.Run(root, vui.WithTheme(uiThemeFromBaseColors(t.Colors)))
		}
	}
	return vui.Run(root)
}

func runWatchCommand(ctx context.Context, command []string) (string, error) {
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err == nil {
		return terminal.PrintableANSIOutput(bytes.NewReader(output)), nil
	}

	message := strings.TrimSpace(terminal.PrintableANSIOutput(&stderr))
	if message == "" {
		message = err.Error()
	}
	return "", errors.New(message)
}
