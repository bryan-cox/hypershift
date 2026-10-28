#!/usr/bin/env python3
"""Check that teardown observation cannot mistake missing evidence for cleanup."""

import importlib.util
import unittest
from pathlib import Path
from unittest import mock

spec = importlib.util.spec_from_file_location(
    "teardown", Path(__file__).with_name("cluster-teardown-timeline.py")
)
teardown = importlib.util.module_from_spec(spec)
spec.loader.exec_module(teardown)


class TeardownTimelineTest(unittest.TestCase):
    def snapshot(self):
        return {
            "kubernetes": {"hostedcluster": {"ok": True, "data": []}},
            "aws": {"vpc": {"ok": True, "data": []}},
        }

    def test_command_success_alone_does_not_prove_cleanup(self):
        data = self.snapshot()
        data["aws"]["vpc"]["data"] = ["vpc-still-present"]
        self.assertFalse(teardown.evaluate(data, {"exitCode": 0})["fully_complete"])

    def test_errors_and_unknown_results_cannot_be_empty(self):
        for result in [
            {"ok": False, "data": []},
            {"ok": True, "data": None},
            {"data": []},
        ]:
            with self.subTest(result=result):
                self.assertFalse(teardown.is_empty(result))

    def test_all_checks_and_successful_command_are_required(self):
        self.assertTrue(
            teardown.evaluate(self.snapshot(), {"exitCode": 0})["fully_complete"]
        )
        self.assertFalse(teardown.evaluate(self.snapshot(), {})["fully_complete"])
        self.assertFalse(
            teardown.evaluate(self.snapshot(), {"exitCode": 1})["fully_complete"]
        )

    def test_only_explicit_not_found_errors_are_absence(self):
        result = mock.Mock(
            returncode=1, stderr="An error occurred (NoSuchEntity)", stdout=""
        )
        with mock.patch.object(teardown.subprocess, "run", return_value=result):
            self.assertEqual(
                teardown.query(["aws"], ("NoSuchEntity",)), {"ok": True, "data": []}
            )
            self.assertFalse(teardown.query(["aws"])["ok"])
            result.stderr = "An error occurred (AccessDenied)"
            self.assertFalse(teardown.query(["aws"], ("NoSuchEntity",))["ok"])

    def test_normalization_keeps_finalizers_and_excludes_secret_data(self):
        obj = {
            "kind": "Example",
            "metadata": {
                "name": "example",
                "uid": "original",
                "deletionTimestamp": "2026-10-28T21:00:00Z",
                "finalizers": ["cleanup"],
            },
            "data": {"token": "must-not-be-written"},
            "spec": {"credentials": "must-not-be-written"},
            "status": {"conditions": [{"type": "Deleting", "status": "True"}]},
        }
        normalized = teardown.normalize({"items": [obj]})[0]
        self.assertEqual(normalized["finalizers"], ["cleanup"])
        self.assertEqual(normalized["uid"], "original")
        self.assertNotIn("data", normalized)
        self.assertNotIn("spec", normalized)


if __name__ == "__main__":
    unittest.main()
