## Plan: Implement Envoy Proxy with xDS

This plan outlines the implementation of an Envoy-based Layer 4 proxy to facilitate connectivity from the secondary network to the Hosted Control Plane, managed by a new `Proxy` CRD and an xDS control plane.

### Steps
1.  **Define Proxy API (`api/v1alpha1/proxy_types.go`)**
    *   Create `Proxy` (or `ProxyServer`) CRD to match `DNSServer`/`DHCPServer` pattern.
    *   `Spec` will include:
        *   `NetworkConfig`: IP, NAD, etc. for the secondary network.
        *   `Backends`: List of `{Name, Hostname, DestinationService, DestinationPort}`.
    *   `Status` will track the Deployment and Service status.

2.  **Implement `oooi proxy` Command (xDS Server)**
    *   Create `cmd/proxy.go` and `internal/proxy/server.go`.
    *   Implement an xDS server using `go-control-plane` that listens on `localhost`.
    *   Watch the `Proxy` CR for changes.
    *   Translate `Backends` into Envoy configuration:
        *   **Listener**: Single listener on port 443 (or configured port).
        *   **Filter Chains**: Use SNI (`server_names`) to route to different Clusters.
        *   **Clusters**: `LOGICAL_DNS` clusters pointing to the internal K8s Services (e.g., `api.cluster.svc`).

3.  **Implement Proxy Controller (`internal/controller/proxy_controller.go`)**
    *   Reconcile `Proxy` CRs.
    *   Create a **ConfigMap** containing the Envoy bootstrap config (pointing to xDS at `localhost`).
    *   Create a **Deployment** with two containers:
        1.  `envoy`: The sidecar, running Envoy with the bootstrap config.
        2.  `manager`: The `oooi` binary running `oooi proxy`.
    *   Ensure proper Multus annotations on the Pod for secondary network connectivity.

4.  **Update Infra Controller (`internal/controller/infra_controller.go`)**
    *   Add logic to create/update the `Proxy` CR based on `infra.Spec.InfraComponents.Proxy`.
    *   Define the standard HCP backends (API, OAuth, Ignition, Konnectivity) with their specific SNI hostnames and target internal Services.

5.  **Integration & RBAC**
    *   Register the new controller and API in `main.go`.
    *   Update RBAC roles to allow managing `Proxy` resources and Deployments.

### Further Considerations
1.  **Dependency**: We will need to add `github.com/envoyproxy/go-control-plane` to `go.mod`.
2.  **Naming**: Is `ProxyServer` acceptable as the Kind name to align with `DNSServer` and `DHCPServer`?
3.  **Ports**: Confirm that *all* traffic (API, OAuth, Ignition, Konnectivity) should be multiplexed over port 443 via SNI, or if we need to support multiple ports (e.g. 22623 for Ignition). (The prompt implies "single ip... SNI", usually meaning 443, but Ignition often uses 22623. We can support both via the CRD).
