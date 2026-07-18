#!/usr/bin/env python3
"""Credential-free semantic observation/action walkthrough for the live TUI."""

from __future__ import annotations

import argparse
from collections.abc import Callable, Iterable
from dataclasses import asdict, dataclass, field
import math
from enum import Enum
import hashlib
import json
import os
import pathlib
import random
import re
import subprocess
import sys
import time

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2]))
from scripts.e2e.pty_driver import Driver, POLL_INTERVAL_SECONDS, ScenarioFailure, normalize_terminal

TRACE_SCHEMA = 1
DEFAULT_SEED = 743389
UINT64_MAX = (1 << 64) - 1
MAX_ACTIONS = 48
WALL_DEADLINE_SECONDS = 300.0
TRANSITION_DEADLINE_SECONDS = 30.0
MAX_IDENTICAL_STATES = 6
MAX_DECISIONS_WITHOUT_GOAL = 8
MAX_TRACE_RECORDS = 64
MAX_TRACE_BYTES = 1024 * 1024
MAX_DIAGNOSTIC_SCREEN_BYTES = 2 * 1024 * 1024
MIN_ACTIONS = 10
STABLE_OBSERVATION_POLLS = 3
LOG_MARKER = "SURFSK8S_E2E_LOG_MARKER_7d9f3a"


class SemanticFailure(ScenarioFailure):
    pass


class ScreenState(Enum):
    CONTEXT_PICKER = "context_picker"
    CATALOG = "catalog"
    GROUP_RESOURCES = "group_resources"
    COMMANDS = "commands"
    RESOURCE_FINDER = "resource_finder"
    PODS_LIST = "pods_list"
    RESOURCE_LIST = "resource_list"
    POD_DETAIL = "pod_detail"
    RESOURCE_DETAIL = "resource_detail"
    LOGS = "logs"
    CONFIRMATION = "confirmation"
    INPUT_PROMPT = "input_prompt"
    SHELL_RESTORED = "shell_restored"


@dataclass(frozen=True)
class Observation:
    state: ScreenState
    fingerprint: str
    raw_hash: str
    affordances: tuple[str, ...]
    targets: tuple[str, ...]
    capabilities: tuple[str, ...]
    canonical_screen: str = field(repr=False)


@dataclass(frozen=True, order=True)
class Action:
    id: str
    weight: int = field(default=1, compare=False)


@dataclass(frozen=True)
class TraceRecord:
    schema: int
    seed: int
    step: int
    state: str
    fingerprint: str
    affordances: tuple[str, ...]
    targets: tuple[str, ...]
    capabilities: tuple[str, ...]
    candidates: tuple[str, ...]
    action: str
    result_state: str
    result_fingerprint: str
    new_goals: tuple[str, ...]


@dataclass
class Progress:
    goals: set[str] = field(default_factory=set)
    states: set[str] = field(default_factory=set)
    identical_count: int = 0
    decisions_without_goal: int = 0
    last_fingerprint: str | None = None

    def record(self, fingerprint: str, new_goals: frozenset[str]) -> None:
        if fingerprint == self.last_fingerprint:
            self.identical_count += 1
        else:
            self.identical_count = 1
            self.last_fingerprint = fingerprint
        if self.identical_count > MAX_IDENTICAL_STATES:
            raise SemanticFailure(f"stagnation: observation repeated more than {MAX_IDENTICAL_STATES} times")
        if new_goals:
            self.goals.update(new_goals)
            self.decisions_without_goal = 0
        else:
            self.decisions_without_goal += 1
        if self.decisions_without_goal > MAX_DECISIONS_WITHOUT_GOAL:
            raise SemanticFailure(
                f"stagnation: more than {MAX_DECISIONS_WITHOUT_GOAL} decisions without a new goal"
            )


SAFE_BASE_ACTIONS = frozenset({
    "select_context", "connect", "open_commands", "open_resource_finder", "back", "clear_filter",
    "open_logs", "resize_small", "resize_large", "cancel_confirmation", "cancel_input",
    "widget_watch", "quit",
})
SAFE_PREFIXES = ("run_command:", "filter_resource:", "open_resource:", "open_detail:")
SAFE_TARGETS = frozenset({"pods", "widgets", "deployments", "catalog", "log-marker", "alpha", "scalable"})
DESTRUCTIVE_TERMS = frozenset({"delete", "edit", "exec", "port-forward", "restart", "scale"})
WALK_GOALS = frozenset({
    "connected", "three_states", "dynamic_route", "visible_detail", "back", "resized",
    "log_marker", "widget_watch", "finder",
})
REQUIRED_GOALS = WALK_GOALS | {"api_unchanged"}
ENVIRONMENT_CAPABILITIES = ("resize",)

_AFFORDANCE_MARKERS = {
    "back": ("esc back", "esc clear/back", "esc close"),
    "cancel": ("esc cancel",),
    "commands": (": commands",),
    "connect": ("enter connect",),
    "filter": ("/ filter", "type to filter"),
    "logs": ("l logs",),
    "open": ("enter open", "enter run"),
    "quit": ("q quit",),
    "resource_finder": ("r resource-find",),
    "select": ("space toggle",),
}


def parse_seed(value: str) -> int:
    if re.fullmatch(r"0|[1-9][0-9]{0,19}", value) is None:
        raise ValueError("seed must be a decimal uint64")
    seed = int(value)
    if seed > UINT64_MAX:
        raise ValueError("seed must be a decimal uint64")
    return seed


