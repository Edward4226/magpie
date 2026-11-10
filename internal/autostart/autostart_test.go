package autostart

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

// On, the system's record names this program and the tray; off, it's gone,
// and turning it off again is no error.
func TestSet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Run key is the real user's")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", home+"/.config")
	// Linux's system folders, a package's entry in them: the test's own
	t.Setenv("XDG_CONFIG_DIRS", home+"/etc/xdg")
	t.Setenv("MAGPIE_EXECUTABLE", "")
	t.Setenv("APPIMAGE", "/opt/my apps/magpie.AppImage")
	if Enabled() != Off {
		t.Fatal("on before it was turned on")
	}
	if err := Set(true); err != nil {
		t.Fatal(err)
	}
	if Enabled() == Off {
		t.Fatal("not on after Set(true)")
	}
	path := record()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	arg := Arg
	if runtime.GOOS == "android" {
		arg = "serve"
	}
	for _, want := range []string{"/opt/my apps/magpie.AppImage", arg} {
		if !strings.Contains(string(b), want) {
			t.Errorf("%s lacks %q:\n%s", path, want, b)
		}
	}
	if err := Set(false); err != nil || Enabled() != Off {
		t.Fatalf("still on after Set(false): %v", err)
	}
	if err := Set(false); err != nil {
		t.Fatal(err)
	}
}

// A packager names the launcher to start, which for Nix and Guix is a
// wrapper with its own environment and not this process's executable. It is
// started as written, path and all.
func TestSelfUsesPackagerExecutable(t *testing.T) {
	t.Setenv("APPIMAGE", "")
	const wrapper = "/nix/store/example/bin/magpie app"
	t.Setenv("MAGPIE_EXECUTABLE", wrapper)
	got, err := self()
	if err != nil {
		t.Fatal(err)
	}
	if got != wrapper {
		t.Fatalf("self() = %q, want the packager's %q", got, wrapper)
	}
}
