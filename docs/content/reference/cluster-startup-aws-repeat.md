---
title: AWS Startup Repeat Run (5.0.0-rc.7)
---

# AWS Startup Repeat Run (5.0.0-rc.7)

This compares the [first AWS observation](cluster-startup-aws-observation.md)
with a second startup on the same management cluster, using the same release,
HO digest, two `m5.large` workers, `us-east-1a`, public endpoints, and
`SingleReplica` availability policies.

The first HostedCluster and its AWS infrastructure were destroyed before the
repeat. The existing healthy HO deployment stayed in place. The second cluster
was created using the same copied AWS creation script. No credential repair,
HO rollout, or manual bootstrap intervention occurred after its creation.
These are two observations, not a statistical performance baseline. The
management cluster and image caches were warm for the repeat.

| Setting | First run | Repeat run |
|---------|-----------|------------|
| HC creation (`t=0`) | `2026-10-28T19:15:46+00:00` | `2026-10-28T20:09:30+00:00` |
| Infrastructure ID | `startup-roadmap-aws-snz2q` | `startup-roadmap-aws-dpqbm` |
| Collector observations | 86 | 83 |
| Intervention after HC creation | HO rollout and OIDC credential-format repair | None |

## Before the second HostedCluster existed

The first cluster's normal deletion finished at `20:05:56Z`, approximately
seven minutes after teardown began. Checks confirmed its HostedCluster, VPC,
cluster IAM roles, OIDC provider, and OIDC documents were removed. The first
run's evidence was retained separately and verified against a SHA-256 manifest.

The first recreation attempt failed at `20:07:16Z` with
`NatGatewayLimitExceeded` in `us-east-1a`, before a HostedCluster API object
existed. The failed attempt had allocated a VPC, subnets, internet gateway,
and Elastic IP. Those resources were removed using the failed attempt's
infrastructure ID before retrying. The retry used the same zone and settings;
no quota change was made.

The successful create script started at `20:09:11.810Z`; cloud provisioning
started at `20:09:13Z`, and HostedCluster creation followed at `20:09:30Z`.
Thus its successful command-to-HC interval was approximately 18 seconds
(17 seconds from cloud provisioning). Teardown, the failed attempt, and its
cleanup are separate from the HC startup clock below.

## Credential repair belongs to the first run

The OIDC credential-format repair occurred **only during run 1**. Run 2 reused
the corrected HO configuration. Its `ValidOIDCConfiguration=True` transition
was at HC creation and no later false state was captured. AWS identity-provider
validation during the second bootstrap was a separate readiness gate; it did
not require a new credential repair.

## Milestone comparison

Each time is measured from that run's HostedCluster creation. `~` marks first
polling observation rather than a resource transition timestamp. The difference
is repeat minus first run; negative values mean an earlier milestone.

| Stage | Milestone | First run | Repeat run | Difference |
|-------|-----------|-----------|------------|------------|
| P0 | HostedCluster created | 0s | 0s | +0s |
| P0 | Validation passed | 1s | 0s | −1s |
| P1 | HostedControlPlane created | 2s | 0s | −2s |
| P1 | Platform infrastructure object created | 2s | 0s | −2s |
| P1 | CAPI Cluster created | 2s | 0s | −2s |
| P1 | Management operators deployed | 3s | 4s | +1s |
| P1 | Management operators available | 2m 19s | 1m 25s | −54s |
| P2 | HCP InfrastructureReady | 28s | 23s | −5s |
| P3 | etcd available | 1m 15s | 52s | −23s |
| P3 | featuregate-generator ready | 33s | 33s | +0s |
| P4 | Kube API server available | 1m 40s | 1m 20s | −20s |
| P4 | HostedCluster kubeconfig published | ~2m 31s | ~58s | −~1m 33s |
| P5 | HostedControlPlane available | 5m 04s | 4m 32s | −32s |
| P5 | HostedCluster available | 6m 37s | 4m 32s | −2m 05s |
| P5 | Control-plane rollout complete | 5m 52s | 5m 32s | −20s |
| P6 | NodePool created | 0s | 0s | +0s |
| P6 | Node bootstrap payload generated | 4m 51s | 3m 13s | −1m 38s |
| P6 | MachineDeployment or MachineSet created | 8m 01s | 5m 35s | −2m 26s |
| P7 | First CAPI Machine created | 8m 05s | 5m 39s | −2m 26s |
| P7 | Ignition endpoint reached | 8m 29s | 6m 04s | −2m 25s |
| P8 | All CAPI Machines ready | 12m 01s | 9m 39s | −2m 22s |
| P8 | All CAPI Nodes healthy | 12m 01s | 9m 39s | −2m 22s |
| P8 | NodePool ready | 12m 01s | 9m 40s | −2m 21s |
| P9 | HostedCluster version Completed | 19m 19s | 16m 59s | −2m 20s |
| P9 | Guest ClusterVersion complete | 19m 19s | 16m 59s | −2m 20s |
| P9 | Guest ClusterOperators converged | 19m 02s | 16m 34s | −2m 28s |
| P9 | Full completion checklist | ~19m 24s | ~17m 02s | −~2m 22s |

