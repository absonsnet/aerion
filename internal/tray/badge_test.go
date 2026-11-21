package tray

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

// syntheticBase returns a small known icon: mint envelope-ish square, so
// pixel assertions do not depend on the real app icon file.
func syntheticBase(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	mint := color.RGBA{R: 0xA7, G: 0xF3, B: 0xD0, A: 0xFF}
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			img.Set(x, y, mint)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode synthetic base: %v", err)
	}
	return buf.Bytes()
}

func isBadgeRed(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	// colour.RGBA() returns 16-bit values.
	return r > 0xC000 && g < 0x8000 && b < 0x8000
}

func isNearWhite(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return r > 0xEB00 && g > 0xEB00 && b > 0xEB00
}

func TestFormatBadgeCount(t *testing.T) {
	cases := []struct {
		count int
		want  string
	}{
		{0, ""},
		{-3, ""},
		{1, "1"},
		{5, "5"},
		{99, "99"},
		{100, "99+"},
		{150, "99+"},
		{1000, "99+"},
	}
	for _, tc := range cases {
		if got := FormatBadgeCount(tc.count); got != tc.want {
			t.Errorf("FormatBadgeCount(%d) = %q, want %q", tc.count, got, tc.want)
		}
	}
}

func TestBadgeTooltip(t *testing.T) {
	if got := BadgeTooltip(0); got != "Daak" {
		t.Errorf("BadgeTooltip(0) = %q, want %q", got, "Daak")
	}
	if got := BadgeTooltip(5); got != "Daak — 5 unread" {
		t.Errorf("BadgeTooltip(5) = %q, want %q", got, "Daak — 5 unread")
	}
}

func TestRenderBadgedIconPNG(t *testing.T) {
	base := syntheticBase(t)

	// Zero count returns the base bytes unchanged.
	plain, err := RenderBadgedIconPNG(base, 0)
	if err != nil {
		t.Fatalf("RenderBadgedIconPNG(0): %v", err)
	}
	if !bytes.Equal(plain, base) {
		t.Error("RenderBadgedIconPNG(0) should return the base bytes unchanged")
	}

	badged, err := RenderBadgedIconPNG(base, 5)
	if err != nil {
		t.Fatalf("RenderBadgedIconPNG(5): %v", err)
	}
	img, err := png.Decode(bytes.NewReader(badged))
	if err != nil {
		t.Fatalf("decode badged PNG: %v", err)
	}
	if img.Bounds().Dx() != 256 || img.Bounds().Dy() != 256 {
		t.Errorf("badged size = %dx%d, want 256x256", img.Bounds().Dx(), img.Bounds().Dy())
	}
	if bytes.Equal(badged, base) {
		t.Error("badged PNG should differ from the base")
	}
	// The badge region must be mostly red (the badge) with white digit
	// strokes in it. Single pixels are not asserted: a digit stroke can
	// legitimately cover any one point inside the badge.
	w := float64(256)
	cx, cy := int(0.81*w), int(0.18*w)
	red, white := 0, 0
	for y := cy - 45; y <= cy+45; y++ {
		for x := cx - 45; x <= cx+45; x++ {
			dx, dy := float64(x-cx), float64(y-cy)
			if dx*dx+dy*dy > 45*45 {
				continue
			}
			if isBadgeRed(img.At(x, y)) {
				red++
			}
			if isNearWhite(img.At(x, y)) {
				white++
			}
		}
	}
	if red < 1000 {
		t.Errorf("only %d red pixels in the badge region, want >= 1000", red)
	}
	if white < 20 {
		t.Errorf("only %d near-white pixels in the badge, want >= 20 (digit strokes)", white)
	}

	// "99+" is a pill, wider than the circle: the leftmost red pixel on
	// the badge row must sit further left for count 150 than for count 5.
	leftmostRed := func(im image.Image) int {
		for x := 0; x < 256; x++ {
			if isBadgeRed(im.At(x, cy)) {
				return x
			}
		}
		return -1
	}
	img5, _ := png.Decode(bytes.NewReader(badged))
	left5 := leftmostRed(img5)
	pill, err := RenderBadgedIconPNG(base, 150)
	if err != nil {
		t.Fatalf("RenderBadgedIconPNG(150): %v", err)
	}
	img150, err := png.Decode(bytes.NewReader(pill))
	if err != nil {
		t.Fatalf("decode 99+ PNG: %v", err)
	}
	left150 := leftmostRed(img150)
	if left150 < 0 || left5 < 0 {
		t.Fatalf("no red pixel on the badge row (5: %d, 150: %d)", left5, left150)
	}
	if left150 > left5-10 {
		t.Errorf("99+ pill left edge = %d, want at least 10px left of the count-5 circle edge %d", left150, left5)
	}

	// Non-PNG input is an error.
	if _, err := RenderBadgedIconPNG([]byte("not a png"), 5); err == nil {
		t.Error("RenderBadgedIconPNG(non-PNG) should error")
	}
}

