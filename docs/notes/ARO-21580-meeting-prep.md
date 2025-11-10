# ARO-21580 Meeting Preparation

**Issue**: Missing/Deleted Managed Identity Cluster Deletion Automated Cleanup
**Related Bug**: OCPBUGS-63720
**Status**: To Do
**Assignee**: Bryan Cox
**Meeting Date**: 2025-11-11

---

## Executive Summary

### The Problem
When customers delete their Azure managed identities before deleting an ARO HCP cluster, the cluster deletion gets stuck indefinitely because CAPZ (Cluster API Provider Azure) cannot authenticate to clean up Azure infrastructure resources (VMs, load balancers, etc.) in the managed resource group.

### The Solution
Implement an automated cleanup mechanism similar to AWS that:
1. Detects when managed identities are invalid during cluster deletion
2. Removes finalizers from stuck AzureMachine CRs to allow deletion to proceed
3. Relies on ARO's first-party application (1P app) to clean up the managed resource group resources

### Impact
- **Customer Experience**: Enables self-service cluster deletion without SRE intervention
- **SRE Burden**: Eliminates manual cleanup procedures
- **Reliability**: Prevents permanently stuck clusters

---

## Problem Deep Dive

### Background: ARO HCP Architecture

**Managed Resource Group**:
- Created and fully controlled by ARO's first-party application (1P app/FPSP)
- Contains customer node infrastructure: VMs, load balancers, NICs, etc.
- Protected by deny assignment preventing customer modifications

**Managed Identities (MSIs)**:
- Customer-provided and customer-controlled
- Used by CAPZ and other operators for infrastructure management
- Required for normal cluster operations
- Can be deleted or have permissions revoked by customer

### Current Deletion Flow

```
1. Customer → Frontend → Cluster Service → Maestro
2. Maestro deletes HostedCluster CR
3. HyperShift reconciles deletion
4. CAPZ attempts to delete AzureMachine resources
   ❌ FAILS: "AADSTS700016: Application with identifier 'app-id' was not found in directory 'tenant-id'"
5. Deletion stuck - finalizers prevent CR removal
```

### Why This Is a Problem

1. **Deny Assignment**: Once deletion starts, deny assignment prevents adding identities back
2. **Stuck State**: Cluster permanently stuck in "Deleting" status
3. **Orphaned Resources**: CRs remain in management cluster, resources may remain in Azure
4. **Manual Intervention**: Currently requires SRE to manually patch finalizers

### The Opportunity

ARO has a powerful first-party application with full permissions over the managed resource group. We should leverage this instead of blocking on customer-controlled identities during deletion.

---

## Current Implementation Reference (AWS)

AWS already solved this problem. Here's the implementation:

### AWS ValidCredentials Check
**Location**: `hypershift-operator/controllers/hostedcluster/internal/platform/aws/aws.go:330-340`

```go
func ValidCredentials(hc *hyperv1.HostedCluster) bool {
    oidcConfigValid := meta.FindStatusCondition(hc.Status.Conditions, string(hyperv1.ValidOIDCConfiguration))
    if oidcConfigValid != nil && oidcConfigValid.Status == metav1.ConditionFalse {
        return false
    }
    validIdentityProvider := meta.FindStatusCondition(hc.Status.Conditions, string(hyperv1.ValidAWSIdentityProvider))
    if validIdentityProvider != nil && validIdentityProvider.Status != metav1.ConditionTrue {
        return false
    }
    return true
}
```

**Key Insight**: Uses HostedCluster status conditions to determine credential validity

### AWS DeleteOrphanedMachines
**Location**: `hypershift-operator/controllers/hostedcluster/internal/platform/aws/aws.go:342-364`

