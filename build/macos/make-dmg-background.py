#!/usr/bin/env python3
"""Generate build/macos/dmg-background.png — the background for the Daak
macOS installer DMG (create-dmg, window 660x400 points).

IMPORTANT — size: the image MUST be exactly 660x400, the window size in
pixels. Finder displays the DMG background at 1:1 pixels (it does not
scale it to the window and ignores DPI metadata), so a 2x image
(1320x800) is cropped to its top-left quadrant. That is exactly what
happened in v0.7.7/v0.7.8: the centred title was cut to "Da", the tagline
to "Your mail.", and the tiles sat below the visible area. Do NOT go back
to 2x — the 1x image fills the window exactly on the target display.

Style (approved by Abid, Nov 17, 2026 — StableBar-style dark background):
- Near-black background, big white bold "Daak" title centred at the top,
  grey tagline "Your mail. Your way." beneath it.
- Two dark rounded tiles. The left tile holds a white rounded square;
  the right tile holds a drawn light-blue Applications folder.
- Orange arrow between the tiles with "Drag to install" beneath it;
  small pill labels "Daak.app" and "Applications" under the tiles.

Layout (image is 660x400 = the window):
- Left tile (85,165)-(270,315), right tile (390,165)-(575,315).
  Tile centres: (177,240) and (482,240) — the create-dmg --icon /
  --app-drop-link positions in the release workflow must match these,
  so Finder renders Daak.app centred on the left tile and the
  Applications link centred on the right tile.

Deliberate: the envelope icon is NOT drawn in this background. Finder
renders the real Daak.app icon (128pt) on top of the white square, so a
drawn envelope would double up and peek out from under the real one.
The white square is 140px so the 128pt icon sits inside it.
The drawn folder is kept: the real Applications icon renders on top of
it at the same centre, covering it, so the tile never looks empty.

Run from the repo root:  python3 build/macos/make-dmg-background.py
"""
from PIL import Image, ImageDraw, ImageFont

W, H = 660, 400
BG = (12, 12, 13)              # near-black
TILE = (30, 30, 33)            # dark tile
TILE_EDGE = (48, 48, 52)
WHITE = (255, 255, 255)
GREY = (155, 155, 158)         # tagline
DARK_GREY = (110, 110, 114)    # "Drag to install"
ORANGE = (240, 140, 10)        # drag arrow
PILL = (42, 42, 46)

BOLD = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"
REG = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"

img = Image.new("RGB", (W, H), BG)
d = ImageDraw.Draw(img)

# Title + tagline
title_font = ImageFont.truetype(BOLD, 58)
tag_font = ImageFont.truetype(REG, 16)
d.text((W // 2, 54), "Daak", font=title_font, fill=WHITE, anchor="mm")
d.text((W // 2, 96), "Your mail. Your way.", font=tag_font, fill=GREY, anchor="mm")

# Tiles: left (85,165)-(270,315), right (390,165)-(575,315)
for (x0, y0, x1, y1) in [(85, 165, 270, 315), (390, 165, 575, 315)]:
    d.rounded_rectangle([x0, y0, x1, y1], radius=14, fill=TILE,
                        outline=TILE_EDGE, width=1)

# Left tile: white rounded square for the real app icon (140px,
# centred on the tile centre (177,240) so the 128pt Finder icon fits).
d.rounded_rectangle([107, 170, 247, 310], radius=18, fill=WHITE)

# Right tile: light-blue Applications folder, centred on the tile
# centre (482,240). Finder renders the real Applications link on top.
fx0, fy0, fx1, fy1 = 427, 200, 537, 280
# back + tab
d.rounded_rectangle([fx0, fy0 + 13, fx1, fy1], radius=8, fill=(74, 144, 205))
d.polygon([(fx0 + 3, fy0 + 17), (fx0 + 3, fy0 + 4), (fx0 + 46, fy0 + 4),
           (fx0 + 59, fy0 + 17)], fill=(74, 144, 205))
# front
d.rounded_rectangle([fx0, fy0 + 24, fx1, fy1], radius=8, fill=(124, 196, 248))
d.rounded_rectangle([fx0, fy0 + 24, fx1, fy0 + 39], radius=8, fill=(124, 196, 248))
d.rectangle([fx0, fy0 + 31, fx1, fy0 + 39], fill=(124, 196, 248))
# stylised "A" on the folder front
a_font = ImageFont.truetype(BOLD, 48)
d.text(((fx0 + fx1) // 2, (fy0 + 24 + fy1) // 2 + 3), "A", font=a_font,
       fill=(47, 127, 194), anchor="mm")

# Orange drag arrow between the tiles (gap is x 270-390)
ay = 235
d.line([(292, ay), (350, ay)], fill=ORANGE, width=4)
d.polygon([(350, ay - 8), (350, ay + 8), (368, ay)], fill=ORANGE)
drag_font = ImageFont.truetype(REG, 14)
d.text((330, 261), "Drag to install", font=drag_font, fill=DARK_GREY, anchor="mm")

# Labels under the tiles
label_font = ImageFont.truetype(REG, 14)
for cx, text in [(177, "Daak.app"), (482, "Applications")]:
    tw = d.textlength(text, font=label_font)
    d.rounded_rectangle([cx - tw / 2 - 11, 326, cx + tw / 2 + 11, 350],
                        radius=6, fill=PILL)
    d.text((cx, 338), text, font=label_font, fill=(205, 205, 208), anchor="mm")

# Faint divider near the bottom
d.line([(30, 381), (W - 30, 381)], fill=(38, 38, 41), width=1)

img.save("build/macos/dmg-background.png")
print("wrote build/macos/dmg-background.png", img.size)
