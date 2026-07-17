#!/usr/bin/env python3
"""Real-PTY scripted surfsk8s E2E driver backed by tmux."""

from __future__ import annotations

import argparse
import json
import os
import pathlib
import re
import subprocess
import sys
import time

ANSI_CSI = re.compile(r"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[()][0-2A-Z0-9]|[@-_])")
CLUSTER_NAME = re.compile(r"^surfsk8s-e2e-[a-z0-9](?:[a-z0-9-]{0,16}[a-z0-9])?$")
TMUX_NAME = re.compile(r"^surfsk8s-e2e-[A-Za-z0-9_-]{1,48}$")
MAX_CAPTURE_BYTES = 2 * 1024 * 1024
POLL_INTERVAL_SECONDS = 0.2


def validate_cluster_name(name: str) -> str:
    if len(name) > 32 or CLUSTER_NAME.fullmatch(name) is None:
        raise ValueError(f"unsafe cluster name: {name!r}")
    return name


def normalize_terminal(value: str) -> str:
    value = ANSI_CSI.sub("", value).replace("\r\n", "\n").replace("\r", "\n")
    while "\b" in value:
        value = re.sub(r"[^\n]\x08", "", value)
    return "\n".join(line.rstrip() for line in value.splitlines()).strip()


def context_is_selected(screen: str, context: str) -> bool:
    return re.search(r"(?m)^\s*\[x\]\s+" + re.escape(context) + r"(?:\s|$)", screen) is not None


def scale_prompt_cancelled(screen: str) -> bool:
    return "surfsk8s · resource details" in screen and "replicas>" not in screen


def bounded_capture_command(output: pathlib.Path, helper: pathlib.Path, max_bytes: int) -> str:
    if max_bytes <= 0:
        raise ValueError("raw capture maximum must be positive")
    return (
        f"{shell_quote(str(helper))} --output {shell_quote(str(output))} "
        f"--max-bytes {max_bytes}"
    )


class ScenarioFailure(RuntimeError):
    pass


