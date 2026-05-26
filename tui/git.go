package tui

import (
	"os/exec"
	"strings"
)

func gitWorkTreeRoot() string {
	output, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
