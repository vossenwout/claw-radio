#!/usr/bin/env python3
"""Warm Chatterbox daemon and one-shot renderer.

Daemon mode:
    python daemon.py <socket_path>

One-shot mode:
    python daemon.py --one-shot <text> <out_path> [--voice <wav>]
"""

from __future__ import annotations

import argparse
import json
import os
import signal
import socket
import sys
from typing import Any, Optional

import torchaudio as ta


def get_device() -> str:
    import torch

    if torch.backends.mps.is_available():
        return "mps"
    if torch.cuda.is_available():
        return "cuda"
    return "cpu"


def load_model(device: Optional[str] = None):
    device = device or get_device()

    # Prefer turbo when available; fall back to the base model.
    try:
        from chatterbox.tts_turbo import ChatterboxTurboTTS

        return ChatterboxTurboTTS.from_pretrained(device=device)
    except Exception:
        from chatterbox.tts import ChatterboxTTS

        return ChatterboxTTS.from_pretrained(device=device)


def _extract_wav_data(wav: Any) -> Any:
    # Chatterbox typically returns a torch.Tensor. Some wrappers return
    # tuple-like structures; support both with best-effort extraction.
    if isinstance(wav, (tuple, list)) and wav:
        return wav[0]
    return wav


def synthesize_to_path(model: Any, text: str, out_path: str, voice_prompt: Optional[str] = None) -> None:
    kwargs = {}
    if voice_prompt:
        kwargs["audio_prompt_path"] = voice_prompt

    wav = model.generate(text, **kwargs)
    wav_data = _extract_wav_data(wav)

    out_dir = os.path.dirname(out_path)
    if out_dir:
        os.makedirs(out_dir, exist_ok=True)

    sample_rate = getattr(model, "sr", 24000)
    ta.save(out_path, wav_data, sample_rate)


def _parse_request(line: bytes) -> dict[str, Any]:
    req = json.loads(line.decode("utf-8"))
    if not isinstance(req, dict):
        raise ValueError("request must be a JSON object")

    text = req.get("text")
    out_path = req.get("out_path")

    if not isinstance(text, str) or not text.strip():
        raise ValueError("text must be a non-empty string")
    if not isinstance(out_path, str) or not out_path.strip():
        raise ValueError("out_path must be a non-empty string")

    voice_prompt = req.get("voice_prompt")
    if voice_prompt is not None and not isinstance(voice_prompt, str):
        raise ValueError("voice_prompt must be a string when provided")

    return {
        "text": text,
        "out_path": out_path,
        "voice_prompt": voice_prompt,
    }


def _send_response(conn: socket.socket, payload: dict[str, Any]) -> None:
    conn.sendall((json.dumps(payload) + "\n").encode("utf-8"))


def _read_request_line(conn: socket.socket) -> bytes:
    buf = bytearray()
    while True:
        chunk = conn.recv(4096)
        if not chunk:
            break
        buf.extend(chunk)
        if b"\n" in chunk:
            break

    if not buf:
        raise ValueError("empty request")

    line = bytes(buf).split(b"\n", 1)[0].strip()
    if not line:
        raise ValueError("empty request")
    return line


def serve(socket_path: str) -> int:
    model = load_model()

    if os.path.exists(socket_path):
        os.remove(socket_path)

    listener = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    should_stop = False

    def _handle_stop(_signum, _frame):
        nonlocal should_stop
        should_stop = True
        try:
            listener.close()
        except Exception:
            pass

    signal.signal(signal.SIGTERM, _handle_stop)
    signal.signal(signal.SIGINT, _handle_stop)

    try:
        listener.bind(socket_path)
        listener.listen(16)
        listener.settimeout(0.5)

        while not should_stop:
            try:
                conn, _ = listener.accept()
            except socket.timeout:
                continue
            except OSError:
                if should_stop:
                    break
                raise

            with conn:
                try:
                    line = _read_request_line(conn)
                    req = _parse_request(line)
                    synthesize_to_path(
                        model,
                        req["text"],
                        req["out_path"],
                        req["voice_prompt"],
                    )
                    _send_response(conn, {"status": "ok"})
                except Exception as err:  # keep daemon alive on request failures
                    try:
                        _send_response(conn, {"error": str(err)})
                    except OSError:
                        # Client disconnected before reading the error.
                        pass

        return 0
    finally:
        try:
            listener.close()
        except Exception:
            pass
        try:
            if os.path.exists(socket_path):
                os.remove(socket_path)
        except Exception:
            pass


def run_one_shot(text: str, out_path: str, voice_prompt: Optional[str]) -> int:
    model = load_model()
    synthesize_to_path(model, text, out_path, voice_prompt)
    return 0


def parse_args(argv: list[str]) -> argparse.Namespace:
    if "--one-shot" in argv:
        parser = argparse.ArgumentParser(description="claw-radio chatterbox one-shot")
        parser.add_argument("--one-shot", action="store_true", help="render once and exit")
        parser.add_argument("text", help="text to render")
        parser.add_argument("out_path", help="output wav path")
        parser.add_argument("--voice", default=None, help="voice prompt wav path")
        return parser.parse_args(argv)

    parser = argparse.ArgumentParser(description="claw-radio chatterbox daemon")
    parser.add_argument("socket", help="unix socket path")
    return parser.parse_args(argv)


def main(argv: list[str]) -> int:
    args = parse_args(argv)

    if getattr(args, "one_shot", False):
        return run_one_shot(args.text, args.out_path, args.voice)

    return serve(args.socket)


if __name__ == "__main__":
    try:
        raise SystemExit(main(sys.argv[1:]))
    except KeyboardInterrupt:
        raise SystemExit(0)
    except Exception as err:
        print(f"daemon error: {err}", file=sys.stderr)
        raise SystemExit(1)