def validate_max_actions(value: int) -> int:
    if value < 1 or value > MAX_ACTIONS:
        raise ValueError(f"max actions must be 1..{MAX_ACTIONS}")
    return value


def canonicalize_screen(screen: str, context: str) -> str:
    normalized = normalize_terminal(screen).replace(context, "<CONTEXT>")
    normalized = re.sub(r"k3d-surfsk8s-e2e-[a-z0-9-]+", "<CONTEXT>", normalized)
    normalized = re.sub(r"k3d-surfsk8s-e2[^\s│]*", "<CONTEXT>", normalized)
    normalized = re.sub(r"\bscalable-[a-z0-9]{6,}-[a-z0-9…]+", "scalable-<POD>", normalized)
    normalized = re.sub(r"\b\d{4}-\d\d-\d\dT\d\d:\d\d(?::\d\d(?:\.\d+)?)?(?:Z|[+-]\d\d:\d\d)\b", "<TIMESTAMP>", normalized)
    normalized = re.sub(r"(?im)(Resource version:\s*)\d+", r"\1<VERSION>", normalized)
    normalized = re.sub(r"\b(?:10|172|192)\.\d+\.\d+\.\d+\b", "<IP>", normalized)
    normalized = re.sub(r"\b(?:\d+d)?(?:\d+h)?(?:\d+m)?\d+s\b", "<AGE>", normalized)
    normalized = re.sub(r"\b\d+(?:\.\d+)?(?:ms|s|m|h|d)\b", "<AGE>", normalized)
    normalized = re.sub(r"[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏]", "", normalized)
    return "\n".join(line.rstrip() for line in normalized.splitlines()).strip()


def canonical_semantic_rows(canonical_screen: str) -> tuple[str, ...]:
    rows: list[str] = []
    for line in canonical_screen.splitlines():
        compact = " ".join(line.strip().split())
        if not compact or re.fullmatch(r"[─│┬┼┤├┴┐└┘ ]+", compact):
            continue
        rows.append(compact[:512])
        if len(rows) == 256:
            break
    return tuple(rows)


def canonical_replay_rows(
    canonical_screen: str,
    state: ScreenState,
    project_crd_count: bool = False,
) -> tuple[str, ...]:
    rows = canonical_semantic_rows(canonical_screen)
    projected: list[str] = []
    in_resource_usage = False
    for row in rows:
        row = re.sub(r"(?<=<CONTEXT>):discovery-partial(?=\s|$)", "", row)
        if state in {ScreenState.CATALOG, ScreenState.RESOURCE_FINDER}:
            row = re.sub(r"\brows:\d+/\d+\b", "rows:<DISCOVERY>", row)
        if state == ScreenState.CATALOG and project_crd_count:
            group_count = re.fullmatch(r"(?:│\s*)?(.+?)\s+\(\d+ resources\)(?:\s+│)?", row)
            if group_count is not None:
                if group_count.group(1).strip() == "CRDs":
                    projected.append("CRDs group available")
                continue
        if state == ScreenState.RESOURCE_FINDER:
            resource_row = re.fullmatch(
                r"(?:│\s*)?(?:★\s+)?([^ ]+).*\([^()]+ · (?:ns|cluster)\)(?:\s+│)?",
                row,
            )
            if resource_row is not None and resource_row.group(1).lower() not in {
                "deployments", "pods", "widgets",
            }:
                continue
        if state == ScreenState.POD_DETAIL:
            if re.match(r"^(?:│\s*)?Resource usage\b", row):
                if projected and re.fullmatch(r"╭─+╮", projected[-1]):
                    projected[-1] = "<RESOURCE_USAGE_BORDER>"
                in_resource_usage = True
            usage = re.match(r"^(│\s*)?-\s*(CPU|Memory|Ephemeral)\b", row)
            if usage is not None:
                border = " │" if row.endswith("│") else ""
                row = f"{usage.group(1) or ''}- {usage.group(2)} <VOLATILE_USAGE>{border}"
            elif in_resource_usage and re.fullmatch(r"╰─+╯", row):
                row = "<RESOURCE_USAGE_BORDER>"
                in_resource_usage = False
        projected.append(row)
    return tuple(projected)


def _classify(screen: str) -> ScreenState:
    restoration = re.search(r"SURFSK8S_SHELL_RESTORED rc=([^\s]+)", screen)
    if restoration is not None and restoration.group(1) != "0":
        raise SemanticFailure(f"shell restoration reported nonzero rc={restoration.group(1)}")
    signatures: list[tuple[ScreenState, bool]] = [
        (ScreenState.SHELL_RESTORED, restoration is not None),
        (ScreenState.CONTEXT_PICKER, bool(re.search(r"(?m)^surfsk8s · (?:select|add) contexts$", screen))),
        (ScreenState.CATALOG, bool(re.search(r"(?m)^surfsk8s · resource catalog$", screen)) or ("GROUPS" in screen and "enter open group" in screen and "r resource-find" in screen)),
        (ScreenState.GROUP_RESOURCES, bool(re.search(r"(?m)^surfsk8s · .+$", screen)) and "enter open resource" in screen and ": commands" in screen),
        (ScreenState.COMMANDS, bool(re.search(r"(?m)^surfsk8s · commands$", screen))),
        (ScreenState.RESOURCE_FINDER, bool(re.search(r"(?m)^surfsk8s · resource finder$", screen))),
        (ScreenState.POD_DETAIL, bool(re.search(r"(?m)^surfsk8s · pod details$", screen))),
        (ScreenState.RESOURCE_DETAIL, "replicas>" not in screen and bool(re.search(r"(?m)^surfsk8s · resource details(?: · active pane: \w+)?$", screen))),
        (ScreenState.LOGS, bool(re.search(r"(?m)^surfsk8s · (?:pod|deployment|container) logs(?: · .+)?$", screen))),
        (ScreenState.CONFIRMATION, bool(re.search(r"(?m)^surfsk8s · confirm action$", screen))),
        (ScreenState.INPUT_PROMPT, "replicas>" in screen),
        (ScreenState.PODS_LIST, "enter open" in screen and "log-marker" in screen and ("esc back" in screen or bool(re.search(r"(?m)^surfsk8s · .* · pods ·", screen)))),
        (ScreenState.RESOURCE_LIST, "enter open" in screen and bool(re.search(r"(?mi)(?:^widgets · surfsk8s\.dev$|^deployments(?: · apps)?$|^surfsk8s · .* · (?:widgets · surfsk8s\.dev|deployments(?: · apps)?) ·)", screen))),
    ]
    matches = [state for state, matched in signatures if matched]
    # Shell output can retain the final alternate-screen title in tmux history.
    if ScreenState.SHELL_RESTORED in matches:
        return ScreenState.SHELL_RESTORED
    if len(matches) != 1:
        names = ", ".join(state.value for state in matches) or "none"
        raise SemanticFailure(f"unknown or ambiguous semantic screen state: {names}")
    return matches[0]


