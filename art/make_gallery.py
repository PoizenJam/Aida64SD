"""Generate the Marketplace gallery images (1920x960, 2:1) in art/marketplace/.

Usage (from the repo root): python art/make_gallery.py   (needs Pillow)

The key tiles are re-rendered here with the plugin's own layout rules so the
images show what the plugin actually draws: 72x72 tiles, one graph column per
sample, foreground fill under a highlight line with vertical connectors
(pkg/graph drawGraph), labels centred in DejaVu Sans Bold 10.5 px at
baselines 19 (title, #b7b7b7) and 44 / 56 (value lines, white), default
colours fg #005128 / hl #009E00 on black (delegate.go), composite slots as
stacked bands with 9 px titles and 10.5 px values in the per-slot palette
(composite.go). Tiles are drawn at SCALE times the pixel density, like a
high-DPI device, so they stay sharp at gallery size.

The device is a generic key grid, not a render of any Elgato product.
Replace image 1 with a photo of a real deck later if wanted.
"""
import math
import os
import random

from PIL import Image, ImageChops, ImageDraw, ImageFont

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT = os.path.join(ROOT, "art", "marketplace")
FONT_BOLD = os.path.join(ROOT, "com.poizenjam.aida64sd.sdPlugin", "DejaVuSans-Bold.ttf")
FONT_REG = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
if not os.path.exists(FONT_REG):
    FONT_REG = FONT_BOLD

W, H = 1920, 960
SCALE = 2.5            # tile pixel density; 72 * 2.5 = 180 px keys
BG = (14, 6, 7)        # gallery field, same as the thumbnail
ACCENT = (217, 38, 44)  # icon red
TEXT = (240, 240, 240)
SUB = (190, 190, 190)


def hexrgb(h):
    h = h.lstrip("#")
    return tuple(int(h[i:i + 2], 16) for i in (0, 2, 4))


def font(path, px):
    return ImageFont.truetype(path, max(1, int(round(px))))


def series(n, base, swing, seed, spikes=0.0):
    """Plausible sensor trace in 0..1 (random walk with occasional spikes)."""
    rnd = random.Random(seed)
    v, out = base, []
    for _ in range(n):
        v += rnd.uniform(-swing, swing)
        v = min(0.95, max(0.03, v + (base - v) * 0.15))
        s = v + (rnd.uniform(0.15, 0.35) if rnd.random() < spikes else 0.0)
        # kept below the value line (baseline 44 of 72) so text stays legible,
        # as it would with sensible per-tile Min/Max
        out.append(min(0.97, s) * 0.7)
    return out


def draw_graph(img, vals, fg, hl, s, height_px=72):
    """pkg/graph drawGraph at 1 column per sample, scaled by s."""
    d = ImageDraw.Draw(img)
    eff = height_px
    prev = None
    for x, v in enumerate(vals):
        vay = int(round(v * (eff - 1)))
        x0, x1 = int(round(x * s)), int(round((x + 1) * s))
        for y in range(eff):
            if y == vay or (prev is not None and min(prev, vay) <= y <= max(prev, vay)):
                c = hl
            elif y < vay:
                c = fg
            else:
                continue
            y0 = int(round((72 - 1 - y) * s))
            y1 = int(round((72 - y) * s))
            d.rectangle([x0, y0, x1 - 1, y1 - 1], fill=c)
        prev = vay


def text_center(img, txt, baseline, size, color, s):
    d = ImageDraw.Draw(img)
    # The real renderer does not shrink or clip text, so only use strings
    # that fit a 72 px key (with a 2 px margin each side).
    width = d.textlength(txt, font=font(FONT_BOLD, size * s)) / s
    if width > 68:
        raise ValueError(f"{txt!r} is {width:.1f} px wide at {size} px; does not fit a 72 px key")
    d.text((36 * s, baseline * s), txt, font=font(FONT_BOLD, size * s), fill=color, anchor="ms")


