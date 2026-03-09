import json
import os
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path


DAEMON_PATH = Path(__file__).with_name("daemon.py")


def _wait_for_socket(socket_path: Path, timeout: float = 5.0) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        if socket_path.exists():
            return
        time.sleep(0.05)
    raise TimeoutError(f"socket did not become ready: {socket_path}")


def _send_request(socket_path: Path, payload: dict) -> dict:
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as client:
        client.settimeout(2)
        client.connect(str(socket_path))
        message = json.dumps(payload).encode("utf-8") + b"\n"
        client.sendall(message)
        data = b""
        while b"\n" not in data:
            chunk = client.recv(4096)
            if not chunk:
                break
            data += chunk

    line = data.split(b"\n", 1)[0].decode("utf-8")
    return json.loads(line)


def _read_log(path: Path) -> list[dict]:
    if not path.exists():
        return []
    entries = []
    with open(path, "r", encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if line:
                entries.append(json.loads(line))
    return entries


class DaemonScriptTest(unittest.TestCase):
    def setUp(self) -> None:
        self.tempdir = Path(tempfile.mkdtemp(prefix="claw-radio-tts-test-"))
        self.fake_modules = self.tempdir / "fake_modules"
        self.fake_modules.mkdir(parents=True, exist_ok=True)
        self.generate_log = self.tempdir / "generate.log"
        self.save_log = self.tempdir / "save.log"
        self._write_fake_modules()

    def tearDown(self) -> None:
        shutil.rmtree(self.tempdir, ignore_errors=True)

    def _write_fake_modules(self) -> None:
        chatterbox_dir = self.fake_modules / "chatterbox"
        chatterbox_dir.mkdir(parents=True, exist_ok=True)

        (chatterbox_dir / "__init__.py").write_text("", encoding="utf-8")
        (chatterbox_dir / "tts.py").write_text(
            """
import json
import os


class FakeTensor:
    def __init__(self, values):
        self.values = list(values)

    def __mul__(self, scalar):
        return FakeTensor([value * scalar for value in self.values])

    def __len__(self):
        return len(self.values)


class ChatterboxTTS:
    def __init__(self, device="cpu"):
        self.device = device
        self.sr = 24000

    @classmethod
    def from_pretrained(cls, device="cpu"):
        return cls(device=device)

    def generate(self, text, audio_prompt_path=None):
        path = os.environ.get("FAKE_CHATTERBOX_LOG")
        if path:
            with open(path, "a", encoding="utf-8") as fh:
                fh.write(json.dumps({
                    "text": text,
                    "audio_prompt_path": audio_prompt_path,
                    "device": self.device,
                }) + "\\n")
        return FakeTensor([0.25, -0.25])
""".strip()
            + "\n",
            encoding="utf-8",
        )

        (self.fake_modules / "torch.py").write_text(
            """
class _MPS:
    @staticmethod
    def is_available():
        return False


class _Backends:
    mps = _MPS()


class _Cuda:
    @staticmethod
    def is_available():
        return False


def clamp(wav_data, low, high):
    values = []
    for value in wav_data.values:
        if value < low:
            values.append(low)
        elif value > high:
            values.append(high)
        else:
            values.append(value)
    return type(wav_data)(values)


backends = _Backends()
cuda = _Cuda()
""".strip()
            + "\n",
            encoding="utf-8",
        )

        (self.fake_modules / "torchaudio.py").write_text(
            """
import json
import os


def save(path, wav_data, sample_rate):
    log_path = os.environ.get("FAKE_TORCHAUDIO_LOG")
    if log_path:
        with open(log_path, "a", encoding="utf-8") as fh:
                fh.write(json.dumps({
                    "path": path,
                    "sample_rate": sample_rate,
                    "size": len(wav_data) if hasattr(wav_data, "__len__") else 0,
                    "values": getattr(wav_data, "values", None),
                }) + "\\n")
    with open(path, "wb") as out:
        out.write(b"RIFF")
        out.write(b"FAKE")
""".strip()
            + "\n",
            encoding="utf-8",
        )

    def _base_env(self) -> dict:
        env = os.environ.copy()
        env["PYTHONPATH"] = f"{self.fake_modules}:{env.get('PYTHONPATH', '')}"
        env["FAKE_CHATTERBOX_LOG"] = str(self.generate_log)
        env["FAKE_TORCHAUDIO_LOG"] = str(self.save_log)
        return env

    def _start_daemon(self):
        socket_path = self.tempdir / "tts.sock"
        proc = subprocess.Popen(
            [sys.executable, str(DAEMON_PATH), str(socket_path)],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            env=self._base_env(),
        )
        try:
            _wait_for_socket(socket_path)
        except Exception:
            if proc.poll() is None:
                proc.terminate()
                proc.communicate(timeout=5)
                raise

            out, err = proc.communicate(timeout=5)
            raise RuntimeError(
                f"daemon failed to start (rc={proc.returncode}) stdout={out!r} stderr={err!r}"
            )
        return proc, socket_path

    def _stop_daemon(self, proc: subprocess.Popen) -> tuple[int, str, str]:
        if proc.poll() is None:
            proc.send_signal(signal.SIGTERM)
        out, err = proc.communicate(timeout=5)
        return proc.returncode, out, err

    def test_daemon_request_without_voice_uses_default_generate(self):
        proc, socket_path = self._start_daemon()
        try:
            out_path = self.tempdir / "out-no-voice.wav"
            resp = _send_request(
                socket_path,
                {
                    "text": "Hello world",
                    "out_path": str(out_path),
                },
            )
            self.assertEqual(resp, {"status": "ok"})
            self.assertTrue(out_path.exists())

            calls = _read_log(self.generate_log)
            self.assertEqual(len(calls), 1)
            self.assertEqual(calls[0]["text"], "Hello world")
            self.assertIsNone(calls[0]["audio_prompt_path"])
        finally:
            self._stop_daemon(proc)

    def test_daemon_request_with_voice_prompt_uses_audio_prompt(self):
        proc, socket_path = self._start_daemon()
        try:
            out_path = self.tempdir / "out-voice.wav"
            voice_path = self.tempdir / "voice.wav"
            voice_path.write_bytes(b"RIFFvoice")

            resp = _send_request(
                socket_path,
                {
                    "text": "Clone me",
                    "out_path": str(out_path),
                    "voice_prompt": str(voice_path),
                },
            )
            self.assertEqual(resp, {"status": "ok"})
            self.assertTrue(out_path.exists())

            calls = _read_log(self.generate_log)
            self.assertEqual(len(calls), 1)
            self.assertEqual(calls[0]["audio_prompt_path"], str(voice_path))
        finally:
            self._stop_daemon(proc)

    def test_daemon_invalid_out_path_returns_error_and_keeps_accepting(self):
        proc, socket_path = self._start_daemon()
        try:
            blocker = self.tempdir / "not-a-dir"
            blocker.write_text("x", encoding="utf-8")
            bad_out = blocker / "child.wav"

            bad_resp = _send_request(
                socket_path,
                {
                    "text": "Will fail",
                    "out_path": str(bad_out),
                },
            )
            self.assertIn("error", bad_resp)
            self.assertTrue(bad_resp["error"])

            good_out = self.tempdir / "recovered.wav"
            good_resp = _send_request(
                socket_path,
                {
                    "text": "Still alive",
                    "out_path": str(good_out),
                },
            )
            self.assertEqual(good_resp, {"status": "ok"})
            self.assertTrue(good_out.exists())
            self.assertIsNone(proc.poll())
        finally:
            self._stop_daemon(proc)

    def test_one_shot_creates_file(self):
        out_path = self.tempdir / "oneshot.wav"
        proc = subprocess.run(
            [
                sys.executable,
                str(DAEMON_PATH),
                "--one-shot",
                "Hello",
                str(out_path),
            ],
            capture_output=True,
            text=True,
            env=self._base_env(),
            check=False,
        )
        self.assertEqual(proc.returncode, 0, msg=proc.stderr)
        self.assertTrue(out_path.exists())

        calls = _read_log(self.generate_log)
        self.assertEqual(len(calls), 1)
        self.assertIsNone(calls[0]["audio_prompt_path"])

        saves = _read_log(self.save_log)
        self.assertEqual(len(saves), 1)
        self.assertEqual(saves[0]["sample_rate"], 24000)
        self.assertAlmostEqual(saves[0]["values"][0], 0.25, places=6)
        self.assertAlmostEqual(saves[0]["values"][1], -0.25, places=6)

    def test_one_shot_with_voice_uses_audio_prompt(self):
        out_path = self.tempdir / "oneshot-voice.wav"
        voice_path = self.tempdir / "voice.wav"
        voice_path.write_bytes(b"RIFFvoice")

        proc = subprocess.run(
            [
                sys.executable,
                str(DAEMON_PATH),
                "--one-shot",
                "Hello",
                str(out_path),
                "--voice",
                str(voice_path),
            ],
            capture_output=True,
            text=True,
            env=self._base_env(),
            check=False,
        )
        self.assertEqual(proc.returncode, 0, msg=proc.stderr)
        self.assertTrue(out_path.exists())

        calls = _read_log(self.generate_log)
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0]["audio_prompt_path"], str(voice_path))

    def test_sigterm_in_daemon_mode_exits_cleanly_without_traceback(self):
        proc, _socket_path = self._start_daemon()
        code, _out, err = self._stop_daemon(proc)
        self.assertEqual(code, 0, msg=err)
        self.assertNotIn("Traceback", err)


if __name__ == "__main__":
    unittest.main()
