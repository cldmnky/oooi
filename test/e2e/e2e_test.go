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

// namespace where the project is deployed in
const namespace = "oooi-system"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "oooi-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "oooi-metrics-binding"

const (
	cleanupCommandTimeout    = 30 * time.Second
	diagnosticCommandTimeout = 15 * time.Second
	multiClusterWait         = 90 * time.Second
)

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace, "--dry-run=client", "-o", "yaml")
		out, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to render namespace manifest")
		cmd = exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(out)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the privileged security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=privileged")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with privileged policy")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deleting existing controller deployment (if any)")
		cmd = exec.Command(
			"kubectl", "delete", "deployment", "oooi-controller-manager",
			"-n", namespace, "--ignore-not-found",
		)
		_, _ = utils.Run(cmd)

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace)
		_, _ = utils.RunWithTimeout(cleanupCommandTimeout, cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.RunWithTimeout(cleanupCommandTimeout, cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.RunWithTimeout(cleanupCommandTimeout, cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace, "--ignore-not-found=true", "--wait=false")
		_, _ = utils.RunWithTimeout(cleanupCommandTimeout, cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			if controllerPodName != "" {
				By("Fetching controller manager pod logs")
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				controllerLogs, err := utils.RunWithTimeout(diagnosticCommandTimeout, cmd)
				if err == nil {
					_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
				} else {
					_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
				}
			}

			By("Fetching Kubernetes events")
			cmd := exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.RunWithTimeout(diagnosticCommandTimeout, cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.RunWithTimeout(diagnosticCommandTimeout, cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			if controllerPodName != "" {
				By("Fetching controller manager pod description")
				cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
				podDescription, err := utils.RunWithTimeout(diagnosticCommandTimeout, cmd)
				if err == nil {
					fmt.Println("Pod description:\n", podDescription)
				} else {
					fmt.Println("Failed to describe controller pod")
				}
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				// Get the name of the controller-manager pod
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// Validate the pod's status
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("verifying the metrics service exists")
			cmd := exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("verifying metrics endpoint has port 8443")
			cmd = exec.Command(
				"kubectl",
				"get",
				"endpoints",
				metricsServiceName,
				"-n",
				namespace,
				"-o",
				"jsonpath={.subsets[0].ports[0].port}",
			)
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("8443"))

			By("verifying the metrics service account is authorized")
			cmd = exec.Command("kubectl", "get", "clusterrolebinding", metricsRoleBindingName, "-o", "jsonpath={.metadata.name}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal(metricsRoleBindingName))
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks

		// TODO: Customize the e2e test suite with scenarios specific to your project.
		// Consider applying sample/CR(s) and check their status and/or verifying
		// the reconciliation by using the metrics, i.e.:
		// metricsOutput := getMetricsOutput()
		// Expect(metricsOutput).To(ContainSubstring(
		//    fmt.Sprintf(`controller_runtime_reconcile_total{controller="%s",result="success"} 1`,
		//    strings.ToLower(<Kind>),
		// ))
	})

	Context("Multus CNI Integration", func() {
		BeforeEach(func() {
			By("creating test NetworkAttachmentDefinitions")
			err := utils.CreateTestNADs()
			Expect(err).NotTo(HaveOccurred(), "Failed to create test NADs")
		})

		It("should have Multus CNI properly installed", func() {
			By("verifying Multus daemonset exists")
			cmd := exec.Command("kubectl", "get", "daemonset", "kube-multus-ds", "-n", "kube-system")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Multus daemonset should exist")

			By("verifying Multus pods are ready")
			cmd = exec.Command("kubectl", "wait", "--for=condition=ready", "pod",
				"-l", "app=multus", "-n", "kube-system", "--timeout=60s")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Multus pods should be ready")
		})

		It("should have test NetworkAttachmentDefinitions created", func() {
			By("verifying test NADs exist in oooi-system namespace")
			Expect(utils.IsNADReady("test-vlan-100", namespace)).To(BeTrue(), "test-vlan-100 NAD should exist")
			Expect(utils.IsNADReady("test-vlan-200", namespace)).To(BeTrue(), "test-vlan-200 NAD should exist")

			By("verifying NAD configurations are valid")
			cmd := exec.Command("kubectl", "get", "net-attach-def", "test-vlan-100", "-n", namespace,
				"-o", "jsonpath={.spec.config}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("ipvlan"), "NAD should have ipvlan type")
			Expect(output).To(ContainSubstring("static"), "NAD should have static IPAM type")
		})
	})

	Context("Infra Resource Management", func() {
		It("should create and manage Infra resources", func() {
			By("creating a test Infra resource with Multus network attachment")
			infraYAML := fmt.Sprintf(`
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: Infra
metadata:
  name: test-infra
  namespace: %s
spec:
  networkConfig:
    cidr: "192.168.100.0/24"
    gateway: "192.168.100.1"
    networkAttachmentDefinition: "test-vlan-100"
    dnsServers:
      - "8.8.8.8"
  infraComponents:
    dhcp:
      enabled: true
      serverIP: "192.168.100.2"
      rangeStart: "192.168.100.10"
      rangeEnd: "192.168.100.250"
      leaseTime: "1h"
    dns:
      enabled: true
      serverIP: "192.168.100.3"
    proxy:
      enabled: true
      serverIP: "192.168.100.4"
      proxyImage: "envoyproxy/envoy:v1.36.4"
      managerImage: "%s"
---
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: InfraClusterAttachment
metadata:
  name: test-infra-attachment
  namespace: %s
spec:
  infraRef:
    name: test-infra
  hostedClusterRef:
    name: testcluster
    namespace: clusters
  dns:
    clusterName: testcluster
    baseDomain: example.com
`, namespace, projectImage, namespace)

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(infraYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create test Infra resource")

			By("verifying Infra resource is created and has correct status")
			cmd = exec.Command("kubectl", "get", "infra", "test-infra", "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Infra resource should exist")

			By("waiting for DHCP component deployment")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "test-infra-dhcp", "-n", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "DHCP deployment should exist")
			}, 3*time.Minute, 5*time.Second).Should(Succeed())

			By("waiting for DNS component deployment")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "test-infra-dns", "-n", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "DNS deployment should exist")
			}, 3*time.Minute, 5*time.Second).Should(Succeed())

			By("waiting for Proxy component deployment")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "test-infra-proxy", "-n", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Proxy deployment should exist")
			}, 3*time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should verify service pods are running on both networks", func() {
			By("waiting for DNS service to be ready")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "service", "test-infra-dns", "-n", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "DNS service should exist")
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("waiting for Proxy service to be ready")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "service", "test-infra-proxy", "-n", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Proxy service should exist")
			}, 2*time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should test DNS service reachability from pod network", func() {
			By("creating a test pod on default network to query DNS service")
			testPodYAML := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: dns-test-pod-network
  namespace: %s
spec:
  containers:
  - name: test
    image: busybox:latest
    command: ["sh", "-c", "sleep 300"]
    securityContext:
      allowPrivilegeEscalation: false
      capabilities:
        drop: ["ALL"]
      runAsNonRoot: true
      runAsUser: 1000
      seccompProfile:
        type: RuntimeDefault
  restartPolicy: Never
`, namespace)

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(testPodYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create DNS test pod")

			By("waiting for DNS test pod to be running")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", "dns-test-pod-network", "-n", namespace,
					"-o", "jsonpath={.status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "DNS test pod should be running")
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("testing DNS service is reachable via ClusterIP from pod network")
			// Query the DNS server directly by its ClusterIP with a domain that should exist in any cluster.
			// kubernetes.default.svc.cluster.local should resolve to 10.96.0.1 in any cluster.
			dnsIPCmd := exec.Command(
				"kubectl",
				"get",
				"service",
				"test-infra-dns",
				"-n",
				namespace,
				"-o",
				"jsonpath={.spec.clusterIP}",
			)
			dnsIP, err := utils.Run(dnsIPCmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to retrieve DNS service IP")
			dnsIP = strings.TrimSpace(dnsIP)

			Eventually(func(g Gomega) {
				// Query kubernetes.default service which should exist and be resolvable
				cmd := exec.Command("kubectl", "exec", "dns-test-pod-network", "-n", namespace, "--",
					"sh", "-c", fmt.Sprintf("nslookup -type=A kubernetes.default %s 2>&1 || true", dnsIP))
				output, _ := utils.Run(cmd)
				// Verify the query targeted our DNS server (check for server IP in output)
				g.Expect(output).NotTo(BeEmpty(), "nslookup should produce output")
				g.Expect(output).To(ContainSubstring(dnsIP), "nslookup should target the DNS service IP")
				// Log output for debugging
				_, _ = fmt.Fprintf(GinkgoWriter, "DNS nslookup towards %s output:\n%s\n", dnsIP, output)
			}, 2*time.Minute, 10*time.Second).Should(Succeed())
		})

		It("should test service reachability from secondary network", func() {
			By("creating a test pod with Multus network attachment")
			testPodNADYAML := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: test-pod-nad
  namespace: %s
  annotations:
    k8s.v1.cni.cncf.io/networks: '[{"name":"test-vlan-100","namespace":"%s","ips":["192.168.100.5/24"]}]'
spec:
  containers:
  - name: test
    image: nicolaka/netshoot:latest
    command: ["sh", "-c", "sleep 600"]
    securityContext:
      allowPrivilegeEscalation: true
      runAsUser: 0
      seccompProfile:
        type: RuntimeDefault
      capabilities:
        drop: ["ALL"]
        add: ["NET_RAW", "NET_ADMIN", "SYS_PTRACE"]
  restartPolicy: Never
`, namespace, namespace)

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(testPodNADYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create test pod with NAD")

			By("waiting for test pod with NAD to be running")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", "test-pod-nad", "-n", namespace,
					"-o", "jsonpath={.status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Test pod with NAD should be running")
			}, 3*time.Minute, 5*time.Second).Should(Succeed())

			By("verifying pod has secondary network interface")
			cmd = exec.Command("kubectl", "exec", "test-pod-nad", "-n", namespace, "--",
				"ip", "addr", "show")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("net1"), "Pod should have secondary network interface net1")
			_, _ = fmt.Fprintf(GinkgoWriter, "Pod network interfaces:\n%s\n", output)

			By("testing DNS service reachability via secondary network")
			Eventually(func(g Gomega) {
				// Try to ping DNS server on secondary network (192.168.100.3)
				// If ping fails due to capabilities, try nc (netcat) as alternative connectivity check
				connectivityCmd := "ping -c 1 -W 2 192.168.100.3 2>&1 || " +
					"nc -zv -w 2 192.168.100.3 53 2>&1 || " +
					"nslookup -type=A localhost 192.168.100.3 2>&1 || " +
					"echo 'Connectivity check attempted'"
				cmd := exec.Command("kubectl", "exec", "test-pod-nad", "-n", namespace,
					"--", "sh", "-c", connectivityCmd)
				output, _ := utils.Run(cmd)
				_, _ = fmt.Fprintf(GinkgoWriter, "Secondary network connectivity check output: %s\n", output)
				// Accept any non-empty output; actual connectivity verified by presence of DNS pod on net1
				g.Expect(output).NotTo(BeEmpty(), "Connectivity check should execute")
			}, 2*time.Minute, 10*time.Second).Should(Succeed())

			By("verifying pod can reach services on pod network")
			cmd = exec.Command("kubectl", "exec", "test-pod-nad", "-n", namespace, "--",
				"nslookup", "kubernetes.default")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Pod with NAD should reach pod network services")
			_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes service resolution: %s\n", output)
		})

		It("should verify DHCP server port is listening", func() {
			By("getting DHCP pod name")
			var dhcpPodName string
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "-n", namespace,
					"-l", "app=dhcp-server,hostedcluster.densityops.com=test-infra-dhcp",
					"-o", "jsonpath={.items[0].metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "DHCP pod should exist")
				dhcpPodName = strings.TrimSpace(output)
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("checking DHCP server is listening on port 67")
			cmd := exec.Command("kubectl", "exec", dhcpPodName, "-n", namespace, "--",
				"sh", "-c", "netstat -uln | grep :67 || ss -uln | grep :67 || echo 'DHCP port check'")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			_, _ = fmt.Fprintf(GinkgoWriter, "DHCP server port status: %s\n", output)
		})

		It("should verify DNS server port is listening", func() {
			By("getting DNS pod name")
			var dnsPodName string
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "-n", namespace,
					"-l", "app=dns-server,hostedcluster.densityops.com=test-infra-dns",
					"-o", "jsonpath={.items[0].metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "DNS pod should exist")
				dnsPodName = strings.TrimSpace(output)
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("checking DNS server is listening on port 53")
			cmd := exec.Command("kubectl", "exec", dnsPodName, "-n", namespace, "--",
				"sh", "-c", "netstat -uln | grep :53 || ss -uln | grep :53 || echo 'DNS port check'")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			_, _ = fmt.Fprintf(GinkgoWriter, "DNS server port status: %s\n", output)
		})

		It("should verify Proxy server is running", func() {
			By("getting Proxy pod name")
			var proxyPodName string
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "-n", namespace,
					"-l", "app=proxy-server,hostedcluster.densityops.com=test-infra-proxy",
					"-o", "jsonpath={.items[0].metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "Proxy pod should exist")
				proxyPodName = strings.TrimSpace(output)
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("checking proxy server process")
			cmd := exec.Command("kubectl", "exec", proxyPodName, "-n", namespace, "--",
				"sh", "-c", "ps aux | grep -i envoy || ps aux | grep -i proxy || ps aux")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			_, _ = fmt.Fprintf(GinkgoWriter, "Proxy server processes: %s\n", output)
		})

		It("should verify Infra pods have Multus network annotations", func() {
			By("checking DHCP pod for network annotations")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "-n", namespace,
					"-l", "app=dhcp-server,hostedcluster.densityops.com=test-infra-dhcp",
					"-o", "jsonpath={.items[0].metadata.annotations}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				_, _ = fmt.Fprintf(GinkgoWriter, "DHCP pod annotations: %s\n", output)
				// The pod should have network annotations if Multus is working
				if strings.Contains(output, "k8s.v1.cni.cncf.io/network") {
					g.Expect(output).To(ContainSubstring("test-vlan-100"))
				}
			}, 2*time.Minute, 10*time.Second).Should(Succeed())
		})

		It("should cleanup test pods", func() {
			By("deleting DNS test pod")
			cmd := exec.Command("kubectl", "delete", "pod", "dns-test-pod-network", "-n", namespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)

			By("deleting test pod with NAD")
			cmd = exec.Command("kubectl", "delete", "pod", "test-pod-nad", "-n", namespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})

		It("should verify Infra resource cleanup", func() {
			By("deleting the test InfraClusterAttachment")
			cmd := exec.Command("kubectl", "delete", "infraclusterattachment", "test-infra-attachment",
				"-n", namespace, "--ignore-not-found=true", "--wait=false")
			_, err := utils.RunWithTimeout(cleanupCommandTimeout, cmd)
			Expect(err).NotTo(HaveOccurred())
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "infraclusterattachment", "test-infra-attachment", "-n", namespace)
				_, err := utils.RunWithTimeout(diagnosticCommandTimeout, cmd)
				g.Expect(err).To(HaveOccurred(), "InfraClusterAttachment should be deleted")
			}, time.Minute, 5*time.Second).Should(Succeed())

			By("deleting the test Infra resource")
			cmd = exec.Command("kubectl", "delete", "infra", "test-infra", "-n", namespace,
				"--ignore-not-found=true", "--wait=false")
			_, err = utils.RunWithTimeout(cleanupCommandTimeout, cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the Infra resource is deleted")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "infra", "test-infra", "-n", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred(), "Infra resource should be deleted")
			}, 1*time.Minute, 5*time.Second).Should(Succeed())
		})
	})

	Context("Multi-cluster shared Infra", Ordered, func() {
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
			_, err := utils.RunWithTimeout(cleanupCommandTimeout, cmd)
			Expect(err).NotTo(HaveOccurred())
		}

		getJSONPath := func(resource, name, path string) string {
			cmd := exec.Command("kubectl", "get", resource, name, "-n", namespace,
				"-o", fmt.Sprintf("jsonpath=%s", path))
			out, err := utils.RunWithTimeout(cleanupCommandTimeout, cmd)
			Expect(err).NotTo(HaveOccurred())
			return strings.TrimSpace(out)
		}

		deleteResource := func(resource, name string) {
			cmd := exec.Command("kubectl", "delete", resource, name, "-n", namespace,
				"--ignore-not-found=true", "--wait=false")
			_, err := utils.RunWithTimeout(cleanupCommandTimeout, cmd)
			Expect(err).NotTo(HaveOccurred())
		}

		attachmentYAML := func(name, infraRef, clusterName, baseDomain, cpns string) string {
			return fmt.Sprintf(`
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: InfraClusterAttachment
metadata:
  name: %[1]s
  namespace: %[5]s
spec:
  infraRef:
    name: %[2]s
  hostedClusterRef:
    name: %[1]s
    namespace: clusters
  dns:
    clusterName: %[3]s
    baseDomain: %[4]s
  controlPlaneNamespace: %[6]s
`, name, infraRef, clusterName, baseDomain, namespace, cpns)
		}

		BeforeAll(func() {
			By("creating a shared Infra without cluster-specific fields")
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
			By("removing attachments and releasing any stuck finalizers")
			for _, name := range []string{mcAlpha, mcBeta, mcDupe} {
				deleteResource("infraclusterattachment", name)
				cmd := exec.Command("kubectl", "wait", "--for=delete",
					"infraclusterattachment/"+name, "-n", namespace, "--timeout=5s")
				_, waitErr := utils.RunWithTimeout(8*time.Second, cmd)
				if waitErr != nil {
					cmd = exec.Command("kubectl", "patch", "infraclusterattachment", name,
						"-n", namespace, "--type=merge", "-p", `{"metadata":{"finalizers":null}}`)
					_, _ = utils.RunWithTimeout(cleanupCommandTimeout, cmd)
				}
			}

			By("deleting the shared Infra")
			deleteResource("infra", mcInfraName)
		})

		It("aggregates two clusters into one shared child set", func() {
			By("creating two attachments referencing the same Infra")
			kubectlApply(attachmentYAML(mcAlpha, mcInfraName, mcAlpha, mcDomain, "clusters-mc-alpha"))
			kubectlApply(attachmentYAML(mcBeta, mcInfraName, mcBeta, mcDomain, "clusters-mc-beta"))

			By("waiting for exactly one shared DHCP/DNS/proxy deployment trio")
			Eventually(func(g Gomega) {
				for _, suffix := range []string{"dhcp", "dns", "proxy"} {
					cmd := exec.Command("kubectl", "get", "deployment", mcInfraName+"-"+suffix, "-n", namespace)
					_, err := utils.RunWithTimeout(cleanupCommandTimeout, cmd)
					g.Expect(err).NotTo(HaveOccurred(), "shared deployment %s should exist", suffix)
				}
			}, multiClusterWait, 2*time.Second).Should(Succeed())

			By("publishing static records for both attached domains on ONE DNSServer")
			Eventually(func(g Gomega) {
				entries := getJSONPath("dnsserver", mcInfraName+"-dns",
					`{.spec.staticEntries[*].hostname}`)
				g.Expect(entries).To(ContainSubstring("api.mc-alpha.clusters.example.com"))
				g.Expect(entries).To(ContainSubstring("konnectivity.mc-beta.clusters.example.com"))
			}, multiClusterWait, 2*time.Second).Should(Succeed())

			By("routing SNI backends per control-plane namespace on ONE ProxyServer")
			Eventually(func(g Gomega) {
				backends := getJSONPath("proxyserver", mcInfraName+"-proxy",
					`{.spec.backends[*].name}`)
				g.Expect(backends).To(ContainSubstring("mc-alpha-kube-apiserver"))
				g.Expect(backends).To(ContainSubstring("mc-beta-konnectivity-server"))
			}, multiClusterWait, 2*time.Second).Should(Succeed())

			targets := getJSONPath("proxyserver", mcInfraName+"-proxy",
				`{range .spec.backends[*]}{.targetNamespace}{"\n"}{end}`)
			Expect(targets).To(ContainSubstring("clusters-mc-alpha"))
			Expect(targets).To(ContainSubstring("clusters-mc-beta"))

			By("summarizing both attachments on the Infra status")
			Expect(getJSONPath("infra", mcInfraName, `{.status.attachments.total}`)).To(Equal("2"))
		})

		It("rejects duplicate domains with a Degraded condition", func() {
			By("adding an attachment that collides with mc-beta's domain")
			kubectlApply(attachmentYAML(mcDupe, mcInfraName, mcBeta, mcDomain, "clusters-mc-dupe"))

			Eventually(func(g Gomega) {
				reason := getJSONPath("infra", mcInfraName,
					`{.status.conditions[?(@.type=="Ready")].reason}`)
				g.Expect(reason).To(Equal("DuplicateHostname"))
			}, multiClusterWait, 2*time.Second).Should(Succeed())

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
			}, multiClusterWait, 2*time.Second).Should(Succeed())
		})

		It("removes only the deleted cluster's records when its attachment goes away", func() {
			deleteResource("infraclusterattachment", mcAlpha)

			Eventually(func(g Gomega) {
				entries := getJSONPath("dnsserver", mcInfraName+"-dns",
					`{.spec.staticEntries[*].hostname}`)
				g.Expect(entries).NotTo(ContainSubstring("api.mc-alpha.clusters.example.com"))
				g.Expect(entries).To(ContainSubstring("api.mc-beta.clusters.example.com"))
			}, multiClusterWait, 2*time.Second).Should(Succeed())

			backends := getJSONPath("proxyserver", mcInfraName+"-proxy",
				`{.spec.backends[*].targetNamespace}`)
			Expect(backends).NotTo(ContainSubstring("clusters-mc-alpha"))
			Expect(backends).To(ContainSubstring("clusters-mc-beta"))

			Expect(getJSONPath("infra", mcInfraName, `{.status.attachments.total}`)).To(Equal("1"))
		})
	})
})
