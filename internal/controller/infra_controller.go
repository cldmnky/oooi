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
	"reflect"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
)

const (
	// PhaseDegraded indicates a component is in a degraded state
	PhaseDegraded = "Degraded"
)

// InfraReconciler reconciles a Infra object
type InfraReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// HostedClusterClientFactory creates a client for the hosted cluster.
	// Used for installing MetalLB and managing ingress service in the hosted cluster.
	HostedClusterClientFactory func(ctx context.Context, infra *hostedclusterv1alpha1.Infra) (client.Client, error)
}

// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=infras,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=infras/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=infras/finalizers,verbs=update
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=dhcpservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=dnsservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=proxyservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=hypershift.openshift.io,resources=hostedclusters,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *InfraReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the Infra instance
	infra := &hostedclusterv1alpha1.Infra{}
	err := r.Get(ctx, req.NamespacedName, infra)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("Infra resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Infra")
		return ctrl.Result{}, err
	}

	// Reconcile infrastructure components
	if err := r.reconcileDHCPComponent(ctx, infra); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileDNSComponent(ctx, infra); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileProxyComponent(ctx, infra); err != nil {
		return ctrl.Result{}, err
	}

	r.reconcileAppsIngress(ctx, infra)

	// Update status
	return r.updateInfraStatus(ctx, infra)
}

// reconcileDHCPComponent handles DHCP server creation and updates
func (r *InfraReconciler) reconcileDHCPComponent(ctx context.Context, infra *hostedclusterv1alpha1.Infra) error {
	log := logf.FromContext(ctx)

	if !infra.Spec.InfraComponents.DHCP.Enabled {
		return nil
	}

	dhcpServer := r.dhcpServerForInfra(infra)
	if err := ctrl.SetControllerReference(infra, dhcpServer, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference for DHCPServer")
		return err
	}

	foundDHCPServer := &hostedclusterv1alpha1.DHCPServer{}
	err := r.Get(ctx, types.NamespacedName{Name: dhcpServer.Name, Namespace: dhcpServer.Namespace}, foundDHCPServer)
	if err != nil && errors.IsNotFound(err) {
		log.Info("Creating a new DHCPServer", "DHCPServer.Namespace", dhcpServer.Namespace, "DHCPServer.Name", dhcpServer.Name)
		return r.Create(ctx, dhcpServer)
	} else if err != nil {
		log.Error(err, "Failed to get DHCPServer")
		return err
	}

	// Update existing DHCPServer if spec differs
	if !reflect.DeepEqual(foundDHCPServer.Spec, dhcpServer.Spec) {
		log.Info("Updating DHCPServer spec", "DHCPServer.Name", dhcpServer.Name)
		foundDHCPServer.Spec = dhcpServer.Spec
		return r.Update(ctx, foundDHCPServer)
	}

	return nil
}

// reconcileDNSComponent handles DNS server creation and updates
func (r *InfraReconciler) reconcileDNSComponent(ctx context.Context, infra *hostedclusterv1alpha1.Infra) error {
	log := logf.FromContext(ctx)

	if !infra.Spec.InfraComponents.DNS.Enabled {
		return nil
	}

	dnsServer := r.dnsServerForInfra(infra)
	if err := ctrl.SetControllerReference(infra, dnsServer, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference for DNSServer")
		return err
	}

	foundDNSServer := &hostedclusterv1alpha1.DNSServer{}
	err := r.Get(ctx, types.NamespacedName{Name: dnsServer.Name, Namespace: dnsServer.Namespace}, foundDNSServer)
	if err != nil && errors.IsNotFound(err) {
		log.Info("Creating a new DNSServer", "DNSServer.Namespace", dnsServer.Namespace, "DNSServer.Name", dnsServer.Name)
		return r.Create(ctx, dnsServer)
	} else if err != nil {
		log.Error(err, "Failed to get DNSServer")
		return err
	}

	// Update existing DNSServer if spec differs
	if !reflect.DeepEqual(foundDNSServer.Spec, dnsServer.Spec) {
		log.Info("Updating DNSServer spec", "DNSServer.Name", dnsServer.Name)
		foundDNSServer.Spec = dnsServer.Spec
		return r.Update(ctx, foundDNSServer)
	}

	return nil
}

