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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
)

var _ = Describe("Apps ingress", func() {
	ctx := context.Background()

	It("does not delete the default ingress service when the attachment never enabled apps ingress", func() {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oooi-ingress",
				Namespace: "openshift-ingress",
			},
		}
		hostedClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(service).Build()

		Expect(cleanupMetalLBInstallation(ctx, hostedClient, hostedclusterv1alpha1.AppsIngressConfig{})).To(Succeed())
		Expect(hostedClient.Get(ctx, types.NamespacedName{
			Name: "oooi-ingress", Namespace: "openshift-ingress",
		}, &corev1.Service{})).To(Succeed())
	})

	It("waits for a Ready hosted node before provisioning MetalLB", func() {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		hostedClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		status := hostedclusterv1alpha1.AppsIngressStatus{}
		target := appsIngressTarget{
			HostedClusterRef: hostedclusterv1alpha1.HostedClusterReference{
				Name:      "example-hcp",
				Namespace: "clusters",
			},
			Config: hostedclusterv1alpha1.AppsIngressConfig{
				Enabled: true,
				MetalLB: hostedclusterv1alpha1.AppsIngressMetalLB{
					AddressPoolName:    "apps-pool",
					IPAddressPoolRange: "192.0.2.10-192.0.2.20",
				},
			},
		}

		result := reconcileAppsIngressCore(ctx, func(context.Context) (client.Client, error) {
			return hostedClient, nil
		}, target, &status)

		Expect(result.RequeueAfter).ToNot(BeZero())
		Expect(status.Phase).To(Equal(PhasePending))
		Expect(status.Reason).To(Equal("WaitingForHostedClusterNodes"))
	})
})
