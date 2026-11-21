// Unread badge rendering and formatting.
//
// The badge is the unified-inbox unread count shown on the OS chrome:
// the macOS Dock icon (native badgeLabel), the Windows taskbar button
// (overlay icon) and the tray / menu-bar icon (the number drawn onto the
// tray icon itself). The platform setters live in badge_darwin.go,
// badge_windows.go and badge_other.go; this file holds the shared pieces:
// the label format, the tooltip text, and the badge drawing (a red circle
// — a pill for "99+" — with a white number, drawn with the embedded Go
// Regular font so no font files are needed at runtime).
package tray

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sync"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// badgeRed is the badge fill: system red, readable on the mint envelope
// and on light and dark menu bars alike.
var badgeRed = color.RGBA{R: 0xFF, G: 0x3B, B: 0x30, A: 0xFF}

// badgeText is the number colour.
var badgeText = color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}

// badgeMaxCount is the largest count shown as digits; larger counts show
// as "99+".
const badgeMaxCount = 99

// FormatBadgeCount renders the unread count as the badge label: "" for
// none, the number up to 99, "99+" beyond that.
func FormatBadgeCount(count int) string {
	if count <= 0 {
		return ""
	}
	if count > badgeMaxCount {
		return "99+"
	}
	return fmt.Sprintf("%d", count)
}

// BadgeTooltip is the tray tooltip for a count: plain "Daak" at zero,
// "Daak — N unread" otherwise. The tiny menu-bar number is hard to read,
// so the tooltip carries the exact count too.
func BadgeTooltip(count int) string {
	if count <= 0 {
		return "Daak"
	}
	return fmt.Sprintf("Daak — %d unread", count)
}

// badgeFont is the embedded Go Regular font, parsed once.
var (
	badgeFontOnce sync.Once
	badgeFont     *opentype.Font
	badgeFontErr  error
)

func badgeTypeFace(size float64) (font.Face, error) {
	badgeFontOnce.Do(func() {
		badgeFont, badgeFontErr = opentype.Parse(goregular.TTF)
	})
	if badgeFontErr != nil {
		return nil, badgeFontErr
	}
	return opentype.NewFace(badgeFont, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
}

// inRoundedRect reports whether (px, py) lies inside the rounded rect
// [x0,y0]-[x1,y1] with corner radius r.
func inRoundedRect(px, py, x0, y0, x1, y1, r float64) bool {
	if px < x0 || px > x1 || py < y0 || py > y1 {
		return false
	}
	ix0, iy0, ix1, iy1 := x0+r, y0+r, x1-r, y1-r
	dx := 0.0
	if px < ix0 {
		dx = ix0 - px
	} else if px > ix1 {
		dx = px - ix1
	}
	dy := 0.0
	if py < iy0 {
		dy = iy0 - py
	} else if py > iy1 {
		dy = py - iy1
	}
	return dx*dx+dy*dy <= r*r
}

// drawBadge draws the red badge shape (circle for 1-2 characters, pill
// for "99+") centred at (cx, cy) with the given radius, with the white
// label centred in it. All geometry is in img pixels; the shape is
// clamped inside the image.
func drawBadge(img *image.RGBA, label string, cx, cy, radius float64) error {
	face, err := badgeTypeFace(1.75 * radius)
	if err != nil {
		return err
	}
	// Size the pill from the measured label width.
	textW := float64(font.MeasureString(face, label)) / 64
	pad := 0.45 * radius
	halfW := radius
	if textW+2*pad > 2*radius {
		halfW = (textW + 2*pad) / 2
	}
	// Cap the pill width: for a 3-character label ("99+") shrink the
	// font so the pill fits 3.2 radii wide instead of spanning the
	// whole top of the icon.
	if halfW > 1.6*radius {
		scale := 1.6 * radius / halfW
		face, err = badgeTypeFace(1.75 * radius * scale)
		if err != nil {
			return err
		}
		textW = float64(font.MeasureString(face, label)) / 64
		halfW = (textW + 2*pad) / 2
	}
	halfH := radius
	// Clamp the shape inside the image.
	b := img.Bounds()
	if cx+halfW > float64(b.Max.X)-2 {
		cx = float64(b.Max.X) - 2 - halfW
	}
	if cx-halfW < float64(b.Min.X)+2 {
		cx = float64(b.Min.X) + 2 + halfW
	}
	if cy+halfH > float64(b.Max.Y)-2 {
		cy = float64(b.Max.Y) - 2 - halfH
	}
	if cy-halfH < float64(b.Min.Y)+2 {
		cy = float64(b.Min.Y) + 2 + halfH
	}
	// Fill the rounded rect.
	x0 := int(cx - halfW)
	x1 := int(cx + halfW + 1)
	y0 := int(cy - halfH)
	y1 := int(cy + halfH + 1)
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			if !inRoundedRect(float64(x)+0.5, float64(y)+0.5, cx-halfW, cy-halfH, cx+halfW, cy+halfH, halfH) {
				continue
			}
			img.Set(x, y, badgeRed)
		}
	}
	// Centre the label on the badge: baseline half a cap height below
	// the centre line.
	m := face.Metrics()
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(badgeText),
		Face: face,
	}
	d.Dot = fixed.Point26_6{
		X: fixed.I(int(cx)) - font.MeasureString(face, label)/2,
		Y: fixed.I(int(cy)) + m.CapHeight/2,
	}
	d.DrawString(label)
	return nil
}

