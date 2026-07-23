#!/usr/bin/env python3
import os
import pathlib
import subprocess
import tempfile
import unittest

SCRIPTS = pathlib.Path(__file__).resolve().parent
RUN = SCRIPTS / "run.sh"
AGENTIC = SCRIPTS / "agentic.sh"


class CLIPreflightSafetyTest(unittest.TestCase):
    def run_without_allocating(self, command: list[str]) -> subprocess.CompletedProcess[str]:
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            artifacts = root / "artifacts"
            tmpdir = root / "tmp"
            tmpdir.mkdir()
            env = os.environ.copy()
            env["TMPDIR"] = str(tmpdir)
            env["SURFSK8S_E2E_ARTIFACTS"] = str(artifacts)
            result = subprocess.run(
                command,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                env=env,
                timeout=5,
            )
            self.assertFalse(artifacts.exists(), result.stdout + result.stderr)
            self.assertEqual(tuple(tmpdir.iterdir()), ())
            return result

    def test_run_help_exits_before_state_allocation(self):
        result = self.run_without_allocating([str(RUN), "--help"])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("usage:", result.stdout)

    def test_agentic_help_exits_before_state_allocation(self):
        result = self.run_without_allocating([str(AGENTIC), "--help"])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("usage:", result.stdout)

    def test_unknown_agentic_argument_fails_before_state_allocation(self):
        result = self.run_without_allocating([str(AGENTIC), "--unknown"])
        self.assertEqual(result.returncode, 2)
        self.assertIn("usage:", result.stderr)


if __name__ == "__main__":
    unittest.main()
