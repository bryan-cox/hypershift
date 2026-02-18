package kas

import (
	"context"
	"testing"

	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	"github.com/openshift/hypershift/control-plane-operator/controllers/hostedcontrolplane/manifests"
	"github.com/openshift/hypershift/support/certs"
	component "github.com/openshift/hypershift/support/controlplane-component"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testKubeconfigNamespace = "test-hcp-namespace"

func newTestCSRSignerCA(t *testing.T, namespace string) *corev1.Secret {
	t.Helper()
	secret := manifests.CSRSignerCASecret(namespace)
	secret.Data = map[string][]byte{}
	if err := certs.ReconcileSelfSignedCA(secret, "test-csr-signer", "test"); err != nil {
		t.Fatalf("failed to generate test CSR signer CA: %v", err)
	}
	return secret
}

func newTestRootCA(t *testing.T, namespace string) *corev1.Secret {
	t.Helper()
	secret := manifests.RootCASecret(namespace)
	secret.Data = map[string][]byte{}
	if err := certs.ReconcileSelfSignedCA(secret, "test-root-ca", "test"); err != nil {
		t.Fatalf("failed to generate test root CA: %v", err)
	}
	return secret
}

func TestAdaptAzureWorkloadIdentityWebhookKubeconfigSecret(t *testing.T) {
	testCases := []struct {
		name                   string
		skipCertificateSigning bool
		setupObjects           func(t *testing.T) []corev1.Secret
		expectError            bool
		expectNilResult        bool
	}{
		{
			name:                   "When SkipCertificateSigning is true it should return nil",
			skipCertificateSigning: true,
			setupObjects: func(t *testing.T) []corev1.Secret {
				return []corev1.Secret{
					*newTestCSRSignerCA(t, testKubeconfigNamespace),
					*newTestRootCA(t, testKubeconfigNamespace),
				}
			},
			expectNilResult: true,
		},
		{
			name: "When CSR signer secret is missing it should return an error",
			setupObjects: func(t *testing.T) []corev1.Secret {
				return nil
			},
			expectError: true,
		},
		{
			name: "When root CA secret is missing it should return an error",
			setupObjects: func(t *testing.T) []corev1.Secret {
				return []corev1.Secret{*newTestCSRSignerCA(t, testKubeconfigNamespace)}
			},
			expectError: true,
		},
		{
			name: "When adapting Azure webhook kubeconfig it should generate correct SA binding",
			setupObjects: func(t *testing.T) []corev1.Secret {
				return []corev1.Secret{
					*newTestCSRSignerCA(t, testKubeconfigNamespace),
					*newTestRootCA(t, testKubeconfigNamespace),
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			hcp := &hyperv1.HostedControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hcp",
					Namespace: testKubeconfigNamespace,
				},
				Spec: hyperv1.HostedControlPlaneSpec{
					Platform: hyperv1.PlatformSpec{
						Type: hyperv1.AzurePlatform,
					},
				},
			}

			clientBuilder := fake.NewClientBuilder()
			if tc.setupObjects != nil {
				objects := tc.setupObjects(t)
				for i := range objects {
					clientBuilder = clientBuilder.WithObjects(&objects[i])
				}
			}

			cpContext := component.WorkloadContext{
				Context:                context.Background(),
				Client:                 clientBuilder.Build(),
				HCP:                    hcp,
				SkipCertificateSigning: tc.skipCertificateSigning,
			}

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "azure-workload-identity-webhook-kubeconfig",
					Namespace: testKubeconfigNamespace,
				},
			}

			err := adaptAzureWorkloadIdentityWebhookKubeconfigSecret(cpContext, secret)

			if tc.expectError {
				if err == nil {
					t.Fatal("expected an error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.expectNilResult {
				if secret.Data != nil && len(secret.Data[KubeconfigKey]) > 0 {
					t.Fatal("expected secret data to be empty when SkipCertificateSigning is true")
				}
				return
			}

			kubeconfigData, ok := secret.Data[KubeconfigKey]
			if !ok || len(kubeconfigData) == 0 {
				t.Fatal("expected kubeconfig data in secret but it was empty or missing")
			}

			kubecfg, err := clientcmd.Load(kubeconfigData)
			if err != nil {
				t.Fatalf("failed to parse generated kubeconfig: %v", err)
			}

			cluster, ok := kubecfg.Clusters["cluster"]
			if !ok {
				t.Fatal("expected 'cluster' entry in kubeconfig clusters")
			}
			expectedURL := InClusterKASURL(hyperv1.AzurePlatform)
			if cluster.Server != expectedURL {
				t.Fatalf("expected cluster server URL %q, got %q", expectedURL, cluster.Server)
			}

			authInfo, ok := kubecfg.AuthInfos["admin"]
			if !ok {
				t.Fatal("expected 'admin' entry in kubeconfig auth infos")
			}
			if len(authInfo.ClientCertificateData) == 0 {
				t.Fatal("expected client certificate data in kubeconfig auth info")
			}
			if len(authInfo.ClientKeyData) == 0 {
				t.Fatal("expected client key data in kubeconfig auth info")
			}
		})
	}
}