```go
func (AWS) DeleteOrphanedMachines(ctx context.Context, c client.Client, hc *hyperv1.HostedCluster, controlPlaneNamespace string) error {
    if ValidCredentials(hc) {
        return nil  // Only orphan if credentials are invalid
    }
    awsMachineList := capiaws.AWSMachineList{}
    if err := c.List(ctx, &awsMachineList, client.InNamespace(controlPlaneNamespace)); err != nil {
        return fmt.Errorf("failed to list AWSMachines in %s: %w", controlPlaneNamespace, err)
    }
    logger := ctrl.LoggerFrom(ctx)
    var errs []error
    for i := range awsMachineList.Items {
        awsMachine := &awsMachineList.Items[i]
        if !awsMachine.DeletionTimestamp.IsZero() {
            awsMachine.Finalizers = []string{}  // Remove all finalizers
            if err := c.Update(ctx, awsMachine); err != nil {
                errs = append(errs, fmt.Errorf("failed to delete machine %s/%s: %w", awsMachine.Namespace, awsMachine.Name, err))
                continue
            }
            logger.Info("skipping cleanup of awsmachine because of invalid AWS identity provider", "machine", client.ObjectKeyFromObject(awsMachine))
        }
    }
    return utilerrors.NewAggregate(errs)
}
```

**Key Logic**:
1. Check if credentials are valid - if yes, do nothing
2. If invalid, list all AWSMachine CRs
3. For machines with deletion timestamp, remove finalizers
4. Log the orphaning action
5. Let Kubernetes GC handle the rest

### Integration Point
**Location**: `hypershift-operator/controllers/hostedcluster/hostedcluster_controller.go:3195-3199`

```go
if od, ok := p.(platform.OrphanDeleter); ok {
    if err = od.DeleteOrphanedMachines(ctx, r.Client, hc, controlPlaneNamespace); err != nil {
        return false, err
    }
}
```

**Pattern**: Uses type assertion to check if platform implements OrphanDeleter interface

---

## Proposed Solution for Azure

### Implementation Components

#### 1. Add ValidCredentials Function
**Location**: `hypershift-operator/controllers/hostedcluster/internal/platform/azure/azure.go`

```go
func ValidCredentials(hc *hyperv1.HostedCluster) bool {
    // Option A: Check for specific Azure identity condition (if exists)
    validIdentityProvider := meta.FindStatusCondition(hc.Status.Conditions, string(hyperv1.ValidAzureIdentityProvider))
    if validIdentityProvider != nil && validIdentityProvider.Status != metav1.ConditionTrue {
        return false
    }
    return true

    // Option B: If no condition exists, we may need to add one
    // This requires updating the condition types and controller logic
}
```

**Decision Needed**: Do we need to add a new condition type `ValidAzureIdentityProvider`?

#### 2. Implement DeleteOrphanedMachines for Azure
**Location**: `hypershift-operator/controllers/hostedcluster/internal/platform/azure/azure.go`

Replace the current no-op `DeleteCredentials` with:

```go
func (Azure) DeleteOrphanedMachines(ctx context.Context, c client.Client, hc *hyperv1.HostedCluster, controlPlaneNamespace string) error {
    if ValidCredentials(hc) {
        return nil
    }

    azureMachineList := capiazure.AzureMachineList{}
    if err := c.List(ctx, &azureMachineList, client.InNamespace(controlPlaneNamespace)); err != nil {
        return fmt.Errorf("failed to list AzureMachines in %s: %w", controlPlaneNamespace, err)
    }

    logger := ctrl.LoggerFrom(ctx)
    var errs []error
    for i := range azureMachineList.Items {
        azureMachine := &azureMachineList.Items[i]
        if !azureMachine.DeletionTimestamp.IsZero() {
            azureMachine.Finalizers = []string{}
            if err := c.Update(ctx, azureMachine); err != nil {
                errs = append(errs, fmt.Errorf("failed to orphan machine %s/%s: %w", azureMachine.Namespace, azureMachine.Name, err))
                continue
            }
            logger.Info("skipping cleanup of azuremachine because of invalid Azure identity", "machine", client.ObjectKeyFromObject(azureMachine))
        }
    }
    return utilerrors.NewAggregate(errs)
}
```

#### 3. Ensure Azure Type Implements OrphanDeleter
The Azure struct must implement the `platform.OrphanDeleter` interface:

```go
type OrphanDeleter interface {
    DeleteOrphanedMachines(ctx context.Context, c client.Client, hc *hyperv1.HostedCluster, controlPlaneNamespace string) error
}
```

This is automatically satisfied once we add the DeleteOrphanedMachines method.

#### 4. Add Condition Tracking (Optional but Recommended)

**Location**: `api/hypershift/v1beta1/hostedcluster_conditions.go`

