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

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// InfraClusterAttachmentSpec defines the desired state of InfraClusterAttachment.
type InfraClusterAttachmentSpec struct {
	// InfraRef references the shared Infra resource that owns the VLAN
	// infrastructure (DHCP, DNS, and proxy). The InfraClusterAttachment must
	// be created in the same namespace as the referenced Infra.
	// +kubebuilder:validation:Required
	InfraRef InfraReference `json:"infraRef"`

	// HostedClusterRef references the HyperShift HostedCluster attached to
	// the shared infrastructure. Each HostedCluster may be referenced by at
	// most one InfraClusterAttachment.
	// +kubebuilder:validation:Required
	HostedClusterRef HostedClusterReference `json:"hostedClusterRef"`

	// DNS defines the hosted cluster DNS naming used to generate static
	// records on the shared DNSServer and SNI routes on the shared ProxyServer.
	// +kubebuilder:validation:Required
	DNS AttachmentDNSConfig `json:"dns"`

	// ControlPlaneNamespace is the management-cluster namespace hosting this
	// cluster's control-plane Services. When empty, oooi derives it from the
	// HostedCluster reference as "<namespace>-<name>".
	// +optional
	ControlPlaneNamespace string `json:"controlPlaneNamespace,omitempty"`

	// APIServerService is the name of the hosted API server Service in the
	// control plane namespace. Defaults to "kube-apiserver".
	// +optional
	APIServerService string `json:"apiServerService,omitempty"`

	// AppsIngress configures optional per-cluster *.apps ingress automation.
	// MetalLB ranges must not overlap between attachments sharing a VLAN.
	// +optional
	AppsIngress AppsIngressConfig `json:"appsIngress,omitempty"`
}

// InfraReference identifies an Infra resource in the same namespace.
type InfraReference struct {
	// Name is the name of the referenced Infra resource.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// AttachmentDNSConfig defines the hosted cluster DNS naming for an attachment.
type AttachmentDNSConfig struct {
	// ClusterName is the hosted cluster name used to construct FQDNs such as
	// "api.<clusterName>.<baseDomain>".
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	ClusterName string `json:"clusterName"`

	// BaseDomain is the DNS base domain of the hosted cluster, for example
	// "clusters.example.com".
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)+$`
	BaseDomain string `json:"baseDomain"`
}

// InfraClusterAttachmentStatus defines the observed state of InfraClusterAttachment.
type InfraClusterAttachmentStatus struct {
	// Conditions represents the latest available observations of the
	// InfraClusterAttachment's state.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// ObservedGeneration reflects the generation of the most recently
	// observed InfraClusterAttachment.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Domain is the fully qualified hosted cluster domain derived from
	// spec.dns ("<clusterName>.<baseDomain>").
	// +optional
	Domain string `json:"domain,omitempty"`

	// ControlPlaneNamespace is the resolved management-cluster namespace
	// hosting this cluster's control-plane Services.
	// +optional
	ControlPlaneNamespace string `json:"controlPlaneNamespace,omitempty"`

	// AppsIngressStatus tracks the per-cluster apps ingress state in the
	// hosted cluster.
	// +optional
	AppsIngressStatus AppsIngressStatus `json:"appsIngressStatus,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=infraattachment;ica
// +kubebuilder:printcolumn:name="Infra",type="string",JSONPath=".spec.infraRef.name",description="Referenced Infra"
// +kubebuilder:printcolumn:name="HostedCluster",type="string",JSONPath=".spec.hostedClusterRef.name",description="Attached HostedCluster"
// +kubebuilder:printcolumn:name="Domain",type="string",JSONPath=".status.domain",description="Hosted cluster domain"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status",description="Ready status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// InfraClusterAttachment attaches one HyperShift HostedCluster to the shared
// VLAN infrastructure owned by an Infra resource. Multiple attachments may
// reference the same Infra; each contributes its own DNS records, Envoy SNI
// backends, and optional apps-ingress automation.
type InfraClusterAttachment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InfraClusterAttachmentSpec   `json:"spec,omitempty"`
	Status InfraClusterAttachmentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// InfraClusterAttachmentList contains a list of InfraClusterAttachment.
type InfraClusterAttachmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InfraClusterAttachment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InfraClusterAttachment{}, &InfraClusterAttachmentList{})
}
