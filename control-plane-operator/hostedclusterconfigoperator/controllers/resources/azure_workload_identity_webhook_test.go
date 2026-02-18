package resources

import (
	"testing"

	. "github.com/onsi/gomega"

	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	"github.com/openshift/hypershift/control-plane-operator/hostedclusterconfigoperator/api"
	"github.com/openshift/hypershift/control-plane-operator/hostedclusterconfigoperator/controllers/resources/manifests"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap/zaptest"
)

func TestReconcileAzureWorkloadIdentityWebhook(t *testing.T) {
	t.Run("When reconciling Azure webhook it should create all guest cluster resources with correct specs", func(t *testing.T) {
		g := NewWithT(t)
		ctx := logr.NewContext(t.Context(), zapr.NewLogger(zaptest.NewLogger(t)))

		testRootCA := "test-root-ca-bundle"
		guestClient := fake.NewClientBuilder().
			WithScheme(api.Scheme).
			Build()

		r := &reconciler{
			client:                 guestClient,
			CreateOrUpdateProvider: &simpleCreateOrUpdater{},
			rootCA:                 testRootCA,
			platformType:           hyperv1.AzurePlatform,
		}

		errs := r.reconcileAzureWorkloadIdentityWebhook(ctx)
		g.Expect(errs).To(BeEmpty(), "expected no errors from reconcileAzureWorkloadIdentityWebhook")

		// Verify MutatingWebhookConfiguration
		webhookConfig := manifests.AzureWorkloadIdentityWebhook()
		err := guestClient.Get(ctx, client.ObjectKeyFromObject(webhookConfig), webhookConfig)
		g.Expect(err).ToNot(HaveOccurred(), "expected MutatingWebhookConfiguration to be created")
		g.Expect(webhookConfig.Name).To(Equal("azure-workload-identity"))
		g.Expect(webhookConfig.Webhooks).To(HaveLen(1))

		wh := webhookConfig.Webhooks[0]
		g.Expect(wh.Name).To(Equal("azure-wi-webhook.azure.workload.identity"))
		g.Expect(wh.AdmissionReviewVersions).To(Equal([]string{"v1"}))
		g.Expect(wh.ClientConfig.CABundle).To(Equal([]byte(testRootCA)))
		g.Expect(wh.ClientConfig.URL).ToNot(BeNil())
		g.Expect(*wh.ClientConfig.URL).To(Equal("https://127.0.0.1:9443/mutate-v1-pod"))
		g.Expect(wh.FailurePolicy).ToNot(BeNil())
		g.Expect(*wh.FailurePolicy).To(Equal(admissionregistrationv1.Ignore))
		g.Expect(wh.Rules).To(HaveLen(1))
		g.Expect(wh.Rules[0].Operations).To(Equal([]admissionregistrationv1.OperationType{admissionregistrationv1.Create}))
		g.Expect(wh.Rules[0].Rule.APIGroups).To(Equal([]string{""}))
		g.Expect(wh.Rules[0].Rule.APIVersions).To(Equal([]string{"v1"}))
		g.Expect(wh.Rules[0].Rule.Resources).To(Equal([]string{"pods"}))
		g.Expect(wh.SideEffects).ToNot(BeNil())
		g.Expect(*wh.SideEffects).To(Equal(admissionregistrationv1.SideEffectClassNone))

		// Verify ClusterRole
		clusterRole := manifests.AzureWorkloadIdentityWebhookClusterRole()
		err = guestClient.Get(ctx, client.ObjectKeyFromObject(clusterRole), clusterRole)
		g.Expect(err).ToNot(HaveOccurred(), "expected ClusterRole to be created")
		g.Expect(clusterRole.Name).To(Equal("azure-workload-identity-webhook"))
		g.Expect(clusterRole.Rules).To(HaveLen(1))
		g.Expect(clusterRole.Rules[0].APIGroups).To(Equal([]string{""}))
		g.Expect(clusterRole.Rules[0].Resources).To(Equal([]string{"serviceaccounts"}))
		g.Expect(clusterRole.Rules[0].Verbs).To(Equal([]string{"get", "list", "watch"}))

		// Verify ClusterRoleBinding
		clusterRoleBinding := manifests.AzureWorkloadIdentityWebhookClusterRoleBinding()
		err = guestClient.Get(ctx, client.ObjectKeyFromObject(clusterRoleBinding), clusterRoleBinding)
		g.Expect(err).ToNot(HaveOccurred(), "expected ClusterRoleBinding to be created")
		g.Expect(clusterRoleBinding.Name).To(Equal("azure-workload-identity-webhook"))
		g.Expect(clusterRoleBinding.RoleRef).To(Equal(rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "azure-workload-identity-webhook",
		}))
		g.Expect(clusterRoleBinding.Subjects).To(HaveLen(1))
		g.Expect(clusterRoleBinding.Subjects[0]).To(Equal(rbacv1.Subject{
			Kind:      "ServiceAccount",
			Name:      "azure-workload-identity-webhook",
			Namespace: "openshift-cloud-credential-operator",
		}))
	})

	t.Run("When platform is not Azure it should not have webhook resources", func(t *testing.T) {
		g := NewWithT(t)
		ctx := logr.NewContext(t.Context(), zapr.NewLogger(zaptest.NewLogger(t)))

		platforms := []hyperv1.PlatformType{
			hyperv1.AWSPlatform,
			hyperv1.NonePlatform,
			hyperv1.KubevirtPlatform,
		}

		for _, platform := range platforms {
			t.Run(string(platform), func(t *testing.T) {
				guestClient := fake.NewClientBuilder().
					WithScheme(api.Scheme).
					Build()

				// Verify the resources don't exist (no reconcileAzureWorkloadIdentityWebhook called)
				webhookConfig := manifests.AzureWorkloadIdentityWebhook()
				err := guestClient.Get(ctx, client.ObjectKeyFromObject(webhookConfig), webhookConfig)
				g.Expect(err).To(HaveOccurred(), "expected MutatingWebhookConfiguration to not exist for %s", platform)

				clusterRole := manifests.AzureWorkloadIdentityWebhookClusterRole()
				err = guestClient.Get(ctx, client.ObjectKeyFromObject(clusterRole), clusterRole)
				g.Expect(err).To(HaveOccurred(), "expected ClusterRole to not exist for %s", platform)

				clusterRoleBinding := manifests.AzureWorkloadIdentityWebhookClusterRoleBinding()
				err = guestClient.Get(ctx, client.ObjectKeyFromObject(clusterRoleBinding), clusterRoleBinding)
				g.Expect(err).To(HaveOccurred(), "expected ClusterRoleBinding to not exist for %s", platform)
			})
		}
	})
}
