# Diagnose or observe teardown

Use when deletion has already begun or cleanup is part of the authorized task.
First retain HC UID, infrastructure ID, worker VPC or resource-group ownership,
provider identities and published endpoint resource identities. If starting
deletion, inventory before invoking the normal supported destroy workflow.
Do not infer permission to destroy from a startup-debugging request.

Follow the first remaining dependency, allowing cleanup workstreams to overlap:

| Remaining resource | Inspect first |
|--------------------|---------------|
| NodePool/Machine | Deleting conditions, infrastructure-machine finalizers, CAPA/CAPZ logs and instance/VM state |
| CAPI/provider Cluster | Remaining Machines, provider conditions and exact cloud errors |
| HCP | CPO cleanup, guest cloud resources, Services/PVCs and endpoint cleanup |
| HCP namespace/pods | Actual remaining objects, namespace conditions, finalizers and pod grace periods |
| HC after HCP namespace removal | CLI finalizer, destroy command progress and remaining cloud/IAM resources |
| Failed query | Authentication, permissions, discovery or connectivity; absence is unverified |

An AWS Machine can remain in `WaitingForInfrastructureDeletion` while EC2
shuts down. A terminating pod's `deletionTimestamp` can be a future grace
deadline. Retain deletion/termination grace-period fields and first observed
termination separately from first absence. Namespace conditions can lag or
omit what a separate object query still sees; inspect both.

On AWS, temporary internet-gateway `DependencyViolation` errors for mapped
public addresses can accompany a NAT gateway still `deleting`. Watch the
exact gateway and its dependencies for progress before treating a retry as
a permanent leak. A terminal `deleted` NAT record or `terminated` EC2 record
can remain visible after cleanup; report the terminal state accurately.

For timing, use the available read-only `hack/cluster-teardown-timeline.py`
and its `--help`/teardown roadmap, or scoped timestamped snapshots. The AWS
collector requires a pre-deletion inventory, exact HC UID/infra/VPC/OIDC
inputs and a command-state file. Its NodePool inventory covers the whole HC
namespace, so use it only with a dedicated HC namespace or adapt filtering
before applying it to a shared one. It does not implement Azure cloud cleanup.

Distinguish successful destroy-command return from independent cleanup checks.
Verify the HC, its selected NodePools, HCP namespace and relevant CAPI resources
are absent, then verify the exact owned cloud/network/IAM resources. Track
detached EIPs, volumes, gateways, load balancers/target groups and private zones
by retained identity so detachment alone is not counted as deletion. For Azure,
respect shared resource groups and verify exact owned resources; do not delete
a whole resource group merely to clear one cluster.

Do not remove finalizers or force-delete to improve a timing sample. If normal
cleanup fails, save the concrete blocker and propose a scoped remedy. Shared
management infrastructure, HO, base-domain zones and OIDC buckets can remain
outside the cluster's deletion scope; name that scope in the cleanup result.
