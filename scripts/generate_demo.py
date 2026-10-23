#!/usr/bin/env python3
"""
Minimalist Full HD (1920x1080) Application Workflow Demo Generator
- Pure 1:1 1920x1080 UI demo only (no text overlays, no badges, no background music, no 3D effects)
- Slow, clear, deliberate English voiceover narration via KittenTTS (speed=0.88, generous sentence pauses)
- Demonstrates all application features end-to-end
"""

import os
import re
import time
import subprocess
import numpy as np
import soundfile as sf
from PIL import Image, ImageDraw

BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUTPUT_DIR = os.path.join(BASE_DIR, "demo_artifacts")
UI_CAPTURES_DIR = os.path.join(OUTPUT_DIR, "ui_captures")
VIDEO_FILE = os.path.join(BASE_DIR, "demo_walkthrough.mp4")

WIDTH = 1920
HEIGHT = 1080
FPS = 30
SAMPLE_RATE = 24000

os.makedirs(OUTPUT_DIR, exist_ok=True)

_IMG_CACHE = {}


def get_ui_image(name):
    if name not in _IMG_CACHE:
        path = os.path.join(UI_CAPTURES_DIR, name)
        _IMG_CACHE[name] = Image.open(path).convert("RGB").resize((WIDTH, HEIGHT), Image.Resampling.LANCZOS)
    return _IMG_CACHE[name]


def clamp(x, a=0.0, b=1.0):
    return max(a, min(b, float(x)))


def ease_in_out_cubic(u):
    u = clamp(u)
    return 4 * u * u * u if u < 0.5 else 1 - ((-2 * u + 2) ** 3) / 2


def draw_cursor(draw, x, y, clicking=False, ripple_prog=0.0):
    x, y = int(x), int(y)
    if ripple_prog > 0.01:
        rr = int(8 + 36 * ripple_prog)
        draw.ellipse(
            [(x - rr, y - rr), (x + rr, y + rr)],
            outline=(79, 70, 229),
            width=2,
        )
    scale = 0.88 if clicking else 1.0
    pts = [
        (x, y),
        (int(x + 10 * scale), int(y + 26 * scale)),
        (int(x + 14 * scale), int(y + 18 * scale)),
        (int(x + 24 * scale), int(y + 16 * scale)),
    ]
    s_pts = [(px + 2, py + 2) for px, py in pts]
    draw.polygon(s_pts, fill=(15, 23, 42))
    draw.polygon(pts, fill="#4F46E5", outline="#FFFFFF")


# ----------------------------------------------------------------------
# Complete 14-Step Minimalist Application Workflow Storyboard
# ----------------------------------------------------------------------

