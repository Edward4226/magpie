package proc

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// #2685: an MCP server started by fnm's npx.cmd, by its full path, runs
// the package it fetched, which runs `node` by name. A terminal's fnm put
// node's folder on PATH; magpie's PATH, the registry's, didn't have it,
// and the server exited 1 with cmd's "'node' is not recognized as an
// internal or external command, operable program or batch file." After
// UserPath the child finds fnm's node, and FindTool finds a bare npx.
func TestUserPathFnmNode(t *testing.T) {
	home := t.TempDir()
	appdata := filepath.Join(home, "AppData", "Roaming")
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", appdata)
	t.Setenv("FNM_DIR", "")
	sys := filepath.Join(os.Getenv("SystemRoot"), "System32")
	t.Setenv("PATH", sys)
	root := filepath.Join(appdata, "fnm")
	inst := filepath.Join(root, "node-versions", "v24.19.0", "installation")
	if err := os.MkdirAll(inst, 0o755); err != nil {
		t.Fatal(err)
	}
	// npx's package bin, as npm's cmd shim runs it: node from PATH
	npx := filepath.Join(inst, "npx.cmd")
	for p, s := range map[string]string{npx: "@echo off\r\nnode %*\r\n", filepath.Join(inst, "node.cmd"): "@echo fnm node %*\r\n"} {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "aliases", "default")
	if err := os.MkdirAll(filepath.Dir(alias), 0o755); err != nil {
		t.Fatal(err)
	}
	// a junction, as fnm makes it
	if out, err := Command("cmd", "/c", "mklink", "/J", alias, inst).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v %s", err, out)
	}
	run := func() (string, error) {
		cmd := Command(npx, "-y", "chrome-devtools-mcp@latest")
		cmd.Dir = t.TempDir() // cmd looks in its folder before PATH
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	if out, err := run(); err == nil {
		t.Fatalf("before UserPath node was found already (%q): the test proves nothing", out)
	}

	UserPath()
	if dirs := filepath.SplitList(os.Getenv("PATH")); !slices.Contains(dirs, alias) {
		t.Fatalf("PATH after UserPath = %q, want fnm's default alias %q on it", dirs, alias)
	}
	if out, err := run(); err != nil || out != "fnm node -y chrome-devtools-mcp@latest" {
		t.Fatalf("npx.cmd by its full path: %v %q, want fnm's node run", err, out)
	}
	t.Setenv("PATH", sys)
	Forget()
	if got := FindTool("npx"); !strings.EqualFold(got, filepath.Join(alias, "npx.cmd")) {
		t.Fatalf("FindTool(npx) = %q, want the default alias's %q", got, filepath.Join(alias, "npx.cmd"))
	}
}

// #2735: mise's Node on Windows installs npm's global packages into the
// version folder itself (installs\node\<version>\pi.cmd), with no bin under
// it and no shim: `npm i -g` from a mise shell left magpie's shims folder
// without one, so every Pi row said "CLI not found" while a terminal ran it.
// UserBinDirs names each installed version, newest first, so FindTool finds
// the .cmd. MISE_DATA_DIR and an absolute XDG_DATA_HOME move mise's root, as
// they already did for its shims.
func TestUserBinDirsMiseNode(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("FNM_DIR", "")
	t.Setenv("APPDATA", filepath.Join(t.TempDir(), "Roaming"))

	for _, tc := range []struct {
		name string
		xdg  bool
		mise bool
	}{
		{name: "LOCALAPPDATA"},
		{name: "MISE_DATA_DIR", mise: true},
		{name: "XDG_DATA_HOME", xdg: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			local := filepath.Join(t.TempDir(), "Local")
			t.Setenv("LOCALAPPDATA", local)
			var xdg, data string
			switch {
			case tc.mise:
				data = filepath.Join(t.TempDir(), "mise-data")
			case tc.xdg:
				xdg = filepath.Join(t.TempDir(), "xdg")
			}
			t.Setenv("XDG_DATA_HOME", xdg)
			t.Setenv("MISE_DATA_DIR", data)
			root := filepath.Join(local, "mise")
			switch {
			case data != "":
				root = data
			case xdg != "":
				root = filepath.Join(xdg, "mise")
			}
			node := filepath.Join(root, "installs", "node")
			newest := filepath.Join(node, "24.13.0")
			older := filepath.Join(node, "22.1.0")
			for _, d := range []string{newest, older} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			pi := filepath.Join(newest, "pi.cmd")
			if err := os.WriteFile(pi, []byte("@echo pi\r\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			Forget()
			dirs := UserBinDirs()
			i := slices.Index(dirs, newest)
			if i < 0 {
				t.Fatalf("UserBinDirs = %q, want mise's Node %q among them", dirs, newest)
			}
			if j := slices.Index(dirs, older); j < 0 || j < i {
				t.Fatalf("UserBinDirs = %q, want %q before %q (newest first)", dirs, newest, older)
			}
			if got := FindTool("pi"); !strings.EqualFold(got, pi) {
				t.Fatalf("FindTool(pi) = %q, want %q", got, pi)
			}
		})
	}
}