Add new condition type:
```go
// ValidAzureIdentityProvider indicates the Azure managed identity credentials are valid
ValidAzureIdentityProvider ConditionType = "ValidAzureIdentityProvider"
```

**Location**: Controller that validates Azure credentials (TBD - needs investigation)

Add logic to set this condition based on Azure SDK errors, specifically detecting:
- `AADSTS700016`: Application not found in directory
- `AADSTS700027`: Client assertion failed
- Other Azure authentication errors

---

## Technical Details

### Files to Modify

1. **hypershift-operator/controllers/hostedcluster/internal/platform/azure/azure.go**
   - Line 369-371: Replace `DeleteCredentials` no-op or add `DeleteOrphanedMachines`
   - Add `ValidCredentials` function
   - Import required packages (capiazure, ctrl, utilerrors)

2. **api/hypershift/v1beta1/hostedcluster_conditions.go** (Optional)
   - Add `ValidAzureIdentityProvider` condition type

3. **Appropriate Azure credential validator** (TBD)
   - Add logic to set `ValidAzureIdentityProvider` condition
   - Detect AADSTS700016 and similar errors

### Resources Affected

- **AzureMachine CRs**: Primary target for finalizer removal
- **AzureCluster CR**: May also need consideration
- **HostedCluster CR**: Status conditions updated

### CAPZ Resource Types
Located in `vendor/sigs.k8s.io/cluster-api-provider-azure/api/v1beta1/`:
- `AzureMachine`
- `AzureCluster`
- `AzureClusterIdentity`

---

## Open Questions for Discussion

### 1. Condition Tracking
**Question**: Should we add a new `ValidAzureIdentityProvider` condition type, or use an existing mechanism?

**Options**:
- A) Add new condition type (consistent with AWS pattern)
- B) Use existing `ValidAzureKMSConfig` or other Azure conditions
- C) Check for errors directly without condition tracking

**Recommendation**: Option A - add new condition type for consistency

### 2. Credential Validation Location
**Question**: Where should we detect and set the invalid identity condition?

**Options**:
- A) In the CAPZ controller (monitor CAPZ errors)
- B) In HyperShift Azure reconciliation logic
- C) Periodic validation check

**Needs Investigation**: Where are AWS credentials validated?

### 3. AzureCluster Handling
**Question**: Should we also orphan AzureCluster CR or just AzureMachine CRs?

**Considerations**:
- AWS only handles AWSMachine CRs
- AzureCluster might have its own finalizers
- Need to understand dependency chain

### 4. Testing Strategy
**Question**: How do we test this behavior?

**Options**:
- A) E2E test that deletes identity before cluster deletion
- B) Unit tests with mocked conditions
- C) Integration test with Azure SDK

**Recommendation**: All three - unit tests, integration tests, and E2E validation

### 5. Permissions Issue Handling
**Question**: The ticket asks "if the identity exists, but permissions are missing for cleanup, how should we handle it?"

**Options**:
- A) Treat permission errors same as missing identity (orphan the resources)
- B) Different handling for permission vs missing identity
- C) Only orphan if identity is completely missing

**Recommendation**: Needs discussion - likely Option A for simplicity

### 6. Other Azure Resources
**Question**: Are there other Azure CAPI resources beyond AzureMachine that might need orphaning?

**Potential Resources**:
- Network interfaces
- Load balancers
- Storage accounts
- Public IPs

**Note**: These are typically managed as part of the managed resource group and will be cleaned up by the 1P app, but we should verify.

### 7. Metrics and Monitoring
**Question**: Should we add metrics for orphaned machine cleanup?

**Considerations**:
- Track frequency of this scenario
- Alert on high rates (may indicate systemic issue)
- Customer education opportunity

---

## Dependencies

### OCPSTRAT-2541
ARO-21580 depends on this issue. Need to understand:
- What is OCPSTRAT-2541 about?
- How does it relate to this work?
- Is it blocking or can we proceed in parallel?

**Action**: Check OCPSTRAT-2541 status before meeting

### Cluster Service Integration
Need to verify:
- Does Cluster Service know to use 1P app for final cleanup?
- Is there any coordination needed between HyperShift orphaning and CS cleanup?
- What happens if CS starts cleanup before HyperShift orphans?

---

## Success Criteria

