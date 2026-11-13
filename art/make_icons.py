"""Generate Aida64SD's icons: SVG sources in art/icons/ and the PNGs the
manifest references in com.poizenjam.aida64sd.sdPlugin/.

Usage (from the repo root): python art/make_icons.py
Needs Python 3 and rsvg-convert (librsvg). On Windows, run it from WSL or
MSYS2 (pacman -S mingw-w64-x86_64-librsvg); the PNGs are committed, so this
is only needed when the art changes.

Design follows the hwinfo-streamdeck plugin's icon system:
- action/category icons: the mark alone, monochrome; variants add a small
  badge bottom-right (composite 2x2 grid, derived sigma, dial knob);
- plugin icon and default key image: the mark in grey inside a rounded
  outline, over a filled sparkline, on a dark field tinted from the accent.
Their blue (#3D6EE0 line, #1F3872 fill, #0A1423 field) maps to AIDA64's red
here. The mark is an original rounded "A." in the same letter-plus-dot form as
HWiNFO's "h."; AIDA64's own "64" and gear marks are FinalWire's and are not
used (Elgato's Marketplace rules forbid images you do not own, and the plugin
is not affiliated with FinalWire).

Sizes per Elgato's plugin guidelines (docs.elgato.com, "Images and layouts"):
plugin icon 256/512 PNG; category icon 28/56; action icon 20/40, monochrome
white (#FFFFFF) on transparent; key icon 72/144. Marketplace app icon 288 PNG
and thumbnail 1920x960 (docs.elgato.com, "Thumbnails and App Icons").
"""
import math
import os
import random
import subprocess

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SRC = os.path.join(ROOT, "art", "icons")
PLUGIN = os.path.join(ROOT, "com.poizenjam.aida64sd.sdPlugin")
MARKET = os.path.join(ROOT, "art", "marketplace")

FIELD = "#170A0B"   # dark field tinted from the accent
LINE = "#D9262C"    # outline + graph line (AIDA64 red #BB0000, lifted for a dark field)
FILL = "#6B1418"    # graph fill, about half the line's intensity
MARK = "#7E8287"    # neutral grey mark on the key/plugin art
WHITE = "#FFFFFF"   # action/category icons (Elgato: white, monochrome)


def mark(color, tx=0.0, ty=0.0, s=1.0):
    """The "A." mark in a 100x100 box: a rounded arch with a crossbar and a
    dot, flat feet. Returned as an SVG group."""
    return (
        f'<g transform="translate({tx} {ty}) scale({s})" fill="{color}">'
        # Coordinates sit on a 5-unit grid so straight edges land on whole
        # pixels at 20 px (5 units = 1 px) and 40 px. Arch: legs 15 wide,
        # outer r 25, inner r 10 (keeps the counter open at 20 px). The
        # crossbar overlaps both legs so no antialiasing seam shows at the joins.
        '<path d="M10 90 V45 A25 25 0 0 1 60 45 V90 H45 V45 A10 10 0 0 0 25 45 V90 Z"/>'
        '<rect x="20" y="60" width="30" height="10"/>'
        '<circle cx="77.5" cy="82.5" r="7.5"/>'
        "</g>"
    )


