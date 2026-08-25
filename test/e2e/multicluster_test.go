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

package e2e

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/cldmnky/oooi/test/utils"
)

// The multi-cluster specs verify that one Infra can serve several
// InfraClusterAttachments: a single DHCP/DNS/proxy child set whose DNS
// entries and SNI backends cover every attached cluster domain. The hosted
// clusters themselves are out of scope here (the suite's Kind cluster does
// not run HyperShift); aggregation is driven purely by attachment objects.
var _ = Describe("Multi-cluster shared Infra", Ordered, func() {
	const (
		mcInfraName = "mc-infra"

		mcDomain = "clusters.example.com"

		mcAlpha = "mc-alpha"
		mcBeta  = "mc-beta"
		mcDupe  = "mc-dupe"
	)

	kubectlApply := func(manifest string) {
		cmd := exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(manifest)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
	}

	getJSONPath := func(resource, name, path string) string {
		cmd := exec.Command("kubectl", "get", resource, name, "-n", namespace,
			"-o", fmt.Sprintf("jsonpath=%s", path))
		out, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		return strings.TrimSpace(out)
	}

	deleteResource := func(resource, name string) {
		cmd := exec.Command("kubectl", "delete", resource, name, "-n", namespace,
			"--ignore-not-found=true", "--wait=false")
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
	}

	attachmentYAML := func(name, infraRef, clusterName, baseDomain, cpns string) string {
		return fmt.Sprintf(`
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: InfraClusterAttachment
metadata:
  name: %s
  namespace: %s
spec:
  infraRef:
    name: %s
  hostedClusterRef:
    name: %s
    namespace: clusters
  dns:
    clusterName: %s
    baseDomain: %s
  controlPlaneNamespace: %s
`, name, namespace, infraRef, name, clusterName, baseDomain, cpns)
	}

	BeforeAll(func() {
		By("creating a shared Infra without legacy cluster fields")
		kubectlApply(fmt.Sprintf(`
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: Infra
metadata:
  name: %s
  namespace: %s
spec:
  networkConfig:
    cidr: "192.168.101.0/24"
    gateway: "192.168.101.1"
    networkAttachmentDefinition: "test-vlan-200"
    dnsServers:
      - "8.8.8.8"
  infraComponents:
    dhcp:
      enabled: true
      serverIP: "192.168.101.2"
      rangeStart: "192.168.101.10"
      rangeEnd: "192.168.101.250"
      leaseTime: "1h"
    dns:
      enabled: true
      serverIP: "192.168.101.3"
    proxy:
      enabled: true
      serverIP: "192.168.101.4"
      proxyImage: "envoyproxy/envoy:v1.36.4"
`, mcInfraName, namespace))
	})

	AfterAll(func() {
		By("removing attachments before the shared Infra")
		for _, name := range []string{mcAlpha, mcBeta, mcDupe} {
			deleteResource("infraclusterattachment", name)
		}
		deleteResource("infra", mcInfraName)
		for _, suffix := range []string{"dhcp", "dns", "proxy"} {
			deleteResource("deployment", mcInfraName+"-"+suffix)
		}
	})

	It("aggregates two clusters into one shared child set", func() {
		By("creating two attachments referencing the same Infra")
		kubectlApply(attachmentYAML(mcAlpha, mcInfraName, mcAlpha, mcDomain, "clusters-mc-alpha"))
		kubectlApply(attachmentYAML(mcBeta, mcInfraName, mcBeta, mcDomain, "clusters-mc-beta"))

		By("waiting for exactly one DHCPServer/DNSServer/ProxyServer trio")
		Eventually(func(g Gomega) {
			for _, suffix := range []string{"dhcp", "dns", "proxy"} {
				cmd := exec.Command("kubectl", "get", "deployment", mcInfraName+"-"+suffix, "-n", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "shared deployment %s should exist", suffix)
			}
		}, 3*time.Minute, 5*time.Second).Should(Succeed())

		By("publishing static records for both attached domains on ONE DNSServer")
		Eventually(func(g Gomega) {
			entries := getJSONPath("dnsserver", mcInfraName+"-dns",
				`{.spec.staticEntries[*].hostname}`)
			g.Expect(entries).To(ContainSubstring("api.mc-alpha.clusters.example.com"))
			g.Expect(entries).To(ContainSubstring("konnectivity.mc-beta.clusters.example.com"))
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		By("routing SNI backends per control-plane namespace on ONE ProxyServer")
		Eventually(func(g Gomega) {
			backends := getJSONPath("proxyserver", mcInfraName+"-proxy",
				`{.spec.backends[*].name}`)
			g.Expect(backends).To(ContainSubstring("mc-alpha-kube-apiserver"))
			g.Expect(backends).To(ContainSubstring("mc-beta-konnectivity-server"))
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		targets := getJSONPath("proxyserver", mcInfraName+"-proxy",
			`{range .spec.backends[*]}{.targetNamespace}{"\n"}{end}`)
		Expect(targets).To(ContainSubstring("clusters-mc-alpha"))
		Expect(targets).To(ContainSubstring("clusters-mc-beta"))

		By("summarizing both attachments on the Infra status")
		total := getJSONPath("infra", mcInfraName, `{.status.attachments.total}`)
		Expect(total).To(Equal("2"))
	})

	It("rejects duplicate domains with a Degraded condition", func() {
		By("adding an attachment that collides with mc-beta's domain")
		kubectlApply(attachmentYAML(mcDupe, mcInfraName, mcBeta, mcDomain, "clusters-mc-dupe"))

		Eventually(func(g Gomega) {
			reason := getJSONPath("infra", mcInfraName,
				`{.status.conditions[?(@.type=="Ready")].reason}`)
			g.Expect(reason).To(Equal("DuplicateHostname"))
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		message := getJSONPath("infra", mcInfraName,
			`{.status.conditions[?(@.type=="Ready")].message}`)
		Expect(message).To(ContainSubstring(mcBeta))
		Expect(message).To(ContainSubstring(mcDupe))

		By("removing the colliding attachment restores Ready")
		deleteResource("infraclusterattachment", mcDupe)
		Eventually(func(g Gomega) {
			status := getJSONPath("infra", mcInfraName,
				`{.status.conditions[?(@.type=="Ready")].status}`)
			g.Expect(status).To(Equal("True"))
		}, 2*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("removes only the deleted cluster's records when its attachment goes away", func() {
		deleteResource("infraclusterattachment", mcAlpha)

		Eventually(func(g Gomega) {
			entries := getJSONPath("dnsserver", mcInfraName+"-dns",
				`{.spec.staticEntries[*].hostname}`)
			g.Expect(entries).NotTo(ContainSubstring("api.mc-alpha.clusters.example.com"))
			g.Expect(entries).To(ContainSubstring("api.mc-beta.clusters.example.com"))
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		backends := getJSONPath("proxyserver", mcInfraName+"-proxy",
			`{.spec.backends[*].targetNamespace}`)
		Expect(backends).NotTo(ContainSubstring("clusters-mc-alpha"))
		Expect(backends).To(ContainSubstring("clusters-mc-beta"))

		total := getJSONPath("infra", mcInfraName, `{.status.attachments.total}`)
		Expect(total).To(Equal("1"))
	})
})