def sensor_tile(title, value, vals, fg="#005128", hl="#009E00", bg="#000000",
                title_color="#b7b7b7", value_color="#ffffff", line2=None, s=SCALE):
    img = Image.new("RGB", (int(72 * s), int(72 * s)), hexrgb(bg))
    draw_graph(img, vals, hexrgb(fg), hexrgb(hl), s)
    text_center(img, title, 19, 10.5, hexrgb(title_color), s)
    text_center(img, value, 44, 10.5, hexrgb(value_color), s)
    if line2:
        text_center(img, line2, 56, 10.5, hexrgb(value_color), s)
    return img


COMPOSITE_SLOTS = [("#005128", "#009E00"), ("#004050", "#009EBE"),
                   ("#502800", "#BE6000"), ("#500050", "#9E009E")]


def composite_tile(slots, s=SCALE, graphs=True):
    """composite.go renderCompositeTile: per-slot graphs lighten-blended at
    55% fill, then label/value centred per horizontal band."""
    n = len(slots)
    img = Image.new("RGB", (int(72 * s), int(72 * s)), (0, 0, 0))
    if graphs:
        for i, (_, _, vals) in enumerate(slots):
            fg, hl = COMPOSITE_SLOTS[i]
            layer = Image.new("RGB", img.size, (0, 0, 0))
            fgc = tuple(int(c * 0.55) for c in hexrgb(fg))
            draw_graph(layer, vals, fgc, hexrgb(hl), s)
            img = ImageChops.lighter(img, layer)
    zone = 72 / n
    for i, (label, value, _) in enumerate(slots):
        mid = i * zone + zone / 2
        gap = (9.0 + 10.5) / 2
        label_y = round(mid - gap * 0.3)
        value_y = round(mid + gap * 0.85)
        text_center(img, label, label_y, 9.0, hexrgb("#888888"), s)
        text_center(img, value, value_y, 10.5, hexrgb(COMPOSITE_SLOTS[i][1]), s)
    return img


def rounded(img, radius):
    mask = Image.new("L", img.size, 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, img.size[0] - 1, img.size[1] - 1], radius, fill=255)
    out = Image.new("RGBA", img.size)
    out.paste(img, (0, 0), mask)
    return out


def device(tiles, cols, rows, key=180, gap=24, pad=42):
    """Generic key grid: dark rounded body, rounded keys."""
    w = pad * 2 + cols * key + (cols - 1) * gap
    h = pad * 2 + rows * key + (rows - 1) * gap
    body = Image.new("RGBA", (w, h), (0, 0, 0, 0))
    d = ImageDraw.Draw(body)
    d.rounded_rectangle([0, 0, w - 1, h - 1], 38, fill=(10, 10, 11, 255), outline=(48, 48, 52, 255), width=3)
    for i, t in enumerate(tiles):
        if t is None:
            t = Image.new("RGB", (key, key), (0, 0, 0))
        r, c = divmod(i, cols)
        x = pad + c * (key + gap)
        y = pad + r * (key + gap)
        d.rounded_rectangle([x - 3, y - 3, x + key + 2, y + key + 2], 24, fill=(30, 30, 33, 255))
        body.alpha_composite(rounded(t.resize((key, key), Image.LANCZOS) if t.size[0] != key else t, 20), (x, y))
    return body


def canvas_with_headline(headline, sub):
    img = Image.new("RGBA", (W, H), BG + (255,))
    d = ImageDraw.Draw(img)
    # faint background trace, as on the thumbnail
    vals = series(64, 0.35, 0.08, 11, spikes=0.05)
    step = W / len(vals)
    pts = [(0, H)]
    for i, v in enumerate(vals):
        y = H - v * 300
        pts += [(i * step, y), ((i + 1) * step, y)]
    pts.append((W, H))
    d.polygon(pts, fill=(36, 10, 12))
    d.rectangle([100, 300, 112, 300 + 12], fill=ACCENT)
    y = 330
    for line in headline.split("\n"):
        d.text((100, y), line, font=font(FONT_BOLD, 76), fill=TEXT)
        y += 92
    y += 18
    for line in sub.split("\n"):
        d.text((102, y), line, font=font(FONT_REG, 32), fill=SUB)
        y += 44
    return img


def place(img, layer, cx, cy):
    img.alpha_composite(layer, (int(cx - layer.size[0] / 2), int(cy - layer.size[1] / 2)))


