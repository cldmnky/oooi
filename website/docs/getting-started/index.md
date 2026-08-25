# Getting started

Deploy oooi after your OpenShift Container Platform management cluster has
hosted control planes (HyperShift) and Red Hat OpenShift Virtualization in
place, and while you create a hosted cluster. The recommended flow is:

1. [Verify prerequisites](prerequisites.md) — cluster operators, network, IP plan, secrets.
2. [Install oooi](../installation/deploy-operator.md) — CRDs plus the controller Deployment.
3. Create the `HostedCluster` (and `NodePool`) for your KubeVirt workers.
4. Apply an [`Infra`](../configuration/infra-reference.md) resource describing
   the VLAN and an `InfraClusterAttachment` for each hosted cluster using it.
5. [Verify](../operations/verify.md) the VLAN path, plus the optional pod-network
   and public-DNS paths when those integrations are configured.

!!! tip "Order matters"

    Apply `Infra` **as soon as the `HostedCluster` object exists** — do not wait
    for it to become `Available`. The DHCP, DNS, and proxy services are required
    *while* worker VMs bootstrap. Without them the Ignition and konnectivity
    traffic from new nodes has nowhere to go.

<div class="grid cards" markdown>

-   :material-clipboard-check-outline: __Prerequisites__

    ---

    Management-cluster capabilities, network planning worksheet, and secrets.

    [Check requirements :material-arrow-right:](prerequisites.md)

-   :material-rocket-launch-outline: __Quickstart__

    ---

    A complete walkthrough from operator install to a verified hosted cluster.

    [Run the walkthrough :material-arrow-right:](quickstart.md)

</div>