// badgeGeometry is the badge placement on the full tray icon, as fractions
// of the icon width: centre near the top-right corner, radius sized so
// the number still reads when the icon is scaled down to menu-bar size
// (macOS displays the status-item image at 16pt, so the badge diameter
// is only ~5pt on screen — it has to be a large fraction of the icon,
// about the same proportion as an iOS app-icon badge).
const (
	badgeCentreX = 0.81
	badgeCentreY = 0.18
	badgeRadius  = 0.16
)

// RenderBadgedIconPNG renders the tray icon PNG with the unread badge
// drawn on it. For count <= 0 it returns the base bytes unchanged, so
// the plain icon is byte-identical to the unbadged one.
func RenderBadgedIconPNG(basePNG []byte, count int) ([]byte, error) {
	if count <= 0 {
		return basePNG, nil
	}
	img, err := png.Decode(bytes.NewReader(basePNG))
	if err != nil {
		return nil, fmt.Errorf("decode tray icon: %w", err)
	}
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, image.Point{}, draw.Src)
	w := float64(rgba.Bounds().Dx())
	if err := drawBadge(rgba, FormatBadgeCount(count), badgeCentreX*w, badgeCentreY*w, badgeRadius*w); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		return nil, fmt.Errorf("encode badged icon: %w", err)
	}
	return buf.Bytes(), nil
}

// RenderBadgedIconICO renders the badged icon as a .ico file: PNG data
// embedded in an ICO container, which Windows has supported since Vista.
// The Windows tray library loads icons with LoadImage(IMAGE_ICON), which
// only accepts .ico — raw PNG bytes fail to load there. The icon is
// rendered at 256px: ICO directory entries cap at 256.
func RenderBadgedIconICO(basePNG []byte, count int) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(basePNG))
	if err != nil {
		return nil, fmt.Errorf("decode tray icon: %w", err)
	}
	const size = 256
	rgba := image.NewRGBA(image.Rect(0, 0, size, size))
	xdraw.BiLinear.Scale(rgba, rgba.Bounds(), img, img.Bounds(), xdraw.Src, nil)
	if count > 0 {
		if err := drawBadge(rgba, FormatBadgeCount(count), badgeCentreX*size, badgeCentreY*size, badgeRadius*size); err != nil {
			return nil, err
		}
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, rgba); err != nil {
		return nil, fmt.Errorf("encode badged icon: %w", err)
	}
	pngBytes := pngBuf.Bytes()
	// ICO container: 6-byte header + 16-byte directory entry + PNG data.
	ico := make([]byte, 0, 6+16+len(pngBytes))
	ico = append(ico, 0, 0, 1, 0, 1, 0) // reserved, type 1 (icon), 1 image
	ico = append(ico,
		0,    // width: 0 means 256
		0,    // height: 0 means 256
		0,    // palette colours
		0,    // reserved
		1, 0, // colour planes
		32, 0, // bits per pixel
		byte(len(pngBytes)), byte(len(pngBytes)>>8), byte(len(pngBytes)>>16), byte(len(pngBytes)>>24), // data size
		22, 0, 0, 0, // offset of the PNG data (6 + 16)
	)
	ico = append(ico, pngBytes...)
	return ico, nil
}

// RenderBadgeOnly renders just the badge (transparent background, no
// envelope) at the given square size, for the Windows taskbar overlay
// icon — the taskbar button already shows the envelope, so the overlay
// is the badge alone. Returns nil for count <= 0 (the caller clears the
// overlay instead).
func RenderBadgeOnly(count, size int) (*image.RGBA, error) {
	if count <= 0 || size <= 0 {
		return nil, nil
	}
	rgba := image.NewRGBA(image.Rect(0, 0, size, size))
	if err := drawBadge(rgba, FormatBadgeCount(count), float64(size)/2, float64(size)/2, float64(size)*0.42); err != nil {
		return nil, err
	}
	return rgba, nil
}
