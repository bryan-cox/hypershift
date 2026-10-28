# Capture evidence without changing the experiment

For a quick diagnosis, save a small timestamped set of the relevant objects,
conditions, events and logs. A continuous collector is useful when the task
asks to watch startup, retain durations, or reproduce a transition. Do not
start a new cluster merely to acquire a timeline for an existing failure.

Keep each lifecycle attempt in a separate directory with HC UID, namespace/
name, infrastructure ID, provider, region/location, release tag and resolved
digest, HO image/source revision, release CPO image/source revision, NodePool
topology, endpoint access, management-cluster context, and collection times.
Record interventions explicitly. Never merge clusters with reused names or
mix management and guest observations under an unlabeled kubeconfig.

If the available HyperShift checkout contains the timeline collector, execute
it with an explicit management kubeconfig (Python 3.11+, `oc`; verify the
selected script's `--help`). `HYPERSHIFT_REPO` is the resolved existing checkout,
not an assumed path:

```bash
umask 077
python3 "$HYPERSHIFT_REPO/hack/cluster-startup-timeline.py" \
  --kubeconfig "$MGMT_KUBECONFIG" \
  --namespace "$HC_NAMESPACE" --name "$HC_NAME" \
  --interval 10 --timeout 45m --output-dir "$RUN_DIR/startup"
```

Add `--guest-kubeconfig "$GUEST_KUBECONFIG"` when the matching config is
available. For capture begun before creation, use the collector's supported
late-loading workflow if available; do not create a placeholder that can
accidentally fall back to another cluster. With no collector checkout, retain
scoped timestamped snapshots instead; core diagnosis does not depend on it.

Retain original `observations.jsonl`, latest snapshot and report, command logs,
and separately derived summaries. Record API creation/transition/completion
timestamps where available; otherwise label first polling observation.
For creation, keep command start → HC creation separate from HC creation →
readiness. For deletion, keep command start, API deletion timestamp, command
return and independently verified absence separate. Exclude review/idle time
from both clocks. Retain failed pre-HC attempts separately from startup timing.

Polling includes query duration and is not an atomic snapshot. Conditions may
lag workloads. Preserve first successful observations and later regressions;
the latest `lastTransitionTime` is not necessarily the first success. Current
readiness must be checked against the latest intended generation/version.
Insufficient or failed observations remain unknown, not ready or absent.

Use restrictive directory/file permissions for captures. Keep raw credentials,
guest kubeconfigs and Secret data outside ordinary evidence bundles and git.
Inspect logs and provider console output for sensitive content before sharing.
Save the dataset paths in the result and say whether they are local, committed,
or uploaded; a pushed documentation change is not a backup of ignored raw data.

When comparing runs, preserve configuration, version and intervention
differences and the number of observations. Two samples on a warm management
cluster are observations, not a production timing SLO or timeout rule.

Optional deeper source material in a matching HyperShift checkout:
`docs/content/reference/cluster-startup-roadmap.md`,
`cluster-startup-aws-repeat.md`, and `cluster-teardown-roadmap.md` in the same
reference directory. They may only exist on the roadmap branch. Load a
relevant section if more detail is needed; do not require these files for
basic use of this self-contained skill.
