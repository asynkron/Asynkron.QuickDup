#!/usr/bin/env python3
"""Run the repository's Go tests and emit Faktorial quality evidence."""

from __future__ import annotations

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
from collections import defaultdict
from typing import Any, Sequence


ENVELOPE_SCHEMA = "quality-evidence.v1"
PAYLOAD_SCHEMA = "test-results.v1"
PRODUCER_ID = "backend-tests"
ARTIFACT_NAME = f"{PRODUCER_ID}.quality-evidence.json"
NATIVE_RESULT_NAME = f"{PRODUCER_ID}.go-test-json"
TERMINAL_ACTIONS = frozenset({"pass", "fail", "skip"})
SUPPORTED_ACTIONS = frozenset(
    {"start", "run", "pause", "cont", "pass", "bench", "fail", "output", "skip"}
)
MAX_REASON = 1_000
MAX_FAILURE_MESSAGE = 2_000
STALE_TOLERANCE_NS = 2_000_000_000


class TranslationError(ValueError):
    """Raised when native Go output cannot be translated exactly."""


def _bounded(value: str, limit: int) -> str:
    value = value.strip()
    return value if len(value) <= limit else value[:limit]


def _atomic_write(path: Path, data: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary_name = ""
    try:
        with tempfile.NamedTemporaryFile(
            dir=path.parent,
            prefix=f".{path.name}.",
            suffix=".tmp",
            delete=False,
        ) as temporary:
            temporary_name = temporary.name
            os.chmod(temporary_name, 0o600)
            temporary.write(data)
            temporary.flush()
            os.fsync(temporary.fileno())
        os.replace(temporary_name, path)
    finally:
        if temporary_name:
            try:
                os.unlink(temporary_name)
            except FileNotFoundError:
                pass


def _failure_message(lines: list[str]) -> str:
    useful = []
    for line in lines:
        stripped = line.strip()
        if not stripped or stripped.startswith(
            ("=== RUN", "--- FAIL", "--- PASS", "--- SKIP")
        ):
            continue
        useful.append(stripped)
    return _bounded("\n".join(useful), MAX_FAILURE_MESSAGE)


def translate_go_test_json(raw: str, return_code: int) -> dict[str, Any]:
    """Translate a complete ``go test -json`` stream into test-results.v1."""

    if not raw.strip():
        raise TranslationError("go test produced no structured events")
    if return_code < 0:
        raise TranslationError(f"go test was terminated by signal {-return_code}")

    test_started: set[tuple[str, str]] = set()
    test_terminal: dict[tuple[str, str], str] = {}
    test_output: defaultdict[tuple[str, str], list[str]] = defaultdict(list)
    packages_seen: set[str] = set()
    package_terminal: dict[str, str] = {}
    package_output: defaultdict[str, list[str]] = defaultdict(list)

    for line_number, line in enumerate(raw.splitlines(), start=1):
        if not line.strip():
            continue
        try:
            event = json.loads(line)
        except json.JSONDecodeError as error:
            raise TranslationError(
                f"invalid go test JSON on line {line_number}: {error.msg}"
            ) from error
        if not isinstance(event, dict):
            raise TranslationError(f"go test event on line {line_number} is not an object")

        action = event.get("Action")
        package = event.get("Package")
        if not isinstance(action, str) or action not in SUPPORTED_ACTIONS:
            raise TranslationError(
                f"unsupported go test action on line {line_number}: {action!r}"
            )
        if not isinstance(package, str) or not package.strip():
            raise TranslationError(f"go test event on line {line_number} omitted Package")
        packages_seen.add(package)

        test = event.get("Test")
        if test is not None and (not isinstance(test, str) or not test.strip()):
            raise TranslationError(
                f"go test event on line {line_number} has an invalid Test identity"
            )
        test = test or ""

        if action == "output":
            output = event.get("Output")
            if not isinstance(output, str):
                raise TranslationError(f"go test output event on line {line_number} omitted Output")
            if test:
                test_output[(package, test)].append(output)
            else:
                package_output[package].append(output)
            continue

        if test:
            key = (package, test)
            if action == "run":
                test_started.add(key)
            elif action in TERMINAL_ACTIONS:
                if key in test_terminal:
                    raise TranslationError(
                        f"test {package}/{test} emitted more than one terminal result"
                    )
                test_terminal[key] = action
            continue

        if action in TERMINAL_ACTIONS:
            if package in package_terminal:
                raise TranslationError(f"package {package} emitted more than one terminal result")
            package_terminal[package] = action

    unterminated_tests = sorted(test_started.difference(test_terminal))
    if unterminated_tests:
        package, test = unterminated_tests[0]
        raise TranslationError(f"test {package}/{test} did not emit a terminal result")
    unterminated_packages = sorted(packages_seen.difference(package_terminal))
    if unterminated_packages:
        raise TranslationError(f"package {unterminated_packages[0]} did not emit a terminal result")
    if not package_terminal:
        raise TranslationError("go test produced no terminal package results")

    failures: list[dict[str, str]] = []
    passed = 0
    skipped = 0
    failed_packages: set[str] = set()
    for (package, test), action in sorted(test_terminal.items()):
        if action == "pass":
            passed += 1
        elif action == "skip":
            skipped += 1
        else:
            failed_packages.add(package)
            failure = {"suite": package, "test": test}
            message = _failure_message(test_output[(package, test)])
            if message:
                failure["message"] = message
            failures.append(failure)

    for package, action in sorted(package_terminal.items()):
        if action != "fail" or package in failed_packages:
            continue
        failure = {"suite": package, "test": "build"}
        message = _failure_message(package_output[package])
        if message:
            failure["message"] = message
        failures.append(failure)

    failed = len(failures)
    total = passed + failed + skipped
    if return_code == 0 and failed:
        raise TranslationError("go test exited successfully but emitted failed terminal results")
    if return_code != 0 and failed == 0:
        raise TranslationError("go test failed without a translatable failed test or package result")

    return {
        "counts": {"total": total, "passed": passed, "failed": failed, "skipped": skipped},
        "failures": failures,
        "omitted_failures": 0,
    }


def make_envelope(
    status: str,
    *,
    payload: dict[str, Any] | None = None,
    reason: str = "",
) -> dict[str, Any]:
    envelope: dict[str, Any] = {
        "schema_version": ENVELOPE_SCHEMA,
        "producer_id": PRODUCER_ID,
        "kind": "test-results",
        "payload_schema": PAYLOAD_SCHEMA,
        "status": status,
    }
    if status == "complete":
        envelope["payload"] = payload
    else:
        envelope["reason"] = _bounded(reason, MAX_REASON)
    return envelope


def validate_envelope(envelope: Any) -> None:
    if not isinstance(envelope, dict):
        raise TranslationError("quality evidence envelope is not an object")
    expected = {
        "schema_version": ENVELOPE_SCHEMA,
        "producer_id": PRODUCER_ID,
        "kind": "test-results",
        "payload_schema": PAYLOAD_SCHEMA,
    }
    for field, value in expected.items():
        if envelope.get(field) != value:
            raise TranslationError(f"quality evidence {field} is {envelope.get(field)!r}, want {value!r}")

    status = envelope.get("status")
    if status not in {"complete", "not_applicable", "translation_failed", "incomplete"}:
        raise TranslationError(f"quality evidence has unsupported status {status!r}")
    if status != "complete":
        if not isinstance(envelope.get("reason"), str) or not envelope["reason"].strip():
            raise TranslationError(f"{status} quality evidence omitted its reason")
        return

    payload = envelope.get("payload")
    if not isinstance(payload, dict):
        raise TranslationError("complete quality evidence omitted its payload")
    counts = payload.get("counts")
    failures = payload.get("failures")
    omitted = payload.get("omitted_failures", 0)
    if not isinstance(counts, dict) or not isinstance(failures, list):
        raise TranslationError("complete test results omitted counts or failures")
    values = [counts.get(name) for name in ("total", "passed", "failed", "skipped")]
    if any(not isinstance(value, int) or isinstance(value, bool) or value < 0 for value in values):
        raise TranslationError("test result counts must be non-negative integers")
    total, passed, failed, skipped = values
    if total != passed + failed + skipped:
        raise TranslationError("total test count does not equal terminal counts")
    if not isinstance(omitted, int) or isinstance(omitted, bool) or omitted < 0:
        raise TranslationError("omitted_failures must be a non-negative integer")
    if failed != len(failures) + omitted:
        raise TranslationError("failed count does not equal reported plus omitted failures")
    for failure in failures:
        if (
            not isinstance(failure, dict)
            or not isinstance(failure.get("test"), str)
            or not failure["test"].strip()
        ):
            raise TranslationError("failed test identity is empty")


def write_envelope(evidence_dir: Path, envelope: dict[str, Any]) -> Path:
    validate_envelope(envelope)
    target = evidence_dir / ARTIFACT_NAME
    encoded = (json.dumps(envelope, indent=2, sort_keys=True) + "\n").encode("utf-8")
    _atomic_write(target, encoded)
    return target


def validate_evidence_directory(evidence_dir: Path, *, not_before_ns: int | None = None) -> None:
    artifacts = sorted(evidence_dir.glob("*.quality-evidence.json"))
    expected = evidence_dir / ARTIFACT_NAME
    if artifacts != [expected]:
        names = ", ".join(path.name for path in artifacts) or "none"
        raise TranslationError(f"expected exactly {ARTIFACT_NAME}; observed {names}")
    if not_before_ns is not None and expected.stat().st_mtime_ns + STALE_TOLERANCE_NS < not_before_ns:
        raise TranslationError(f"quality evidence artifact {ARTIFACT_NAME} is stale")
    try:
        envelope = json.loads(expected.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise TranslationError(f"quality evidence artifact is invalid: {error}") from error
    validate_envelope(envelope)


def run_adapter(evidence_dir: Path, command: Sequence[str] | None = None) -> int:
    started_ns = time.time_ns()
    command = list(command or ("go", "test", "-json", "-count=1", "./..."))
    try:
        completed = subprocess.run(command, check=False, capture_output=True, text=True)
        raw = completed.stdout
        stderr = completed.stderr
        return_code = completed.returncode
    except OSError as error:
        raw = ""
        stderr = ""
        return_code = 2
        envelope = make_envelope(
            "translation_failed", reason=f"could not execute go test: {error}"
        )
    else:
        try:
            payload = translate_go_test_json(raw, return_code)
            envelope = make_envelope("complete", payload=payload)
        except TranslationError as error:
            detail = str(error)
            if stderr.strip():
                detail = f"{detail}: {_bounded(stderr, MAX_REASON // 2)}"
            envelope = make_envelope("translation_failed", reason=detail)
            if return_code == 0:
                return_code = 2

    evidence_dir.mkdir(parents=True, exist_ok=True)
    _atomic_write(evidence_dir / NATIVE_RESULT_NAME, raw.encode("utf-8"))
    write_envelope(evidence_dir, envelope)
    try:
        validate_evidence_directory(evidence_dir, not_before_ns=started_ns)
    except TranslationError as error:
        print(f"quality-evidence: conformance failure: {error}", file=sys.stderr)
        return_code = return_code or 2

    if raw:
        print(raw, end="")
    if stderr:
        print(stderr, end="", file=sys.stderr)
    return return_code


def main() -> int:
    configured_dir = os.environ.get("FAKTORIAL_QUALITY_EVIDENCE_DIR", "").strip()
    if not configured_dir:
        print("quality-evidence: FAKTORIAL_QUALITY_EVIDENCE_DIR is required", file=sys.stderr)
        return 2
    try:
        return run_adapter(Path(configured_dir))
    except (OSError, TranslationError) as error:
        print(f"quality-evidence: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
