from __future__ import annotations

import contextlib
import io
import json
import os
from pathlib import Path
import sys
import tempfile
import time
import unittest

import quality_evidence


def event(action: str, package: str, *, test: str = "", output: str = "") -> str:
    value = {"Action": action, "Package": package}
    if test:
        value["Test"] = test
    if action == "output":
        value["Output"] = output
    return json.dumps(value)


def mixed_result_fixture() -> str:
    package = "example/pkg"
    broken = "example/broken"
    return "\n".join(
        [
            event("start", package),
            event("run", package, test="TestPass"),
            event("pass", package, test="TestPass"),
            event("run", package, test="TestSkip"),
            event("skip", package, test="TestSkip"),
            event("run", package, test="TestFail"),
            event(
                "output",
                package,
                test="TestFail",
                output="fixture_test.go:42: want 1, got 2\n",
            ),
            event("fail", package, test="TestFail"),
            event("fail", package),
            event("start", broken),
            event("output", broken, output="./broken.go:3: syntax error\n"),
            event("fail", broken),
        ]
    ) + "\n"


class QualityEvidenceTests(unittest.TestCase):
    def test_config_declares_every_adapter_producer(self) -> None:
        root = Path(__file__).resolve().parents[1]
        config = json.loads(
            (root / ".faktorial" / "main-verify.json").read_text(encoding="utf-8")
        )

        self.assertEqual("main-verify.v2", config["schema_version"])
        self.assertEqual(["python3 ./scripts/quality_evidence.py"], config["commands"])
        self.assertEqual(
            [
                {
                    "command": "python3 ./scripts/quality_evidence.py",
                    "producer_id": quality_evidence.PRODUCER_ID,
                    "kind": "test-results",
                    "payload_schema": quality_evidence.PAYLOAD_SCHEMA,
                    "required": True,
                }
            ],
            config["evidence"],
        )

    def test_translation_reports_exact_terminal_counts_and_failures(self) -> None:
        payload = quality_evidence.translate_go_test_json(mixed_result_fixture(), 1)

        self.assertEqual(
            {"total": 4, "passed": 1, "failed": 2, "skipped": 1},
            payload["counts"],
        )
        self.assertEqual(
            ["TestFail", "build"],
            [failure["test"] for failure in payload["failures"]],
        )
        self.assertIn("want 1, got 2", payload["failures"][0]["message"])
        self.assertIn("syntax error", payload["failures"][1]["message"])

    def test_failed_command_still_emits_one_complete_envelope(self) -> None:
        fixture = mixed_result_fixture()
        command = [
            sys.executable,
            "-c",
            f"import sys; print({fixture!r}, end=''); raise SystemExit(1)",
        ]
        with tempfile.TemporaryDirectory() as directory:
            with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
                return_code = quality_evidence.run_adapter(Path(directory), command)

            self.assertEqual(1, return_code)
            quality_evidence.validate_evidence_directory(Path(directory))
            envelope = json.loads((Path(directory) / quality_evidence.ARTIFACT_NAME).read_text())
            self.assertEqual("complete", envelope["status"])
            self.assertEqual(2, envelope["payload"]["counts"]["failed"])
            self.assertTrue((Path(directory) / quality_evidence.NATIVE_RESULT_NAME).is_file())

    def test_process_start_failure_still_emits_translation_failed(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
                return_code = quality_evidence.run_adapter(
                    Path(directory), [str(Path(directory) / "missing-go")]
                )

            self.assertEqual(2, return_code)
            quality_evidence.validate_evidence_directory(Path(directory))
            envelope = json.loads(
                (Path(directory) / quality_evidence.ARTIFACT_NAME).read_text()
            )
            self.assertEqual("translation_failed", envelope["status"])
            self.assertIn("could not execute go test", envelope["reason"])

    def test_untranslatable_output_reports_translation_failed(self) -> None:
        command = [
            sys.executable,
            "-c",
            "import sys; print('not-json'); raise SystemExit(1)",
        ]
        with tempfile.TemporaryDirectory() as directory:
            with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
                return_code = quality_evidence.run_adapter(Path(directory), command)

            self.assertEqual(1, return_code)
            envelope = json.loads((Path(directory) / quality_evidence.ARTIFACT_NAME).read_text())
            self.assertEqual("translation_failed", envelope["status"])
            self.assertIn("invalid go test JSON", envelope["reason"])

    def test_not_applicable_requires_an_explicit_reason(self) -> None:
        envelope = quality_evidence.make_envelope(
            "not_applicable", reason="test lane excluded by scope"
        )
        quality_evidence.validate_envelope(envelope)
        with self.assertRaisesRegex(quality_evidence.TranslationError, "omitted its reason"):
            quality_evidence.validate_envelope(quality_evidence.make_envelope("not_applicable"))

    def test_conformance_rejects_invalid_evidence_sets(self) -> None:
        complete = quality_evidence.make_envelope(
            "complete",
            payload={
                "counts": {"total": 1, "passed": 1, "failed": 0, "skipped": 0},
                "failures": [],
                "omitted_failures": 0,
            },
        )
        cases = ["missing", "duplicate", "stale", "invalid", "incomplete"]
        for case in cases:
            with self.subTest(case=case), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                if case != "missing":
                    quality_evidence.write_envelope(root, complete)
                if case == "duplicate":
                    (root / "duplicate.quality-evidence.json").write_text(json.dumps(complete))
                elif case == "stale":
                    old = time.time() - 60
                    os.utime(root / quality_evidence.ARTIFACT_NAME, (old, old))
                elif case == "invalid":
                    (root / quality_evidence.ARTIFACT_NAME).write_text("not-json")
                elif case == "incomplete":
                    broken = dict(complete)
                    broken["payload"] = {
                        "counts": {"total": 2, "passed": 1, "failed": 0, "skipped": 0},
                        "failures": [],
                    }
                    (root / quality_evidence.ARTIFACT_NAME).write_text(json.dumps(broken))

                not_before = time.time_ns() if case == "stale" else None
                with self.assertRaises(quality_evidence.TranslationError):
                    quality_evidence.validate_evidence_directory(root, not_before_ns=not_before)


if __name__ == "__main__":
    unittest.main()