// reconcileProxyComponent handles proxy server creation, updates, and network policy
func (r *InfraReconciler) reconcileProxyComponent(ctx context.Context, infra *hostedclusterv1alpha1.Infra) error {
	log := logf.FromContext(ctx)

	if !infra.Spec.InfraComponents.Proxy.Enabled {
		return nil
	}

	proxyServer := r.proxyServerForInfra(infra)
	if err := ctrl.SetControllerReference(infra, proxyServer, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference for ProxyServer")
		return err
	}

	foundProxyServer := &hostedclusterv1alpha1.ProxyServer{}
	err := r.Get(ctx, types.NamespacedName{Name: proxyServer.Name, Namespace: proxyServer.Namespace}, foundProxyServer)
	if err != nil && errors.IsNotFound(err) {
		log.Info("Creating a new ProxyServer", "ProxyServer.Namespace", proxyServer.Namespace, "ProxyServer.Name", proxyServer.Name)
		err = r.Create(ctx, proxyServer)
		if err != nil {
			log.Error(err, "Failed to create new ProxyServer")
			return err
		}
	} else if err != nil {
		log.Error(err, "Failed to get ProxyServer")
		return err
	} else {
		// Update existing ProxyServer if spec differs
		if !reflect.DeepEqual(foundProxyServer.Spec, proxyServer.Spec) {
			log.Info("Updating ProxyServer spec", "ProxyServer.Name", proxyServer.Name)
			foundProxyServer.Spec = proxyServer.Spec
			if err := r.Update(ctx, foundProxyServer); err != nil {
				log.Error(err, "Failed to update ProxyServer")
				return err
			}
		}
	}

	// Create NetworkPolicy in HCP namespace if ControlPlaneNamespace is specified
	if infra.Spec.InfraComponents.Proxy.ControlPlaneNamespace != "" {
		return r.reconcileNetworkPolicy(ctx, infra)
	}

	return nil
}

// reconcileNetworkPolicy creates the network policy for the proxy component
func (r *InfraReconciler) reconcileNetworkPolicy(ctx context.Context, infra *hostedclusterv1alpha1.Infra) error {
	log := logf.FromContext(ctx)

	networkPolicy := r.networkPolicyForInfra(infra)
	// Note: Cannot set owner reference for cross-namespace resources
	// Kubernetes disallows cross-namespace owner references

	foundNetworkPolicy := &networkingv1.NetworkPolicy{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      networkPolicy.Name,
		Namespace: networkPolicy.Namespace,
	}, foundNetworkPolicy)
	if err != nil && errors.IsNotFound(err) {
		log.Info("Creating NetworkPolicy in HCP namespace",
			"namespace", networkPolicy.Namespace,
			"name", networkPolicy.Name)
		return r.Create(ctx, networkPolicy)
	} else if err != nil {
		log.Error(err, "Failed to get NetworkPolicy")
		return err
	}

	return nil
}

// reconcileAppsIngress handles apps ingress configuration for hosted clusters.
func (r *InfraReconciler) reconcileAppsIngress(ctx context.Context, infra *hostedclusterv1alpha1.Infra) {
	// If apps ingress is not enabled, skip
	if !infra.Spec.AppsIngress.Enabled {
		return
	}

	hostedClient, err := r.getHostedClusterClient(ctx, infra)
	if err != nil {
		infra.Status.AppsIngressStatus.Phase = PhaseDegraded
		infra.Status.AppsIngressStatus.Reason = "HostedClusterAccessFailed"
		infra.Status.AppsIngressStatus.Message = err.Error()
		infra.Status.AppsIngressStatus.LastSyncTime = metav1.Now()
		return
	}

	if err := r.ensureMetalLBInstalled(ctx, hostedClient, infra); err != nil {
		infra.Status.AppsIngressStatus.Phase = PhaseDegraded
		infra.Status.AppsIngressStatus.Reason = "MetalLBInstallFailed"
		infra.Status.AppsIngressStatus.Message = err.Error()
		infra.Status.AppsIngressStatus.LastSyncTime = metav1.Now()
		return
	}

	if err := r.ensureAppsIngressService(ctx, hostedClient, infra); err != nil {
		infra.Status.AppsIngressStatus.Phase = PhaseDegraded
		infra.Status.AppsIngressStatus.Reason = "IngressServiceFailed"
		infra.Status.AppsIngressStatus.Message = err.Error()
		infra.Status.AppsIngressStatus.LastSyncTime = metav1.Now()
		return
	}

	infra.Status.AppsIngressStatus.Phase = "Pending"
	infra.Status.AppsIngressStatus.Reason = "WaitingForExternalIP"
	infra.Status.AppsIngressStatus.Message = "MetalLB and ingress service configured; waiting for external IP"
	infra.Status.AppsIngressStatus.LastSyncTime = metav1.Now()
}

