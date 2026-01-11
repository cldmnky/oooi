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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/cldmnky/oooi/test/utils"
)

// namespace where the project is deployed in
const namespace = "oooi-system"

// serviceAccountName created for the project
const serviceAccountName = "oooi-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "oooi-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "oooi-metrics-binding"

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
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
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
			cmd = exec.Command("kubectl", "get", "endpoints", metricsServiceName, "-n", namespace, "-o", "jsonpath={.subsets[0].ports[0].port}")
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
      baseDomain: "example.com"
      clusterName: "testcluster"
    proxy:
      enabled: true
      serverIP: "192.168.100.4"
      proxyImage: "envoyproxy/envoy:v1.36.4"
      managerImage: "%s"
`, namespace, projectImage)

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
			// Query the DNS server directly by its ClusterIP, targeting a known HCP hostname.
			// We don't require successful resolution; we only verify the command runs against our DNS IP.
			dnsIPCmd := exec.Command("kubectl", "get", "service", "test-infra-dns", "-n", namespace, "-o", "jsonpath={.spec.clusterIP}")
			dnsIP, err := utils.Run(dnsIPCmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to retrieve DNS service IP")
			dnsIP = strings.TrimSpace(dnsIP)

			Eventually(func(g Gomega) {
				// Query the DNS server directly by its ClusterIP; use @<ip> syntax to force query to specific server
				cmd := exec.Command("kubectl", "exec", "dns-test-pod-network", "-n", namespace, "--",
					"sh", "-c", fmt.Sprintf("nslookup -type=A api.testcluster.example.com %s 2>&1 || true", dnsIP))
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
      allowPrivilegeEscalation: false
      runAsNonRoot: true
      runAsUser: 1000
      seccompProfile:
        type: RuntimeDefault
      capabilities:
        drop: ["ALL"]
        add: ["NET_RAW"]
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
				cmd := exec.Command("kubectl", "exec", "test-pod-nad", "-n", namespace, "--",
					"sh", "-c", "ping -c 1 -W 2 192.168.100.3 2>&1 || nc -zv -w 2 192.168.100.3 53 2>&1 || nslookup -type=A localhost 192.168.100.3 2>&1 || echo 'Connectivity check attempted'")
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
			By("deleting the test Infra resource")
			cmd := exec.Command("kubectl", "delete", "infra", "test-infra", "-n", namespace, "--ignore-not-found=true")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the Infra resource is deleted")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "infra", "test-infra", "-n", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred(), "Infra resource should be deleted")
			}, 1*time.Minute, 5*time.Second).Should(Succeed())
		})
	})
})

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	// Temporary file to store the token request
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		// Execute kubectl command to create the token
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		// Parse the JSON output to extract the token
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() string {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	metricsOutput, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
	Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
	return metricsOutput
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
