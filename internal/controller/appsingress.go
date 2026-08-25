/*
Copyright 2026.

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

package controller

import (
	"context"
	stderrors "errors"
	"net"
	"net/url"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
)

// hostedClusterFactory builds a client for a hosted cluster's API server.
// Callers close over a resolved InfraClusterAttachment.
type hostedClusterFactory func(ctx context.Context) (client.Client, error)

// appsIngressTarget carries the per-cluster inputs required by the shared
// apps-ingress automation. It is produced from an InfraClusterAttachment.
type appsIngressTarget struct {
	AttachmentName        string
	HostedClusterRef      hostedclusterv1alpha1.HostedClusterReference
	APIServerService      string
	ControlPlaneNamespace string
	Domain                string
	Config                hostedclusterv1alpha1.AppsIngressConfig
}

const (
	defaultIngressServiceName      = "oooi-ingress"
	defaultIngressServiceNamespace = "openshift-ingress"
)

// defaultHostedClusterClient builds a client for the referenced HostedCluster.
// The kubeconfig endpoint is intended for cluster clients on the VLAN; the
// management controller must use the in-cluster API Service instead while
// retaining the public hostname for certificate validation and SNI.
func defaultHostedClusterClient(ctx context.Context, c client.Client, ref hostedclusterv1alpha1.HostedClusterReference, apiServerService, controlPlaneNamespace string) (client.Client, error) {
	hostedCluster := &unstructured.Unstructured{}
	hostedCluster.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "hypershift.openshift.io",
		Version: "v1beta1",
		Kind:    "HostedCluster",
	})
	if err := c.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, hostedCluster); err != nil {
		return nil, err
	}

	kubeconfigName, found, err := unstructured.NestedString(hostedCluster.Object, "status", "kubeconfig", "name")
	if err != nil {
		return nil, err
	}
	if !found || kubeconfigName == "" {
		return nil, errors.NewBadRequest("hostedcluster does not report a kubeconfig")
	}

	kubeconfigSecret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Name: kubeconfigName, Namespace: ref.Namespace}, kubeconfigSecret); err != nil {
		return nil, err
	}

	kubeconfigBytes, ok := kubeconfigSecret.Data["kubeconfig"]
	if !ok || len(kubeconfigBytes) == 0 {
		return nil, errors.NewBadRequest("kubeconfig secret has no kubeconfig data")
	}

	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return nil, err
	}

	serverURL, err := url.Parse(config.Host)
	if err != nil {
		return nil, err
	}
	serverName := config.ServerName
	if serverName == "" {
		serverName = serverURL.Hostname()
	}
	port := serverURL.Port()
	if port == "" {
		port = "443"
	}
	if apiServerService == "" {
		apiServerService = "kube-apiserver"
	}
	apiServerHost := apiServerService + "." + controlPlaneNamespace + ".svc.cluster.local"
	config.Host = (&url.URL{
		Scheme: "https",
		Host:   net.JoinHostPort(apiServerHost, port),
	}).String()
	config.ServerName = serverName

	hostedScheme := runtime.NewScheme()
	if err := corev1.AddToScheme(hostedScheme); err != nil {
		return nil, err
	}

	return client.New(config, client.Options{Scheme: hostedScheme})
}

// ensureMetalLBInstalledFor installs MetalLB into the hosted cluster and
// creates the IPAddressPool/L2Advertisement pair described by cfg.
func ensureMetalLBInstalledFor(ctx context.Context, hostedClient client.Client, cfg hostedclusterv1alpha1.AppsIngressConfig) error {
	addressPoolName := cfg.MetalLB.AddressPoolName
	if addressPoolName == "" {
		return errors.NewBadRequest("appsIngress.metallb.addressPoolName is required")
	}

	addressRange := cfg.MetalLB.IPAddressPoolRange
	if addressRange == "" {
		return errors.NewBadRequest("appsIngress.metallb.ipAddressPoolRange is required")
	}

	operatorNamespace := "openshift-operators"

	subscription := &unstructured.Unstructured{}
	subscription.SetGroupVersionKind(schema.GroupVersionKind{Group: "operators.coreos.com", Version: "v1alpha1", Kind: "Subscription"})
	subscription.SetName("metallb-operator")
	subscription.SetNamespace(operatorNamespace)
	subscription.Object["spec"] = map[string]interface{}{
		"channel":             "stable",
		"name":                "metallb-operator",
		"source":              "redhat-operators",
		"sourceNamespace":     "openshift-marketplace",
		"installPlanApproval": "Automatic",
	}
	if err := applyUnstructured(ctx, hostedClient, subscription); err != nil {
		return err
	}

	metallb := &unstructured.Unstructured{}
	metallb.SetGroupVersionKind(schema.GroupVersionKind{Group: "metallb.io", Version: "v1beta1", Kind: "MetalLB"})
	metallb.SetName("metallb")
	metallb.SetNamespace(operatorNamespace)
	if err := applyUnstructured(ctx, hostedClient, metallb); err != nil {
		return err
	}

	ipPool := &unstructured.Unstructured{}
	ipPool.SetGroupVersionKind(schema.GroupVersionKind{Group: "metallb.io", Version: "v1beta1", Kind: "IPAddressPool"})
	ipPool.SetName(addressPoolName)
	ipPool.SetNamespace(operatorNamespace)
	ipPool.Object["spec"] = map[string]interface{}{
		"autoAssign": true,
		"addresses":  []interface{}{addressRange},
	}
	if err := applyUnstructured(ctx, hostedClient, ipPool); err != nil {
		return err
	}

	advertisementName := cfg.MetalLB.L2AdvertisementName
	if advertisementName == "" {
		advertisementName = "advertise-" + addressPoolName
	}
	l2Adv := &unstructured.Unstructured{}
	l2Adv.SetGroupVersionKind(schema.GroupVersionKind{Group: "metallb.io", Version: "v1beta1", Kind: "L2Advertisement"})
	l2Adv.SetName(advertisementName)
	l2Adv.SetNamespace(operatorNamespace)
	l2Adv.Object["spec"] = map[string]interface{}{
		"ipAddressPools": []interface{}{addressPoolName},
	}
	return applyUnstructured(ctx, hostedClient, l2Adv)
}

// cleanupMetalLBInstallation removes the hosted-cluster objects created by
// ensureMetalLBInstalledFor plus the ingress LoadBalancer Service. All deletes
// ignore NotFound; callers decide whether failures are fatal to finalization.
func cleanupMetalLBInstallation(ctx context.Context, hostedClient client.Client, cfg hostedclusterv1alpha1.AppsIngressConfig) error {
	if !cfg.Enabled {
		return nil
	}
	serviceName := cfg.Service.Name
	if serviceName == "" {
		serviceName = defaultIngressServiceName
	}
	serviceNamespace := cfg.Service.Namespace
	if serviceNamespace == "" {
		serviceNamespace = defaultIngressServiceNamespace
	}
	service := &corev1.Service{}
	service.SetName(serviceName)
	service.SetNamespace(serviceNamespace)
	if err := hostedClient.Delete(ctx, service); err != nil && !errors.IsNotFound(err) {
		return err
	}

	poolName := cfg.MetalLB.AddressPoolName
	if poolName == "" {
		return nil
	}
	advertisementName := cfg.MetalLB.L2AdvertisementName
	if advertisementName == "" {
		advertisementName = "advertise-" + poolName
	}
	operatorNamespace := "openshift-operators"
	for _, obj := range []client.Object{
		func() client.Object {
			o := &unstructured.Unstructured{}
			o.SetGroupVersionKind(schema.GroupVersionKind{Group: "metallb.io", Version: "v1beta1", Kind: "L2Advertisement"})
			o.SetName(advertisementName)
			o.SetNamespace(operatorNamespace)
			return o
		}(),
		func() client.Object {
			o := &unstructured.Unstructured{}
			o.SetGroupVersionKind(schema.GroupVersionKind{Group: "metallb.io", Version: "v1beta1", Kind: "IPAddressPool"})
			o.SetName(poolName)
			o.SetNamespace(operatorNamespace)
			return o
		}(),
		func() client.Object {
			o := &unstructured.Unstructured{}
			o.SetGroupVersionKind(schema.GroupVersionKind{Group: "metallb.io", Version: "v1beta1", Kind: "MetalLB"})
			o.SetName("metallb")
			o.SetNamespace(operatorNamespace)
			return o
		}(),
		func() client.Object {
			o := &unstructured.Unstructured{}
			o.SetGroupVersionKind(schema.GroupVersionKind{Group: "operators.coreos.com", Version: "v1alpha1", Kind: "Subscription"})
			o.SetName("metallb-operator")
			o.SetNamespace(operatorNamespace)
			return o
		}(),
	} {
		if err := hostedClient.Delete(ctx, obj); err != nil && !errors.IsNotFound(err) {
			var noMatch *meta.NoKindMatchError
			if stderrors.As(err, &noMatch) {
				continue // CRDs already gone from the hosted cluster
			}
			return err
		}
	}
	return nil
}

// ensureAppsIngressServiceFor creates or updates the wildcard ingress
// LoadBalancer Service in the hosted cluster as described by cfg.
func ensureAppsIngressServiceFor(ctx context.Context, hostedClient client.Client, cfg hostedclusterv1alpha1.AppsIngressConfig) error {
	serviceName := cfg.Service.Name
	if serviceName == "" {
		serviceName = defaultIngressServiceName
	}
	serviceNamespace := cfg.Service.Namespace
	if serviceNamespace == "" {
		serviceNamespace = defaultIngressServiceNamespace
	}

	httpPort := cfg.Ports.HTTP
	if httpPort == 0 {
		httpPort = 80
	}
	httpsPort := cfg.Ports.HTTPS
	if httpsPort == 0 {
		httpsPort = 443
	}

	service := &corev1.Service{}
	err := hostedClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, service)
	if err == nil {
		service.Spec.Type = corev1.ServiceTypeLoadBalancer
		service.Spec.Selector = map[string]string{
			"ingresscontroller.operator.openshift.io/deployment-ingresscontroller": "default",
		}
		service.Spec.Ports = []corev1.ServicePort{
			{Name: "http", Protocol: corev1.ProtocolTCP, Port: httpPort, TargetPort: intstrFromInt32(httpPort)},
			{Name: "https", Protocol: corev1.ProtocolTCP, Port: httpsPort, TargetPort: intstrFromInt32(httpsPort)},
		}
		if service.Annotations == nil {
			service.Annotations = map[string]string{}
		}
		if cfg.MetalLB.AddressPoolName != "" {
			service.Annotations["metallb.universe.tf/address-pool"] = cfg.MetalLB.AddressPoolName
		}
		if service.Labels == nil {
			service.Labels = map[string]string{}
		}
		for key, value := range cfg.Service.Labels {
			service.Labels[key] = value
		}
		for key, value := range cfg.Service.Annotations {
			service.Annotations[key] = value
		}
		if cfg.MetalLB.AddressPoolName == "" {
			delete(service.Annotations, "metallb.universe.tf/address-pool")
		} else {
			service.Annotations["metallb.universe.tf/address-pool"] = cfg.MetalLB.AddressPoolName
		}
		return hostedClient.Update(ctx, service)
	}
	if !errors.IsNotFound(err) {
		return err
	}

	labels := map[string]string{}
	for key, value := range cfg.Service.Labels {
		labels[key] = value
	}
	annotations := map[string]string{
		"metallb.universe.tf/address-pool": cfg.MetalLB.AddressPoolName,
	}
	for key, value := range cfg.Service.Annotations {
		annotations[key] = value
	}
	if cfg.MetalLB.AddressPoolName == "" {
		delete(annotations, "metallb.universe.tf/address-pool")
	}

	service = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Namespace:   serviceNamespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{
				{Name: "http", Protocol: corev1.ProtocolTCP, Port: httpPort, TargetPort: intstrFromInt32(httpPort)},
				{Name: "https", Protocol: corev1.ProtocolTCP, Port: httpsPort, TargetPort: intstrFromInt32(httpsPort)},
			},
			Selector: map[string]string{
				"ingresscontroller.operator.openshift.io/deployment-ingresscontroller": "default",
			},
		},
	}

	return hostedClient.Create(ctx, service)
}

// discoverAppsIngressExternalIPFor reads the LoadBalancer Service status from
// the hosted cluster. Exactly one of ip or hostname is non-empty once assigned;
// both are empty while the endpoint is pending.
func discoverAppsIngressExternalIPFor(ctx context.Context, hostedClient client.Client, cfg hostedclusterv1alpha1.AppsIngressConfig) (ip, hostname string, err error) {
	serviceName := cfg.Service.Name
	if serviceName == "" {
		serviceName = defaultIngressServiceName
	}
	serviceNamespace := cfg.Service.Namespace
	if serviceNamespace == "" {
		serviceNamespace = defaultIngressServiceNamespace
	}
	svc := &corev1.Service{}
	if err = hostedClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, svc); err != nil {
		return "", "", err
	}
	if len(svc.Status.LoadBalancer.Ingress) == 0 {
		return "", "", nil
	}
	ingress := svc.Status.LoadBalancer.Ingress[0]
	if ingress.IP != "" {
		return ingress.IP, "", nil
	}
	if ingress.Hostname != "" {
		return "", ingress.Hostname, nil
	}
	return "", "", nil
}

// reconcileAppsIngressCore drives one target's apps-ingress automation to the
// desired state and writes observations into status. It never returns errors:
// failures are recorded on status with an appropriate requeue delay, matching
// the historical Infra-owned behavior.
func reconcileAppsIngressCore(ctx context.Context, factory hostedClusterFactory, target appsIngressTarget, status *hostedclusterv1alpha1.AppsIngressStatus) ctrl.Result {
	previousStatus := *status

	if !target.Config.Enabled {
		*status = hostedclusterv1alpha1.AppsIngressStatus{}
		return ctrl.Result{}
	}

	hostedClient, err := factory(ctx)
	if err != nil {
		status.Phase = PhaseDegraded
		status.Reason = "HostedClusterAccessFailed"
		status.Message = err.Error()
		setAppsIngressLastSyncTime(status, previousStatus)
		status.ExternalIP = ""
		status.ExternalHostname = ""
		return ctrl.Result{RequeueAfter: 30 * time.Second}
	}

	nodes := &corev1.NodeList{}
	if err := hostedClient.List(ctx, nodes); err != nil {
		status.Phase = PhaseDegraded
		status.Reason = "HostedClusterNodeListFailed"
		status.Message = err.Error()
		setAppsIngressLastSyncTime(status, previousStatus)
		status.ExternalIP = ""
		status.ExternalHostname = ""
		return ctrl.Result{RequeueAfter: 30 * time.Second}
	}
	if !hasReadyNode(nodes.Items) {
		status.Phase = PhasePending
		status.Reason = "WaitingForHostedClusterNodes"
		status.Message = "waiting for a Ready hosted cluster node before installing MetalLB"
		setAppsIngressLastSyncTime(status, previousStatus)
		status.ExternalIP = ""
		status.ExternalHostname = ""
		return ctrl.Result{RequeueAfter: 30 * time.Second}
	}

	if err := ensureMetalLBInstalledFor(ctx, hostedClient, target.Config); err != nil {
		status.Phase = PhaseDegraded
		status.Reason = "MetalLBInstallFailed"
		status.Message = err.Error()
		if meta.IsNoMatchError(err) {
			status.Phase = PhasePending
			status.Reason = "WaitingForMetalLBCRDs"
			status.Message = "MetalLB operator is not ready: " + err.Error()
		}
		setAppsIngressLastSyncTime(status, previousStatus)
		status.ExternalIP = ""
		status.ExternalHostname = ""
		return ctrl.Result{RequeueAfter: 30 * time.Second}
	}

	if err := ensureAppsIngressServiceFor(ctx, hostedClient, target.Config); err != nil {
		status.Phase = PhaseDegraded
		status.Reason = "IngressServiceFailed"
		status.Message = err.Error()
		setAppsIngressLastSyncTime(status, previousStatus)
		status.ExternalIP = ""
		status.ExternalHostname = ""
		return ctrl.Result{RequeueAfter: 30 * time.Second}
	}

	externalIP, externalHostname, err := discoverAppsIngressExternalIPFor(ctx, hostedClient, target.Config)
	if err != nil {
		status.Phase = PhaseDegraded
		status.Reason = "ExternalIPDiscoveryFailed"
		status.Message = err.Error()
		setAppsIngressLastSyncTime(status, previousStatus)
		status.ExternalIP = ""
		status.ExternalHostname = ""
		return ctrl.Result{RequeueAfter: 15 * time.Second}
	}
	if externalIP == "" && externalHostname == "" {
		status.Phase = PhasePending
		status.Reason = "WaitingForExternalIP"
		status.Message = "MetalLB and ingress service configured; waiting for external IP"
		status.ExternalIP = ""
		status.ExternalHostname = ""
		setAppsIngressLastSyncTime(status, previousStatus)
		return ctrl.Result{RequeueAfter: 15 * time.Second}
	}

	status.ExternalIP = externalIP
	status.ExternalHostname = externalHostname
	status.Phase = phaseReady
	status.Reason = "ReconciliationSucceeded"
	endpoint := externalIP
	if endpoint == "" {
		endpoint = externalHostname
	}
	status.Message = "Apps ingress ready with external endpoint " + endpoint
	setAppsIngressLastSyncTime(status, previousStatus)
	return ctrl.Result{}
}