def catalog_capabilities_ready(
    state: ScreenState,
    affordances: tuple[str, ...],
    targets: tuple[str, ...],
    canonical_screen: str,
) -> bool:
    return (
        state == ScreenState.CATALOG
        and {"catalog", "deployments", "pods"}.issubset(targets)
        and {"commands", "open", "resource_finder"}.issubset(affordances)
        and "2 Running" in canonical_screen
        and "1 Running" in canonical_screen
        and re.search(r"(?m)^\s*CRDs(?:\s|$)", canonical_screen) is not None
    )


def observe(screen: str, context: str) -> Observation:
    normalized = normalize_terminal(screen)
    encoded = normalized.encode("utf-8")
    if len(encoded) > MAX_DIAGNOSTIC_SCREEN_BYTES:
        raise SemanticFailure("normalized capture exceeded 2 MiB bound")
    state = _classify(normalized)
    affordances = tuple(sorted(
        name for name, markers in _AFFORDANCE_MARKERS.items() if any(marker in normalized for marker in markers)
    ))
    targets = tuple(sorted(target for target in SAFE_TARGETS if re.search(r"(?<![\w-])" + re.escape(target) + r"(?![\w-])", normalized, re.IGNORECASE)))
    canonical = canonicalize_screen(normalized, context)
    project_crd_count = catalog_capabilities_ready(state, affordances, targets, canonical)
    replay_rows = canonical_replay_rows(canonical, state, project_crd_count)
    identity = json.dumps(
        {"state": state.value, "affordances": affordances, "targets": targets, "rows": replay_rows},
        sort_keys=True,
        separators=(",", ":"),
    )
    fingerprint = hashlib.sha256(identity.encode("utf-8")).hexdigest()
    raw_hash = hashlib.sha256(encoded).hexdigest()
    return Observation(state, fingerprint, raw_hash, affordances, targets, ENVIRONMENT_CAPABILITIES, canonical)


def validate_action_id(action_id: str) -> str:
    lowered = action_id.lower()
    if any(term in lowered for term in DESTRUCTIVE_TERMS):
        raise SemanticFailure(f"unsafe semantic action: {action_id}")
    if action_id in SAFE_BASE_ACTIONS:
        return action_id
    for prefix in SAFE_PREFIXES:
        if action_id.startswith(prefix) and action_id[len(prefix):] in SAFE_TARGETS:
            return action_id
    raise SemanticFailure(f"unknown semantic action: {action_id}")


def exact_detail_filter_target(observation: Observation) -> str | None:
    match = re.search(r"(?m)^\s*/=(alpha|scalable|log-marker)\s*$", observation.canonical_screen)
    if match is None:
        return None
    target = match.group(1)
    detail_targets = {"alpha", "scalable", "log-marker"}
    if set(observation.targets) & detail_targets != {target}:
        return None
    return target


def connect_catalog_ready(observation: Observation) -> bool:
    return catalog_capabilities_ready(
        observation.state,
        observation.affordances,
        observation.targets,
        observation.canonical_screen,
    )


