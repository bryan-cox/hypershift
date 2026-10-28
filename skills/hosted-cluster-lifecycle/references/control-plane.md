# P0–P5: Find the blocking control-plane dependency

Use the variables and explicit kubeconfig established by intake. Read only
the subsection that matches the current gate. Start with the object's exact
condition reason/message before expanding to logs or cloud resources.

## P0: Intake or validation

If HC is absent, use setup's pre-HC failure path. If present, inspect its
required validation conditions and `Progressing` reason. Common condition
families concern configuration, release image, platform identity, KMS, proxy
and IDP inputs; their presence and ownership vary by version/provider.
An identity/OIDC failure can arise after HCP and KAS exist, so inspect current
validation even when resources are already running.

```bash
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HC_NAMESPACE" \
  get hostedcluster "$HC_NAME" -o json | jq -r \
  '.status.conditions[]? | [.type,.status,.reason,.message,.lastTransitionTime] | @tsv'
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HO_NAMESPACE" \
  logs deployment/"$HO_DEPLOYMENT" --since=10m --tail=200
```

Confirm the actual failed input before proposing credential rotation or an HO
rollout. Provider identity details are in the AWS/Azure references.

## P1: Scaffold

Look for HCP, platform infrastructure CR, CAPI Cluster and management operator
deployments. Their creation is different from readiness. The expected core
chain is HCP → provider infrastructure object → CAPI Cluster; operator setup
and other reconciliation then overlap.

```bash
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HCP_NAMESPACE" \
  get clusters.cluster.x-k8s.io,deployments,pods
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HCP_NAMESPACE" \
  get events --sort-by=.lastTimestamp
```

For a missing object, use HO logs and the last created dependency. For a
present but unready deployment, inspect its pods/init containers and events.
An AWS provider pod may wait for `service-network-admin-kubeconfig` while KAS
starts; an early `FailedMount` alone does not prove bad cloud credentials.

## P2: Published endpoints

HCP `InfrastructureReady=True` means required endpoint addresses were
published. It does not certify the provider infrastructure object, worker
network, DNS reachability from every client, or worker readiness.

```bash
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HCP_NAMESPACE" \
  get hostedcontrolplane "$HC_NAME" -o json | jq '.status.conditions'
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HCP_NAMESPACE" get services
```

Inspect Routes only where the API and publishing strategy use them. Follow
the condition to the missing Service/LB address, Route, DNS or private endpoint.
For cloud/private networking details, load the selected provider reference.

## P3–P4: Foundation and KAS

With managed etcd, start at etcd and featuregate-generator dependencies before
KAS. With external etcd, follow its configured endpoint/identity path instead
of diagnosing a deliberately absent managed etcd StatefulSet.

```bash
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HCP_NAMESPACE" \
  get controlplanecomponents featuregate-generator kube-apiserver -o yaml
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HCP_NAMESPACE" get pods,pvc,jobs
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HCP_NAMESPACE" \
  get deployment kube-apiserver -o yaml
```

For managed etcd, also inspect the `etcd` CPC, PVC binding,
storage/scheduling events and pod logs. For the
featuregate generator, inspect the Job and its pod logs. For KAS, distinguish
Deployment availability (`KubeAPIServerAvailable`) from the separate endpoint
health check used by HCP availability. `KASLoadBalancerNotReachable` can indicate
DNS or network reachability even with a healthy KAS deployment.

Use HC `status.kubeconfig` and HCP `status.kubeConfig` as publication signals,
not proof of guest API reachability. Their availability can unlock worker
bootstrap before all control-plane components finish rolling out.

## P5: Availability versus rollout completion

Read HCP `Available` reason/message. For `ControlPlaneComponentsNotAvailable`,
inspect the named CPCs; for an endpoint/foundation reason, return to that gate.

```bash
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HCP_NAMESPACE" \
  get controlplanecomponents -o json | jq -r '.items[] |
  .metadata.name as $name | .status.conditions[]? |
  select(.type == "Available" or .type == "RolloutComplete") |
  [$name,.type,.status,.reason,.message] | @tsv'
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HCP_NAMESPACE" \
  get hostedcontrolplane "$HC_NAME" -o json | jq '.status.controlPlaneVersion'
```

`WaitingForDependencies` directs inspection upstream; `WaitingForOperands`
can mean a primary workload is ready while a child operand is not. Component
registration order is not boot order. Follow dependency status/version rather
than restarting every component downstream of an unready dependency.

HC `Available` mirrors HCP availability; neither checks workers. CPC
`Available=True` alone is also weaker than `RolloutComplete=True` at the desired
component version and HCP `controlPlaneVersion.history[0].state=Completed`.
Use version-matched source/CRDs if this component API differs or is absent in
the deployed release; API discovery failure is not a failed component condition.