class Driver:
    def __init__(self, args: argparse.Namespace) -> None:
        self.binary = pathlib.Path(args.binary).resolve()
        self.kubeconfig = pathlib.Path(args.kubeconfig).resolve()
        self.xdg_config = pathlib.Path(args.xdg_config).resolve()
        self.artifacts = pathlib.Path(args.artifacts).resolve()
        self.artifacts.mkdir(parents=True, exist_ok=True)
        self.context = args.context
        self.namespace = args.namespace
        self.cluster = validate_cluster_name(args.cluster)
        if TMUX_NAME.fullmatch(args.session) is None:
            raise ValueError(f"unsafe tmux session name: {args.session!r}")
        if TMUX_NAME.fullmatch(args.tmux_socket) is None:
            raise ValueError(f"unsafe tmux socket name: {args.tmux_socket!r}")
        self.session = args.session
        self.tmux_socket = args.tmux_socket
        self.raw_capture_max_bytes = getattr(args, "raw_capture_max_bytes", 16 * 1024 * 1024)
        if self.raw_capture_max_bytes <= 0:
            raise ValueError("raw capture maximum must be positive")
        self.capture_helper = pathlib.Path(
            getattr(args, "capture_helper", pathlib.Path(__file__).with_name("bounded_capture.py"))
        ).resolve()
        process_state_dir = getattr(args, "process_state_dir", None)
        self.process_state_dir = pathlib.Path(process_state_dir).resolve() if process_state_dir else None
        self.checkpoint_index = 0
        self.started = False

    def tmux(self, *args: str, check: bool = True) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            ["tmux", "-L", self.tmux_socket, *args], text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            timeout=10, check=check,
        )

    def kubectl(self, *args: str, timeout: int = 20) -> subprocess.CompletedProcess[str]:
        command = [
            "kubectl", "--kubeconfig", str(self.kubeconfig), "--context", self.context,
            "--namespace", self.namespace, *args,
        ]
        result = subprocess.run(command, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
        with (self.artifacts / "kubectl-assertions.log").open("a", encoding="utf-8") as output:
            output.write("$ " + " ".join(command[:1] + command[5:]) + "\n")
            output.write(result.stdout + "\n")
        if result.returncode != 0:
            raise ScenarioFailure(f"kubectl assertion failed ({result.returncode}): {' '.join(args)}\n{result.stdout}")
        return result

    def capture(self) -> str:
        result = self.tmux("capture-pane", "-p", "-J", "-t", self.session, "-S", "-")
        text = normalize_terminal(result.stdout)
        if len(text.encode()) > MAX_CAPTURE_BYTES:
            raise ScenarioFailure("tmux capture exceeded 2 MiB bound")
        return text

    def checkpoint(self, name: str, required: tuple[str, ...], timeout: float = 30.0) -> str:
        deadline = time.monotonic() + timeout
        last = ""
        while time.monotonic() < deadline:
            try:
                last = self.capture()
            except subprocess.CalledProcessError as error:
                last = error.stderr
            if all(fragment in last for fragment in required):
                self.checkpoint_index += 1
                path = self.artifacts / f"{self.checkpoint_index:02d}-{name}.screen.txt"
                path.write_text(last + "\n", encoding="utf-8")
                print(f"checkpoint {name}: {', '.join(required)}", flush=True)
                return last
            time.sleep(POLL_INTERVAL_SECONDS)
        failure = self.artifacts / f"FAILED-{name}.screen.txt"
        failure.write_text(last + "\n", encoding="utf-8")
        raise ScenarioFailure(f"scenario {name!r} timed out waiting for {required}; screen: {failure}")

    def wait_absent(self, name: str, forbidden: str, required: str, timeout: float = 20.0) -> None:
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            screen = self.capture()
            if required in screen and forbidden not in screen:
                self.checkpoint_index += 1
                (self.artifacts / f"{self.checkpoint_index:02d}-{name}.screen.txt").write_text(screen + "\n", encoding="utf-8")
                print(f"checkpoint {name}: absent {forbidden}", flush=True)
                return
            time.sleep(POLL_INTERVAL_SECONDS)
        raise ScenarioFailure(f"scenario {name!r} timed out waiting for {forbidden!r} to disappear")

    def wait_scale_prompt_cancelled(self, timeout: float = 20.0) -> None:
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            screen = self.capture()
            if scale_prompt_cancelled(screen):
                self.checkpoint_index += 1
                path = self.artifacts / f"{self.checkpoint_index:02d}-scale-cancelled.screen.txt"
                path.write_text(screen + "\n", encoding="utf-8")
                print("checkpoint scale-cancelled: replicas prompt absent", flush=True)
                return
            time.sleep(POLL_INTERVAL_SECONDS)
        raise ScenarioFailure("scenario 'scale-cancelled' timed out waiting for replicas prompt to disappear")

    def literal(self, value: str) -> None:
        self.tmux("send-keys", "-t", self.session, "-l", value)

    def key(self, value: str) -> None:
        self.tmux("send-keys", "-t", self.session, value)

    def command(self, name: str, expected: tuple[str, ...], checkpoint: str) -> None:
        self.literal(":")
        self.checkpoint(checkpoint + "-command", ("surfsk8s · commands",))
        self.literal(name)
        self.key("Enter")
        self.checkpoint(checkpoint, expected)

    def exact_filter(self, query: str, expected: str, checkpoint: str) -> None:
        self.literal("/")
        self.literal("=" + query)
        self.key("Enter")
        self.checkpoint(checkpoint, (expected, query))

    def build_launch_script(self) -> str:
        return (
            "#!/usr/bin/env bash\nset -uo pipefail\n"
            f"env TERM={shell_quote('xterm-256color')} XDG_CONFIG_HOME={shell_quote(str(self.xdg_config))} "
            f"{shell_quote(str(self.binary))} -kubeconfig {shell_quote(str(self.kubeconfig))} "
            f"-context {shell_quote(self.context)} -namespace {shell_quote(self.namespace)}\n"
            "rc=$?\nprintf 'SURFSK8S_SHELL_RESTORED rc=%s\\n' \"$rc\"\n"
            "exec bash --noprofile --norc\n"
        )

    def assert_isolated_preferences(self) -> None:
        path = self.xdg_config / "surfsk8s" / "preferences.json"
        try:
            preferences = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as error:
            raise ScenarioFailure(f"isolated preferences missing or invalid: {path}: {error}") from error
        last_session = preferences.get("last_session")
        if not isinstance(last_session, dict):
            raise ScenarioFailure(f"isolated preferences have no last_session: {path}")
        if last_session.get("namespace") != self.namespace or last_session.get("query") != "=scalable":
            raise ScenarioFailure(f"unexpected isolated last_session in {path}: {last_session!r}")
        print(f"isolated preferences verified: {path}", flush=True)

    def record_owned_process_identity(self, name: str, pid: int) -> None:
        if self.process_state_dir is None:
            return
        if pid <= 0:
            raise ScenarioFailure(f"invalid owned process PID: {pid}")
        stat_fields = pathlib.Path(f"/proc/{pid}/stat").read_text(encoding="utf-8").split()
        if len(stat_fields) < 22:
            raise ScenarioFailure(f"incomplete process identity for PID {pid}")
        self.process_state_dir.mkdir(parents=True, exist_ok=True)
        (self.process_state_dir / f"{name}.identity").write_text(
            f"{pid} {stat_fields[21]}\n", encoding="utf-8"
        )

    def start(self) -> None:
        if not self.binary.is_file() or not os.access(self.binary, os.X_OK):
            raise ScenarioFailure(f"binary is not executable: {self.binary}")
        if not self.kubeconfig.is_file():
            raise ScenarioFailure(f"kubeconfig missing: {self.kubeconfig}")
        if not self.xdg_config.is_dir():
            raise ScenarioFailure(f"XDG config directory missing: {self.xdg_config}")
        self.tmux("kill-session", "-t", self.session, check=False)
        launch = self.artifacts / "launch.sh"
        launch.write_text(self.build_launch_script(), encoding="utf-8")
        launch.chmod(0o700)
        subprocess.run(
            ["tmux", "-L", self.tmux_socket, "new-session", "-d", "-s", self.session, "-x", "140", "-y", "36", str(launch)],
            check=True, timeout=10,
        )
        self.started = True
        tmux_server_pid = int(self.tmux("display-message", "-p", "-t", self.session, "#{pid}").stdout.strip())
        tmux_pane_pid = int(self.tmux("display-message", "-p", "-t", self.session, "#{pane_pid}").stdout.strip())
        self.record_owned_process_identity("tmux-server", tmux_server_pid)
        self.record_owned_process_identity("tmux-pane", tmux_pane_pid)
        raw = self.artifacts / "raw-terminal.log"
        capture_command = bounded_capture_command(raw, self.capture_helper, self.raw_capture_max_bytes)
        self.tmux("pipe-pane", "-t", self.session, "-o", capture_command)

    def run(self) -> None:
        self.start()
        startup = self.checkpoint("startup-context-picker", ("surfsk8s · select contexts", self.context))
        if not context_is_selected(startup, self.context):
            self.key("Space")
        self.checkpoint("startup-context-selected", ("[x]", self.context))
        self.key("Enter")
        self.checkpoint("connected-catalog", ("surfsk8s · resource catalog",))

        self.tmux("resize-window", "-t", self.session, "-x", "84", "-y", "22")
        self.checkpoint("resize-small", ("GROUPS", "Favourites", "resource-find"))
        self.tmux("resize-window", "-t", self.session, "-x", "160", "-y", "44")
        self.checkpoint("resize-large", ("surfsk8s · resource catalog",))

        self.command("pods", ("surfsk8s", "pods"), "pods-list")
        self.exact_filter("log-marker", "log-marker", "pod-filter")
        self.key("Enter")
        self.checkpoint("pod-detail", ("surfsk8s · pod details", "log-marker"))
        self.literal("l")
        self.checkpoint("pod-logs", ("surfsk8s · pod logs", "SURFSK8S_E2E_LOG_MARKER_7d9f3a"), 40)
        self.key("Escape")
        self.checkpoint("pod-log-back", ("surfsk8s · pod details",))

        self.command("catalog", ("surfsk8s · resource catalog",), "catalog-return")
        self.literal("r")
        self.checkpoint("resource-finder", ("surfsk8s · resource finder",))
        self.literal("widgets")
        self.key("Enter")
        self.checkpoint("crd-list", ("widgets · surfsk8s.dev", "COLOR", "alpha", "blue"), 40)
        self.key("Enter")
        self.checkpoint("crd-detail", ("surfsk8s · resource details", "alpha", "blue"))
        self.key("Escape")
        self.checkpoint("crd-back", ("widgets · surfsk8s.dev", "alpha"))

        churn_file = self.artifacts / "churn.yaml"
        churn_file.write_text(
            "apiVersion: surfsk8s.dev/v1alpha1\nkind: Widget\nmetadata:\n  name: beta\n"
            f"  namespace: {self.namespace}\nspec:\n  color: green\nstatus:\n  phase: Churned\n",
            encoding="utf-8",
        )
        self.kubectl("apply", "-f", str(churn_file))
        self.checkpoint("live-watch-add", ("widgets · surfsk8s.dev", "beta", "green", "Churned"), 30)
        self.kubectl("delete", "-f", str(churn_file), "--wait=true")
        self.wait_absent("live-watch-delete", "beta", "widgets · surfsk8s.dev", 30)

        self.command("deployments", ("surfsk8s", "deployments"), "deployments-list")
        self.exact_filter("scalable", "scalable", "deployment-filter")
        self.key("Enter")
        self.checkpoint("deployment-detail", ("surfsk8s · resource details", "scalable", "s scale"))
        self.literal("s")
        self.checkpoint("scale-prompt-cancel", ("replicas>",))
        self.key("Escape")
        self.wait_scale_prompt_cancelled()
        self.literal("s")
        self.checkpoint("scale-prompt", ("replicas>",))
        self.key("C-u")
        self.literal("2")
        self.key("Enter")
        self.checkpoint("scale-confirm", ("surfsk8s · confirm action", "scale deployment", "2"))
        self.key("Enter")

        deadline = time.monotonic() + 45
        replicas = ""
        while time.monotonic() < deadline:
            replicas = self.kubectl("get", "deployment", "scalable", "-o", "jsonpath={.spec.replicas}").stdout
            if replicas == "2":
                break
            time.sleep(POLL_INTERVAL_SECONDS)
        if replicas != "2":
            raise ScenarioFailure(f"deployment scale assertion got replicas={replicas!r}, want '2'")
        self.kubectl("rollout", "status", "deployment/scalable", "--timeout=60s", timeout=70)
        self.checkpoint("scale-observed", ("surfsk8s · resource details", "scalable"), 30)

        self.literal("q")
        self.checkpoint("quit-restoration", ("SURFSK8S_SHELL_RESTORED rc=0",), 20)
        self.assert_isolated_preferences()
        print("all scripted live PTY scenarios passed", flush=True)

    def cleanup(self) -> None:
        if self.started:
            try:
                screen = self.capture()
                (self.artifacts / "final.screen.txt").write_text(screen + "\n", encoding="utf-8")
            except Exception:
                pass
            self.tmux("kill-session", "-t", self.session, check=False)


def shell_quote(value: str) -> str:
    return "'" + value.replace("'", "'\\''") + "'"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
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
    return parser.parse_args()


def main() -> int:
    driver = Driver(parse_args())
    try:
        driver.run()
        return 0
    except (ScenarioFailure, subprocess.SubprocessError, OSError) as error:
        print(f"E2E FAILURE: {error}", file=sys.stderr)
        return 1
    finally:
        driver.cleanup()


if __name__ == "__main__":
    raise SystemExit(main())
