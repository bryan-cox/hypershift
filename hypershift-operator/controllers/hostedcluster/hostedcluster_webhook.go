package hostedcluster

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"

	hyperv1 "github.com/openshift/hypershift/api/v1beta1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// Webhook implements a validating webhook for HostedCluster.
type Webhook struct{}

// SetupWebhookWithManager sets up HostedCluster webhooks.
func SetupWebhookWithManager(mgr ctrl.Manager) error {
	err := ctrl.NewWebhookManagedBy(mgr).
		For(&hyperv1.HostedCluster{}).
		WithValidator(&Webhook{}).
		Complete()
	if err != nil {
		return fmt.Errorf("unable to register hostedcluster webhook: %w", err)
	}
	err = ctrl.NewWebhookManagedBy(mgr).
		For(&hyperv1.NodePool{}).
		Complete()
	if err != nil {
		return fmt.Errorf("unable to register nodepool webhook: %w", err)
	}
	err = ctrl.NewWebhookManagedBy(mgr).
		For(&hyperv1.HostedControlPlane{}).
		Complete()
	if err != nil {
		return fmt.Errorf("unable to register hostedcontrolplane webhook: %w", err)
	}
	err = ctrl.NewWebhookManagedBy(mgr).
		For(&hyperv1.AWSEndpointService{}).
		Complete()
	if err != nil {
		return fmt.Errorf("unable to register awsendpointservice webhook: %w", err)
	}
	return nil

}

var _ webhook.CustomValidator = &Webhook{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type.
func (webhook *Webhook) ValidateCreate(_ context.Context, obj runtime.Object) error {
	hostedCluster, ok := obj.(*hyperv1.HostedCluster)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("expected a HostedCluster but got a %T", obj))
	}

	return validateHostedClusterCreate(hostedCluster)
}

type cidrEntry struct {
	net  net.IPNet
	path field.Path
}

func cidrsOverlap(net1 *net.IPNet, net2 *net.IPNet) error {
	if net1.Contains(net2.IP) || net2.Contains(net1.IP) {
		return fmt.Errorf("%s and %s", net1.String(), net2.String())
	}
	return nil
}

func compareCIDREntries(ce []cidrEntry) field.ErrorList {
	var errs field.ErrorList

	for o := range ce {
		for i := o + 1; i < len(ce); i++ {
			if err := cidrsOverlap(&ce[o].net, &ce[i].net); err != nil {
				errs = append(errs, field.Invalid(&ce[o].path, ce[o].net.String(), fmt.Sprintf("%s and %s overlap: %s", ce[o].path.String(), ce[i].path.String(), err)))
			}
		}
	}
	return errs
}

func validateSliceNetworkCIDRs(hc *hyperv1.HostedCluster) field.ErrorList {
	var cidrEntries []cidrEntry

	for _, cidr := range hc.Spec.Networking.MachineNetwork {
		ce := cidrEntry{(net.IPNet)(cidr.CIDR), *field.NewPath("spec.networking.MachineNetwork")}
		cidrEntries = append(cidrEntries, ce)
	}
	for _, cidr := range hc.Spec.Networking.ServiceNetwork {
		ce := cidrEntry{(net.IPNet)(cidr.CIDR), *field.NewPath("spec.networking.ServiceNetwork")}
		cidrEntries = append(cidrEntries, ce)
	}
	for _, cidr := range hc.Spec.Networking.ClusterNetwork {
		ce := cidrEntry{(net.IPNet)(cidr.CIDR), *field.NewPath("spec.networking.ClusterNetwork")}
		cidrEntries = append(cidrEntries, ce)
	}

	return compareCIDREntries(cidrEntries)
}