From OCPBUGS-63720:

1. ✅ On deleting a Hosted cluster, check if Azure credentials are valid from the hosted cluster status
2. ✅ If credentials are invalid, skip deleting resources from the managed resource group
3. ✅ Log a message indicating resource deletion was skipped due to invalid credentials

From ARO-21580:

1. ✅ A customer can delete a cluster that's missing managed identities and the cluster deletion will succeed
2. ✅ The customer does not have to perform any manual steps in order for the cluster deletion to succeed

---

## Implementation Estimate

### Phase 1: Core Implementation (1 sprint)
- Add ValidCredentials function for Azure
- Implement DeleteOrphanedMachines method
- Add condition type (if needed)
- Unit tests

### Phase 2: Condition Tracking (1 sprint)
- Add logic to detect invalid credentials
- Set condition appropriately
- Integration tests

### Phase 3: Validation (1 sprint)
- E2E testing
- Documentation
- SOP updates (removing the manual process)

**Total**: ~3 sprints

---

## Alternative Approaches Considered

### 1. Prevent Identity Deletion
**Approach**: Block customers from deleting identities
**Pros**: Prevents the problem entirely
**Cons**: Not feasible - identities are in customer subscription

### 2. Automatically Recreate Identities
**Approach**: Detect missing identity and recreate it
**Pros**: Enables normal cleanup
**Cons**: Complex, may not have permissions, doesn't handle deny assignment

### 3. Skip Cleanup Entirely
**Approach**: Remove all finalizers immediately when identity missing
**Pros**: Simple, fast deletion
**Cons**: Could leave orphaned resources in management cluster, breaks normal flow

**Selected Approach**: Orphan CAPI machines, let 1P app clean up Azure resources
**Rationale**: Balances cleanup completeness with deletion success, leverages existing 1P app capabilities

---

## Next Steps

### Before Meeting
- [x] Review Jira tickets
- [x] Analyze codebase
- [x] Document AWS implementation
- [ ] Check OCPSTRAT-2541 dependency
- [ ] Review with team lead

### During Meeting
- [ ] Confirm approach
- [ ] Resolve open questions
- [ ] Assign tasks
- [ ] Set timeline

### After Meeting
- [ ] Update Jira with decisions
- [ ] Create implementation subtasks
- [ ] Start Phase 1 implementation
- [ ] Schedule design review if needed

---

## References

### Jira Issues
- **ARO-21580**: Missing/Deleted Managed Identity Cluster Deletion Automated Cleanup
  - https://issues.redhat.com/browse/ARO-21580
  - Parent: HCMSTRAT-6 (ARO HCP Milestone - Private Preview Improvements)
  - Labels: PotentialOCPDependency, ReliabilityAndExperience

- **OCPBUGS-63720**: Ignore resource deletion failures when identities are invalid for managed resource group
  - Assigned to: Bryan Cox
  - Status: New

- **OCPSTRAT-2541**: (Dependency - needs investigation)

### Code Locations

**AWS Reference Implementation**:
- `hypershift-operator/controllers/hostedcluster/internal/platform/aws/aws.go:330-364`

**Azure Platform Implementation**:
- `hypershift-operator/controllers/hostedcluster/internal/platform/azure/azure.go`

**Platform Interface**:
- `hypershift-operator/controllers/hostedcluster/internal/platform/platform.go:80-84`

**Controller Integration**:
- `hypershift-operator/controllers/hostedcluster/hostedcluster_controller.go:3195-3199`

**Condition Types**:
- `api/hypershift/v1beta1/hostedcluster_conditions.go`

### Related Documentation
- CAPZ Identity Docs: https://capz.sigs.k8s.io/topics/identities
- HyperShift Architecture: See AGENTS.md in repo
- Cluster Deletion Flow: See issue description

---

## Meeting Notes Section

### Attendees
- [ ] Bryan Cox
- [ ]
- [ ]
- [ ]

### Decisions Made
1.
2.
3.

### Action Items
1. **Owner**: Task - Due:
2. **Owner**: Task - Due:
3. **Owner**: Task - Due:

### Follow-up Questions
1.
2.
3.

---

*Document prepared by: Claude Code*
*Last updated: 2025-11-10*
*Meeting date: 2025-11-11*