func TestRenderBadgedIconICO(t *testing.T) {
	base := syntheticBase(t)
	ico, err := RenderBadgedIconICO(base, 5)
	if err != nil {
		t.Fatalf("RenderBadgedIconICO(5): %v", err)
	}
	if len(ico) < 22 {
		t.Fatalf("ICO too short: %d bytes", len(ico))
	}
	if ico[0] != 0 || ico[1] != 0 || ico[2] != 1 || ico[3] != 0 || ico[4] != 1 || ico[5] != 0 {
		t.Errorf("bad ICO header: %v", ico[:6])
	}
	if ico[6] != 0 || ico[7] != 0 {
		t.Errorf("ICO width/height bytes = %d/%d, want 0/0 (256)", ico[6], ico[7])
	}
	size := int(binary.LittleEndian.Uint32(ico[14:18]))
	off := int(binary.LittleEndian.Uint32(ico[18:22]))
	if off != 22 {
		t.Errorf("ICO data offset = %d, want 22", off)
	}
	if off+size != len(ico) {
		t.Errorf("ICO data end = %d, want file length %d", off+size, len(ico))
	}
	img, err := png.Decode(bytes.NewReader(ico[off:]))
	if err != nil {
		t.Fatalf("embedded PNG does not decode: %v", err)
	}
	if img.Bounds().Dx() != 256 || img.Bounds().Dy() != 256 {
		t.Errorf("embedded PNG size = %dx%d, want 256x256", img.Bounds().Dx(), img.Bounds().Dy())
	}
	// Zero count still produces a valid ICO wrapping the plain icon.
	ico0, err := RenderBadgedIconICO(base, 0)
	if err != nil {
		t.Fatalf("RenderBadgedIconICO(0): %v", err)
	}
	off0 := int(binary.LittleEndian.Uint32(ico0[18:22]))
	if _, err := png.Decode(bytes.NewReader(ico0[off0:])); err != nil {
		t.Errorf("zero-count ICO embedded PNG does not decode: %v", err)
	}
}

func TestRenderBadgeOnly(t *testing.T) {
	if img, err := RenderBadgeOnly(0, 64); err != nil || img != nil {
		t.Errorf("RenderBadgeOnly(0) = %v, %v; want nil, nil", img, err)
	}
	img, err := RenderBadgeOnly(5, 64)
	if err != nil {
		t.Fatalf("RenderBadgeOnly(5): %v", err)
	}
	if img.Bounds().Dx() != 64 || img.Bounds().Dy() != 64 {
		t.Fatalf("badge-only size = %dx%d, want 64x64", img.Bounds().Dx(), img.Bounds().Dy())
	}
	// Near the top of the badge, clear of the digit strokes.
	if !isBadgeRed(img.At(32, 8)) {
		t.Errorf("badge-only top = %v, want badge red", img.At(32, 8))
	}
	_, _, _, a := img.At(0, 0).RGBA()
	if a != 0 {
		t.Errorf("badge-only corner alpha = %d, want 0 (transparent background)", a)
	}
}

func TestTrayBadgeDedupe(t *testing.T) {
	tr := New()
	pushes := 0
	old := badgePushHook
	badgePushHook = func(count int) { pushes++ }
	defer func() { badgePushHook = old }()

	tr.SetBadge(5)
	if pushes != 1 {
		t.Fatalf("after SetBadge(5): pushes = %d, want 1", pushes)
	}
	tr.SetBadge(5)
	if pushes != 1 {
		t.Fatalf("after repeat SetBadge(5): pushes = %d, want 1 (no change, no push)", pushes)
	}
	tr.SetBadge(7)
	if pushes != 2 {
		t.Fatalf("after SetBadge(7): pushes = %d, want 2", pushes)
	}
	tr.SetBadge(0)
	if pushes != 3 {
		t.Fatalf("after SetBadge(0): pushes = %d, want 3", pushes)
	}
	if tr.badgeLastCount != 0 {
		t.Fatalf("badgeLastCount = %d, want 0", tr.badgeLastCount)
	}

	tr.SetBadgeEnabled(false)
	if pushes != 4 {
		t.Fatalf("after disable: pushes = %d, want 4 (clear push)", pushes)
	}
	tr.SetBadge(7)
	if pushes != 4 {
		t.Fatalf("after SetBadge(7) while disabled: pushes = %d, want 4 (no push)", pushes)
	}
	tr.SetBadgeEnabled(true)
	if pushes != 4 {
		t.Fatalf("after re-enable: pushes = %d, want 4 (no push yet)", pushes)
	}
	tr.SetBadge(7)
	if pushes != 5 {
		t.Fatalf("after SetBadge(7) following re-enable: pushes = %d, want 5 (forced push)", pushes)
	}
	tr.SetBadgeEnabled(true) // already enabled: no-op
	if pushes != 5 {
		t.Fatalf("after redundant enable: pushes = %d, want 5", pushes)
	}
}

// TestRenderBadgeSamples writes badged icons from the real app icon for
// a visual check (menu-bar size, light and dark backgrounds). It always
// passes on decode; the looking is done by a human.
func TestRenderBadgeSamples(t *testing.T) {
	base, err := os.ReadFile("../../build/appicon.png")
	if err != nil {
		t.Skipf("real app icon not available: %v", err)
	}
	dir := "/tmp/badge-samples"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir samples: %v", err)
	}
	for _, count := range []int{5, 99, 150} {
		out, err := RenderBadgedIconPNG(base, count)
		if err != nil {
			t.Fatalf("RenderBadgedIconPNG(%d): %v", count, err)
		}
		name := dir + "/badged-" + FormatBadgeCount(count) + ".png"
		if count == 150 {
			name = dir + "/badged-99plus.png"
		}
		if err := os.WriteFile(name, out, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		if _, err := png.Decode(bytes.NewReader(out)); err != nil {
			t.Fatalf("sample %s does not decode: %v", name, err)
		}
	}
}
