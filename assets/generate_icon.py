#!/usr/bin/env python3
"""Generate the Termac application icon (assets/icon.ico and assets/icon.png).

The design mirrors the dashboard's default "Cyan Cyber" theme (see
pkg/theme/theme.go): a terminal window with a prompt chevron and cursor on
the deep-obsidian background. Requires Pillow:

    python -m pip install Pillow
    python assets/generate_icon.py
"""

from pathlib import Path

from PIL import Image, ImageDraw

# Default theme colors from pkg/theme/theme.go
BACKGROUND = (0x0B, 0x0F, 0x19, 255)  # Deep Obsidian
PRIMARY = (0x00, 0xE5, 0xFF, 255)     # Vibrant Cyan
ACCENT = (0xF4, 0x3F, 0x5E, 255)      # Electric Rose
FOREGROUND = (0xF8, 0xFA, 0xFC, 255)  # Bright Slate

MASTER = 512
ICO_SIZES = [256, 48, 32, 16]  # multi-res sizes the original assets doc promised

HERE = Path(__file__).resolve().parent


def rounded_rect(draw, box, radius, fill=None, outline=None, width=1):
    draw.rounded_rectangle(box, radius=radius, fill=fill, outline=outline, width=width)


def draw_logo(size: int) -> Image.Image:
    scale = size / MASTER
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)

    # Outer glow ring.
    ring = int(18 * scale)
    rounded_rect(
        d,
        (ring, ring, size - ring, size - ring),
        radius=int(96 * scale),
        fill=BACKGROUND,
        outline=PRIMARY,
        width=max(2, int(14 * scale)),
    )

    # Window title bar divider.
    bar_bottom = int(150 * scale)
    d.line(
        (int(40 * scale), bar_bottom, size - int(40 * scale), bar_bottom),
        fill=(0x1E, 0x29, 0x3B, 255),
        width=max(1, int(6 * scale)),
    )

    # Title-bar dots (minimize/restore/close vibe).
    dot_r = int(14 * scale)
    for i, color in enumerate(
        (PRIMARY, FOREGROUND, ACCENT)
    ):
        cx = int(78 * scale) + i * int(52 * scale)
        cy = int(96 * scale)
        d.ellipse(
            (cx - dot_r, cy - dot_r, cx + dot_r, cy + dot_r),
            fill=color,
        )

    # Prompt chevron ">".
    chev_x = int(120 * scale)
    chev_top = int(210 * scale)
    chev_mid_y = int(310 * scale)
    chev_bot = int(410 * scale)
    chev_tip = int(240 * scale)
    line_w = max(2, int(34 * scale))
    d.line((chev_x, chev_top, chev_tip, chev_mid_y), fill=PRIMARY, width=line_w)
    d.line((chev_tip, chev_mid_y, chev_x, chev_bot), fill=PRIMARY, width=line_w)

    # Cursor block, shifted one row down like a blinking caret.
    cur_x = int(285 * scale)
    cur_y = int(330 * scale)
    cur_s = int(78 * scale)
    rounded_rect(
        d,
        (cur_x, cur_y, cur_x + cur_s, cur_y + cur_s),
        radius=int(10 * scale),
        fill=ACCENT,
    )

    return img


def main() -> None:
    master = draw_logo(MASTER)

    png_path = HERE / "icon.png"
    master.save(png_path, "PNG")

    ico_path = HERE / "icon.ico"
    frames = [master if s == MASTER else master.resize((s, s), Image.LANCZOS) for s in ICO_SIZES]
    frames[0].save(ico_path, format="ICO", sizes=[(s, s) for s in ICO_SIZES])

    print(f"wrote {png_path} ({MASTER}x{MASTER})")
    print(f"wrote {ico_path} (sizes: {', '.join(str(s) for s in ICO_SIZES)})")


if __name__ == "__main__":
    main()
