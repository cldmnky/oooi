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
	"fmt"
	"net"
	"reflect"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
)

const (
	// PhaseDegraded indicates a component is in a degraded state
	PhaseDegraded = "Degraded"
	// PhasePending indicates a component is waiting for a dependency
	PhasePending = "Pending"

	phaseReady           = "Ready"
	appsHTTPBackendName  = "apps-http"
	appsHTTPSBackendName = "apps-https"
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

	// Resolve explicit attachments (or the implicit legacy binding) before
	// reconciling components so DNS/proxy children see a consistent view set.
	agg, err := r.aggregateAttachments(ctx, infra)
	if err != nil {
		log.Error(err, "Failed to list InfraClusterAttachments")
		return ctrl.Result{}, err
	}

	// Legacy path: drive apps ingress here so the discovered endpoint feeds
	// DNS/proxy generation in this same pass. Explicit attachments reconcile
	// their own ingress and publish endpoints through attachment status.
	var appsIngressResult ctrl.Result
	if len(agg.views) == 1 && !agg.views[0].explicit {
		appsIngressResult = r.reconcileImplicitAppsIngress(ctx, infra, &agg.views[0])
	}

	// Reconcile infrastructure components
	if err := r.reconcileDHCPComponent(ctx, infra); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileDNSComponent(ctx, infra, agg.views); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileProxyComponent(ctx, infra, agg.views); err != nil {
		return ctrl.Result{}, err
	}

	// Update status
	if err := r.updateInfraStatus(ctx, infra, agg); err != nil {
		return ctrl.Result{}, err
	}
	if appsIngressResult.Requeue || appsIngressResult.RequeueAfter > 0 {
		return appsIngressResult, nil
	}
	return ctrl.Result{}, nil
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
func (r *InfraReconciler) reconcileDNSComponent(ctx context.Context, infra *hostedclusterv1alpha1.Infra, views []attachmentView) error {
	log := logf.FromContext(ctx)

	if !infra.Spec.InfraComponents.DNS.Enabled {
		return nil
	}

	dnsServer := r.dnsServerForInfra(infra, views)
	if internalProxy := dnsServer.Spec.NetworkConfig.InternalProxyIP; internalProxy != "" && net.ParseIP(internalProxy) == nil {
		resolvedProxyIP, err := r.resolveInternalProxyService(ctx, internalProxy)
		if err != nil {
			return err
		}
		dnsServer.Spec.NetworkConfig.InternalProxyIP = resolvedProxyIP
	}
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

// resolveInternalProxyService resolves an internal proxy Service reference to
// its ClusterIP so the generated CoreDNS hosts records remain valid A records.
// A missing or headless Service is treated as pending; the owning ProxyServer
// event will cause Infra to retry once the Service has a ClusterIP.
func (r *InfraReconciler) resolveInternalProxyService(ctx context.Context, reference string) (string, error) {
	parts := strings.Split(strings.TrimSuffix(reference, "."), ".")
	if len(parts) < 2 {
		return "", errors.NewBadRequest("internalProxyService must be a ClusterIP or service.namespace DNS name")
	}

	service := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Name: parts[0], Namespace: parts[1]}, service); err != nil {
		if errors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	if service.Spec.ClusterIP == "" || service.Spec.ClusterIP == corev1.ClusterIPNone {
		return "", nil
	}
	return service.Spec.ClusterIP, nil
}

// reconcileProxyComponent handles proxy server creation, updates, and network policy
func (r *InfraReconciler) reconcileProxyComponent(ctx context.Context, infra *hostedclusterv1alpha1.Infra, views []attachmentView) error {
	log := logf.FromContext(ctx)

	if !infra.Spec.InfraComponents.Proxy.Enabled {
		return nil
	}

	proxyServer := r.proxyServerForInfra(infra, views)
	if len(proxyServer.Spec.Backends) == 0 {
		// Every attachment was excluded (e.g. duplicate-hostname conflict).
		// Leave any existing proxy untouched rather than applying an invalid
		// empty backend set; the Infra condition reports the conflict.
		log.Info("No valid SNI backends after aggregation; leaving ProxyServer unchanged")
		return nil
	}
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

	// Create NetworkPolicy in HCP namespace if ControlPlaneNamespace is specified.
	// This covers only the implicit legacy binding; explicit attachments manage
	// their own policies through the InfraClusterAttachment controller.
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

// attachmentView is the normalized per-cluster input derived either from an
// explicit InfraClusterAttachment or synthesized from this Infra's legacy
// cluster-specific fields.
type attachmentView struct {
	name                  string // attachment name; legacyAttachmentName when synthesized
	explicit              bool
	createNetworkPolicy   bool
	hostedClusterRef      hostedclusterv1alpha1.HostedClusterReference
	apiServerService      string
	controlPlaneNamespace string
	domain                string // "<clusterName>.<baseDomain>"; may be "." for empty legacy fields
	appsConfig            hostedclusterv1alpha1.AppsIngressConfig
	appsExternalIP        string // wildcard DNS answer; empty until the VIP exists
	appsEndpoint          string // IP or hostname used for Envoy apps backends
	ready                 bool   // explicit: Ready condition; implicit: always
}

const (
	legacyAttachmentName       = "<legacy>"
	reasonDuplicateHostname    = "DuplicateHostname"
	reasonDuplicateHostedClust = "DuplicateHostedCluster"
)

// aggregation is the resolved per-cluster view set for one reconcile pass,
// plus observability about how it was built.
type aggregation struct {
	views          []attachmentView
	legacyIgnored  bool
	total          int32
	ready          int32
	conflicts      []string
	degradedReason string
}

// validDomain reports whether a computed hosted-cluster domain is usable for
// DNS records and SNI routes.
func validDomain(domain string) bool {
	return domain != "" && domain != "."
}

// normalizeHostedClusterRef applies the historical default namespace.
func normalizeHostedClusterRef(ref hostedclusterv1alpha1.HostedClusterReference) hostedclusterv1alpha1.HostedClusterReference {
	if ref.Namespace == "" {
		ref.Namespace = "clusters"
	}
	return ref
}

// hasLegacyClusterConfig reports whether this Infra still carries
// cluster-specific settings in its deprecated fields.
func hasLegacyClusterConfig(infra *hostedclusterv1alpha1.Infra) bool {
	dnsSpec := infra.Spec.InfraComponents.DNS
	proxySpec := infra.Spec.InfraComponents.Proxy
	return dnsSpec.ClusterName != "" || dnsSpec.BaseDomain != "" ||
		proxySpec.ControlPlaneNamespace != "" ||
		infra.Spec.AppsIngress.HostedClusterRef.Name != ""
}

// legacyAttachmentView synthesizes the implicit single-cluster binding from
// this Infra's own fields, preserving pre-multi-cluster behavior exactly.
func legacyAttachmentView(infra *hostedclusterv1alpha1.Infra) attachmentView {
	proxySpec := infra.Spec.InfraComponents.Proxy
	cpns := proxySpec.ControlPlaneNamespace
	if cpns == "" {
		cpns = infra.Namespace + "-" + infra.Name
	}
	hcRef := normalizeHostedClusterRef(infra.Spec.AppsIngress.HostedClusterRef)
	return attachmentView{
		name:                  legacyAttachmentName,
		explicit:              false,
		ready:                 true,
		createNetworkPolicy:   proxySpec.ControlPlaneNamespace != "",
		hostedClusterRef:      hcRef,
		apiServerService:      proxySpec.APIServerService,
		controlPlaneNamespace: cpns,
		domain:                infra.Spec.InfraComponents.DNS.ClusterName + "." + infra.Spec.InfraComponents.DNS.BaseDomain,
		appsConfig:            infra.Spec.AppsIngress,
	}
}

// attachmentFromAttachment builds a view from an explicit InfraClusterAttachment.
func attachmentFromAttachment(att *hostedclusterv1alpha1.InfraClusterAttachment) attachmentView {
	hcRef := normalizeHostedClusterRef(att.Spec.HostedClusterRef)
	cpns := att.Spec.ControlPlaneNamespace
	if cpns == "" {
		cpns = hcRef.Namespace + "-" + hcRef.Name
	}
	view := attachmentView{
		name:                  att.Name,
		explicit:              true,
		ready:                 meta.IsStatusConditionTrue(att.Status.Conditions, phaseReady),
		createNetworkPolicy:   false, // owned by the attachment controller
		hostedClusterRef:      hcRef,
		apiServerService:      att.Spec.APIServerService,
		controlPlaneNamespace: cpns,
		domain:                att.Spec.DNS.ClusterName + "." + att.Spec.DNS.BaseDomain,
		appsConfig:            att.Spec.AppsIngress,
	}
	status := att.Status.AppsIngressStatus
	if att.Spec.AppsIngress.Enabled && status.Phase == phaseReady {
		view.appsEndpoint = status.ExternalIP
		if view.appsEndpoint == "" {
			view.appsEndpoint = status.ExternalHostname
		}
		view.appsExternalIP = status.ExternalIP // hostname-only endpoints have no A record
	}
	return view
}

// aggregateAttachments resolves every InfraClusterAttachment targeting infra
// into deterministic views, detecting duplicate HostedCluster references and
// duplicate domains. When no explicit attachments exist, a single implicit
// view synthesized from legacy fields preserves historical behavior.
func (r *InfraReconciler) aggregateAttachments(ctx context.Context, infra *hostedclusterv1alpha1.Infra) (*aggregation, error) {
	list := &hostedclusterv1alpha1.InfraClusterAttachmentList{}
	if err := r.List(ctx, list, client.InNamespace(infra.Namespace)); err != nil {
		return nil, err
	}
	mine := make([]hostedclusterv1alpha1.InfraClusterAttachment, 0, len(list.Items))
	for _, att := range list.Items {
		// A terminating attachment is no longer desired infrastructure. Do not
		// retain its DNS/SNI records while its cleanup finalizer runs.
		if att.DeletionTimestamp.IsZero() && att.Spec.InfraRef.Name == infra.Name {
			mine = append(mine, att)
		}
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].Name < mine[j].Name })

	agg := &aggregation{total: int32(len(mine))}
	for i := range mine {
		if meta.IsStatusConditionTrue(mine[i].Status.Conditions, phaseReady) {
			agg.ready++
		}
	}
	if len(mine) == 0 {
		agg.views = []attachmentView{legacyAttachmentView(infra)}
		return agg, nil
	}
	agg.legacyIgnored = hasLegacyClusterConfig(infra)

	seenHC := map[string]string{}
	seenDomain := map[string]string{}
	excluded := map[string]bool{}
	for i := range mine {
		att := &mine[i]
		domain := att.Spec.DNS.ClusterName + "." + att.Spec.DNS.BaseDomain
		hcRef := normalizeHostedClusterRef(att.Spec.HostedClusterRef)
		hcKey := hcRef.Namespace + "/" + hcRef.Name
		if owner, ok := seenHC[hcKey]; ok && !excluded[att.Name] {
			excluded[att.Name] = true
			excluded[owner] = true
			agg.conflicts = append(agg.conflicts,
				fmt.Sprintf("attachments %q and %q reference HostedCluster %s", owner, att.Name, hcKey))
			agg.degradedReason = reasonDuplicateHostedClust
			continue
		}
		if owner, ok := seenDomain[domain]; ok && !excluded[att.Name] {
			excluded[att.Name] = true
			excluded[owner] = true
			agg.conflicts = append(agg.conflicts,
				fmt.Sprintf("attachments %q and %q declare domain %q", owner, att.Name, domain))
			agg.degradedReason = reasonDuplicateHostname
			continue
		}
		seenHC[hcKey] = att.Name
		seenDomain[domain] = att.Name
	}
	for i := range mine {
		att := &mine[i]
		if excluded[att.Name] {
			continue
		}
		agg.views = append(agg.views, attachmentFromAttachment(att))
	}
	sort.Slice(agg.conflicts, func(i, j int) bool { return agg.conflicts[i] < agg.conflicts[j] })
	return agg, nil
}

