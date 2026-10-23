#!/usr/bin/env python3
"""
Combines:
1. All 10 OpenMOSS/MOSS-TTS 48kHz voiceover audio segments at natural 1.0x speed
2. A warm, gentle ambient background music track (stereo 48kHz) with automatic sidechain-style ducking
3. The live-recorded 1920x1080 Playwright interaction video (demo_artifacts/live_recorded.webm)
into the final Full HD `demo_walkthrough.mp4`.
"""

import json
import os
import subprocess
import wave
import numpy as np

BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
MOSS_DIR = os.path.join(BASE_DIR, "demo_artifacts", "moss_tts")
CONCAT_FILE = os.path.join(BASE_DIR, "demo_artifacts", "frames_concat.txt")
MIXED_WAV = os.path.join(BASE_DIR, "demo_artifacts", "moss_vo_with_bgm.wav")
FINAL_MP4 = os.path.join(BASE_DIR, "docs", "demo", "demo_walkthrough.mp4")

SAMPLE_RATE = 48000


def read_wav_mono(path: str) -> np.ndarray:
    with wave.open(path, "rb") as wf:
        sr = wf.getframerate()
        n_ch = wf.getnchannels()
        sampwidth = wf.getsampwidth()
        raw = wf.readframes(wf.getnframes())
    if sampwidth == 2:
        arr = np.frombuffer(raw, dtype=np.int16).astype(np.float32) / 32768.0
    elif sampwidth == 4:
        arr = np.frombuffer(raw, dtype=np.int32).astype(np.float32) / 2147483648.0
    else:
        raise ValueError(f"Unsupported sample width: {sampwidth}")
    if n_ch > 1:
        arr = arr.reshape(-1, n_ch).mean(axis=1)
    if sr != SAMPLE_RATE:
        old_idx = np.linspace(0.0, 1.0, num=len(arr), endpoint=False)
        new_len = int(round(len(arr) * float(SAMPLE_RATE) / float(sr)))
        new_idx = np.linspace(0.0, 1.0, num=new_len, endpoint=False)
        arr = np.interp(new_idx, old_idx, arr).astype(np.float32)
    return arr


def write_wav_stereo(path: str, left: np.ndarray, right: np.ndarray, sr: int = SAMPLE_RATE) -> None:
    stereo = np.stack([np.clip(left, -1.0, 1.0), np.clip(right, -1.0, 1.0)], axis=1)
    int16_data = (stereo * 32767.0).astype(np.int16)
    with wave.open(path, "wb") as wf:
        wf.setnchannels(2)
        wf.setsampwidth(2)
        wf.setframerate(sr)
        wf.writeframes(int16_data.tobytes())


