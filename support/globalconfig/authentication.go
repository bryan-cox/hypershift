package globalconfig

import (
	"fmt"

	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"

	configv1 "github.com/openshift/api/config/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	postInstallClientSecretSuffix = "post-install-client-secret"
)

func AuthenticationConfiguration() *configv1.Authentication {
	return &configv1.Authentication{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
	}
}

func ReconcileAuthenticationConfiguration(authentication *configv1.Authentication, config *hyperv1.ClusterConfiguration, issuerURL string) error {
	if config != nil && config.Authentication != nil {
		authentication.Spec = *config.Authentication
	}
	authentication.Spec.ServiceAccountIssuer = issuerURL
	for i := range authentication.Spec.OIDCProviders {
		for j, client := range authentication.Spec.OIDCProviders[i].OIDCClients {
			if client.ClientSecret.Name == "" {
				authentication.Spec.OIDCProviders[i].OIDCClients[j].ClientSecret.Name = fmt.Sprintf("%s-%s", client.ComponentName, postInstallClientSecretSuffix)
			}
		}
	}
	return nil
}
