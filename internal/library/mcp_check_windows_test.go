//go:build windows

package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/proc"
)

// A bare command is the program a terminal would run: stdioCommand resolves
// it through proc.FindTool (PATH first, then the folders a user's tools are
// installed in), so npx is the fnm one when its folder is on PATH. An
// absolute path is taken as it is. Only the helper is tested; no server is
// started.
func TestStdioCommandWindows(t *testing.T) {
	dir := t.TempDir()
	npx := filepath.Join(dir, "npx.cmd")
	if err := os.WriteFile(npx, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	proc.Forget()

	// Windows paths are case-insensitive: the same file whatever Go appends
	// from PATHEXT.
	if got := stdioCommand("npx"); !strings.EqualFold(got, npx) {
		t.Fatalf("stdioCommand(npx) = %q, want %q", got, npx)
	}
	const abs = `C:\Windows\System32\cmd.exe`
	if got := stdioCommand(abs); got != abs {
		t.Fatalf("stdioCommand(%q) = %q, want it as it is", abs, got)
	}
}