def candidates(observation: Observation, goals: frozenset[str]) -> tuple[Action, ...]:
    state = observation.state
    visible = set(observation.targets)
    affordances = set(observation.affordances)
    capabilities = set(observation.capabilities)
    result: list[Action] = []
    if state == ScreenState.CONTEXT_PICKER:
        if "connect" in affordances:
            result.append(Action("connect", 20))
        if "select" in affordances and "connect" not in affordances:
            result.append(Action("select_context", 20))
        if WALK_GOALS.issubset(goals) and "minimum_actions" in goals and "quit" in affordances:
            result = [Action("quit", 100)]
    elif state in {ScreenState.CATALOG, ScreenState.GROUP_RESOURCES}:
        if "resized" not in goals and "resize" in capabilities:
            result.extend((Action("resize_small", 8), Action("resize_large", 8)))
        elif "back" not in affordances and "resize" in capabilities:
            return (Action("resize_large", 30),)
        if "commands" in affordances and ({"log_marker", "widget_watch"} - goals):
            result.append(Action("open_commands", 12))
        if "resource_finder" in affordances and "finder" not in goals:
            result.append(Action("open_resource_finder", 10))
        if WALK_GOALS.issubset(goals) and "minimum_actions" in goals and "back" in affordances:
            result = [Action("back", 100)]
    elif state == ScreenState.COMMANDS:
        if "pods" in visible and "log_marker" not in goals:
            result.append(Action("run_command:pods", 20))
        if "widgets" in visible and "widget_watch" not in goals:
            result.append(Action("run_command:widgets", 20))
        if not result and "back" in affordances:
            result.append(Action("back", 1))
    elif state == ScreenState.RESOURCE_FINDER:
        visible_resources = visible & {"pods", "widgets", "deployments"}
        if visible_resources == {"widgets"} and "widget_watch" not in goals:
            result.append(Action("open_resource:widgets", 20))
        elif visible_resources == {"pods"} and "log_marker" not in goals:
            result.append(Action("open_resource:pods", 20))
        elif "widget_watch" not in goals and "filter" in affordances:
            result.append(Action("filter_resource:widgets", 15))
        elif "log_marker" not in goals and "filter" in affordances:
            result.append(Action("filter_resource:pods", 15))
        if not result and "back" in affordances:
            result.append(Action("back", 1))
    elif state == ScreenState.PODS_LIST:
        if exact_detail_filter_target(observation) is not None:
            if "back" in affordances:
                result.append(Action("clear_filter", 30))
        else:
            if "log-marker" in visible and "open" in affordances and ({"visible_detail", "log_marker"} - goals):
                result.append(Action("open_detail:log-marker", 20))
            if "visible_detail" in goals and "back" in affordances:
                result.append(Action("back", 5))
            if not result and "back" not in affordances and "resize" in capabilities:
                result.append(Action("resize_large", 30))
    elif state == ScreenState.RESOURCE_LIST:
        if exact_detail_filter_target(observation) is not None:
            if "back" in affordances:
                result.append(Action("clear_filter", 30))
        else:
            if "alpha" in visible and "widget_watch" not in goals:
                result.append(Action("widget_watch", 25))
            for target in ("alpha", "scalable"):
                if target in visible and "open" in affordances and "visible_detail" not in goals:
                    result.append(Action(f"open_detail:{target}", 10))
            if "visible_detail" in goals and "back" in affordances:
                result.append(Action("back", 8))
            if not result and "back" not in affordances and "resize" in capabilities:
                result.append(Action("resize_large", 30))
    elif state == ScreenState.POD_DETAIL:
        if "logs" in affordances and "log_marker" not in goals:
            result.append(Action("open_logs", 30))
        elif "back" in affordances:
            result.append(Action("back", 10))
    elif state == ScreenState.RESOURCE_DETAIL:
        if "back" in affordances:
            result.append(Action("back", 10))
    elif state == ScreenState.LOGS:
        if "back" in affordances:
            result.append(Action("back", 10))
    elif state == ScreenState.CONFIRMATION and "cancel" in affordances:
        result.append(Action("cancel_confirmation", 20))
    elif state == ScreenState.INPUT_PROMPT and "cancel" in affordances:
        result.append(Action("cancel_input", 20))
    safe = sorted({action.id: action for action in result}.values(), key=lambda action: action.id)
    for action in safe:
        validate_action_id(action.id)
    return tuple(safe)


def choose_action(actions: Iterable[Action], prng: random.Random) -> Action:
    ordered = sorted(actions, key=lambda action: action.id)
    if not ordered:
        raise SemanticFailure("no safe candidate actions are visible")
    total = sum(action.weight for action in ordered)
    if total <= 0:
        raise SemanticFailure("candidate action weights must be positive")
    choice = prng.randrange(total)
    for action in ordered:
        if choice < action.weight:
            return action
        choice -= action.weight
    raise AssertionError("weighted action selection exceeded total")


def _record_from_object(value: object, line_number: int) -> TraceRecord:
    if not isinstance(value, dict):
        raise SemanticFailure(f"trace record {line_number}: expected object")
    expected = {field.name for field in TraceRecord.__dataclass_fields__.values()}
    if set(value) != expected:
        raise SemanticFailure(f"trace record {line_number}: unexpected schema fields")
    for key in ("affordances", "targets", "capabilities", "candidates", "new_goals"):
        if not isinstance(value[key], list) or not all(isinstance(item, str) for item in value[key]):
            raise SemanticFailure(f"trace record {line_number}: {key} must be a string array")
        value[key] = tuple(value[key])
    try:
        record = TraceRecord(**value)
    except TypeError as error:
        raise SemanticFailure(f"trace record {line_number}: invalid fields") from error
    if record.schema != TRACE_SCHEMA:
        raise SemanticFailure(f"trace record {line_number}: unsupported schema {record.schema}")
    if record.step != line_number - 1:
        raise SemanticFailure(f"trace record {line_number}: step order divergence")
    if not isinstance(record.seed, int) or isinstance(record.seed, bool) or not 0 <= record.seed <= UINT64_MAX:
        raise SemanticFailure(f"trace record {line_number}: invalid seed")
    if record.state not in {state.value for state in ScreenState} or record.result_state not in {state.value for state in ScreenState}:
        raise SemanticFailure(f"trace record {line_number}: unknown state")
    if re.fullmatch(r"[0-9a-f]{64}", record.fingerprint) is None or re.fullmatch(r"[0-9a-f]{64}", record.result_fingerprint) is None:
        raise SemanticFailure(f"trace record {line_number}: invalid fingerprint")
    if tuple(sorted(record.affordances)) != record.affordances or tuple(sorted(record.targets)) != record.targets or tuple(sorted(record.capabilities)) != record.capabilities or tuple(sorted(record.candidates)) != record.candidates:
        raise SemanticFailure(f"trace record {line_number}: arrays must be sorted")
    validate_action_id(record.action)
    for action in record.candidates:
        validate_action_id(action)
    if record.action not in record.candidates:
        raise SemanticFailure(f"trace record {line_number}: action absent from candidates")
    return record


