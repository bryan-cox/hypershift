/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
// Code generated by client-gen. DO NOT EDIT.

package clientset

import (
	fmt "fmt"
	http "net/http"

	certificatesv1alpha1 "github.com/openshift/hypershift/client/clientset/clientset/typed/certificates/v1alpha1"
	hypershiftv1beta1 "github.com/openshift/hypershift/client/clientset/clientset/typed/hypershift/v1beta1"
	karpenterv1beta1 "github.com/openshift/hypershift/client/clientset/clientset/typed/karpenter/v1beta1"
	schedulingv1alpha1 "github.com/openshift/hypershift/client/clientset/clientset/typed/scheduling/v1alpha1"
	discovery "k8s.io/client-go/discovery"
	rest "k8s.io/client-go/rest"
	flowcontrol "k8s.io/client-go/util/flowcontrol"
)

type Interface interface {
	Discovery() discovery.DiscoveryInterface
	CertificatesV1alpha1() certificatesv1alpha1.CertificatesV1alpha1Interface
	HypershiftV1beta1() hypershiftv1beta1.HypershiftV1beta1Interface
	KarpenterV1beta1() karpenterv1beta1.KarpenterV1beta1Interface
	SchedulingV1alpha1() schedulingv1alpha1.SchedulingV1alpha1Interface
}

// Clientset contains the clients for groups.
type Clientset struct {
	*discovery.DiscoveryClient
	certificatesV1alpha1 *certificatesv1alpha1.CertificatesV1alpha1Client
	hypershiftV1beta1    *hypershiftv1beta1.HypershiftV1beta1Client
	karpenterV1beta1     *karpenterv1beta1.KarpenterV1beta1Client
	schedulingV1alpha1   *schedulingv1alpha1.SchedulingV1alpha1Client
}

// CertificatesV1alpha1 retrieves the CertificatesV1alpha1Client
func (c *Clientset) CertificatesV1alpha1() certificatesv1alpha1.CertificatesV1alpha1Interface {
	return c.certificatesV1alpha1
}

// HypershiftV1beta1 retrieves the HypershiftV1beta1Client
func (c *Clientset) HypershiftV1beta1() hypershiftv1beta1.HypershiftV1beta1Interface {
	return c.hypershiftV1beta1
}

// KarpenterV1beta1 retrieves the KarpenterV1beta1Client
func (c *Clientset) KarpenterV1beta1() karpenterv1beta1.KarpenterV1beta1Interface {
	return c.karpenterV1beta1
}

// SchedulingV1alpha1 retrieves the SchedulingV1alpha1Client
func (c *Clientset) SchedulingV1alpha1() schedulingv1alpha1.SchedulingV1alpha1Interface {
	return c.schedulingV1alpha1
}

// Discovery retrieves the DiscoveryClient
func (c *Clientset) Discovery() discovery.DiscoveryInterface {
	if c == nil {
		return nil
	}
	return c.DiscoveryClient
}

// NewForConfig creates a new Clientset for the given config.
// If config's RateLimiter is not set and QPS and Burst are acceptable,
// NewForConfig will generate a rate-limiter in configShallowCopy.
// NewForConfig is equivalent to NewForConfigAndClient(c, httpClient),
// where httpClient was generated with rest.HTTPClientFor(c).
func NewForConfig(c *rest.Config) (*Clientset, error) {
	configShallowCopy := *c

	if configShallowCopy.UserAgent == "" {
		configShallowCopy.UserAgent = rest.DefaultKubernetesUserAgent()
	}

	// share the transport between all clients
	httpClient, err := rest.HTTPClientFor(&configShallowCopy)
	if err != nil {
		return nil, err
	}

	return NewForConfigAndClient(&configShallowCopy, httpClient)
}

// NewForConfigAndClient creates a new Clientset for the given config and http client.
// Note the http client provided takes precedence over the configured transport values.
// If config's RateLimiter is not set and QPS and Burst are acceptable,
// NewForConfigAndClient will generate a rate-limiter in configShallowCopy.
func NewForConfigAndClient(c *rest.Config, httpClient *http.Client) (*Clientset, error) {
	configShallowCopy := *c
	if configShallowCopy.RateLimiter == nil && configShallowCopy.QPS > 0 {
		if configShallowCopy.Burst <= 0 {
			return nil, fmt.Errorf("burst is required to be greater than 0 when RateLimiter is not set and QPS is set to greater than 0")
		}
		configShallowCopy.RateLimiter = flowcontrol.NewTokenBucketRateLimiter(configShallowCopy.QPS, configShallowCopy.Burst)
	}

	var cs Clientset
	var err error
	cs.certificatesV1alpha1, err = certificatesv1alpha1.NewForConfigAndClient(&configShallowCopy, httpClient)
	if err != nil {
		return nil, err
	}
	cs.hypershiftV1beta1, err = hypershiftv1beta1.NewForConfigAndClient(&configShallowCopy, httpClient)
	if err != nil {
		return nil, err
	}
	cs.karpenterV1beta1, err = karpenterv1beta1.NewForConfigAndClient(&configShallowCopy, httpClient)
	if err != nil {
		return nil, err
	}
	cs.schedulingV1alpha1, err = schedulingv1alpha1.NewForConfigAndClient(&configShallowCopy, httpClient)
	if err != nil {
		return nil, err
	}

	cs.DiscoveryClient, err = discovery.NewDiscoveryClientForConfigAndClient(&configShallowCopy, httpClient)
	if err != nil {
		return nil, err
	}
	return &cs, nil
}

// NewForConfigOrDie creates a new Clientset for the given config and
// panics if there is an error in the config.
func NewForConfigOrDie(c *rest.Config) *Clientset {
	cs, err := NewForConfig(c)
	if err != nil {
		panic(err)
	}
	return cs
}

// New creates a new Clientset for the given RESTClient.
func New(c rest.Interface) *Clientset {
	var cs Clientset
	cs.certificatesV1alpha1 = certificatesv1alpha1.New(c)
	cs.hypershiftV1beta1 = hypershiftv1beta1.New(c)
	cs.karpenterV1beta1 = karpenterv1beta1.New(c)
	cs.schedulingV1alpha1 = schedulingv1alpha1.New(c)

	cs.DiscoveryClient = discovery.NewDiscoveryClient(c)
	return &cs
}
