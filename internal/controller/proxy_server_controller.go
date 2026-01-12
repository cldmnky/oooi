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
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
)

const defaultManagerImage = "quay.io/cldmnky/oooi:latest"

// ProxyServerReconciler reconciles a ProxyServer object
type ProxyServerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// newProxyServiceAccount creates a ServiceAccount for the proxy pods
func (r *ProxyServerReconciler) newProxyServiceAccount(proxyServer *hostedclusterv1alpha1.ProxyServer) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      proxyServer.Name + "-proxy",
			Namespace: proxyServer.Namespace,
			Labels: map[string]string{
				"app": "proxy-server",
			},
		},
	}
}

// newProxyRole creates a Role with permissions to list/watch ProxyServer resources
func (r *ProxyServerReconciler) newProxyRole(proxyServer *hostedclusterv1alpha1.ProxyServer) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      proxyServer.Name + "-proxy",
			Namespace: proxyServer.Namespace,
			Labels: map[string]string{
				"app": "proxy-server",
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"hostedcluster.densityops.com"},
				Resources: []string{"proxyservers"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}
}

// newProxyRoleBinding creates a RoleBinding linking the ServiceAccount to the Role
func (r *ProxyServerReconciler) newProxyRoleBinding(proxyServer *hostedclusterv1alpha1.ProxyServer) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      proxyServer.Name + "-proxy",
			Namespace: proxyServer.Namespace,
			Labels: map[string]string{
				"app": "proxy-server",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     proxyServer.Name + "-proxy",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      proxyServer.Name + "-proxy",
				Namespace: proxyServer.Namespace,
			},
		},
	}
}

// boolPtr returns a pointer to a bool value
func boolPtr(b bool) *bool {
	return &b
}

// newSCCRoleBinding returns a RoleBinding that grants the privileged SCC to the service account
// Matches the pattern used by DHCP and DNS servers
func (r *ProxyServerReconciler) newSCCRoleBinding(proxyServer *hostedclusterv1alpha1.ProxyServer, serviceAccountName string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      proxyServer.Name + "-privileged-scc",
			Namespace: proxyServer.Namespace,
			Labels: map[string]string{
				"app": proxyServer.Name,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "system:openshift:scc:privileged",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccountName,
				Namespace: proxyServer.Namespace,
			},
		},
	}
}

// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=proxyservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=proxyservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=proxyservers/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ProxyServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the ProxyServer instance
	proxyServer := &hostedclusterv1alpha1.ProxyServer{}
	if err := r.Get(ctx, req.NamespacedName, proxyServer); err != nil {
		log.Error(err, "unable to fetch ProxyServer")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Ensure proxy deployment and all its resources
	if err := r.ensureProxyDeployment(ctx, proxyServer); err != nil {
		log.Error(err, "unable to ensure proxy deployment")
		return ctrl.Result{}, err
	}

	// Get the Service to retrieve its ClusterIP for status
	serviceName := proxyServer.Name
	foundService := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: proxyServer.Namespace}, foundService); err != nil {
		log.Error(err, "unable to fetch proxy Service for status update")
		return ctrl.Result{}, err
	}

	// Update status
	proxyServer.Status.ObservedGeneration = proxyServer.Generation
	proxyServer.Status.ConfigMapName = proxyServer.Name + "-proxy-bootstrap"
	proxyServer.Status.DeploymentName = proxyServer.Name
	proxyServer.Status.ServiceName = serviceName
	proxyServer.Status.ServiceIP = foundService.Spec.ClusterIP
	proxyServer.Status.BackendCount = int32(len(proxyServer.Spec.Backends))

	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: proxyServer.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             "ReconciliationSucceeded",
		Message:            fmt.Sprintf("Proxy deployment ready with %d backends", len(proxyServer.Spec.Backends)),
	}
	proxyServer.Status.Conditions = []metav1.Condition{condition}

	if err := r.Status().Update(ctx, proxyServer); err != nil {
		log.Error(err, "Failed to update ProxyServer status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// ensureProxyDeployment ensures that a proxy deployment and all required resources exist
func (r *ProxyServerReconciler) ensureProxyDeployment(ctx context.Context, proxyServer *hostedclusterv1alpha1.ProxyServer) error {
	log := logf.FromContext(ctx)

	// Ensure ServiceAccount
	serviceAccount := r.newProxyServiceAccount(proxyServer)
	if err := ctrl.SetControllerReference(proxyServer, serviceAccount, r.Scheme); err != nil {
		log.Error(err, "unable to set owner reference on ServiceAccount")
		return err
	}
	if err := r.createOrUpdateWithRetries(ctx, serviceAccount, func() error {
		return ctrl.SetControllerReference(proxyServer, serviceAccount, r.Scheme)
	}); err != nil {
		log.Error(err, "unable to ensure ServiceAccount")
		return err
	}

	// Ensure Role with ProxyServer permissions
	role := r.newProxyRole(proxyServer)
	if err := ctrl.SetControllerReference(proxyServer, role, r.Scheme); err != nil {
		log.Error(err, "unable to set owner reference on Role")
		return err
	}
	if err := r.createOrUpdateWithRetries(ctx, role, func() error {
		desiredRole := r.newProxyRole(proxyServer)
		role.Rules = desiredRole.Rules
		return ctrl.SetControllerReference(proxyServer, role, r.Scheme)
	}); err != nil {
		log.Error(err, "unable to ensure Role")
		return err
	}

	// Ensure RoleBinding
	roleBinding := r.newProxyRoleBinding(proxyServer)
	if err := ctrl.SetControllerReference(proxyServer, roleBinding, r.Scheme); err != nil {
		log.Error(err, "unable to set owner reference on RoleBinding")
		return err
	}
	if err := r.createOrUpdateWithRetries(ctx, roleBinding, func() error {
		desiredRoleBinding := r.newProxyRoleBinding(proxyServer)
		roleBinding.RoleRef = desiredRoleBinding.RoleRef
		roleBinding.Subjects = desiredRoleBinding.Subjects
		return ctrl.SetControllerReference(proxyServer, roleBinding, r.Scheme)
	}); err != nil {
		log.Error(err, "unable to ensure RoleBinding")
		return err
	}

	// Ensure OpenShift SCC RoleBinding for privileged ports
	sccRoleBinding := r.newSCCRoleBinding(proxyServer, serviceAccount.Name)
	if err := ctrl.SetControllerReference(proxyServer, sccRoleBinding, r.Scheme); err != nil {
		log.Error(err, "unable to set owner reference on SCC RoleBinding")
		return err
	}
	if err := r.createOrUpdateWithRetries(ctx, sccRoleBinding, func() error {
		desiredRB := r.newSCCRoleBinding(proxyServer, serviceAccount.Name)
		sccRoleBinding.RoleRef = desiredRB.RoleRef
		sccRoleBinding.Subjects = desiredRB.Subjects
		sccRoleBinding.Labels = desiredRB.Labels
		return ctrl.SetControllerReference(proxyServer, sccRoleBinding, r.Scheme)
	}); err != nil {
		log.Error(err, "unable to ensure SCC RoleBinding")
		return err
	}
	log.Info("Ensured OpenShift SCC RoleBinding", "serviceAccount", serviceAccount.Name)

	// Ensure ConfigMap with Envoy bootstrap config
	configMap := r.newEnvoyBootstrapConfigMap(proxyServer)
	if err := ctrl.SetControllerReference(proxyServer, configMap, r.Scheme); err != nil {
		log.Error(err, "unable to set owner reference on ConfigMap")
		return err
	}
	if err := r.createOrUpdateWithRetries(ctx, configMap, func() error {
		desiredConfigMap := r.newEnvoyBootstrapConfigMap(proxyServer)
		configMap.Data = desiredConfigMap.Data
		configMap.Labels = desiredConfigMap.Labels
		return ctrl.SetControllerReference(proxyServer, configMap, r.Scheme)
	}); err != nil {
		log.Error(err, "unable to ensure ConfigMap")
		return err
	}

	// Ensure Deployment
	deployment := r.newProxyDeployment(proxyServer)
	if err := ctrl.SetControllerReference(proxyServer, deployment, r.Scheme); err != nil {
		log.Error(err, "unable to set owner reference on proxy deployment")
		return err
	}

	if err := r.createOrUpdateWithRetries(ctx, deployment, func() error {
		return ctrl.SetControllerReference(proxyServer, deployment, r.Scheme)
	}); err != nil {
		log.Error(err, "unable to ensure proxy deployment")
		return err
	}

	// Ensure Service
	service := r.newProxyService(proxyServer)
	if err := ctrl.SetControllerReference(proxyServer, service, r.Scheme); err != nil {
		log.Error(err, "unable to set owner reference on Service")
		return err
	}
	if err := r.createOrUpdateWithRetries(ctx, service, func() error {
		return ctrl.SetControllerReference(proxyServer, service, r.Scheme)
	}); err != nil {
		log.Error(err, "unable to ensure Service")
		return err
	}

	return nil
}

// newEnvoyBootstrapConfigMap creates a ConfigMap with the Envoy bootstrap configuration
func (r *ProxyServerReconciler) newEnvoyBootstrapConfigMap(proxyServer *hostedclusterv1alpha1.ProxyServer) *corev1.ConfigMap {
	xdsPort := proxyServer.Spec.XDSPort
	if xdsPort == 0 {
		xdsPort = 18000
	}

	// Envoy bootstrap configuration pointing to xDS server on localhost
	bootstrapConfig := fmt.Sprintf(`{
  "node": {
    "id": "%s",
    "cluster": "%s"
  },
  "dynamic_resources": {
    "ads_config": {
      "api_type": "GRPC",
      "transport_api_version": "V3",
      "grpc_services": [
        {
          "envoy_grpc": {
            "cluster_name": "xds_cluster"
          }
        }
      ]
    },
    "cds_config": {
      "resource_api_version": "V3",
      "ads": {}
    },
    "lds_config": {
      "resource_api_version": "V3",
      "ads": {}
    }
  },
  "static_resources": {
    "clusters": [
      {
        "name": "xds_cluster",
        "connect_timeout": "5s",
        "type": "STATIC",
        "typed_extension_protocol_options": {
          "envoy.extensions.upstreams.http.v3.HttpProtocolOptions": {
            "@type": "type.googleapis.com/envoy.extensions.upstreams.http.v3.HttpProtocolOptions",
            "explicit_http_config": {
              "http2_protocol_options": {}
            }
          }
        },
        "load_assignment": {
          "cluster_name": "xds_cluster",
          "endpoints": [
            {
              "lb_endpoints": [
                {
                  "endpoint": {
                    "address": {
                      "socket_address": {
                        "address": "127.0.0.1",
                        "port_value": %d
                      }
                    }
                  }
                }
              ]
            }
          ]
        }
      }
    ]
  },
  "admin": {
    "address": {
      "socket_address": {
        "address": "0.0.0.0",
        "port_value": 9901
      }
    }
  }
}`, proxyServer.Name, proxyServer.Name, xdsPort)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      proxyServer.Name + "-proxy-bootstrap",
			Namespace: proxyServer.Namespace,
			Labels: map[string]string{
				"app": proxyServer.Name,
			},
		},
		Data: map[string]string{
			"bootstrap.json": bootstrapConfig,
		},
	}
}