func (r *InfraReconciler) getHostedClusterClient(ctx context.Context, infra *hostedclusterv1alpha1.Infra) (client.Client, error) {
	if r.HostedClusterClientFactory != nil {
		return r.HostedClusterClientFactory(ctx, infra)
	}

	hostedClusterName := infra.Spec.AppsIngress.HostedClusterRef.Name
	hostedClusterNamespace := infra.Spec.AppsIngress.HostedClusterRef.Namespace
	if hostedClusterNamespace == "" {
		hostedClusterNamespace = "clusters"
	}
	if hostedClusterName == "" {
		return nil, errors.NewBadRequest("appsIngress.hostedClusterRef.name is required")
	}

	hostedCluster := &unstructured.Unstructured{}
	hostedCluster.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "hypershift.openshift.io",
		Version: "v1beta1",
		Kind:    "HostedCluster",
	})
	if err := r.Get(ctx, types.NamespacedName{Name: hostedClusterName, Namespace: hostedClusterNamespace}, hostedCluster); err != nil {
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
	if err := r.Get(ctx, types.NamespacedName{Name: kubeconfigName, Namespace: hostedClusterNamespace}, kubeconfigSecret); err != nil {
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

	hostedScheme := runtime.NewScheme()
	if err := corev1.AddToScheme(hostedScheme); err != nil {
		return nil, err
	}

	return client.New(config, client.Options{Scheme: hostedScheme})
}

func (r *InfraReconciler) ensureMetalLBInstalled(ctx context.Context, hostedClient client.Client, infra *hostedclusterv1alpha1.Infra) error {
	addressPoolName := infra.Spec.AppsIngress.MetalLB.AddressPoolName
	if addressPoolName == "" {
		return errors.NewBadRequest("appsIngress.metallb.addressPoolName is required")
	}

	addressRange := infra.Spec.AppsIngress.MetalLB.IPAddressPoolRange
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
	if err := r.applyUnstructured(ctx, hostedClient, subscription); err != nil {
		return err
	}

	metallb := &unstructured.Unstructured{}
	metallb.SetGroupVersionKind(schema.GroupVersionKind{Group: "metallb.io", Version: "v1beta1", Kind: "MetalLB"})
	metallb.SetName("metallb")
	metallb.SetNamespace(operatorNamespace)
	if err := r.applyUnstructured(ctx, hostedClient, metallb); err != nil {
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
	if err := r.applyUnstructured(ctx, hostedClient, ipPool); err != nil {
		return err
	}

	advertisementName := infra.Spec.AppsIngress.MetalLB.L2AdvertisementName
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
	if err := r.applyUnstructured(ctx, hostedClient, l2Adv); err != nil {
		return err
	}

	return nil
}

func (r *InfraReconciler) ensureAppsIngressService(ctx context.Context, hostedClient client.Client, infra *hostedclusterv1alpha1.Infra) error {
	serviceName := infra.Spec.AppsIngress.Service.Name
	if serviceName == "" {
		serviceName = "oooi-ingress"
	}
	serviceNamespace := infra.Spec.AppsIngress.Service.Namespace
	if serviceNamespace == "" {
		serviceNamespace = "openshift-ingress"
	}

	httpPort := infra.Spec.AppsIngress.Ports.HTTP
	if httpPort == 0 {
		httpPort = 80
	}
	httpsPort := infra.Spec.AppsIngress.Ports.HTTPS
	if httpsPort == 0 {
		httpsPort = 443
	}

	service := &corev1.Service{}
	if err := hostedClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, service); err == nil {
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
		if infra.Spec.AppsIngress.MetalLB.AddressPoolName != "" {
			service.Annotations["metallb.universe.tf/address-pool"] = infra.Spec.AppsIngress.MetalLB.AddressPoolName
		}
		return hostedClient.Update(ctx, service)
	}

	service = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: serviceNamespace,
			Annotations: map[string]string{
				"metallb.universe.tf/address-pool": infra.Spec.AppsIngress.MetalLB.AddressPoolName,
			},
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

	if infra.Spec.AppsIngress.MetalLB.AddressPoolName == "" {
		service.Annotations = nil
	}

	return hostedClient.Create(ctx, service)
}

func (r *InfraReconciler) applyUnstructured(ctx context.Context, c client.Client, desired *unstructured.Unstructured) error {
	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(desired.GroupVersionKind())
	if err := c.Get(ctx, types.NamespacedName{Name: desired.GetName(), Namespace: desired.GetNamespace()}, current); err != nil {
		if errors.IsNotFound(err) {
			return c.Create(ctx, desired)
		}
		return err
	}

	desired.SetResourceVersion(current.GetResourceVersion())
	return c.Update(ctx, desired)
}

func intstrFromInt32(value int32) intstr.IntOrString {
	return intstr.FromInt32(value)
}

// updateInfraStatus updates the status of the Infra resource
func (r *InfraReconciler) updateInfraStatus(ctx context.Context, infra *hostedclusterv1alpha1.Infra) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	infra.Status.ObservedGeneration = infra.Generation
	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: infra.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             "ReconciliationSucceeded",
		Message:            "Infrastructure components provisioned successfully",
	}

	infra.Status.Conditions = []metav1.Condition{condition}
	if infra.Spec.InfraComponents.DHCP.Enabled {
		infra.Status.ComponentStatus.DHCPReady = true
	}
	if infra.Spec.InfraComponents.DNS.Enabled {
		infra.Status.ComponentStatus.DNSReady = true
	}
	if infra.Spec.InfraComponents.Proxy.Enabled {
		infra.Status.ComponentStatus.ProxyReady = true
	}

	if err := r.Status().Update(ctx, infra); err != nil {
		log.Error(err, "Failed to update Infra status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// dhcpServerForInfra returns a DHCPServer object for the Infra
func (r *InfraReconciler) dhcpServerForInfra(infra *hostedclusterv1alpha1.Infra) *hostedclusterv1alpha1.DHCPServer {
	dhcpSpec := infra.Spec.InfraComponents.DHCP

	// Use default image if not specified
	image := dhcpSpec.Image
	if image == "" {
		image = "quay.io/cldmnky/oooi:latest"
	}

	// Get NAD namespace from NetworkConfig or default to Infra's namespace
	nadName := infra.Spec.NetworkConfig.NetworkAttachmentDefinition
	nadNamespace := infra.Namespace
	if infra.Spec.NetworkConfig.NetworkAttachmentNamespace != "" {
		nadNamespace = infra.Spec.NetworkConfig.NetworkAttachmentNamespace
	}

	// Determine DNS servers for DHCP clients:
	// 1. If DNS is enabled, use our DNS server IP (which forwards to upstream)
	// 2. Otherwise, use explicitly configured DNS servers from NetworkConfig
	// 3. Otherwise, leave empty (will default to 8.8.8.8 in DHCP controller)
	var dnsServers []string
	if infra.Spec.InfraComponents.DNS.Enabled {
		// Use our DNS server - it will handle forwarding to upstream
		dnsServers = []string{infra.Spec.InfraComponents.DNS.ServerIP}
	} else {
		// No DNS server deployed, use upstream directly
		dnsServers = infra.Spec.NetworkConfig.DNSServers
	}

	return &hostedclusterv1alpha1.DHCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      infra.Name + "-dhcp",
			Namespace: infra.Namespace,
		},
		Spec: hostedclusterv1alpha1.DHCPServerSpec{
			NetworkConfig: hostedclusterv1alpha1.DHCPNetworkConfig{
				CIDR:                       infra.Spec.NetworkConfig.CIDR,
				Gateway:                    infra.Spec.NetworkConfig.Gateway,
				ServerIP:                   dhcpSpec.ServerIP,
				DNSServers:                 dnsServers,
				NetworkAttachmentName:      nadName,
				NetworkAttachmentNamespace: nadNamespace,
			},
			LeaseConfig: hostedclusterv1alpha1.DHCPLeaseConfig{
				RangeStart: dhcpSpec.RangeStart,
				RangeEnd:   dhcpSpec.RangeEnd,
				LeaseTime:  dhcpSpec.LeaseTime,
			},
			Image: image,
		},
	}
}