def badge(kind, color):
    """Variant badges for the bottom-right quadrant (x,y 54..100)."""
    if kind == "composite":
        cells = []
        for cx in (58, 79):
            for cy in (58, 79):
                cells.append(f'<rect x="{cx}" y="{cy}" width="17" height="17" rx="3"/>')
        return f'<g fill="{color}">{"".join(cells)}</g>'
    if kind == "derived":
        return (f'<path d="M94 58 H60 L78 77 L60 96 H94" fill="none" stroke="{color}" '
                'stroke-width="8" stroke-linejoin="miter" stroke-linecap="butt"/>')
    if kind == "settings":
        # generic 8-tooth gear (the standard settings glyph; HWiNFO's set uses
        # a gear badge for its Settings action too)
        cx, cy, r_out, r_in, hole = 77, 77, 21, 15, 6
        pts = []
        for i in range(16):
            a0 = math.radians(i * 22.5 - 7)
            a1 = math.radians(i * 22.5 + 7)
            r = r_out if i % 2 == 0 else r_in
            pts += [(cx + r * math.cos(a0), cy + r * math.sin(a0)),
                    (cx + r * math.cos(a1), cy + r * math.sin(a1))]
        d = "M" + " L".join(f"{x:.2f} {y:.2f}" for x, y in pts) + " Z"
        hole_d = (f"M{cx + hole} {cy} A{hole} {hole} 0 1 0 {cx - hole} {cy} "
                  f"A{hole} {hole} 0 1 0 {cx + hole} {cy} Z")
        return f'<path d="{d} {hole_d}" fill="{color}" fill-rule="evenodd"/>'
    if kind == "dial":
        # rotation arrow (the dial cycles readings); an open 270 degree arc with
        # a head, not a ring with a line, which reads as a power symbol
        return (f'<g fill="none" stroke="{color}" stroke-width="7" stroke-linecap="butt">'
                '<path d="M77 60 A17 17 0 1 1 60 77"/></g>'
                f'<path d="M50 78 L70 78 L60 64 Z" fill="{color}"/>')
    raise ValueError(kind)


def mono_icon(kind=None):
    """Action/category icon, 100x100 viewBox. Variants shrink the mark to the
    top-left and knock a gap around the badge, as the HWiNFO set does."""
    if kind is None:
        body = mark(WHITE, 5, 0, 1)
    else:
        knock = {
            "composite": '<rect x="50" y="50" width="54" height="54" rx="6"/>',
            "derived": '<rect x="50" y="50" width="54" height="54"/>',
            "dial": '<circle cx="77" cy="77" r="27"/>',
            "settings": '<circle cx="77" cy="77" r="27"/>',
        }[kind]
        body = (
            '<mask id="m"><rect width="100" height="100" fill="#fff"/>'
            f'<g fill="#000">{knock}</g></mask>'
            f'<g mask="url(#m)">{mark(WHITE, 0, 0, 0.82)}</g>'
            + badge(kind, WHITE)
        )
    return f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">{body}</svg>'


def sparkline_points(n, seed):
    """A deterministic sensor-like trace in 0..1: a broad hump plus spikes,
    the same character as the HWiNFO art's graph."""
    rnd = random.Random(seed)
    pts = []
    for i in range(n):
        x = i / (n - 1)
        hump = 0.55 * math.exp(-((x - 0.32) / 0.12) ** 2)
        tail = 0.18 * math.exp(-((x - 0.78) / 0.08) ** 2)
        noise = rnd.random() * 0.12
        spike = 0.25 if rnd.random() < 0.08 else 0.0
        pts.append(min(0.95, 0.12 + hump + tail + noise + spike))
    return pts


def graph(x0, y0, w, h, stroke_w, seed=64):
    """Stepped area graph (matches the tile renderer's bar-per-sample look)."""
    vals = sparkline_points(28, seed)
    step = w / len(vals)
    d = [f"M{x0:.2f} {y0 + h:.2f}"]
    for i, v in enumerate(vals):
        y = y0 + h - v * h
        d.append(f"L{x0 + i * step:.2f} {y:.2f} L{x0 + (i + 1) * step:.2f} {y:.2f}")
    d.append(f"L{x0 + w:.2f} {y0 + h:.2f} Z")
    path = " ".join(d)
    return (f'<path d="{path}" fill="{FILL}"/>'
            f'<path d="{path}" fill="none" stroke="{LINE}" stroke-width="{stroke_w}" '
            'stroke-linejoin="miter"/>')


