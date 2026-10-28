#!/usr/bin/env python3
"""Observe AWS HostedCluster teardown without modifying cluster or cloud resources.

Writes normalized object metadata/status, AWS resource identities/states, and
polling-based milestone times. Never queries Kubernetes Secret data.
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import time
from concurrent.futures import ThreadPoolExecutor
from datetime import UTC, datetime
from pathlib import Path


def now():
    return datetime.now(UTC).isoformat(timespec="milliseconds")


def query(command, absent_errors=()):
    try:
        result = subprocess.run(
            command, capture_output=True, text=True, timeout=40, check=False
        )
    except subprocess.TimeoutExpired:
        return {"ok": False, "error": "command timed out"}
    if result.returncode:
        if any(code in result.stderr for code in absent_errors):
            return {"ok": True, "data": []}
        return {"ok": False, "error": result.stderr.strip()[:1200]}
    try:
        data = json.loads(result.stdout) if result.stdout.strip() else []
    except ValueError:
        return {"ok": False, "error": "invalid JSON output"}
    return {"ok": True, "data": data}


def objects(data):
    if isinstance(data, list):
        return data
    return data.get("items", [data] if data else [])


def normalize(data):
    result = []
    for obj in objects(data):
        meta, status = obj.get("metadata", {}), obj.get("status", {})
        result.append(
            {
                "kind": obj.get("kind"),
                "name": meta.get("name"),
                "uid": meta.get("uid"),
                "createdAt": meta.get("creationTimestamp"),
                "deletionTimestamp": meta.get("deletionTimestamp"),
                "deletionGracePeriodSeconds": meta.get("deletionGracePeriodSeconds"),
                "terminationGracePeriodSeconds": obj.get("spec", {}).get(
                    "terminationGracePeriodSeconds"
                ),
                "finalizers": meta.get("finalizers", []),
                "specFinalizers": obj.get("spec", {}).get("finalizers", []),
                "phase": status.get("phase"),
                "conditions": status.get("conditions", []),
                "replicas": status.get("replicas"),
                "readyReplicas": status.get("readyReplicas"),
                "nodeRef": status.get("nodeRef"),
                "providerID": obj.get("spec", {}).get("providerID"),
                "loadBalancerIngress": status.get("loadBalancer", {}).get(
                    "ingress", []
                ),
            }
        )
    return result


def is_empty(item):
    # Query failures are unknown, never evidence of successful cleanup.
    return item.get("ok") is True and item.get("data") == []


def evaluate(snapshot, command):
    kube, cloud = snapshot["kubernetes"], snapshot["aws"]
    gates = {f"{key}_removed": is_empty(value) for key, value in kube.items()}
    gates.update(
        {f"aws_{key}_removed": is_empty(value) for key, value in cloud.items()}
    )
    gates["destroy_command_succeeded"] = command.get("exitCode") == 0
    gates["fully_complete"] = all(gates.values())
    return gates


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in [
        "kubeconfig",
        "namespace",
        "name",
        "uid",
        "infra-id",
        "vpc-id",
        "oidc-bucket",
        "oidc-provider-arn",
        "output-dir",
        "command-state-file",
    ]:
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--region", default="us-east-1")
    parser.add_argument("--interval", type=float, default=10)
    parser.add_argument("--timeout", type=float, default=3600)
    parser.add_argument("--once", action="store_true")
    args = parser.parse_args()
    if args.interval <= 0 or args.timeout <= 0:
        parser.error("--interval and --timeout must be greater than zero")
    os.umask(0o077)
    output = Path(args.output_dir)
    output.mkdir(parents=True, exist_ok=True, mode=0o700)
    observations_path = output / "observations.jsonl"
    if observations_path.exists() and observations_path.stat().st_size:
        parser.error("output already contains observations; choose a new directory")
    command_path = Path(args.command_state_file)
    hcp_namespace = f"{args.namespace}-{args.name}"
    oc = ["oc", "--kubeconfig", args.kubeconfig, "--request-timeout=8s"]
    aws = [
        "aws",
        "--region",
        args.region,
        "--output",
        "json",
        "--no-cli-pager",
        "--cli-connect-timeout",
        "5",
        "--cli-read-timeout",
        "15",
    ]
    vpc_filter = ["--filters", f"Name=vpc-id,Values={args.vpc_id}"]
    zone_result = query(
        aws
        + [
            "route53",
            "list-hosted-zones-by-vpc",
            "--vpc-id",
            args.vpc_id,
            "--vpc-region",
            args.region,
        ]
    )
    if not zone_result["ok"]:
        raise SystemExit(
            "Cannot inventory private hosted zones before teardown: "
            + zone_result["error"]
        )
    zone_ids = [
        z["HostedZoneId"] for z in zone_result["data"].get("HostedZoneSummaries", [])
    ]
    known_eips, known_volumes, known_igws = set(), set(), set()
    known_lb_dns, known_lb_arns, known_target_groups = set(), set(), set()
    first = {}
    started = time.monotonic()
    count = 0
    while True:
        collected = now()
        tasks = {}
        resources = {
            "hostedcluster": ("hostedclusters", args.namespace, args.name),
            "nodepools": ("nodepools", args.namespace, None),
            "hostedcontrolplanes": ("hostedcontrolplanes", hcp_namespace, None),
            "machines": ("machines.cluster.x-k8s.io", hcp_namespace, None),
            "awsmachines": (
                "awsmachines.infrastructure.cluster.x-k8s.io",
                hcp_namespace,
                None,
            ),
            "capi_clusters": ("clusters.cluster.x-k8s.io", hcp_namespace, None),
            "awsclusters": (
                "awsclusters.infrastructure.cluster.x-k8s.io",
                hcp_namespace,
                None,
            ),
            "pods": ("pods", hcp_namespace, None),
            "services": ("services", hcp_namespace, None),
            "persistentvolumeclaims": ("persistentvolumeclaims", hcp_namespace, None),
            "controlplanecomponents": ("controlplanecomponents", hcp_namespace, None),
            "hcp_namespace": ("namespaces", None, hcp_namespace),
        }
        for key, (resource, ns, name) in resources.items():
            cmd = oc + ["get", resource, "--ignore-not-found", "-o", "json"]
            if ns:
                cmd += ["--namespace", ns]
            if name:
                cmd += [name]
            tasks[("kubernetes", key)] = (cmd, ())
        cloud_tasks = {
            "instances": (
                [
                    "ec2",
                    "describe-instances",
                    *vpc_filter,
                    "--query",
                    "Reservations[].Instances[].{id:InstanceId,state:State.Name,volumes:BlockDeviceMappings[].Ebs.VolumeId}",
                ],
                (),
            ),
            "nat_gateways": (
                [
                    "ec2",
                    "describe-nat-gateways",
                    "--filter",
                    f"Name=vpc-id,Values={args.vpc_id}",
                    "--query",
                    "NatGateways[].{id:NatGatewayId,state:State,eips:NatGatewayAddresses[].AllocationId}",
                ],
                (),
            ),
            "network_interfaces": (
                [
                    "ec2",
                    "describe-network-interfaces",
                    *vpc_filter,
                    "--query",
                    "NetworkInterfaces[].{id:NetworkInterfaceId,status:Status,description:Description}",
                ],
                (),
            ),
            "vpc_endpoints": (
                [
                    "ec2",
                    "describe-vpc-endpoints",
                    *vpc_filter,
                    "--query",
                    "VpcEndpoints[].{id:VpcEndpointId,state:State}",
                ],
                (),
            ),
            "subnets": (
                [
                    "ec2",
                    "describe-subnets",
                    *vpc_filter,
                    "--query",
                    "Subnets[].SubnetId",
                ],
                (),
            ),
            "security_groups": (
                [
                    "ec2",
                    "describe-security-groups",
                    *vpc_filter,
                    "--query",
                    "SecurityGroups[].GroupId",
                ],
                (),
            ),
            "route_tables": (
                [
                    "ec2",
                    "describe-route-tables",
                    *vpc_filter,
                    "--query",
                    "RouteTables[].RouteTableId",
                ],
                (),
            ),
            "attached_internet_gateways": (
                [
                    "ec2",
                    "describe-internet-gateways",
                    "--filters",
                    f"Name=attachment.vpc-id,Values={args.vpc_id}",
                    "--query",
                    "InternetGateways[].InternetGatewayId",
                ],
                (),
            ),
            "dhcp_options": (
                [
                    "ec2",
                    "describe-dhcp-options",
                    "--filters",
                    f"Name=tag:kubernetes.io/cluster/{args.infra_id},Values=owned",
                    "--query",
                    "DhcpOptions[].DhcpOptionsId",
                ],
                (),
            ),
            "vpc": (
                ["ec2", "describe-vpcs", *vpc_filter, "--query", "Vpcs[].VpcId"],
                (),
            ),
            "load_balancers": (
                [
                    "elbv2",
                    "describe-load-balancers",
                    "--query",
                    "LoadBalancers[].{id:LoadBalancerArn,state:State.Code,dns:DNSName,vpc:VpcId}",
                ],
                (),
            ),
            "classic_load_balancers": (
                [
                    "elb",
                    "describe-load-balancers",
                    "--query",
                    "LoadBalancerDescriptions[].{id:LoadBalancerName,dns:DNSName,vpc:VPCId}",
                ],
                (),
            ),
            "target_groups": (
                [
                    "elbv2",
                    "describe-target-groups",
                    "--query",
                    "TargetGroups[].{id:TargetGroupArn,vpc:VpcId,lbs:LoadBalancerArns}",
                ],
                (),
            ),
            "iam_roles": (
                [
                    "iam",
                    "list-roles",
                    "--query",
                    f"Roles[?starts_with(RoleName, '{args.infra_id}-')].RoleName",
                ],
                (),
            ),
            "instance_profiles": (
                [
                    "iam",
                    "list-instance-profiles",
                    "--query",
                    f"InstanceProfiles[?starts_with(InstanceProfileName, '{args.infra_id}-')].InstanceProfileName",
                ],
                (),
            ),
            "oidc_provider": (
                [
                    "iam",
                    "get-open-id-connect-provider",
                    "--open-id-connect-provider-arn",
                    args.oidc_provider_arn,
                ],
                ("NoSuchEntity",),
            ),
            "oidc_documents": (
                [
                    "s3api",
                    "list-objects-v2",
                    "--bucket",
                    args.oidc_bucket,
                    "--prefix",
                    args.infra_id + "/",
                    "--query",
                    "Contents[].Key",
                ],
                (),
            ),
        }
        for key, (cmd, absent) in cloud_tasks.items():
            tasks[("aws", key)] = (aws + cmd, absent)
        for ident in sorted(known_eips):
            tasks[("eips", ident)] = (
                aws
                + [
                    "ec2",
                    "describe-addresses",
                    "--allocation-ids",
                    ident,
                    "--query",
                    "Addresses[].AllocationId",
                ],
                ("InvalidAllocationID.NotFound",),
            )
        for ident in sorted(known_volumes):
            tasks[("volumes", ident)] = (
                aws
                + [
                    "ec2",
                    "describe-volumes",
                    "--volume-ids",
                    ident,
                    "--query",
                    "Volumes[].{id:VolumeId,state:State}",
                ],
                ("InvalidVolume.NotFound",),
            )
        for ident in sorted(known_igws):
            tasks[("igws", ident)] = (
                aws
                + [
                    "ec2",
                    "describe-internet-gateways",
                    "--internet-gateway-ids",
                    ident,
                    "--query",
                    "InternetGateways[].InternetGatewayId",
                ],
                ("InvalidInternetGatewayID.NotFound",),
            )
        for ident in zone_ids:
            tasks[("zones", ident)] = (
                aws + ["route53", "get-hosted-zone", "--id", ident],
                ("NoSuchHostedZone",),
            )
        with ThreadPoolExecutor(max_workers=8) as executor:
            pending = {
                key: executor.submit(query, *value) for key, value in tasks.items()
            }
            results = {key: future.result() for key, future in pending.items()}
        snapshot = {
            "schemaVersion": 1,
            "collectedAt": collected,
            "collectionFinishedAt": now(),
            "target": {
                "name": args.name,
                "namespace": args.namespace,
                "uid": args.uid,
                "infraID": args.infra_id,
                "vpcID": args.vpc_id,
            },
            "kubernetes": {},
            "aws": {},
        }
        for (group, key), item in results.items():
            if group in ["kubernetes", "aws"]:
                snapshot[group][key] = item
                if group == "kubernetes" and item["ok"]:
                    item["data"] = normalize(item["data"])
        hcs = snapshot["kubernetes"]["hostedcluster"]
        if hcs["ok"] and any(hc["uid"] != args.uid for hc in hcs["data"]):
            raise SystemExit(
                "HostedCluster UID changed; refusing to mix cluster incarnations"
            )
        services = snapshot["kubernetes"]["services"]
        if services["ok"]:
            known_lb_dns.update(
                ingress["hostname"].removeprefix("dualstack.")
                for service in services["data"]
                for ingress in service["loadBalancerIngress"]
                if ingress.get("hostname")
            )
        for key in ["load_balancers", "classic_load_balancers"]:
            item = snapshot["aws"][key]
            if item["ok"]:
                item["data"] = [
                    lb
                    for lb in item["data"]
                    if lb.get("vpc") == args.vpc_id
                    or lb.get("dns", "").removeprefix("dualstack.") in known_lb_dns
                ]
                if key == "load_balancers":
                    known_lb_arns.update(lb["id"] for lb in item["data"])
        target_groups = snapshot["aws"]["target_groups"]
        if target_groups["ok"]:
            target_groups["data"] = [
                group
                for group in target_groups["data"]
                if group["id"] in known_target_groups
                or group.get("vpc") == args.vpc_id
                or any(lb in known_lb_arns for lb in group.get("lbs", []))
            ]
            known_target_groups.update(group["id"] for group in target_groups["data"])
        igws = snapshot["aws"]["attached_internet_gateways"]
        if igws["ok"]:
            known_igws.update(igws["data"])
        for key in ["instances", "nat_gateways"]:
            item = snapshot["aws"][key]
            if item["ok"]:
                for obj in item["data"]:
                    known_volumes.update(obj.get("volumes", []))
                    known_eips.update(obj.get("eips", []))
                terminal = "terminated" if key == "instances" else "deleted"
                item["terminalResources"] = [
                    obj for obj in item["data"] if obj.get("state") == terminal
                ]
                item["data"] = [
                    obj for obj in item["data"] if obj.get("state") != terminal
                ]
        # S3's empty Contents query returns JSON null, which is successful absence.
        if (
            snapshot["aws"]["oidc_documents"].get("ok")
            and snapshot["aws"]["oidc_documents"].get("data") is None
        ):
            snapshot["aws"]["oidc_documents"]["data"] = []
        for group, label, identities in [
            ("eips", "elastic_ips", known_eips),
            ("volumes", "worker_volumes", known_volumes),
            ("zones", "private_zones", zone_ids),
            ("igws", "internet_gateways", known_igws),
        ]:
            values = [
                results[(group, ident)]
                for ident in identities
                if (group, ident) in results
            ]
            # Newly discovered IDs must be queried before absence can be concluded.
            ok = len(values) == len(identities) and all(item["ok"] for item in values)
            snapshot["aws"][label] = {
                "ok": ok,
                "data": [
                    obj
                    for item in values
                    if item["ok"]
                    for obj in objects(item["data"])
                ],
                "trackedIDs": sorted(identities),
            }
            if not ok:
                snapshot["aws"][label]["errors"] = [
                    item.get("error", "pending initial inventory query")
                    for item in values
                    if not item["ok"]
                ] or ["pending initial inventory query"]
        command = json.loads(command_path.read_text()) if command_path.exists() else {}
        snapshot["command"] = command
        snapshot["milestones"] = evaluate(snapshot, command)
        if command.get("startedAt"):
            for key, reached in snapshot["milestones"].items():
                if reached and key not in first:
                    first[key] = {
                        "timestamp": snapshot["collectionFinishedAt"],
                        "seconds": round(
                            (
                                datetime.fromisoformat(snapshot["collectionFinishedAt"])
                                - datetime.fromisoformat(command["startedAt"])
                            ).total_seconds(),
                            3,
                        ),
                        "timingBasis": "first polling observation",
                    }
            if hcs["ok"] and hcs["data"] and hcs["data"][0].get("deletionTimestamp"):
                stamp = hcs["data"][0]["deletionTimestamp"]
                first.setdefault(
                    "hostedcluster_deletion_requested",
                    {
                        "timestamp": stamp,
                        "seconds": round(
                            (
                                datetime.fromisoformat(stamp)
                                - datetime.fromisoformat(command["startedAt"])
                            ).total_seconds(),
                            3,
                        ),
                        "timingBasis": "API deletion timestamp",
                    },
                )
        snapshot["firstMilestones"] = first
        with (output / "observations.jsonl").open("a") as stream:
            stream.write(json.dumps(snapshot, sort_keys=True) + "\n")
        (output / "latest.json").write_text(json.dumps(snapshot, indent=2) + "\n")
        count += 1
        summary = {
            "target": snapshot["target"],
            "observations": count,
            "firstMilestones": first,
            "lastCollectedAt": snapshot["collectionFinishedAt"],
            "fullyComplete": snapshot["milestones"]["fully_complete"],
            "command": command,
        }
        (output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
        (output / "ready.json").write_text(
            json.dumps(
                {
                    "collectedAt": snapshot["collectionFinishedAt"],
                    "allQueriesSucceeded": all(
                        item["ok"]
                        for group in ["kubernetes", "aws"]
                        for item in snapshot[group].values()
                    ),
                }
            )
            + "\n"
        )
        print(
            snapshot["collectionFinishedAt"],
            "remaining",
            [key for key, value in snapshot["milestones"].items() if not value],
            flush=True,
        )
        if snapshot["milestones"]["fully_complete"]:
            return 0
        if args.once:
            return 0
        if time.monotonic() - started >= args.timeout:
            return 2
        time.sleep(args.interval)


if __name__ == "__main__":
    raise SystemExit(main())
