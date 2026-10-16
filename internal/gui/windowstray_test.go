//go:build !nogui

package gui

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

// Windows scales a tray icon into its 16×16 slot, and icon.png is 64×64 with
// a 6px clear border on each side, so the bird drawn in it lands about 13×13
// and looks small (#2404). windowsTrayIcon trims that border at the caller
// (app.go), leaving the app icon's own code alone: the trimmed drawing is the
// 52×52 content box, and its opaque content box then meets all four edges, so
// no clear row or column is left. The old path passed icon.png through whole,
// so the bounds here catch a regression.
func TestWindowsTrayIconTrimsClearBorder(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(windowsTrayIcon(appIcon)))
	if err != nil {
		t.Fatal(err)
	}
	want := image.Rect(0, 0, 52, 52)
	if got := img.Bounds(); got != want {
		t.Errorf("windows tray icon bounds %v, want %v: the 64×64 app icon's 6px clear border should be trimmed", got, want)
	}
	// the trim is tight only if some opaque pixel reaches each edge: measure
	// the opaque content box and check it against the canvas. The drawing is
	// not a filled square, so its own transparent-box corners are clear too,
	// and checking the corners would call a correct trim wrong.
	box := image.Rectangle{Min: img.Bounds().Max, Max: img.Bounds().Min}
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
				box = box.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	if box != img.Bounds() {
		t.Errorf("windows tray icon content box %v, want the canvas %v: the trim should leave no clear row or column", box, img.Bounds())
	}
}

// The tray shows what it is handed, so a decode or encode magpie can't do
// must come back as the bytes given, not an empty or nil icon the slot would
// draw as nothing.
func TestWindowsTrayIconPassesFailuresThrough(t *testing.T) {
	in := []byte("nope")
	if got := windowsTrayIcon(in); !bytes.Equal(got, in) {
		t.Errorf("windowsTrayIcon(%q) = %q, want it back unchanged", in, got)
	}
}

// Trimming only reads the global appIcon and must return new bytes: writing
// into the embedded slice would corrupt the icon app.go hands the system and
// every later caller.
func TestWindowsTrayIconLeavesAppIcon(t *testing.T) {
	before := append([]byte(nil), appIcon...)
	windowsTrayIcon(appIcon)
	if len(appIcon) != len(before) || !bytes.Equal(appIcon, before) {
		t.Errorf("windowsTrayIcon changed appIcon: %d bytes, want the original %d", len(appIcon), len(before))
	}
}