def card(w, h, rows, title=None):
    """A small dark settings-style card (illustration of PI values)."""
    c = Image.new("RGBA", (w, h), (0, 0, 0, 0))
    d = ImageDraw.Draw(c)
    d.rounded_rectangle([0, 0, w - 1, h - 1], 18, fill=(34, 34, 36, 255), outline=(60, 60, 64, 255), width=2)
    y = 26
    if title:
        d.text((28, y), title, font=font(FONT_BOLD, 28), fill=(225, 225, 225))
        y += 54
    for label, value in rows:
        d.text((32, y), label, font=font(FONT_REG, 30), fill=(160, 160, 160))
        d.text((w // 2 + 20, y), value, font=font(FONT_BOLD, 30), fill=(235, 235, 235))
        y += 50
    return c


def gallery_1():
    tiles = [
        sensor_tile("CPU Pkg", "64 °C", series(72, 0.32, 0.04, 1)),
        sensor_tile("CPU Load", "23 %", series(72, 0.2, 0.07, 2, spikes=0.08)),
        sensor_tile("GPU Hot", "71 °C", series(72, 0.36, 0.03, 3)),
        sensor_tile("GPU Power", "287 W", series(72, 0.38, 0.06, 4, spikes=0.05)),
        sensor_tile("GPU Clock", "2730 MHz", series(72, 0.4, 0.03, 5)),
        sensor_tile("CPU Fan", "1250 RPM", series(72, 0.3, 0.03, 6)),
        sensor_tile("RAM Used", "41 %", series(72, 0.3, 0.01, 7)),
        composite_tile([("CPU", "64 °C", [v * 0.35 for v in series(72, 0.3, 0.04, 8)]),
                        ("GPU", "71 °C", [v * 0.35 for v in series(72, 0.4, 0.03, 9)])]),
        sensor_tile("NVMe", "48 °C", series(72, 0.28, 0.02, 10)),
        sensor_tile("Pump", "2400 RPM", series(72, 0.36, 0.01, 11)),
        sensor_tile("Sys Power", "512 W", series(72, 0.36, 0.06, 12, spikes=0.06)),
        sensor_tile("GPU2", "58 °C", series(72, 0.3, 0.04, 13)),
        sensor_tile("Download", "38 MB/s", series(72, 0.18, 0.1, 14, spikes=0.15)),
        sensor_tile("Vcore", "1.21 V", series(72, 0.34, 0.02, 15)),
        sensor_tile("Coolant", "33 °C", series(72, 0.24, 0.005, 16)),
    ]
    img = canvas_with_headline("Your AIDA64\nsensors, live\non a key.",
                               "Temperatures, clocks, fans, power\nand more, with live graphs.")
    place(img, device(tiles, 5, 3), 1290, 480)
    return img


def gallery_2():
    low = lambda vals: [v * 0.35 for v in vals]  # graphs span the whole key; keep them under the text
    two = composite_tile([("CPU Pkg", "64 °C", low(series(72, 0.3, 0.04, 21))),
                          ("GPU Hot", "71 °C", low(series(72, 0.42, 0.03, 22)))], s=4)
    three = composite_tile([("CPU", "64 °C", None), ("GPU", "71 °C", None),
                            ("NVMe", "48 °C", None)], s=4, graphs=False)  # text-only mode
    img = canvas_with_headline("Several\nreadings\non one key.",
                               "Composite tiles stack up to four\nreadings, each with its own\ncolour and graph.")
    k = 320
    place(img, device([two.resize((k, k), Image.LANCZOS), three.resize((k, k), Image.LANCZOS)], 2, 1, key=k, gap=60, pad=56), 1290, 480)
    return img


def gallery_3():
    tile = sensor_tile("Sys Power", "512 W", series(72, 0.36, 0.06, 31, spikes=0.06), s=4)
    img = canvas_with_headline("Combine\nreadings into\na new metric.",
                               "Derived tiles sum, average,\nsubtract, or take the max, min\nor a percentage of readings.")
    c = card(600, 360, [("CPU Package", "142 W"), ("GPU1 Power", "287 W"),
                        ("GPU2 Power", "83 W"), ("Formula", "Sum")], title="Derived: System Power")
    place(img, c, 1080, 480)
    place(img, device([tile.resize((300, 300), Image.LANCZOS)], 1, 1, key=300, pad=48), 1590, 480)
    d = ImageDraw.Draw(img)
    d.polygon([(1376, 460), (1406, 480), (1376, 500)], fill=ACCENT)
    return img


def gallery_4():
    vals_ok = series(72, 0.36, 0.03, 41)
    vals_hot = vals_ok[:52] + [min(0.9, v + 0.25) for v in series(20, 0.4, 0.03, 42)]
    ok = sensor_tile("GPU Hot", "71 °C", vals_ok, s=4)
    hot = sensor_tile("GPU Hot", "96 °C", vals_hot, fg="#5A0A0A", hl="#FF3B30",
                      bg="#1E0000", title_color="#FFB3B3", value_color="#FFFFFF", line2="HOT", s=4)
    img = canvas_with_headline("Know when\nit runs hot.",
                               "Thresholds recolour the tile and\nshow alert text, per tile or\nfrom a shared library.")
    k = 300
    place(img, device([ok.resize((k, k), Image.LANCZOS), hot.resize((k, k), Image.LANCZOS)], 2, 1, key=k, gap=120, pad=56), 1290, 480)
    d = ImageDraw.Draw(img)
    d.polygon([(1276, 455), (1306, 480), (1276, 505)], fill=ACCENT)
    return img


def text_outlined(img, txt, baseline, size, color, s, stroke=(0x17, 0x0A, 0x0B)):
    """settings_tile.go: text over the key art gets a 1 px outline in the
    art's field colour."""
    for dy in (-1, 0, 1):
        for dx in (-1, 0, 1):
            if dx or dy:
                layer = Image.new("RGB", img.size, (0, 0, 0))
                mask = Image.new("L", img.size, 0)
                d = ImageDraw.Draw(mask)
                d.text((36 * s + dx * s, (baseline + dy) * s), txt, font=font(FONT_BOLD, size * s), fill=255, anchor="ms")
                img.paste(stroke, (0, 0), mask)
    text_center(img, txt, baseline, size, color, s)


def settings_tile(interval, auto, s=SCALE):
    """settings_tile.go renderSettingsTile: the key art, "Refresh Rate" in
    9 px grey at 19, the interval in 10.5 px white at 44, "auto" at 56."""
    art = Image.open(os.path.join(ROOT, "com.poizenjam.aida64sd.sdPlugin", "settingsImage@2x.png")).convert("RGB")
    img = art.resize((int(72 * s), int(72 * s)), Image.LANCZOS)
    text_outlined(img, "Refresh Rate", 19, 9.0, hexrgb("#b7b7b7"), s)
    text_outlined(img, interval, 44, 10.5, hexrgb("#ffffff"), s)
    if auto:
        text_outlined(img, "auto", 56, 9.0, hexrgb("#b7b7b7"), s)
    return img


def gallery_5():
    auto = settings_tile("1000ms", True, s=4)
    manual = settings_tile("250ms", False, s=4)
    img = canvas_with_headline("One key for\nrefresh and\nstatus.",
                               "The Settings key shows the refresh\nrate and holds the shared controls.")
    k = 260
    place(img, device([auto.resize((k, k), Image.LANCZOS), manual.resize((k, k), Image.LANCZOS)], 1, 2, key=k, gap=48, pad=48), 930, 480)
    c = card(640, 340, [("Refresh rate", "Auto (INI)"), ("AIDA64 rate", "1000 ms"),
                        ("Thresholds", "3 global"), ("AIDA64", "Connected"),
                        ("Version", "1.0.0")], title="Settings")
    place(img, c, 1490, 480)
    return img


def main():
    os.makedirs(OUT, exist_ok=True)
    for i, fn in enumerate([gallery_1, gallery_2, gallery_3, gallery_4, gallery_5], 1):
        fn().convert("RGB").save(os.path.join(OUT, f"gallery-{i}-1920x960.png"))


if __name__ == "__main__":
    main()
