#!/usr/bin/env python3
import fcntl
import os
import pathlib
import shlex
import subprocess
import tempfile
import textwrap
import unittest

SCRIPT = pathlib.Path(__file__).with_name("cleanup-owned.sh").resolve()
CLUSTER = "surfsk8s-e2e-test"


class CleanupHarness:
    def __init__(self, root: pathlib.Path) -> None:
        self.root = root
        self.state = root / "surfsk8s-e2e.state"
        self.bin = root / "bin"
        self.markers = root / "markers"
        self.state.mkdir()
        self.bin.mkdir()
        self.markers.mkdir()
        self.kubeconfig = self.state / "kubeconfig"
        self.kubeconfig.write_text("recovery credential\n", encoding="utf-8")
        self.helper = self.state / "cleanup-owned-e2e.sh"
        self.helper.write_bytes(SCRIPT.read_bytes())
        self.helper.chmod(0o700)
        self.config = self.state / "ownership.env"
        self._write_mock_tools()
        self.config.write_text(
            "\n".join(
                f"{key}={shlex.quote(str(value))}"
                for key, value in {
                    "tmux_socket": "surfsk8s-e2e-socket",
                    "tmux_socket_path": self.markers / "surfsk8s-e2e-socket",
                    "session": "surfsk8s-e2e-session",
                    "k3d_bin": self.bin / "k3d",
                    "cluster_name": CLUSTER,
                    "kubeconfig": self.kubeconfig,
                    "state_root": self.state,
                    "state_parent": self.root,
                    "docker_network": f"k3d-{CLUSTER}",
                    "docker_images_volume": f"k3d-{CLUSTER}-images",
                }.items()
            )
            + "\n",
            encoding="utf-8",
        )
        for marker in ("cluster", "container", "network", "volume", "tmux-session", "surfsk8s-e2e-socket"):
            (self.markers / marker).touch()

    def _write(self, name: str, body: str) -> None:
        path = self.bin / name
        path.write_text("#!/usr/bin/env bash\nset -u\n" + textwrap.dedent(body), encoding="utf-8")
        path.chmod(0o700)

    def _write_mock_tools(self) -> None:
        markers = shlex.quote(str(self.markers))
        self._write(
            "k3d",
            f"""
            markers={markers}
            if [[ "$1 $2" == "cluster list" ]]; then
              [[ -e "$markers/list-fail" ]] && exit 42
              [[ -e "$markers/cluster" ]] && printf '%s running\\n' {shlex.quote(CLUSTER)}
              exit 0
            fi
            if [[ "$1 $2" == "cluster delete" ]]; then
              [[ -e "$markers/delete-fail" ]] && exit 43
              rm -f "$markers/cluster"
              if [[ ! -e "$markers/delete-partial" ]]; then
                rm -f "$markers/container" "$markers/network" "$markers/volume"
              fi
              exit 0
            fi
            exit 44
            """,
        )
        self._write(
            "docker",
            f"""
            markers={markers}
            [[ -e "$markers/docker-list-fail" ]] && exit 45
            case "$1 $2" in
              "ps -a") [[ ! -e "$markers/container" ]] || printf 'k3d-{CLUSTER}-server-0\\nk3d-{CLUSTER}-serverlb\\n' ;;
              "network ls") [[ ! -e "$markers/network" ]] || printf 'k3d-{CLUSTER}\\n' ;;
              "volume ls") [[ ! -e "$markers/volume" ]] || printf 'k3d-{CLUSTER}-images\\n' ;;
              *) exit 46 ;;
            esac
            exit 0
            """,
        )
        self._write(
            "tmux",
            f"""
            markers={markers}
            case "$*" in
              *kill-session*) rm -f "$markers/tmux-session" ;;
              *kill-server*) rm -f "$markers/tmux-session" "$markers/surfsk8s-e2e-socket" ;;
              *has-session*) [[ -e "$markers/tmux-session" ]] ;;
              *) exit 47 ;;
            esac
            """,
        )

    def run(self, lock_timeout: int = 2) -> subprocess.CompletedProcess[str]:
        env = os.environ.copy()
        env["PATH"] = str(self.bin) + os.pathsep + env["PATH"]
        env["SURFSK8S_CLEANUP_LOCK_TIMEOUT_SECONDS"] = str(lock_timeout)
        return subprocess.run(
            [str(self.helper), str(self.config)], text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            env=env, timeout=8,
        )


class CleanupSafetyTest(unittest.TestCase):
    def test_interrupted_lock_is_bounded_and_retry_succeeds(self):
        with tempfile.TemporaryDirectory() as temporary:
            harness = CleanupHarness(pathlib.Path(temporary))
            lock_path = harness.state / ".cleanup.lock"
            with lock_path.open("w", encoding="utf-8") as holder:
                fcntl.flock(holder, fcntl.LOCK_EX | fcntl.LOCK_NB)
                blocked = harness.run(lock_timeout=1)
                self.assertNotEqual(blocked.returncode, 0)
                self.assertTrue(harness.state.is_dir())
                self.assertIn("retry cleanup", blocked.stderr)
            retried = harness.run()
            self.assertEqual(retried.returncode, 0, retried.stderr)
            self.assertFalse(harness.state.exists())

    def test_stale_lock_file_does_not_block_successful_cleanup(self):
        with tempfile.TemporaryDirectory() as temporary:
            harness = CleanupHarness(pathlib.Path(temporary))
            (harness.state / ".cleanup.lock").touch()
            result = harness.run()
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertFalse(harness.state.exists())
            repeated = subprocess.run(
                [str(SCRIPT), str(harness.config)], text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=5
            )
            self.assertEqual(repeated.returncode, 0, repeated.stderr)

    def test_cluster_list_failure_retains_all_recovery_state(self):
        with tempfile.TemporaryDirectory() as temporary:
            harness = CleanupHarness(pathlib.Path(temporary))
            (harness.markers / "list-fail").touch()
            result = harness.run()
            self.assertNotEqual(result.returncode, 0)
            self.assertTrue(harness.helper.is_file())
            self.assertTrue(harness.kubeconfig.is_file())
            self.assertTrue((harness.markers / "cluster").exists())
            self.assertIn(str(harness.helper), result.stderr)

    def test_partial_cluster_delete_retains_recovery_state(self):
        with tempfile.TemporaryDirectory() as temporary:
            harness = CleanupHarness(pathlib.Path(temporary))
            (harness.markers / "delete-partial").touch()
            result = harness.run()
            self.assertNotEqual(result.returncode, 0)
            self.assertTrue(harness.helper.is_file())
            self.assertTrue(harness.kubeconfig.is_file())
            self.assertIn("owned Docker", result.stderr)

    def test_delete_failure_then_retry_is_idempotent(self):
        with tempfile.TemporaryDirectory() as temporary:
            harness = CleanupHarness(pathlib.Path(temporary))
            (harness.markers / "delete-fail").touch()
            failed = harness.run()
            self.assertNotEqual(failed.returncode, 0)
            self.assertTrue(harness.state.is_dir())
            (harness.markers / "delete-fail").unlink()
            retried = harness.run()
            self.assertEqual(retried.returncode, 0, retried.stderr)
            self.assertFalse(harness.state.exists())


if __name__ == "__main__":
    unittest.main()
