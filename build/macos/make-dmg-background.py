#!/usr/bin/env python3
"""Generate build/macos/dmg-background.png — the background for the Daak
macOS installer DMG (create-dmg, window 660x400 points, image at 2x).

Style (approved by Abid, Nov 17, 2026 — StableBar-style dark background):
- Near-black background, big white bold "Daak" title centred at the top,
  grey tagline "Your mail. Your way." beneath it.
- Two dark rounded tiles. The left tile holds a white rounded square;
  the right tile holds a drawn light-blue Applications folder.
- Orange arrow between the tiles with "Drag to install" beneath it;
  small pill labels "Daak.app" and "Applications" under the tiles.

Layout (image is 1320x800, i.e. 2x the 660x400 window):
- Left tile (170,330)-(540,630), right tile (780,330)-(1150,630).
  Tile centres in window points: (177,240) and (482,240) — the
  create-dmg --icon / --app-drop-link positions in the release workflow
  must match these, so Finder renders Daak.app centred on the left tile
  and the Applications link centred on the right tile.

Deliberate: the envelope icon is NOT drawn in this background. Finder
renders the real Daak.app icon (128pt) on top of the white square, so a
drawn envelope would double up and peek out from under the real one.
The white square is 280px (140pt) so the 128pt icon sits inside it.
The drawn folder is kept: the real Applications icon renders on top of
it at the same centre, covering it, so the tile never looks empty.

Run from the repo root:  python3 build/macos/make-dmg-background.py
"""
from PIL import Image, ImageDraw, ImageFont

W, H = 1320, 800
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
title_font = ImageFont.truetype(BOLD, 116)
tag_font = ImageFont.truetype(REG, 32)
d.text((W // 2, 108), "Daak", font=title_font, fill=WHITE, anchor="mm")
d.text((W // 2, 192), "Your mail. Your way.", font=tag_font, fill=GREY, anchor="mm")

# Tiles: left (170,330)-(540,630), right (780,330)-(1150,630)
for (x0, y0, x1, y1) in [(170, 330, 540, 630), (780, 330, 1150, 630)]:
    d.rounded_rectangle([x0, y0, x1, y1], radius=28, fill=TILE,
                        outline=TILE_EDGE, width=2)

# Left tile: white rounded square for the real app icon (280px = 140pt,
# centred on the tile centre (355,480) so the 128pt Finder icon fits).
d.rounded_rectangle([215, 340, 495, 620], radius=36, fill=WHITE)

# Right tile: light-blue Applications folder, centred on the tile
# centre (965,480). Finder renders the real Applications link on top.
fx0, fy0, fx1, fy1 = 855, 400, 1075, 560
# back + tab
d.rounded_rectangle([fx0, fy0 + 26, fx1, fy1], radius=16, fill=(74, 144, 205))
d.polygon([(fx0 + 6, fy0 + 34), (fx0 + 6, fy0 + 8), (fx0 + 92, fy0 + 8),
           (fx0 + 118, fy0 + 34)], fill=(74, 144, 205))
# front
d.rounded_rectangle([fx0, fy0 + 48, fx1, fy1], radius=16, fill=(124, 196, 248))
d.rounded_rectangle([fx0, fy0 + 48, fx1, fy0 + 78], radius=16, fill=(124, 196, 248))
d.rectangle([fx0, fy0 + 62, fx1, fy0 + 78], fill=(124, 196, 248))
# stylised "A" on the folder front
a_font = ImageFont.truetype(BOLD, 96)
d.text(((fx0 + fx1) // 2, (fy0 + 48 + fy1) // 2 + 6), "A", font=a_font,
       fill=(47, 127, 194), anchor="mm")

# Orange drag arrow between the tiles (gap is x 540-780 at 2x)
ay = 470
d.line([(585, ay), (700, ay)], fill=ORANGE, width=7)
d.polygon([(700, ay - 16), (700, ay + 16), (736, ay)], fill=ORANGE)
drag_font = ImageFont.truetype(REG, 27)
d.text((660, 522), "Drag to install", font=drag_font, fill=DARK_GREY, anchor="mm")

# Labels under the tiles
label_font = ImageFont.truetype(REG, 27)
for cx, text in [(355, "Daak.app"), (965, "Applications")]:
    tw = d.textlength(text, font=label_font)
    d.rounded_rectangle([cx - tw / 2 - 22, 652, cx + tw / 2 + 22, 700],
                        radius=12, fill=PILL)
    d.text((cx, 676), text, font=label_font, fill=(205, 205, 208), anchor="mm")

# Faint divider near the bottom
d.line([(60, 762), (W - 60, 762)], fill=(38, 38, 41), width=2)

img.save("build/macos/dmg-background.png")
print("wrote build/macos/dmg-background.png", img.size)
