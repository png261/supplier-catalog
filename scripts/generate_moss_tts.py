#!/usr/bin/env python3
"""
Synthesizes natural-speed voiceover audio segments using OpenMOSS/MOSS-TTS
(https://github.com/OpenMOSS/MOSS-TTS / https://github.com/OpenMOSS/MOSS-TTS-Nano)
via the official standalone ONNX CPU runtime (`OnnxTtsRuntime`) on Apple Silicon.
"""

import json
import os
import sys
import time
import wave
import numpy as np

sys.path.insert(0, "/tmp/moss_onnx_cpu")
from onnx_tts_runtime import OnnxTtsRuntime

BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT_DIR = os.path.join(BASE_DIR, "demo_artifacts", "moss_tts")
os.makedirs(OUT_DIR, exist_ok=True)

REF_AUDIO_PATH = "/tmp/moss_onnx_cpu/en_4.wav"
SAMPLE_RATE = 48000

SEGMENTS = [
    {
        "id": "seg_01_gatekeeper",
        "title": "01. Multi-Tenant Gatekeeper",
        "text": (
            "Welcome to the Supplier Catalog Synchronization Engine. "
            "When opening the portal, the Multi-Tenant Gatekeeper validates the shop identifier, "
            "rejects invalid input, and launches an isolated workspace for store flagship."
        ),
    },
    {
        "id": "seg_02_update1_preview",
        "title": "02. Pre-Sync Diff Preview (Update 1)",
        "text": (
            "On the Dashboard, each supplier feed is managed independently. "
            "Clicking Sync on the Baseline Catalog Feed first opens a Pre-Sync Diff Preview modal, "
            "allowing staff to inspect new products, updates, and anomalies before committing."
        ),
    },
    {
        "id": "seg_03_update1_sync",
        "title": "03. Live Batch Sync & SSE Progress (Update 1)",
        "text": (
            "Clicking Confirm and Sync streams ten batches into SQLite WAL storage with real-time "
            "Server-Sent Events progress. Valid items commit atomically, duplicates are skipped, "
            "and malformed records are quarantined."
        ),
    },
    {
        "id": "seg_04_update2_crash",
        "title": "04. Mid-Feed Crash & Checkpoint Pause (Update 2)",
        "text": (
            "Next, we demonstrate fault tolerance with the Mid-Feed Crash and Checkpoint Recovery update. "
            "After confirming the diff preview, the sync runs through batch five of ten and halts midway, "
            "preserving all completed batches in a WAL checkpoint."
        ),
    },
    {
        "id": "seg_05_update2_resume",
        "title": "05. Resuming Interrupted Update from Checkpoint",
        "text": (
            "Staff can resume the interrupted update directly from its saved checkpoint. "
            "The engine continues seamlessly from batch six through batch ten without repeating earlier batches "
            "or losing any data."
        ),
    },
    {
        "id": "seg_06_drilldown_analytics",
        "title": "06. Interactive KPI Drilldowns & Analytics",
        "text": (
            "Clicking any KPI summary card opens an interactive drilldown modal to inspect updated prices, "
            "permanently tombstoned withdrawn items, and skipped duplicates, alongside time-range analytics filters."
        ),
    },
    {
        "id": "seg_07_catalog_history",
        "title": "07. Live Catalog & Product Modification History",
        "text": (
            "In the Catalog view, staff can filter by Active or Withdrawn status, search by SKU, "
            "and click History on any item to view its complete chronological modification audit trail."
        ),
    },
    {
        "id": "seg_08_quarantine_remediation",
        "title": "08. Quarantine Inspection & Staff Remediation",
        "text": (
            "The Quarantine tab isolates defective supplier records by error reason. "
            "Clicking Review and Fix displays the raw JSON payload so staff can correct the product name and price, "
            "importing the fixed item directly into the live catalog."
        ),
    },
    {
        "id": "seg_09_history_diff_revert",
        "title": "09. Version Diffs & Independent Update Revert",
        "text": (
            "In History and Versions, staff can inspect row-level diffs for any completed sync. "
            "Back on the Dashboard, clicking Revert on a synced update previews the inverse diff first, "
            "and confirming rolls back that update with live batch progress."
        ),
    },
    {
        "id": "seg_10_tenant_switch",
        "title": "10. Multi-Tenant Store Switcher",
        "text": (
            "Finally, using the Switch Tenant button in the sidebar, staff can switch to store boutique two, "
            "opening a completely independent catalog database. Thank you for watching."
        ),
    },
]


def get_wav_duration(path: str) -> float:
    with wave.open(path, "rb") as wf:
        return float(wf.getnframes()) / float(wf.getframerate())


def main():
    print("Initializing OpenMOSS/MOSS-TTS-Nano ONNX CPU Runtime...", flush=True)
    runtime = OnnxTtsRuntime(
        model_dir=None,
        thread_count=8,
        execution_provider="cpu",
    )

    manifest = []
    for idx, seg in enumerate(SEGMENTS):
        wav_path = os.path.join(OUT_DIR, f"{seg['id']}.wav")
        t0 = time.time()
        print(f"[MOSS-TTS ONNX] Synthesizing {idx+1}/{len(SEGMENTS)}: {seg['title']}...", flush=True)
        res = runtime.synthesize(
            text=seg["text"],
            prompt_audio_path=REF_AUDIO_PATH,
            output_audio_path=wav_path,
            sample_mode="fixed",
            do_sample=True,
            streaming=True,
            max_new_frames=375,
            voice_clone_max_text_tokens=75,
            enable_wetext=False,
            enable_normalize_tts_text=True,
            seed=42 + idx,
        )
        dur = get_wav_duration(wav_path)
        elapsed = time.time() - t0
        print(f"  => Saved {wav_path} (audio={dur:.2f}s, synth={elapsed:.2f}s)", flush=True)
        manifest.append({
            "id": seg["id"],
            "title": seg["title"],
            "text": seg["text"],
            "wav": wav_path,
            "duration_sec": round(dur, 3),
            "duration_ms": int(round(dur * 1000)),
        })

    manifest_path = os.path.join(OUT_DIR, "manifest.json")
    with open(manifest_path, "w", encoding="utf-8") as f:
        json.dump(manifest, f, indent=2)
    total_sec = sum(m["duration_sec"] for m in manifest)
    print(f"\nAll {len(manifest)} MOSS-TTS segments synthesized! Total duration: {total_sec:.2f}s ({total_sec/60:.2f} min)", flush=True)


if __name__ == "__main__":
    main()
