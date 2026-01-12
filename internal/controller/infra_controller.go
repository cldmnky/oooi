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

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
)

// InfraReconciler reconciles a Infra object
type InfraReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=infras,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=infras/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=infras/finalizers,verbs=update
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=dhcpservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=dnsservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=proxyservers,verbs=get;list;watch;create;update;patch;delete

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

	// Create DHCPServer CR if DHCP is enabled
	if infra.Spec.InfraComponents.DHCP.Enabled {
		dhcpServer := r.dhcpServerForInfra(infra)
		if err := ctrl.SetControllerReference(infra, dhcpServer, r.Scheme); err != nil {
			log.Error(err, "Failed to set controller reference for DHCPServer")
			return ctrl.Result{}, err
		}

		foundDHCPServer := &hostedclusterv1alpha1.DHCPServer{}
		err = r.Get(ctx, types.NamespacedName{Name: dhcpServer.Name, Namespace: dhcpServer.Namespace}, foundDHCPServer)
		if err != nil && errors.IsNotFound(err) {
			log.Info("Creating a new DHCPServer", "DHCPServer.Namespace", dhcpServer.Namespace, "DHCPServer.Name", dhcpServer.Name)
			err = r.Create(ctx, dhcpServer)
			if err != nil {
				log.Error(err, "Failed to create new DHCPServer")
				return ctrl.Result{}, err
			}
		} else if err != nil {
			log.Error(err, "Failed to get DHCPServer")
			return ctrl.Result{}, err
		} else {
			// Update existing DHCPServer if spec differs
			if !reflect.DeepEqual(foundDHCPServer.Spec, dhcpServer.Spec) {
				log.Info("Updating DHCPServer spec", "DHCPServer.Name", dhcpServer.Name)
				foundDHCPServer.Spec = dhcpServer.Spec
				if err := r.Update(ctx, foundDHCPServer); err != nil {
					log.Error(err, "Failed to update DHCPServer")
					return ctrl.Result{}, err
				}
			}
		}
	}

	// Create DNSServer CR if DNS is enabled
	if infra.Spec.InfraComponents.DNS.Enabled {
		dnsServer := r.dnsServerForInfra(infra)
		if err := ctrl.SetControllerReference(infra, dnsServer, r.Scheme); err != nil {
			log.Error(err, "Failed to set controller reference for DNSServer")
			return ctrl.Result{}, err
		}

		foundDNSServer := &hostedclusterv1alpha1.DNSServer{}
		err = r.Get(ctx, types.NamespacedName{Name: dnsServer.Name, Namespace: dnsServer.Namespace}, foundDNSServer)
		if err != nil && errors.IsNotFound(err) {
			log.Info("Creating a new DNSServer", "DNSServer.Namespace", dnsServer.Namespace, "DNSServer.Name", dnsServer.Name)
			err = r.Create(ctx, dnsServer)
			if err != nil {
				log.Error(err, "Failed to create new DNSServer")
				return ctrl.Result{}, err
			}
		} else if err != nil {
			log.Error(err, "Failed to get DNSServer")
			return ctrl.Result{}, err
		}
	}

	// Create ProxyServer CR if Proxy is enabled
	if infra.Spec.InfraComponents.Proxy.Enabled {
		proxyServer := r.proxyServerForInfra(infra)
		if err := ctrl.SetControllerReference(infra, proxyServer, r.Scheme); err != nil {
			log.Error(err, "Failed to set controller reference for ProxyServer")
			return ctrl.Result{}, err
		}

		foundProxyServer := &hostedclusterv1alpha1.ProxyServer{}
		err = r.Get(ctx, types.NamespacedName{Name: proxyServer.Name, Namespace: proxyServer.Namespace}, foundProxyServer)
		if err != nil && errors.IsNotFound(err) {
			log.Info("Creating a new ProxyServer", "ProxyServer.Namespace", proxyServer.Namespace, "ProxyServer.Name", proxyServer.Name)
			err = r.Create(ctx, proxyServer)
			if err != nil {
				log.Error(err, "Failed to create new ProxyServer")
				return ctrl.Result{}, err
			}
		} else if err != nil {
			log.Error(err, "Failed to get ProxyServer")
			return ctrl.Result{}, err
		}
	}

	// Update status
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
			Port:            443,
			TargetService:   "kube-apiserver",
			TargetPort:      6443,
			TargetNamespace: controlPlaneNamespace,
			Protocol:        "TCP",
			TimeoutSeconds:  30,
		},
		{
			Name:            "kube-apiserver-internal",
			Hostname:        "api-int." + hostedClusterDomain,
			Port:            443,
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
			Name:            "ignition",
			Hostname:        "ignition." + hostedClusterDomain,
			Port:            443,
			TargetService:   "ignition",
			TargetPort:      22623,
			TargetNamespace: controlPlaneNamespace,
			Protocol:        "TCP",
			TimeoutSeconds:  30,
		},
		{
			Name:            "konnectivity",
			Hostname:        "konnectivity." + hostedClusterDomain,
			Port:            443,
			TargetService:   "konnectivity",
			TargetPort:      8132,
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

// SetupWithManager sets up the controller with the Manager.
func (r *InfraReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hostedclusterv1alpha1.Infra{}).
		Owns(&hostedclusterv1alpha1.DHCPServer{}).
		Owns(&hostedclusterv1alpha1.DNSServer{}).
		Owns(&hostedclusterv1alpha1.ProxyServer{}).
		Named("infra").
		Complete(r)
}