func validateKubevirtBaseDomainPassthroughCreate(hc *hyperv1.HostedCluster) *field.Error {

	// It is invalid for someone to enable the BaseDomainPassthrough feature
	// and attempt to set their own custom BaseDomain during HC Creation.
	//
	// The BaseDomainPassthrough feature autocreates the BaseDomain for the user.
	if hc.Spec.Platform.Type == hyperv1.KubevirtPlatform &&
		hc.Spec.Platform.Kubevirt != nil &&
		hc.Spec.Platform.Kubevirt.BaseDomainPassthrough != nil &&
		*hc.Spec.Platform.Kubevirt.BaseDomainPassthrough &&
		hc.Spec.DNS.BaseDomain != "" {

		return field.InternalError(
			field.NewPath("HostedCluster.spec.platform.kubevirt.baseDomainPassthrough"),
			errors.New("BaseDomain can not be set when KubeVirt's BaseDomainPassthrough feature is enabled"))
	}

	return nil
}

func validateHostedClusterCreate(hc *hyperv1.HostedCluster) error {
	errs := validateSliceNetworkCIDRs(hc)

	if err := validateKubevirtBaseDomainPassthroughCreate(hc); err != nil {
		errs = append(errs, err)
	}

	return errs.ToAggregate()
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type.
func (webhook *Webhook) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) error {
	newHC, ok := newObj.(*hyperv1.HostedCluster)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("expected a HostedCluster but got a %T", newObj))
	}

	oldHC, ok := oldObj.(*hyperv1.HostedCluster)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("expected a HostedCluster but got a %T", oldObj))
	}

	return validateHostedClusterUpdate(newHC, oldHC)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type.
func (webhook *Webhook) ValidateDelete(_ context.Context, _ runtime.Object) error {
	return nil
}

// filterMutableHostedClusterSpecFields zeros out non-immutable entries so that they are
// "equal" when we do the comparison below.
func filterMutableHostedClusterSpecFields(spec *hyperv1.HostedClusterSpec) {
	spec.Release.Image = ""
	spec.Configuration = nil
	spec.AdditionalTrustBundle = nil
	spec.SecretEncryption = nil
	spec.PausedUntil = nil
	for i, svc := range spec.Services {
		if svc.Type == hyperv1.NodePort && svc.NodePort != nil {
			spec.Services[i].NodePort.Address = ""
			spec.Services[i].NodePort.Port = 0
		}
	}
	if spec.Platform.Type == hyperv1.AWSPlatform && spec.Platform.AWS != nil {
		spec.Platform.AWS.ResourceTags = nil
		// This is to enable reconcileDeprecatedAWSRoles.
		spec.Platform.AWS.RolesRef = hyperv1.AWSRolesRef{}
	}

	// This is to enable reconcileDeprecatedNetworkSettings
	// reset everything except network type and apiserver settings
	spec.Networking = hyperv1.ClusterNetworking{
		NetworkType: spec.Networking.NetworkType,
		APIServer:   spec.Networking.APIServer,
	}
}

// validateStructDeepEqual walks through a struct and compares each entry.  If it comes across a substruct it
// recursively calls itself.  Returns a list of immutable field errors generated by any field being changed.
func validateStructDeepEqual(x reflect.Value, y reflect.Value, path *field.Path, errs field.ErrorList) field.ErrorList {
	for i := 0; i < x.NumField(); i++ {
		v1 := x.Field(i)
		v2 := y.Field(i)
		jsonId := x.Type().Field(i).Tag.Get("json")
		sep := strings.Split(jsonId, ",")
		if len(sep) > 1 {
			jsonId = sep[0]
		}

		if v1.Kind() == reflect.Pointer {
			// If this is a pointer to a struct, dereference before continuing.
			if v1.Elem().Kind() == reflect.Struct {
				v1 = v1.Elem()
				v2 = v2.Elem()
			}
		}
		if v1.Kind() == reflect.Struct {
			errs = validateStructDeepEqual(v1, v2, path.Child(jsonId), errs)
		} else {
			if v1.CanInterface() {
				// Slices are actually tricky to compare and determine what has actually changed.  Only do the comparisons
				// If they are the same length, otherwise we'll just have to rely on DeepEqual().
				if v1.Kind() == reflect.Slice && v1.Len() > 0 && v1.Len() == v2.Len() && v1.Index(0).Kind() == reflect.Struct {
					for i := 0; i < v1.Len(); i++ {
						errs = validateStructDeepEqual(v1.Index(i), v2.Index(i), path.Child(jsonId), errs)
					}
				} else {
					// Using DeepEqual() here because it takes care of all the type checking/comparison magic.
					if !equality.Semantic.DeepEqual(v1.Interface(), v2.Interface()) {
						errs = append(errs, field.Invalid(path.Child(jsonId), v1.Interface(), "Attempted to change an immutable field"))
					}
				}
			}
		}
	}
	return errs
}

