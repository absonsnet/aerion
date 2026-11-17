#!/usr/bin/env python3
"""Generate build/macos/dmg-background.png — the background for the Daak
macOS installer DMG (create-dmg, window 660x400 points, image at 2x).

Layout (image is 1320x800, i.e. 2x the 660x400 window):
- Header at the top: the envelope logo + "Drag Daak to Applications".
- A brand-blue arrow in the gap between the two icons create-dmg places:
  the app icon at (180, 200) and the Applications drop link at (480, 200)
  in window points — so the arrow sits between them, never under an icon.
- No fake Applications folder is drawn: the real symlink icon renders
  there at install time.
"""
from PIL import Image, ImageDraw, ImageFont

W, H = 1320, 800
BG = (247, 249, 252, 255)          # very light blue-grey
BLUE = (12, 94, 246, 255)          # Daak brand blue #0C5EF6
TEXT = (31, 41, 55, 255)           # dark grey

img = Image.new("RGBA", (W, H), BG)
draw = ImageDraw.Draw(img)

# Header: envelope logo + "Drag Daak to Applications", centred near the top.
logo = Image.open("brand/icon.png").convert("RGBA")
logo_size = 64
logo = logo.resize((logo_size, logo_size), Image.LANCZOS)
font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 46)
text = "Drag Daak to Applications"
bbox = draw.textbbox((0, 0), text, font=font)
text_w = bbox[2] - bbox[0]
text_h = bbox[3] - bbox[1]
gap = 18
total_w = logo_size + gap + text_w
start_x = (W - total_w) // 2
top = 96
img.paste(logo, (start_x, top), logo)
draw.text(
    (start_x + logo_size + gap, top + (logo_size - text_h) // 2 - bbox[1]),
    text, font=font, fill=TEXT,
)

# Arrow between the icons: app icon spans x 232-488, Applications link
# spans x 832-1088 (2x coords, icon size 128pt). Arrow fits the gap.
cy = 400
shaft_x0, shaft_x1 = 540, 760
shaft_half = 20
head_x0, head_x1 = 760, 850
head_half = 62
draw.polygon(
    [
        (shaft_x0, cy - shaft_half),
        (shaft_x1, cy - shaft_half),
        (shaft_x1, cy - head_half),
        (head_x1, cy),
        (shaft_x1, cy + head_half),
        (shaft_x1, cy + shaft_half),
        (shaft_x0, cy + shaft_half),
    ],
    fill=BLUE,
)

img.convert("RGB").save("build/macos/dmg-background.png")
print("wrote build/macos/dmg-background.png", img.size)
