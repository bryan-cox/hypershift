package nodepool

import (
	"fmt"
	"strings"
	"testing"

	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	"github.com/openshift/hypershift/support/api"
	"github.com/openshift/hypershift/support/releaseinfo"

	imagev1 "github.com/openshift/api/image/v1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// BenchmarkNewConfigGenerator measures the config and rollout hashes generated
// from three core configs and twelve user MachineConfigs. The fake client and
// fixtures are built outside the timer so the profile covers config generation,
// not test setup or manifest construction.
func BenchmarkNewConfigGenerator(b *testing.B) {
	const (
		clusterNamespace      = "clusters"
		controlPlaneNamespace = "clusters-example"
		userConfigMaps        = 4
		manifestsPerConfigMap = 3
	)

	manifest := func(name string) string {
		return fmt.Sprintf(`apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  name: %s
spec:
  config:
    ignition:
      version: 3.2.0
    storage:
      files:
      - path: /etc/hypershift/%s
        mode: 420
        contents:
          source: data:text/plain;charset=utf-8;base64,SGVsbG8gV29ybGQ=
`, name, name)
	}

	var objects []client.Object
	for i := range 3 {
		objects = append(objects, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("core-%d", i),
				Namespace: controlPlaneNamespace,
				Labels:    map[string]string{nodePoolCoreIgnitionConfigLabel: "true"},
			},
			Data: map[string]string{TokenSecretConfigKey: manifest(fmt.Sprintf("core-%d", i))},
		})
	}

	nodePool := &hyperv1.NodePool{ObjectMeta: metav1.ObjectMeta{Namespace: clusterNamespace}}
	for i := range userConfigMaps {
		name := fmt.Sprintf("user-%d", i)
		var manifests []string
		for j := range manifestsPerConfigMap {
			manifests = append(manifests, manifest(fmt.Sprintf("%s-%d", name, j)))
		}
		objects = append(objects, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: clusterNamespace},
			Data:       map[string]string{TokenSecretConfigKey: strings.Join(manifests, "\n---\n")},
		})
		nodePool.Spec.Config = append(nodePool.Spec.Config, corev1.LocalObjectReference{Name: name})
	}

	cl := fake.NewClientBuilder().WithScheme(api.Scheme).WithObjects(objects...).Build()
	hostedCluster := &hyperv1.HostedCluster{
		Spec: hyperv1.HostedClusterSpec{PullSecret: corev1.LocalObjectReference{Name: "pull-secret"}},
	}
	releaseImage := &releaseinfo.ReleaseImage{ImageStream: &imagev1.ImageStream{
		ObjectMeta: metav1.ObjectMeta{Name: "4.22.0"},
	}}

	generate := func() (*ConfigGenerator, error) {
		return NewConfigGenerator(b.Context(), cl, hostedCluster, nodePool, releaseImage, "", controlPlaneNamespace, StreamRHEL9)
	}
	initial, err := generate()
	if err != nil {
		b.Fatal(err)
	}
	if initial.Hash() == "" || initial.RolloutHash() == "" || initial.mcoRawConfig == "" {
		b.Fatal("config generator produced empty config or hash")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		cg, err := generate()
		if err != nil {
			b.Fatal(err)
		}
		benchmarkConfigHash = cg.Hash()
		benchmarkRolloutHash = cg.RolloutHash()
	}
}

var benchmarkConfigHash, benchmarkRolloutHash string
