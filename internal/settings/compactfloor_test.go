package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/filememo"
)

// A compaction threshold typed as a bare number below CompactFloor is the
// unit misread (#2487: the box says 272k, so a user types 400 meaning 400K,
// and 400 is read as 400 tokens). It is read as not set, so no agent is
// told to compact every turn; a file already written with one is read the
// same way. The file is written by hand here, past Save, which is what a
// magpie before the fix left behind.
func TestCompactUnderFloorReadsAsUnset(t *testing.T) {
	write := func(body string) {
		t.Helper()
		h := t.TempDir()
		t.Setenv("HOME", h)
		t.Setenv("USERPROFILE", h)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
		if err := os.MkdirAll(Dir(), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(Path(), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		filememo.Forget()
	}

	write(`{"compactAt":400}`)
	if s := Load(); s.CompactAt != 0 || s.Compact() != WorkingWindow {
		t.Fatalf("a bare 400 was kept at %d, compacting at %d; want unset and %d",
			s.CompactAt, s.Compact(), WorkingWindow)
	}

	write(`{"modelCompacts":{"a/m":400}}`)
	if s := Load(); len(s.ModelCompacts) != 0 {
		t.Fatalf("a bare 400 model threshold survived: %v", s.ModelCompacts)
	}
}
