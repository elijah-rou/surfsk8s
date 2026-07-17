#!/usr/bin/env python3
import importlib.util
import pathlib
import unittest

MODULE_PATH = pathlib.Path(__file__).with_name("pty_driver.py")
SPEC = importlib.util.spec_from_file_location("pty_driver", MODULE_PATH)
pty_driver = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(pty_driver)


class NormalizeScreenTest(unittest.TestCase):
    def test_removes_terminal_control_sequences_without_changing_text(self):
        raw = "\x1b[?1049h\x1b[31mready\x1b[0m\r\nline\x1b]0;title\x07"
        self.assertEqual(pty_driver.normalize_terminal(raw), "ready\nline")

    def test_removes_backspace_overstrikes(self):
        self.assertEqual(pty_driver.normalize_terminal("ab\bcd"), "acd")


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
