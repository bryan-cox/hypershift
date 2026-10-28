---
title: Clean AWS Startup Observation (5.0.0-rc.7)
---

# Clean AWS Startup Observation (5.0.0-rc.7)

This is clean cycle 1 of the [startup roadmap](cluster-startup-roadmap.md)
validation. The dataset is `run-02`, created on 28 October 2026. The initial
trial was excluded and its data removed; its timings do not contribute to
this observation. The requested HO was healthy before creation, and no
manual credential repair, HO rollout, or bootstrap intervention occurred
during this startup. The management cluster and its image caches were warm.

## Configuration

| Setting | Value |
|---------|-------|
| Release | `quay.io/openshift-release-dev/ocp-release:5.0.0-rc.7-multi` |
| HO image | `quay.io/acm-d/rhtap-hypershift-operator@sha256:d7eb652dbbfac8ae9866b413c7125c86961a5cfb46396eb31774e0b3e171a3a5` |
| Region / worker zone | `us-east-1` / `us-east-1a` |
| Workers | Two `m5.large` instances, one NodePool |
| Availability | Controller and infrastructure `SingleReplica` |
| Endpoint access | `Public` |
| HC creation (`t=0`) | `2026-10-28T20:09:30Z` |

A failed CLI attempt hit a NAT quota before this HC existed; its resources
were cleaned before retrying. That event is outside the startup clock.
The successful script began approximately 18 seconds before HC creation.

## Startup milestones

Times start at HC creation. `~` marks the first polling observation; other
times use resource timestamps. The collector ran continuously with a
ten-second polling delay plus query time and captured 83 observations.

| Stage | Milestone | Elapsed |
|-------|-----------|---------|
| P0 | HostedCluster created | 0s |
| P0 | Validation passed | 0s |
| P1 | HostedControlPlane created | 0s |
| P1 | Platform infrastructure object created | 0s |
| P1 | CAPI Cluster created | 0s |
| P1 | Management operators deployed | 4s |
| P1 | Management operators available | 1m 25s |
| P2 | HCP InfrastructureReady | 23s |
| P3 | etcd available | 52s |
| P3 | featuregate-generator ready | 33s |
| P4 | Kube API server available | 1m 20s |
| P4 | HostedCluster kubeconfig published | ~58s |
| P5 | HostedControlPlane available | 4m 32s |
| P5 | HostedCluster available | 4m 32s |
| P5 | Control-plane rollout complete | 5m 32s |
| P6 | NodePool created | 0s |
| P6 | Node bootstrap payload generated | 3m 13s |
| P6 | MachineDeployment or MachineSet created | 5m 35s |
| P7 | First CAPI Machine created | 5m 39s |
| P7 | Ignition endpoint reached | 6m 04s |
| P8 | All CAPI Machines ready | 9m 39s |
| P8 | All CAPI Nodes healthy | 9m 39s |
| P8 | NodePool ready | 9m 40s |
| P9 | HostedCluster version Completed | 16m 59s |
| P9 | Guest ClusterVersion complete | 16m 59s |
| P9 | Guest ClusterOperators converged | 16m 34s |
| P9 | Full completion checklist | ~17m 02s |

## Observed gates

The release CPO revision is `b9d6ae1c63453c012e889525eb924b3a07bac26b`.
Its AWS identity check in `control-plane-operator/controllers/healthcheck/aws.go`
sets `ValidAWSIdentityProvider=Unknown` and returns no error when KAS is not
available yet. `healthcheck_controller.go` then schedules the next check five
minutes later, using its successful-check interval. This explains the
approximately five-minute identity-validation wait in these observations;
it is version-specific behavior and does not indicate a credential repair.

`ValidOIDCConfiguration=True` was present from creation. Bootstrap payload
generation at +3m13s preceded worker security-group readiness. CPO AWS
identity-provider validation passed at +5m34s; the default worker security
group and MachineDeployment followed at +5m35s. Machines were created at
+5m39s, ignition was reached at +6m04s, and the NodePool became ready at +9m40s.

Monitoring was the last guest operator to converge. Its initial reconciliation
reported missing `cluster-monitoring-view` and `monitoring-alertmanager-edit`
RBAC dependencies. Prometheus remained absent during a five-minute availability
validation. The next reconciliation created `Prometheus/k8s` at +16m14s;
monitoring converged at +16m34s without manual intervention. Guest CVO and HC
version completion followed at +16m59s, and the full checklist was first
observed at approximately +17m02s.

## Control-plane components

First successful transitions can be followed by later rollouts. Creation to
availability measures API-object bootstrap, including dependency waits, rather
than container startup time.

