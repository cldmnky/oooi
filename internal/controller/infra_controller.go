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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubevirtv1 "kubevirt.io/api/core/v1"

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
}

// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=infras,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=infras/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=infras/finalizers,verbs=update
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=dhcpservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=dnsservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=proxyservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachineinstances,verbs=get;list;watch

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

	// Resolve attachments before reconciling components so DNS/proxy children
	// see a consistent view set.
	agg, err := r.aggregateAttachments(ctx, infra)
	if err != nil {
		log.Error(err, "Failed to list InfraClusterAttachments")
		return ctrl.Result{}, err
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
	if len(views) == 0 {
		server := &hostedclusterv1alpha1.DNSServer{}
		err := r.Get(ctx, types.NamespacedName{Name: infra.Name + "-dns", Namespace: infra.Namespace}, server)
		if errors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return client.IgnoreNotFound(r.Delete(ctx, server))
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
		// Every attachment was excluded (e.g. duplicate-hostname conflict), or
		// the Infra has no attachments. Remove stale routing rather than applying
		// an invalid empty backend set.
		server := &hostedclusterv1alpha1.ProxyServer{}
		err := r.Get(ctx, types.NamespacedName{Name: proxyServer.Name, Namespace: proxyServer.Namespace}, server)
		if errors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		log.Info("No valid SNI backends after aggregation; deleting ProxyServer", "name", proxyServer.Name)
		return client.IgnoreNotFound(r.Delete(ctx, server))
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

	return nil
}

// attachmentView is the normalized per-cluster input derived from an
// InfraClusterAttachment.
type attachmentView struct {
	name                  string
	hostedClusterRef      hostedclusterv1alpha1.HostedClusterReference
	apiServerService      string
	controlPlaneNamespace string
	domain                string // "<clusterName>.<baseDomain>"
	appsConfig            hostedclusterv1alpha1.AppsIngressConfig
	appsExternalIP        string // wildcard DNS answer; empty until the VIP exists
	appsEndpoint          string // IP or hostname used for Envoy apps backends
	sourceCIDRs           []string
	ready                 bool
}

const (
	reasonDuplicateHostname    = "DuplicateHostname"
	reasonDuplicateHostedClust = "DuplicateHostedCluster"
	reasonDuplicateSourceIP    = "DuplicateSourceIP"
)

// Suffixes appended to the per-attachment prefix to form backend names.
// backendNamePrefix budgets for the longest one, so a new suffix must not
// exceed its length or generated names will break the 63-char limit.
const (
	suffixKubeAPIServerInternal = "kube-apiserver-internal" // longest suffix
	suffixKubernetesHostname    = "kubernetes-hostname"
)

// aggregation is the resolved per-cluster view set for one reconcile pass,
// plus observability about how it was built.
type aggregation struct {
	views          []attachmentView
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

// attachmentFromAttachment builds a view from an InfraClusterAttachment.
func attachmentFromAttachment(att *hostedclusterv1alpha1.InfraClusterAttachment) attachmentView {
	hcRef := normalizeHostedClusterRef(att.Spec.HostedClusterRef)
	cpns := att.Spec.ControlPlaneNamespace
	if cpns == "" {
		cpns = hcRef.Namespace + "-" + hcRef.Name
	}
	view := attachmentView{
		name:                  att.Name,
		ready:                 meta.IsStatusConditionTrue(att.Status.Conditions, phaseReady),
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

// maxAliasSourcePrefixRanges matches kubebuilder MaxItems on
// ProxyBackend.sourcePrefixRanges so generated specs always pass admission.
const maxAliasSourcePrefixRanges = 256

// resolveAttachmentSourceCIDRs discovers VM IPs in the attachment's
// control-plane namespace that lie within infraCIDR and returns them as /32
// CIDRs. Empty infraCIDR or no matching VMIs returns nil.
func (r *InfraReconciler) resolveAttachmentSourceCIDRs(ctx context.Context, att *hostedclusterv1alpha1.InfraClusterAttachment, infraCIDR string) []string {
	if infraCIDR == "" {
		return nil
	}
	_, cidrNet, err := net.ParseCIDR(infraCIDR)
	if err != nil {
		return nil
	}
	cpns := att.Spec.ControlPlaneNamespace
	if cpns == "" {
		hcRef := normalizeHostedClusterRef(att.Spec.HostedClusterRef)
		cpns = hcRef.Namespace + "-" + hcRef.Name
	}
	vmiList := &kubevirtv1.VirtualMachineInstanceList{}
	if err := r.List(ctx, vmiList, client.InNamespace(cpns)); err != nil {
		// No list permission or namespace missing -> treat as no source IPs.
		return nil
	}
	seen := map[string]bool{}
	var cidrs []string
	for _, vmi := range vmiList.Items {
		for _, iface := range vmi.Status.Interfaces {
			ips := iface.IPs
			if iface.IP != "" {
				ips = append(ips, iface.IP)
			}
			for _, ipStr := range ips {
				ip := net.ParseIP(strings.TrimSpace(ipStr))
				if ip == nil || !cidrNet.Contains(ip) {
					continue
				}
				cidr := ip.String() + "/32"
				if !seen[cidr] {
					seen[cidr] = true
					cidrs = append(cidrs, cidr)
				}
			}
		}
	}
	sort.Strings(cidrs)
	if len(cidrs) > maxAliasSourcePrefixRanges {
		log := logf.FromContext(ctx)
		log.Info("truncating source CIDRs to the ProxyBackend limit; VMs beyond the cap will not match kubernetes.* alias chains",
			"attachment", att.Name, "namespace", cpns, "found", len(cidrs), "kept", maxAliasSourcePrefixRanges)
		cidrs = cidrs[:maxAliasSourcePrefixRanges]
	}
	return cidrs
}

// aliasBackendsForView builds source-IP scoped kubernetes.* backends for one
// attached cluster. Returns nil when no sourceCIDRs are available.
func aliasBackendsForView(view attachmentView, prefix string) []hostedclusterv1alpha1.ProxyBackend {
	if len(view.sourceCIDRs) == 0 || !validDomain(view.domain) {
		return nil
	}
	return []hostedclusterv1alpha1.ProxyBackend{{
		Name:               prefix + suffixKubernetesHostname,
		Hostname:           "kubernetes",
		AlternateHostnames: []string{"kubernetes.default", "kubernetes.default.svc", "kubernetes.default.svc.cluster.local", "kubernetes." + view.domain},
		SourcePrefixRanges: view.sourceCIDRs,
		Port:               443,
		TargetService:      "kube-apiserver",
		TargetPort:         6443,
		TargetNamespace:    view.controlPlaneNamespace,
		Protocol:           "TCP",
		TimeoutSeconds:     30,
	}}
}

// aggregateAttachments resolves every InfraClusterAttachment targeting infra
// into deterministic views, detecting duplicate HostedCluster references and
// duplicate domains.
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
		return agg, nil
	}

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
	// Resolve VM source CIDRs per attachment. A source IP claimed by more than
	// one attachment makes only the kubernetes.* alias chains ambiguous, so the
	// conflicting CIDRs are dropped from every claimant; the attachments keep
	// their fully qualified SNI/DNS routing.
	cidrsByAttachment := make(map[string][]string, len(mine))
	claims := map[string][]string{}
	for i := range mine {
		att := &mine[i]
		if excluded[att.Name] {
			continue
		}
		cidrs := r.resolveAttachmentSourceCIDRs(ctx, att, infra.Spec.NetworkConfig.CIDR)
		cidrsByAttachment[att.Name] = cidrs
		for _, cidr := range cidrs {
			// resolveAttachmentSourceCIDRs deduplicates within one attachment,
			// so a second entry here always means a different attachment.
			claims[cidr] = append(claims[cidr], att.Name)
		}
	}
	conflictingCIDRs := map[string]bool{}
	for cidr, names := range claims {
		if len(names) < 2 {
			continue
		}
		sort.Strings(names)
		conflictingCIDRs[cidr] = true
		quotedNames := make([]string, len(names))
		for i, name := range names {
			quotedNames[i] = fmt.Sprintf("%q", name)
		}
		agg.conflicts = append(agg.conflicts,
			fmt.Sprintf("attachments %s share source CIDR %q", strings.Join(quotedNames, ", "), cidr))
		agg.degradedReason = reasonDuplicateSourceIP
	}
	if len(conflictingCIDRs) > 0 {
		for name, cidrs := range cidrsByAttachment {
			filtered := make([]string, 0, len(cidrs))
			for _, cidr := range cidrs {
				if !conflictingCIDRs[cidr] {
					filtered = append(filtered, cidr)
				}
			}
			cidrsByAttachment[name] = filtered
		}
	}
	for i := range mine {
		att := &mine[i]
		if excluded[att.Name] {
			continue
		}
		view := attachmentFromAttachment(att)
		view.sourceCIDRs = cidrsByAttachment[att.Name]
		agg.views = append(agg.views, view)
	}
	sort.Slice(agg.conflicts, func(i, j int) bool { return agg.conflicts[i] < agg.conflicts[j] })
	return agg, nil
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
	condition = preserveConditionTransitionTime(infra.Status.Conditions, condition)
	infra.Status.Conditions = []metav1.Condition{condition}
	infra.Status.ComponentStatus.DHCPReady = infra.Spec.InfraComponents.DHCP.Enabled
	infra.Status.ComponentStatus.DNSReady = infra.Spec.InfraComponents.DNS.Enabled
	infra.Status.ComponentStatus.ProxyReady = false
	if infra.Spec.InfraComponents.Proxy.Enabled && agg != nil {
		infra.Status.ComponentStatus.ProxyReady = len(r.proxyServerForInfra(infra, agg.views).Spec.Backends) > 0
	}
	infra.Status.Attachments = nil
	if agg != nil && agg.total > 0 {
		infra.Status.Attachments = &hostedclusterv1alpha1.AttachmentsSummary{
			Total: agg.total,
			Ready: agg.ready,
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
		}
	}
	hasAlias := false
	for _, view := range views {
		if len(view.sourceCIDRs) > 0 {
			hasAlias = true
			break
		}
	}
	if hasAlias {
		for _, alias := range []string{"kubernetes", "kubernetes.default", "kubernetes.default.svc", "kubernetes.default.svc.cluster.local"} {
			appendUniqueEntry(&staticEntries, alias, externalProxyIP)
		}
	}

	// The reconciler does not create a DNS child without an attachment, but keep
	// the generated object schema-valid for direct callers and updates.
	if hostedClusterDomain == "" {
		hostedClusterDomain = infra.Name
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
func backendNamePrefix(view attachmentView) string {
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
	const maxBase = len(suffixKubeAPIServerInternal) // must stay the longest suffix
	const maxPrefix = 63 - maxBase - 1
	if len(prefix) > maxPrefix {
		prefix = strings.TrimRight(prefix[:maxPrefix], "-")
	}
	return prefix + "-"
}

// hcpBackendsForView builds the standard fully qualified control-plane SNI
// backends for one attached cluster.
func hcpBackendsForView(view attachmentView, prefix string) []hostedclusterv1alpha1.ProxyBackend {
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
			Name:            prefix + suffixKubeAPIServerInternal,
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

	var backends []hostedclusterv1alpha1.ProxyBackend
	for _, view := range views {
		if !validDomain(view.domain) {
			continue
		}
		prefix := backendNamePrefix(view)
		backends = append(backends, hcpBackendsForView(view, prefix)...)
		backends = append(backends, appsBackendsForView(view, prefix)...)
		backends = append(backends, aliasBackendsForView(view, prefix)...)
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

// readyAttachmentDomains returns sorted oauth FQDNs of attachments whose Ready
// condition is True.
func readyAttachmentDomains(views []attachmentView) []string {
	domains := make([]string, 0, len(views))
	for _, v := range views {
		if v.ready && validDomain(v.domain) {
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

// SetupWithManager sets up the controller with the Manager.
func (r *InfraReconciler) SetupWithManager(mgr ctrl.Manager) error {
	enqueueInfra := func(queue workqueue.TypedRateLimitingInterface[reconcile.Request], object client.Object) {
		att, ok := object.(*hostedclusterv1alpha1.InfraClusterAttachment)
		if !ok || att.Spec.InfraRef.Name == "" {
			return
		}
		queue.Add(reconcile.Request{
			NamespacedName: types.NamespacedName{Name: att.Spec.InfraRef.Name, Namespace: att.Namespace},
		})
	}
	attachmentHandler := handler.Funcs{
		CreateFunc: func(_ context.Context, e event.CreateEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueueInfra(queue, e.Object)
		},
		UpdateFunc: func(_ context.Context, e event.UpdateEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			// Reconcile both sides of an InfraRef change so the old shared
			// resource drops routes and the new one gains them.
			enqueueInfra(queue, e.ObjectOld)
			enqueueInfra(queue, e.ObjectNew)
		},
		DeleteFunc: func(_ context.Context, e event.DeleteEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueueInfra(queue, e.Object)
		},
		GenericFunc: func(_ context.Context, e event.GenericEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueueInfra(queue, e.Object)
		},
	}
	enqueueInfraForVMI := func(ctx context.Context, obj client.Object, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
		vmi, ok := obj.(*kubevirtv1.VirtualMachineInstance)
		if !ok {
			return
		}
		// Find attachments whose control-plane namespace matches the VMI namespace.
		attList := &hostedclusterv1alpha1.InfraClusterAttachmentList{}
		if err := r.List(ctx, attList); err != nil {
			return
		}
		for _, att := range attList.Items {
			cpns := att.Spec.ControlPlaneNamespace
			if cpns == "" {
				hcRef := normalizeHostedClusterRef(att.Spec.HostedClusterRef)
				cpns = hcRef.Namespace + "-" + hcRef.Name
			}
			if cpns == vmi.Namespace {
				q.Add(reconcile.Request{
					NamespacedName: types.NamespacedName{Name: att.Spec.InfraRef.Name, Namespace: att.Namespace},
				})
			}
		}
	}
	vmiHandler := handler.Funcs{
		CreateFunc: func(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueueInfraForVMI(ctx, e.Object, q)
		},
		UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueueInfraForVMI(ctx, e.ObjectNew, q)
		},
		DeleteFunc: func(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueueInfraForVMI(ctx, e.Object, q)
		},
		GenericFunc: func(ctx context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueueInfraForVMI(ctx, e.Object, q)
		},
	}
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&hostedclusterv1alpha1.Infra{}).
		Owns(&hostedclusterv1alpha1.DHCPServer{}).
		Owns(&hostedclusterv1alpha1.DNSServer{}).
		Owns(&hostedclusterv1alpha1.ProxyServer{}).
		Watches(
			&hostedclusterv1alpha1.InfraClusterAttachment{},
			attachmentHandler,
		)

	// Source-IP alias discovery watches VirtualMachineInstances. Clusters
	// without the kubevirt.io CRDs must still run the operator; aliases are
	// simply not generated there, so skip the watch instead of failing start.
	if _, err := mgr.GetRESTMapper().RESTMapping(
		schema.GroupKind{Group: "kubevirt.io", Kind: "VirtualMachineInstance"}, "v1",
	); err != nil {
		logf.Log.Info("kubevirt.io/v1 VirtualMachineInstance not served; kubernetes.* source-IP aliases disabled", "cause", err.Error())
		return builder.Named("infra").Complete(r)
	}
	logf.Log.Info("watching kubevirt.io VirtualMachineInstances for source-IP alias discovery")
	return builder.
		Watches(
			&kubevirtv1.VirtualMachineInstance{},
			vmiHandler,
		).
		Named("infra").
		Complete(r)
}