The collector ran continuously before both second-run create attempts and
was not restarted. The new cluster's guest kubeconfig was loaded atomically at
`20:10:23.813Z` (approximately 54 seconds after HC creation). Initial guest API
queries encountered the new load balancer's DNS lookup failures and then
connection timeouts; later queries succeeded without intervention. These
errors remain in the saved observations rather than being counted as successful
guest readiness.

## Interpretation

The repeat reached full completion approximately **2m22s earlier**. HCP
availability was 32 seconds earlier and NodePool readiness was 2m21s earlier.
The interval from NodePool readiness to full completion was nearly unchanged:
approximately 7m23s in run 1 and 7m22s in run 2. These differences include the
first run's interventions and the second run's warm management environment;
they do not isolate the effect of any single change.

### A generated payload can still wait for the worker security group

Run 2 generated its NodePool bootstrap payload at **+3m13s** but did not create
Machines immediately. The NodePool reported
`AWSSecurityGroupAvailable=False` with reason `DefaultSGNotReady`, while the
`AWSCluster` already reported ready. The subsequent gates were:

| Gate | Second-run transition |
|------|-----------------------|
| HCP `ValidAWSIdentityProvider=True` | +5m34s |
| HCP `AWSDefaultSecurityGroupCreated=True` | +5m35s |
| MachineDeployment created | +5m35s |
| First Machine created | +5m39s |

The CPO creates the default worker security group after AWS identity-provider
validation and publishes it through HCP status; the HO copies that platform
status to the HC. The NodePool waits for
`HostedCluster.status.platform.aws.defaultWorkerSecurityGroupID`. This gives
the roadmap a concrete next check when the payload is ready but no Machines
exist. This readiness validation recovered through normal reconciliation,
without an OIDC credential repair in run 2.

### Monitoring reproduced the same final wait

Both workers were ready by **+9m40s**. Monitoring was the last guest operator to
converge. At `20:20:32Z` and `20:20:33Z`, its initial reconciliation reported
missing `cluster-monitoring-view` and `monitoring-alertmanager-edit` RBAC
dependencies. At that point, no Prometheus custom resource existed even though
several other monitoring pods were healthy.

The Prometheus task reported its failed availability validation at
`20:25:33Z`, five minutes after the RBAC error. The next reconciliation began
its Prometheus task at `20:25:37Z` and created `Prometheus/k8s` at `20:25:44Z`
(**+16m14s**). Monitoring converged at **+16m34s** without manual intervention.
The guest CVO and HC version history reported completion at **+16m59s**;
the full checklist was first observed at **~17m02s**.

This repeat establishes that the monitoring wait can occur independently of
the first run's OIDC repair. It does not establish how often it occurs across
other releases or environments. Inspect CMO task errors and missing operands,
as well as pod readiness, when investigating this state.

## Control-plane component comparison

These are first captured successful transitions, not the end of every later
rollout. A component can become available and roll again as configuration
arrives. HCP aggregation and the final simultaneous checklist are separate.