| Component | First available | First rollout complete | Creation to available |
|-----------|-----------------|------------------------|-----------------------|
| `aws-cloud-controller-manager` | 1m 35s | 1m 35s | 1m 11s |
| `capi-provider` | 1m 26s | 1m 31s | 1m 22s |
| `catalog-operator` | 3m 31s | 3m 31s | 3m 06s |
| `certified-operators-catalog` | 3m 31s | 3m 31s | 3m 06s |
| `cloud-credential-operator` | 3m 57s | 3m 57s | 3m 33s |
| `cluster-api` | 7s | 7s | 3s |
| `cluster-autoscaler` | 1m 23s | 1m 23s | 30s |
| `cluster-image-registry-operator` | 3m 31s | 3m 31s | 3m 07s |
| `cluster-network-operator` | 3m 57s | 5m 30s | 3m 33s |
| `cluster-node-tuning-operator` | 2m 17s | 2m 17s | 1m 53s |
| `cluster-policy-controller` | 2m 17s | 2m 17s | 1m 53s |
| `cluster-storage-operator` | 2m 37s | 3m 57s | 2m 13s |
| `cluster-version-operator` | 2m 27s | 2m 27s | 2m 03s |
| `community-operators-catalog` | 3m 31s | 3m 31s | 3m 06s |
| `control-plane-operator` | 5s | 5s | 4s |
| `control-plane-pki-operator` | 1m 26s | 1m 26s | 1m 03s |
| `csi-snapshot-controller-operator` | 2m 38s | 2m 48s | 2m 14s |
| `dns-operator` | 3m 30s | 3m 30s | 3m 06s |
| `etcd` | 51s | 51s | 27s |
| `featuregate-generator` | 26s | 33s | 2s |
| `hosted-cluster-config-operator` | 2m 47s | 2m 47s | 2m 23s |
| `ignition-server` | 28s | 28s | 3s |
| `ignition-server-proxy` | 30s | 30s | 5s |
| `ingress-operator` | 3m 57s | 3m 57s | 3m 33s |
| `konnectivity-agent` | 1m 27s | 1m 27s | 1m 03s |
| `kube-apiserver` | 1m 18s | 1m 18s | 54s |
| `kube-controller-manager` | 1m 25s | 1m 29s | 1m 01s |
| `kube-scheduler` | 1m 18s | 1m 23s | 54s |
| `kube-storage-version-migrator` | 1m 29s | 1m 29s | 1m 04s |
| `machine-approver` | 1m 26s | 1m 26s | 33s |
| `oauth-openshift` | 1m 50s | 2m 20s | 1m 26s |
| `olm-collect-profiles` | 1m 20s | 1m 20s | 55s |
| `olm-operator` | 3m 31s | 3m 31s | 3m 06s |
| `openshift-apiserver` | 1m 23s | 1m 44s | 59s |
| `openshift-controller-manager` | 2m 18s | 2m 18s | 1m 54s |
| `openshift-oauth-apiserver` | 1m 50s | 2m 20s | 1m 26s |
| `openshift-route-controller-manager` | 1m 50s | 2m 19s | 1m 26s |
| `packageserver` | 3m 31s | 3m 31s | 3m 06s |
| `redhat-operators-catalog` | 4m 32s | 4m 32s | 4m 07s |

## Guest operators

First convergence requires explicit `Available=True`, `Progressing=False`,
and `Degraded=False`. Full completion requires all operators to converge in
the same collection cycle, along with control-plane, worker, and version checks.

| Operator | First converged |
|----------|-----------------|
| `cloud-credential` | 3m 48s |
| `console` | 11m 55s |
| `csi-snapshot-controller` | 2m 47s |
| `dns` | 11m 06s |
| `image-registry` | 10m 58s |
| `ingress` | 11m 53s |
| `insights` | 9m 58s |
| `kube-apiserver` | 2m 55s |
| `kube-controller-manager` | 2m 55s |
| `kube-scheduler` | 2m 55s |
| `kube-storage-version-migrator` | 10m 21s |
| `monitoring` | 16m 34s |
| `network` | 10m 52s |
| `node-tuning` | 9m 18s |
| `openshift-apiserver` | 2m 55s |
| `openshift-controller-manager` | 2m 55s |
| `openshift-samples` | 11m 33s |
| `operator-lifecycle-manager` | 3m 25s |
| `operator-lifecycle-manager-catalog` | 3m 48s |
| `operator-lifecycle-manager-packageserver` | 2m 55s |
| `service-ca` | 10m 23s |
| `storage` | 9m 50s |

## Evidence

Normalized startup observations and logs are under
`aws/startup-roadmap-aws-20261028/artifacts/run-02/` in the private scripts
workspace. Credentials and kubeconfigs are ignored by git. These timings are
an observation from one environment, not a performance distribution or SLO.

The [clean lifecycle comparison](cluster-startup-aws-repeat.md) pairs this
startup with its measured teardown and a second complete startup/teardown.
