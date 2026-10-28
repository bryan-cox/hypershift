# P9: Guest convergence and completion

Use the guest kubeconfig supplied for this HC. If obtaining it is necessary,
resolve the Secret from HC `status.kubeconfig.name`, verify its HC ownership
and creation identity, and save it locally with restrictive permissions.
Do not assume a predecessor's kubeconfig still targets the new HC with the
same name. Do not print Secret data or include the kubeconfig in an evidence bundle.

```bash
oc --kubeconfig "$GUEST_KUBECONFIG" get clusterversion version -o json
oc --kubeconfig "$GUEST_KUBECONFIG" get clusteroperators
oc --kubeconfig "$GUEST_KUBECONFIG" get clusteroperators -o json | jq -r \
  '.items[] | .metadata.name as $name | .status.conditions[]? |
  select((.type == "Available" and .status != "True") or
    ((.type == "Progressing" or .type == "Degraded") and .status != "False")) |
  [$name,.type,.status,.reason,.message] | @tsv'
```

For the remaining operator, inspect its conditions, operand custom resources,
deployments, events and controller logs. Some guest operators have controllers
running in the HCP namespace; use the actual deployment location.

For monitoring specifically, inspect `Prometheus/k8s` and CMO reconciliation
tasks, not only existing pods:

```bash
oc --kubeconfig "$GUEST_KUBECONFIG" -n openshift-monitoring \
  get prometheus,deployments,statefulsets,pods
oc --kubeconfig "$GUEST_KUBECONFIG" -n openshift-monitoring \
  logs deployment/cluster-monitoring-operator --since=10m --tail=250
```

A dependency error may be followed by an availability-validation timer before
a retry. Use the exact error and observed progress; do not infer expired STS
credentials from an absent Prometheus. The observations reference describes
the pinned release where this was measured; it is not a universal timer.

HCCO propagates guest CVO/operator status to HCP, and HO then propagates it to
HC. A brief lag between guest completion and HC history completion can be
reconciliation delay. If the guest is complete but HC stays stale, inspect
HCCO/HO status propagation and generations.

For a full-cluster target, verify these together in the current observation:

- Required HC validation passes with no blocking required condition false;
  HC and HCP `Available=True`.
- Relevant CPCs are available and rollout-complete at the desired version;
  HCP `status.controlPlaneVersion.history[0].state` is `Completed`.
- Every requested NodePool has `Ready`, `AllMachinesReady` and
  `AllNodesHealthy` true, populated desired `status.version`, and the intended
  worker count. Respect intentional zero replicas or control-plane-only scope.
- Guest ClusterVersion is Available, not Progressing and not Failing; its latest
  history entry is Completed for the intended release. Resolve tag/digest
  equivalence from release metadata rather than comparing unrelated strings.
- Every expected guest ClusterOperator is Available, not Progressing and not
  Degraded; do not accept an empty or partial operator list as convergence.
- HC `status.version.history[0]` is Completed for `status.version.desired.image`.

Management `Progressing=False` alone does not establish these conditions. If
guest checks cannot be performed, label full guest convergence unverified.
For an explicit control-plane-only target, report the applicable control-plane
gates and state which worker/guest workload checks are outside that target.
