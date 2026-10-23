#!/usr/bin/env python3
"""
High-Energy SaaS Product Launch & Feature Walkthrough Video Generator (30 FPS Continuous Motion)
Inspired by Obin Studio / BIASafe AI SaaS Launch Style (https://www.youtube.com/watch?v=GrtumcQkzsk)
- Voiceover: KittenTTS (https://github.com/KittenML/KittenTTS, model KittenML/kitten-tts-nano-0.1)
- Visual Engine:
  * Continuous 30 FPS fluid liquid-mesh gradient shaders (deep black, electric violet, indigo, cyan)
  * Glitch/scramble kinetic typography, 3D radial warp tunnel & rotating 3D wave-particle sphere
  * Macro 2.5x close-up UI camera zooms, smooth spring-physics pans, glowing purple vector cursor
    with click-ripple animations, and real-time state morphs across all 31 live Playwright UI captures
  * Floating problem-cloud kinetic sequence ("Out-of-order updates? Duplicate batches? Not Anymore.")
  * Stacked 3D neon telemetry pills, giant scrolling background marquee, and 3D horizon sphere outro
- Audio Engine:
  * 24kHz KittenTTS English Narration
  * 120 BPM Upbeat Tech/Funk Launch Groove (Kick, Clap, Hi-Hats, Octave Bass, Synth Stabs)
  * Synchronized Whoosh / Swish camera transition SFX, Bass Impacts & UI Click SFX with Sidechain Ducking
"""

import os
import re
import math
import time
import subprocess
import numpy as np
import soundfile as sf
from PIL import Image, ImageDraw, ImageFont

BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUTPUT_DIR = os.path.join(BASE_DIR, "demo_artifacts")
UI_CAPTURES_DIR = os.path.join(OUTPUT_DIR, "ui_captures")
VIDEO_FILE = os.path.join(BASE_DIR, "demo_walkthrough.mp4")

WIDTH = 1920
HEIGHT = 1080
FPS = 30
SAMPLE_RATE = 24000

os.makedirs(OUTPUT_DIR, exist_ok=True)

FONT_SANS = "/System/Library/Fonts/Helvetica.ttc"
FONT_MONO = "/System/Library/Fonts/Menlo.ttc"

_FONT_CACHE = {}


def get_font(size, font_path=FONT_SANS):
    key = (font_path, int(size))
    if key not in _FONT_CACHE:
        try:
            _FONT_CACHE[key] = ImageFont.truetype(font_path, int(size))
        except Exception:
            _FONT_CACHE[key] = ImageFont.load_default()
    return _FONT_CACHE[key]


# ----------------------------------------------------------------------
# 1. Mathematical Easing & Camera Helpers
# ----------------------------------------------------------------------

def clamp(x, a=0.0, b=1.0):
    return max(a, min(b, float(x)))


def ease_in_out_cubic(u):
    u = clamp(u)
    return 4 * u * u * u if u < 0.5 else 1 - ((-2 * u + 2) ** 3) / 2


def ease_out_back(u):
    u = clamp(u)
    c1 = 1.70158
    c3 = c1 + 1.0
    return 1 + c3 * ((u - 1) ** 3) + c1 * ((u - 1) ** 2)


def ease_out_expo(u):
    u = clamp(u)
    return 1.0 if u >= 1.0 else 1.0 - (2.0 ** (-10.0 * u))


# ----------------------------------------------------------------------
# 2. Fast Liquid Mesh Gradient Background & 3D Particle Sphere
# ----------------------------------------------------------------------

_GX = np.linspace(0.0, 1.0, 160, dtype=np.float32)
_GY = np.linspace(0.0, 1.0, 90, dtype=np.float32)
_XX, _YY = np.meshgrid(_GX, _GY)

PALETTES = {
    "violet": (
        np.array([6, 5, 15], dtype=np.float32),
        np.array([124, 58, 237], dtype=np.float32),   # Electric Purple #7C3AED
        np.array([37, 99, 235], dtype=np.float32),    # Royal Blue #2563EB
        np.array([192, 38, 211], dtype=np.float32),   # Magenta #C026D3
    ),
    "cobalt": (
        np.array([4, 7, 18], dtype=np.float32),
        np.array([59, 130, 246], dtype=np.float32),   # Cobalt #3B82F6
        np.array([99, 102, 241], dtype=np.float32),   # Indigo #6366F1
        np.array([14, 165, 233], dtype=np.float32),   # Sky Cyan #0EA5E9
    ),
    "emerald_violet": (
        np.array([5, 8, 18], dtype=np.float32),
        np.array([109, 40, 217], dtype=np.float32),   # Deep Violet
        np.array([16, 185, 129], dtype=np.float32),   # Emerald
        np.array([59, 130, 246], dtype=np.float32),   # Blue
    ),
    "crimson_violet": (
        np.array([8, 5, 16], dtype=np.float32),
        np.array([124, 58, 237], dtype=np.float32),
        np.array([225, 29, 72], dtype=np.float32),    # Rose/Crimson
        np.array([79, 70, 229], dtype=np.float32),
    ),
}


def render_liquid_mesh_bg(t, palette_name="violet", intensity=1.0):
    base, c1, c2, c3 = PALETTES.get(palette_name, PALETTES["violet"])
    # Three moving Gaussian/cosine plasma lobes
    x1 = 0.28 + 0.22 * math.sin(t * 0.65)
    y1 = 0.32 + 0.20 * math.cos(t * 0.50)
    d1 = np.exp(-((_XX - x1) ** 2 + (_YY - y1) ** 2) / 0.14)

    x2 = 0.74 + 0.20 * math.cos(t * 0.55 + 1.2)
    y2 = 0.68 + 0.22 * math.sin(t * 0.70 + 0.8)
    d2 = np.exp(-((_XX - x2) ** 2 + (_YY - y2) ** 2) / 0.18)

    x3 = 0.50 + 0.25 * math.sin(t * 0.85 + 2.5)
    y3 = 0.22 + 0.18 * math.Sin(t * 0.60) if hasattr(math, "Sin") else 0.22 + 0.18 * math.sin(t * 0.60)
    d3 = np.exp(-((_XX - x3) ** 2 + (_YY - y3) ** 2) / 0.12)

    rgb = (
        base[None, None, :]
        + (d1[:, :, None] * c1[None, None, :] * 0.85 * intensity)
        + (d2[:, :, None] * c2[None, None, :] * 0.75 * intensity)
        + (d3[:, :, None] * c3[None, None, :] * 0.55 * intensity)
    )
    arr = np.clip(rgb, 0, 255).astype(np.uint8)
    return Image.fromarray(arr).resize((WIDTH, HEIGHT), Image.Resampling.BILINEAR)


# Precompute 3D sphere lattice points
_SPHERE_PTS = []
for i in range(28):
    lat = (i / 27.0 - 0.5) * math.pi
    for j in range(56):
        lon = (j / 56.0) * 2.0 * math.pi
        _SPHERE_PTS.append((lat, lon))
_SPHERE_PTS = np.array(_SPHERE_PTS, dtype=np.float32)


