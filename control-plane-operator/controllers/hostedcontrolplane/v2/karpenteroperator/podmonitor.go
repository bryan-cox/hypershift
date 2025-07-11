package karpenteroperator

import (
	component "github.com/openshift/hypershift/support/controlplane-component"
	"github.com/openshift/hypershift/support/util"

	prometheusoperatorv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
)

func adaptPodMonitor(cpContext component.WorkloadContext, podMonitor *prometheusoperatorv1.PodMonitor) error {
	podMonitor.Spec.NamespaceSelector = prometheusoperatorv1.NamespaceSelector{MatchNames: []string{cpContext.HCP.Namespace}}
	util.ApplyClusterIDLabelToPodMonitor(&podMonitor.Spec.PodMetricsEndpoints[0], cpContext.HCP.Spec.ClusterID)

	return nil
}