| Component | First available: run 1 | First available: run 2 | First rollout: run 1 | First rollout: run 2 |
|-----------|------------------------|------------------------|----------------------|----------------------|
| `aws-cloud-controller-manager` | 1m 50s | 1m 35s | 1m 50s | 1m 35s |
| `capi-provider` | 2m 25s | 1m 26s | 3m 32s | 1m 31s |
| `catalog-operator` | 2m 55s | 3m 31s | 2m 55s | 3m 31s |
| `certified-operators-catalog` | 2m 56s | 3m 31s | 2m 56s | 3m 31s |
| `cloud-credential-operator` | 4m 00s | 3m 57s | 4m 00s | 3m 57s |
| `cluster-api` | 7s | 7s | 7s | 7s |
| `cluster-autoscaler` | 1m 41s | 1m 23s | 1m 41s | 1m 23s |
| `cluster-image-registry-operator` | 3m 18s | 3m 31s | 3m 18s | 3m 31s |
| `cluster-network-operator` | 4m 01s | 3m 57s | 5m 08s | 5m 30s |
| `cluster-node-tuning-operator` | 2m 22s | 2m 17s | 2m 22s | 2m 17s |
| `cluster-policy-controller` | 2m 22s | 2m 17s | 2m 22s | 2m 17s |
| `cluster-storage-operator` | 2m 46s | 2m 37s | 4m 01s | 3m 57s |
| `cluster-version-operator` | 2m 30s | 2m 27s | 2m 30s | 2m 27s |
| `community-operators-catalog` | 2m 56s | 3m 31s | 2m 56s | 3m 31s |
| `control-plane-operator` | 4s | 5s | 4s | 5s |
| `control-plane-pki-operator` | 1m 45s | 1m 26s | 1m 45s | 1m 26s |
| `csi-snapshot-controller-operator` | 2m 46s | 2m 38s | 3m 18s | 2m 48s |
| `dns-operator` | 2m 54s | 3m 30s | 2m 54s | 3m 30s |
| `etcd` | 1m 17s | 51s | 1m 17s | 51s |
| `featuregate-generator` | 29s | 26s | 33s | 33s |
| `hosted-cluster-config-operator` | 2m 54s | 2m 47s | 2m 54s | 2m 47s |
| `ignition-server` | 31s | 28s | 31s | 28s |
| `ignition-server-proxy` | 33s | 30s | 33s | 30s |
| `ingress-operator` | 4m 02s | 3m 57s | 4m 02s | 3m 57s |
| `konnectivity-agent` | 1m 46s | 1m 27s | 1m 46s | 1m 27s |
| `kube-apiserver` | 1m 38s | 1m 18s | 1m 38s | 1m 18s |
| `kube-controller-manager` | 1m 41s | 1m 25s | 1m 48s | 1m 29s |
| `kube-scheduler` | 1m 41s | 1m 18s | 1m 45s | 1m 23s |
| `kube-storage-version-migrator` | 1m 44s | 1m 29s | 1m 44s | 1m 29s |
| `machine-approver` | 1m 45s | 1m 26s | 1m 45s | 1m 26s |
| `oauth-openshift` | 1m 59s | 1m 50s | 3m 59s | 2m 20s |
| `olm-collect-profiles` | 1m 40s | 1m 20s | 1m 40s | 1m 20s |
| `olm-operator` | 2m 55s | 3m 31s | 2m 55s | 3m 31s |
| `openshift-apiserver` | 1m 41s | 1m 23s | 1m 54s | 1m 44s |
| `openshift-controller-manager` | 2m 23s | 2m 18s | 2m 23s | 2m 18s |
| `openshift-oauth-apiserver` | 2m 00s | 1m 50s | 2m 51s | 2m 20s |
| `openshift-route-controller-manager` | 1m 59s | 1m 50s | 2m 22s | 2m 19s |
| `packageserver` | 2m 56s | 3m 31s | 2m 56s | 3m 31s |
| `redhat-operators-catalog` | 4m 45s | 4m 32s | 4m 45s | 4m 32s |

### Component creation to availability

This duration starts at the `ControlPlaneComponent` API object's creation and
ends at its first captured `Available=True` transition. It is a component-level
bootstrap interval, not container startup time or a critical-path estimate.