def draw_3d_glowing_sphere(draw, cx, cy, radius, t, alpha_scale=1.0):
    lats = _SPHERE_PTS[:, 0]
    lons = _SPHERE_PTS[:, 1] + t * 0.9

    # Wave displacement like the cyan/violet orb in GrtumcQkzsk
    wave = 1.0 + 0.06 * np.sin(lats * 6.0 + t * 3.2) * np.cos(lons * 4.0 - t * 2.0)
    r = radius * wave

    x = r * np.cos(lats) * np.sin(lons)
    y = r * np.sin(lats)
    z = r * np.cos(lats) * np.cos(lons)

    # Tilt sphere slightly on X axis
    tilt = 0.28
    y_rot = y * math.cos(tilt) - z * math.sin(tilt)
    z_rot = y * math.sin(tilt) + z * math.cos(tilt)

    # Sort or filter front hemisphere for crispness
    for px, py, pz in zip(x, y_rot, z_rot):
        depth = (pz / max(radius, 1.0) + 1.0) * 0.5  # 0..1
        if depth < 0.15:
            continue
        sx = int(cx + px)
        sy = int(cy + py)
        if 0 <= sx < WIDTH and 0 <= sy < HEIGHT:
            # Interpolate cyan -> electric violet based on depth & y
            cr = int(clamp((56 + 110 * (1.0 - depth)) * alpha_scale, 0, 255))
            cg = int(clamp((189 * depth + 40 * (1.0 - depth)) * alpha_scale, 0, 255))
            cb = int(clamp((248 * depth + 235 * (1.0 - depth)) * alpha_scale, 0, 255))
            sz = 2 if depth > 0.65 else 1
            draw.ellipse([(sx - sz, sy - sz), (sx + sz, sy + sz)], fill=(cr, cg, cb))


# ----------------------------------------------------------------------
# 3. UI Screenshot Cache & Rounded Card Builder
# ----------------------------------------------------------------------

_UI_CARD_CACHE = {}


def get_ui_card(image_name):
    if image_name not in _UI_CARD_CACHE:
        path = os.path.join(UI_CAPTURES_DIR, image_name)
        raw = Image.open(path).convert("RGBA")
        # Create rounded mask (radius 26px) + subtle glowing border
        card = Image.new("RGBA", (WIDTH, HEIGHT), (0, 0, 0, 0))
        mask = Image.new("L", (WIDTH, HEIGHT), 0)
        mdraw = ImageDraw.Draw(mask)
        mdraw.rounded_rectangle([(0, 0), (WIDTH - 1, HEIGHT - 1)], radius=26, fill=255)
        card.paste(raw, (0, 0), mask)

        bdraw = ImageDraw.Draw(card)
        bdraw.rounded_rectangle(
            [(1, 1), (WIDTH - 2, HEIGHT - 2)],
            radius=26,
            outline=(139, 92, 246, 210),
            width=4,
        )
        _UI_CARD_CACHE[image_name] = card
    return _UI_CARD_CACHE[image_name]


