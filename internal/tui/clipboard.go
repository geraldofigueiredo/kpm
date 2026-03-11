package tui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// copyToClipboard writes text to the system clipboard.
// Supports macOS (pbcopy) and Linux (xclip).
func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		cmd = exec.Command("xclip", "-selection", "clipboard")
	default:
		return fmt.Errorf("clipboard not supported on %s", runtime.GOOS)
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
