# P6–P8: Bootstrap, cloud machines and usable workers

Select only NodePools with `spec.clusterName == HC_NAME`, then inspect the
pool named by the failing aggregate message. CAPI objects in the derived HCP
namespace belong to that HC; inspect the relevant pool/workload when there
are multiple pools. Use fully qualified CAPI resources.

## P6: No machine workload yet

```bash
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HC_NAMESPACE" \
  get nodepool "$NODEPOOL_NAME" -o yaml
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HCP_NAMESPACE" \
  get machinedeployments.cluster.x-k8s.io,machinesets.cluster.x-k8s.io
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HC_NAMESPACE" \
  get hostedcluster "$HC_NAME" -o json | jq '{kubeconfig:.status.kubeconfig,platform:.status.platform}'
```

Check release/platform image, machine configuration, version skew, generated
payload and ignition-endpoint conditions. `IgnitionEndpointAvailable=False`
is actionable; the healthy condition may be removed rather than set `True`.
An empty initial NodePool `status.version` is expected until rollout completes.

Once HC kubeconfig is published, the NodePool controller generates token and
user-data Secrets and a platform template. Replace upgrades normally use a
MachineDeployment; InPlace uses a MachineSet. Inspect Secret names, creation
times and expected annotations only, never their data or bootstrap tokens.

On AWS, a generated payload can coexist with `AWSSecurityGroupAvailable=False`
and `DefaultSGNotReady`. Load the AWS reference before investigating a worker
that has not yet been created. A ready `AWSCluster` does not prove the default
worker SG has been published.

If bootstrap/workload objects are still missing after their prerequisites,
inspect HO's pool-specific reconciliation errors rather than guest kubelet logs.

## P7: Cloud provisioning and ignition

```bash
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HCP_NAMESPACE" \
  get machines.cluster.x-k8s.io -o wide
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HCP_NAMESPACE" \
  logs deployment/capi-provider -c manager --since=10m --tail=200
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HCP_NAMESPACE" \
  get events --sort-by=.lastTimestamp
```

Inspect AWSMachine or AzureMachine status/provider ID, the concrete provider
error, and the corresponding instance/VM. Use the provider reference for cloud
queries. If no instance exists, distinguish IAM/quota/image/network provisioning
errors before investigating an ignition download.

`ReachedIgnitionEndpoint=True` means at least one instance fetched a payload.
It does not prove all machines fetched it, a Node registered, or kubelet is
healthy. Match each Machine/provider ID to its cloud instance and eventual
Node. Boot-time MachineConfig application and a reboot can occur after the
ignition fetch but before a CSR/Node appears. Inspect console/boot progress
before concluding that the missing Node proves ignition failure.

## P8: Machine and Node readiness

```bash
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HCP_NAMESPACE" \
  get machines.cluster.x-k8s.io -o json | jq -r '.items[] |
  .metadata.name as $name | .status.conditions[]? |
  [$name,.type,.status,.reason,.message] | @tsv'
oc --kubeconfig "$GUEST_KUBECONFIG" get nodes -o wide
oc --kubeconfig "$GUEST_KUBECONFIG" get csr
```

`AllMachinesReady` aggregates CAPI Machine readiness; `AllNodesHealthy`
aggregates NodeHealthy; NodePool `Ready` follows its CAPI workload. Read the
aggregate's named Machine and reason rather than assuming these gates advance
in one order. A running EC2 instance/VM is weaker evidence than any of them.

For absent Nodes, follow boot progress, guest API reachability and CSR approval
conditions. For registered unhealthy Nodes, inspect Ready/network/disk/memory
conditions, CNI rollout and kubelet errors. Do not blindly approve every CSR.
Check the requested pool version, desired replica count and actual ready Nodes;
zero existing Machines cannot prove that requested workers are ready.

If guest access is unavailable, report what management-side status establishes
and what remains unverified. Do not substitute management-cluster Nodes for
guest Nodes. Once the requested workers are ready, use guest convergence for
the remaining full-cluster checks.
