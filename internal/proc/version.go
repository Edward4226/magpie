package proc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Version is what the CLI at bin says its version is: the version in the
// package.json of the npm package it is a file of, read rather than run,
// else what bin --version prints (stdout and stderr, ANSI and all), "" when
// it says nothing. A run that says nothing, or takes too long, isn't made
// again until the binary changes (#864, #2693): macOS stops a program
// XProtect flags with a "contains malware" alert on each start, and a CLI
// asked its version every few minutes put one up every time magpie's window
// opened. An npm CLI isn't run at all: its package says the version its
// --version would. An Electron desktop app is never run at all either:
// given --version it starts Chromium instead of printing anything, hanging
// until the timeout and holding its single-instance lock (#2693).
func Version(bin string) string {
	real, err := RealPath(bin)
	if err != nil {
		real = bin
	}
	st, err := os.Stat(real)
	if err != nil {
		return ""
	}
	if v := npmVersion(real); v != "" {
		return v
	}
	if v := shimVersion(real); v != "" {
		return v
	}
	if isElectronApp(real) {
		return electronAppVersion(real)
	}
	stamp := fmt.Sprint(real, st.Size(), st.ModTime().UnixNano())
	versionsFailed.Lock()
	failed := versionsFailed.m[bin] == stamp
	versionsFailed.Unlock()
	if failed {
		return ""
	}
	out, _ := runVersion(bin)
	// a run that said nothing, or was cut off by the timeout, isn't made
	// again until the binary changes: what it says may come from a file
	// beside it that an update changes, but a run that answered is asked
	// again as its caller likes.
	if out == "" {
		versionsFailed.Lock()
		versionsFailed.m[bin] = stamp
		versionsFailed.Unlock()
	}
	return out
}

// versionsFailed is, for each CLI whose run failed, the binary it was.
var versionsFailed = struct {
	sync.Mutex
	m map[string]string
}{m: map[string]string{}}

// runVersion runs bin --version with nothing on its stdin; a var so tests
// can count the runs.
var runVersion = func(bin string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := ProbeContext(ctx, bin, "--version")
	cmd.Stdin = nil // /dev/null: one that would ask something gets nothing
	// Cline checks for an update on every start, --version too, and at exit
	// spawns a detached npm install -g cline@latest the user never asked
	// for (新v学员 on Discord); asking its version mustn't update it.
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "CLINE_NO_AUTO_UPDATE=1")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	// one that says its version and exits non-zero has still said it
	return strings.TrimSpace(string(out)), err
}

// npmVersion is the version of the npm package the file at real is in
// (…/node_modules/@openai/codex/bin/codex.js is in @openai/codex), "" when
// it is in none.
func npmVersion(real string) string {
	for d := filepath.Dir(real); ; d = filepath.Dir(d) {
		up := filepath.Dir(d)
		if up == d {
			return ""
		}
		if filepath.Base(up) == "node_modules" || strings.HasPrefix(filepath.Base(up), "@") && filepath.Base(filepath.Dir(up)) == "node_modules" {
			var p struct {
				Version string `json:"version"`
			}
			b, err := os.ReadFile(filepath.Join(d, "package.json"))
			if err != nil || json.Unmarshal(b, &p) != nil {
				return ""
			}
			return p.Version
		}
	}
}

// shimVersion is the version of the npm package a shim at real starts, ""
// when real is no shim. npm on Windows puts a cline.cmd (and .ps1, and a sh
// script) beside node_modules, not a link into it, so npmVersion can't
// follow it; the shim names %dp0%\node_modules\cline\bin\cline, and that
// package's package.json says the version without running anything.
func shimVersion(real string) string {
	st, err := os.Stat(real)
	if err != nil || st.IsDir() || st.Size() > 64<<10 {
		return ""
	}
	b, err := os.ReadFile(real)
	if err != nil {
		return ""
	}
	text := strings.ReplaceAll(string(b), `\`, "/")
	const nm = "/node_modules/"
	i := strings.Index(text, nm)
	if i < 0 {
		return ""
	}
	parts := strings.SplitN(text[i+len(nm):], "/", 3)
	if len(parts) < 2 {
		return ""
	}
	pkg := parts[0]
	if strings.HasPrefix(pkg, "@") {
		pkg += "/" + parts[1]
	}
	if pkg == "" || strings.ContainsAny(pkg, " \t\r\n\"'%$") {
		return ""
	}
	var p struct {
		Version string `json:"version"`
	}
	b, err = os.ReadFile(filepath.Join(filepath.Dir(real), "node_modules", filepath.FromSlash(pkg), "package.json"))
	if err != nil || json.Unmarshal(b, &p) != nil {
		return ""
	}
	return p.Version
}

// isElectronApp reports whether the binary at real belongs to an Electron
// desktop app: electron-builder leaves chrome-sandbox, resources.pak or
// resources/app.asar beside it. Such a binary must never be started to ask
// its version: given --version it starts Chromium instead of printing
// anything, hanging until it is killed and holding its single-instance
// lock, which breaks a window the user opens themselves (#2693).
func isElectronApp(real string) bool {
	dir := filepath.Dir(real)
	for _, marker := range []string{"chrome-sandbox", "resources.pak", filepath.Join("resources", "app.asar")} {
		st, err := os.Stat(filepath.Join(dir, marker))
		if err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}

// electronAppVersion is the version of the Electron desktop app at real,
// read from beside it rather than by running it: OpenCode Desktop writes
// its bundled CLI's version to resources/opencode-cli.version and reads it
// the same way itself (#2693). "" when it can't be told; a failed read
// means unknown, never "no version". Other Electron apps have no such
// file, so this changes nothing for them.
func electronAppVersion(real string) string {
	b, err := os.ReadFile(filepath.Join(filepath.Dir(real), "resources", "opencode-cli.version"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