// dnsServerForInfra returns a DNSServer object for the Infra
func (r *InfraReconciler) dnsServerForInfra(infra *hostedclusterv1alpha1.Infra) *hostedclusterv1alpha1.DNSServer {
	dnsSpec := infra.Spec.InfraComponents.DNS

	// Use default image if not specified
	image := dnsSpec.Image
	if image == "" {
		image = "quay.io/cldmnky/oooi:latest"
	}

	// Get NAD namespace from NetworkConfig or default to Infra's namespace
	nadName := infra.Spec.NetworkConfig.NetworkAttachmentDefinition
	nadNamespace := infra.Namespace
	if infra.Spec.NetworkConfig.NetworkAttachmentNamespace != "" {
		nadNamespace = infra.Spec.NetworkConfig.NetworkAttachmentNamespace
	}

	// Build hosted cluster domain from ClusterName and BaseDomain
	hostedClusterDomain := dnsSpec.ClusterName + "." + dnsSpec.BaseDomain

	// Get proxy IPs (external for VMs on secondary network, internal for management pods)
	externalProxyIP := infra.Spec.InfraComponents.Proxy.ServerIP
	internalProxyIP := infra.Spec.InfraComponents.Proxy.InternalProxyService

	// Build static DNS entries for HCP endpoints
	// These entries use the external proxy IP - the controller will create
	// separate entries for the internal proxy IP in the default view
	// Common HCP endpoints:
	// - api.<hostedClusterDomain>: Main Kubernetes API endpoint
	// - api-int.<hostedClusterDomain>: Internal API endpoint
	// - oauth.<hostedClusterDomain>: OAuth server endpoint
	// - ignition.<hostedClusterDomain>: Ignition configuration server
	// - konnectivity.<hostedClusterDomain>: Konnectivity proxy endpoint
	staticEntries := []hostedclusterv1alpha1.DNSStaticEntry{
		{
			Hostname: "api." + hostedClusterDomain,
			IP:       externalProxyIP,
		},
		{
			Hostname: "api-int." + hostedClusterDomain,
			IP:       externalProxyIP,
		},
		{
			Hostname: "oauth." + hostedClusterDomain,
			IP:       externalProxyIP,
		},
		{
			Hostname: "ignition." + hostedClusterDomain,
			IP:       externalProxyIP,
		},
		{
			Hostname: "konnectivity." + hostedClusterDomain,
			IP:       externalProxyIP,
		},
	}

	return &hostedclusterv1alpha1.DNSServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      infra.Name + "-dns",
			Namespace: infra.Namespace,
		},
		Spec: hostedclusterv1alpha1.DNSServerSpec{
			NetworkConfig: hostedclusterv1alpha1.DNSNetworkConfig{
				ServerIP:                   dnsSpec.ServerIP,
				ProxyIP:                    externalProxyIP,
				InternalProxyIP:            internalProxyIP,
				SecondaryNetworkCIDR:       infra.Spec.NetworkConfig.CIDR,
				NetworkAttachmentName:      nadName,
				NetworkAttachmentNamespace: nadNamespace,
				DNSPort:                    53,
			},
			HostedClusterDomain: hostedClusterDomain,
			StaticEntries:       staticEntries,
			UpstreamDNS:         infra.Spec.NetworkConfig.DNSServers,
			Image:               image,
			ReloadInterval:      "5s",
			CacheTTL:            "30s",
		},
	}
}

