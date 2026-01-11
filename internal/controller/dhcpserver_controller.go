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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
)

// DHCPServerReconciler reconciles a DHCPServer object
type DHCPServerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=dhcpservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=dhcpservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=dhcpservers/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *DHCPServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the DHCPServer instance
	dhcpServer := &hostedclusterv1alpha1.DHCPServer{}
	if err := r.Get(ctx, req.NamespacedName, dhcpServer); err != nil {
		log.Error(err, "unable to fetch DHCPServer")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Ensure DHCP deployment and all its resources
	if err := r.ensureDHCPDeployment(ctx, dhcpServer); err != nil {
		log.Error(err, "unable to ensure DHCP deployment")
		return ctrl.Result{}, err
	}

	// Update status
	dhcpServer.Status.ObservedGeneration = dhcpServer.Generation
	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: dhcpServer.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             "ReconciliationSucceeded",
		Message:            "DHCP server resources created successfully",
	}
	dhcpServer.Status.Conditions = []metav1.Condition{condition}

	if err := r.Status().Update(ctx, dhcpServer); err != nil {
		log.Error(err, "Failed to update DHCPServer status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// ensureDHCPDeployment ensures that a DHCP server deployment and all required resources exist
func (r *DHCPServerReconciler) ensureDHCPDeployment(ctx context.Context, dhcpServer *hostedclusterv1alpha1.DHCPServer) error {
	log := logf.FromContext(ctx)

	// Ensure ConfigMap
	configMap := r.newDHCPConfigMap(dhcpServer)
	if err := ctrl.SetControllerReference(dhcpServer, configMap, r.Scheme); err != nil {
		log.Error(err, "unable to set owner reference on ConfigMap")
		return err
	}
	if err := r.createOrUpdateWithRetries(ctx, configMap, func() error {
		desiredConfigMap := r.newDHCPConfigMap(dhcpServer)
		configMap.Data = desiredConfigMap.Data
		configMap.Labels = desiredConfigMap.Labels
		return ctrl.SetControllerReference(dhcpServer, configMap, r.Scheme)
	}); err != nil {
		log.Error(err, "unable to ensure ConfigMap")
		return err
	}

	// Ensure PVC
	pvc := r.newDHCPPVC(dhcpServer)
	if err := ctrl.SetControllerReference(dhcpServer, pvc, r.Scheme); err != nil {
		log.Error(err, "unable to set owner reference on PVC")
		return err
	}
	if err := r.createOrUpdateWithRetries(ctx, pvc, func() error {
		return ctrl.SetControllerReference(dhcpServer, pvc, r.Scheme)
	}); err != nil {
		log.Error(err, "unable to ensure PVC")
		return err
	}

	// Ensure ServiceAccount
	sa := r.newDHCPServiceAccount(dhcpServer)
	if err := ctrl.SetControllerReference(dhcpServer, sa, r.Scheme); err != nil {
		log.Error(err, "unable to set owner reference on ServiceAccount")
		return err
	}
	if err := r.createOrUpdateWithRetries(ctx, sa, func() error {
		return ctrl.SetControllerReference(dhcpServer, sa, r.Scheme)
	}); err != nil {
		log.Error(err, "unable to ensure ServiceAccount")
		return err
	}

	// Ensure Deployment
	deployment := r.newDHCPDeployment(dhcpServer)
	if err := ctrl.SetControllerReference(dhcpServer, deployment, r.Scheme); err != nil {
		log.Error(err, "unable to set owner reference on DHCP deployment")
		return err
	}

	if err := r.createOrUpdateWithRetries(ctx, deployment, func() error {
		return ctrl.SetControllerReference(dhcpServer, deployment, r.Scheme)
	}); err != nil {
		log.Error(err, "unable to ensure DHCP deployment")
		return err
	}

	return nil
}

// newDHCPConfigMap returns a ConfigMap object for the DHCP configuration
func (r *DHCPServerReconciler) newDHCPConfigMap(dhcpServer *hostedclusterv1alpha1.DHCPServer) *corev1.ConfigMap {
	// Get DNS server (use first one)
	dns := "8.8.8.8"
	if len(dhcpServer.Spec.NetworkConfig.DNSServers) > 0 {
		dns = dhcpServer.Spec.NetworkConfig.DNSServers[0]
	}

	// Format lease time (default to 60s if not specified)
	leaseTime := dhcpServer.Spec.LeaseConfig.LeaseTime
	if leaseTime == "" {
		leaseTime = "60s"
	}

	// Calculate subnet mask from CIDR (simplified - using /24 as default)
	subnetMask := "255.255.255.0"

	// Use server4 format with plugins that matches working manual setup
	config := fmt.Sprintf(`# hyperdhcp configuration
server4:
    listen:
    - "%%net1"
    plugins:
        - kubevirt:
        - server_id: %s
        - dns: %s
        - router: %s
        - netmask: %s
        - range: /var/lib/dhcp/leases.txt %s %s %s
`,
		dhcpServer.Spec.NetworkConfig.ServerIP,
		dns,
		dhcpServer.Spec.NetworkConfig.Gateway,
		subnetMask,
		dhcpServer.Spec.LeaseConfig.RangeStart,
		dhcpServer.Spec.LeaseConfig.RangeEnd,
		leaseTime)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dhcpServer.Name + "-dhcp-config",
			Namespace: dhcpServer.Namespace,
			Labels: map[string]string{
				"app": dhcpServer.Name,
			},
		},
		Data: map[string]string{
			"hyperdhcp.yaml": config,
		},
	}
}

