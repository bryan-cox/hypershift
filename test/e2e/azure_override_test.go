//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"os"
	"testing"

	. "github.com/onsi/gomega"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	e2eutil "github.com/openshift/hypershift/test/e2e/util"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// TestAzureCreateClusterWithOverride validates that HostedCluster creation
// works correctly when CPO overrides are configured for the Azure platform.
// This test ensures override images are properly applied during cluster creation.
//
// When it runs:
//   - Only when TEST_CPO_OVERRIDE=1 environment variable is set
//   - Only when platform is Azure
//
// Test flow:
//  1. Create cluster using override test releases
//  2. Verify cluster is created successfully
//  3. Validate cluster reaches Available status
func TestAzureCreateClusterWithOverride(t *testing.T) {
	t.Parallel()

	// Skip if override testing is not enabled
	if os.Getenv("TEST_CPO_OVERRIDE") != "1" {
		t.Skip("Skipping test because TEST_CPO_OVERRIDE is not set to 1")
	}

	// Skip if not running on Azure platform
	if globalOpts.Platform != hyperv1.AzurePlatform {
		t.Skip("Skipping test because it requires Azure platform")
	}

	ctx, cancel := context.WithCancel(testContext)
	defer cancel()

	// Get default cluster options which will use override test releases
	// when TEST_CPO_OVERRIDE=1 via the Complete() method
	clusterOpts := globalOpts.DefaultClusterOptions(t)

	e2eutil.NewHypershiftTest(t, ctx, func(t *testing.T, g Gomega, mgtClient crclient.Client, hostedCluster *hyperv1.HostedCluster) {
		// Sanity check the cluster by waiting for the nodes to report ready
		guestClient := e2eutil.WaitForGuestClient(t, ctx, mgtClient, hostedCluster)

		// Wait for nodes to be ready
		numNodes := clusterOpts.NodePoolReplicas
		_ = e2eutil.WaitForNReadyNodes(t, ctx, guestClient, numNodes, hostedCluster.Spec.Platform.Type)

		t.Logf("Azure override test cluster created successfully")
	}).Execute(&clusterOpts, globalOpts.Platform, globalOpts.ArtifactDir, "azure-override", globalOpts.ServiceAccountSigningKey)
}
