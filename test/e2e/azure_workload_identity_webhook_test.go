//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	"github.com/openshift/hypershift/hypershift-operator/controllers/manifests"
	e2eutil "github.com/openshift/hypershift/test/e2e/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	wiAnnotationClientID = "azure.workload.identity/client-id"
	wiLabelUse           = "azure.workload.identity/use"

	wiEnvClientID           = "AZURE_CLIENT_ID"
	wiEnvTenantID           = "AZURE_TENANT_ID"
	wiEnvFederatedTokenFile = "AZURE_FEDERATED_TOKEN_FILE"

	wiTestClientID = "test-client-id"
)

func TestAzureWorkloadIdentityWebhook(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(testContext)
	defer cancel()

	clusterOpts := globalOpts.DefaultClusterOptions(t)

	if globalOpts.Platform != "Azure" {
		t.Skip("test requires Azure platform")
	}

	e2eutil.NewHypershiftTest(t, ctx, func(t *testing.T, g Gomega, mgtClient crclient.Client, hostedCluster *hyperv1.HostedCluster) {
		guestClient := e2eutil.WaitForGuestClient(t, ctx, mgtClient, hostedCluster)

		t.Run("When pod opts in to workload identity it should get Azure credentials injected", func(t *testing.T) {
			testOptedInPodGetCredentials(t, ctx, guestClient)
		})

		t.Run("When pod does not opt in it should not be mutated", func(t *testing.T) {
			testNonOptedInPodNotMutated(t, ctx, guestClient)
		})

		t.Run("When control plane restarts it should continue injecting credentials", func(t *testing.T) {
			testCredentialInjectionSurvivesRestart(t, ctx, mgtClient, guestClient, hostedCluster)
		})
	}).Execute(&clusterOpts, globalOpts.Platform, globalOpts.ArtifactDir,
		"azure-workload-identity-webhook", globalOpts.ServiceAccountSigningKey)
}

func testOptedInPodGetCredentials(t *testing.T, ctx context.Context, guestClient crclient.Client) {
	g := NewWithT(t)
	ns := createWITestNamespace(t, ctx, g, guestClient, "e2e-wi-webhook-optin")
	defer deleteWITestNamespace(t, ctx, guestClient, ns)

	sa := createWIAnnotatedServiceAccount(t, ctx, g, guestClient, ns, "wi-sa", map[string]string{
		wiAnnotationClientID: wiTestClientID,
	})

	pod := createWIOptedInPod(t, ctx, g, guestClient, ns, "wi-pod", sa.Name)
	assertPodHasWorkloadIdentityCredentials(t, ctx, guestClient, pod)
}

func testNonOptedInPodNotMutated(t *testing.T, ctx context.Context, guestClient crclient.Client) {
	g := NewWithT(t)
	ns := createWITestNamespace(t, ctx, g, guestClient, "e2e-wi-webhook-nooptin")
	defer deleteWITestNamespace(t, ctx, guestClient, ns)

	sa := createWIAnnotatedServiceAccount(t, ctx, g, guestClient, ns, "wi-sa", map[string]string{
		wiAnnotationClientID: wiTestClientID,
	})

	pod := createWINonOptedInPod(t, ctx, g, guestClient, ns, "wi-pod-nooptin", sa.Name)
	assertPodDoesNotHaveWorkloadIdentityCredentials(t, ctx, guestClient, pod)
}