func validateEndpointAccess(new *hyperv1.PlatformSpec, old *hyperv1.PlatformSpec) error {
	if old.Type != hyperv1.AWSPlatform || new.Type != hyperv1.AWSPlatform || old.AWS == nil || new.AWS == nil {
		return nil
	}
	if old.AWS.EndpointAccess == new.AWS.EndpointAccess {
		return nil
	}
	if old.AWS.EndpointAccess == hyperv1.Public || new.AWS.EndpointAccess == hyperv1.Public {
		return fmt.Errorf("transitioning from EndpointAccess %s to %s is not allowed", old.AWS.EndpointAccess, new.AWS.EndpointAccess)
	}
	// Clear EndpointAccess for further validation
	old.AWS.EndpointAccess = ""
	new.AWS.EndpointAccess = ""
	return nil
}

// validateStructEqual uses introspection to walk through the fields of a struct and check
// for differences.  Any differences are flagged as an invalid change to an immutable field.
func validateStructEqual(x any, y any, path *field.Path) field.ErrorList {
	var errs field.ErrorList

	if x == nil || y == nil {
		errs = append(errs, field.InternalError(path, errors.New("nil struct")))
		return errs
	}
	v1 := reflect.ValueOf(x)
	v2 := reflect.ValueOf(y)
	if v1.Type() != v2.Type() {
		errs = append(errs, field.InternalError(path, errors.New("comparing structs of different type")))
		return errs
	}
	if v1.Kind() != reflect.Struct {
		errs = append(errs, field.InternalError(path, errors.New("comparing non structs")))
		return errs
	}
	return validateStructDeepEqual(v1, v2, path, errs)
}

func validateHostedClusterUpdate(new *hyperv1.HostedCluster, old *hyperv1.HostedCluster) error {
	filterMutableHostedClusterSpecFields(&new.Spec)
	filterMutableHostedClusterSpecFields(&old.Spec)

	// Only allow these to be set from empty.  Once set they should not be changed.
	if old.Spec.InfraID == "" {
		new.Spec.InfraID = ""
	}
	if old.Spec.ClusterID == "" {
		new.Spec.ClusterID = ""
	}

	// We default the port in Azure management cluster, so we allow setting it from being unset, but no updates.
	if new.Spec.Networking.APIServer != nil && (old.Spec.Networking.APIServer == nil || old.Spec.Networking.APIServer.Port == nil) {
		if old.Spec.Networking.APIServer == nil {
			old.Spec.Networking.APIServer = &hyperv1.APIServerNetworking{}
		}
		old.Spec.Networking.APIServer.Port = new.Spec.Networking.APIServer.Port
	}

	// We default the basedomain for KubeVirt clusters, so we allow the baseDomain
	// to go from unset to set, but no updates after basedomain is set.
	if new.Spec.Platform.Type == hyperv1.KubevirtPlatform &&
		new.Spec.Platform.Kubevirt != nil &&
		new.Spec.Platform.Kubevirt.BaseDomainPassthrough != nil &&
		*new.Spec.Platform.Kubevirt.BaseDomainPassthrough &&
		new.Spec.DNS.BaseDomain != "" &&
		old.Spec.DNS.BaseDomain == "" {

		old.Spec.DNS.BaseDomain = new.Spec.DNS.BaseDomain
	}

	if err := validateEndpointAccess(&new.Spec.Platform, &old.Spec.Platform); err != nil {
		return err
	}

	errs := validateStructEqual(new.Spec, old.Spec, field.NewPath("HostedCluster.spec"))

	return errs.ToAggregate()
}
