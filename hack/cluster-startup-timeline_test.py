#!/usr/bin/env python3
"""Tests for the HostedCluster startup timeline collector."""

from __future__ import annotations

import contextlib
import importlib.util
import io
import json
import os
import subprocess
import sys
import tempfile
import textwrap
import unittest
from pathlib import Path
from typing import Any
from unittest import mock


SCRIPT_PATH = Path(__file__).with_name("cluster-startup-timeline.py")
SPEC = importlib.util.spec_from_file_location("cluster_startup_timeline", SCRIPT_PATH)
timeline = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(timeline)


def condition(
    condition_type: str,
    status: str = "True",
    timestamp: str = "2026-01-01T00:05:00Z",
    reason: str = "AsExpected",
) -> dict[str, Any]:
    return {
        "type": condition_type,
        "status": status,
        "reason": reason,
        "message": "",
        "lastTransitionTime": timestamp,
    }


def fixture() -> dict[str, Any]:
    hosted_cluster = {
        "metadata": {
            "name": "example",
            "namespace": "clusters",
            "creationTimestamp": "2026-01-01T00:00:00Z",
            "generation": 1,
            "uid": "hosted-cluster-uid",
        },
        "spec": {"platform": {"type": "AWS"}},
        "status": {
            "conditions": [
                condition("ValidConfiguration"),
                condition("SupportedHostedCluster"),
                condition("ValidReleaseImage"),
                condition("ValidProxyConfiguration"),
                condition("Available"),
            ],
            "kubeconfig": {"name": "example-admin-kubeconfig"},
            "version": {
                "desired": {"image": "registry.example/release@sha256:0"},
                "history": [
                    {
                        "state": "Completed",
                        "image": "registry.example/release@sha256:0",
                        "completionTime": "2026-01-01T00:45:00Z",
                    }
                ],
            },
        },
    }
    hosted_control_plane = {
        "metadata": {
            "name": "example",
            "namespace": "clusters-example",
            "creationTimestamp": "2026-01-01T00:01:00Z",
            "generation": 1,
        },
        "status": {
            "conditions": [
                condition("InfrastructureReady"),
                condition("EtcdAvailable"),
                condition("KubeAPIServerAvailable"),
                condition("Available", timestamp="2026-01-01T00:20:00Z"),
            ],
            "kubeConfig": {"name": "example-kubeconfig"},
            "controlPlaneVersion": {
                "desired": {"image": "registry.example/release@sha256:0"},
                "history": [
                    {
                        "state": "Completed",
                        "image": "registry.example/release@sha256:0",
                        "completionTime": "2026-01-01T00:20:00Z",
                    }
                ],
            },
        },
    }
    node_pool = {
        "metadata": {
            "name": "workers",
            "namespace": "clusters",
            "creationTimestamp": "2026-01-01T00:10:00Z",
            "generation": 1,
            "ownerReferences": [
                {
                    "apiVersion": "hypershift.openshift.io/v1beta1",
                    "kind": "HostedCluster",
                    "name": "example",
                    "uid": "hosted-cluster-uid",
                }
            ],
        },
        "spec": {"clusterName": "example"},
        "status": {
            "version": "4.18.1",
            "conditions": [
                condition("ValidGeneratedPayload", timestamp="2026-01-01T00:11:00Z"),
                condition("ReachedIgnitionEndpoint", timestamp="2026-01-01T00:15:00Z"),
                condition("AllMachinesReady", timestamp="2026-01-01T00:30:00Z"),
                condition("AllNodesHealthy", timestamp="2026-01-01T00:31:00Z"),
                condition("Ready", timestamp="2026-01-01T00:32:00Z"),
            ],
        },
    }
    unrelated_node_pool = json.loads(json.dumps(node_pool))
    unrelated_node_pool["metadata"]["name"] = "other-cluster-workers"
    unrelated_node_pool["spec"]["clusterName"] = "other"
    component = {
        "metadata": {
            "name": "featuregate-generator",
            "namespace": "clusters-example",
            "creationTimestamp": "2026-01-01T00:03:00Z",
        },
        "status": {
            "version": "4.18.1",
            "conditions": [
                condition("Available", timestamp="2026-01-01T00:04:00Z"),
                condition("RolloutComplete", timestamp="2026-01-01T00:04:00Z"),
            ],
        },
    }

    def deployment(name: str) -> dict[str, Any]:
        return {
            "metadata": {
                "name": name,
                "namespace": "clusters-example",
                "creationTimestamp": "2026-01-01T00:02:00Z",
            },
            "spec": {"replicas": 1},
            "status": {
                "readyReplicas": 1,
                "availableReplicas": 1,
                "conditions": [condition("Available")],
            },
        }

    def capi_object(kind: str, name: str) -> dict[str, Any]:
        return {
            "metadata": {
                "name": name,
                "namespace": "clusters-example",
                "creationTimestamp": "2026-01-01T00:08:00Z",
            },
            "spec": {},
            "status": {"conditions": [condition("Ready")]},
        }

    return {
        "hostedclusters/clusters/example": hosted_cluster,
        "hostedcontrolplanes/clusters-example/example": hosted_control_plane,
        "nodepools/clusters/None": {"items": [node_pool, unrelated_node_pool]},
        "controlplanecomponents/clusters-example/None": {"items": [component]},
        "deployments/clusters-example/None": {
            "items": [
                deployment("control-plane-operator"),
                deployment("cluster-api"),
                deployment("capi-provider"),
            ]
        },
        "awsclusters.infrastructure.cluster.x-k8s.io/clusters-example/None": {
            "items": [capi_object("AWSCluster", "example")]
        },
        "clusters.cluster.x-k8s.io/clusters-example/None": {
            "items": [capi_object("Cluster", "example")]
        },
        "machinedeployments.cluster.x-k8s.io/clusters-example/None": {
            "items": [capi_object("MachineDeployment", "workers")]
        },
        "machines.cluster.x-k8s.io/clusters-example/None": {
            "items": [capi_object("Machine", "workers-0")]
        },
    }