def key_art(rounded_tile):
    """Default key image / plugin icon, 144x144 viewBox: outline box, grey
    mark, graph in front of the mark's lower half."""
    tile = (f'<rect width="144" height="144" rx="26" fill="{FIELD}"/>' if rounded_tile
            else f'<rect width="144" height="144" fill="{FIELD}"/>')
    box_x, box_y, box_w, box_h = 22, 22, 100, 100
    clip = (f'<clipPath id="c"><rect x="{box_x}" y="{box_y}" width="{box_w}" '
            f'height="{box_h}" rx="10"/></clipPath>')
    return (
        '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 144 144">'
        f"{tile}<defs>{clip}</defs>"
        f'<g clip-path="url(#c)">'
        f"{mark(MARK, 42, 30, 0.62)}"
        f"{graph(box_x, 70, box_w, 52, 2.2)}"
        "</g>"
        f'<rect x="{box_x}" y="{box_y}" width="{box_w}" height="{box_h}" rx="10" '
        f'fill="none" stroke="{LINE}" stroke-width="5"/>'
        "</svg>"
    )


def thumbnail():
    """Marketplace thumbnail, 1920x960: product name as the focus."""
    icon = key_art(True).replace(
        '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 144 144">',
        '<svg x="150" y="210" width="540" height="540" viewBox="0 0 144 144">')
    return (
        '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1920 960">'
        f'<rect width="1920" height="960" fill="#0E0607"/>'
        f'<g opacity="0.35">{graph(0, 640, 1920, 320, 4, seed=7)}</g>'
        f"{icon}"
        '<text x="770" y="470" font-family="DejaVu Sans" font-weight="bold" '
        f'font-size="170" fill="{WHITE}">Aida64SD</text>'
        '<text x="776" y="556" font-family="DejaVu Sans" font-size="44" '
        f'fill="#C9C9C9">AIDA64 sensor readings on Stream Deck</text>'
        "</svg>"
    )


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        f.write(text)


def render(svg_path, png_path, size, height=None):
    subprocess.run(["rsvg-convert", "-w", str(size), "-h", str(height or size),
                    "-o", png_path, svg_path], check=True)


def main():
    sources = {
        "categoryIcon": mono_icon(),
        "actionIcon": mono_icon(),
        "actionIcon_composite": mono_icon("composite"),
        "actionIcon_derived": mono_icon("derived"),
        "actionIcon_dial": mono_icon("dial"),
        "actionIcon_settings": mono_icon("settings"),
        "defaultImage": key_art(False),
        # Settings key base and the host's cached placeholder (plugin.go tries
        # settingsImage.png first, as hwinfo-streamdeck does): same key art.
        "settingsImage": key_art(False),
        "pluginIcon": key_art(True),
    }
    sizes = {
        "categoryIcon": 28, "actionIcon": 20, "actionIcon_composite": 20,
        "actionIcon_derived": 20, "actionIcon_dial": 20, "actionIcon_settings": 20,
        "defaultImage": 72, "settingsImage": 72, "pluginIcon": 256,
    }
    for name, svg in sources.items():
        svg_path = os.path.join(SRC, name + ".svg")
        write(svg_path, svg)
        base = sizes[name]
        render(svg_path, os.path.join(PLUGIN, name + ".png"), base)
        render(svg_path, os.path.join(PLUGIN, name + "@2x.png"), base * 2)

    os.makedirs(MARKET, exist_ok=True)
    render(os.path.join(SRC, "pluginIcon.svg"), os.path.join(MARKET, "app-icon-288.png"), 288)
    thumb = os.path.join(SRC, "thumbnail.svg")
    write(thumb, thumbnail())
    render(thumb, os.path.join(MARKET, "thumbnail-1920x960.png"), 1920, 960)
    # GitHub social preview (repo Settings > Social preview; 1280x640 recommended)
    github = os.path.join(ROOT, "art", "github")
    os.makedirs(github, exist_ok=True)
    render(thumb, os.path.join(github, "social-preview-1280x640.png"), 1280, 640)


if __name__ == "__main__":
    main()
