//go:build windows

package proc

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// layFnm writes fnm's layout at root as its Windows installer leaves it:
// aliases\default a folder of its own (fnm makes it a link to the default
// version's folder), and each version's tools directly in
// node-versions\<v>\installation, with no bin.
func layFnm(t *testing.T, root string, versions ...string) {
	t.Helper()
	dirs := []string{filepath.Join(root, "aliases", "default")}
	for _, v := range versions {
		dirs = append(dirs, filepath.Join(root, "node-versions", v, "installation"))
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "npx.cmd"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// fnm's default alias comes first, then its versions newest first, by
// number: v24 before v20, and v20 before v9 — which plain string order
// gets backwards.
func TestFnmBinsWindows(t *testing.T) {
	root := filepath.Join(t.TempDir(), "fnm")
	layFnm(t, root, "v20.18.0", "v24.18.0", "v9.11.2")
	want := []string{
		filepath.Join(root, "aliases", "default"),
		filepath.Join(root, "node-versions", "v24.18.0", "installation"),
		filepath.Join(root, "node-versions", "v20.18.0", "installation"),
		filepath.Join(root, "node-versions", "v9.11.2", "installation"),
	}
	if got := fnmBins(root); !slices.Equal(got, want) {
		t.Fatalf("fnmBins(%q) = %q, want %q", root, got, want)
	}
}

// A version folder with no installation folder isn't one of fnm's tools.
func TestFnmBinsSkipsUninstalled(t *testing.T) {
	root := filepath.Join(t.TempDir(), "fnm")
	layFnm(t, root, "v20.18.0")
	if err := os.MkdirAll(filepath.Join(root, "node-versions", "v24.18.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(root, "aliases", "default"),
		filepath.Join(root, "node-versions", "v20.18.0", "installation"),
	}
	if got := fnmBins(root); !slices.Equal(got, want) {
		t.Fatalf("fnmBins = %q, want %q", got, want)
	}
}

// UserBinDirs has fnm's folders when APPDATA says where its root is — as
// its installer leaves it — and when FNM_DIR overrides it, so a bare npx a
// terminal gets from fnm is found.
func TestUserBinDirsFnmWindows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("FNM_DIR", "")

	under := func(root string, want []string) {
		t.Helper()
		var got []string
		for _, d := range UserBinDirs() {
			if strings.HasPrefix(d, root) {
				got = append(got, d)
			}
		}
		if !slices.Equal(got, want) {
			t.Fatalf("UserBinDirs under %q = %q, want %q", root, got, want)
		}
	}

	root := filepath.Join(home, "fnm")
	layFnm(t, root, "v20.18.0", "v24.18.0")
	under(root, []string{
		filepath.Join(root, "aliases", "default"),
		filepath.Join(root, "node-versions", "v24.18.0", "installation"),
		filepath.Join(root, "node-versions", "v20.18.0", "installation"),
	})

	moved := filepath.Join(home, "elsewhere", "fnm")
	layFnm(t, moved, "v22.12.0")
	t.Setenv("FNM_DIR", moved)
	under(moved, []string{
		filepath.Join(moved, "aliases", "default"),
		filepath.Join(moved, "node-versions", "v22.12.0", "installation"),
	})
}