class ClusterStartupTimelineTest(unittest.TestCase):
    def fake_oc(self, data: dict[str, Any]) -> str:
        temporary_directory = tempfile.TemporaryDirectory()
        self.addCleanup(temporary_directory.cleanup)
        script = temporary_directory.name + "/oc"
        fixture_json = json.dumps(data)
        Path(script).write_text(
            textwrap.dedent(
                f"""
                #!/usr/bin/env python3
                import json
                import sys

                fixture = json.loads({fixture_json!r})
                args = sys.argv[1:]
                if args[:2] == ["get", "secrets"]:
                    print("token-example\\t2026-01-01T00:11:00Z\\tTrue")
                    raise SystemExit(0)
                kind = args[1]
                name = args[2] if len(args) > 2 and not args[2].startswith("-") else None
                namespace = args[args.index("-n") + 1]
                value = fixture.get(f"{{kind}}/{{namespace}}/{{name}}")
                if value is None and name is not None:
                    print("{{}}")
                    raise SystemExit(0)
                value = value or {{"items": []}}
                print(json.dumps(value))
                """
            ).lstrip(),
            encoding="utf-8",
        )
        os.chmod(script, 0o700)
        return script

    def collect(self, data: dict[str, Any]):
        runner = timeline.OcRunner(self.fake_oc(data), None, None)
        return timeline.collect_snapshot(runner, None, "clusters", "example")

    def test_restore_first_seen_preserves_earliest_observation(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "observations.jsonl"
            observations = [
                {"collectedAt": "2026-01-01T00:01:00Z", "milestones": {"api": {"reached": False}}},
                {"collectedAt": "2026-01-01T00:02:00Z", "milestones": {"api": {"reached": True}}},
                {"collectedAt": "2026-01-01T00:03:00Z", "milestones": {"api": {"reached": True}}},
            ]
            path.write_text("\n".join(json.dumps(item) for item in observations) + "\n")
            self.assertEqual(
                timeline.restore_first_seen(path),
                {"api": {"timestamp": "2026-01-01T00:02:00Z"}},
            )

    def test_missing_hosted_cluster(self) -> None:
        snapshot = self.collect({})
        self.assertFalse(snapshot["hostedCluster"]["exists"])
        self.assertFalse(snapshot["milestones"]["hostedcluster_created"]["reached"])
        report = timeline.render_report(snapshot, {}, Path("/tmp/example-trace"))
        self.assertIn("Waiting for HostedCluster", report)

    def test_guest_bootstrap_errors_preserve_other_observations(self) -> None:
        runner = timeline.OcRunner("oc", "/tmp/guest.kubeconfig", None)
        replies = [
            subprocess.CompletedProcess([], 1, "", "ClusterVersion API is not ready"),
            subprocess.CompletedProcess([], 1, "", "ClusterOperator API is not ready"),
            subprocess.CompletedProcess([], 0, '{"items": []}', ""),
        ]
        with mock.patch.object(timeline.subprocess, "run", side_effect=replies):
            guest = timeline.collect_guest(runner)
        self.assertTrue(guest["enabled"])
        self.assertIsNone(guest["clusterVersion"])
        self.assertEqual(guest["nodes"], [])
        self.assertEqual(len(guest["errors"]), 2)

    def test_startup_through_worker_readiness(self) -> None:
        snapshot = self.collect(fixture())
        self.assertTrue(snapshot["hostedControlPlane"]["exists"])
        self.assertEqual(len(snapshot["nodePools"]), 1)
        self.assertEqual(snapshot["nodePools"][0]["version"], "4.18.1")
        self.assertEqual(snapshot["secrets"][0]["name"], "token-example")
        self.assertNotIn("data", snapshot["secrets"][0])

        milestones = snapshot["milestones"]
        for name in (
            "validation_passed",
            "infrastructure_ready",
            "featuregate_ready",
            "hostedcluster_kubeconfig_published",
            "control_plane_rollout_complete",
            "nodepool_payload_generated",
            "ignition_reached",
            "all_machines_ready",
            "all_nodes_healthy",
            "nodepool_ready",
        ):
            self.assertTrue(milestones[name]["reached"], name)
        self.assertEqual(
            milestones["nodepool_ready"]["timestamp"], "2026-01-01T00:32:00Z"
        )

        states = timeline.stage_states(milestones)
        self.assertEqual(states["P8"], "done")
        self.assertEqual(states["P9"], "unverified")

        report = timeline.render_report(snapshot, {}, Path("/tmp/example-trace"))
        self.assertIn("### Management operators", report)
        self.assertIn("### Control-plane components", report)
        self.assertIn("### NodePools", report)

    def test_nodepool_ready_requires_status_version(self) -> None:
        snapshot = self.collect(fixture())
        snapshot["nodePools"][0]["version"] = ""
        milestones = timeline.compute_milestones(snapshot)
        self.assertFalse(milestones["nodepool_ready"]["reached"])
        self.assertIn("status.version is not populated", milestones["nodepool_ready"]["detail"])

    def test_full_completion_requires_validation_and_explicit_operator_convergence(self) -> None:
        snapshot = self.collect(fixture())
        healthy = {
            "Available": condition("Available"),
            "Progressing": condition("Progressing", "False", "2026-01-01T00:50:00Z"),
            "Degraded": condition("Degraded", "False"),
        }
        snapshot["guest"] = {
            "enabled": True,
            "clusterVersion": {
                "conditions": dict(healthy, Failing=condition("Failing", "False")),
                "history": [{"state": "Completed", "completionTime": "2026-01-01T00:45:00Z"}],
            },
            "clusterOperators": [{"name": "network", "conditions": healthy}],
        }
        milestones = timeline.compute_milestones(snapshot)
        self.assertTrue(milestones["fully_complete"]["reached"])
        self.assertEqual(milestones["guest_operators_converged"]["timestamp"], "2026-01-01T00:50:00Z")
        # A copied CVO completion time must not backdate the full checklist.
        snapshot["collectedAt"] = "2026-01-01T00:55:00Z"
        snapshot["milestones"] = milestones
        first_seen = {}
        timeline.update_first_seen(snapshot, first_seen)
        timestamp, observed = timeline.best_milestone_time(
            milestones["fully_complete"], first_seen["fully_complete"]
        )
        self.assertTrue(observed)
        self.assertEqual(timeline.isoformat(timestamp), "2026-01-01T00:55:00Z")

        snapshot["hostedCluster"]["conditions"]["ValidReleaseImage"] = condition("ValidReleaseImage", "Unknown")
        self.assertFalse(timeline.compute_milestones(snapshot)["fully_complete"]["reached"])
        snapshot["hostedCluster"]["conditions"]["ValidReleaseImage"] = condition("ValidReleaseImage")

        snapshot["hostedCluster"]["conditions"]["ValidOIDCConfiguration"] = condition("ValidOIDCConfiguration", "False")
        self.assertFalse(timeline.compute_milestones(snapshot)["fully_complete"]["reached"])
        del snapshot["hostedCluster"]["conditions"]["ValidOIDCConfiguration"]

        for name in ("Progressing", "Degraded"):
            with self.subTest(condition=name):
                healthy[name] = condition(name, "Unknown")
                self.assertFalse(timeline.compute_milestones(snapshot)["fully_complete"]["reached"])
                del healthy[name]
                self.assertFalse(timeline.compute_milestones(snapshot)["fully_complete"]["reached"])
                healthy[name] = condition(name, "False")

    def test_once_writes_output_files(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            output_dir = Path(temporary_directory) / "trace"
            arguments = [
                sys.argv[0],
                "--namespace",
                "clusters",
                "--name",
                "example",
                "--oc",
                self.fake_oc(fixture()),
                "--once",
                "--output-dir",
                str(output_dir),
            ]
            with mock.patch.object(sys, "argv", arguments):
                with (
                    contextlib.redirect_stdout(io.StringIO()),
                    contextlib.redirect_stderr(io.StringIO()),
                ):
                    self.assertEqual(timeline.main(), 0)

            observations = (output_dir / "observations.jsonl").read_text(
                encoding="utf-8"
            ).splitlines()
            self.assertEqual(len(observations), 1)
            latest = json.loads(
                (output_dir / "latest.json").read_text(encoding="utf-8")
            )
            self.assertEqual(latest["cluster"]["name"], "example")
            report = (output_dir / "report.md").read_text(encoding="utf-8")
            self.assertIn("## Component timings", report)


if __name__ == "__main__":
    unittest.main()
