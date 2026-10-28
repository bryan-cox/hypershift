# Identify the target and the current gates

Resolve these from the request and existing session before asking for missing
information: plan/create/monitor/debug, management kubeconfig and optional
context, HC namespace/name, provider, release/HO images, intended NodePools,
and readiness target. Do not use remembered personal kubeconfig paths or old
experiment images as defaults. For an offline request, use supplied snapshots
and identify their collection times and HC UID; do not contact a cluster.

For live work, define these from the resolved target:

```bash
MGMT_KUBECONFIG=/path/from-the-current-task
HC_NAMESPACE=selected-namespace
HC_NAME=selected-cluster
```

Add `--context "$MGMT_CONTEXT"` to management commands when a context was
selected within a multi-context kubeconfig. Keep guest commands separate.
The examples below assume the selected context is already the kubeconfig's
current context; do not change the user's global current context to make them work.

Take a minimal snapshot with `oc` and `jq`:

```bash
oc --kubeconfig "$MGMT_KUBECONFIG" config current-context
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HC_NAMESPACE" \
  get hostedcluster "$HC_NAME" -o json | jq '{
    metadata: (.metadata | {name, namespace, uid, generation,
      creationTimestamp, deletionTimestamp}),
    platform: .spec.platform.type, release: .spec.release,
    infraID: .spec.infraID, status: .status}'

HCP_NAMESPACE="${HC_NAMESPACE}-${HC_NAME//./-}"
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HCP_NAMESPACE" \
  get hostedcontrolplane "$HC_NAME" --ignore-not-found -o json
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HC_NAMESPACE" \
  get nodepools -o json | jq --arg hc "$HC_NAME" \
  '[.items[] | select(.spec.clusterName == $hc) | {
    name: .metadata.name, uid: .metadata.uid,
    generation: .metadata.generation, replicas: .spec.replicas,
    release: .spec.release, status: .status}]'
```

If HC is absent, check the exact create command and its logs before P0.
Cloud resources may already have been allocated. A permission, transport, or
discovery failure is different from a successful NotFound response.

Derive HCP namespace using the naming rule above and verify against discovered
objects if a version or deployment differs. Discover the HO deployment and
namespace rather than assuming `hypershift/operator` always exists. The
common HCP deployments are `control-plane-operator`, `cluster-api`, and
`capi-provider`. Use fully qualified CAPI resource names to avoid confusing
them with OpenShift `Cluster` or Machine API resources.

Read `status.conditions` as a map keyed by `type`; keep `status`, `reason`,
`message`, `lastTransitionTime`, and `observedGeneration` where present.
Check generation freshness when reported. Missing optional conditions are
not failures, and some healthy conditions are intentionally absent. Interpret
the polarity: `Progressing=False` and `Degraded=False` are not failed gates.

Choose the relevant workstreams rather than inferring everything from HC
`Available`: HCP/HC availability concerns control-plane usability; component
rollout completion adds version convergence; NodePool readiness concerns
workers; guest CVO/operators determine the remaining full-cluster target.
If worker intent is unknown, establish it before diagnosing absent NodePools
as a fault. Control-plane-only and deliberately zero-replica configurations
must not cause the agent to create workers without instruction.
