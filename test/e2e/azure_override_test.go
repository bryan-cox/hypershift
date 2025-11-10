//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	"github.com/openshift/hypershift/hypershift-operator/controllers/manifests"
	controlplaneoperatoroverrides "github.com/openshift/hypershift/hypershift-operator/controlplaneoperator-overrides"
	e2eutil "github.com/openshift/hypershift/test/e2e/util"
	corev1 "k8s.io/api/core/v1"
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

		// Verify the CPO override image is actually being used
		controlPlaneNamespace := manifests.HostedControlPlaneNamespace(hostedCluster.Namespace, hostedCluster.Name)
		verifyCPOOverrideImage(t, ctx, mgtClient, controlPlaneNamespace, clusterOpts.ReleaseImage, string(globalOpts.Platform))

		t.Logf("Azure override test cluster created successfully with correct CPO image")
	}).Execute(&clusterOpts, globalOpts.Platform, globalOpts.ArtifactDir, "azure-override", globalOpts.ServiceAccountSigningKey)
}

// verifyCPOOverrideImage verifies that the control-plane-operator pod is running
// with the expected override image from overrides.yaml
func verifyCPOOverrideImage(t *testing.T, ctx context.Context, mgtClient crclient.Client, controlPlaneNamespace, releaseImage, platform string) {
	// Extract version from release image (e.g., "quay.io/openshift-release-dev/ocp-release:4.19.10-x86_64" -> "4.19.10")
	version := extractVersionFromReleaseImage(releaseImage)
	if version == "" {
		t.Fatalf("Failed to extract version from release image: %s", releaseImage)
	}

	// Get expected CPO override image from overrides.yaml
	expectedImage := controlplaneoperatoroverrides.CPOImage(platform, version)
	if expectedImage == "" {
		t.Fatalf("No CPO override found for platform %s and version %s", platform, version)
	}

	t.Logf("Expecting CPO image: %s (platform: %s, version: %s)", expectedImage, platform, version)

	e2eutil.EventuallyObject(t, ctx, "control-plane-operator pod is running with expected override image",
		func(ctx context.Context) (*corev1.Pod, error) {
			podList := &corev1.PodList{}
			err := mgtClient.List(ctx, podList, crclient.InNamespace(controlPlaneNamespace), crclient.MatchingLabels{"app": "control-plane-operator"})
			if err != nil {
				return nil, err
			}

			if len(podList.Items) == 0 {
				return nil, fmt.Errorf("no pods found for control-plane-operator")
			}

			// Return the first running pod
			for i := range podList.Items {
				if podList.Items[i].Status.Phase == corev1.PodRunning {
					return &podList.Items[i], nil
				}
			}

			return nil, fmt.Errorf("no running control-plane-operator pods found")
		},
		[]e2eutil.Predicate[*corev1.Pod]{
			func(pod *corev1.Pod) (done bool, reasons string, err error) {
				if pod.Status.Phase != corev1.PodRunning {
					return false, fmt.Sprintf("pod is not running, phase: %s", pod.Status.Phase), nil
				}
				return true, "pod is running", nil
			},
			func(pod *corev1.Pod) (done bool, reasons string, err error) {
				if len(pod.Spec.Containers) == 0 {
					return false, "no containers found in pod spec", nil
				}

				actualImage := pod.Spec.Containers[0].Image
				if actualImage != expectedImage {
					return false, fmt.Sprintf("expected CPO image %s, got %s", expectedImage, actualImage), nil
				}

				return true, fmt.Sprintf("CPO override image verified: %s", actualImage), nil
			},
		}, e2eutil.WithTimeout(5*time.Minute), e2eutil.WithInterval(10*time.Second),
	)
}

// extractVersionFromReleaseImage extracts the version from a release image reference.
// For example: "quay.io/openshift-release-dev/ocp-release:4.19.10-x86_64" -> "4.19.10"
func extractVersionFromReleaseImage(releaseImage string) string {
	// Split by ':' to get the tag
	parts := strings.Split(releaseImage, ":")
	if len(parts) != 2 {
		return ""
	}

	tag := parts[1]

	// Remove architecture suffix (e.g., "-x86_64", "-multi")
	// Split by '-' and take all parts except the last one (architecture)
	tagParts := strings.Split(tag, "-")
	if len(tagParts) < 2 {
		return tag // No architecture suffix, return as-is
	}

	// Check if last part looks like an architecture
	lastPart := tagParts[len(tagParts)-1]
	if lastPart == "x86_64" || lastPart == "amd64" || lastPart == "arm64" || lastPart == "ppc64le" || lastPart == "s390x" || lastPart == "multi" {
		// Join all parts except the last one
		return strings.Join(tagParts[:len(tagParts)-1], "-")
	}

	// No known architecture suffix found, return the tag as-is
	return tag
}