def synthesize_ambient_bgm(num_samples: int, sr: int = SAMPLE_RATE) -> tuple[np.ndarray, np.ndarray]:
    """
    Generates a warm, modern, soothing corporate/lofi ambient background track in C major / A minor
    with stereo Rhodes-style warm electric piano chords, subtle arpeggio plucks, and soft sub-bass.
    """
    t = np.arange(num_samples, dtype=np.float32) / float(sr)
    left = np.zeros(num_samples, dtype=np.float32)
    right = np.zeros(num_samples, dtype=np.float32)

    # 4-chord warm progression (4 seconds per chord = 16s loop):
    # Fmaj9 -> G6 -> Em7 -> Am9
    chords = [
        [174.61, 220.00, 261.63, 329.63, 392.00],  # Fmaj9
        [196.00, 246.94, 293.66, 329.63, 392.00],  # G6
        [164.81, 196.00, 246.94, 293.66, 392.00],  # Em7
        [220.00, 261.63, 329.63, 392.00, 493.88],  # Am9
    ]
    chord_dur = 4.0
    chord_samples = int(chord_dur * sr)
    total_chords = (num_samples // chord_samples) + 2

    for c_idx in range(total_chords):
        s0 = c_idx * chord_samples
        if s0 >= num_samples:
            break
        s1 = min(num_samples, s0 + chord_samples)
        n = s1 - s0
        lt = np.arange(n, dtype=np.float32) / float(sr)

        # Smooth pad envelope with soft attack & release
        attack = np.minimum(1.0, lt / 0.65)
        release = np.minimum(1.0, (chord_dur - lt) / 0.75)
        env = attack * release

        notes = chords[c_idx % len(chords)]
        pad_l = np.zeros(n, dtype=np.float32)
        pad_r = np.zeros(n, dtype=np.float32)

        for i, freq in enumerate(notes):
            detune = 1.0 + (i - 2) * 0.0008
            phase_l = 2.0 * np.pi * freq * lt
            phase_r = 2.0 * np.pi * (freq * detune) * lt + (0.35 * i)
            # Warm fundamental + gentle 2nd harmonic
            tone_l = np.sin(phase_l) + 0.18 * np.sin(2.0 * phase_l) * np.exp(-lt * 1.4)
            tone_r = np.sin(phase_r) + 0.18 * np.sin(2.0 * phase_r) * np.exp(-lt * 1.4)
            weight = 0.24 if i > 0 else 0.34
            pad_l += weight * tone_l
            pad_r += weight * tone_r

        # Gentle arpeggiated warm bell pluck every 0.5s (120 BPM eighth notes)
        step_dur = 0.5
        step_samples = int(step_dur * sr)
        arp_pattern = [0, 2, 3, 4, 3, 2, 1, 2]
        for step_i, note_idx in enumerate(arp_pattern):
            as0 = step_i * step_samples
            if as0 >= n:
                break
            as1 = min(n, as0 + step_samples)
            al = as1 - as0
            at = np.arange(al, dtype=np.float32) / float(sr)
            afreq = notes[note_idx % len(notes)] * 2.0
            a_env = np.minimum(1.0, at / 0.015) * np.exp(-at * 5.5)
            pluck = 0.11 * a_env * np.sin(2.0 * np.pi * afreq * at)
            pan = 0.35 + 0.30 * ((step_i % 2) * 2 - 1)
            pad_l[as0:as1] += pluck * pan
            pad_r[as0:as1] += pluck * (1.0 - pan)

        left[s0:s1] += pad_l * env
        right[s0:s1] += pad_r * env

    # Slow stereo warmth modulation
    lfo = 0.92 + 0.08 * np.sin(2.0 * np.pi * 0.18 * t)
    left *= lfo
    right *= (1.84 - lfo)

    # Fade in first 1.2s and fade out last 2.0s
    fade_in = np.minimum(1.0, t / 1.2)
    fade_out = np.minimum(1.0, (t[-1] - t) / 2.0)
    master_env = fade_in * fade_out

    peak = max(1e-4, float(max(np.max(np.abs(left)), np.max(np.abs(right)))))
    left = (left / peak) * master_env
    right = (right / peak) * master_env
    return left, right


def main():
    manifest_path = os.path.join(MOSS_DIR, "manifest.json")
    with open(manifest_path, "r", encoding="utf-8") as f:
        manifest = json.load(f)

    vo_chunks = []
    for seg in manifest:
        wav_data = read_wav_mono(seg["wav"])
        vo_chunks.append(wav_data)
    full_vo = np.concatenate(vo_chunks)

    # Normalize voiceover cleanly to -1.5 dBFS
    vo_peak = max(1e-4, float(np.max(np.abs(full_vo))))
    full_vo = (full_vo / vo_peak) * 0.85

    # Generate warm stereo ambient background music matching the exact duration
    bgm_l, bgm_r = synthesize_ambient_bgm(len(full_vo), SAMPLE_RATE)

    # Compute smooth voice activity envelope for automatic ducking:
    # When voice is active, BGM plays softly at 11% amplitude; during pauses, swells gently to 20%.
    win = int(SAMPLE_RATE * 0.18)
    abs_vo = np.abs(full_vo)
    kernel = np.ones(win, dtype=np.float32) / float(win)
    vo_env = np.convolve(abs_vo, kernel, mode="same")
    speaking = np.clip(vo_env / 0.04, 0.0, 1.0)
    bgm_gain = 0.19 - 0.085 * speaking

    mix_l = full_vo + bgm_l * bgm_gain
    mix_r = full_vo + bgm_r * bgm_gain

    mix_peak = max(1e-4, float(max(np.max(np.abs(mix_l)), np.max(np.abs(mix_r)))))
    if mix_peak > 0.96:
        mix_l = (mix_l / mix_peak) * 0.95
        mix_r = (mix_r / mix_peak) * 0.95

    write_wav_stereo(MIXED_WAV, mix_l, mix_r, SAMPLE_RATE)
    total_dur = len(full_vo) / float(SAMPLE_RATE)
    print(f"Mixed MOSS-TTS voiceover + ambient background music: {MIXED_WAV} ({total_dur:.2f}s)")

    if not os.path.exists(CONCAT_FILE):
        raise FileNotFoundError(f"Zero-drift frame concat manifest not found: {CONCAT_FILE}")

    os.makedirs(os.path.dirname(FINAL_MP4), exist_ok=True)
    print("Encoding final Full HD 1080p MP4 (H.264 + AAC 192kbps) with zero audio/video drift...")
    cmd = [
        "ffmpeg", "-y",
        "-f", "concat",
        "-safe", "0",
        "-i", CONCAT_FILE,
        "-i", MIXED_WAV,
        "-map", "0:v:0",
        "-map", "1:a:0",
        "-vf", "fps=30,scale=1920:1080:flags=lanczos,format=yuv420p",
        "-c:v", "libx264",
        "-preset", "fast",
        "-crf", "17",
        "-c:a", "aac",
        "-b:a", "192k",
        "-ar", "48000",
        "-shortest",
        "-movflags", "+faststart",
        FINAL_MP4,
    ]
    subprocess.run(cmd, check=True)
    size_mb = os.path.getsize(FINAL_MP4) / (1024 * 1024)
    print(f"Done! Created {FINAL_MP4} ({size_mb:.2f} MB)")


if __name__ == "__main__":
    main()
