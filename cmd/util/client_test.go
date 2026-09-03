package util

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"

	hyperapi "github.com/openshift/hypershift/support/api"

	"k8s.io/client-go/kubernetes"
	fakekubeclient "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func writeTestKubeconfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	kubeconfigFile := filepath.Join(dir, "kubeconfig")
	content := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://localhost:6443
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user:
    token: test-token
`
	err := os.WriteFile(kubeconfigFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to write kubeconfig: %v", err)
	}
	return kubeconfigFile
}

func TestGetConfig(t *testing.T) {
	t.Run("When KUBECONFIG env var points to a valid kubeconfig, it should create a config", func(t *testing.T) {
		g := NewWithT(t)
		t.Setenv("KUBECONFIG", writeTestKubeconfig(t))
		cfg, err := GetConfig()
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(cfg).ToNot(BeNil())
		g.Expect(cfg.QPS).To(Equal(float32(100)))
		g.Expect(cfg.Burst).To(Equal(100))
	})
}

func TestClientProvider(t *testing.T) {
	g := NewWithT(t)
	controllerClient := fake.NewClientBuilder().WithScheme(hyperapi.Scheme).Build()
	typedClient := fakekubeclient.NewClientset()
	config := &rest.Config{Host: "https://example.com"}
	provider := &ClientProvider{
		ControllerRuntimeClient: func(_ string) (client.Client, error) {
			return controllerClient, nil
		},
		KubernetesClientSet: func(_ string) (kubernetes.Interface, error) {
			return typedClient, nil
		},
		Config: func(_ string) (*rest.Config, error) {
			return config, nil
		},
		ImpersonatedClient: func(_ string) (client.Client, error) {
			return controllerClient, nil
		},
	}

	gotControllerClient, err := provider.ControllerRuntimeClientFor("")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(gotControllerClient).To(Equal(controllerClient))
	gotTypedClient, err := provider.KubernetesClientSetFor("")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(gotTypedClient).To(Equal(typedClient))
	gotConfig, err := provider.ConfigFor("")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(gotConfig).To(Equal(config))
	gotImpersonatedClient, err := provider.ImpersonatedClientFor("test-user")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(gotImpersonatedClient).To(Equal(controllerClient))
}

func TestGetConfigWithKubeconfig(t *testing.T) {
	tests := []struct {
		name             string
		kubeconfigPath   string
		useHelper        bool
		setKubeconfigEnv bool
		expectError      bool
		errorContains    string
	}{
		{
			name:             "When kubeconfig path is empty, it should fall back to KUBECONFIG env var resolution",
			kubeconfigPath:   "",
			setKubeconfigEnv: true,
		},
		{
			name:           "When kubeconfig file does not exist, it should return an error",
			kubeconfigPath: "/nonexistent/path/kubeconfig",
			expectError:    true,
			errorContains:  "unable to build config from kubeconfig",
		},
		{
			name:        "When a valid kubeconfig file is provided, it should create a config with correct QPS and burst",
			useHelper:   true,
			expectError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			kubeconfigPath := tc.kubeconfigPath
			if tc.setKubeconfigEnv {
				t.Setenv("KUBECONFIG", writeTestKubeconfig(t))
			}
			if tc.useHelper {
				kubeconfigPath = writeTestKubeconfig(t)
			}

			cfg, err := GetConfigWithKubeconfig(kubeconfigPath)
			if tc.expectError {
				g.Expect(err).To(HaveOccurred())
				if tc.errorContains != "" {
					g.Expect(err.Error()).To(ContainSubstring(tc.errorContains))
				}
			} else {
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cfg).ToNot(BeNil())
				g.Expect(cfg.QPS).To(Equal(float32(100)))
				g.Expect(cfg.Burst).To(Equal(100))
				g.Expect(cfg.Host).To(Equal("https://localhost:6443"))
			}
		})
	}
}

func TestGetClientWithKubeconfig(t *testing.T) {
	tests := []struct {
		name           string
		kubeconfigPath string
		useHelper      bool
		expectError    bool
		errorContains  string
	}{
		{
			name:           "When kubeconfig file does not exist, it should return an error",
			kubeconfigPath: "/nonexistent/path/kubeconfig",
			expectError:    true,
			errorContains:  "unable to get kubernetes config",
		},
		{
			name:        "When a valid kubeconfig is provided, it should create a client",
			useHelper:   true,
			expectError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			kubeconfigPath := tc.kubeconfigPath
			if tc.useHelper {
				kubeconfigPath = writeTestKubeconfig(t)
			}

			client, err := GetClientWithKubeconfig(kubeconfigPath)
			if tc.expectError {
				g.Expect(err).To(HaveOccurred())
				if tc.errorContains != "" {
					g.Expect(err.Error()).To(ContainSubstring(tc.errorContains))
				}
			} else {
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(client).ToNot(BeNil())
			}
		})
	}
}

func TestGetKubernetesClientSetWithKubeconfig(t *testing.T) {
	g := NewWithT(t)
	client, err := GetKubernetesClientSetWithKubeconfig(writeTestKubeconfig(t))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(client).NotTo(BeNil())
}