// newDHCPPVC returns a PersistentVolumeClaim object for DHCP lease storage
func (r *DHCPServerReconciler) newDHCPPVC(dhcpServer *hostedclusterv1alpha1.DHCPServer) *corev1.PersistentVolumeClaim {
	storageClassName := "standard"
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dhcpServer.Name + "-dhcp-leases",
			Namespace: dhcpServer.Namespace,
			Labels: map[string]string{
				"app": dhcpServer.Name,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClassName,
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("25Mi"),
				},
			},
		},
	}
}

// newDHCPServiceAccount returns a ServiceAccount object for the DHCP server
func (r *DHCPServerReconciler) newDHCPServiceAccount(dhcpServer *hostedclusterv1alpha1.DHCPServer) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dhcpServer.Name + "-dhcp",
			Namespace: dhcpServer.Namespace,
			Labels: map[string]string{
				"app": dhcpServer.Name,
			},
		},
	}
}

// newDHCPDeployment returns a Deployment object for the DHCP server
func (r *DHCPServerReconciler) newDHCPDeployment(dhcpServer *hostedclusterv1alpha1.DHCPServer) *appsv1.Deployment {
	labels := map[string]string{
		"app":                          "dhcp-server",
		"hostedcluster.densityops.com": dhcpServer.Name,
	}

	replicas := int32(1)
	runAsNonRoot := false

	// Build network attachment annotation
	// Format: [{"name": "<nad-name>", "namespace": "<nad-namespace>", "ips": ["<ip>/<prefix>"]}]
	networkAnnotation := fmt.Sprintf(`[
  {
    "name": "%s",
    "namespace": "%s",
    "ips": ["%s"]
  }
]`,
		dhcpServer.Spec.NetworkConfig.NetworkAttachmentName,
		dhcpServer.Spec.NetworkConfig.NetworkAttachmentNamespace,
		dhcpServer.Spec.NetworkConfig.ServerIP+"/"+getNetmaskBits(dhcpServer.Spec.NetworkConfig.CIDR))

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dhcpServer.Name,
			Namespace: dhcpServer.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						"k8s.v1.cni.cncf.io/networks": networkAnnotation,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: dhcpServer.Name + "-dhcp",
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &runAsNonRoot,
					},
					Containers: []corev1.Container{
						{
							Name:  "dhcp-server",
							Image: dhcpServer.Spec.Image,
							Args: []string{
								"dhcp",
								"--config-file",
								"/etc/dhcp/hyperdhcp.yaml",
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "dhcp",
									ContainerPort: 67,
									Protocol:      corev1.ProtocolUDP,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "dhcp-config",
									MountPath: "/etc/dhcp",
									ReadOnly:  true,
								},
								{
									Name:      "dhcp-leases",
									MountPath: "/var/lib/dhcp",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "dhcp-config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: dhcpServer.Name + "-dhcp-config",
									},
									Items: []corev1.KeyToPath{
										{
											Key:  "hyperdhcp.yaml",
											Path: "hyperdhcp.yaml",
										},
									},
								},
							},
						},
						{
							Name: "dhcp-leases",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: dhcpServer.Name + "-dhcp-leases",
								},
							},
						},
					},
				},
			},
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *DHCPServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hostedclusterv1alpha1.DHCPServer{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Named("dhcpserver").
		Complete(r)
}

// getNetmaskBits extracts the netmask bits from a CIDR string
// Example: "192.168.100.0/24" -> "24"
func getNetmaskBits(cidr string) string {
	parts := strings.Split(cidr, "/")
	if len(parts) == 2 {
		return parts[1]
	}
	return "24" // default to /24
}
