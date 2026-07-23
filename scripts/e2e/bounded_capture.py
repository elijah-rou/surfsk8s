#!/usr/bin/env python3
"""Drain a byte stream while retaining at most a deterministic prefix."""

import argparse
import pathlib
import sys

MAX_ALLOWED_BYTES = 1024 * 1024 * 1024
READ_BLOCK_BYTES = 64 * 1024


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", required=True)
    parser.add_argument("--max-bytes", required=True, type=int)
    args = parser.parse_args()
    if not 1 <= args.max_bytes <= MAX_ALLOWED_BYTES:
        parser.error(f"--max-bytes must be 1..{MAX_ALLOWED_BYTES}")
    return args


def main() -> int:
    args = parse_args()
    output = pathlib.Path(args.output)
    marker = output.with_name(output.name + ".truncated")
    output.parent.mkdir(parents=True, exist_ok=True)
    existing_size = output.stat().st_size if output.exists() else 0
    truncated = existing_size > args.max_bytes

    with output.open("a+b") as destination:
        if existing_size > args.max_bytes:
            destination.truncate(args.max_bytes)
            existing_size = args.max_bytes
        remaining = args.max_bytes - existing_size
        while chunk := sys.stdin.buffer.read(READ_BLOCK_BYTES):
            retained_bytes = min(len(chunk), remaining)
            if retained_bytes > 0:
                destination.write(chunk[:retained_bytes])
                remaining -= retained_bytes
            if retained_bytes < len(chunk):
                truncated = True

    if truncated:
        marker.write_text(f"capture exceeded {args.max_bytes} bytes\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
