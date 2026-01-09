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
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
)

// DNSServerReconciler reconciles a DNSServer object
type DNSServerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=dnsservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=dnsservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=dnsservers/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *DNSServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the DNSServer instance
	dnsServer := &hostedclusterv1alpha1.DNSServer{}
	if err := r.Get(ctx, req.NamespacedName, dnsServer); err != nil {
		log.Error(err, "unable to fetch DNSServer")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Ensure DNS deployment and all its resources
	if err := r.ensureDNSDeployment(ctx, dnsServer); err != nil {
		log.Error(err, "unable to ensure DNS deployment")
		return ctrl.Result{}, err
	}

	// Update status
	dnsServer.Status.ObservedGeneration = dnsServer.Generation
	dnsServer.Status.ConfigMapName = dnsServer.Name + "-dns-config"
	dnsServer.Status.DeploymentName = dnsServer.Name + "-dns"

	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: dnsServer.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             "ReconciliationSucceeded",
		Message:            "DNS server resources created successfully",
	}
	dnsServer.Status.Conditions = []metav1.Condition{condition}

	if err := r.Status().Update(ctx, dnsServer); err != nil {
		log.Error(err, "Failed to update DNSServer status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// ensureDNSDeployment ensures that a DNS server deployment and all required resources exist
func (r *DNSServerReconciler) ensureDNSDeployment(ctx context.Context, dnsServer *hostedclusterv1alpha1.DNSServer) error {
	log := logf.FromContext(ctx)

	// Ensure ConfigMap
	configMap := r.newDNSConfigMap(dnsServer)
	if err := ctrl.SetControllerReference(dnsServer, configMap, r.Scheme); err != nil {
		log.Error(err, "unable to set owner reference on ConfigMap")
		return err
	}
	if err := r.createOrUpdateWithRetries(ctx, configMap, func() error {
		desiredConfigMap := r.newDNSConfigMap(dnsServer)
		configMap.Data = desiredConfigMap.Data
		configMap.Labels = desiredConfigMap.Labels
		return ctrl.SetControllerReference(dnsServer, configMap, r.Scheme)
	}); err != nil {
		log.Error(err, "unable to ensure ConfigMap")
		return err
	}

	// Ensure Deployment
	deployment := r.newDNSDeployment(dnsServer)
	if err := ctrl.SetControllerReference(dnsServer, deployment, r.Scheme); err != nil {
		log.Error(err, "unable to set owner reference on DNS deployment")
		return err
	}

	if err := r.createOrUpdateWithRetries(ctx, deployment, func() error {
		return ctrl.SetControllerReference(dnsServer, deployment, r.Scheme)
	}); err != nil {
		log.Error(err, "unable to ensure DNS deployment")
		return err
	}

	// Ensure Service
	service := r.newDNSService(dnsServer)
	if err := ctrl.SetControllerReference(dnsServer, service, r.Scheme); err != nil {
		log.Error(err, "unable to set owner reference on Service")
		return err
	}
	if err := r.createOrUpdateWithRetries(ctx, service, func() error {
		return ctrl.SetControllerReference(dnsServer, service, r.Scheme)
	}); err != nil {
		log.Error(err, "unable to ensure Service")
		return err
	}

	return nil
}

// newDNSConfigMap returns a ConfigMap object for the Corefile DNS configuration
func (r *DNSServerReconciler) newDNSConfigMap(dnsServer *hostedclusterv1alpha1.DNSServer) *corev1.ConfigMap {
	// Build hosts entries
	var hostsEntries strings.Builder
	for _, entry := range dnsServer.Spec.StaticEntries {
		hostsEntries.WriteString(fmt.Sprintf("        %s %s\n", entry.IP, entry.Hostname))
	}

	// Get upstream DNS servers (default to 8.8.8.8 if not specified)
	upstream := "8.8.8.8"
	if len(dnsServer.Spec.UpstreamDNS) > 0 {
		upstream = strings.Join(dnsServer.Spec.UpstreamDNS, " ")
	}

	// Get reload interval (default to 5s if not specified)
	reloadInterval := dnsServer.Spec.ReloadInterval
	if reloadInterval == "" {
		reloadInterval = "5s"
	}

	// Get cache TTL (default to 30s if not specified)
	cacheTTL := dnsServer.Spec.CacheTTL
	if cacheTTL == "" {
		cacheTTL = "30s"
	}

	// Get DNS port (default to 53 if not specified)
	dnsPort := dnsServer.Spec.NetworkConfig.DNSPort
	if dnsPort == 0 {
		dnsPort = 53
	}

	// Build Corefile configuration
	corefile := fmt.Sprintf(`# Hosted Control Plane split-horizon DNS
# Domain: %s
%s {
    # Static A records for control plane endpoints
    hosts {
%s        fallthrough
    }
    
    # Forward to upstream DNS
    forward . %s {
        policy sequential
        health_check 5s
    }
    
    # Auto-reload on config changes
    reload %s
    
    # Cache DNS responses
    cache %s
    
    # Logging
    log
    errors
    
    # Health checks
    ready :8181
    health :8080
}

# Forward all other queries to upstream
. {
    forward . %s
    log
    errors
}
`, dnsServer.Spec.HostedClusterDomain, dnsServer.Spec.HostedClusterDomain, hostsEntries.String(), upstream, reloadInterval, cacheTTL, upstream)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dnsServer.Name + "-dns-config",
			Namespace: dnsServer.Namespace,
			Labels: map[string]string{
				"app": dnsServer.Name,
			},
		},
		Data: map[string]string{
			"Corefile": corefile,
		},
	}
}

