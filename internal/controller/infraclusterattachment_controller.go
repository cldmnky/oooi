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
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
)

const (
	// infraClusterAttachmentFinalizer gates hosted-cluster cleanup on deletion.
	infraClusterAttachmentFinalizer = "hostedcluster.densityops.com/apps-ingress-cleanup"

	reasonAttachmentInvalidConfig = "InvalidConfiguration"
	reasonAttachmentInfraNotFound = "InfraNotFound"
	defaultAPIServerServiceName   = "kube-apiserver"
)

// InfraClusterAttachmentReconciler reconciles an InfraClusterAttachment object.
type InfraClusterAttachmentReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// HostedClusterClientFactory creates a client for the attached hosted
	// cluster. Used for MetalLB installation and ingress service management.
	HostedClusterClientFactory func(ctx context.Context, att *hostedclusterv1alpha1.InfraClusterAttachment, apiServerService, controlPlaneNamespace string) (client.Client, error)
}

// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=infraclusterattachments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=infraclusterattachments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=infraclusterattachments/finalizers,verbs=update
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=infras,verbs=get;list;watch
// +kubebuilder:rbac:groups=hypershift.openshift.io,resources=hostedclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;create;delete

// Reconcile moves the current state of the cluster closer to the desired state
// for one InfraClusterAttachment.
func (r *InfraClusterAttachmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	att := &hostedclusterv1alpha1.InfraClusterAttachment{}
	if err := r.Get(ctx, req.NamespacedName, att); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !att.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, att)
	}

	if !controllerutil.ContainsFinalizer(att, infraClusterAttachmentFinalizer) {
		controllerutil.AddFinalizer(att, infraClusterAttachmentFinalizer)
		if err := r.Update(ctx, att); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Resolve the referenced shared Infra. It must exist before anything else
	// happens; without it there is no VLAN for this cluster.
	infra := &hostedclusterv1alpha1.Infra{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      att.Spec.InfraRef.Name,
		Namespace: att.Namespace,
	}, infra)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("Referenced Infra not found", "infra", att.Spec.InfraRef.Name)
			return r.setStatusAndFinish(ctx, att, metav1.ConditionFalse,
				reasonAttachmentInfraNotFound,
				"referenced Infra "+att.Spec.InfraRef.Name+" not found")
		}
		return ctrl.Result{}, err
	}

	// Resolve defaults for control-plane coordinates.
	target := appsIngressTarget{
		AttachmentName:   att.Name,
		HostedClusterRef: normalizeHostedClusterRef(att.Spec.HostedClusterRef),
		APIServerService: att.Spec.APIServerService,
		Config:           att.Spec.AppsIngress,
	}
	target.ControlPlaneNamespace = att.Spec.ControlPlaneNamespace
	if target.ControlPlaneNamespace == "" {
		target.ControlPlaneNamespace = target.HostedClusterRef.Namespace + "-" + target.HostedClusterRef.Name
	}
	target.Domain = att.Spec.DNS.ClusterName + "." + att.Spec.DNS.BaseDomain
	if target.APIServerService == "" {
		target.APIServerService = defaultAPIServerServiceName
	}

	if att.Spec.AppsIngress.Enabled {
		if att.Spec.AppsIngress.MetalLB.AddressPoolName == "" ||
			att.Spec.AppsIngress.MetalLB.IPAddressPoolRange == "" {
			return r.setAppsIngressStatusAndFinish(ctx, att, target,
				hostedclusterv1alpha1.AppsIngressStatus{
					Phase:   PhaseDegraded,
					Reason:  reasonAttachmentInvalidConfig,
					Message: "appsIngress requires metallb.addressPoolName and metallb.ipAddressPoolRange",
				})
		}
		result := reconcileAppsIngressCore(ctx, r.hostedFactory(att, target), target, &att.Status.AppsIngressStatus)
		if err := r.updateStatusCommon(ctx, att, target); err != nil {
			return ctrl.Result{}, err
		}
		return result, nil
	}
	att.Status.AppsIngressStatus = hostedclusterv1alpha1.AppsIngressStatus{}

	if err := r.ensureControlPlaneNetworkPolicy(ctx, target); err != nil {
		return ctrl.Result{}, err
	}

	return r.setStatusReady(ctx, att, target)
}