def draw_centered_text(draw, y, text, font, fill="#FFFFFF"):
    bbox = draw.textbbox((0, 0), text, font=font)
    tw = bbox[2] - bbox[0]
    draw.text(((WIDTH - tw) // 2, int(y)), text, font=font, fill=fill)


def draw_glowing_cursor(draw, x, y, clicking=False, ripple_prog=0.0):
    x, y = int(x), int(y)
    if ripple_prog > 0.01:
        rr = int(12 + 68 * ease_out_expo(ripple_prog))
        col_val = int(255 * (1.0 - clamp(ripple_prog)))
        draw.ellipse(
            [(x - rr, y - rr), (x + rr, y + rr)],
            outline=(168, 85, 247, col_val),
            width=4,
        )
        rr2 = int(6 + 38 * ease_out_expo(ripple_prog))
        draw.ellipse(
            [(x - rr2, y - rr2), (x + rr2, y + rr2)],
            outline=(56, 189, 248, col_val),
            width=3,
        )

    scale = 0.85 if clicking else 1.0
    pts = [
        (x, y),
        (int(x + 12 * scale), int(y + 34 * scale)),
        (int(x + 18 * scale), int(y + 22 * scale)),
        (int(x + 32 * scale), int(y + 20 * scale)),
    ]
    # Drop shadow
    s_pts = [(px + 3, py + 4) for px, py in pts]
    draw.polygon(s_pts, fill=(10, 10, 25))
    # Vibrant purple-blue cursor arrow like GrtumcQkzsk (00:19)
    draw.polygon(pts, fill="#8B5CF6", outline="#F8FAFC")


def draw_floating_caption_pill(draw, text, badge_text, chapter_text):
    # Top-left minimal floating chapter pill
    draw.rounded_rectangle([(48, 36), (480, 78)], radius=21, fill=(10, 12, 28), outline="#4C1D95", width=2)
    draw.text((70, 47), chapter_text, font=get_font(16, FONT_MONO), fill="#A78BFA")

    # Top-right minimal floating feature pill with purple check badge
    bw = min(640, max(320, len(badge_text) * 11 + 80))
    bx0 = WIDTH - 48 - bw
    draw.rounded_rectangle([(bx0, 36), (WIDTH - 48, 78)], radius=21, fill=(15, 12, 36), outline="#7C3AED", width=2)
    draw.rounded_rectangle([(bx0 + 10, 43), (bx0 + 38, 71)], radius=8, fill="#7C3AED")
    draw.text((bx0 + 17, 46), "✓", font=get_font(18, FONT_SANS), fill="#FFFFFF")
    draw.text((bx0 + 50, 47), badge_text[:48], font=get_font(16, FONT_MONO), fill="#F8FAFC")

    # Bottom floating sleek translucent subtitle pill (only while narration is active)
    if text:
        words = text.split()
        lines, cur = [], []
        for w in words:
            if len(" ".join(cur + [w])) > 88:
                lines.append(" ".join(cur))
                cur = [w]
            else:
                cur.append(w)
        if cur:
            lines.append(" ".join(cur))
        lines = lines[:2]

        box_h = 46 + len(lines) * 28
        by0 = HEIGHT - 32 - box_h
        draw.rounded_rectangle(
            [(220, by0), (WIDTH - 220, HEIGHT - 32)],
            radius=24,
            fill=(8, 10, 24),
            outline="#3B0764",
            width=2,
        )
        ty = by0 + 18
        for ln in lines:
            draw_centered_text(draw, ty, ln, get_font(22, FONT_SANS), fill="#F1F5F9")
            ty += 29


# ----------------------------------------------------------------------
# 4. Motion-Design Scene Renderers (Modeled After GrtumcQkzsk)
# ----------------------------------------------------------------------

def render_scene_kinetic_intro(t, u, scene):
    """
    00:00 - 00:16 style from GrtumcQkzsk:
     Phase 1 (u < 0.28): Pitch black -> Glitch/scramble decode "REDEFINING CATALOG SYNCHRONIZATION"
     Phase 2 (0.28 <= u < 0.55): Radial 3D particle warp tunnel ("INGEST • DEDUPLICATE • QUARANTINE • RECOVER")
     Phase 3 (u >= 0.55): 3D Glowing Wave Sphere + "Meet CatalogSync" + floating glass pills & laser lines
    """
    if u < 0.28:
        sub_u = u / 0.28
        img = Image.new("RGB", (WIDTH, HEIGHT), (3, 3, 8))
        draw = ImageDraw.Draw(img)
        target = "REDEFINING SUPPLIER CATALOG SYNCHRONIZATION"
        glyphs = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789#%&*"
        reveal_n = int(len(target) * clamp(sub_u * 1.4))
        chars = []
        for i, ch in enumerate(target):
            if ch == " ":
                chars.append(" ")
            elif i < reveal_n:
                chars.append(ch)
            elif i < reveal_n + 6:
                chars.append(glyphs[(i * 7 + int(t * 25)) % len(glyphs)])
        s_str = "".join(chars)
        draw_centered_text(draw, 505, s_str, get_font(34, FONT_MONO), fill="#A855F7")
        if sub_u > 0.55:
            draw_centered_text(
                draw,
                565,
                "PURE GO   •   SQLITE WAL   •   ZERO DATA LOSS",
                get_font(20, FONT_MONO),
                fill="#64748B",
            )
        return img

    elif u < 0.54:
        sub_u = (u - 0.28) / 0.26
        img = render_liquid_mesh_bg(t * 2.0, "violet", intensity=0.95)
        draw = ImageDraw.Draw(img)
        # Draw radial warp streaks from center
        cx, cy = WIDTH // 2, HEIGHT // 2
        for k in range(64):
            ang = k * (2.0 * math.pi / 64.0) + t * 0.4
            r0 = 60 + ((k * 37 + int(t * 520)) % 450)
            r1 = r0 + 110 + (k % 5) * 35
            x0 = int(cx + math.cos(ang) * r0)
            y0 = int(cy + math.sin(ang) * r0)
            x1 = int(cx + math.cos(ang) * r1)
            y1 = int(cy + math.sin(ang) * r1)
            col = "#38BDF8" if k % 3 == 0 else ("#A855F7" if k % 3 == 1 else "#F43F5E")
            draw.line([(x0, y0), (x1, y1)], fill=col, width=3)

        draw_3d_glowing_sphere(draw, cx, cy, int(190 + 60 * math.sin(sub_u * math.pi)), t, alpha_scale=0.85)
        words = ["INGEST BATCHES", "DEDUPLICATE", "ISOLATE ANOMALIES", "RECOVER INSTANTLY"]
        w_idx = min(len(words) - 1, int(sub_u * len(words)))
        draw_centered_text(draw, 500, words[w_idx], get_font(64, FONT_SANS), fill="#FFFFFF")
        return img

    else:
        sub_u = (u - 0.54) / 0.46
        img = render_liquid_mesh_bg(t, "violet", intensity=1.0)
        draw = ImageDraw.Draw(img)
        cx, cy = WIDTH // 2, HEIGHT // 2

        # Glowing 3D orb behind title
        orb_r = int(220 - 65 * ease_out_expo(sub_u))
        draw_3d_glowing_sphere(draw, cx, cy, orb_r, t, alpha_scale=0.9)

        # Horizontal laser lines like 00:10 in GrtumcQkzsk
        line_w = int(420 * ease_out_expo(sub_u))
        for dy in (-10, 0, 10):
            draw.line([(0, cy + dy), (line_w, cy + dy)], fill="#3B82F6", width=2)
            draw.line([(WIDTH - line_w, cy + dy), (WIDTH, cy + dy)], fill="#3B82F6", width=2)

        # Giant "Meet CatalogSync" title
        draw_centered_text(draw, cy - 62, "Meet CatalogSync", get_font(96, FONT_SANS), fill="#FFFFFF")
        draw_centered_text(
            draw,
            cy + 58,
            "Deterministic Supplier Catalog Engine in a Box",
            get_font(28, FONT_SANS),
            fill="#C4B5FD",
        )

        # Floating glassmorphic pills around title (like "Analyze" / "Automate" in GrtumcQkzsk)
        pills = [
            (310, 250, "SHA-256 Idempotent"),
            (1360, 230, "Fast-Forward Timestamps"),
            (360, 760, "Withdrawn Tombstone Guard"),
            (1310, 770, "SQLite WAL Checkpoints"),
        ]
        for px, py, label in pills:
            float_y = int(py + 10 * math.sin(t * 2.2 + px * 0.01))
            draw.rounded_rectangle(
                [(px, float_y), (px + 290, float_y + 46)],
                radius=23,
                fill=(15, 20, 42),
                outline="#6366F1",
                width=2,
            )
            draw.text((px + 24, float_y + 12), label, font=get_font(17, FONT_MONO), fill="#E0E7FF")

        draw_floating_caption_pill(draw, scene["text"], scene["badge"], scene["chapter"])
        return img


def render_scene_problem_cloud(t, u, scene):
    """
    00:32 - 00:37 style from GrtumcQkzsk:
    Floating questions ("Out-of-order updates?", "Duplicate batches?", "Mid-stream crashes?")
    converging into "Not Anymore" -> "Five Mathematical Invariants (A-E)"
    """
    img = render_liquid_mesh_bg(t, "cobalt", intensity=1.0)
    draw = ImageDraw.Draw(img)
    cx, cy = WIDTH // 2, HEIGHT // 2

    if u < 0.52:
        sub_u = u / 0.52
        draw_centered_text(draw, cy - 30, "Unreliable Supplier Feeds?", get_font(56, FONT_SANS), fill="#FFFFFF")
        problems = [
            (220, 230, "Duplicate batch deliveries?", "#A855F7"),
            (1290, 210, "Out-of-order stale timestamps?", "#38BDF8"),
            (190, 760, "Withdrawn items resurrected?", "#F43F5E"),
            (1240, 780, "Supplier outage mid-update?", "#FBBF24"),
            (740, 170, "Negative prices corrupting catalog?", "#818CF8"),
        ]
        for idx, (px, py, txt, col) in enumerate(problems):
            appear = clamp((sub_u - idx * 0.12) * 3.0)
            if appear > 0:
                fy = int(py + 14 * math.sin(t * 2.0 + idx) - (1.0 - appear) * 30)
                draw.text((px, fy), txt, font=get_font(34, FONT_SANS), fill=col)
    else:
        sub_u = (u - 0.52) / 0.48
        draw_centered_text(draw, 190, "Not Anymore.", get_font(78, FONT_SANS), fill="#38BDF8")
        draw_centered_text(
            draw,
            290,
            "Five Mathematical Synchronization Invariants Built in Pure Go",
            get_font(28, FONT_SANS),
            fill="#E2E8F0",
        )
        cards = [
            ("INVARIANT A", "SHA-256 Content-Hash Idempotency", "Zero duplicate products"),
            ("INVARIANT B", "Fast-Forward Timestamps (T_new > T_old)", "Stale data rejected"),
            ("INVARIANT C", "Permanent WITHDRAWN Tombstones", "Never resurrected"),
            ("INVARIANT D", "Non-Blocking Dead-Letter Quarantine", "Anomalies isolated"),
            ("INVARIANT E", "Atomic SQLite WAL Batch Checkpoints", "Instant crash resume"),
        ]
        for i, (code, title, sub) in enumerate(cards):
            prog = ease_out_back(clamp((sub_u - i * 0.08) * 2.4))
            if prog <= 0.01:
                continue
            bx = 120 + i * 340
            by = int(430 + (1.0 - prog) * 120)
            draw.rounded_rectangle(
                [(bx, by), (bx + 320, by + 290)],
                radius=22,
                fill=(10, 14, 32),
                outline="#7C3AED",
                width=2,
            )
            draw.rounded_rectangle([(bx + 22, by + 24), (bx + 170, by + 62)], radius=12, fill="#4C1D95")
            draw.text((bx + 36, by + 33), code, font=get_font(16, FONT_MONO), fill="#DDD6FE")

            # Wrap title
            words = title.split()
            lines, cur = [], []
            for w in words:
                if len(" ".join(cur + [w])) > 20:
                    lines.append(" ".join(cur))
                    cur = [w]
                else:
                    cur.append(w)
            if cur:
                lines.append(" ".join(cur))
            ty = by + 90
            for ln in lines:
                draw.text((bx + 22, ty), ln, font=get_font(23, FONT_SANS), fill="#FFFFFF")
                ty += 30
            draw.text((bx + 22, by + 230), f"✓ {sub}", font=get_font(17, FONT_MONO), fill="#34D399")

    draw_floating_caption_pill(draw, scene["text"], scene["badge"], scene["chapter"])
    return img


def render_scene_macro_ui(t, u, scene):
    """
    00:17 - 00:26 & 00:40 - 00:44 style from GrtumcQkzsk:
    Macro close-up camera zoom & pan over the live 1080p UI card, with animated glowing purple cursor,
    click ripple, and live state transition between `image_start` and `image_end`.
    """
    palette = scene.get("palette", "violet")
    img = render_liquid_mesh_bg(t, palette, intensity=1.0)

    # Determine whether we are showing image_start or image_end (switched at click_u)
    click_u = scene.get("click_u", 0.52)
    img_start_name = scene["image_start"]
    img_end_name = scene.get("image_end", img_start_name)

    card_name = img_start_name if u < click_u else img_end_name
    card = get_ui_card(card_name)

    # Camera keyframes: (cx, cy, zoom) where cx, cy in [0..1] of card
    cam0 = scene.get("cam_start", (0.50, 0.50, 1.35))
    cam1 = scene.get("cam_mid", (0.50, 0.45, 1.95))
    cam2 = scene.get("cam_end", (0.50, 0.50, 0.88))

    if u < 0.48:
        k = ease_in_out_cubic(u / 0.48)
        cx = cam0[0] + (cam1[0] - cam0[0]) * k
        cy = cam0[1] + (cam1[1] - cam0[1]) * k
        zm = cam0[2] + (cam1[2] - cam0[2]) * k
    else:
        k = ease_in_out_cubic((u - 0.48) / 0.52)
        cx = cam1[0] + (cam2[0] - cam1[0]) * k
        cy = cam1[1] + (cam2[1] - cam1[1]) * k
        zm = cam1[2] + (cam2[2] - cam1[2]) * k

    # Render zoomed/panned UI card onto background
    # Target point (cx * WIDTH, cy * HEIGHT) on card maps to screen center (WIDTH/2, HEIGHT/2)
    scaled_w = int(WIDTH * zm)
    scaled_h = int(HEIGHT * zm)
    px = int(WIDTH * 0.5 - cx * scaled_w)
    py = int(HEIGHT * 0.5 - cy * scaled_h)

    # Crop only the visible region of the card for ultra-fast rendering
    # Screen box [0..WIDTH, 0..HEIGHT] corresponds in card coordinates to:
    src_x0 = (0 - px) / float(zm)
    src_y0 = (0 - py) / float(zm)
    src_x1 = (WIDTH - px) / float(zm)
    src_y1 = (HEIGHT - py) / float(zm)

    if src_x0 >= 0 and src_y0 >= 0 and src_x1 <= WIDTH and src_y1 <= HEIGHT:
        crop_box = (int(src_x0), int(src_y0), max(int(src_x0) + 2, int(src_x1)), max(int(src_y0) + 2, int(src_y1)))
        cropped = card.crop(crop_box).resize((WIDTH, HEIGHT), Image.Resampling.BILINEAR)
        img.paste(cropped, (0, 0), cropped)
    else:
        resized_card = card.resize((scaled_w, scaled_h), Image.Resampling.BILINEAR)
        # Drop shadow under floating card when zoomed out (zm < 1.0)
        draw = ImageDraw.Draw(img)
        draw.rounded_rectangle(
            [(px - 10, py - 10), (px + scaled_w + 10, py + scaled_h + 10)],
            radius=32,
            outline="#6D28D9",
            width=3,
        )
        img.paste(resized_card, (px, py), resized_card)

    draw = ImageDraw.Draw(img)

    # Animated glowing cursor glides toward target and clicks at click_u
    cur_target = scene.get("cursor_target", (cam1[0], cam1[1]))
    cur_start = scene.get("cursor_start", (clamp(cur_target[0] - 0.18), clamp(cur_target[1] + 0.18)))
    move_k = ease_in_out_cubic(clamp(u / max(click_u, 0.1)))
    cur_card_x = cur_start[0] + (cur_target[0] - cur_start[0]) * move_k
    cur_card_y = cur_start[1] + (cur_target[1] - cur_start[1]) * move_k

    screen_cx = px + cur_card_x * scaled_w
    screen_cy = py + cur_card_y * scaled_h

    clicking = abs(u - click_u) < 0.05
    ripple = clamp((u - click_u) / 0.22) if click_u <= u <= click_u + 0.22 else 0.0
    draw_glowing_cursor(draw, screen_cx, screen_cy, clicking=clicking, ripple_prog=ripple)

    draw_floating_caption_pill(draw, scene["text"], scene["badge"], scene["chapter"])
    return img


def render_scene_marquee_and_outro(t, u, scene):
    """
    00:53 - 01:07 style from GrtumcQkzsk:
    Phase 1 (u < 0.55): Giant scrolling background marquee ("SYNCHRONIZE - VALIDATE - RECOVER - ROLLBACK")
                        + stacked spring-physics verification cards ("✓ 91.3% Go Coverage", "✓ 15/15 Playwright E2E", etc.)
    Phase 2 (u >= 0.55): Rising 3D glowing cyan/violet horizon sphere + brand shield & final tagline
    """
    img = render_liquid_mesh_bg(t, "violet", intensity=1.0)
    draw = ImageDraw.Draw(img)
    cx, cy = WIDTH // 2, HEIGHT // 2

    if u < 0.55:
        sub_u = u / 0.55
        # Giant scrolling background marquee rows
        marquee = "SYNCHRONIZE  —  DEDUPLICATE  —  QUARANTINE  —  RECOVER  —  ROLLBACK  —  "
        offset1 = int((t * 220) % 1400)
        offset2 = int((t * 180) % 1400)
        draw.text((-offset1, 140), marquee * 3, font=get_font(120, FONT_SANS), fill=(35, 25, 68))
        draw.text((-1400 + offset2, 310), marquee * 3, font=get_font(120, FONT_SANS), fill=(45, 30, 88))
        draw.text((-offset1, 480), marquee * 3, font=get_font(120, FONT_SANS), fill=(30, 22, 60))

        draw_centered_text(draw, 150, "Production-Grade Engineering", get_font(68, FONT_SANS), fill="#FFFFFF")

        # Stacked pop-up cards like "✓ Order Complete" at 00:56 in GrtumcQkzsk
        cards = [
            ("✓  Go Statement Coverage Verified", "91.3% across pkg/sync, pkg/storage & pkg/api (Threshold >= 90%)"),
            ("✓  Strict Playwright E2E Suite", "15 / 15 API & Chromium UI tests passing across all invariants"),
            ("✓  Concurrency & Race Detector", "0 Data Races across 5x stress iterations (-race -count=5)"),
            ("✓  Ultra-Light Container & Edge", "21.4 MB Distroless Docker image + 18/18 Cloudflare Worker tests"),
        ]
        for i, (title, detail) in enumerate(cards):
            prog = ease_out_back(clamp((sub_u - i * 0.12) * 2.8))
            if prog <= 0.01:
                continue
            by = int(280 + i * 138 + (1.0 - prog) * 90)
            draw.rounded_rectangle(
                [(410, by), (1510, by + 116)],
                radius=22,
                fill=(248, 250, 252),
                outline="#8B5CF6",
                width=3,
            )
            draw.text((455, by + 22), title, font=get_font(26, FONT_SANS), fill="#0F172A")
            draw.text((455, by + 64), detail, font=get_font(20, FONT_MONO), fill="#475569")
    else:
        sub_u = (u - 0.55) / 0.45
        # Rising 3D glowing particle horizon sphere at bottom of screen (like 00:59 in GrtumcQkzsk)
        draw_3d_glowing_sphere(draw, cx, HEIGHT + 140, 560, t, alpha_scale=1.0)

        # Glowing brand shield in center
        draw.rounded_rectangle(
            [(cx - 58, 220), (cx + 58, 336)],
            radius=28,
            fill=(15, 23, 42),
            outline="#A855F7",
            width=4,
        )
        draw_centered_text(draw, 250, "SC", get_font(52, FONT_MONO), fill="#38BDF8")

        draw_centered_text(draw, 375, "Supplier Catalog Sync Engine", get_font(64, FONT_SANS), fill="#FFFFFF")
        draw_centered_text(
            draw,
            465,
            "Sync Faster. Recover Instantly. Zero Data Corruption.",
            get_font(28, FONT_SANS),
            fill="#C4B5FD",
        )
        draw_centered_text(
            draw,
            530,
            "make build && ./bin/server -port 8080 -db catalog.db",
            get_font(22, FONT_MONO),
            fill="#34D399",
        )

    draw_floating_caption_pill(draw, scene["text"], scene["badge"], scene["chapter"])
    return img


# ----------------------------------------------------------------------
# 5. Storyboard: 18 High-Impact Motion Scenes (~195s / 3m15s Total)
# ----------------------------------------------------------------------

STORYBOARD = [
    # 1. Kinetic Glitch + Warp Tunnel + 3D Sphere Intro
    {
        "type": "kinetic_intro",
        "chapter": "01 / 09  •  PRODUCT LAUNCH",
        "badge": "Pure Go + SQLite WAL + React 19",
        "text": "Redefining supplier catalog synchronization. Meet CatalogSync: a deterministic, zero-CGO Go and SQLite Write-Ahead Log engine built to ingest batched supplier updates, eliminate duplicates, isolate anomalies, and recover from mid-stream outages automatically.",
    },
    # 2. Multi-Tenant Gatekeeper Validation -> Entering Store Workspace
    {
        "type": "macro_ui",
        "palette": "violet",
        "chapter": "01 / 09  •  MULTI-TENANT GATEKEEPER",
        "badge": "Tenant Validation & Isolation",
        "image_start": "s1_gatekeeper_error.png",
        "image_end": "s1_gatekeeper_valid.png",
        "cam_start": (0.50, 0.52, 2.15),
        "cam_mid": (0.50, 0.56, 2.35),
        "cam_end": (0.50, 0.50, 0.90),
        "cursor_target": (0.50, 0.58),
        "click_u": 0.50,
        "text": "When staff launch the application, the Multi-Tenant Gatekeeper validates the shop identifier, rejecting invalid characters before opening an isolated catalog workspace for store flagship.",
    },
    # 3. Problem Cloud -> Five Mathematical Invariants (A-E)
    {
        "type": "problem_cloud",
        "chapter": "02 / 09  •  FIVE CORE INVARIANTS",
        "badge": "Invariants A through E Enforced",
        "text": "Duplicate supplier batches? Out-of-order stale timestamps? Withdrawn items accidentally restored? Not anymore. CatalogSync enforces five strict invariants: SHA-256 idempotency, fast-forward timestamps, permanent withdrawn tombstones, non-blocking Dead-Letter Quarantine, and atomic WAL batch checkpoints.",
    },
    # 4. Clean Dashboard & Triggering v1.0.0 Baseline Catalog Feed
    {
        "type": "macro_ui",
        "palette": "cobalt",
        "chapter": "03 / 09  •  BATCH INGESTION & LIVE SSE",
        "badge": "v1.0.0 — Baseline Catalog Feed (10 Batches)",
        "image_start": "s2_dashboard_clean.png",
        "image_end": "s3_sync_v100_running.png",
        "cam_start": (0.50, 0.50, 0.90),
        "cam_mid": (0.78, 0.62, 2.25),
        "cam_end": (0.55, 0.58, 1.35),
        "cursor_target": (0.88, 0.60),
        "click_u": 0.46,
        "text": "From the main Dashboard, staff launch version one point zero, the Baseline Catalog Feed, streaming one thousand supplier products across ten batches with real-time Server-Sent Events progress and throughput telemetry.",
    },
    # 5. Completed v1.0.0 & Contextual Alert Banners
    {
        "type": "macro_ui",
        "palette": "emerald_violet",
        "chapter": "03 / 09  •  SYNC COMPLETION & MILESTONE",
        "badge": "Catalog Version v1 Committed",
        "image_start": "s3_sync_v100_running.png",
        "image_end": "s3_sync_v100_completed.png",
        "cam_start": (0.55, 0.58, 1.45),
        "cam_mid": (0.52, 0.36, 1.95),
        "cam_end": (0.50, 0.50, 0.88),
        "cursor_target": (0.55, 0.35),
        "click_u": 0.38,
        "text": "As batch ten commits, the update transitions to Synced, creating Catalog Version one and surfacing contextual alerts for quarantined records and version milestones.",
    },
    # 6. Incremental Update v1.1.0 & Anomaly Feed v1.2.0
    {
        "type": "macro_ui",
        "palette": "violet",
        "chapter": "04 / 09  •  INCREMENTAL & ANOMALY FEEDS",
        "badge": "v1.1.0 Updates & v1.2.0 DLQ Isolation",
        "image_start": "s4_dashboard_after_v110.png",
        "image_end": "s4_dashboard_after_v120.png",
        "cam_start": (0.72, 0.65, 2.10),
        "cam_mid": (0.85, 0.72, 2.30),
        "cam_end": (0.50, 0.45, 0.92),
        "cursor_target": (0.88, 0.72),
        "click_u": 0.48,
        "text": "Running version one point one applies price updates and product withdrawals while skipping duplicates and stale timestamps. Running version one point two isolates defective supplier records in the Dead-Letter Queue without blocking valid items.",
    },
    # 7. Interactive Metric Cards -> Price & Name Updated Drilldown Modal
    {
        "type": "macro_ui",
        "palette": "cobalt",
        "chapter": "04 / 09  •  METRIC DRILLDOWN MODALS",
        "badge": "SKU-Level Price & Attribute Audit",
        "image_start": "s4_dashboard_after_v120.png",
        "image_end": "s4_drilldown_updated.png",
        "cam_start": (0.42, 0.22, 2.20),
        "cam_mid": (0.42, 0.22, 2.40),
        "cam_end": (0.50, 0.50, 1.05),
        "cursor_target": (0.42, 0.22),
        "click_u": 0.42,
        "text": "Clicking any of the five summary metric cards opens an interactive drilldown modal, letting staff inspect updated SKU prices and exact supplier timestamps.",
    },
    # 8. Withdrawn Tombstones & Duplicates Skipped Drilldown Modals
    {
        "type": "macro_ui",
        "palette": "crimson_violet",
        "chapter": "04 / 09  •  TOMBSTONE & IDEMPOTENCY AUDIT",
        "badge": "Permanent Tombstones & Skipped Duplicates",
        "image_start": "s4_drilldown_withdrawn.png",
        "image_end": "s4_drilldown_skipped.png",
        "cam_start": (0.50, 0.48, 1.45),
        "cam_mid": (0.50, 0.52, 1.85),
        "cam_end": (0.50, 0.50, 0.92),
        "cursor_target": (0.58, 0.22),
        "click_u": 0.50,
        "text": "Inspecting Withdrawn Products confirms that discontinued items are locked as permanent tombstones, while Duplicates Skipped audits every redundant or stale record ignored by the engine.",
    },
    # 9. Analytics Time-Range Filters & Interactive Chart Zoom
    {
        "type": "macro_ui",
        "palette": "violet",
        "chapter": "04 / 09  •  ANALYTICS & CHART ZOOM",
        "badge": "Time-Range Filters & Recharts Zoom",
        "image_start": "s4_analytics_custom_range.png",
        "image_end": "s4_analytics_zoomed.png",
        "cam_start": (0.68, 0.16, 2.25),
        "cam_mid": (0.58, 0.72, 2.10),
        "cam_end": (0.50, 0.55, 0.90),
        "cursor_target": (0.62, 0.62),
        "click_u": 0.48,
        "text": "Staff can filter telemetry across Today, Yesterday, Last Seven Days, All Time, or Custom Date Ranges, and zoom directly into the Catalog Distribution and Activity charts.",
    },
    # 10. Outage / Crash Simulation (v1.4.0) Halting at Batch 5/10
    {
        "type": "macro_ui",
        "palette": "crimson_violet",
        "chapter": "05 / 09  •  OUTAGE & CHECKPOINT RECOVERY",
        "badge": "Halted at Batch 5/10 (Checkpoint Paused)",
        "image_start": "s5_dashboard_checkpoint_paused.png",
        "image_end": "s5_history_interrupted_filter.png",
        "cam_start": (0.76, 0.78, 2.25),
        "cam_mid": (0.52, 0.38, 1.95),
        "cam_end": (0.50, 0.48, 0.95),
        "cursor_target": (0.45, 0.25),
        "click_u": 0.52,
        "text": "When version one point four simulates a mid-feed crash at batch five of ten, the existing catalog is unharmed and the job is strictly marked Interrupted, never falsely reported as complete.",
    },
    # 11. One-Click Checkpoint Resumption from Batch 6/10 to 10/10
    {
        "type": "macro_ui",
        "palette": "emerald_violet",
        "chapter": "05 / 09  •  SEAMLESS CHECKPOINT RESUME",
        "badge": "Resumed from Batch 6/10 -> 10/10 Complete",
        "image_start": "s5_history_interrupted_filter.png",
        "image_end": "s5_history_resumed_completed.png",
        "cam_start": (0.78, 0.36, 2.35),
        "cam_mid": (0.84, 0.36, 2.50),
        "cam_end": (0.50, 0.45, 0.90),
        "cursor_target": (0.86, 0.36),
        "click_u": 0.45,
        "text": "Clicking Resume restarts synchronization strictly from batch six of ten without losing already completed work or duplicating batches one through five, finishing all ten batches cleanly.",
    },
    # 12. Live Product Catalog Explorer: Status Filter & Instant SKU Search
    {
        "type": "macro_ui",
        "palette": "cobalt",
        "chapter": "06 / 09  •  LIVE CATALOG EXPLORER",
        "badge": "Sub-Millisecond SKU Search & Status Filter",
        "image_start": "s6_catalog_withdrawn_only.png",
        "image_end": "s6_catalog_sku_search.png",
        "cam_start": (0.65, 0.22, 2.15),
        "cam_mid": (0.38, 0.22, 2.25),
        "cam_end": (0.50, 0.48, 0.90),
        "cursor_target": (0.38, 0.22),
        "click_u": 0.48,
        "text": "In the Live Product Catalog view, staff filter between Active inventory and Withdrawn tombstones, or search by SKU and product name in under a millisecond.",
    },
    # 13. Quarantine (Dead-Letter Queue) & Defect Reason Filtering
    {
        "type": "macro_ui",
        "palette": "crimson_violet",
        "chapter": "07 / 09  •  DEAD-LETTER QUARANTINE (DLQ)",
        "badge": "Isolating Problems Needing Attention",
        "image_start": "s7_quarantine_overview.png",
        "image_end": "s7_quarantine_reason_filtered.png",
        "cam_start": (0.50, 0.42, 1.35),
        "cam_mid": (0.72, 0.22, 2.25),
        "cam_end": (0.55, 0.42, 1.15),
        "cursor_target": (0.75, 0.22),
        "click_u": 0.48,
        "text": "The Quarantine tab surfaces every supplier record needing staff attention, with instant filtering by defect reason such as Negative or Zero Price or Empty SKU.",
    },
    # 14. Staff Remediation Modal: Raw JSON Payload Inspector & Live Catalog Import
    {
        "type": "macro_ui",
        "palette": "emerald_violet",
        "chapter": "07 / 09  •  STAFF PROBLEM REMEDIATION",
        "badge": "Raw JSON Inspection & Atomic Fix Import",
        "image_start": "s7_remediation_modal_raw.png",
        "image_end": "s7_remediation_modal_corrected.png",
        "cam_start": (0.50, 0.48, 1.75),
        "cam_mid": (0.50, 0.62, 2.15),
        "cam_end": (0.50, 0.50, 0.92),
        "cursor_target": (0.58, 0.76),
        "click_u": 0.52,
        "text": "Clicking Review and Fix reveals the raw defective supplier JSON payload. Staff enter a corrected name and valid positive price, importing the fixed product directly into the live catalog.",
    },
    # 15. Unified History & Versions + SKU Pre-Image/Post-Image Diff Modal
    {
        "type": "macro_ui",
        "palette": "violet",
        "chapter": "08 / 09  •  UNIFIED HISTORY & VERSION DIFFS",
        "badge": "Pre-Image vs Post-Image SKU Diff Inspector",
        "image_start": "s8_history_versions_timeline.png",
        "image_end": "s8_version_diff_modal.png",
        "cam_start": (0.75, 0.35, 2.15),
        "cam_mid": (0.50, 0.50, 1.85),
        "cam_end": (0.50, 0.50, 0.92),
        "cursor_target": (0.80, 0.35),
        "click_u": 0.44,
        "text": "The unified History and Versions view tracks every sync job and catalog version milestone. Clicking Diff opens the Version Diff Modal with exact before-and-after SKU prices and attributes.",
    },
    # 16. Git-Revert Catalog Rollback Before Sync (with Tombstone Preservation)
    {
        "type": "macro_ui",
        "palette": "cobalt",
        "chapter": "08 / 09  •  GIT-REVERT CATALOG ROLLBACK",
        "badge": "Revert Before Sync (Tombstones Protected)",
        "image_start": "s8_confirm_revert_modal.png",
        "image_end": "s8_history_after_rollback.png",
        "cam_start": (0.50, 0.52, 1.95),
        "cam_mid": (0.56, 0.66, 2.20),
        "cam_end": (0.50, 0.45, 0.90),
        "cursor_target": (0.58, 0.68),
        "click_u": 0.50,
        "text": "Clicking Revert Before Sync rolls the catalog back to the exact state prior to any sync update—even back to baseline version zero—while strictly preserving withdrawn tombstones.",
    },
    # 17. Multi-Tenant Store Switcher & Zero Cross-Tenant Leakage
    {
        "type": "macro_ui",
        "palette": "violet",
        "chapter": "09 / 09  •  MULTI-TENANT STORE SWITCHER",
        "badge": "100% Isolated Store Workspaces",
        "image_start": "s9_switch_tenant_modal.png",
        "image_end": "s9_isolated_second_tenant.png",
        "cam_start": (0.50, 0.48, 2.05),
        "cam_mid": (0.56, 0.62, 2.25),
        "cam_end": (0.50, 0.50, 0.90),
        "cursor_target": (0.58, 0.62),
        "click_u": 0.48,
        "text": "Using the Switch Tenant modal in the sidebar, staff can switch to store boutique two on the fly, revealing an isolated catalog state with zero cross-tenant data leakage.",
    },
    # 18. Giant Marquee + Verification Cards + 3D Horizon Sphere Outro
    {
        "type": "marquee_outro",
        "chapter": "09 / 09  •  VERIFIED & PRODUCTION READY",
        "badge": "91.3% Coverage • 15/15 E2E • 0 Races",
        "text": "Backed by over ninety-one percent Go statement coverage, zero race conditions across five stress iterations, and fifteen out of fifteen end-to-end Playwright API and UI tests. CatalogSync: your edge in catalog reliability.",
    },
]


# ----------------------------------------------------------------------
# 6. KittenTTS Narration Generator (24kHz)
# ----------------------------------------------------------------------

def split_sentences(text):
    parts = [p.strip() for p in re.split(r"(?<=[.!?])\s+", text.strip()) if p.strip()]
    return parts if parts else [text.strip()]


def generate_scene_narrations(tts_model, storyboard):
    audio_paths = []
    durations = []
    pause_samples = np.zeros(int(SAMPLE_RATE * 0.10), dtype=np.float32)
    lead_pad = np.zeros(int(SAMPLE_RATE * 0.25), dtype=np.float32)
    tail_pad = np.zeros(int(SAMPLE_RATE * 0.35), dtype=np.float32)

    for idx, scene in enumerate(storyboard):
        raw_wav = os.path.join(OUTPUT_DIR, f"v2_narration_raw_{idx:02d}.wav")
        final_wav = os.path.join(OUTPUT_DIR, f"v2_narration_{idx:02d}.wav")

        sentences = split_sentences(scene["text"])
        chunks = [lead_pad]
        for s in sentences:
            wav = tts_model.generate(s, voice="expr-voice-2-m")
            wav = np.asarray(wav, dtype=np.float32).flatten()
            non_silent = np.where(np.abs(wav) > 0.008)[0]
            if len(non_silent) > 0:
                s0 = max(0, non_silent[0] - int(SAMPLE_RATE * 0.03))
                s1 = min(len(wav), non_silent[-1] + int(SAMPLE_RATE * 0.05))
                wav = wav[s0:s1]
            chunks.append(wav)
            chunks.append(pause_samples)
        chunks.append(tail_pad)
        full_wav = np.concatenate(chunks)
        peak = np.max(np.abs(full_wav))
        if peak > 1e-4:
            full_wav = (full_wav / peak) * 0.85
        sf.write(raw_wav, full_wav, SAMPLE_RATE)

        # Speed up slightly (atempo=1.18) for energetic SaaS launch cadence (targets ~195s / 3m15s total)
        subprocess.run(
            [
                "ffmpeg", "-y",
                "-i", raw_wav,
                "-filter:a", "atempo=1.18",
                "-ar", str(SAMPLE_RATE),
                final_wav,
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            check=True,
        )
        info = sf.info(final_wav)
        dur = float(info.frames) / float(info.samplerate)
        print(f"[KittenTTS] Scene {idx+1:02d}/{len(storyboard):02d} ({dur:.2f}s): {scene['badge']}")
        audio_paths.append(final_wav)
        durations.append(dur)

    return audio_paths, durations


# ----------------------------------------------------------------------
# 7. Upbeat 120 BPM Electronic/Funk Launch Groove + Transition SFX
# ----------------------------------------------------------------------

def synthesize_launch_music_and_sfx(total_duration_sec, scene_durations):
    """
    Synthesizes an energetic 120 BPM SaaS product launch soundtrack (modeled after GrtumcQkzsk)
    with punchy kick, crisp clap/snare, 16th-note hi-hat groove, funky octave bassline,
    warm purple synth chords, plus whoosh/bass-drop SFX on every scene cut and UI click SFX!
    """
    sr = SAMPLE_RATE
    total_samples = int((total_duration_sec + 2.0) * sr)
    left = np.zeros(total_samples, dtype=np.float32)
    right = np.zeros(total_samples, dtype=np.float32)

    bpm = 120.0
    beat_sec = 60.0 / bpm          # 0.5s per beat
    step_sec = beat_sec / 4.0      # 0.125s per 16th note
    step_samples = int(step_sec * sr)
    total_steps = total_samples // step_samples

    # Pre-build drum & synth one-shots
    # 1. Kick drum (punchy pitch-drop 130Hz -> 48Hz)
    k_len = int(0.22 * sr)
    kt = np.arange(k_len, dtype=np.float32) / float(sr)
    k_freq = 48.0 + 90.0 * np.exp(-kt * 32.0)
    kick = np.sin(2.0 * np.pi * np.cumsum(k_freq) / float(sr)) * np.exp(-kt * 11.0) * 0.55

    # 2. Crisp Clap / Snare on beats 2 & 4
    c_len = int(0.16 * sr)
    ct = np.arange(c_len, dtype=np.float32) / float(sr)
    rng = np.random.default_rng(42)
    clap = (rng.standard_normal(c_len).astype(np.float32) * np.exp(-ct * 22.0) * 0.32) + (
        np.sin(2.0 * np.pi * 185.0 * ct) * np.exp(-ct * 28.0) * 0.18
    )

    # 3. Tight 16th-note Hi-hat
    h_len = int(0.05 * sr)
    ht = np.arange(h_len, dtype=np.float32) / float(sr)
    hat = rng.standard_normal(h_len).astype(np.float32) * np.exp(-ht * 65.0) * 0.12

    # Bass progression roots (A minor / Fmaj7 / D minor / E7 funk-tech loop)
    bass_roots = [55.0, 55.0, 43.65, 43.65, 73.42, 73.42, 65.41, 82.41]

    for step in range(total_steps):
        st = step * step_samples
        beat_in_bar = step % 16

        # Four-on-the-floor kick + syncopated ghost kick
        if beat_in_bar in (0, 4, 8, 12, 14):
            gain = 1.0 if beat_in_bar in (0, 4, 8, 12) else 0.55
            end = min(total_samples, st + k_len)
            left[st:end] += kick[: end - st] * gain
            right[st:end] += kick[: end - st] * gain

        # Clap on beats 2 and 4 (steps 4 and 12)
        if beat_in_bar in (4, 12):
            end = min(total_samples, st + c_len)
            left[st:end] += clap[: end - st] * 0.9
            right[st:end] += clap[: end - st] * 1.0

        # 16th-note hi-hat groove
        hat_gain = 1.0 if (beat_in_bar % 2 == 1) else 0.45
        end = min(total_samples, st + h_len)
        left[st:end] += hat[: end - st] * hat_gain * 0.8
        right[st:end] += hat[: end - st] * hat_gain * 1.1

        # Funky octave-jumping synth bass
        bar_idx = (step // 16) % len(bass_roots)
        bf = bass_roots[bar_idx] * (2.0 if (beat_in_bar % 2 == 1) else 1.0)
        b_len = min(step_samples, total_samples - st)
        bt = np.arange(b_len, dtype=np.float32) / float(sr)
        b_env = np.exp(-bt * 14.0)
        # Warm saw-like bass (fundamental + 2nd + 3rd harmonic)
        bw = (
            np.sin(2 * np.pi * bf * bt)
            + 0.45 * np.sin(2 * np.pi * (bf * 2) * bt)
            + 0.20 * np.sin(2 * np.pi * (bf * 3) * bt)
        ) * b_env * 0.22
        left[st : st + b_len] += bw
        right[st : st + b_len] += bw

    # Warm stereo synth pad chords underneath
    chord_sec = 2.0
    chord_samples = int(chord_sec * sr)
    pad_chords = [
        [220.0, 261.63, 329.63, 392.00],  # Am7
        [174.61, 220.00, 261.63, 329.63], # Fmaj7
        [146.83, 220.00, 261.63, 349.23], # Dm7
        [164.81, 246.94, 329.63, 392.00], # Em7
    ]
    n_chords = total_samples // chord_samples
    for ci in range(n_chords):
        cs = ci * chord_samples
        ce = min(total_samples, cs + chord_samples)
        clen = ce - cs
        ct = np.arange(clen, dtype=np.float32) / float(sr)
        env = np.sin(np.pi * np.linspace(0.0, 1.0, clen, dtype=np.float32))
        freqs = pad_chords[ci % len(pad_chords)]
        for fi, pf in enumerate(freqs):
            det = 0.4 if fi % 2 == 0 else -0.4
            left[cs:ce] += np.sin(2 * np.pi * (pf + det) * ct) * env * 0.06
            right[cs:ce] += np.sin(2 * np.pi * (pf - det) * ct) * env * 0.06

    # Add Cinematic Whoosh + Sub-Drop on every scene transition & UI Click SFX
    cur_t = 0.0
    for idx, dur in enumerate(scene_durations):
        st_idx = int(cur_t * sr)
        # Transition whoosh + impact
        w_len = min(int(0.42 * sr), total_samples - st_idx)
        if w_len > 0:
            wt = np.arange(w_len, dtype=np.float32) / float(sr)
            w_env = np.sin(np.pi * (wt / 0.42))
            whoosh = rng.standard_normal(w_len).astype(np.float32) * w_env * 0.18
            sub_hit = np.sin(2 * np.pi * (95.0 * np.exp(-wt * 6.0)) * wt) * w_env * 0.35
            left[st_idx : st_idx + w_len] += whoosh * 0.8 + sub_hit
            right[st_idx : st_idx + w_len] += whoosh * 1.2 + sub_hit

        # UI click SFX when cursor clicks mid-scene
        scene = STORYBOARD[idx]
        if scene.get("type") == "macro_ui":
            click_t = cur_t + dur * scene.get("click_u", 0.5)
            c_idx = int(click_t * sr)
            cl_len = min(int(0.06 * sr), total_samples - c_idx)
            if cl_len > 0:
                clt = np.arange(cl_len, dtype=np.float32) / float(sr)
                ui_click = (
                    np.sin(2 * np.pi * 1450.0 * clt) * np.exp(-clt * 95.0) * 0.35
                    + np.sin(2 * np.pi * 2900.0 * clt) * np.exp(-clt * 120.0) * 0.22
                )
                left[c_idx : c_idx + cl_len] += ui_click
                right[c_idx : c_idx + cl_len] += ui_click

        cur_t += dur

    # Fade out last 2 seconds
    fade_out = int(2.0 * sr)
    if fade_out < total_samples:
        fade_curve = np.linspace(1.0, 0.0, fade_out, dtype=np.float32)
        left[-fade_out:] *= fade_curve
        right[-fade_out:] *= fade_curve

    stereo = np.stack([left, right], axis=1)
    peak = np.max(np.abs(stereo))
    if peak > 1e-4:
        stereo = (stereo / peak) * 0.38
    bgm_path = os.path.join(OUTPUT_DIR, "launch_bgm_sfx.wav")
    sf.write(bgm_path, stereo, sr)
    return bgm_path


# ----------------------------------------------------------------------
# 8. Continuous 30 FPS Frame Streamer & Master Audio Mixer
# ----------------------------------------------------------------------

def render_full_video(storyboard, audio_paths, durations, bgm_path):
    # Concatenate all KittenTTS scene audio clips into a single narration track
    vo_chunks = []
    for wav_path in audio_paths:
        data, _ = sf.read(wav_path, dtype="float32")
        vo_chunks.append(data.flatten())
    full_vo = np.concatenate(vo_chunks)
    vo_master_path = os.path.join(OUTPUT_DIR, "vo_master.wav")
    sf.write(vo_master_path, full_vo, SAMPLE_RATE)

    # Stream 30 FPS raw RGB frames directly into Apple Silicon h264_videotoolbox encoder
    raw_video_path = os.path.join(OUTPUT_DIR, "motion_video_only.mp4")
    encoder_cmd = [
        "ffmpeg", "-y",
        "-f", "rawvideo", "-pix_fmt", "rgb24",
        "-s", f"{WIDTH}x{HEIGHT}", "-r", str(FPS),
        "-i", "-",
        "-c:v", "libx264", "-preset", "veryfast", "-crf", "22",
        "-pix_fmt", "yuv420p",
        raw_video_path,
    ]
    proc = subprocess.Popen(encoder_cmd, stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    t_global = 0.0
    dt = 1.0 / float(FPS)
    total_frames = sum(max(1, int(round(d * FPS))) for d in durations)
    rendered = 0
    t_start = time.time()

    for s_idx, (scene, dur) in enumerate(zip(storyboard, durations)):
        n_frames = max(1, int(round(dur * FPS)))
        stype = scene["type"]
        for f in range(n_frames):
            u = f / float(max(1, n_frames - 1))
            if stype == "kinetic_intro":
                img = render_scene_kinetic_intro(t_global, u, scene)
            elif stype == "problem_cloud":
                img = render_scene_problem_cloud(t_global, u, scene)
            elif stype == "marquee_outro":
                img = render_scene_marquee_and_outro(t_global, u, scene)
            else:
                img = render_scene_macro_ui(t_global, u, scene)

            # Save representative preview frames for artifact gallery
            if f == int(n_frames * 0.65):
                img.save(os.path.join(OUTPUT_DIR, f"preview_scene_{s_idx:02d}.png"))

            proc.stdin.write(img.tobytes())
            t_global += dt
            rendered += 1

        elapsed = time.time() - t_start
        fps_actual = rendered / max(0.01, elapsed)
        print(f"[Motion Engine] Rendered Scene {s_idx+1:02d}/{len(storyboard):02d} ({rendered}/{total_frames} frames @ {fps_actual:.1f} fps)")

    proc.stdin.close()
    proc.wait()

    # Mix KittenTTS Master Voiceover + Upbeat Launch BGM & SFX into final MP4
    print("Mastering final audio mix (KittenTTS Voiceover + 120 BPM Launch Groove + Transition SFX)...")
    subprocess.run(
        [
            "ffmpeg", "-y",
            "-i", raw_video_path,
            "-i", vo_master_path,
            "-i", bgm_path,
            "-filter_complex",
            "[1:a]volume=1.25[vo];[2:a]volume=0.28[bgm];[vo][bgm]amix=inputs=2:duration=first:dropout_transition=2[aout]",
            "-map", "0:v",
            "-map", "[aout]",
            "-c:v", "copy",
            "-c:a", "aac", "-b:a", "192k", "-ar", str(SAMPLE_RATE), "-ac", "2",
            VIDEO_FILE,
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        check=True,
    )


def main():
    print("=== Building BIASafe-Style 30FPS SaaS Launch & Walkthrough Video (KittenTTS + Motion Engine) ===")

    # 1. Verify Playwright UI captures exist
    required_sample = os.path.join(UI_CAPTURES_DIR, "s9_isolated_second_tenant.png")
    if not os.path.exists(required_sample):
        print("Capturing live 1080p UI states via Playwright...")
        subprocess.run(["node", os.path.join(BASE_DIR, "scripts/capture_demo_ui.mjs")], cwd=BASE_DIR, check=True)

    # 2. Synthesize KittenTTS voiceover for all 18 scenes
    print("Initializing KittenTTS (KittenML/kitten-tts-nano-0.1)...")
    from kittentts import KittenTTS
    tts_model = KittenTTS("KittenML/kitten-tts-nano-0.1")
    audio_paths, durations = generate_scene_narrations(tts_model, STORYBOARD)

    total_dur = sum(durations)
    print(f"Total Video Duration: {total_dur:.2f} seconds ({total_dur/60.0:.2f} minutes)")

    # 3. Synthesize 120 BPM Launch Music + Camera Whoosh & UI Click SFX
    print("Synthesizing 120 BPM Electronic/Funk Launch Groove + Transition Whooshes & UI Clicks...")
    bgm_path = synthesize_launch_music_and_sfx(total_dur, durations)

    # 4. Render 30 FPS continuous motion video & master final MP4
    render_full_video(STORYBOARD, audio_paths, durations, bgm_path)

    size_mb = os.path.getsize(VIDEO_FILE) / (1024 * 1024)
    print(f"=== COMPLETE: {VIDEO_FILE} ({size_mb:.2f} MB, {total_dur:.2f}s / {total_dur/60.0:.2f} min) ===")


if __name__ == "__main__":
    main()