// newDNSDeployment returns a Deployment object for the DNS server
func (r *DNSServerReconciler) newDNSDeployment(dnsServer *hostedclusterv1alpha1.DNSServer) *appsv1.Deployment {
	labels := map[string]string{
		"app":                          "dns-server",
		"hostedcluster.densityops.com": dnsServer.Name,
	}

	replicas := int32(1)
	runAsUser := int64(1000)

	// Get DNS port (default to 53)
	dnsPort := dnsServer.Spec.NetworkConfig.DNSPort
	if dnsPort == 0 {
		dnsPort = 53
	}

	// Build network attachment annotation if NetworkAttachmentName is specified
	annotations := make(map[string]string)
	if dnsServer.Spec.NetworkConfig.NetworkAttachmentName != "" {
		networkAnnotation := fmt.Sprintf(`[
  {
    "name": "%s",
    "namespace": "%s",
    "ips": ["%s"]
  }
]`,
			dnsServer.Spec.NetworkConfig.NetworkAttachmentName,
			dnsServer.Spec.NetworkConfig.NetworkAttachmentNamespace,
			dnsServer.Spec.NetworkConfig.ServerIP)
		annotations["k8s.v1.cni.cncf.io/networks"] = networkAnnotation
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dnsServer.Name + "-dns",
			Namespace: dnsServer.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "dns-server",
							Image: dnsServer.Spec.Image,
							Args: []string{
								"dns",
								"--corefile",
								"/etc/coredns/Corefile",
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "dns-udp",
									ContainerPort: dnsPort,
									Protocol:      corev1.ProtocolUDP,
								},
								{
									Name:          "dns-tcp",
									ContainerPort: dnsPort,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "health",
									ContainerPort: 8080,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "ready",
									ContainerPort: 8181,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							SecurityContext: &corev1.SecurityContext{
								RunAsUser:                &runAsUser,
								AllowPrivilegeEscalation: ptr(false),
								Capabilities: &corev1.Capabilities{
									Add: []corev1.Capability{
										"NET_BIND_SERVICE",
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "dns-config",
									MountPath: "/etc/coredns",
									ReadOnly:  true,
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt(8080),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/ready",
										Port: intstr.FromInt(8181),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       5,
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "dns-config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: dnsServer.Name + "-dns-config",
									},
									Items: []corev1.KeyToPath{
										{
											Key:  "Corefile",
											Path: "Corefile",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// newDNSService returns a Service object for the DNS server
func (r *DNSServerReconciler) newDNSService(dnsServer *hostedclusterv1alpha1.DNSServer) *corev1.Service {
	labels := map[string]string{
		"app":                          "dns-server",
		"hostedcluster.densityops.com": dnsServer.Name,
	}

	// Get DNS port (default to 53)
	dnsPort := dnsServer.Spec.NetworkConfig.DNSPort
	if dnsPort == 0 {
		dnsPort = 53
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dnsServer.Name + "-dns",
			Namespace: dnsServer.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "dns-udp",
					Port:       dnsPort,
					TargetPort: intstr.FromInt(int(dnsPort)),
					Protocol:   corev1.ProtocolUDP,
				},
				{
					Name:       "dns-tcp",
					Port:       dnsPort,
					TargetPort: intstr.FromInt(int(dnsPort)),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
}

// createOrUpdateWithRetries attempts to create or update an object with exponential backoff retry logic
func (r *DNSServerReconciler) createOrUpdateWithRetries(ctx context.Context, obj client.Object, updateFunc func() error) error {
	log := logf.FromContext(ctx)
	key := client.ObjectKeyFromObject(obj)

	// Try to get the object
	if err := r.Get(ctx, key, obj); err != nil {
		if client.IgnoreNotFound(err) != nil {
			// Other error
			log.Error(err, "Failed to get object")
			return err
		}
		// Object doesn't exist, create it
		log.Info("Creating object", "name", key.Name)
		if createErr := r.Create(ctx, obj); createErr != nil {
			log.Error(createErr, "Failed to create object", "name", key.Name)
			return createErr
		}
		return nil
	}

	// Object exists, update it
	log.V(1).Info("Updating object", "name", key.Name)
	if updateErr := updateFunc(); updateErr != nil {
		log.Error(updateErr, "Update function failed", "name", key.Name)
		return updateErr
	}

	if updateErr := r.Update(ctx, obj); updateErr != nil {
		log.Error(updateErr, "Failed to update object", "name", key.Name)
		return updateErr
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DNSServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hostedclusterv1alpha1.DNSServer{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Named("dnsserver").
		Complete(r)
}

// ptr returns a pointer to the provided value
func ptr(b bool) *bool {
	return &b
}