STORYBOARD = [
    {
        "title": "01. Multi-Tenant Gatekeeper",
        "states": [
            (0.00, "s1_gatekeeper_empty.png", (960, 540)),
            (0.35, "s1_gatekeeper_error.png", (960, 580)),
            (0.68, "s1_gatekeeper_valid.png", (960, 620)),
        ],
        "text": "Welcome to the Supplier Catalog Synchronization Engine. When opening the application, the Multi-Tenant Gatekeeper validates the shop identifier. It rejects invalid characters, and opens an isolated catalog workspace for store flagship.",
    },
    {
        "title": "02. Dashboard & Pre-Sync Diff Preview",
        "states": [
            (0.00, "s2_dashboard_clean.png", (1815, 160)),
            (0.45, "s3_sync_baseline_diff_preview.png", (1280, 825)),
        ],
        "text": "On the main Dashboard, staff can manage five independent supplier updates. Clicking Sync on the Baseline Catalog Feed first opens a Diff Preview modal. This dry-run preview shows every new product, skipped duplicate, and quarantined record before any change is committed.",
    },
    {
        "title": "03. Confirm & Sync Baseline Catalog Feed",
        "states": [
            (0.00, "s3_sync_baseline_diff_preview.png", (1280, 825)),
            (0.28, "s3_sync_v100_running.png", (1420, 210)),
            (0.65, "s3_sync_v100_completed.png", (1735, 160)),
        ],
        "text": "Clicking Confirm and Sync streams ten supplier batches into the local catalog, with real-time Server-Sent Events progress and throughput metrics. Once all ten batches commit, the update is marked Synced.",
    },
    {
        "title": "04. Price & Inventory Adjustment Update",
        "states": [
            (0.00, "s4_sync_v110_diff_preview.png", (1280, 825)),
            (0.45, "s4_sync_v110_running.png", (1420, 275)),
            (0.75, "s4_dashboard_after_v110.png", (1735, 220)),
        ],
        "text": "Next, clicking Sync on Price and Inventory Adjustment previews row-by-row price changes and product withdrawals. Confirming the update applies newer timestamps, while automatically skipping duplicate records and out-of-order stale timestamps.",
    },
    {
        "title": "05. Supplier Anomaly Feed & Dead-Letter Quarantine",
        "states": [
            (0.00, "s4_dashboard_after_v110.png", (1815, 280)),
            (0.45, "s4_dashboard_after_v120.png", (980, 470)),
        ],
        "text": "Running the Supplier Anomaly Feed processes fifteen batches containing corrupt data. Valid products commit normally, while records with negative prices or empty SKUs are isolated in the Dead-Letter Queue without stopping the sync.",
    },
    {
        "title": "06. Interactive Metric Card Drilldowns",
        "states": [
            (0.00, "s4_drilldown_updated.png", (960, 520)),
            (0.36, "s4_drilldown_withdrawn.png", (960, 520)),
            (0.70, "s4_drilldown_skipped.png", (960, 520)),
        ],
        "text": "Clicking any of the summary metric cards opens a detailed drilldown view. Staff can inspect updated product prices, verify that withdrawn products remain permanently tombstoned, and review skipped duplicate entries.",
    },
    {
        "title": "07. Analytics Filters & Chart Zoom",
        "states": [
            (0.00, "s4_analytics_custom_range.png", (1265, 552)),
            (0.50, "s4_analytics_zoomed.png", (1565, 775)),
        ],
        "text": "In the Analytics section, staff can filter metrics by Today, Yesterday, Last Seven Days, All Time, or a Custom date range, and zoom directly into the activity chart.",
    },
    {
        "title": "08. Mid-Feed Interruption & Atomic Checkpoint",
        "states": [
            (0.00, "s5_crash_diff_preview.png", (1280, 825)),
            (0.35, "s5_dashboard_checkpoint_paused.png", (1755, 390)),
            (0.72, "s5_history_interrupted_filter.png", (1665, 365)),
        ],
        "text": "Next, we trigger the Mid-Feed Crash and Checkpoint Recovery scenario. The update halts midway at batch five of ten. The existing catalog is unharmed, and the job is marked Interrupted rather than complete, with its batch checkpoint saved in SQLite.",
    },
    {
        "title": "09. Resuming Interrupted Update",
        "states": [
            (0.00, "s5_history_interrupted_filter.png", (1665, 365)),
            (0.48, "s5_history_resumed_completed.png", (1410, 365)),
        ],
        "text": "Clicking Resume continues the interrupted job directly from batch six of ten, without losing completed work or repeating batches one through five.",
    },
    {
        "title": "10. Live Catalog & Product Modification History",
        "states": [
            (0.00, "s6_catalog_all_active.png", (1690, 125)),
            (0.26, "s6_catalog_withdrawn_only.png", (1690, 125)),
            (0.50, "s6_catalog_sku_search.png", (1795, 290)),
            (0.72, "s6_catalog_product_history_modal.png", (960, 520)),
        ],
        "text": "In the Catalog tab, staff can filter by Active or Withdrawn status and search by SKU. Clicking History on any product opens the Product Modification History modal, showing the chronological before-and-after audit trail for that item.",
    },
    {
        "title": "11. Quarantine Inspector",
        "states": [
            (0.00, "s7_quarantine_overview.png", (1680, 125)),
            (0.50, "s7_quarantine_reason_filtered.png", (1770, 290)),
        ],
        "text": "The Quarantine tab displays all malformed supplier records that still need staff attention, with filtering by error reason such as Negative or Zero Price.",
    },
    {
        "title": "12. Staff Problem Remediation",
        "states": [
            (0.00, "s7_remediation_modal_raw.png", (960, 520)),
            (0.45, "s7_remediation_modal_corrected.png", (1120, 745)),
            (0.78, "s7_quarantine_after_import.png", (960, 360)),
        ],
        "text": "Clicking Review and Fix shows the raw supplier JSON payload. Staff can correct the product name and enter a valid positive price, then import the remediated product directly into the active catalog.",
    },
    {
        "title": "13. Version Diffs & Independent Update Revert",
        "states": [
            (0.00, "s8_history_versions_timeline.png", (1610, 420)),
            (0.22, "s8_version_diff_modal.png", (1280, 825)),
            (0.46, "s8_revert_update_diff_preview.png", (1280, 825)),
            (0.72, "s8_revert_running.png", (1420, 275)),
            (0.88, "s8_overview_after_independent_revert.png", (1760, 220)),
        ],
        "text": "In History and Versions, clicking Diff displays the exact row-level changes for any sync run. Back on the Dashboard, each synced update can be reverted independently. Clicking Revert first previews the exact rollback diff, and confirming restores the previous catalog state with live batch progress.",
    },
    {
        "title": "14. Multi-Tenant Store Switcher",
        "states": [
            (0.00, "s9_switch_tenant_modal.png", (1110, 620)),
            (0.50, "s9_isolated_second_tenant.png", (960, 440)),
        ],
        "text": "Finally, using the Switch button in the sidebar, staff can switch to another tenant such as store boutique two, opening a completely isolated database workspace. Thank you for watching.",
    },
]