func (r *InfraClusterAttachmentReconciler) hostedFactory(att *hostedclusterv1alpha1.InfraClusterAttachment, target appsIngressTarget) hostedClusterFactory {
	return func(ctx context.Context) (client.Client, error) {
		if r.HostedClusterClientFactory != nil {
			return r.HostedClusterClientFactory(ctx, att, target.APIServerService, target.ControlPlaneNamespace)
		}
		return defaultHostedClusterClient(ctx, r.Client, target.HostedClusterRef, target.APIServerService, target.ControlPlaneNamespace)
	}
}

// ensureControlPlaneNetworkPolicy reconciles the allow-infrastructure policy
// in this attachment's control-plane namespace. The policy is cross-namespace
// relative to the attachment and therefore cannot carry an owner reference;
// its lifecycle is tracked through the finalizer instead.
func (r *InfraClusterAttachmentReconciler) ensureControlPlaneNetworkPolicy(ctx context.Context, target appsIngressTarget) error {
	if target.ControlPlaneNamespace == "" {
		return nil
	}
	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "allow-infrastructure",
			Namespace: target.ControlPlaneNamespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"hostedcluster.densityops.com/network-policy-group": "infrastructure",
						},
					},
				}},
			}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
		},
	}
	found := &networkingv1.NetworkPolicy{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(policy), found); err != nil {
		if errors.IsNotFound(err) {
			return r.Create(ctx, policy)
		}
		return err
	}
	found.Spec = policy.Spec
	return r.Update(ctx, found)
}

