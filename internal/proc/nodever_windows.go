//go:build windows

package proc

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// A Node version manager keeps one folder per version, and a terminal runs
// the one it was told to use, not the one whose name sorts last. fnm's
// Windows install puts the tools directly in each version's folder —
// node-versions\<v>\installation\node.exe, no bin as its POSIX layout has
// (nodever_other.go) — with aliases\default a link to the default
// version's folder. The folders are taken in the order a terminal would
// reach them: the default alias's first, then the highest version.

// fnmBins are the folders of the Node versions fnm keeps in dir on
// Windows: the default alias's folder (aliases\default, a link to the
// version's folder) first, then the versions newest first.
func fnmBins(dir string) []string {
	var bins []string
	// os.Stat follows the link, so one to a folder is a folder; the path
	// itself is kept rather than what it resolves to, since toolIn can
	// search it as it stands.
	if def := filepath.Join(dir, "aliases", "default"); isDirWin(def) {
		bins = append(bins, def)
	}
	root := filepath.Join(dir, "node-versions")
	for _, n := range fnmVersions(root) {
		bins = append(bins, filepath.Join(root, n, "installation"))
	}
	return bins
}

// fnmVersions are the names of the version folders under root that have
// their installation folder (root\<version>\installation), highest version
// first (fnmVersionLess); a name that is no version after every one that
// is.
func fnmVersions(root string) []string {
	es, _ := os.ReadDir(root)
	var names []string
	for _, e := range es {
		if isDirWin(filepath.Join(root, e.Name(), "installation")) {
			names = append(names, e.Name())
		}
	}
	slices.SortStableFunc(names, func(a, b string) int {
		switch {
		case fnmVersionLess(b, a):
			return -1
		case fnmVersionLess(a, b):
			return 1
		}
		return strings.Compare(a, b)
	})
	return names
}

// parseFnmVersion reads v22.12.0 (or 22.12.0, or 22, a partial one) as its
// numbers; false for a name that isn't one.
func parseFnmVersion(s string) ([3]int, int, bool) {
	var v [3]int
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) > 3 || parts[0] == "" {
		return v, 0, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, 0, false
		}
		v[i] = n
	}
	return v, len(parts), true
}

// fnmVersionLess says a is a lower version than b, by number: v9 before
// v22. A name that is no version is lower than one that is.
func fnmVersionLess(a, b string) bool {
	va, _, oka := parseFnmVersion(a)
	vb, _, okb := parseFnmVersion(b)
	if oka != okb {
		return okb
	}
	return oka && slices.Compare(va[:], vb[:]) < 0
}

// isDirWin is whether p is a folder, following a symlink or junction.
func isDirWin(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
