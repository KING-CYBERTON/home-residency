#!/usr/bin/env python3
"""Crop and compress a photograph for the website (internal/web/static/img/photos/).

Writes <name>-<w>x<h>.avif, .webp and .jpg for each width. Needs Pillow 11+ (AVIF support).

  python3 scripts/photo.py SOURCE.jpg NAME ASPECT [--focus X,Y] [--widths W,W,...] [--duotone]

ASPECT is like 16:9 or 4:3. --focus is the point to keep in frame, as fractions of the width and
height (default 0.5,0.5). --duotone maps a black-and-white photo onto the brand teal. Then add the
photo to static/img/photos/credits.json. The site uses:

  hero            16:9  640,960,1280,1920   (hero.narrow, 4:5 480,800,1080, is the phone crop)
  providers       16:9  640,1280,1920
  everything else  4:3  480,800,1200
"""
import argparse
import os

from PIL import Image, ImageOps

OUT = os.path.join(os.path.dirname(__file__), "..", "internal", "web", "static", "img", "photos")

ap = argparse.ArgumentParser()
ap.add_argument("source")
ap.add_argument("name")
ap.add_argument("aspect")
ap.add_argument("--focus", default="0.5,0.5")
ap.add_argument("--widths", default="480,800,1200")
ap.add_argument("--duotone", action="store_true")
a = ap.parse_args()

aw, ah = (int(v) for v in a.aspect.split(":"))
fx, fy = (float(v) for v in a.focus.split(","))
im = ImageOps.exif_transpose(Image.open(a.source)).convert("RGB")
if a.duotone:
    im = ImageOps.colorize(ImageOps.grayscale(im), black="#134e4a", white="#f0fdfa")
W, H = im.size
cw, ch = (round(H * aw / ah), H) if W / H > aw / ah else (W, round(W * ah / aw))
x = min(max(round(fx * W - cw / 2), 0), W - cw)
y = min(max(round(fy * H - ch / 2), 0), H - ch)
crop = im.crop((x, y, x + cw, y + ch))
for w in (int(v) for v in a.widths.split(",")):
    h = round(w * ah / aw)
    r = crop.resize((w, h), Image.LANCZOS)
    base = os.path.join(OUT, f"{a.name}-{w}x{h}")
    r.save(base + ".avif", quality=50, speed=4)
    r.save(base + ".webp", quality=72, method=6)
    r.save(base + ".jpg", quality=76, optimize=True, progressive=True)
    print(base)
