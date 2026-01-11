#!/bin/bash
# Quick Start Guide - E2E Testing with Multus and KubeVirt

cat << 'EOF'

╔═══════════════════════════════════════════════════════════════════════════════╗
║                                                                               ║
║                  OOOI E2E Testing - Quick Start Guide                         ║
║                                                                               ║
║         Running E2E Tests with Multus CNI and KubeVirt Support               ║
║                                                                               ║
╚═══════════════════════════════════════════════════════════════════════════════╝

📋 PREREQUISITES
================
✓ kind      (auto-installed via 'make kind' if needed)
✓ kubectl   (should be in PATH)
✓ podman    (or docker - for container runtime)
✓ Go 1.21+  (for building)


🚀 QUICK START (3 Commands)
============================

1. Complete Setup:
   $ make setup-test-e2e

2. Run E2E Tests:
   $ make test-e2e

3. Cleanup:
   $ make cleanup-test-e2e


📊 WHAT GETS TESTED
====================
✓ Controller Manager - Pod runs and metrics serve correctly
✓ Multus CNI         - Daemonset running, NADs configured
✓ KubeVirt           - Operator installed, VMs supported
✓ Infra Resources    - DHCP/DNS/Proxy components deploy with network attachments


⚙️ ADVANCED OPTIONS
====================

Using Podman Runtime:
  $ PODMAN_RUNTIME=true make test-e2e

Skip CertManager Installation:
  $ CERT_MANAGER_INSTALL_SKIP=true make test-e2e

Verify Setup Only (no tests):
  $ make setup-test-e2e

Install Specific Component:
  $ make install-multus
  $ make install-kubevirt
  $ make create-test-nads

Deep Cleanup (removes images too):
  $ make cleanup-test-e2e-deep


📂 IMPORTANT FILES
===================
config files:        hack/kind-config*.yaml
test NADs:          test/e2e/test-nads.yaml
test code:          test/e2e/e2e*.go
utilities:          test/utils/utils.go
setup script:       hack/e2e-setup.sh
documentation:      docs/E2E_TEST_SETUP.md


🔍 MONITORING & DEBUGGING
===========================

Check cluster status:
  $ kubectl cluster-info
  $ kubectl get nodes

Check Multus status:
  $ kubectl get ds kube-multus-ds -n kube-system
  $ kubectl get net-attach-def -A

Check KubeVirt status:
  $ kubectl get kubevirt -n kubevirt
  $ kubectl get vms,vmis -A

View logs:
  $ kubectl logs -l app=multus -n kube-system
  $ kubectl logs -l kubevirt.io=virt-operator -n kubevirt


📝 USEFUL COMMANDS
===================

# See all available make targets
make help | grep test-e2e

# Run tests with verbose output
make test-e2e

# Check if cluster exists
kind get clusters

# Get cluster details
kubectl cluster-info

# List Kind clusters with details
kind get clusters
for c in $(kind get clusters); do
  echo "Cluster: $c"
  kind get kubeconfig --name $c
done


🆘 TROUBLESHOOTING
===================

Problem: "Kind cluster creation hangs"
Solution: Increase timeout - kind create cluster --wait 10m

Problem: "Multus pods not starting"
Solution: Check node status - kubectl get nodes
          Check daemonset - kubectl describe ds kube-multus-ds -n kube-system

Problem: "KubeVirt not deploying"
Solution: Check operator - kubectl logs -n kubevirt -l kubevirt.io=virt-operator
          Enable emulation - kubectl patch -n kubevirt kubevirt kubevirt --type=merge --patch '{"spec":{"configuration":{"developerConfiguration":{"useEmulation":true}}}}'


📚 DETAILED DOCUMENTATION
===========================

For complete setup instructions:    docs/E2E_TEST_SETUP.md
For implementation details:         docs/E2E_IMPLEMENTATION_SUMMARY.md
For automated setup:                hack/e2e-setup.sh


🎯 NEXT STEPS
==============

1. Run the e2e tests:
   $ cd /Users/mbengtss/code/src/github.com/cldmnky/oooi
   $ make test-e2e

2. After successful tests, explore:
   - Modify test cases in test/e2e/e2e_test.go
   - Add more NetworkAttachmentDefinitions in test/e2e/test-nads.yaml
   - Extend Kind configuration for multi-node clusters

3. Integrate into CI/CD:
   - Add 'make test-e2e' to your pipeline
   - Configure test reporting
   - Set up automated cleanup


💡 TIPS
========

• Use './hack/e2e-setup.sh' for step-by-step interactive setup with status reporting
• Environment variable PODMAN_RUNTIME controls container runtime selection
• The setup is idempotent - safe to run multiple times
• All components can be installed individually with 'make install-*' targets
• Tests automatically cleanup resources on completion

╔═══════════════════════════════════════════════════════════════════════════════╗
║                                                                               ║
║                        Ready to test? Run:                                   ║
║                                                                               ║
║                           make test-e2e                                       ║
║                                                                               ║
╚═══════════════════════════════════════════════════════════════════════════════╝

EOF