def read_trace(path: pathlib.Path) -> tuple[TraceRecord, ...]:
    try:
        size = path.stat().st_size
    except OSError as error:
        raise SemanticFailure(f"cannot stat replay trace: {error}") from error
    if size < 1 or size > MAX_TRACE_BYTES:
        raise SemanticFailure(f"replay trace size must be 1..{MAX_TRACE_BYTES} bytes")
    records: list[TraceRecord] = []
    try:
        with path.open("r", encoding="utf-8") as source:
            for line_number, line in enumerate(source, 1):
                if line_number > MAX_TRACE_RECORDS:
                    raise SemanticFailure(f"replay trace exceeds {MAX_TRACE_RECORDS} records")
                try:
                    value = json.loads(line)
                except json.JSONDecodeError as error:
                    raise SemanticFailure(f"trace record {line_number}: malformed JSON: {error.msg}") from error
                records.append(_record_from_object(value, line_number))
    except UnicodeError as error:
        raise SemanticFailure(f"replay trace is not UTF-8: {error}") from error
    if not records:
        raise SemanticFailure("replay trace has no records")
    seeds = {record.seed for record in records}
    if len(seeds) != 1:
        raise SemanticFailure("replay trace seed changes between records")
    return tuple(records)


def write_trace(path: pathlib.Path, records: Iterable[TraceRecord]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    encoded = b"".join((json.dumps(asdict(record), sort_keys=True, separators=(",", ":")) + "\n").encode("utf-8") for record in records)
    record_count = encoded.count(b"\n")
    if record_count < 1 or record_count > MAX_TRACE_RECORDS:
        raise SemanticFailure(f"trace records must be 1..{MAX_TRACE_RECORDS}")
    if len(encoded) > MAX_TRACE_BYTES:
        raise SemanticFailure(f"trace exceeds {MAX_TRACE_BYTES} bytes")
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_bytes(encoded)
    os.replace(temporary, path)


def validate_replay_decision(record: TraceRecord, observation: Observation, available: Iterable[Action]) -> None:
    if record.state != observation.state.value:
        raise SemanticFailure(f"replay step {record.step}: state divergence: got {observation.state.value}, want {record.state}")
    if record.fingerprint != observation.fingerprint:
        raise SemanticFailure(f"replay step {record.step}: fingerprint divergence")
    if record.affordances != observation.affordances or record.targets != observation.targets:
        raise SemanticFailure(f"replay step {record.step}: visible observation divergence")
    if record.capabilities != observation.capabilities:
        raise SemanticFailure(f"replay step {record.step}: environment capability divergence")
    current = tuple(action.id for action in sorted(available, key=lambda action: action.id))
    if record.candidates != current:
        raise SemanticFailure(f"replay step {record.step}: candidate set divergence: got {current}, want {record.candidates}")
    if record.action not in current:
        raise SemanticFailure(f"replay step {record.step}: action unavailable in current candidates")


class SemanticWalkthrough:
    def __init__(
        self,
        driver: Driver,
        seed: int,
        replay: tuple[TraceRecord, ...] = (),
        monotonic: Callable[[], float] = time.monotonic,
    ) -> None:
        if not 0 <= seed <= UINT64_MAX:
            raise ValueError("seed outside uint64")
        if len(replay) > MAX_ACTIONS:
            raise SemanticFailure(f"replay exceeds action bound {MAX_ACTIONS}")
        self.driver = driver
        self.seed = seed
        self.replay = replay
        self.monotonic = monotonic
        self.prng = random.Random(seed)
        self.progress = Progress()
        self.records: list[TraceRecord] = []
        self.deadline = 0.0
        self.api_before: dict[str, str] = {}
        self.transient_widget_name = f"semantic-beta-{seed:016x}"

    def _remaining_timeout(self, maximum: int = 20) -> int:
        remaining = self.deadline - self.monotonic()
        if remaining <= 0:
            raise SemanticFailure("semantic wall deadline exceeded before blocking operation")
        return max(1, min(maximum, math.ceil(remaining)))

    def _kubectl(self, *args: str, maximum_timeout: int = 20) -> subprocess.CompletedProcess[str]:
        return self.driver.kubectl(*args, timeout=self._remaining_timeout(maximum_timeout))

    def _tmux_live(self) -> None:
        result = self.driver.tmux("has-session", "-t", self.driver.session, check=False)
        if result.returncode != 0:
            raise SemanticFailure("tmux session is not live")

    def _save_diagnostic(self, name: str, screen: str) -> pathlib.Path:
        path = self.driver.artifacts / name
        encoded = normalize_terminal(screen).encode("utf-8")[:MAX_DIAGNOSTIC_SCREEN_BYTES]
        path.write_bytes(encoded + b"\n")
        return path

    def _capture_initial_observation(self) -> Observation:
        deadline = min(self.deadline, self.monotonic() + TRANSITION_DEADLINE_SECONDS)
        last_screen = ""
        previous: Observation | None = None
        stable_count = 0
        while self.monotonic() < deadline:
            self._tmux_live()
            last_screen = self.driver.capture()
            try:
                current = observe(last_screen, self.driver.context)
                if previous is not None and current.fingerprint == previous.fingerprint:
                    stable_count += 1
                else:
                    stable_count = 1
                previous = current
                if stable_count >= STABLE_OBSERVATION_POLLS:
                    return current
            except SemanticFailure:
                previous = None
                stable_count = 0
            time.sleep(POLL_INTERVAL_SECONDS)
        path = self._save_diagnostic("FAILED-semantic-startup.screen.txt", last_screen)
        raise SemanticFailure(f"semantic startup classification deadline; screen: {path}")

    def _execute(self, action: Action, before: Observation) -> Callable[[Observation], bool]:
        action_id = validate_action_id(action.id)
        self._tmux_live()
        if action_id == "select_context":
            self.driver.key("Space")
            return lambda observation: observation.state == ScreenState.CONTEXT_PICKER and observation.fingerprint != before.fingerprint
        elif action_id == "connect":
            self.driver.key("Enter")
            return lambda observation: (
                observation.state == ScreenState.GROUP_RESOURCES
                or connect_catalog_ready(observation)
            )
        elif action_id == "open_commands":
            self.driver.literal(":")
            return lambda observation: observation.state == ScreenState.COMMANDS
        elif action_id == "open_resource_finder":
            self.driver.literal("r")
            return lambda observation: observation.state == ScreenState.RESOURCE_FINDER
        elif action_id.startswith("run_command:"):
            target = action_id.split(":", 1)[1]
            self.driver.literal(target)
            self.driver.key("Enter")
            expected = ScreenState.PODS_LIST if target == "pods" else ScreenState.RESOURCE_LIST
            return lambda observation: observation.state == expected
        elif action_id.startswith("filter_resource:"):
            target = action_id.split(":", 1)[1]
            self.driver.literal(target)
            return lambda observation: observation.state == ScreenState.RESOURCE_FINDER and target in observation.targets
        elif action_id.startswith("open_resource:"):
            target = action_id.split(":", 1)[1]
            self.driver.key("Enter")
            expected = ScreenState.PODS_LIST if target == "pods" else ScreenState.RESOURCE_LIST
            expected_target = "log-marker" if target == "pods" else "alpha"
            return lambda observation: observation.state == expected and expected_target in observation.targets
        elif action_id.startswith("open_detail:"):
            target = action_id.split(":", 1)[1]
            if target not in before.targets:
                raise SemanticFailure(f"visible target disappeared before action: {target}")
            self.driver.literal("/")
            self.driver.literal(f"={target}")
            selected = self._poll_unique_detail_target(before, target, len(self.records))
            self.driver.key("Enter")
            self._poll_unique_detail_target(selected, target, len(self.records))
            self.driver.key("Enter")
            expected = ScreenState.POD_DETAIL if target == "log-marker" else ScreenState.RESOURCE_DETAIL
            return lambda observation: observation.state == expected and target in observation.targets
        elif action_id == "back":
            self.driver.key("Escape")
            return lambda observation: observation.state != before.state
        elif action_id == "clear_filter":
            if exact_detail_filter_target(before) is None:
                raise SemanticFailure("clear_filter requires a unique exact-filtered detail row")
            self.driver.key("Escape")
            return lambda observation: (
                observation.state == before.state
                and observation.fingerprint != before.fingerprint
                and exact_detail_filter_target(observation) is None
            )
        elif action_id in {"cancel_confirmation", "cancel_input"}:
            self.driver.key("Escape")
            return lambda observation: observation.state != before.state
        elif action_id == "open_logs":
            self.driver.literal("l")
            return lambda observation: observation.state == ScreenState.LOGS and LOG_MARKER in observation.canonical_screen
        elif action_id == "resize_small":
            self.driver.tmux("resize-window", "-t", self.driver.session, "-x", "84", "-y", "22")
        elif action_id == "resize_large":
            self.driver.tmux("resize-window", "-t", self.driver.session, "-x", "280", "-y", "50")
        elif action_id == "widget_watch":
            self._widget_watch()
            return lambda observation: observation.state == ScreenState.RESOURCE_LIST and "alpha" in observation.targets
        elif action_id == "quit":
            self.driver.literal("q")
            return lambda observation: observation.state == ScreenState.SHELL_RESTORED
        else:
            raise AssertionError(f"safe action has no executor: {action_id}")
        return lambda observation: observation.fingerprint != before.fingerprint or observation.state != before.state

    def _poll_unique_detail_target(self, before: Observation, target: str, step: int) -> Observation:
        detail_targets = {"alpha", "scalable", "log-marker"}
        if target not in detail_targets:
            raise SemanticFailure(f"unsupported detail target: {target}")
        exact_filter = f"={target}"
        return self._poll_transition(
            before,
            lambda observation: (
                observation.state == before.state
                and set(observation.targets) & detail_targets == {target}
                and exact_filter in observation.canonical_screen
            ),
            step,
        )

    def _poll_transition(self, before: Observation, predicate: Callable[[Observation], bool], step: int) -> Observation:
        deadline = min(self.deadline, self.monotonic() + TRANSITION_DEADLINE_SECONDS)
        last_screen = ""
        previous_match: Observation | None = None
        stable_count = 0
        while self.monotonic() < deadline:
            self._tmux_live()
            last_screen = self.driver.capture()
            try:
                current = observe(last_screen, self.driver.context)
            except SemanticFailure:
                time.sleep(POLL_INTERVAL_SECONDS)
                continue
            if predicate(current):
                if previous_match is not None and current.fingerprint == previous_match.fingerprint:
                    stable_count += 1
                else:
                    stable_count = 1
                previous_match = current
                if stable_count >= STABLE_OBSERVATION_POLLS:
                    return current
            else:
                previous_match = None
                stable_count = 0
            time.sleep(POLL_INTERVAL_SECONDS)
        path = self._save_diagnostic(f"FAILED-semantic-step-{step}.screen.txt", last_screen)
        raise SemanticFailure(f"semantic transition deadline at step {step}; screen: {path}")

    def _widget_uid(self) -> str:
        return self._kubectl(
            "get", "widget", self.transient_widget_name, "--ignore-not-found=true",
            "-o", "jsonpath={.metadata.uid}",
        ).stdout.strip()

    def _assert_widget_absent(self) -> None:
        uid = self._widget_uid()
        if uid:
            raise SemanticFailure(f"transient Widget ownership collision: {self.transient_widget_name} uid={uid}")

    def _delete_owned_widget(self, uid: str) -> None:
        current_uid = self._widget_uid()
        if not current_uid:
            return
        if current_uid != uid:
            raise SemanticFailure(
                f"transient Widget UID changed before cleanup: captured={uid} current={current_uid}"
            )
        delete_options = self.driver.artifacts / "semantic-widget-delete-options.json"
        delete_options.write_text(
            json.dumps({
                "apiVersion": "v1", "kind": "DeleteOptions",
                "preconditions": {"uid": uid},
                "propagationPolicy": "Background",
            }, sort_keys=True) + "\n",
            encoding="utf-8",
        )
        try:
            uri = (
                f"/apis/surfsk8s.dev/v1alpha1/namespaces/{self.driver.namespace}"
                f"/widgets/{self.transient_widget_name}"
            )
            self._kubectl("delete", "--raw", uri, "-f", str(delete_options))
        finally:
            delete_options.unlink(missing_ok=True)

    def _widget_watch(self) -> None:
        manifest = self.driver.artifacts / "semantic-widget-transient.yaml"
        manifest.write_text(
            "apiVersion: surfsk8s.dev/v1alpha1\nkind: Widget\nmetadata:\n"
            f"  name: {self.transient_widget_name}\n  namespace: {self.driver.namespace}\n"
            "spec:\n  color: green\nstatus:\n  phase: Transient\n",
            encoding="utf-8",
        )
        uid = ""
        self._assert_widget_absent()
        try:
            uid = self._kubectl("create", "-f", str(manifest), "-o", "jsonpath={.metadata.uid}").stdout.strip()
            if re.fullmatch(r"[0-9a-f-]{8,64}", uid) is None:
                raise SemanticFailure(f"created transient Widget returned invalid UID: {uid!r}")
            self._poll_screen_contains(self.transient_widget_name, True, TRANSITION_DEADLINE_SECONDS)
            self._delete_owned_widget(uid)
            self._poll_screen_contains(self.transient_widget_name, False, TRANSITION_DEADLINE_SECONDS)
            self._assert_widget_absent()
            uid = ""
        finally:
            if uid:
                self._delete_owned_widget(uid)
                self._assert_widget_absent()
            manifest.unlink(missing_ok=True)

    def _poll_screen_contains(self, value: str, present: bool, timeout_seconds: float) -> None:
        deadline = min(self.deadline, self.monotonic() + timeout_seconds)
        while self.monotonic() < deadline:
            self._tmux_live()
            found = value in self.driver.capture()
            if found == present:
                return
            time.sleep(POLL_INTERVAL_SECONDS)
        raise SemanticFailure(f"widget watch deadline waiting for {value!r} present={present}")

    def _api_snapshot(self) -> dict[str, str]:
        self._remaining_timeout()
        ready = self._kubectl("get", "--raw=/readyz").stdout.strip()
        pod = self._kubectl("get", "pod", "log-marker", "-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}").stdout
        deployment = self._kubectl("get", "deployment", "scalable", "-o", "jsonpath={.metadata.uid}{'|'}{.spec.replicas}").stdout
        widget = self._kubectl("get", "widget", "alpha", "-o", "jsonpath={.metadata.uid}{'|'}{.spec.color}{'|'}{.status.phase}").stdout
        transient = self._widget_uid()
        if ready != "ok" or pod != "True" or not deployment.endswith("|1") or not widget.endswith("|blue|Stable") or transient:
            raise SemanticFailure(f"fixture API contract failed: ready={ready!r} pod={pod!r} deployment={deployment!r} widget={widget!r} transient={transient!r}")
        return {"ready": ready, "pod": pod, "deployment": deployment, "widget": widget, "transient": transient}

    def _new_goals(self, before: Observation, action: Action, result: Observation, step: int) -> frozenset[str]:
        goals: set[str] = set()
        self.progress.states.add(result.state.value)
        if result.state not in {ScreenState.CONTEXT_PICKER, ScreenState.SHELL_RESTORED}:
            goals.add("connected")
        if len(self.progress.states - {ScreenState.CONTEXT_PICKER.value, ScreenState.SHELL_RESTORED.value}) >= 3:
            goals.add("three_states")
        if action.id.startswith(("run_command:", "open_resource:")):
            goals.add("dynamic_route")
        if result.state in {ScreenState.POD_DETAIL, ScreenState.RESOURCE_DETAIL}:
            goals.add("visible_detail")
        if action.id == "back":
            goals.add("back")
        if action.id.startswith("resize_"):
            goals.add("resized")
        if result.state == ScreenState.RESOURCE_FINDER:
            goals.add("finder")
        if result.state == ScreenState.LOGS and LOG_MARKER in result.canonical_screen:
            goals.add("log_marker")
        if action.id == "widget_watch":
            goals.add("widget_watch")
        if step + 1 >= MIN_ACTIONS:
            goals.add("minimum_actions")
        return frozenset(goals - self.progress.goals)

    def run(self) -> None:
        self.deadline = self.monotonic() + WALL_DEADLINE_SECONDS
        self.api_before = self._api_snapshot()
        self.driver.start()
        observation = self._capture_initial_observation()
        replay_count = len(self.replay)
        for step in range(MAX_ACTIONS):
            if self.monotonic() >= self.deadline:
                raise SemanticFailure("semantic wall deadline exceeded")
            available = candidates(observation, frozenset(self.progress.goals))
            if not available:
                raise SemanticFailure(f"no safe candidates in state {observation.state.value}")
            if self.replay:
                if step >= replay_count:
                    raise SemanticFailure("replay ended before clean restoration")
                expected = self.replay[step]
                try:
                    validate_replay_decision(expected, observation, available)
                except SemanticFailure as error:
                    path = self._save_diagnostic(f"FAILED-replay-step-{step}.screen.txt", observation.canonical_screen)
                    raise SemanticFailure(f"{error}; screen: {path}") from error
                action = next(action for action in available if action.id == expected.action)
            else:
                action = choose_action(available, self.prng)
            predicate = self._execute(action, observation)
            result = self._poll_transition(observation, predicate, step)
            new_goals = self._new_goals(observation, action, result, step)
            self.progress.record(result.fingerprint, new_goals)
            record = TraceRecord(
                TRACE_SCHEMA, self.seed, step, observation.state.value, observation.fingerprint,
                observation.affordances, observation.targets, observation.capabilities,
                tuple(item.id for item in available), action.id, result.state.value,
                result.fingerprint, tuple(sorted(new_goals)),
            )
            if self.replay:
                expected = self.replay[step]
                if record.result_state != expected.result_state or record.result_fingerprint != expected.result_fingerprint or record.new_goals != expected.new_goals:
                    path = self._save_diagnostic(f"FAILED-replay-step-{step}.screen.txt", result.canonical_screen)
                    raise SemanticFailure(f"replay step {step}: result divergence; screen: {path}")
            self.records.append(record)
            write_trace(self.driver.artifacts / "semantic-decisions.jsonl", self.records)
            observation = result
            if result.state == ScreenState.SHELL_RESTORED:
                if self.replay and step + 1 != replay_count:
                    raise SemanticFailure("replay has records after shell restoration")
                break
        else:
            raise SemanticFailure(f"semantic action bound {MAX_ACTIONS} exhausted")
        if observation.state != ScreenState.SHELL_RESTORED:
            raise SemanticFailure("semantic walk did not restore shell")
        if not WALK_GOALS.issubset(self.progress.goals) or "minimum_actions" not in self.progress.goals:
            missing = sorted((WALK_GOALS | {"minimum_actions"}) - self.progress.goals)
            raise SemanticFailure(f"semantic goals missed: {missing}")
        api_after = self._api_snapshot()
        if api_after != self.api_before:
            raise SemanticFailure(f"baseline fixture API changed: before={self.api_before}, after={api_after}")
        self.progress.goals.add("api_unchanged")
        summary = {
            "schema": 1, "seed": self.seed, "actions": len(self.records),
            "states": sorted(self.progress.states), "goals": sorted(self.progress.goals),
            "api_unchanged": True, "shell_restored": True,
        }
        (self.driver.artifacts / "semantic-goals.json").write_text(json.dumps(summary, sort_keys=True, indent=2) + "\n", encoding="utf-8")
        self.driver.assert_isolated_preferences()
        print(f"semantic walkthrough passed: seed={self.seed} actions={len(self.records)} goals={','.join(sorted(self.progress.goals))}", flush=True)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--seed", type=parse_seed)
    parser.add_argument("--replay", type=pathlib.Path)
    parser.add_argument("--binary", required=True)
    parser.add_argument("--kubeconfig", required=True)
    parser.add_argument("--xdg-config", required=True)
    parser.add_argument("--context", required=True)
    parser.add_argument("--namespace", required=True)
    parser.add_argument("--cluster", required=True)
    parser.add_argument("--session", required=True)
    parser.add_argument("--tmux-socket", required=True)
    parser.add_argument("--artifacts", required=True)
    parser.add_argument("--capture-helper", required=True)
    parser.add_argument("--raw-capture-max-bytes", required=True, type=int)
    parser.add_argument("--process-state-dir", required=True)
    args = parser.parse_args()
    if args.seed is not None and args.replay is not None:
        parser.error("--seed and --replay are mutually exclusive")
    if args.seed is None and args.replay is None:
        args.seed = DEFAULT_SEED
    args.raw_capture_name = "semantic-raw-terminal.log"
    args.expected_preferences_query = None
    return args


def main() -> int:
    args = parse_args()
    driver = Driver(args)
    try:
        replay = read_trace(args.replay.resolve()) if args.replay is not None else ()
        seed = replay[0].seed if replay else args.seed
        assert seed is not None
        SemanticWalkthrough(driver, seed, replay).run()
        return 0
    except (ScenarioFailure, subprocess.SubprocessError, OSError, ValueError) as error:
        print(f"SEMANTIC E2E FAILURE: {error}", file=sys.stderr)
        return 1
    finally:
        driver.cleanup()


if __name__ == "__main__":
    raise SystemExit(main())