func testCredentialInjectionSurvivesRestart(t *testing.T, ctx context.Context, mgtClient, guestClient crclient.Client, hostedCluster *hyperv1.HostedCluster) {
	g := NewWithT(t)

	controlPlaneNamespace := manifests.HostedControlPlaneNamespace(hostedCluster.Namespace, hostedCluster.Name)

	kasPods := &corev1.PodList{}
	err := mgtClient.List(ctx, kasPods,
		crclient.InNamespace(controlPlaneNamespace),
		crclient.MatchingLabels{"app": "kube-apiserver"},
	)
	g.Expect(err).NotTo(HaveOccurred(), "failed to list kube-apiserver pods")
	t.Logf("Found %d kube-apiserver pod(s) to delete", len(kasPods.Items))

	for i := range kasPods.Items {
		err := mgtClient.Delete(ctx, &kasPods.Items[i])
		g.Expect(err).NotTo(HaveOccurred(), "failed to delete kube-apiserver pod %s", kasPods.Items[i].Name)
		t.Logf("Deleted kube-apiserver pod %s", kasPods.Items[i].Name)
	}

	e2eutil.EventuallyObject(t, ctx, "kube-apiserver pod is running with all containers ready",
		func(ctx context.Context) (*corev1.Pod, error) {
			podList := &corev1.PodList{}
			err := mgtClient.List(ctx, podList,
				crclient.InNamespace(controlPlaneNamespace),
				crclient.MatchingLabels{"app": "kube-apiserver"},
			)
			if err != nil {
				return nil, err
			}
			if len(podList.Items) == 0 {
				return nil, fmt.Errorf("no kube-apiserver pods found")
			}
			return &podList.Items[0], nil
		},
		[]e2eutil.Predicate[*corev1.Pod]{
			func(pod *corev1.Pod) (done bool, reasons string, err error) {
				if pod.Status.Phase != corev1.PodRunning {
					return false, fmt.Sprintf("pod phase is %s, expected Running", pod.Status.Phase), nil
				}
				return true, "pod is running", nil
			},
			func(pod *corev1.Pod) (done bool, reasons string, err error) {
				for _, cs := range pod.Status.ContainerStatuses {
					if !cs.Ready {
						return false, fmt.Sprintf("container %s is not ready", cs.Name), nil
					}
				}
				return true, "all containers are ready", nil
			},
		},
		e2eutil.WithTimeout(5*time.Minute),
		e2eutil.WithInterval(10*time.Second),
	)

	ns := createWITestNamespace(t, ctx, g, guestClient, "e2e-wi-webhook-restart")
	defer deleteWITestNamespace(t, ctx, guestClient, ns)

	sa := createWIAnnotatedServiceAccount(t, ctx, g, guestClient, ns, "wi-sa", map[string]string{
		wiAnnotationClientID: wiTestClientID,
	})

	pod := createWIOptedInPod(t, ctx, g, guestClient, ns, "wi-pod-restart", sa.Name)
	assertPodHasWorkloadIdentityCredentials(t, ctx, guestClient, pod)
}

func createWITestNamespace(t *testing.T, ctx context.Context, g Gomega, client crclient.Client, name string) string {
	t.Helper()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	err := client.Create(ctx, ns)
	g.Expect(err).NotTo(HaveOccurred(), "failed to create namespace %s", name)
	t.Logf("Created namespace %s", name)
	return name
}

func deleteWITestNamespace(t *testing.T, ctx context.Context, client crclient.Client, name string) {
	t.Helper()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	if err := client.Delete(ctx, ns); err != nil {
		t.Logf("Warning: failed to delete namespace %s: %v", name, err)
	} else {
		t.Logf("Deleted namespace %s", name)
	}
}

func createWIAnnotatedServiceAccount(t *testing.T, ctx context.Context, g Gomega, client crclient.Client, namespace, name string, annotations map[string]string) *corev1.ServiceAccount {
	t.Helper()
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
	}
	err := client.Create(ctx, sa)
	g.Expect(err).NotTo(HaveOccurred(), "failed to create service account %s/%s", namespace, name)
	return sa
}

func createWIOptedInPod(t *testing.T, ctx context.Context, g Gomega, client crclient.Client, namespace, name, saName string) *corev1.Pod {
	t.Helper()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				wiLabelUse: "true",
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: saName,
			Containers: []corev1.Container{
				{
					Name:    "busybox",
					Image:   "registry.k8s.io/e2e-test-images/busybox:1.36.1-1",
					Command: []string{"sleep", "3600"},
				},
			},
		},
	}
	err := client.Create(ctx, pod)
	g.Expect(err).NotTo(HaveOccurred(), "failed to create opted-in pod %s/%s", namespace, name)
	return pod
}

func createWINonOptedInPod(t *testing.T, ctx context.Context, g Gomega, client crclient.Client, namespace, name, saName string) *corev1.Pod {
	t.Helper()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: saName,
			Containers: []corev1.Container{
				{
					Name:    "busybox",
					Image:   "registry.k8s.io/e2e-test-images/busybox:1.36.1-1",
					Command: []string{"sleep", "3600"},
				},
			},
		},
	}
	err := client.Create(ctx, pod)
	g.Expect(err).NotTo(HaveOccurred(), "failed to create non-opted-in pod %s/%s", namespace, name)
	return pod
}