def split_sentences(text):
    parts = [p.strip() for p in re.split(r"(?<=[.!?])\s+", text.strip()) if p.strip()]
    return parts if parts else [text.strip()]


def generate_scene_narrations(tts_model, storyboard):
    """
    Synthesizes slow, clear, deliberate English voiceover narration for each scene.
    Uses speed=0.88 and generous pauses between sentences so every word is clear and unhurried.
    """
    audio_paths = []
    durations = []
    pause_samples = np.zeros(int(SAMPLE_RATE * 0.42), dtype=np.float32)
    lead_pad = np.zeros(int(SAMPLE_RATE * 0.55), dtype=np.float32)
    tail_pad = np.zeros(int(SAMPLE_RATE * 0.65), dtype=np.float32)

    for idx, scene in enumerate(storyboard):
        final_wav = os.path.join(OUTPUT_DIR, f"minimal_narration_{idx:02d}.wav")

        sentences = split_sentences(scene["text"])
        chunks = [lead_pad]
        for s in sentences:
            try:
                wav = tts_model.generate(s, voice="expr-voice-2-m", speed=0.88)
            except TypeError:
                wav = tts_model.generate(s, voice="expr-voice-2-m")
            wav = np.asarray(wav, dtype=np.float32).flatten()
            non_silent = np.where(np.abs(wav) > 0.008)[0]
            if len(non_silent) > 0:
                s0 = max(0, non_silent[0] - int(SAMPLE_RATE * 0.05))
                s1 = min(len(wav), non_silent[-1] + int(SAMPLE_RATE * 0.08))
                wav = wav[s0:s1]
            chunks.append(wav)
            chunks.append(pause_samples)
        chunks.append(tail_pad)
        full_wav = np.concatenate(chunks)
        peak = np.max(np.abs(full_wav))
        if peak > 1e-4:
            full_wav = (full_wav / peak) * 0.88
        sf.write(final_wav, full_wav, SAMPLE_RATE)

        info = sf.info(final_wav)
        dur = float(info.frames) / float(info.samplerate)
        print(f"[Narration] Scene {idx+1:02d}/{len(storyboard):02d} ({dur:.2f}s): {scene['title']}")
        audio_paths.append(final_wav)
        durations.append(dur)

    return audio_paths, durations


def render_workflow_scene(u, scene):
    states = scene["states"]
    active_idx = 0
    for i, (threshold, _, _) in enumerate(states):
        if u >= threshold:
            active_idx = i

    _, img_name, target_pt = states[active_idx]
    base_img = get_ui_image(img_name).copy()
    draw = ImageDraw.Draw(base_img)

    prev_pt = states[max(0, active_idx - 1)][2] if active_idx > 0 else (target_pt[0] - 120, target_pt[1] + 80)
    t0 = states[active_idx][0]
    t1 = states[active_idx + 1][0] if active_idx + 1 < len(states) else 1.0
    span = max(0.05, t1 - t0)
    local_u = clamp((u - t0) / span)

    move_k = ease_in_out_cubic(clamp(local_u / 0.65))
    cx = prev_pt[0] + (target_pt[0] - prev_pt[0]) * move_k
    cy = prev_pt[1] + (target_pt[1] - prev_pt[1]) * move_k

    clicking = 0.62 <= local_u <= 0.72
    ripple = clamp((local_u - 0.62) / 0.30) if 0.62 <= local_u <= 0.92 else 0.0
    draw_cursor(draw, cx, cy, clicking=clicking, ripple_prog=ripple)

    return base_img