// legacyHostedFactory adapts the shared apps-ingress machinery to the
// Infra-owned hosted-cluster client source.
func (r *InfraReconciler) legacyHostedFactory(infra *hostedclusterv1alpha1.Infra, target appsIngressTarget) hostedClusterFactory {
	return func(ctx context.Context) (client.Client, error) {
		if r.HostedClusterClientFactory != nil {
			return r.HostedClusterClientFactory(ctx, infra)
		}
		return defaultHostedClusterClient(ctx, r.Client, target.HostedClusterRef, target.APIServerService, target.ControlPlaneNamespace)
	}
}

// reconcileImplicitAppsIngress runs the shared apps-ingress automation for the
// synthesized legacy view and mirrors the resulting endpoint onto the view so
// DNS/proxy generation can consume it in the same pass.
func (r *InfraReconciler) reconcileImplicitAppsIngress(ctx context.Context, infra *hostedclusterv1alpha1.Infra, view *attachmentView) ctrl.Result {
	target := appsIngressTarget{
		AttachmentName:        view.name,
		HostedClusterRef:      view.hostedClusterRef,
		APIServerService:      view.apiServerService,
		ControlPlaneNamespace: view.controlPlaneNamespace,
		Config:                view.appsConfig,
	}
	result := reconcileAppsIngressCore(ctx, r.legacyHostedFactory(infra, target), target, &infra.Status.AppsIngressStatus)
	status := infra.Status.AppsIngressStatus
	if view.appsConfig.Enabled && status.Phase == phaseReady {
		view.appsEndpoint = status.ExternalIP
		if view.appsEndpoint == "" {
			view.appsEndpoint = status.ExternalHostname
		}
		view.appsExternalIP = status.ExternalIP
	}
	return result
}

