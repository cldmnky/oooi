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
	"os"
	"os/exec"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/cldmnky/oooi/test/utils"
)

var (
	// Optional Environment Variables:
	// - CERT_MANAGER_INSTALL_SKIP=true: Skips CertManager installation during test setup.
	// These variables are useful if CertManager is already installed, avoiding
	// re-installation and conflicts.
	skipCertManagerInstall = os.Getenv("CERT_MANAGER_INSTALL_SKIP") == "true"
	// isCertManagerAlreadyInstalled will be set true when CertManager CRDs be found on the cluster
	isCertManagerAlreadyInstalled = false

	// projectImage is the name of the image which will be built and loaded
	// with the code source changes to be tested. Allow override via env var IMG.
	projectImage = func() string {
		if v := os.Getenv("IMG"); v != "" {
			return v
		}
		return "quay.io/cldmnky/oooi:latest"
	}()
)

// TestE2E runs the end-to-end (e2e) test suite for the project. These tests execute in an isolated,
// temporary environment to validate project changes with the purposed to be used in CI jobs.
// The default setup requires Kind, builds/loads the Manager Docker image locally, and installs
// CertManager.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting oooi integration test suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	By("verifying Multus CNI is installed")
	if !utils.IsMultusInstalled() {
		_, _ = fmt.Fprintf(GinkgoWriter, "Installing Multus CNI...\n")
		Expect(utils.InstallMultus()).To(Succeed(), "Failed to install Multus CNI")
	} else {
		_, _ = fmt.Fprintf(GinkgoWriter, "Multus CNI is already installed\n")
	}

	By("creating test NetworkAttachmentDefinitions")
	Expect(utils.CreateTestNADs()).To(Succeed(), "Failed to create test NADs")

	By("verifying test NADs are ready")
	Eventually(func(g Gomega) {
		g.Expect(utils.IsNADReady("test-vlan-100", namespace)).To(BeTrue())
		g.Expect(utils.IsNADReady("test-vlan-200", namespace)).To(BeTrue())
	}, 2*time.Minute, time.Second).Should(Succeed())

	By("building the manager(Operator) image")
	cmd := exec.Command("make", "container-build-e2e")
	_, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the manager(Operator) image")

	// When using Kind with KIND_CLUSTER env var set, ko automatically loads the image
	// into the cluster. The LoadImageToKindClusterWithName step is skipped in this case.
	if os.Getenv("KIND_CLUSTER") == "" {
		// TODO(user): If you want to change the e2e test vendor from Kind, ensure the image is
		// built and available before running the tests. Also, remove the following block.
		By("loading the manager(Operator) image on Kind")
		err = utils.LoadImageToKindClusterWithName(projectImage)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the manager(Operator) image into Kind")
	}

	// The tests-e2e are intended to run on a temporary cluster that is created and destroyed for testing.
	// To prevent errors when tests run in environments with CertManager already installed,
	// we check for its presence before execution.
	// Setup CertManager before the suite if not skipped and if not already installed
	if !skipCertManagerInstall {
		By("checking if cert manager is installed already")
		isCertManagerAlreadyInstalled = utils.IsCertManagerCRDsInstalled()
		if !isCertManagerAlreadyInstalled {
			_, _ = fmt.Fprintf(GinkgoWriter, "Installing CertManager...\n")
			Expect(utils.InstallCertManager()).To(Succeed(), "Failed to install CertManager")
		} else {
			_, _ = fmt.Fprintf(GinkgoWriter, "WARNING: CertManager is already installed. Skipping installation...\n")
		}
	}
})

var _ = AfterSuite(func() {
	// Teardown CertManager after the suite if not skipped and if it was not already installed
	if !skipCertManagerInstall && !isCertManagerAlreadyInstalled {
		_, _ = fmt.Fprintf(GinkgoWriter, "Uninstalling CertManager...\n")
		utils.UninstallCertManager()
	}
})
