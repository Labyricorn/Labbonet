package main

import (
	"fmt"
	"os"
	"os/exec"
)

// openBrowser opens the default browser at url. Best-effort: Phase 1 only
// targets Windows, and a failure here is not fatal (the launch URL is
// already printed).
func openBrowser(url string) {
	cmd := exec.Command("cmd", "/c", "start", "", url)
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "labbonet: could not open browser automatically:", err)
	}
}