func hasReadyNode(nodes []corev1.Node) bool {
	for _, node := range nodes {
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				return true
			}
		}
	}
	return false
}

func setAppsIngressLastSyncTime(status *hostedclusterv1alpha1.AppsIngressStatus, previous hostedclusterv1alpha1.AppsIngressStatus) {
	current := *status
	current.LastSyncTime = previous.LastSyncTime
	if !reflect.DeepEqual(current, previous) {
		status.LastSyncTime = metav1.Now()
	}
}

func applyUnstructured(ctx context.Context, c client.Client, desired *unstructured.Unstructured) error {
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
func (r *InfraReconciler) updateInfraStatus(ctx context.Context, infra *hostedclusterv1alpha1.Infra, agg *aggregation) error {
	log := logf.FromContext(ctx)
	originalStatus := *infra.Status.DeepCopy()

	infra.Status.ObservedGeneration = infra.Generation
	condition := metav1.Condition{
		Type:               phaseReady,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: infra.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             "ReconciliationSucceeded",
		Message:            "Infrastructure components provisioned successfully",
	}
	if agg != nil && agg.degradedReason != "" {
		condition.Status = metav1.ConditionFalse
		condition.Reason = agg.degradedReason
		condition.Message = strings.Join(agg.conflicts, "; ")
	}
	if infra.Spec.AppsIngress.Enabled && infra.Status.AppsIngressStatus.Phase != phaseReady {
		condition.Status = metav1.ConditionFalse
		condition.Reason = infra.Status.AppsIngressStatus.Reason
		condition.Message = infra.Status.AppsIngressStatus.Message
		if condition.Reason == "" {
			condition.Reason = "AppsIngressPending"
		}
		if condition.Message == "" {
			condition.Message = "Apps ingress is not ready"
		}
	}

	condition = preserveConditionTransitionTime(infra.Status.Conditions, condition)
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
	infra.Status.Attachments = nil
	if agg != nil && (agg.total > 0 || agg.legacyIgnored) {
		infra.Status.Attachments = &hostedclusterv1alpha1.AttachmentsSummary{
			Total:               agg.total,
			Ready:               agg.ready,
			LegacyFieldsIgnored: agg.legacyIgnored,
		}
	}

	if reflect.DeepEqual(originalStatus, infra.Status) {
		return nil
	}

	// Optimistic-concurrency conflicts are expected when child-watch events
	// trigger an immediate re-reconcile before the cache reflects our own
	// previous status write. Retry inline against the latest resourceVersion;
	// falling into the workqueue's exponential backoff here stalls recovery
	// for minutes.
	for attempt := 0; ; attempt++ {
		err := r.Status().Update(ctx, infra)
		if err == nil {
			return nil
		}
		if !errors.IsConflict(err) || attempt >= 4 {
			log.Error(err, "Failed to update Infra status")
			return err
		}
		fresh := &hostedclusterv1alpha1.Infra{}
		if err := r.Get(ctx, types.NamespacedName{Name: infra.Name, Namespace: infra.Namespace}, fresh); err != nil {
			return err
		}
		desired := infra.Status
		fresh.Status = desired
		*infra = *fresh
	}
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

// dnsServerForInfra returns a DNSServer object for the Infra whose static
// entries cover every attached hosted cluster.
func (r *InfraReconciler) dnsServerForInfra(infra *hostedclusterv1alpha1.Infra, views []attachmentView) *hostedclusterv1alpha1.DNSServer {
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

	externalProxyIP := infra.Spec.InfraComponents.Proxy.ServerIP
	internalProxyIP := infra.Spec.InfraComponents.Proxy.InternalProxyService

	// Static DNS entries per attached cluster. Each view contributes its HCP
	// endpoint names (answered with the VLAN proxy IP); a Ready apps-ingress
	// endpoint contributes the *.apps wildcard. Entries resolve to the shared
	// proxy ClusterIP in the pod-network view via InternalProxyIP.
	staticEntries := make([]hostedclusterv1alpha1.DNSStaticEntry, 0, len(views)*6)
	hostedClusterDomain := ""
	for _, view := range views {
		if !validDomain(view.domain) {
			continue
		}
		if hostedClusterDomain == "" {
			hostedClusterDomain = view.domain
		}
		for _, prefix := range []string{"api.", "api-int.", "oauth.", "ignition.", "konnectivity."} {
			appendUniqueEntry(&staticEntries, prefix+view.domain, externalProxyIP)
		}
		if view.appsExternalIP != "" {
			appendUniqueEntry(&staticEntries, "*.apps."+view.domain, view.appsExternalIP)
			// Legacy quirk preserved for the implicit binding only: an explicit
			// appsIngress.baseDomain override publishes an additional wildcard.
			if !view.explicit && view.appsConfig.BaseDomain != "" && view.appsConfig.BaseDomain != dnsSpec.BaseDomain {
				appendUniqueEntry(&staticEntries, "*.apps."+view.appsConfig.BaseDomain, view.appsExternalIP)
			}
		}
	}

	// Preserve the historical HostedClusterDomain value when no valid domain
	// exists (the child CRD requires a non-empty string).
	if hostedClusterDomain == "" {
		hostedClusterDomain = dnsSpec.ClusterName + "." + dnsSpec.BaseDomain
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

// appendUniqueEntry appends a static entry unless an entry with the same
// hostname already exists.
func appendUniqueEntry(entries *[]hostedclusterv1alpha1.DNSStaticEntry, hostname, ip string) {
	for _, e := range *entries {
		if e.Hostname == hostname {
			return
		}
	}
	*entries = append(*entries, hostedclusterv1alpha1.DNSStaticEntry{Hostname: hostname, IP: ip})
}

// backendNamePrefix returns the per-attachment prefix applied to Envoy
// backend names so multiple attachments can coexist on one ProxyServer.
// The implicit legacy binding keeps unprefixed (historical) names.
func backendNamePrefix(view attachmentView) string {
	if !view.explicit {
		return ""
	}
	prefix := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, view.name)
	const maxBase = len("kube-apiserver-kubernetes-hostname")
	const maxPrefix = 63 - maxBase - 1
	if len(prefix) > maxPrefix {
		prefix = strings.TrimRight(prefix[:maxPrefix], "-")
	}
	return prefix + "-"
}

// hcpBackendsForView builds the standard control-plane SNI backends for one
// attached cluster. Unqualified Kubernetes service aliases are emitted only
// when this proxy serves a single implicit binding; they are ambiguous on a
// shared proxy.
func hcpBackendsForView(view attachmentView, prefix string, includeKubeAliases bool) []hostedclusterv1alpha1.ProxyBackend {
	domain := view.domain
	cpns := view.controlPlaneNamespace
	backends := []hostedclusterv1alpha1.ProxyBackend{
		{
			Name:            prefix + "kube-apiserver",
			Hostname:        "api." + domain,
			Port:            6443,
			TargetService:   "kube-apiserver",
			TargetPort:      6443,
			TargetNamespace: cpns,
			Protocol:        "TCP",
			TimeoutSeconds:  30,
		},
		{
			Name:            prefix + "kube-apiserver-internal",
			Hostname:        "api-int." + domain,
			Port:            6443,
			TargetService:   "kube-apiserver",
			TargetPort:      6443,
			TargetNamespace: cpns,
			Protocol:        "TCP",
			TimeoutSeconds:  30,
		},
		{
			Name:            prefix + "oauth-openshift",
			Hostname:        "oauth." + domain,
			Port:            443,
			TargetService:   "oauth-openshift",
			TargetPort:      6443,
			TargetNamespace: cpns,
			Protocol:        "TCP",
			TimeoutSeconds:  30,
		},
		{
			Name:            prefix + "ignition-server",
			Hostname:        "ignition." + domain,
			Port:            443,
			TargetService:   "ignition-server-proxy",
			TargetPort:      443,
			TargetNamespace: cpns,
			Protocol:        "TCP",
			TimeoutSeconds:  30,
		},
	}
	if includeKubeAliases {
		backends = append(backends, hostedclusterv1alpha1.ProxyBackend{
			Name:     prefix + "kube-apiserver-kubernetes-hostname",
			Hostname: "kubernetes." + domain,
			AlternateHostnames: []string{
				"kubernetes",
				"kubernetes.default",
				"kubernetes.default.svc",
				"kubernetes.default.svc.cluster.local",
			},
			Port:            443,
			TargetService:   "kube-apiserver",
			TargetPort:      6443,
			TargetNamespace: cpns,
			Protocol:        "TCP",
			TimeoutSeconds:  30,
		})
	}
	backends = append(backends, hostedclusterv1alpha1.ProxyBackend{
		Name:            prefix + "konnectivity-server",
		Hostname:        "konnectivity." + domain,
		Port:            443,
		TargetService:   "konnectivity-server",
		TargetPort:      8091,
		TargetNamespace: cpns,
		Protocol:        "TCP",
		TimeoutSeconds:  30,
	})
	return backends
}

// appsBackendsForView builds the wildcard apps backends for one attached
// cluster once its LoadBalancer endpoint is Ready.
func appsBackendsForView(view attachmentView, prefix string) []hostedclusterv1alpha1.ProxyBackend {
	if view.appsEndpoint == "" || !validDomain(view.domain) {
		return nil
	}
	httpPort := view.appsConfig.Ports.HTTP
	if httpPort == 0 {
		httpPort = 80
	}
	httpsPort := view.appsConfig.Ports.HTTPS
	if httpsPort == 0 {
		httpsPort = 443
	}
	wildcard := "*.apps." + view.domain
	return []hostedclusterv1alpha1.ProxyBackend{
		{
			Name:            prefix + "apps-http",
			Hostname:        wildcard,
			Port:            httpPort,
			TargetService:   view.appsEndpoint,
			TargetPort:      httpPort,
			TargetNamespace: "default",
			Protocol:        "TCP",
			TimeoutSeconds:  30,
		},
		{
			Name:            prefix + "apps-https",
			Hostname:        wildcard,
			Port:            httpsPort,
			TargetService:   view.appsEndpoint,
			TargetPort:      httpsPort,
			TargetNamespace: "default",
			Protocol:        "TCP",
			TimeoutSeconds:  30,
		},
	}
}

// proxyServerForInfra returns a ProxyServer object whose SNI backends cover
// every attached hosted cluster.
func (r *InfraReconciler) proxyServerForInfra(infra *hostedclusterv1alpha1.Infra, views []attachmentView) *hostedclusterv1alpha1.ProxyServer {
	proxySpec := infra.Spec.InfraComponents.Proxy

	nadName := infra.Spec.NetworkConfig.NetworkAttachmentDefinition
	nadNamespace := infra.Namespace
	if infra.Spec.NetworkConfig.NetworkAttachmentNamespace != "" {
		nadNamespace = infra.Spec.NetworkConfig.NetworkAttachmentNamespace
	}

	singleImplicit := len(views) == 1 && !views[0].explicit

	var backends []hostedclusterv1alpha1.ProxyBackend
	for _, view := range views {
		if !validDomain(view.domain) {
			continue
		}
		prefix := backendNamePrefix(view)
		backends = append(backends, hcpBackendsForView(view, prefix, singleImplicit)...)
		backends = append(backends, appsBackendsForView(view, prefix)...)
	}

	externalService := proxySpec.ExternalService
	if externalService.Enabled && externalService.PublishAttachmentOAuths {
		externalService.Annotations = mergeOAuthHostnames(externalService.Annotations, readyAttachmentDomains(views))
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
			Backends:        backends,
			ProxyImage:      proxySpec.ProxyImage,
			ManagerImage:    proxySpec.ManagerImage,
			Port:            443,
			XDSPort:         18000,
			LogLevel:        "info",
			ExternalService: externalService,
		},
	}
}

// hostnameAnnotationKey is the annotation ExternalDNS consumes for explicit
// hostnames. Multiple names are supported as a comma-separated list.
const hostnameAnnotationKey = "external-dns.alpha.kubernetes.io/hostname"

// readyAttachmentDomains returns sorted oauth FQDNs of explicit attachments
// whose Ready condition is True.
func readyAttachmentDomains(views []attachmentView) []string {
	domains := make([]string, 0, len(views))
	for _, v := range views {
		if v.explicit && v.ready && validDomain(v.domain) {
			domains = append(domains, "oauth."+v.domain)
		}
	}
	sort.Strings(domains)
	return domains
}

// mergeOAuthHostnames returns a copy of annotations with the given hostnames
// folded into the ExternalDNS hostname annotation as a comma-separated list,
// preserving any names the user configured.
func mergeOAuthHostnames(annotations map[string]string, hostnames []string) map[string]string {
	out := make(map[string]string, len(annotations)+1)
	for k, v := range annotations {
		out[k] = v
	}
	if len(hostnames) == 0 {
		return out
	}
	seen := map[string]bool{}
	userParts := make([]string, 0, len(annotations[hostnameAnnotationKey]))
	for _, p := range strings.Split(annotations[hostnameAnnotationKey], ",") {
		p = strings.TrimSpace(p)
		key := hostnameKey(p)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		userParts = append(userParts, p)
	}
	added := make([]string, 0, len(hostnames))
	for _, h := range hostnames {
		h = strings.TrimSpace(h)
		key := hostnameKey(h)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		added = append(added, h)
	}
	sort.Strings(added)
	all := append(userParts, added...)
	out[hostnameAnnotationKey] = strings.Join(all, ",")
	return out
}

func hostnameKey(hostname string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(hostname)), ".")
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
		Watches(
			&hostedclusterv1alpha1.InfraClusterAttachment{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
				att, ok := o.(*hostedclusterv1alpha1.InfraClusterAttachment)
				if !ok || att.Spec.InfraRef.Name == "" {
					return nil
				}
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{Name: att.Spec.InfraRef.Name, Namespace: att.Namespace},
				}}
			}),
		).
		Named("infra").
		Complete(r)
}
