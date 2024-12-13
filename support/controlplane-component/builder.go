package controlplanecomponent

import (
	"github.com/openshift/hypershift/support/util"

	appsv1 "k8s.io/api/apps/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type controlPlaneWorkloadBuilder[T client.Object] struct {
	workload *controlPlaneWorkload
}

func NewDeploymentComponent(name string, opts ComponentOptions) *controlPlaneWorkloadBuilder[*appsv1.Deployment] {
	return &controlPlaneWorkloadBuilder[*appsv1.Deployment]{
		workload: &controlPlaneWorkload{
			name:             name,
			workloadType:     deploymentWorkloadType,
			ComponentOptions: opts,
		},
	}
}

func NewStatefulSetComponent(name string, opts ComponentOptions) *controlPlaneWorkloadBuilder[*appsv1.StatefulSet] {
	return &controlPlaneWorkloadBuilder[*appsv1.StatefulSet]{
		workload: &controlPlaneWorkload{
			name:             name,
			workloadType:     statefulSetWorkloadType,
			ComponentOptions: opts,
		},
	}
}

func (b *controlPlaneWorkloadBuilder[T]) WithAdaptFunction(adapt func(cpContext WorkloadContext, obj T) error) *controlPlaneWorkloadBuilder[T] {
	b.workload.adapt = func(cpContext WorkloadContext, obj client.Object) error {
		return adapt(cpContext, obj.(T))
	}
	return b
}

func (b *controlPlaneWorkloadBuilder[T]) WithPredicate(predicate func(cpContext WorkloadContext) (bool, error)) *controlPlaneWorkloadBuilder[T] {
	b.workload.predicate = predicate
	return b
}

func (b *controlPlaneWorkloadBuilder[T]) WithManifestAdapter(manifestName string, opts ...option) *controlPlaneWorkloadBuilder[T] {
	adapter := &genericAdapter{}
	for _, opt := range opts {
		opt(adapter)
	}

	if b.workload.manifestsAdapters == nil {
		b.workload.manifestsAdapters = make(map[string]genericAdapter)
	}
	b.workload.manifestsAdapters[manifestName] = *adapter
	return b
}

func (b *controlPlaneWorkloadBuilder[T]) RolloutOnSecretChange(name ...string) *controlPlaneWorkloadBuilder[T] {
	b.workload.rolloutSecretsNames = append(b.workload.rolloutSecretsNames, name...)
	return b
}

func (b *controlPlaneWorkloadBuilder[T]) RolloutOnConfigMapChange(name ...string) *controlPlaneWorkloadBuilder[T] {
	b.workload.rolloutConfigMapsNames = append(b.workload.rolloutSecretsNames, name...)
	return b
}

func (b *controlPlaneWorkloadBuilder[T]) WithDependencies(dependencies ...string) *controlPlaneWorkloadBuilder[T] {
	b.workload.dependencies = append(b.workload.dependencies, dependencies...)
	return b
}

func (b *controlPlaneWorkloadBuilder[T]) InjectKonnectivityContainer(opts KonnectivityContainerOptions) *controlPlaneWorkloadBuilder[T] {
	b.workload.konnectivityContainerOpts = &opts
	return b
}

func (b *controlPlaneWorkloadBuilder[T]) InjectAvailabilityProberContainer(opts util.AvailabilityProberOpts) *controlPlaneWorkloadBuilder[T] {
	b.workload.availabilityProberOpts = &opts
	return b
}

func (b *controlPlaneWorkloadBuilder[T]) Build() ControlPlaneComponent {
	b.validate()
	return b.workload
}

func (b *controlPlaneWorkloadBuilder[T]) validate() {
	if b.workload.name == "" {
		panic("name is required")
	}
}
