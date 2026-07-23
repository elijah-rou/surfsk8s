#!/usr/bin/env python3
import pathlib
import subprocess
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).with_name("bounded_capture.py").resolve()


class BoundedCaptureTest(unittest.TestCase):
    def test_keeps_deterministic_prefix_and_caps_file(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = pathlib.Path(temporary) / "raw.log"
            payload = bytes(range(256)) * 8
            result = subprocess.run(
                [str(SCRIPT), "--output", str(output), "--max-bytes", "513"],
                input=payload, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=5,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(output.read_bytes(), payload[:513])
            self.assertEqual(output.stat().st_size, 513)
            self.assertEqual((output.with_suffix(".log.truncated")).read_text(encoding="utf-8"), "capture exceeded 513 bytes\n")

    def test_appends_across_restarts_without_exceeding_cap(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = pathlib.Path(temporary) / "raw.log"
            output.write_bytes(b"abc")
            result = subprocess.run(
                [str(SCRIPT), "--output", str(output), "--max-bytes", "5"],
                input=b"defgh", stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=5,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(output.read_bytes(), b"abcde")
            self.assertLessEqual(output.stat().st_size, 5)


if __name__ == "__main__":
    unittest.main()
