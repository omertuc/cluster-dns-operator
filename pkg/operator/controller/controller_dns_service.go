package controller

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/cluster-dns-operator/pkg/manifests"

	corev1 "k8s.io/api/core/v1"

	"github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ensureDNSService ensures that a service exists for a given DNS.
func (r *reconciler) ensureDNSService(dns *operatorv1.DNS, clusterIP string, daemonsetRef metav1.OwnerReference) (*corev1.Service, error) {
	current, err := r.currentDNSService(dns)
	if err != nil {
		return nil, err
	}
	desired := desiredDNSService(dns, clusterIP, daemonsetRef)

	switch {
	case desired != nil && current == nil:
		if err := r.client.Create(context.TODO(), desired); err != nil {
			return nil, fmt.Errorf("failed to create dns service: %v", err)
		}
		logrus.Infof("created dns service: %s/%s", desired.Namespace, desired.Name)
	case desired != nil && current != nil:
		if err := r.updateDNSService(current, desired); err != nil {
			return nil, err
		}
	}
	return r.currentDNSService(dns)
}

func (r *reconciler) currentDNSService(dns *operatorv1.DNS) (*corev1.Service, error) {
	current := &corev1.Service{}
	err := r.client.Get(context.TODO(), DNSServiceName(dns), current)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return current, nil
}

func desiredDNSService(dns *operatorv1.DNS, clusterIP string, daemonsetRef metav1.OwnerReference) *corev1.Service {
	s := manifests.DNSService()

	name := DNSServiceName(dns)
	s.Namespace = name.Namespace
	s.Name = name.Name
	s.SetOwnerReferences([]metav1.OwnerReference{dnsOwnerRef(dns)})

	s.Labels = map[string]string{
		manifests.OwningDNSLabel: DNSDaemonSetLabel(dns),
	}

	s.Spec.Selector = DNSDaemonSetPodSelector(dns).MatchLabels

	if len(clusterIP) > 0 {
		s.Spec.ClusterIP = clusterIP
	}
	return s
}

func (r *reconciler) updateDNSService(current, desired *corev1.Service) error {
	changed, updated := serviceChanged(current, desired)
	if !changed {
		return nil
	}

	if err := r.client.Update(context.TODO(), updated); err != nil {
		return fmt.Errorf("failed to update dns service %s/%s: %v", updated.Namespace, updated.Name, err)
	}
	logrus.Infof("updated dns service: %s/%s", updated.Namespace, updated.Name)
	return nil
}

func serviceChanged(current, expected *corev1.Service) (bool, *corev1.Service) {
	serviceCmpOpts := []cmp.Option{
		// Ignore fields that the API, other controllers, or user may
		// have modified.
		//
		// TODO: Remove TopologyKeys when the service topology feature gate is enabled.
		cmpopts.IgnoreFields(corev1.ServiceSpec{}, "ClusterIP", "TopologyKeys"),
		cmpopts.EquateEmpty(),
	}
	if cmp.Equal(current.Spec, expected.Spec, serviceCmpOpts...) {
		return false, nil
	}

	updated := current.DeepCopy()
	updated.Spec = expected.Spec

	// Preserve fields that the API, other controllers, or user may have
	// modified.
	updated.Spec.ClusterIP = current.Spec.ClusterIP

	return true, updated
}
