#!/usr/bin/env python3
"""Collect a HostedCluster startup timeline from a management cluster.

The collector intentionally queries a small, normalized subset of resources. It
never writes Secret data to disk: only Secret names, timestamps, and the
ignition-reached annotation are recorded.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


SCHEMA_VERSION = 1
COMPLETED_STATES = {"Completed"}
DEFAULT_INTERVAL_SECONDS = 10
DEFAULT_TIMEOUT_SECONDS = 45 * 60

VALIDATION_CONDITIONS = (
    "ValidConfiguration",
    "SupportedHostedCluster",
    "ValidReleaseImage",
    "ValidIDPConfiguration",
    "ValidProxyConfiguration",
    "ValidOIDCConfiguration",
    "ValidAWSIdentityProvider",
    "ValidAWSKMSConfig",
    "ValidAzureKMSConfig",
)

REQUIRED_VALIDATION_CONDITIONS = (
    "ValidConfiguration",
    "SupportedHostedCluster",
    "ValidReleaseImage",
    "ValidProxyConfiguration",
)

MILESTONE_DEFS: list[tuple[str, str, str]] = [
    ("hostedcluster_created", "P0", "HostedCluster created"),
    ("validation_passed", "P0", "Validation passed"),
    ("hostedcontrolplane_created", "P1", "HostedControlPlane created"),
    ("platform_infra_created", "P1", "Platform infrastructure object created"),
    ("capi_cluster_created", "P1", "CAPI Cluster created"),
    ("management_operators_deployed", "P1", "Management operators deployed"),
    ("management_operators_available", "P1", "Management operators available"),
    ("infrastructure_ready", "P2", "HCP InfrastructureReady"),
    ("etcd_available", "P3", "etcd available"),
    ("featuregate_ready", "P3", "featuregate-generator ready"),
    ("kube_apiserver_available", "P4", "Kube API server available"),
    ("hostedcluster_kubeconfig_published", "P4", "HostedCluster kubeconfig published"),
    ("hostedcontrolplane_available", "P5", "HostedControlPlane available"),
    ("hostedcluster_available", "P5", "HostedCluster available"),
    ("control_plane_rollout_complete", "P5", "Control-plane rollout complete"),
    ("nodepool_created", "P6", "NodePool created"),
    ("nodepool_payload_generated", "P6", "Node bootstrap payload generated"),
    ("machine_workload_created", "P6", "MachineDeployment or MachineSet created"),
    ("machine_created", "P7", "First CAPI Machine created"),
    ("ignition_reached", "P7", "Ignition endpoint reached"),
    ("all_machines_ready", "P8", "All CAPI Machines ready"),
    ("all_nodes_healthy", "P8", "All CAPI Nodes healthy"),
    ("nodepool_ready", "P8", "NodePool ready"),
    ("hostedcluster_version_complete", "P9", "HostedCluster version Completed"),
    ("guest_cvo_complete", "P9", "Guest ClusterVersion complete"),
    ("guest_operators_converged", "P9", "Guest ClusterOperators converged"),
    ("fully_complete", "P9", "Full completion checklist"),
]
MILESTONE_LABELS = {name: label for name, _, label in MILESTONE_DEFS}

STAGES = [
    (
        "P0",
        "Intake and validation",
        ("hostedcluster_created", "validation_passed"),
        "HostedCluster conditions; HyperShift Operator logs",
    ),
    (
        "P1",
        "Management-cluster scaffold",
        (
            "hostedcontrolplane_created",
            "platform_infra_created",
            "capi_cluster_created",
            "management_operators_deployed",
        ),
        "HCP, platform infrastructure CR, CAPI Cluster, operator deployments, events",
    ),
    (
        "P2",
        "Endpoint publication",
        ("infrastructure_ready",),
        "HCP InfrastructureReady reason; Services, Routes, load balancers, DNS",
    ),
    (
        "P3",
        "Control-plane foundation",
        ("etcd_available", "featuregate_ready"),
        "etcd StatefulSet, pods, PVCs, events; featuregate Job",
    ),
    (
        "P4",
        "Kube API server",
        ("kube_apiserver_available", "hostedcluster_kubeconfig_published"),
        "KAS Deployment, pods, Service/LB health, kubeconfig Secrets",
    ),
    (
        "P5",
        "Control-plane convergence",
        (
            "hostedcontrolplane_available",
            "hostedcluster_available",
            "control_plane_rollout_complete",
        ),
        "HCP Available reason; ControlPlaneComponent Available/RolloutComplete",
    ),
    (
        "P6",
        "NodePool bootstrap",
        (
            "nodepool_created",
            "nodepool_payload_generated",
            "machine_workload_created",
        ),
        "NodePool conditions; HostedCluster kubeconfig; token/user-data Secrets; MD/MS",
    ),
    (
        "P7",
        "Machine provisioning and ignition",
        ("machine_created", "ignition_reached"),
        "CAPI provider logs; Machine and platform machine; ignition DNS and endpoint",
    ),
    (
        "P8",
        "Worker readiness",
        ("all_machines_ready", "all_nodes_healthy", "nodepool_ready"),
        "CAPI Machine Ready/NodeHealthy; NodePool aggregate messages",
    ),
    (
        "P9",
        "Guest convergence",
        (
            "hostedcluster_version_complete",
            "guest_cvo_complete",
            "guest_operators_converged",
        ),
        "Guest ClusterVersion and ClusterOperators; HCCO logs; HostedCluster version status",
    ),
]


def utc_now() -> datetime:
    return datetime.now(timezone.utc)


def isoformat(value: datetime | None) -> str | None:
    if value is None:
        return None
    return value.astimezone(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


def parse_timestamp(value: Any) -> datetime | None:
    if not value or not isinstance(value, str):
        return None
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=timezone.utc)
    return parsed.astimezone(timezone.utc)


def parse_duration(value: str) -> float:
    match = re.fullmatch(r"\s*(\d+(?:\.\d+)?)\s*([smhd]?)\s*", value)
    if not match:
        raise argparse.ArgumentTypeError(
            f"invalid duration {value!r}; use a number followed by s, m, h, or d"
        )
    amount = float(match.group(1))
    unit = match.group(2) or "s"
    factor = {"s": 1, "m": 60, "h": 3600, "d": 86400}[unit]
    return amount * factor


def format_elapsed(seconds: float | None, observed: bool = False) -> str:
    if seconds is None:
        return "-"
    total = max(0, int(round(seconds)))
    if total < 60:
        body = f"{total}s"
    elif total < 3600:
        body = f"{total // 60}m {total % 60:02d}s"
    else:
        body = f"{total // 3600}h {(total % 3600) // 60:02d}m {total % 60:02d}s"
    return f"~{body}" if observed else body


def md(value: Any, limit: int = 240) -> str:
    if value is None:
        return ""
    text = str(value).replace("\r", " ").replace("\n", " ").replace("|", "\\|")
    if len(text) > limit:
        text = text[: limit - 3] + "..."
    return text


def condition_map(obj: dict[str, Any] | None) -> dict[str, dict[str, Any]]:
    if not obj:
        return {}
    status = obj.get("status") or {}
    conditions = status.get("conditions") or []
    if not isinstance(conditions, list):
        return {}
    result: dict[str, dict[str, Any]] = {}
    for condition in conditions:
        if not isinstance(condition, dict):
            continue
        condition_type = condition.get("type")
        if not condition_type:
            continue
        result[str(condition_type)] = {
            "status": condition.get("status", "Unknown"),
            "reason": condition.get("reason", ""),
            "message": condition.get("message", ""),
            "lastTransitionTime": condition.get("lastTransitionTime"),
            "observedGeneration": condition.get("observedGeneration"),
        }
    return result


def condition_is(conditions: dict[str, dict[str, Any]], name: str, status: str) -> bool:
    condition = conditions.get(name)
    return bool(condition and condition.get("status") == status)


def condition_detail(conditions: dict[str, dict[str, Any]], name: str) -> str:
    condition = conditions.get(name)
    if not condition:
        return f"{name} condition is missing"
    reason = condition.get("reason") or ""
    message = condition.get("message") or ""
    detail = f"{name}={condition.get('status', 'Unknown')}"
    if reason:
        detail += f" reason={reason}"
    if message:
        detail += f": {message}"
    return detail


def latest_condition_time(
    conditions: dict[str, dict[str, Any]], names: tuple[str, ...] | list[str]
) -> str | None:
    timestamps = []
    for name in names:
        condition = conditions.get(name)
        if condition:
            timestamp = parse_timestamp(condition.get("lastTransitionTime"))
            if timestamp:
                timestamps.append(timestamp)
    return isoformat(max(timestamps)) if timestamps else None


def latest_item_condition_time(
    items: list[dict[str, Any]], condition_name: str
) -> str | None:
    timestamps = []
    for item in items:
        condition = (item.get("conditions") or {}).get(condition_name)
        if condition:
            timestamp = parse_timestamp(condition.get("lastTransitionTime"))
            if timestamp:
                timestamps.append(timestamp)
    return isoformat(max(timestamps)) if timestamps else None


def first_history_entry(status_version: dict[str, Any] | None) -> dict[str, Any]:
    if not status_version:
        return {}
    history = status_version.get("history") or []
    if not isinstance(history, list) or not history:
        return {}
    return history[0] if isinstance(history[0], dict) else {}


def list_items(obj: dict[str, Any] | None) -> list[dict[str, Any]]:
    if not obj:
        return []
    items = obj.get("items") or []
    if not isinstance(items, list):
        return []
    return [item for item in items if isinstance(item, dict)]


def node_pool_belongs_to_hosted_cluster(
    item: dict[str, Any], hosted_cluster_name: str
) -> bool:
    return (item.get("spec") or {}).get("clusterName") == hosted_cluster_name


def metadata_summary(obj: dict[str, Any], kind: str) -> dict[str, Any]:
    metadata = obj.get("metadata") or {}
    status = obj.get("status") or {}
    return {
        "kind": kind,
        "name": metadata.get("name", ""),
        "namespace": metadata.get("namespace", ""),
        "uid": metadata.get("uid", ""),
        "createdAt": metadata.get("creationTimestamp"),
        "deletionTimestamp": metadata.get("deletionTimestamp"),
        "generation": metadata.get("generation"),
        "conditions": condition_map(obj),
        "ready": status.get("ready"),
        "initialized": status.get("initialized"),
        "nodeRef": (status.get("nodeRef") or {}).get("name")
        if isinstance(status.get("nodeRef"), dict)
        else None,
        "infrastructureRef": obj.get("spec", {}).get("infrastructureRef", {})
        if isinstance(obj.get("spec"), dict)
        else {},
        "bootstrapDataSecret": ((obj.get("spec") or {}).get("bootstrap") or {}).get("dataSecretName")
        if isinstance((obj.get("spec") or {}).get("bootstrap"), dict)
        else None,
        "failureReason": status.get("failureReason"),
        "failureMessage": status.get("failureMessage"),
    }


def summarize_deployment(obj: dict[str, Any]) -> dict[str, Any]:
    summary = metadata_summary(obj, "Deployment")
    spec = obj.get("spec") or {}
    status = obj.get("status") or {}
    summary.update(
        {
            "desiredReplicas": spec.get("replicas"),
            "replicas": status.get("replicas"),
            "readyReplicas": status.get("readyReplicas", 0),
            "availableReplicas": status.get("availableReplicas", 0),
            "updatedReplicas": status.get("updatedReplicas", 0),
            "unavailableReplicas": status.get("unavailableReplicas", 0),
            "available": condition_is(summary["conditions"], "Available", "True"),
        }
    )
    return summary


def summarize_pod(obj: dict[str, Any]) -> dict[str, Any]:
    metadata = obj.get("metadata") or {}
    status = obj.get("status") or {}
    container_statuses = status.get("containerStatuses") or []
    ready = bool(container_statuses) and all(
        isinstance(item, dict) and item.get("ready") is True for item in container_statuses
    )
    restarts = sum(
        int(item.get("restartCount", 0))
        for item in container_statuses
        if isinstance(item, dict)
    )
    return {
        "name": metadata.get("name", ""),
        "createdAt": metadata.get("creationTimestamp"),
        "phase": status.get("phase"),
        "nodeName": status.get("nodeName"),
        "ready": ready,
        "restarts": restarts,
        "reason": status.get("reason"),
        "message": status.get("message"),
    }


def summarize_event(obj: dict[str, Any]) -> dict[str, Any]:
    metadata = obj.get("metadata") or {}
    involved = obj.get("involvedObject") or {}
    timestamp = (
        obj.get("lastTimestamp")
        or obj.get("eventTime")
        or obj.get("firstTimestamp")
        or metadata.get("creationTimestamp")
    )
    return {
        "lastTimestamp": timestamp,
        "type": obj.get("type", ""),
        "reason": obj.get("reason", ""),
        "message": obj.get("message", ""),
        "involvedObject": f"{involved.get('kind', '')}/{involved.get('name', '')}".strip("/"),
    }


def summarize_capi_object(obj: dict[str, Any], kind: str) -> dict[str, Any]:
    summary = metadata_summary(obj, kind)
    spec = obj.get("spec") or {}
    status = obj.get("status") or {}
    if kind in {"MachineDeployment", "MachineSet"}:
        summary.update(
            {
                "replicas": spec.get("replicas"),
                "readyReplicas": status.get("readyReplicas", 0),
                "availableReplicas": status.get("availableReplicas", 0),
                "updatedReplicas": status.get("updatedReplicas", 0),
            }
        )
    if kind == "Machine":
        summary["clusterName"] = spec.get("clusterName")
    return summary


def summarize_guest_node(obj: dict[str, Any]) -> dict[str, Any]:
    metadata = obj.get("metadata") or {}
    return {
        "name": metadata.get("name", ""),
        "createdAt": metadata.get("creationTimestamp"),
        "conditions": condition_map(obj),
    }


def result(
    reached: bool | None,
    timestamp: str | None = None,
    source: str = "",
    detail: str = "",
    timing_basis: str = "reported timestamp",
) -> dict[str, Any]:
    return {
        "reached": reached,
        "timestamp": timestamp if reached is True else None,
        "source": source,
        "detail": detail,
        "timingBasis": timing_basis if reached is True else "",
    }


def condition_result(
    conditions: dict[str, dict[str, Any]], name: str, source: str
) -> dict[str, Any]:
    condition = conditions.get(name)
    if not condition:
        return result(False, source=source, detail=f"{name} condition is missing")
    if condition.get("status") == "True":
        return result(
            True,
            condition.get("lastTransitionTime"),
            source,
            timing_basis="condition transition",
        )
    return result(
        False,
        source=source,
        detail=condition_detail(conditions, name),
    )


def all_results(
    items: list[dict[str, Any]],
    timestamp: str | None,
    source: str,
    timing_basis: str = "reported timestamp",
) -> dict[str, Any]:
    if not items:
        return result(False, source=source, detail="no resources found")
    if any(item.get("reached") is None for item in items):
        return result(None, source=source, detail="not evaluated")
    if all(item.get("reached") is True for item in items):
        return result(True, timestamp, source, timing_basis=timing_basis)
    details = "; ".join(item.get("detail", "") for item in items if not item.get("reached"))
    return result(False, source=source, detail=details)


class OcRunner:
    def __init__(self, oc: str, kubeconfig: str | None, context: str | None) -> None:
        self.oc = oc
        self.kubeconfig = kubeconfig
        self.context = context
        self.errors: set[str] = set()

    def reset_errors(self) -> None:
        self.errors.clear()

    def _command(self, args: list[str]) -> list[str]:
        command = [self.oc]
        if self.kubeconfig:
            command.extend(["--kubeconfig", self.kubeconfig])
        if self.context:
            command.extend(["--context", self.context])
        command.extend(args)
        return command

    def get(
        self,
        kind: str,
        namespace: str | None = None,
        name: str | None = None,
        request_timeout: str = "15s",
    ) -> dict[str, Any] | None:
        if kind.startswith("/"):
            # Known guest API paths avoid discovery retries while aggregated
            # API servers are still starting.
            args = ["get", f"--raw={kind}", f"--request-timeout={request_timeout}"]
        else:
            args = ["get", kind]
            if name:
                args.append(name)
            if namespace:
                args.extend(["-n", namespace])
            args.extend(["-o", "json", "--ignore-not-found", f"--request-timeout={request_timeout}"])
        try:
            process = subprocess.run(
                self._command(args),
                check=False,
                capture_output=True,
                text=True,
                timeout=30,
            )
        except (OSError, subprocess.TimeoutExpired) as exc:
            self.errors.add(f"oc get {kind}: {exc}")
            return None
        stdout = process.stdout.strip()
        if process.returncode != 0:
            error = process.stderr.strip().splitlines()
            self.errors.add(f"oc get {kind}: {error[-1] if error else 'unknown error'}")
            return None
        if not stdout:
            return None
        try:
            value = json.loads(stdout)
        except json.JSONDecodeError as exc:
            self.errors.add(f"oc get {kind}: invalid JSON: {exc}")
            return None
        return value if isinstance(value, dict) else None

    def secret_metadata(self, namespace: str) -> list[dict[str, Any]]:
        template = (
            '{{range .items}}{{.metadata.name}}{{"\\t"}}{{.metadata.creationTimestamp}}'
            '{{"\\t"}}{{with .metadata.annotations}}{{index . "hypershift.openshift.io/ignition-reached"}}{{end}}'
            '{{"\\n"}}{{end}}'
        )
        args = [
            "get",
            "secrets",
            "-n",
            namespace,
            "-o",
            f"go-template={template}",
            "--request-timeout=15s",
        ]
        try:
            process = subprocess.run(
                self._command(args),
                check=False,
                capture_output=True,
                text=True,
                timeout=30,
            )
        except (OSError, subprocess.TimeoutExpired) as exc:
            self.errors.add(f"oc get secrets metadata: {exc}")
            return []
        if process.returncode != 0:
            error = process.stderr.strip().splitlines()
            self.errors.add(
                f"oc get secrets metadata: {error[-1] if error else 'unknown error'}"
            )
            return []
        secrets = []
        for line in process.stdout.splitlines():
            fields = line.split("\t")
            if len(fields) < 2:
                continue
            secrets.append(
                {
                    "name": fields[0],
                    "createdAt": fields[1] or None,
                    "ignitionReached": len(fields) > 2 and fields[2] == "True",
                }
            )
        return secrets


def summarize_versions(status_version: dict[str, Any] | None) -> dict[str, Any]:
    if not status_version:
        return {"desired": {}, "history": []}
    history = status_version.get("history") or []
    if not isinstance(history, list):
        history = []
    compact_history = []
    for entry in history[:5]:
        if not isinstance(entry, dict):
            continue
        compact_history.append(
            {
                "state": entry.get("state"),
                "image": entry.get("image"),
                "version": entry.get("version"),
                "startedTime": entry.get("startedTime"),
                "completionTime": entry.get("completionTime"),
            }
        )
    return {
        "desired": status_version.get("desired") or {},
        "history": compact_history,
    }


def collect_guest(
    guest_runner: OcRunner | None,
) -> dict[str, Any]:
    if guest_runner is None:
        return {"enabled": False, "error": "guest kubeconfig not supplied"}
    guest_runner.reset_errors()
    cluster_version = guest_runner.get(
        "/apis/config.openshift.io/v1/clusterversions/version", request_timeout="5s"
    )
    cluster_operators = guest_runner.get(
        "/apis/config.openshift.io/v1/clusteroperators", request_timeout="5s"
    )
    nodes = guest_runner.get("/api/v1/nodes", request_timeout="5s")
    guest = {
        "enabled": True,
        "clusterVersion": None,
        "clusterOperators": [],
        "nodes": [],
        "errors": sorted(guest_runner.errors),
    }
    if cluster_version:
        guest["clusterVersion"] = {
            "desired": (cluster_version.get("status") or {}).get("desired") or {},
            "history": summarize_versions(cluster_version.get("status") or {}).get(
                "history", []
            ),
            "conditions": condition_map(cluster_version),
        }
    for item in list_items(cluster_operators):
        metadata = item.get("metadata") or {}
        guest["clusterOperators"].append(
            {
                "name": metadata.get("name", ""),
                "createdAt": metadata.get("creationTimestamp"),
                "conditions": condition_map(item),
            }
        )
    guest["nodes"] = [summarize_guest_node(item) for item in list_items(nodes)]
    return guest


def collect_snapshot(
    management_runner: OcRunner,
    guest_runner: OcRunner | None,
    namespace: str,
    name: str,
) -> dict[str, Any]:
    management_runner.reset_errors()
    collected_at = utc_now()
    hosted_cluster = management_runner.get("hostedclusters", namespace=namespace, name=name)
    snapshot: dict[str, Any] = {
        "schemaVersion": SCHEMA_VERSION,
        "cluster": {"namespace": namespace, "name": name},
        "collectedAt": isoformat(collected_at),
        "elapsedSeconds": None,
        "platform": "",
        "hostedCluster": {"exists": False},
        "hostedControlPlane": {"exists": False},
        "controlPlaneComponents": [],
        "nodePools": [],
        "deployments": [],
        "pods": {"total": 0, "ready": 0, "notReady": 0, "unready": []},
        "secrets": [],
        "capi": {},
        "platformResources": {},
        "guest": collect_guest(guest_runner),
        "events": {},
        "collectionErrors": [],
    }
    if not hosted_cluster:
        snapshot["collectionErrors"] = sorted(management_runner.errors)
        snapshot["milestones"] = compute_milestones(snapshot)
        return snapshot

    hc_metadata = hosted_cluster.get("metadata") or {}
    hc_created = parse_timestamp(hc_metadata.get("creationTimestamp"))
    if hc_created:
        snapshot["elapsedSeconds"] = max(
            0.0, (collected_at - hc_created).total_seconds()
        )
    hc_spec = hosted_cluster.get("spec") or {}
    platform_type = str(((hc_spec.get("platform") or {}).get("type")) or "")
    snapshot["platform"] = platform_type
    snapshot["hostedCluster"] = {
        "exists": True,
        "namespace": hc_metadata.get("namespace", namespace),
        "name": hc_metadata.get("name", name),
        "uid": hc_metadata.get("uid", ""),
        "createdAt": hc_metadata.get("creationTimestamp"),
        "generation": hc_metadata.get("generation"),
        "conditions": condition_map(hosted_cluster),
        "kubeconfig": (hosted_cluster.get("status") or {}).get("kubeconfig"),
        "ignitionEndpoint": (hosted_cluster.get("status") or {}).get(
            "ignitionEndpoint", ""
        ),
        "version": summarize_versions((hosted_cluster.get("status") or {}).get("version")),
        "hasBeenAvailable": bool(
            (hc_metadata.get("annotations") or {}).get(
                "hypershift.openshift.io/HasBeenAvailable"
            )
        ),
    }

    node_pools = management_runner.get("nodepools", namespace=namespace)
    snapshot["nodePools"] = [
        {
            "name": (item.get("metadata") or {}).get("name", ""),
            "namespace": (item.get("metadata") or {}).get("namespace", namespace),
            "createdAt": (item.get("metadata") or {}).get("creationTimestamp"),
            "generation": (item.get("metadata") or {}).get("generation"),
            "version": (item.get("status") or {}).get("version", ""),
            "conditions": condition_map(item),
        }
        for item in list_items(node_pools)
        if node_pool_belongs_to_hosted_cluster(item, hc_metadata.get("name", name))
    ]

    hcp_namespace = f"{namespace}-{name.replace('.', '-')}"
    hosted_control_plane = management_runner.get(
        "hostedcontrolplanes", namespace=hcp_namespace, name=name
    )
    if hosted_control_plane:
        hcp_metadata = hosted_control_plane.get("metadata") or {}
        hcp_status = hosted_control_plane.get("status") or {}
        snapshot["hostedControlPlane"] = {
            "exists": True,
            "namespace": hcp_metadata.get("namespace", hcp_namespace),
            "name": hcp_metadata.get("name", name),
            "createdAt": hcp_metadata.get("creationTimestamp"),
            "generation": hcp_metadata.get("generation"),
            "ready": hcp_status.get("ready"),
            "initialized": hcp_status.get("initialized"),
            "conditions": condition_map(hosted_control_plane),
            "kubeConfig": hcp_status.get("kubeConfig"),
            "controlPlaneEndpoint": hcp_status.get("controlPlaneEndpoint") or {},
            "controlPlaneVersion": summarize_versions(hcp_status.get("controlPlaneVersion")),
            "versionStatus": summarize_versions(hcp_status.get("versionStatus")),
        }

        components = management_runner.get("controlplanecomponents", namespace=hcp_namespace)
        snapshot["controlPlaneComponents"] = [
            {
                "name": (item.get("metadata") or {}).get("name", ""),
                "createdAt": (item.get("metadata") or {}).get("creationTimestamp"),
                "version": (item.get("status") or {}).get("version", ""),
                "conditions": condition_map(item),
            }
            for item in list_items(components)
        ]

        deployments = management_runner.get("deployments", namespace=hcp_namespace)
        snapshot["deployments"] = [
            summarize_deployment(item) for item in list_items(deployments)
        ]

        pods = management_runner.get("pods", namespace=hcp_namespace)
        pod_summaries = [summarize_pod(item) for item in list_items(pods)]
        unready = sorted(
            [pod for pod in pod_summaries if not pod["ready"]],
            key=lambda pod: pod.get("name", ""),
        )
        snapshot["pods"] = {
            "total": len(pod_summaries),
            "ready": len(pod_summaries) - len(unready),
            "notReady": len(unready),
            "unready": unready[:20],
        }

        all_secrets = management_runner.secret_metadata(hcp_namespace)
        snapshot["secrets"] = [
            secret
            for secret in all_secrets
            if secret["name"].startswith("token-")
            or secret["name"].startswith("user-data-")
        ]

        capi: dict[str, Any] = {}
        for key, kind, resource in (
            ("clusters", "Cluster", "clusters.cluster.x-k8s.io"),
            (
                "machineDeployments",
                "MachineDeployment",
                "machinedeployments.cluster.x-k8s.io",
            ),
            ("machineSets", "MachineSet", "machinesets.cluster.x-k8s.io"),
            ("machines", "Machine", "machines.cluster.x-k8s.io"),
        ):
            response = management_runner.get(resource, namespace=hcp_namespace)
            capi[key] = [
                summarize_capi_object(item, kind) for item in list_items(response)
            ]
        snapshot["capi"] = capi

        platform_resources: dict[str, Any] = {}
        if platform_type.lower() == "aws":
            queries = (
                ("cluster", "AWSCluster", "awsclusters.infrastructure.cluster.x-k8s.io"),
                ("machines", "AWSMachine", "awsmachines.infrastructure.cluster.x-k8s.io"),
                (
                    "machineTemplates",
                    "AWSMachineTemplate",
                    "awsmachinetemplates.infrastructure.cluster.x-k8s.io",
                ),
            )
        elif platform_type.lower() == "azure":
            queries = (
                ("cluster", "AzureCluster", "azureclusters.infrastructure.cluster.x-k8s.io"),
                (
                    "clusterIdentity",
                    "AzureClusterIdentity",
                    "azureclusteridentities.infrastructure.cluster.x-k8s.io",
                ),
                (
                    "machines",
                    "AzureMachine",
                    "azuremachines.infrastructure.cluster.x-k8s.io",
                ),
                (
                    "machineTemplates",
                    "AzureMachineTemplate",
                    "azuremachinetemplates.infrastructure.cluster.x-k8s.io",
                ),
            )
        else:
            queries = ()
        for key, kind, resource in queries:
            response = management_runner.get(resource, namespace=hcp_namespace)
            items = list_items(response)
            if key in {"cluster", "clusterIdentity"}:
                platform_resources[key] = (
                    summarize_capi_object(items[0], kind) if items else None
                )
            else:
                platform_resources[key] = [
                    summarize_capi_object(item, kind) for item in items
                ]
        snapshot["platformResources"] = platform_resources

        events: dict[str, Any] = {}
        relevant_object_names = {
            hc_metadata.get("name", name),
            *(node_pool.get("name", "") for node_pool in snapshot["nodePools"]),
        }
        for key, event_namespace in (
            ("hostedClusterNamespace", namespace),
            ("controlPlaneNamespace", hcp_namespace),
        ):
            response = management_runner.get("events", namespace=event_namespace)
            warnings = []
            for item in list_items(response):
                event_summary = summarize_event(item)
                if event_summary.get("type") != "Warning":
                    continue
                if event_namespace == namespace:
                    involved_object = item.get("involvedObject") or {}
                    if (
                        involved_object.get("kind") not in {"HostedCluster", "NodePool"}
                        or involved_object.get("name") not in relevant_object_names
                    ):
                        continue
                warnings.append(event_summary)
            warnings.sort(key=lambda event: event.get("lastTimestamp") or "")
            events[key] = warnings[-30:]
        snapshot["events"] = events

    snapshot["collectionErrors"] = sorted(management_runner.errors)
    snapshot["milestones"] = compute_milestones(snapshot)
    return snapshot


def compute_milestones(snapshot: dict[str, Any]) -> dict[str, dict[str, Any]]:
    milestones: dict[str, dict[str, Any]] = {
        name: result(False, source="not observed") for name, _, _ in MILESTONE_DEFS
    }
    hc = snapshot.get("hostedCluster") or {}
    hcp = snapshot.get("hostedControlPlane") or {}
    guest = snapshot.get("guest") or {}

    if not hc.get("exists"):
        return milestones

    milestones["hostedcluster_created"] = result(
        True,
        hc.get("createdAt"),
        "HostedCluster.metadata.creationTimestamp",
        timing_basis="creation timestamp",
    )
    hc_conditions = hc.get("conditions") or {}
    blocking_validation = [
        condition_detail(hc_conditions, name)
        for name in VALIDATION_CONDITIONS
        if condition_is(hc_conditions, name, "False")
    ]
    missing_validation = [
        name for name in REQUIRED_VALIDATION_CONDITIONS if name not in hc_conditions
    ]
    milestones["validation_passed"] = result(
        not blocking_validation and not missing_validation,
        latest_condition_time(hc_conditions, REQUIRED_VALIDATION_CONDITIONS),
        source="HostedCluster validation conditions",
        timing_basis="condition transition",
        detail=(
            "blocking conditions: " + "; ".join(blocking_validation)
            if blocking_validation
            else "missing conditions: " + ", ".join(missing_validation)
            if missing_validation
            else "all required validation conditions are true"
        ),
    )

    if hcp.get("exists"):
        milestones["hostedcontrolplane_created"] = result(
            True,
            hcp.get("createdAt"),
            "HostedControlPlane.metadata.creationTimestamp",
            timing_basis="creation timestamp",
        )
    platform_resources = snapshot.get("platformResources") or {}
    platform_cluster = platform_resources.get("cluster")
    if platform_cluster:
        milestones["platform_infra_created"] = result(
            True,
            platform_cluster.get("createdAt"),
            f"{platform_cluster.get('kind', 'platform cluster')}.metadata.creationTimestamp",
            timing_basis="creation timestamp",
        )
    capi_clusters = (snapshot.get("capi") or {}).get("clusters") or []
    if capi_clusters:
        milestones["capi_cluster_created"] = result(
            True,
            capi_clusters[0].get("createdAt"),
            "CAPI Cluster.metadata.creationTimestamp",
            timing_basis="creation timestamp",
        )

    deployments = {
        deployment.get("name"): deployment
        for deployment in snapshot.get("deployments") or []
    }
    required_operator_names = (
        "control-plane-operator",
        "cluster-api",
        "capi-provider",
    )
    operator_deployments = [
        deployments.get(name) for name in required_operator_names if deployments.get(name)
    ]
    if len(operator_deployments) == len(required_operator_names):
        timestamps = [deployment.get("createdAt") for deployment in operator_deployments]
        parsed = [parse_timestamp(timestamp) for timestamp in timestamps]
        parsed = [timestamp for timestamp in parsed if timestamp]
        milestones["management_operators_deployed"] = result(
            True,
            isoformat(max(parsed)) if parsed else None,
            "management operator Deployments",
            timing_basis="creation timestamp",
        )
        available = [
            deployment.get("available") is True for deployment in operator_deployments
        ]
        milestones["management_operators_available"] = result(
            all(available),
            latest_condition_time(
                {
                    name: condition
                    for deployment in operator_deployments
                    for name, condition in (deployment.get("conditions") or {}).items()
                },
                ("Available",),
            ),
            "management operator Deployment Available conditions",
            "one or more required operator Deployments are not available"
            if not all(available)
            else "",
        )
    else:
        missing = [
            name for name in required_operator_names if not deployments.get(name)
        ]
        milestones["management_operators_deployed"] = result(
            False,
            source="management operator Deployments",
            detail="missing: " + ", ".join(missing),
        )
        milestones["management_operators_available"] = result(
            False,
            source="management operator Deployment Available conditions",
            detail="deployments not created yet",
        )

    if hcp.get("exists"):
        hcp_conditions = hcp.get("conditions") or {}
        milestones["infrastructure_ready"] = condition_result(
            hcp_conditions, "InfrastructureReady", "HCP InfrastructureReady"
        )
        milestones["etcd_available"] = condition_result(
            hcp_conditions, "EtcdAvailable", "HCP EtcdAvailable"
        )
        milestones["kube_apiserver_available"] = condition_result(
            hcp_conditions, "KubeAPIServerAvailable", "HCP KubeAPIServerAvailable"
        )
        milestones["hostedcontrolplane_available"] = condition_result(
            hcp_conditions, "Available", "HCP Available"
        )

        components = {
            component.get("name"): component
            for component in snapshot.get("controlPlaneComponents") or []
        }
        featuregate = components.get("featuregate-generator")
        if featuregate:
            fg_conditions = featuregate.get("conditions") or {}
            available = condition_is(fg_conditions, "Available", "True")
            rollout_complete = condition_is(fg_conditions, "RolloutComplete", "True")
            milestones["featuregate_ready"] = result(
                available and rollout_complete,
                latest_condition_time(fg_conditions, ("Available", "RolloutComplete")),
                "ControlPlaneComponent/featuregate-generator",
                condition_detail(fg_conditions, "Available")
                if not available
                else condition_detail(fg_conditions, "RolloutComplete")
                if not rollout_complete
                else "",
                timing_basis="condition transition",
            )
        else:
            milestones["featuregate_ready"] = result(
                False,
                source="ControlPlaneComponent/featuregate-generator",
                detail="component has not been created",
            )

        cp_version = first_history_entry(hcp.get("controlPlaneVersion"))
        cp_rollout_complete = cp_version.get("state") in COMPLETED_STATES
        milestones["control_plane_rollout_complete"] = result(
            cp_rollout_complete,
            cp_version.get("completionTime") if cp_rollout_complete else None,
            "HCP status.controlPlaneVersion.history[0]",
            "" if cp_rollout_complete else "control-plane history is not Completed",
            timing_basis="completion time",
        )

    if hc.get("kubeconfig"):
        milestones["hostedcluster_kubeconfig_published"] = result(
            True, source="HostedCluster.status.kubeconfig"
        )
    milestones["hostedcluster_available"] = condition_result(
        hc_conditions, "Available", "HostedCluster Available"
    )

    node_pools = snapshot.get("nodePools") or []
    if node_pools:
        creation_times = [
            parse_timestamp(node_pool.get("createdAt")) for node_pool in node_pools
        ]
        creation_times = [timestamp for timestamp in creation_times if timestamp]
        milestones["nodepool_created"] = result(
            True,
            isoformat(min(creation_times)) if creation_times else None,
            "NodePool.metadata.creationTimestamp",
            timing_basis="creation timestamp",
        )

        payload_results = []
        for node_pool in node_pools:
            conditions = node_pool.get("conditions") or {}
            payload_results.append(
                condition_result(
                    conditions,
                    "ValidGeneratedPayload",
                    f"NodePool/{node_pool.get('name')}/ValidGeneratedPayload",
                )
            )
        payload_time = latest_item_condition_time(node_pools, "ValidGeneratedPayload")
        milestones["nodepool_payload_generated"] = all_results(
            payload_results,
            payload_time,
            "NodePool ValidGeneratedPayload",
            timing_basis="condition transition",
        )

        ignition_results = [
            condition_result(
                node_pool.get("conditions") or {},
                "ReachedIgnitionEndpoint",
                f"NodePool/{node_pool.get('name')}/ReachedIgnitionEndpoint",
            )
            for node_pool in node_pools
        ]
        ignition_time = latest_item_condition_time(
            node_pools, "ReachedIgnitionEndpoint"
        )
        milestones["ignition_reached"] = all_results(
            ignition_results,
            ignition_time,
            "NodePool ReachedIgnitionEndpoint",
            timing_basis="condition transition",
        )

        machines_ready_results = [
            condition_result(
                node_pool.get("conditions") or {},
                "AllMachinesReady",
                f"NodePool/{node_pool.get('name')}/AllMachinesReady",
            )
            for node_pool in node_pools
        ]
        machines_ready_time = latest_item_condition_time(node_pools, "AllMachinesReady")
        milestones["all_machines_ready"] = all_results(
            machines_ready_results,
            machines_ready_time,
            "NodePool AllMachinesReady",
            timing_basis="condition transition",
        )

        nodes_healthy_results = [
            condition_result(
                node_pool.get("conditions") or {},
                "AllNodesHealthy",
                f"NodePool/{node_pool.get('name')}/AllNodesHealthy",
            )
            for node_pool in node_pools
        ]
        nodes_healthy_time = latest_item_condition_time(node_pools, "AllNodesHealthy")
        milestones["all_nodes_healthy"] = all_results(
            nodes_healthy_results,
            nodes_healthy_time,
            "NodePool AllNodesHealthy",
            timing_basis="condition transition",
        )

        nodepool_ready_results = []
        for node_pool in node_pools:
            conditions = node_pool.get("conditions") or {}
            ready = condition_is(conditions, "Ready", "True")
            has_version = bool(node_pool.get("version"))
            nodepool_ready_results.append(
                result(
                    ready and has_version,
                    conditions.get("Ready", {}).get("lastTransitionTime")
                    if ready and has_version
                    else None,
                    source=f"NodePool/{node_pool.get('name')}/Ready",
                    detail=condition_detail(conditions, "Ready")
                    if not ready
                    else "status.version is not populated"
                    if not has_version
                    else "",
                )
            )
        nodepool_ready_time = latest_item_condition_time(node_pools, "Ready")
        milestones["nodepool_ready"] = all_results(
            nodepool_ready_results,
            nodepool_ready_time,
            "NodePool Ready and status.version",
            timing_basis="condition transition",
        )

    capi = snapshot.get("capi") or {}
    machine_deployments = capi.get("machineDeployments") or []
    machine_sets = capi.get("machineSets") or []
    workload_times = [
        parse_timestamp(item.get("createdAt"))
        for item in [*machine_deployments, *machine_sets]
    ]
    workload_times = [timestamp for timestamp in workload_times if timestamp]
    milestones["machine_workload_created"] = result(
        bool(machine_deployments or machine_sets),
        isoformat(min(workload_times)) if workload_times else None,
        "CAPI MachineDeployment/MachineSet",
        timing_basis="creation timestamp",
    )

    machines = capi.get("machines") or []
    machine_times = [
        parse_timestamp(machine.get("createdAt")) for machine in machines
    ]
    machine_times = [timestamp for timestamp in machine_times if timestamp]
    milestones["machine_created"] = result(
        bool(machines),
        isoformat(min(machine_times)) if machine_times else None,
        "CAPI Machine.metadata.creationTimestamp",
        timing_basis="creation timestamp",
    )

    hc_version = hc.get("version") or {}
    hc_history = first_history_entry(hc_version)
    desired_image = ((hc_version.get("desired") or {}).get("image")) or ""
    hc_version_complete = (
        hc_history.get("state") in COMPLETED_STATES
        and bool(desired_image)
        and hc_history.get("image") == desired_image
    )
    milestones["hostedcluster_version_complete"] = result(
        hc_version_complete,
        hc_history.get("completionTime") if hc_version_complete else None,
        "HostedCluster.status.version.history[0]",
        ""
        if hc_version_complete
        else "desired image or Completed state does not match",
        timing_basis="completion time",
    )

    guest_enabled = bool(guest.get("enabled"))
    if guest_enabled:
        cluster_version = guest.get("clusterVersion") or {}
        cvo_conditions = cluster_version.get("conditions") or {}
        cvo_history = first_history_entry({"history": cluster_version.get("history") or []})
        guest_cvo_complete = (
            condition_is(cvo_conditions, "Available", "True")
            and condition_is(cvo_conditions, "Progressing", "False")
            and condition_is(cvo_conditions, "Failing", "False")
            and cvo_history.get("state") in COMPLETED_STATES
        )
        milestones["guest_cvo_complete"] = result(
            guest_cvo_complete,
            cvo_history.get("completionTime") if guest_cvo_complete else None,
            "guest ClusterVersion",
            "" if guest_cvo_complete else "guest CVO conditions or history are not complete",
        )

        operators = guest.get("clusterOperators") or []
        if not operators:
            milestones["guest_operators_converged"] = result(
                False,
                source="guest ClusterOperators",
                detail="no ClusterOperators found in the guest cluster",
            )
        else:
            failed_operators = []
            for operator in operators:
                conditions = operator.get("conditions") or {}
                if not condition_is(conditions, "Available", "True"):
                    failed_operators.append(
                        f"{operator.get('name')}: {condition_detail(conditions, 'Available')}"
                    )
                if condition_is(conditions, "Progressing", "True"):
                    failed_operators.append(
                        f"{operator.get('name')}: {condition_detail(conditions, 'Progressing')}"
                    )
                if condition_is(conditions, "Degraded", "True"):
                    failed_operators.append(
                        f"{operator.get('name')}: {condition_detail(conditions, 'Degraded')}"
                    )
            operator_times = []
            for operator in operators:
                timestamp = parse_timestamp(
                    ((operator.get("conditions") or {}).get("Available") or {}).get(
                        "lastTransitionTime"
                    )
                )
                if timestamp:
                    operator_times.append(timestamp)
            milestones["guest_operators_converged"] = result(
                not failed_operators,
                isoformat(max(operator_times)) if operator_times else None,
                "guest ClusterOperators",
                "; ".join(failed_operators[:8]),
            )
    else:
        milestones["guest_cvo_complete"] = result(
            None,
            source="guest ClusterVersion",
            detail="guest kubeconfig not supplied",
        )
        milestones["guest_operators_converged"] = result(
            None,
            source="guest ClusterOperators",
            detail="guest kubeconfig not supplied",
        )

    completion_inputs = [
        "hostedcluster_available",
        "hostedcontrolplane_available",
        "control_plane_rollout_complete",
        "kernel_placeholder",
    ]
    del completion_inputs
    full_inputs = (
        "hostedcluster_available",
        "hostedcontrolplane_available",
        "control_plane_rollout_complete",
        "nodepool_ready",
        "all_machines_ready",
        "all_nodes_healthy",
        "hostedcluster_version_complete",
        "guest_cvo_complete",
        "guest_operators_converged",
    )
    full_results = [milestones[name] for name in full_inputs]
    if any(item.get("reached") is None for item in full_results):
        milestones["fully_complete"] = result(
            None,
            source="full completion checklist",
            detail="guest checks were not evaluated",
        )
    elif all(item.get("reached") is True for item in full_results):
        timestamps = [
            parse_timestamp(item.get("timestamp"))
            for item in full_results
            if item.get("timestamp")
        ]
        timestamps = [timestamp for timestamp in timestamps if timestamp]
        milestones["fully_complete"] = result(
            True,
            isoformat(max(timestamps)) if timestamps else None,
            "full completion checklist",
        )
    else:
        incomplete = [
            MILESTONE_LABELS.get(name, name)
            for name in full_inputs
            if milestones[name].get("reached") is not True
        ]
        milestones["fully_complete"] = result(
            False,
            source="full completion checklist",
            detail="incomplete: " + ", ".join(incomplete),
        )
    return milestones


def stage_states(milestones: dict[str, dict[str, Any]]) -> dict[str, str]:
    states: dict[str, str] = {}
    current_assigned = False
    for stage, _, required, _ in STAGES:
        values = [milestones.get(name) or result(False) for name in required]
        if values and all(value.get("reached") is True for value in values):
            states[stage] = "done"
            continue
        if any(value.get("reached") is None for value in values):
            states[stage] = "unverified" if not current_assigned else "pending"
            if not current_assigned:
                current_assigned = True
            continue
        states[stage] = "current" if not current_assigned else "pending"
        current_assigned = True
    return states


def best_milestone_time(
    milestone: dict[str, Any], first_seen: dict[str, dict[str, str]]
) -> tuple[datetime | None, bool]:
    timestamp = parse_timestamp(milestone.get("timestamp"))
    if timestamp:
        return timestamp, False
    observed = parse_timestamp(first_seen.get("timestamp") if isinstance(first_seen, dict) else None)
    if observed:
        return observed, True
    return None, True


def timing_for(
    milestone: dict[str, Any],
    name: str,
    first_seen: dict[str, dict[str, str]],
    start: datetime | None,
) -> tuple[datetime | None, float | None, bool]:
    timestamp, observed = best_milestone_time(milestone, first_seen.get(name, {}))
    if timestamp is None:
        return None, None, observed
    elapsed = (timestamp - start).total_seconds() if start else None
    return timestamp, elapsed, observed


def milestone_stage(name: str) -> str:
    for milestone_name, stage, _ in MILESTONE_DEFS:
        if milestone_name == name:
            return stage
    return ""


def stage_reached_time(
    stage: str,
    snapshot: dict[str, Any],
    first_seen: dict[str, dict[str, str]],
    start: datetime | None,
) -> tuple[datetime | None, float | None, bool]:
    required = next(item[2] for item in STAGES if item[0] == stage)
    if not required or any(
        ((snapshot.get("milestones") or {}).get(name) or {}).get("reached") is not True
        for name in required
    ):
        return None, None, False
    timestamps = []
    observed_any = False
    for name in required:
        milestone = (snapshot.get("milestones") or {}).get(name) or {}
        timestamp, elapsed, observed = timing_for(
            milestone, name, first_seen, start
        )
        if timestamp:
            timestamps.append((timestamp, elapsed, observed))
    if not timestamps:
        return None, None, observed_any
    latest = max(timestamps, key=lambda item: item[0])
    return latest[0], latest[1], latest[2]


def condition_time_text(
    conditions: dict[str, dict[str, Any]], name: str, start: datetime | None
) -> str:
    condition = conditions.get(name)
    if not condition:
        return "missing"
    status = str(condition.get("status", "Unknown"))
    if status != "True":
        reason = condition.get("reason") or ""
        return f"{status} ({reason})" if reason else status
    timestamp = parse_timestamp(condition.get("lastTransitionTime"))
    if timestamp and start:
        return f"True at {format_elapsed((timestamp - start).total_seconds())}"
    return status


def blocker_details(snapshot: dict[str, Any], stage: str) -> list[str]:
    hc = snapshot.get("hostedCluster") or {}
    hcp = snapshot.get("hostedControlPlane") or {}
    hc_conditions = hc.get("conditions") or {}
    hcp_conditions = hcp.get("conditions") or {}
    node_pools = snapshot.get("nodePools") or []
    details: list[str] = []

    if not hc.get("exists"):
        return ["HostedCluster has not been observed yet."]

    if stage == "P0":
        false_conditions = [
            condition_detail(hc_conditions, name)
            for name in VALIDATION_CONDITIONS
            if condition_is(hc_conditions, name, "False")
        ]
        return false_conditions or ["Validation is still in progress or not yet reported."]
    if stage == "P1":
        if not hcp.get("exists"):
            details.append("HostedControlPlane has not been created.")
        if not (snapshot.get("platformResources") or {}).get("cluster"):
            details.append("Platform infrastructure CR has not been created.")
        if not (snapshot.get("capi") or {}).get("clusters"):
            details.append("CAPI Cluster has not been created.")
        deployments = {
            deployment.get("name"): deployment
            for deployment in snapshot.get("deployments") or []
        }
        for name in ("control-plane-operator", "cluster-api", "capi-provider"):
            if name not in deployments:
                details.append(f"Deployment/{name} has not been created.")
            elif not deployments[name].get("available"):
                details.append(
                    condition_detail(deployments[name].get("conditions") or {}, "Available")
                )
        return details or ["Scaffold resources have been created; waiting for next stage."]
    if stage == "P2":
        return [condition_detail(hcp_conditions, "InfrastructureReady")]
    if stage == "P3":
        details.append(condition_detail(hcp_conditions, "EtcdAvailable"))
        components = {
            component.get("name"): component
            for component in snapshot.get("controlPlaneComponents") or []
        }
        featuregate = components.get("featuregate-generator")
        if featuregate:
            for condition_name in ("Available", "RolloutComplete"):
                condition = (featuregate.get("conditions") or {}).get(condition_name)
                if not condition or condition.get("status") != "True":
                    details.append(
                        condition_detail(featuregate.get("conditions") or {}, condition_name)
                    )
        else:
            details.append("ControlPlaneComponent/featuregate-generator has not been created.")
        return details
    if stage == "P4":
        details.append(condition_detail(hcp_conditions, "KubeAPIServerAvailable"))
        if not hc.get("kubeconfig"):
            details.append("HostedCluster.status.kubeconfig is not populated.")
        return details
    if stage == "P5":
        details.append(condition_detail(hcp_conditions, "Available"))
        for component in snapshot.get("controlPlaneComponents") or []:
            conditions = component.get("conditions") or {}
            if not condition_is(conditions, "Available", "True"):
                details.append(
                    f"{component.get('name')}: {condition_detail(conditions, 'Available')}"
                )
            if not condition_is(conditions, "RolloutComplete", "True"):
                details.append(
                    f"{component.get('name')}: {condition_detail(conditions, 'RolloutComplete')}"
                )
            if len(details) >= 10:
                break
        return details[:10]
    if stage == "P6":
        if not hc.get("kubeconfig"):
            details.append("HostedCluster.status.kubeconfig is not populated.")
        for node_pool in node_pools:
            conditions = node_pool.get("conditions") or {}
            for name in (
                "ValidReleaseImage",
                "ValidPlatformImage",
                "ValidMachineConfig",
                "ValidPlatformConfig",
                "IgnitionEndpointAvailable",
                "ValidGeneratedPayload",
            ):
                condition = conditions.get(name)
                if condition and condition.get("status") != "True":
                    details.append(condition_detail(conditions, name))
            if len(details) >= 10:
                break
        if not ((snapshot.get("capi") or {}).get("machineDeployments") or []):
            if not ((snapshot.get("capi") or {}).get("machineSets") or []):
                details.append("No CAPI MachineDeployment or MachineSet exists yet.")
        return details or ["NodePool bootstrap resources have not converged."]
    if stage == "P7":
        for machine in (snapshot.get("capi") or {}).get("machines") or []:
            conditions = machine.get("conditions") or {}
            if not condition_is(conditions, "Ready", "True"):
                details.append(
                    f"Machine/{machine.get('name')}: {condition_detail(conditions, 'Ready')}"
                )
            if len(details) >= 6:
                break
        for node_pool in node_pools:
            condition = (node_pool.get("conditions") or {}).get("ReachedIgnitionEndpoint")
            if not condition or condition.get("status") != "True":
                details.append(
                    f"NodePool/{node_pool.get('name')}: "
                    + condition_detail(node_pool.get("conditions") or {}, "ReachedIgnitionEndpoint")
                )
        return details or ["Waiting for machine provisioning and ignition."]
    if stage == "P8":
        for node_pool in node_pools:
            conditions = node_pool.get("conditions") or {}
            for name in ("AllMachinesReady", "AllNodesHealthy", "Ready"):
                condition = conditions.get(name)
                if not condition or condition.get("status") != "True":
                    details.append(
                        f"NodePool/{node_pool.get('name')}: {condition_detail(conditions, name)}"
                    )
        return details or ["Worker readiness is not yet complete."]
    if stage == "P9":
        guest = snapshot.get("guest") or {}
        if not guest.get("enabled"):
            return ["Guest checks were not run; pass --guest-kubeconfig to evaluate P9."]
        if guest.get("errors"):
            details.extend(f"Guest query: {error}" for error in guest["errors"][:5])
        cluster_version = guest.get("clusterVersion") or {}
        cvo_conditions = cluster_version.get("conditions") or {}
        for name in ("Available", "Progressing", "Failing"):
            condition = cvo_conditions.get(name)
            if condition and condition.get("status") not in ("True", "False"):
                details.append(condition_detail(cvo_conditions, name))
        history = first_history_entry(
            {"history": cluster_version.get("history") or []}
        )
        if history.get("state") not in COMPLETED_STATES:
            details.append(
                f"Guest ClusterVersion history state is {history.get('state') or 'missing'}."
            )
        for operator in guest.get("clusterOperators") or []:
            conditions = operator.get("conditions") or {}
            if (
                not condition_is(conditions, "Available", "True")
                or condition_is(conditions, "Progressing", "True")
                or condition_is(conditions, "Degraded", "True")
            ):
                details.append(f"ClusterOperator/{operator.get('name')} is not converged.")
            if len(details) >= 10:
                break
        return details or ["Guest convergence is not yet complete."]
    return details


def render_report(
    snapshot: dict[str, Any],
    first_seen: dict[str, dict[str, str]],
    output_dir: Path,
) -> str:
    cluster = snapshot.get("cluster") or {}
    hc = snapshot.get("hostedCluster") or {}
    milestones = snapshot.get("milestones") or {}
    collected_at = parse_timestamp(snapshot.get("collectedAt"))
    start = parse_timestamp(hc.get("createdAt"))
    states = stage_states(milestones)
    current_stage = next(
        (stage for stage, _, _, _ in STAGES if states.get(stage) != "done"), "P9"
    )
    full = milestones.get("fully_complete") or {}
    if full.get("reached") is True:
        overall = "Complete"
    elif full.get("reached") is None:
        overall = "In progress; guest checks not evaluated"
    elif not hc.get("exists"):
        overall = "Waiting for HostedCluster"
    else:
        overall = "In progress"

    lines: list[str] = [
        f"# HostedCluster startup timeline: {cluster.get('namespace', '')}/{cluster.get('name', '')}",
        "",
        f"- Collected: {snapshot.get('collectedAt', '')}",
        f"- Platform: {snapshot.get('platform') or 'not observed'}",
        f"- Overall: {overall}",
        f"- Current stage: {current_stage}",
        f"- Observation elapsed: {format_elapsed(snapshot.get('elapsedSeconds'))}",
        "",
    ]
    if not hc.get("exists"):
        lines.extend(
            [
                "HostedCluster has not been observed yet. If the create request is still",
                "being processed, leave the collector running; it will begin timing when",
                "the object appears.",
                "",
            ]
        )
    else:
        lines.append(f"- Control-plane namespace: {hc.get('namespace')}-{str(hc.get('name')).replace('.', '-')}")
        lines.append("")

    lines.extend(["## Stage status", "", "| Stage | State | Reached | Look next |", "|---|---|---|---|"])
    for stage, label, _, inspect in STAGES:
        reached, elapsed, observed = stage_reached_time(
            stage, snapshot, first_seen, start
        )
        reached_text = format_elapsed(elapsed, observed) if reached else "-"
        lines.append(
            f"| {stage} {md(label)} | {states.get(stage, 'pending')} | {reached_text} | {md(inspect)} |"
        )
    lines.append("")

    lines.extend(
        [
            "## Milestone timings",
            "",
            "`~` means the time comes from the first polling observation because the",
            "resource does not expose a transition timestamp for that milestone.",
            "",
            "| Stage | Milestone | State | Elapsed | Timing basis | Source |",
            "|---|---|---|---|---|---|",
        ]
    )
    ordered_milestones = []
    for name, stage, label in MILESTONE_DEFS:
        milestone = milestones.get(name) or {}
        timestamp, elapsed, observed = timing_for(
            milestone, name, first_seen, start
        )
        reached = milestone.get("reached")
        state = "yes" if reached is True else "no" if reached is False else "not checked"
        elapsed_text = format_elapsed(elapsed, observed) if timestamp else "-"
        basis = (
            "first observed"
            if observed
            else str(milestone.get("timingBasis") or "")
            if milestone.get("timingBasis")
            else "condition transition"
            if "condition" in str(milestone.get("source", "")).lower()
            else "creation timestamp"
        )
        lines.append(
            f"| {stage} | {md(label)} | {state} | {elapsed_text} | {basis} | {md(milestone.get('source'))} |"
        )
        if timestamp:
            ordered_milestones.append((timestamp, name, stage, label, observed, elapsed_text))
    lines.append("")

    lines.extend(["## Component timings", ""])
    deployments = snapshot.get("deployments") or []
    management_operators = [
        deployment
        for deployment in deployments
        if deployment.get("name")
        in {"control-plane-operator", "cluster-api", "capi-provider"}
    ]
    if management_operators:
        lines.extend(
            [
                "### Management operators",
                "",
                "| Deployment | Created | Available | Ready replicas |",
                "|---|---|---|---|",
            ]
        )
        for deployment in sorted(
            management_operators, key=lambda item: item.get("name", "")
        ):
            created = parse_timestamp(deployment.get("createdAt"))
            created_text = (
                format_elapsed((created - start).total_seconds())
                if created and start
                else "-"
            )
            lines.append(
                f"| {md(deployment.get('name'))} | {created_text} | "
                f"{condition_time_text(deployment.get('conditions') or {}, 'Available', start)} | "
                f"{deployment.get('readyReplicas', 0)}/{deployment.get('desiredReplicas', 0)} |"
            )
        lines.append("")

    components = snapshot.get("controlPlaneComponents") or []
    if components:
        lines.extend(
            [
                "### Control-plane components",
                "",
                "| Component | Version | Available | Rollout complete |",
                "|---|---|---|---|",
            ]
        )
        for component in sorted(components, key=lambda item: item.get("name", "")):
            conditions = component.get("conditions") or {}
            lines.append(
                f"| {md(component.get('name'))} | {md(component.get('version'))} | "
                f"{condition_time_text(conditions, 'Available', start)} | "
                f"{condition_time_text(conditions, 'RolloutComplete', start)} |"
            )
        lines.append("")

    node_pools = snapshot.get("nodePools") or []
    if node_pools:
        lines.extend(
            [
                "### NodePools",
                "",
                "| NodePool | Version | Ready | Machines ready | Nodes healthy | Ignition reached |",
                "|---|---|---|---|---|---|",
            ]
        )
        for node_pool in sorted(node_pools, key=lambda item: item.get("name", "")):
            conditions = node_pool.get("conditions") or {}
            lines.append(
                f"| {md(node_pool.get('name'))} | {md(node_pool.get('version'))} | "
                f"{condition_time_text(conditions, 'Ready', start)} | "
                f"{condition_time_text(conditions, 'AllMachinesReady', start)} | "
                f"{condition_time_text(conditions, 'AllNodesHealthy', start)} | "
                f"{condition_time_text(conditions, 'ReachedIgnitionEndpoint', start)} |"
            )
        lines.append("")

    first_stage = next((item for item in STAGES if states.get(item[0]) != "done"), STAGES[-1])
    lines.extend(
        [
            "## Current investigation",
            "",
            f"Start with **{first_stage[0]} {first_stage[1]}**.",
            "",
        ]
    )
    for detail in blocker_details(snapshot, first_stage[0]):
        lines.append(f"- {detail}")
    lines.append("")

    if ordered_milestones:
        lines.extend(
            [
                "## Chronology",
                "",
                "| Elapsed | Delta | Stage | Event | Timing basis |",
                "|---|---|---|---|---|",
            ]
        )
        previous_elapsed: float | None = None
        for timestamp, _, stage, label, observed, elapsed_text in sorted(
            ordered_milestones, key=lambda item: item[0]
        ):
            elapsed = (timestamp - start).total_seconds() if start else None
            delta = (
                format_elapsed(elapsed - previous_elapsed)
                if elapsed is not None and previous_elapsed is not None
                else "-"
            )
            lines.append(
                f"| {elapsed_text} | {delta} | {stage} | {md(label)} | {'first observed' if observed else 'reported transition'} |"
            )
            if elapsed is not None:
                previous_elapsed = elapsed
        lines.append("")

        lines.extend(
            [
                "## Visual timeline",
                "",
                "```mermaid",
                "timeline",
                f"    title HostedCluster startup: {cluster.get('name', '')}",
            ]
        )
        for stage, label, _, _ in STAGES:
            stage_events = [
                item for item in ordered_milestones if item[2] == stage
            ]
            if not stage_events:
                continue
            lines.append(f"    section {stage} {md(label)}")
            for _, _, _, event_label, _, elapsed_text in sorted(
                stage_events, key=lambda item: item[0]
            ):
                lines.append(f"        {elapsed_text} : {md(event_label)}")
        lines.extend(["```", ""])

    diagnostics = []
    deployments = snapshot.get("deployments") or []
    unavailable = [
        deployment
        for deployment in deployments
        if deployment.get("available") is not True
    ]
    if unavailable:
        diagnostics.append("### Unavailable deployments")
        diagnostics.append("")
        diagnostics.append("| Namespace | Deployment | Ready | Available |")
        diagnostics.append("|---|---|---|---|")
        for deployment in unavailable[:15]:
            diagnostics.append(
                f"| {md(deployment.get('namespace'))} | {md(deployment.get('name'))} | "
                f"{deployment.get('readyReplicas', 0)}/{deployment.get('desiredReplicas', 0)} | "
                f"{md(condition_detail(deployment.get('conditions') or {}, 'Available'))} |"
            )
        diagnostics.append("")
    pods = snapshot.get("pods") or {}
    if pods.get("unready"):
        diagnostics.append("### Unready Pods")
        diagnostics.append("")
        diagnostics.append("| Pod | Phase | Restarts | Reason |")
        diagnostics.append("|---|---|---|---|")
        for pod in pods["unready"][:15]:
            diagnostics.append(
                f"| {md(pod.get('name'))} | {md(pod.get('phase'))} | {pod.get('restarts', 0)} | {md(pod.get('reason') or pod.get('message'))} |"
            )
        diagnostics.append("")
    warning_events = []
    for namespace_key, events in (snapshot.get("events") or {}).items():
        for event in events or []:
            warning_events.append((namespace_key, event))
    if warning_events:
        diagnostics.append("### Recent warning events")
        diagnostics.append("")
        diagnostics.append("| Namespace | Object | Reason | Message |")
        diagnostics.append("|---|---|---|---|")
        for namespace_key, event in warning_events[-15:]:
            diagnostics.append(
                f"| {md(namespace_key)} | {md(event.get('involvedObject'))} | {md(event.get('reason'))} | {md(event.get('message'), 180)} |"
            )
        diagnostics.append("")
    if diagnostics:
        lines.extend(["## Diagnostics", ""])
        lines.extend(diagnostics)

    errors = snapshot.get("collectionErrors") or []
    guest_errors = (snapshot.get("guest") or {}).get("errors") or []
    if errors or guest_errors:
        lines.extend(["## Collection errors", ""])
        for error in errors:
            lines.append(f"- Management: {error}")
        for error in guest_errors:
            lines.append(f"- Guest: {error}")
        lines.append("")

    lines.extend(
        [
            "## Raw observation",
            "",
            f"- `{output_dir / 'latest.json'}`",
            f"- `{output_dir / 'observations.jsonl'}`",
            "",
        ]
    )
    return "\n".join(lines)


def atomic_write(path: Path, content: str) -> None:
    temporary = path.with_name(f".{path.name}.tmp")
    temporary.write_text(content, encoding="utf-8")
    os.chmod(temporary, 0o600)
    temporary.replace(path)


def update_first_seen(snapshot: dict[str, Any], first_seen: dict[str, dict[str, str]]) -> None:
    collected_at = snapshot.get("collectedAt")
    if not collected_at:
        return
    for name, milestone in (snapshot.get("milestones") or {}).items():
        if milestone.get("reached") is True and name not in first_seen:
            first_seen[name] = {"timestamp": collected_at}


def restore_first_seen(observations_path: Path) -> dict[str, dict[str, str]]:
    first_seen: dict[str, dict[str, str]] = {}
    if observations_path.exists():
        with observations_path.open(encoding="utf-8") as observations_file:
            for line in observations_file:
                if line.strip():
                    update_first_seen(json.loads(line), first_seen)
    return first_seen


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Collect a HostedCluster startup timeline.",
        formatter_class=argparse.ArgumentDefaultsHelpFormatter,
    )
    parser.add_argument("--namespace", required=True, help="HostedCluster namespace")
    parser.add_argument("--name", required=True, help="HostedCluster name")
    parser.add_argument(
        "--output-dir",
        help="Directory for observations and report; defaults to a timestamped directory",
    )
    parser.add_argument(
        "--interval",
        type=float,
        default=DEFAULT_INTERVAL_SECONDS,
        help="Polling interval in seconds",
    )
    parser.add_argument(
        "--timeout",
        type=parse_duration,
        default=DEFAULT_TIMEOUT_SECONDS,
        help="Maximum collection time, for example 45m",
    )
    parser.add_argument(
        "--guest-kubeconfig",
        help="Guest kubeconfig used for CVO, ClusterOperator, and Node checks",
    )
    parser.add_argument("--kubeconfig", help="Management-cluster kubeconfig")
    parser.add_argument("--context", help="Management-cluster kubeconfig context")
    parser.add_argument("--oc", help="Path to the oc binary; defaults to OC or PATH")
    parser.add_argument(
        "--once", action="store_true", help="Collect one snapshot and exit"
    )
    parser.add_argument(
        "--append",
        action="store_true",
        help="Append to an existing observations.jsonl in the output directory",
    )
    args = parser.parse_args()

    if args.interval <= 0:
        parser.error("--interval must be greater than zero")
    if args.timeout <= 0:
        parser.error("--timeout must be greater than zero")
    if args.guest_kubeconfig and not Path(args.guest_kubeconfig).is_file():
        parser.error(f"guest kubeconfig does not exist: {args.guest_kubeconfig}")
    if args.kubeconfig and not Path(args.kubeconfig).is_file():
        parser.error(f"management kubeconfig does not exist: {args.kubeconfig}")

    oc = args.oc or os.environ.get("OC") or shutil.which("oc")
    if not oc:
        parser.error("oc was not found; pass --oc or add oc to PATH")

    if args.output_dir:
        output_dir = Path(args.output_dir)
    else:
        stamp = utc_now().strftime("%Y%m%dT%H%M%SZ")
        output_dir = Path("startup-traces") / f"{args.name}-{stamp}"
    output_dir.mkdir(parents=True, exist_ok=True)
    observations_path = output_dir / "observations.jsonl"
    latest_path = output_dir / "latest.json"
    report_path = output_dir / "report.md"
    if observations_path.exists() and observations_path.stat().st_size > 0 and not args.append:
        parser.error(
            f"{observations_path} already has observations; use --append or a new --output-dir"
        )

    management_runner = OcRunner(oc, args.kubeconfig, args.context)
    guest_runner = (
        OcRunner(oc, args.guest_kubeconfig, None) if args.guest_kubeconfig else None
    )
    first_seen = restore_first_seen(observations_path) if args.append else {}
    deadline = time.monotonic() + args.timeout
    last_snapshot: dict[str, Any] | None = None
    mode = "a" if args.append else "w"
    open_flags = os.O_APPEND if args.append else os.O_TRUNC
    descriptor = os.open(
        observations_path, os.O_WRONLY | os.O_CREAT | open_flags, 0o600
    )
    try:
        with os.fdopen(descriptor, mode, encoding="utf-8") as observations_file:
            while True:
                snapshot = collect_snapshot(
                    management_runner, guest_runner, args.namespace, args.name
                )
                last_snapshot = snapshot
                update_first_seen(snapshot, first_seen)
                observations_file.write(json.dumps(snapshot, sort_keys=True) + "\n")
                observations_file.flush()
                os.fsync(observations_file.fileno())
                atomic_write(
                    latest_path,
                    json.dumps(snapshot, indent=2, sort_keys=True) + "\n",
                )
                report = render_report(snapshot, first_seen, output_dir)
                atomic_write(report_path, report)

                full = (snapshot.get("milestones") or {}).get("fully_complete") or {}
                if full.get("reached") is True:
                    print(f"Observed complete startup; report: {report_path}")
                    break
                if args.once:
                    print(f"Collected one snapshot; report: {report_path}")
                    break
                if time.monotonic() >= deadline:
                    print(f"Collection timeout reached; report: {report_path}")
                    break
                remaining = deadline - time.monotonic()
                time.sleep(min(args.interval, max(0.0, remaining)))
    except KeyboardInterrupt:
        if last_snapshot:
            report = render_report(last_snapshot, first_seen, output_dir)
            atomic_write(report_path, report)
        print(f"Interrupted; report: {report_path}", file=sys.stderr)
        return 130

    if last_snapshot:
        full = (last_snapshot.get("milestones") or {}).get("fully_complete") or {}
        if full.get("reached") is None:
            print(
                "P9 was not evaluated because no guest kubeconfig was supplied.",
                file=sys.stderr,
            )
        elif full.get("reached") is not True:
            print(
                f"Startup is not complete; current stage: "
                f"{next((stage for stage, _, _, _ in STAGES if stage_states(last_snapshot.get('milestones') or {}).get(stage) != 'done'), 'P9')}",
                file=sys.stderr,
            )
    return 0


if __name__ == "__main__":
    sys.exit(main())
