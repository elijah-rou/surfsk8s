#!/usr/bin/env python3
import argparse
import importlib.util
import pathlib
import tempfile
import unittest

MODULE_PATH = pathlib.Path(__file__).with_name("pty_driver.py")
SPEC = importlib.util.spec_from_file_location("pty_driver", MODULE_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError(f"cannot load PTY driver module: {MODULE_PATH}")
pty_driver = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(pty_driver)


class NormalizeScreenTest(unittest.TestCase):
    def test_removes_terminal_control_sequences_without_changing_text(self):
        raw = "\x1b[?1049h\x1b[31mready\x1b[0m\r\nline\x1b]0;title\x07"
        self.assertEqual(pty_driver.normalize_terminal(raw), "ready\nline")

    def test_removes_backspace_overstrikes(self):
        self.assertEqual(pty_driver.normalize_terminal("ab\bcd"), "acd")


class ScaleCancellationTest(unittest.TestCase):
    def test_requires_replicas_prompt_to_disappear_from_detail_screen(self):
        detail = "surfsk8s · resource details\nscalable\ns scale"
        self.assertTrue(pty_driver.scale_prompt_cancelled(detail))
        self.assertFalse(pty_driver.scale_prompt_cancelled(detail + "\nreplicas> 1"))
        self.assertFalse(pty_driver.scale_prompt_cancelled("surfsk8s · resource catalog"))


class ContextSelectionTest(unittest.TestCase):
    def test_detects_current_context_selected_by_default(self):
        screen = "CONTEXTS\n [x] k3d-surfsk8s-e2e-test  (cluster · user) current"
        self.assertTrue(pty_driver.context_is_selected(screen, "k3d-surfsk8s-e2e-test"))
        self.assertFalse(pty_driver.context_is_selected(screen.replace("[x]", "[ ]"), "k3d-surfsk8s-e2e-test"))


class ProcessIdentityTest(unittest.TestCase):
    def test_proc_stat_start_time_handles_spaces_in_process_name(self):
        fields_after_name = ["S"] + [str(value) for value in range(4, 23)]
        fields_after_name[19] = "98765"
        stat = "123 (tmux: server) " + " ".join(fields_after_name) + "\n"

        self.assertEqual(pty_driver.proc_stat_start_time(stat), 98765)


class ScriptedLaunchTest(unittest.TestCase):
    def test_launch_explicitly_passes_isolated_xdg_config_home(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            binary = root / "surfsk8s binary"
            kubeconfig = root / "kube'config"
            xdg_config = root / "xdg config"
            artifacts = root / "artifacts"
            binary.write_text("", encoding="utf-8")
            binary.chmod(0o700)
            kubeconfig.write_text("", encoding="utf-8")
            args = argparse.Namespace(
                binary=str(binary),
                kubeconfig=str(kubeconfig),
                xdg_config=str(xdg_config),
                context="k3d-surfsk8s-e2e-test",
                namespace="surfsk8s-e2e",
                cluster="surfsk8s-e2e-test",
                session="surfsk8s-e2e-session",
                tmux_socket="surfsk8s-e2e-socket",
                artifacts=str(artifacts),
            )

            driver = pty_driver.Driver(args)
            launch = driver.build_launch_script()

            self.assertIn(
                "env TERM='xterm-256color' XDG_CONFIG_HOME="
                + pty_driver.shell_quote(str(xdg_config.resolve())),
                launch,
            )
            self.assertIn(pty_driver.shell_quote(str(binary.resolve())), launch)
            self.assertIn(pty_driver.shell_quote(str(kubeconfig.resolve())), launch)

    def test_raw_capture_command_uses_explicit_byte_cap(self):
        command = pty_driver.bounded_capture_command(
            pathlib.Path("/tmp/raw terminal.log"), pathlib.Path("/tmp/capture helper.py"), 4096
        )
        self.assertIn("'/tmp/capture helper.py'", command)
        self.assertIn("--max-bytes 4096", command)
        self.assertIn("'/tmp/raw terminal.log'", command)
        self.assertNotIn("cat >>", command)


class ClusterNameTest(unittest.TestCase):
    def test_accepts_bounded_dns_label(self):
        self.assertEqual(
            pty_driver.validate_cluster_name("surfsk8s-e2e-a1b2c3"),
            "surfsk8s-e2e-a1b2c3",
        )

    def test_rejects_unsafe_or_ambiguous_names(self):
        for name in ("", "UPPER", "-bad", "bad-", "bad_name", "surfsk8s-e2e-" + "a" * 19, "a" * 64, "other-a1b2"):
            with self.subTest(name=name):
                with self.assertRaises(ValueError):
                    pty_driver.validate_cluster_name(name)


if __name__ == "__main__":
    unittest.main()