// newProxyDeployment creates a Deployment with Envoy sidecar and oooi proxy manager
func (r *ProxyServerReconciler) newProxyDeployment(proxyServer *hostedclusterv1alpha1.ProxyServer) *appsv1.Deployment {
	runAsNonRoot := false
	runAsUser := int64(0)

	labels := map[string]string{
		"app":                          "proxy-server",
		"hostedcluster.densityops.com": proxyServer.Name,
	}

	replicas := int32(1)

	proxyImage := proxyServer.Spec.ProxyImage
	if proxyImage == "" {
		proxyImage = "envoyproxy/envoy:v1.36.4"
	}

	managerImage := proxyServer.Spec.ManagerImage
	if managerImage == "" {
		managerImage = defaultManagerImage
	}

	xdsPort := proxyServer.Spec.XDSPort
	if xdsPort == 0 {
		xdsPort = 18000
	}

	port := proxyServer.Spec.Port
	if port == 0 {
		port = 443
	}

	logLevel := proxyServer.Spec.LogLevel
	if logLevel == "" {
		logLevel = "info"
	}

	nadName := proxyServer.Spec.NetworkConfig.NetworkAttachmentName
	nadNamespace := proxyServer.Spec.NetworkConfig.NetworkAttachmentNamespace
	if nadNamespace == "" {
		nadNamespace = proxyServer.Namespace
	}

	// Build network attachment annotation with static IP
	// Format: [{"name": "<nad-name>", "namespace": "<nad-namespace>", "ips": ["<ip>/<prefix>"]}]
	networkAnnotation := fmt.Sprintf(`[
  {
    "name": "%s",
    "namespace": "%s",
    "ips": ["%s"]
  }
]`,
		nadName,
		nadNamespace,
		ensureIPWithCIDR(proxyServer.Spec.NetworkConfig.ServerIP))

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      proxyServer.Name,
			Namespace: proxyServer.Namespace,
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
					ServiceAccountName: proxyServer.Name + "-proxy",
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &runAsNonRoot,
						RunAsUser:    &runAsUser,
					},
					Containers: []corev1.Container{
						{
							Name:  "envoy",
							Image: proxyImage,
							Ports: []corev1.ContainerPort{
								{
									Name:          "proxy",
									ContainerPort: port,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "admin",
									ContainerPort: 9901,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: boolPtr(true),
								Capabilities: &corev1.Capabilities{
									Add: []corev1.Capability{
										"NET_BIND_SERVICE",
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "bootstrap-config",
									MountPath: "/etc/envoy",
									ReadOnly:  true,
								},
								{
									Name:      "envoy-logs",
									MountPath: "/tmp",
								},
							},
							Command: []string{"/usr/local/bin/envoy"},
							Args: []string{
								"-c", "/etc/envoy/bootstrap.json",
								"-l", logLevel,
								"--log-path", "/tmp/envoy.log",
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    *resource.NewMilliQuantity(100, resource.DecimalSI),
									corev1.ResourceMemory: *resource.NewQuantity(256*1024*1024, resource.BinarySI),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    *resource.NewMilliQuantity(500, resource.DecimalSI),
									corev1.ResourceMemory: *resource.NewQuantity(512*1024*1024, resource.BinarySI),
								},
							},
						},
						{
							Name:  "manager",
							Image: managerImage,
							Args: []string{
								"proxy",
								"--xds-port", fmt.Sprintf("%d", xdsPort),
								"--namespace", proxyServer.Namespace,
								"--proxy-name", proxyServer.Name,
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "xds",
									ContainerPort: xdsPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    *resource.NewMilliQuantity(50, resource.DecimalSI),
									corev1.ResourceMemory: *resource.NewQuantity(128*1024*1024, resource.BinarySI),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    *resource.NewMilliQuantity(200, resource.DecimalSI),
									corev1.ResourceMemory: *resource.NewQuantity(256*1024*1024, resource.BinarySI),
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "bootstrap-config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: proxyServer.Name + "-proxy-bootstrap",
									},
								},
							},
						},
						{
							Name: "envoy-logs",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}
}

// newProxyService creates a Service for the proxy
func (r *ProxyServerReconciler) newProxyService(proxyServer *hostedclusterv1alpha1.ProxyServer) *corev1.Service {
	labels := map[string]string{
		"app": "proxy-server",
	}

	port := proxyServer.Spec.Port
	if port == 0 {
		port = 443
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      proxyServer.Name,
			Namespace: proxyServer.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"app": "proxy-server",
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "proxy",
					Port:       port,
					TargetPort: intstr.FromInt(int(port)),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "admin",
					Port:       9901,
					TargetPort: intstr.FromInt(9901),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// createOrUpdateWithRetries attempts to create or update an object with exponential backoff retry logic
func (r *ProxyServerReconciler) createOrUpdateWithRetries(ctx context.Context, obj client.Object, updateFunc func() error) error {
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

// ensureIPWithCIDR ensures an IP address has CIDR notation
// If the IP already has CIDR notation (contains '/'), returns as-is
// Otherwise, appends /24 as default
func ensureIPWithCIDR(ip string) string {
	if strings.Contains(ip, "/") {
		return ip
	}
	return ip + "/24"
}
func (r *ProxyServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hostedclusterv1alpha1.ProxyServer{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Named("proxyserver").
		Complete(r)
}
