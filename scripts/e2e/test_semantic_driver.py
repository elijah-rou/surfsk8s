#!/usr/bin/env python3
from __future__ import annotations

import importlib.util
import json
import pathlib
import random
import sys
import tempfile
import time
import types
import unittest

MODULE_PATH = pathlib.Path(__file__).with_name("semantic_driver.py")


def load_module():
    spec = importlib.util.spec_from_file_location("semantic_driver", MODULE_PATH)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load semantic driver module: {MODULE_PATH}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


class SemanticDriverContractTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.semantic = load_module()

    def test_classifies_mutually_exclusive_stable_states(self):
        cases = {
            "surfsk8s · select contexts\n[x] k3d-surfsk8s-e2e-one\nspace toggle  enter connect  q quit": "context_picker",
            "surfsk8s · resource catalog\nGROUPS\nFavourites\nr resource-find  : commands  esc clear/back": "catalog",
            "surfsk8s · Favourites\nPods\nenter open resource  : commands  esc clear/back": "group_resources",
            "surfsk8s · commands\npods\nwidgets\ntype to filter  enter run  esc clear/close": "commands",
            "surfsk8s · resource finder\nPods\nWidgets\ntype to filter  enter open  esc close": "resource_finder",
            "pods\nlog-marker  Running\nenter open  / filter  r resource-find  esc back": "pods_list",
            "widgets · surfsk8s.dev\nalpha blue Stable\nenter open  / filter  r resource-find  esc back": "resource_list",
            "surfsk8s · pod details\nlog-marker\nl logs  esc back": "pod_detail",
            "surfsk8s · resource details\nalpha blue\nesc back": "resource_detail",
            "surfsk8s · pod logs\nSURFSK8S_E2E_LOG_MARKER_7d9f3a\nesc back": "logs",
            "SURFSK8S_SHELL_RESTORED rc=0": "shell_restored",
        }
        for screen, expected in cases.items():
            with self.subTest(expected=expected):
                self.assertEqual(self.semantic.observe(screen, "k3d-surfsk8s-e2e-one").state.value, expected)

    def test_nonzero_shell_restoration_fails_closed(self):
        with self.assertRaisesRegex(self.semantic.SemanticFailure, "nonzero"):
            self.semantic.observe("SURFSK8S_SHELL_RESTORED rc=1", "k3d-surfsk8s-e2e-one")

    def test_modal_and_input_states_are_mutually_exclusive(self):
        confirmation = "surfsk8s · confirm action\ndelete widget\nenter confirm  esc cancel"
        prompt = "surfsk8s · resource details\nscalable\nreplicas> 2\nesc cancel"
        self.assertEqual(self.semantic.observe(confirmation, "ctx").state.value, "confirmation")
        self.assertEqual(self.semantic.observe(prompt, "ctx").state.value, "input_prompt")

    def test_unknown_and_ambiguous_screens_fail_closed(self):
        with self.assertRaises(self.semantic.SemanticFailure):
            self.semantic.observe("random shell output", "k3d-surfsk8s-e2e-one")
        with self.assertRaises(self.semantic.SemanticFailure):
            self.semantic.observe(
                "surfsk8s · commands\nsurfsk8s · resource finder\ntype to filter  enter run  enter open",
                "k3d-surfsk8s-e2e-one",
            )

    def test_extracts_only_visible_allowlisted_affordances_and_targets(self):
        observation = self.semantic.observe(
            "surfsk8s · pod details\nlog-marker\nl logs  d delete  x exec  p port-forward  esc back",
            "k3d-surfsk8s-e2e-one",
        )
        self.assertEqual(observation.targets, ("log-marker",))
        self.assertEqual(observation.affordances, ("back", "logs"))
        candidate_ids = [candidate.id for candidate in self.semantic.candidates(observation, frozenset())]
        self.assertEqual(candidate_ids, ["open_logs"])
        for forbidden in ("delete", "edit", "exec", "port-forward", "restart", "scale"):
            self.assertNotIn(forbidden, " ".join(candidate_ids))

    def test_meaningful_visible_content_changes_fingerprint(self):
        red = self.semantic.observe(
            "surfsk8s · ns:surfsk8s-e2e · widgets · surfsk8s.dev · 1 row\nalpha red Stable\nenter open  esc back",
            "ctx",
        )
        green = self.semantic.observe(
            "surfsk8s · ns:surfsk8s-e2e · widgets · surfsk8s.dev · 1 row\nalpha green Stable\nenter open  esc back",
            "ctx",
        )
        self.assertNotEqual(red.fingerprint, green.fingerprint)

    def test_context_and_ages_are_canonicalized(self):
        first = self.semantic.observe(
            "surfsk8s · select contexts\n[x] k3d-surfsk8s-e2e-abc\nAGE 12s\nenter connect  q quit",
            "k3d-surfsk8s-e2e-abc",
        )
        second = self.semantic.observe(
            "surfsk8s · select contexts\n[x] k3d-surfsk8s-e2e-xyz\nAGE 2m13s\nenter connect  q quit",
            "k3d-surfsk8s-e2e-xyz",
        )
        self.assertEqual(first.fingerprint, second.fingerprint)
        self.assertNotEqual(first.raw_hash, second.raw_hash)

    def test_distinct_seeds_can_choose_different_valid_routes(self):
        actions = [self.semantic.Action("open_commands", 10), self.semantic.Action("open_resource_finder", 10)]
        selected = {self.semantic.choose_action(actions, random.Random(seed)).id for seed in range(16)}
        self.assertEqual(selected, {"open_commands", "open_resource_finder"})

    def test_seed_is_uint64_and_policy_is_reproducible(self):
        for invalid in ("", "-1", "+1", "18446744073709551616", "1.0"):
            with self.subTest(invalid=invalid):
                with self.assertRaises(ValueError):
                    self.semantic.parse_seed(invalid)
        self.assertEqual(self.semantic.parse_seed("18446744073709551615"), (1 << 64) - 1)
        actions = [self.semantic.Action("a", 1), self.semantic.Action("b", 1), self.semantic.Action("c", 1)]
        first = self.semantic.choose_action(actions, random.Random(743389))
        second = self.semantic.choose_action(list(reversed(actions)), random.Random(743389))
        self.assertEqual(first, second)

    def test_finder_requires_target_specific_filter_before_enter(self):
        mixed = self.semantic.observe(
            "surfsk8s · resource finder\nPods\nWidgets\ntype to filter  enter open  esc close",
            "ctx",
        )
        ids = tuple(action.id for action in self.semantic.candidates(mixed, frozenset()))
        self.assertNotIn("open_resource:pods", ids)
        self.assertNotIn("open_resource:widgets", ids)
        self.assertIn("filter_resource:widgets", ids)
        filtered = self.semantic.observe(
            "surfsk8s · resource finder\nWidgets\ntype to filter  enter open  esc close",
            "ctx",
        )
        self.assertEqual(
            tuple(action.id for action in self.semantic.candidates(filtered, frozenset())),
            ("open_resource:widgets",),
        )

    def test_open_detail_exact_filters_each_named_row_before_enter(self):
        mixed_screen = (
            "surfsk8s · ns:surfsk8s-e2e · widgets · surfsk8s.dev · 2 rows\n"
            "alpha blue Stable\nscalable 1/1 Available\nenter open  / filter  esc back"
        )

        class FakeDriver:
            context = "ctx"
            session = "session"

            def __init__(self, artifacts, target):
                self.artifacts = artifacts
                self.target = target
                self.events = []

            def tmux(self, *args, **kwargs):
                return types.SimpleNamespace(returncode=0)

            def literal(self, value):
                self.events.append(("literal", value))

            def key(self, value):
                self.events.append(("key", value))

            def capture(self):
                self.events.append(("capture", self.target))
                return (
                    "surfsk8s · ns:surfsk8s-e2e · widgets · surfsk8s.dev · 1 row\n"
                    f"{self.target} selected\n/={self.target}\nenter open  / filter  esc back"
                )

        with tempfile.TemporaryDirectory() as temporary:
            for target in ("alpha", "scalable"):
                with self.subTest(target=target):
                    driver = FakeDriver(pathlib.Path(temporary), target)
                    walk = self.semantic.SemanticWalkthrough(driver, 1)
                    walk.deadline = time.monotonic() + 2
                    before = self.semantic.observe(mixed_screen, driver.context)
                    old_interval = self.semantic.POLL_INTERVAL_SECONDS
                    self.semantic.POLL_INTERVAL_SECONDS = 0
                    try:
                        predicate = walk._execute(self.semantic.Action(f"open_detail:{target}"), before)
                    finally:
                        self.semantic.POLL_INTERVAL_SECONDS = old_interval
                    actions = [event for event in driver.events if event[0] != "capture"]
                    self.assertEqual(
                        actions,
                        [("literal", "/"), ("literal", f"={target}"), ("key", "Enter"), ("key", "Enter")],
                    )
                    expected = self.semantic.observe(
                        f"surfsk8s · resource details\nName: {target}\nesc back", driver.context
                    )
                    wrong = self.semantic.observe(
                        "surfsk8s · resource details\nName: other\nesc back", driver.context
                    )
                    self.assertTrue(predicate(expected))
                    self.assertFalse(predicate(wrong))
                    filtered = self.semantic.observe(driver.capture(), driver.context)
                    after_detail = frozenset({"visible_detail"})
                    self.assertEqual(
                        tuple(action.id for action in self.semantic.candidates(filtered, after_detail)),
                        ("clear_filter",),
                    )

    def test_connect_catalog_waits_for_discovery_convergence_without_crd_count_dependency(self):
        before = self.semantic.observe(
            "surfsk8s · select contexts\n[x] ctx\nenter connect  q quit", "ctx"
        )
        partial = (
            "surfsk8s · resource catalog\nGROUPS\n2 Running\n1 Running\nCRDs  (999 resources)\n"
            "discovery-partial\nr resource-find  enter open group  : commands"
        )
        converged = partial.replace("discovery-partial\n", "")

        class FakeDriver:
            context = "ctx"
            session = "session"

            def __init__(self, artifacts):
                self.artifacts = artifacts
                self.screens = [partial, partial, partial, converged, converged, converged]
                self.capture_count = 0

            def tmux(self, *args, **kwargs):
                return types.SimpleNamespace(returncode=0)

            def key(self, value):
                self.key_sent = value

            def capture(self):
                self.capture_count += 1
                return self.screens.pop(0) if self.screens else converged

        with tempfile.TemporaryDirectory() as temporary:
            driver = FakeDriver(pathlib.Path(temporary))
            walk = self.semantic.SemanticWalkthrough(driver, 1)
            walk.deadline = time.monotonic() + 2
            predicate = walk._execute(self.semantic.Action("connect"), before)
            old_interval = self.semantic.POLL_INTERVAL_SECONDS
            self.semantic.POLL_INTERVAL_SECONDS = 0
            try:
                result = walk._poll_transition(before, predicate, 0)
            finally:
                self.semantic.POLL_INTERVAL_SECONDS = old_interval

        self.assertEqual(driver.key_sent, "Enter")
        self.assertEqual(driver.capture_count, 6)
        self.assertNotIn("discovery-partial", result.canonical_screen)

    def test_no_candidates_and_destructive_replay_actions_fail_closed(self):
        observation = self.semantic.observe(
            "surfsk8s · resource details\nalpha blue\nd delete", "k3d-surfsk8s-e2e-one"
        )
        self.assertEqual(self.semantic.candidates(observation, frozenset()), ())
        for action in ("delete", "edit", "exec", "port-forward", "restart", "scale"):
            with self.subTest(action=action):
                with self.assertRaises(self.semantic.SemanticFailure):
                    self.semantic.validate_action_id(action)

    def test_bounds_reject_invalid_values_and_stagnation(self):
        for value in (0, -1, self.semantic.MAX_ACTIONS + 1):
            with self.assertRaises(ValueError):
                self.semantic.validate_max_actions(value)
        progress = self.semantic.Progress()
        fingerprint = "a" * 64
        for _ in range(self.semantic.MAX_IDENTICAL_STATES):
            progress.record(fingerprint, frozenset())
        with self.assertRaises(self.semantic.SemanticFailure):
            progress.record(fingerprint, frozenset())
        progress = self.semantic.Progress()
        for index in range(self.semantic.MAX_DECISIONS_WITHOUT_GOAL):
            progress.record(f"{index:064x}", frozenset())
        with self.assertRaises(self.semantic.SemanticFailure):
            progress.record("f" * 64, frozenset())

    def test_trace_round_trip_and_strict_validation(self):
        record = self.semantic.TraceRecord(
            schema=self.semantic.TRACE_SCHEMA,
            seed=743389,
            step=0,
            state="catalog",
            fingerprint="a" * 64,
            affordances=("commands",),
            targets=("pods",),
            capabilities=("resize",),
            candidates=("open_commands",),
            action="open_commands",
            result_state="commands",
            result_fingerprint="b" * 64,
            new_goals=("commands",),
        )
        with tempfile.TemporaryDirectory() as temporary:
            path = pathlib.Path(temporary) / "trace.jsonl"
            self.semantic.write_trace(path, [record])
            self.assertEqual(self.semantic.read_trace(path), (record,))
            data = json.loads(path.read_text(encoding="utf-8"))
            self.assertNotIn("screen", data)
            self.assertNotIn("kubeconfig", path.read_text(encoding="utf-8").lower())

    def test_trace_rejects_malformed_oversized_order_and_unknown_action(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = pathlib.Path(temporary) / "trace.jsonl"
            path.write_text("not-json\n", encoding="utf-8")
            with self.assertRaises(self.semantic.SemanticFailure):
                self.semantic.read_trace(path)
            path.write_bytes(b"x" * (self.semantic.MAX_TRACE_BYTES + 1))
            with self.assertRaises(self.semantic.SemanticFailure):
                self.semantic.read_trace(path)
            base = {
                "schema": self.semantic.TRACE_SCHEMA, "seed": 1, "step": 1,
                "state": "catalog", "fingerprint": "a" * 64, "affordances": [], "targets": [],
                "capabilities": ["resize"], "candidates": ["delete"], "action": "delete", "result_state": "catalog",
                "result_fingerprint": "b" * 64, "new_goals": [],
            }
            path.write_text(json.dumps(base) + "\n", encoding="utf-8")
            with self.assertRaises(self.semantic.SemanticFailure):
                self.semantic.read_trace(path)

    def test_widget_watch_uses_absence_create_uid_precondition_and_post_absence(self):
        class FakeDriver:
            context = "ctx"
            namespace = "surfsk8s-e2e"
            session = "session"

            def __init__(self, artifacts):
                self.artifacts = artifacts
                self.calls = []
                self.deletion_body = None
                self.uid_reads = iter(("", "123e4567-e89b-12d3-a456-426614174000", ""))
                self.screens = iter(("semantic-beta-0000000000000001", ""))

            def tmux(self, *args, **kwargs):
                return types.SimpleNamespace(returncode=0)

            def capture(self):
                return next(self.screens, "")

            def kubectl(self, *args, timeout=20):
                self.calls.append((args, timeout))
                if args[:2] == ("get", "widget"):
                    return types.SimpleNamespace(stdout=next(self.uid_reads))
                if args[:2] == ("create", "-f"):
                    return types.SimpleNamespace(stdout="123e4567-e89b-12d3-a456-426614174000")
                if args[:2] == ("delete", "--raw"):
                    self.deletion_body = json.loads(pathlib.Path(args[args.index("-f") + 1]).read_text())
                return types.SimpleNamespace(stdout="")

        with tempfile.TemporaryDirectory() as temporary:
            driver = FakeDriver(pathlib.Path(temporary))
            walk = self.semantic.SemanticWalkthrough(driver, 1)
            walk.deadline = time.monotonic() + 10
            old_interval = self.semantic.POLL_INTERVAL_SECONDS
            self.semantic.POLL_INTERVAL_SECONDS = 0
            try:
                walk._widget_watch()
            finally:
                self.semantic.POLL_INTERVAL_SECONDS = old_interval
            commands = [call[0] for call in driver.calls]
            self.assertEqual(commands[0][:3], ("get", "widget", walk.transient_widget_name))
            self.assertEqual(commands[1][:2], ("create", "-f"))
            delete = next(command for command in commands if command[:2] == ("delete", "--raw"))
            self.assertIn(f"/widgets/{walk.transient_widget_name}", delete[2])
            delete_options = pathlib.Path(delete[delete.index("-f") + 1])
            self.assertFalse(delete_options.exists())
            self.assertEqual(driver.deletion_body["preconditions"]["uid"], "123e4567-e89b-12d3-a456-426614174000")
            self.assertEqual(commands[-1][:3], ("get", "widget", walk.transient_widget_name))
            self.assertFalse((pathlib.Path(temporary) / "semantic-widget-transient.yaml").exists())

    def test_wall_deadline_prevents_new_blocking_kubectl(self):
        driver = types.SimpleNamespace()
        walk = self.semantic.SemanticWalkthrough(driver, 1, monotonic=lambda: 10.0)
        walk.deadline = 10.0
        with self.assertRaisesRegex(self.semantic.SemanticFailure, "wall deadline"):
            walk._remaining_timeout()

    def test_transition_requires_three_consecutive_semantically_identical_captures(self):
        transient = "surfsk8s · resource catalog\nGROUPS\nr resource-find  enter open group  : commands"
        stable = transient + "  / filter  esc clear/back"

        class FakeDriver:
            context = "k3d-surfsk8s-e2e-one"
            session = "surfsk8s-e2e-session"

            def __init__(self, artifacts):
                self.artifacts = artifacts
                self.screens = [transient, stable, stable, stable]
                self.capture_count = 0

            def tmux(self, *args, **kwargs):
                return types.SimpleNamespace(returncode=0)

            def capture(self):
                self.capture_count += 1
                return self.screens.pop(0) if self.screens else stable

        with tempfile.TemporaryDirectory() as temporary:
            driver = FakeDriver(pathlib.Path(temporary))
            walk = self.semantic.SemanticWalkthrough(driver, 1)
            walk.deadline = time.monotonic() + 2
            old_interval = self.semantic.POLL_INTERVAL_SECONDS
            self.semantic.POLL_INTERVAL_SECONDS = 0
            try:
                before = self.semantic.observe(transient, driver.context)
                result = walk._poll_transition(
                    before,
                    lambda observation: observation.state == self.semantic.ScreenState.CATALOG,
                    0,
                )
            finally:
                self.semantic.POLL_INTERVAL_SECONDS = old_interval
            self.assertEqual(result.affordances, ("back", "commands", "filter", "open", "resource_finder"))
            self.assertEqual(driver.capture_count, 4)

    def test_replay_checks_state_fingerprint_and_current_candidates(self):
        observation = self.semantic.observe(
            "surfsk8s · resource catalog\nGROUPS\nr resource-find  : commands  esc clear/back",
            "k3d-surfsk8s-e2e-one",
        )
        record = self.semantic.TraceRecord(
            schema=self.semantic.TRACE_SCHEMA, seed=1, step=0, state="catalog",
            fingerprint=observation.fingerprint, affordances=observation.affordances,
            targets=observation.targets, capabilities=observation.capabilities,
            candidates=("open_commands", "open_resource_finder", "resize_large", "resize_small"),
            action="open_commands", result_state="commands", result_fingerprint="b" * 64,
            new_goals=(),
        )
        self.semantic.validate_replay_decision(record, observation, self.semantic.candidates(observation, frozenset()))
        with self.assertRaisesRegex(self.semantic.SemanticFailure, "fingerprint"):
            self.semantic.validate_replay_decision(record.__class__(**{**record.__dict__, "fingerprint": "c" * 64}), observation, self.semantic.candidates(observation, frozenset()))
        with self.assertRaisesRegex(self.semantic.SemanticFailure, "candidate"):
            self.semantic.validate_replay_decision(record.__class__(**{**record.__dict__, "action": "quit"}), observation, self.semantic.candidates(observation, frozenset()))


if __name__ == "__main__":
    unittest.main()