func assertPodHasWorkloadIdentityCredentials(t *testing.T, ctx context.Context, client crclient.Client, pod *corev1.Pod) {
	t.Helper()
	e2eutil.EventuallyObject(t, ctx, fmt.Sprintf("pod %s/%s has workload identity credentials", pod.Namespace, pod.Name),
		func(ctx context.Context) (*corev1.Pod, error) {
			p := &corev1.Pod{}
			err := client.Get(ctx, crclient.ObjectKeyFromObject(pod), p)
			return p, err
		},
		[]e2eutil.Predicate[*corev1.Pod]{
			func(p *corev1.Pod) (done bool, reasons string, err error) {
				val, found := getWIEnvValue(p, wiEnvClientID)
				if !found {
					return false, fmt.Sprintf("env var %s not found", wiEnvClientID), nil
				}
				if val != wiTestClientID {
					return false, fmt.Sprintf("expected %s=%q, got %q", wiEnvClientID, wiTestClientID, val), nil
				}
				return true, fmt.Sprintf("%s=%s", wiEnvClientID, val), nil
			},
			func(p *corev1.Pod) (done bool, reasons string, err error) {
				val, found := getWIEnvValue(p, wiEnvTenantID)
				if !found || val == "" {
					return false, fmt.Sprintf("env var %s not found or empty", wiEnvTenantID), nil
				}
				return true, fmt.Sprintf("%s is set", wiEnvTenantID), nil
			},
			func(p *corev1.Pod) (done bool, reasons string, err error) {
				val, found := getWIEnvValue(p, wiEnvFederatedTokenFile)
				if !found || val == "" {
					return false, fmt.Sprintf("env var %s not found or empty", wiEnvFederatedTokenFile), nil
				}
				return true, fmt.Sprintf("%s is set", wiEnvFederatedTokenFile), nil
			},
			func(p *corev1.Pod) (done bool, reasons string, err error) {
				if hasWIProjectedTokenVolume(p) {
					return true, "projected token volume found", nil
				}
				return false, "projected token volume not found", nil
			},
		},
		e2eutil.WithTimeout(2*time.Minute),
		e2eutil.WithInterval(5*time.Second),
	)
}

func assertPodDoesNotHaveWorkloadIdentityCredentials(t *testing.T, ctx context.Context, client crclient.Client, pod *corev1.Pod) {
	t.Helper()
	e2eutil.EventuallyObject(t, ctx, fmt.Sprintf("pod %s/%s exists without workload identity credentials", pod.Namespace, pod.Name),
		func(ctx context.Context) (*corev1.Pod, error) {
			p := &corev1.Pod{}
			err := client.Get(ctx, crclient.ObjectKeyFromObject(pod), p)
			return p, err
		},
		[]e2eutil.Predicate[*corev1.Pod]{
			func(p *corev1.Pod) (done bool, reasons string, err error) {
				_, found := getWIEnvValue(p, wiEnvClientID)
				if found {
					return false, fmt.Sprintf("env var %s should not be present", wiEnvClientID), nil
				}
				return true, fmt.Sprintf("%s not present (expected)", wiEnvClientID), nil
			},
			func(p *corev1.Pod) (done bool, reasons string, err error) {
				if hasWIProjectedTokenVolume(p) {
					return false, "projected token volume should not be present", nil
				}
				return true, "projected token volume not present (expected)", nil
			},
		},
		e2eutil.WithTimeout(2*time.Minute),
		e2eutil.WithInterval(5*time.Second),
	)
}

func getWIEnvValue(pod *corev1.Pod, envName string) (string, bool) {
	for _, c := range pod.Spec.Containers {
		for _, env := range c.Env {
			if env.Name == envName {
				return env.Value, true
			}
		}
	}
	return "", false
}

func hasWIProjectedTokenVolume(pod *corev1.Pod) bool {
	for _, v := range pod.Spec.Volumes {
		if v.Projected != nil {
			for _, src := range v.Projected.Sources {
				if src.ServiceAccountToken != nil {
					return true
				}
			}
		}
	}
	return false
}