def render_full_video(storyboard, audio_paths, durations):
    vo_chunks = []
    for wav_path in audio_paths:
        data, _ = sf.read(wav_path, dtype="float32")
        vo_chunks.append(data.flatten())
    full_vo = np.concatenate(vo_chunks)
    vo_master_path = os.path.join(OUTPUT_DIR, "vo_minimal_master.wav")
    sf.write(vo_master_path, full_vo, SAMPLE_RATE)

    raw_video_path = os.path.join(OUTPUT_DIR, "minimal_video_only.mp4")
    encoder_cmd = [
        "ffmpeg", "-y",
        "-f", "rawvideo", "-pix_fmt", "rgb24",
        "-s", f"{WIDTH}x{HEIGHT}", "-r", str(FPS),
        "-i", "-",
        "-c:v", "libx264", "-preset", "fast", "-crf", "16",
        "-pix_fmt", "yuv420p",
        raw_video_path,
    ]
    proc = subprocess.Popen(encoder_cmd, stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    total_frames = sum(max(1, int(round(d * FPS))) for d in durations)
    rendered = 0
    t_start = time.time()

    for s_idx, (scene, dur) in enumerate(zip(storyboard, durations)):
        n_frames = max(1, int(round(dur * FPS)))
        for f in range(n_frames):
            u = f / float(max(1, n_frames - 1))
            img = render_workflow_scene(u, scene)
            proc.stdin.write(img.tobytes())
            rendered += 1

        elapsed = time.time() - t_start
        fps_actual = rendered / max(0.01, elapsed)
        print(f"[Full HD 1080p] Scene {s_idx+1:02d}/{len(storyboard):02d} ({rendered}/{total_frames} frames @ {fps_actual:.1f} fps)")

    proc.stdin.close()
    proc.wait()

    print("Muxing Full HD 1080p video with clear voiceover audio...")
    subprocess.run(
        [
            "ffmpeg", "-y",
            "-i", raw_video_path,
            "-i", vo_master_path,
            "-map", "0:v",
            "-map", "1:a",
            "-c:v", "copy",
            "-c:a", "aac", "-b:a", "192k", "-ar", str(SAMPLE_RATE), "-ac", "2",
            "-movflags", "+faststart",
            VIDEO_FILE,
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        check=True,
    )
    for tmp_f in [raw_video_path, vo_master_path] + audio_paths:
        try:
            if os.path.exists(tmp_f):
                os.remove(tmp_f)
        except Exception:
            pass


def main():
    print("=== Building Minimalist Full HD (1920x1080) Demo Video (Slow, Clear Voiceover Only) ===")
    required_sample = os.path.join(UI_CAPTURES_DIR, "s9_isolated_second_tenant.png")
    if not os.path.exists(required_sample):
        print("Capturing live 1080p UI states via Playwright...")
        subprocess.run(["node", os.path.join(BASE_DIR, "scripts/capture_demo_ui.mjs")], cwd=BASE_DIR, check=True)

    print("Initializing KittenTTS (KittenML/kitten-tts-nano-0.1) at slow, clear speaking rate (speed=0.88)...")
    from kittentts import KittenTTS
    tts_model = KittenTTS("KittenML/kitten-tts-nano-0.1")
    audio_paths, durations = generate_scene_narrations(tts_model, STORYBOARD)

    total_dur = sum(durations)
    print(f"Total Video Duration: {total_dur:.2f} seconds ({total_dur/60.0:.2f} minutes)")

    render_full_video(STORYBOARD, audio_paths, durations)

    size_mb = os.path.getsize(VIDEO_FILE) / (1024 * 1024)
    print(f"=== COMPLETE: {VIDEO_FILE} ({size_mb:.2f} MB, {total_dur:.2f}s / {total_dur/60.0:.2f} min) ===")


if __name__ == "__main__":
    main()
