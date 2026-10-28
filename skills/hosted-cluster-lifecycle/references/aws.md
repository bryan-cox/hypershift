# AWS overlay

Load for AWS setup or a provider/identity/network gate. Keep the account,
region, role/credential source and infrastructure ID tied to the selected HC.
Read cloud identity metadata without displaying credential material:

```bash
aws sts get-caller-identity
oc --kubeconfig "$MGMT_KUBECONFIG" -n "$HCP_NAMESPACE" \
  get awsclusters.infrastructure.cluster.x-k8s.io,awsmachines.infrastructure.cluster.x-k8s.io
```

## Setup and identity

Confirm the workflow's VPC/subnets, region/zones, quotas, OIDC bucket/prefix,
issuer and IAM trust/roles. Existing infrastructure/IAM inputs change what
the create command allocates. Before HC exists, read create logs for NAT,
network or IAM failures; preserve the allocated infrastructure ID before a retry.

Distinguish credential consumers. CLI `create cluster aws --sts-creds` accepts
STS JSON; HO `install --oidc-storage-provider-s3-credentials` supplies a shared
AWS INI profile to its SDK. Temporary INI credentials require a session token
in addition to the access/secret keys. Verify the format and consumer without
printing the file. SDK fallback to EC2 IMDS may indicate unreadable/misformatted
credentials; do not diagnose expiry from that message alone.

Read current conditions and logs to distinguish OIDC document publication,
AWS identity-provider validation and default worker SG creation. These are
related but separate gates. A late OIDC publication error can block startup
after KAS has started.

## No Machines despite a generated payload

Check HCP `ValidAWSIdentityProvider` and `AWSDefaultSecurityGroupCreated`, and
HC `status.platform.aws.defaultWorkerSecurityGroupID`. NodePool can report
`ValidGeneratedPayload=True` alongside `AWSSecurityGroupAvailable=False`
(`DefaultSGNotReady`). This is upstream of instance boot or ignition delivery.
Some release versions defer identity validation until KAS and then use a
five-minute retry interval; check the deployed source and the observations
reference before applying that explanation. `Unknown` alone is not bad STS.

## Endpoints and workers

Inspect Service/LB/DNS and the configured endpoint strategy. Conditions such
as `AWSEndpointServiceAvailable`, `AWSEndpointAvailable`, `ExternalDNSReachable`
and `PublicEndpointExposed` apply only where that workflow uses them. For a
private cluster, distinguish worker-VPC resources from control-plane load
balancers/endpoints in the management VPC.

For a Machine/provider ID, use exact instance IDs and the selected region:

```bash
aws ec2 describe-instances --region "$AWS_REGION" --instance-ids "$INSTANCE_ID" \
  --query 'Reservations[].Instances[].{ID:InstanceId,State:State.Name,Subnet:SubnetId}'
```

When boot evidence is needed, save `aws ec2 get-console-output --latest` for
that exact instance locally; inspect/redact it before sharing. First-boot
MachineConfig work/reboot can follow a successful ignition fetch. Scope
network and quota checks to the implicated VPC/zone and error, rather than
changing account-wide roles or firewall rules as a diagnostic experiment.
