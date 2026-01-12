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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProxyServerSpec defines the desired state of ProxyServer
type ProxyServerSpec struct {
	// NetworkConfig defines the network parameters for the proxy server
	NetworkConfig ProxyNetworkConfig `json:"networkConfig"`

	// Backends defines the list of services to proxy with SNI-based routing
	// Each backend specifies an SNI hostname and the target Kubernetes service
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Backends []ProxyBackend `json:"backends"`

	// Image is the container image for the proxy (Envoy)
	// +optional
	// +kubebuilder:default="envoyproxy/envoy:v1.36.4"
	ProxyImage string `json:"proxyImage,omitempty"`

	// ManagerImage is the container image for the xDS control plane (oooi)
	// +optional
	// +kubebuilder:default="quay.io/cldmnky/oooi:latest"
	ManagerImage string `json:"managerImage,omitempty"`

	// Port is the listening port for the proxy on the secondary network
	// +optional
	// +kubebuilder:default=443
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`

	// XDSPort is the gRPC port for xDS communication between manager and Envoy
	// +optional
	// +kubebuilder:default=18000
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	XDSPort int32 `json:"xdsPort,omitempty"`

	// LogLevel for Envoy logging
	// +optional
	// +kubebuilder:default="info"
	// +kubebuilder:validation:Enum=trace;debug;info;warning;error;critical
	LogLevel string `json:"logLevel,omitempty"`
}

// ProxyNetworkConfig defines the network configuration for the proxy server
type ProxyNetworkConfig struct {
	// ServerIP is the static IP address assigned to the proxy server on the secondary network
	// Can be specified with or without CIDR notation (e.g., "192.168.1.4" or "192.168.1.4/24")
	// If CIDR is omitted, /24 will be assumed for static IPAM
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^(?:[0-9]{1,3}\.){3}[0-9]{1,3}(?:/[0-9]{1,2})?$`
	ServerIP string `json:"serverIP"`

	// NetworkAttachmentName is the name of the NetworkAttachmentDefinition to attach
	// +optional
	NetworkAttachmentName string `json:"networkAttachmentName,omitempty"`

	// NetworkAttachmentNamespace is the namespace of the NetworkAttachmentDefinition
	// +optional
	NetworkAttachmentNamespace string `json:"networkAttachmentNamespace,omitempty"`
}

// ProxyBackend defines a single proxied service with SNI-based routing
type ProxyBackend struct {
	// Name is a unique identifier for this backend (e.g., "kube-apiserver")
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// Hostname is the primary SNI hostname that clients will use to connect
	// Example: "api.my-cluster.example.com"
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Hostname string `json:"hostname"`

	// AlternateHostnames is a list of additional SNI hostnames that should route to this backend
	// This is useful for services that may be accessed via multiple hostnames (e.g., kubernetes service
	// can be accessed as "kubernetes", "kubernetes.default", "kubernetes.default.svc", etc.)
	// +optional
	AlternateHostnames []string `json:"alternateHostnames,omitempty"`

	// Port is the external port clients connect to
	// For HTTPS services, typically 443. For other services, use appropriate ports.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`

	// TargetService is the Kubernetes service name to forward traffic to
	// Example: "kube-apiserver"
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	TargetService string `json:"targetService"`

	// TargetPort is the port on the target service
	// Example: 6443 for kube-apiserver
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	TargetPort int32 `json:"targetPort"`

	// TargetNamespace is the namespace where the target service resides
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	TargetNamespace string `json:"targetNamespace"`

	// Protocol to use for the cluster (TCP is used for L4 proxying)
	// +optional
	// +kubebuilder:default="TCP"
	// +kubebuilder:validation:Enum=TCP;UDP
	Protocol string `json:"protocol,omitempty"`

	// TimeoutSeconds is the timeout for connections to the target service
	// +optional
	// +kubebuilder:default=30
	// +kubebuilder:validation:Minimum=1
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`
}

// ProxyServerStatus defines the observed state of ProxyServer
type ProxyServerStatus struct {
	// Conditions represents the latest available observations of the ProxyServer's state
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// ConfigMapName is the name of the ConfigMap containing the Envoy bootstrap configuration
	// +optional
	ConfigMapName string `json:"configMapName,omitempty"`

	// DeploymentName is the name of the Deployment running the proxy
	// +optional
	DeploymentName string `json:"deploymentName,omitempty"`

	// ServiceName is the name of the Service exposing the proxy
	// +optional
	ServiceName string `json:"serviceName,omitempty"`

	// ServiceIP is the ClusterIP of the proxy Service (for internal access)
	// +optional
	ServiceIP string `json:"serviceIP,omitempty"`

	// ObservedGeneration reflects the generation of the most recently observed ProxyServer
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// BackendCount is the number of successfully configured backends
	// +optional
	BackendCount int32 `json:"backendCount,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=proxy;proxies
// +kubebuilder:printcolumn:name="ServerIP",type=string,JSONPath=`.spec.networkConfig.serverIP`
// +kubebuilder:printcolumn:name="Port",type=integer,JSONPath=`.spec.port`
// +kubebuilder:printcolumn:name="Backends",type=integer,JSONPath=`.status.backendCount`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ProxyServer is the Schema for the proxyservers API
type ProxyServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProxyServerSpec   `json:"spec,omitempty"`
	Status ProxyServerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProxyServerList contains a list of ProxyServer
type ProxyServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProxyServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ProxyServer{}, &ProxyServerList{})
}
