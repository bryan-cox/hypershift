package pki

import (
	"net"
	"testing"

	"github.com/openshift/hypershift/support/certs"
	"github.com/openshift/hypershift/support/config"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

func TestReconcileAzureWorkloadIdentityWebhookServingCert(t *testing.T) {
	t.Parallel()

	ownerRef := config.OwnerRef{
		Reference: &metav1.OwnerReference{
			APIVersion: "v1",
			Kind:       "Deployment",
			Name:       "test-owner",
			UID:        types.UID("test-uid-12345"),
			Controller: ptr.To(true),
		},
	}

	t.Run("When reconciling Azure webhook cert it should produce valid TLS cert with correct SAN", func(t *testing.T) {
		ca := &corev1.Secret{}
		ca.Name = "test-ca"
		ca.Namespace = "test-ns"
		if err := reconcileSelfSignedCA(ca, ownerRef, "test-ca", "test-ou"); err != nil {
			t.Fatalf("failed to create self-signed CA: %v", err)
		}

		secret := &corev1.Secret{}
		secret.Name = "azure-workload-identity-webhook-serving-cert"
		secret.Namespace = "test-ns"

		if err := ReconcileAzureWorkloadIdentityWebhookServingCert(secret, ca, ownerRef); err != nil {
			t.Fatalf("ReconcileAzureWorkloadIdentityWebhookServingCert failed: %v", err)
		}

		if len(secret.Data[corev1.TLSCertKey]) == 0 {
			t.Fatal("expected TLS cert data to be populated")
		}
		if len(secret.Data[corev1.TLSPrivateKeyKey]) == 0 {
			t.Fatal("expected TLS private key data to be populated")
		}

		cert, err := certs.PemToCertificate(secret.Data[corev1.TLSCertKey])
		if err != nil {
			t.Fatalf("failed to parse generated certificate: %v", err)
		}

		if cert.Subject.CommonName != "127.0.0.1" {
			t.Errorf("expected CN to be '127.0.0.1', got '%s'", cert.Subject.CommonName)
		}

		expectedIP := net.ParseIP("127.0.0.1")
		foundIP := false
		for _, ip := range cert.IPAddresses {
			if ip.Equal(expectedIP) {
				foundIP = true
				break
			}
		}
		if !foundIP {
			t.Errorf("expected IP SAN '127.0.0.1' not found in certificate IP addresses: %v", cert.IPAddresses)
		}

		if len(cert.DNSNames) > 0 {
			t.Errorf("expected no DNS SANs, got %v", cert.DNSNames)
		}

		if cert.IsCA {
			t.Error("expected certificate to not be a CA")
		}

		if len(secret.Data[certs.CASignerCertMapKey]) == 0 {
			t.Fatal("expected CA cert data to be present in the secret")
		}
	})

	t.Run("When reconciling Azure webhook cert twice it should be idempotent", func(t *testing.T) {
		ca := &corev1.Secret{}
		ca.Name = "test-ca"
		ca.Namespace = "test-ns"
		if err := reconcileSelfSignedCA(ca, ownerRef, "test-ca", "test-ou"); err != nil {
			t.Fatalf("failed to create self-signed CA: %v", err)
		}

		secret := &corev1.Secret{}
		secret.Name = "azure-workload-identity-webhook-serving-cert"
		secret.Namespace = "test-ns"

		if err := ReconcileAzureWorkloadIdentityWebhookServingCert(secret, ca, ownerRef); err != nil {
			t.Fatalf("first reconcile failed: %v", err)
		}

		originalCert := make([]byte, len(secret.Data[corev1.TLSCertKey]))
		copy(originalCert, secret.Data[corev1.TLSCertKey])
		originalKey := make([]byte, len(secret.Data[corev1.TLSPrivateKeyKey]))
		copy(originalKey, secret.Data[corev1.TLSPrivateKeyKey])

		if err := ReconcileAzureWorkloadIdentityWebhookServingCert(secret, ca, ownerRef); err != nil {
			t.Fatalf("second reconcile failed: %v", err)
		}

		if string(secret.Data[corev1.TLSCertKey]) != string(originalCert) {
			t.Error("expected certificate to remain unchanged on second reconcile")
		}
		if string(secret.Data[corev1.TLSPrivateKeyKey]) != string(originalKey) {
			t.Error("expected private key to remain unchanged on second reconcile")
		}
	})

	t.Run("When reconciling Azure webhook cert it should set owner reference", func(t *testing.T) {
		ca := &corev1.Secret{}
		ca.Name = "test-ca"
		ca.Namespace = "test-ns"
		if err := reconcileSelfSignedCA(ca, ownerRef, "test-ca", "test-ou"); err != nil {
			t.Fatalf("failed to create self-signed CA: %v", err)
		}

		secret := &corev1.Secret{}
		secret.Name = "azure-workload-identity-webhook-serving-cert"
		secret.Namespace = "test-ns"

		if err := ReconcileAzureWorkloadIdentityWebhookServingCert(secret, ca, ownerRef); err != nil {
			t.Fatalf("ReconcileAzureWorkloadIdentityWebhookServingCert failed: %v", err)
		}

		if len(secret.OwnerReferences) == 0 {
			t.Fatal("expected owner references to be set on the secret")
		}
		if secret.OwnerReferences[0].Name != "test-owner" {
			t.Errorf("expected owner reference name to be 'test-owner', got '%s'", secret.OwnerReferences[0].Name)
		}
	})
}