| Component | Creation to available: run 1 | Creation to available: run 2 |
|-----------|------------------------------|------------------------------|
| `aws-cloud-controller-manager` | 1m 23s | 1m 11s |
| `capi-provider` | 2m 22s | 1m 22s |
| `catalog-operator` | 2m 27s | 3m 06s |
| `certified-operators-catalog` | 2m 28s | 3m 06s |
| `cloud-credential-operator` | 3m 33s | 3m 33s |
| `cluster-api` | 4s | 3s |
| `cluster-autoscaler` | 23s | 30s |
| `cluster-image-registry-operator` | 2m 50s | 3m 07s |
| `cluster-network-operator` | 3m 34s | 3m 33s |
| `cluster-node-tuning-operator` | 1m 55s | 1m 53s |
| `cluster-policy-controller` | 1m 55s | 1m 53s |
| `cluster-storage-operator` | 2m 19s | 2m 13s |
| `cluster-version-operator` | 2m 03s | 2m 03s |
| `community-operators-catalog` | 2m 28s | 3m 06s |
| `control-plane-operator` | 2s | 4s |
| `control-plane-pki-operator` | 1m 18s | 1m 03s |
| `csi-snapshot-controller-operator` | 2m 19s | 2m 14s |
| `dns-operator` | 2m 27s | 3m 06s |
| `etcd` | 50s | 27s |
| `featuregate-generator` | 2s | 2s |
| `hosted-cluster-config-operator` | 2m 27s | 2m 23s |
| `ignition-server` | 3s | 3s |
| `ignition-server-proxy` | 5s | 5s |
| `ingress-operator` | 3m 35s | 3m 33s |
| `konnectivity-agent` | 1m 18s | 1m 03s |
| `kube-apiserver` | 1m 11s | 54s |
| `kube-controller-manager` | 1m 14s | 1m 01s |
| `kube-scheduler` | 1m 14s | 54s |
| `kube-storage-version-migrator` | 1m 16s | 1m 04s |
| `machine-approver` | 27s | 33s |
| `oauth-openshift` | 1m 32s | 1m 26s |
| `olm-collect-profiles` | 1m 12s | 55s |
| `olm-operator` | 2m 27s | 3m 06s |
| `openshift-apiserver` | 1m 14s | 59s |
| `openshift-controller-manager` | 1m 56s | 1m 54s |
| `openshift-oauth-apiserver` | 1m 33s | 1m 26s |
| `openshift-route-controller-manager` | 1m 32s | 1m 26s |
| `packageserver` | 2m 28s | 3m 06s |
| `redhat-operators-catalog` | 4m 17s | 4m 07s |

## Guest operator comparison

An operator's first converged state requires `Available=True`,
`Progressing=False`, and `Degraded=False`. It can resume progressing later;
full startup requires all operators to converge simultaneously.

| Guest operator | First converged: run 1 | First converged: run 2 |
|----------------|------------------------|------------------------|
| `cloud-credential` | 3m 45s | 3m 48s |
| `console` | 13m 46s | 11m 55s |
| `csi-snapshot-controller` | 2m 57s | 2m 47s |
| `dns` | 12m 58s | 11m 06s |
| `image-registry` | 3m 02s | 10m 58s |
| `ingress` | 13m 53s | 11m 53s |
| `insights` | 12m 19s | 9m 58s |
| `kube-apiserver` | 2m 59s | 2m 55s |
| `kube-controller-manager` | 2m 59s | 2m 55s |
| `kube-scheduler` | 2m 59s | 2m 55s |
| `kube-storage-version-migrator` | 12m 49s | 10m 21s |
| `monitoring` | 19m 02s | 16m 34s |
| `network` | 13m 14s | 10m 52s |
| `node-tuning` | 11m 40s | 9m 18s |
| `openshift-apiserver` | 2m 59s | 2m 55s |
| `openshift-controller-manager` | 2m 59s | 2m 55s |
| `openshift-samples` | 13m 42s | 11m 33s |
| `operator-lifecycle-manager` | 2m 57s | 3m 25s |
| `operator-lifecycle-manager-catalog` | 3m 28s | 3m 48s |
| `operator-lifecycle-manager-packageserver` | 2m 59s | 2m 55s |
| `service-ca` | 12m 50s | 10m 23s |
| `storage` | 12m 08s | 9m 50s |

## Evidence and limits

The copied scripts and local evidence are in
`aws/startup-roadmap-aws-20261028/` in the private scripts repository.
The original dataset remains under `artifacts/`; the second dataset is under
`artifacts/run-02/`. Each contains `run.json`, the normalized timeline JSONL,
latest snapshot, and report. The second dataset also contains both creation
logs, teardown and partial-create cleanup logs, a first-run checksum manifest,
EC2 boot consoles, monitoring logs, `comparison.json`, and an interactive
`startup-comparison.html`.

The collector records a ten-second polling delay plus query duration. Its
queries are sequential, so the simultaneous completion checklist represents
one collection cycle rather than an atomic read of all resources. API
transition timestamps retain finer precision when present. A first successful
component or operator condition can precede a later rollout; it is distinct
from full completion. Raw credentials and kubeconfigs remain local and ignored
by git.