// proxyServerForInfra returns a ProxyServer object for the Infra
func (r *InfraReconciler) proxyServerForInfra(infra *hostedclusterv1alpha1.Infra) *hostedclusterv1alpha1.ProxyServer {
	proxySpec := infra.Spec.InfraComponents.Proxy

	// Parse NetworkAttachmentDefinition name and namespace
	// Get NAD namespace from NetworkConfig or default to Infra's namespace
	nadName := infra.Spec.NetworkConfig.NetworkAttachmentDefinition
	nadNamespace := infra.Namespace
	if infra.Spec.NetworkConfig.NetworkAttachmentNamespace != "" {
		nadNamespace = infra.Spec.NetworkConfig.NetworkAttachmentNamespace
	}

	// Build hosted cluster domain from ClusterName and BaseDomain
	hostedClusterDomain := infra.Spec.InfraComponents.DNS.ClusterName + "." + infra.Spec.InfraComponents.DNS.BaseDomain

	// Get the control plane namespace
	controlPlaneNamespace := proxySpec.ControlPlaneNamespace
	if controlPlaneNamespace == "" {
		controlPlaneNamespace = infra.Namespace + "-" + infra.Name
	}

	// Build backends for standard HCP services
	// These are the core services that need to be proxied through SNI-based routing
	backends := []hostedclusterv1alpha1.ProxyBackend{
		{
			Name:            "kube-apiserver",
			Hostname:        "api." + hostedClusterDomain,
			Port:            6443,
			TargetService:   "kube-apiserver",
			TargetPort:      6443,
			TargetNamespace: controlPlaneNamespace,
			Protocol:        "TCP",
			TimeoutSeconds:  30,
		},
		{
			Name:            "kube-apiserver-internal",
			Hostname:        "api-int." + hostedClusterDomain,
			Port:            6443,
			TargetService:   "kube-apiserver",
			TargetPort:      6443,
			TargetNamespace: controlPlaneNamespace,
			Protocol:        "TCP",
			TimeoutSeconds:  30,
		},
		{
			Name:            "oauth-openshift",
			Hostname:        "oauth." + hostedClusterDomain,
			Port:            443,
			TargetService:   "oauth-openshift",
			TargetPort:      6443,
			TargetNamespace: controlPlaneNamespace,
			Protocol:        "TCP",
			TimeoutSeconds:  30,
		},
		{
			Name:            "ignition-server",
			Hostname:        "ignition." + hostedClusterDomain,
			Port:            443,
			TargetService:   "ignition-server-proxy",
			TargetPort:      443,
			TargetNamespace: controlPlaneNamespace,
			Protocol:        "TCP",
			TimeoutSeconds:  30,
		},
		{
			Name:     "kube-apiserver-kubernetes-hostname",
			Hostname: "kubernetes." + hostedClusterDomain,
			AlternateHostnames: []string{
				"kubernetes",
				"kubernetes.default",
				"kubernetes.default.svc",
				"kubernetes.default.svc.cluster.local",
			},
			Port:            443,
			TargetService:   "kube-apiserver",
			TargetPort:      6443,
			TargetNamespace: controlPlaneNamespace,
			Protocol:        "TCP",
			TimeoutSeconds:  30,
		},
		{
			Name:            "konnectivity-server",
			Hostname:        "konnectivity." + hostedClusterDomain,
			Port:            443,
			TargetService:   "konnectivity-server",
			TargetPort:      8091,
			TargetNamespace: controlPlaneNamespace,
			Protocol:        "TCP",
			TimeoutSeconds:  30,
		},
	}

	return &hostedclusterv1alpha1.ProxyServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      infra.Name + "-proxy",
			Namespace: infra.Namespace,
		},
		Spec: hostedclusterv1alpha1.ProxyServerSpec{
			NetworkConfig: hostedclusterv1alpha1.ProxyNetworkConfig{
				ServerIP:                   proxySpec.ServerIP,
				NetworkAttachmentName:      nadName,
				NetworkAttachmentNamespace: nadNamespace,
			},
			Backends:     backends,
			ProxyImage:   proxySpec.ProxyImage,
			ManagerImage: proxySpec.ManagerImage,
			Port:         443,
			XDSPort:      18000,
			LogLevel:     "info",
		},
	}
}

// networkPolicyForInfra returns a NetworkPolicy for the HCP namespace to allow infrastructure traffic
func (r *InfraReconciler) networkPolicyForInfra(infra *hostedclusterv1alpha1.Infra) *networkingv1.NetworkPolicy {
	proxySpec := infra.Spec.InfraComponents.Proxy

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "allow-infrastructure",
			Namespace: proxySpec.ControlPlaneNamespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				// Empty selector matches all pods in the namespace
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"hostedcluster.densityops.com/network-policy-group": "infrastructure",
								},
							},
						},
					},
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *InfraReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hostedclusterv1alpha1.Infra{}).
		Owns(&hostedclusterv1alpha1.DHCPServer{}).
		Owns(&hostedclusterv1alpha1.DNSServer{}).
		Owns(&hostedclusterv1alpha1.ProxyServer{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Named("infra").
		Complete(r)
}