// reconcileDelete performs hosted-cluster cleanup, then removes the finalizer.
func (r *InfraClusterAttachmentReconciler) reconcileDelete(ctx context.Context, att *hostedclusterv1alpha1.InfraClusterAttachment) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	target := appsIngressTarget{
		AttachmentName:        att.Name,
		HostedClusterRef:      normalizeHostedClusterRef(att.Spec.HostedClusterRef),
		APIServerService:      att.Spec.APIServerService,
		ControlPlaneNamespace: att.Spec.ControlPlaneNamespace,
		Config:                att.Spec.AppsIngress,
	}
	if target.ControlPlaneNamespace == "" {
		target.ControlPlaneNamespace = target.HostedClusterRef.Namespace + "-" + target.HostedClusterRef.Name
	}
	if target.APIServerService == "" {
		target.APIServerService = defaultAPIServerServiceName
	}

	factory := r.hostedFactory(att, target)
	hostedClient, err := factory(ctx)
	if err != nil {
		// The hosted cluster may already be gone; treat access failure as done.
		if errors.IsNotFound(err) {
			log.Info("Hosted cluster already removed during cleanup", "ref", target.HostedClusterRef)
		} else {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
	} else if err := cleanupMetalLBInstallation(ctx, hostedClient, target.Config); err != nil {
		log.Error(err, "Failed hosted-cluster cleanup; requeueing")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "allow-infrastructure",
			Namespace: target.ControlPlaneNamespace,
		},
	}
	if err := r.Delete(ctx, policy); err != nil && !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	if controllerutil.ContainsFinalizer(att, infraClusterAttachmentFinalizer) {
		controllerutil.RemoveFinalizer(att, infraClusterAttachmentFinalizer)
		if err := r.Update(ctx, att); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *InfraClusterAttachmentReconciler) setAppsIngressStatusAndFinish(ctx context.Context, att *hostedclusterv1alpha1.InfraClusterAttachment, target appsIngressTarget, status hostedclusterv1alpha1.AppsIngressStatus) (ctrl.Result, error) {
	previous := att.Status.AppsIngressStatus
	status.LastSyncTime = previous.LastSyncTime
	if status.Phase != previous.Phase || status.Reason != previous.Reason || status.Message != previous.Message {
		status.LastSyncTime = metav1.Now()
	}
	att.Status.AppsIngressStatus = status
	if err := r.updateStatusCommon(ctx, att, target); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// setStatusReady records a successful reconciliation with no apps ingress.
func (r *InfraClusterAttachmentReconciler) setStatusReady(ctx context.Context, att *hostedclusterv1alpha1.InfraClusterAttachment, target appsIngressTarget) (ctrl.Result, error) {
	cond := metav1.Condition{
		Type:               phaseReady,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: att.Generation,
		Reason:             "ReconciliationSucceeded",
		Message:            "Attachment aggregated into shared infrastructure",
	}
	setAttachmentCondition(att, cond)
	if err := r.updateStatusCommon(ctx, att, target); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *InfraClusterAttachmentReconciler) setStatusAndFinish(ctx context.Context, att *hostedclusterv1alpha1.InfraClusterAttachment, status metav1.ConditionStatus, reason, message string) (ctrl.Result, error) {
	cond := metav1.Condition{
		Type:               phaseReady,
		Status:             status,
		ObservedGeneration: att.Generation,
		Reason:             reason,
		Message:            message,
	}
	setAttachmentCondition(att, cond)
	if err := r.Status().Update(ctx, att); err != nil && !errors.IsConflict(err) {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// updateStatusCommon refreshes derived fields, the Ready condition based on
// apps-ingress state, then persists status.
func (r *InfraClusterAttachmentReconciler) updateStatusCommon(ctx context.Context, att *hostedclusterv1alpha1.InfraClusterAttachment, target appsIngressTarget) error {
	att.Status.ObservedGeneration = att.Generation
	att.Status.Domain = target.Domain
	att.Status.ControlPlaneNamespace = target.ControlPlaneNamespace

	cond := metav1.Condition{
		Type:               phaseReady,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: att.Generation,
		Reason:             "ReconciliationSucceeded",
		Message:            "Attachment aggregated into shared infrastructure",
	}
	switch s := att.Status.AppsIngressStatus; {
	case !att.Spec.AppsIngress.Enabled:
		// Ready as-is.
	case s.Phase == PhasePending:
		cond.Status = metav1.ConditionFalse
		cond.Reason = s.Reason
		cond.Message = s.Message
		if cond.Reason == "" {
			cond.Reason = "AppsIngressPending"
		}
	case s.Phase == PhaseDegraded:
		cond.Status = metav1.ConditionFalse
		cond.Reason = s.Reason
		cond.Message = s.Message
	case s.Phase == phaseReady:
		cond.Message = "Apps ingress ready at endpoint " +
			endpointString(s.ExternalIP, s.ExternalHostname)
	default:
		cond.Status = metav1.ConditionFalse
		cond.Reason = "AppsIngressUnknown"
		cond.Message = "apps ingress phase " + s.Phase
	}
	setAttachmentCondition(att, cond)

	if err := r.Status().Update(ctx, att); err != nil {
		return client.IgnoreNotFound(err)
	}
	return nil
}

func setAttachmentCondition(att *hostedclusterv1alpha1.InfraClusterAttachment, cond metav1.Condition) {
	cond = preserveConditionTransitionTime(att.Status.Conditions, cond)
	meta.SetStatusCondition(&att.Status.Conditions, cond)
}

func endpointString(ip, hostname string) string {
	if ip != "" {
		return ip
	}
	return hostname
}

// SetupWithManager sets up the controller with the Manager.
func (r *InfraClusterAttachmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hostedclusterv1alpha1.InfraClusterAttachment{}).
		Named("infraclusterattachment").
		Complete(r)
}
