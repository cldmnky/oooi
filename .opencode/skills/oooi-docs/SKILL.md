---
name: oooi documentation
description: Maintain and verify the oooi MkDocs site against its Go API and reconcilers. Use for website content, examples, documentation reviews, and GitHub Pages publication.
---

# oooi documentation

Use this skill when editing or reviewing the end-user documentation site.

## Scope

- Site source: `website/docs/`
- MkDocs configuration: `website/mkdocs.yml`
- Generated site: `website/site/` (ignored; never edit)
- GitHub Pages workflow: `.github/workflows/docs.yml`
- Public site: `https://cldmnky.github.io/oooi/`

The site is Markdown rendered by MkDocs Material. Do not add AsciiDoc files or
AsciiDoc-only syntax.

## Source of truth

Verify every API, default, status field, object name, command, and lifecycle
claim against implementation before publishing.

| Subject | Source files |
|---|---|
| `Infra` API schema and defaults | `api/v1alpha1/infra_types.go`, `config/crd/bases/*infra*.yaml` |
| Child CR APIs | `api/v1alpha1/dhcp_server_types.go`, `api/v1alpha1/dns_server_types.go`, `api/v1alpha1/proxy_server_types.go` |
| Infra reconciliation, generated child specs, status, apps ingress, and NetworkPolicy | `internal/controller/infra_controller.go` |
| DHCP, DNS, and proxy workload behavior | `internal/controller/dhcpserver_controller.go`, `internal/controller/dnsserver_controller.go`, `internal/controller/proxy_server_controller.go` |
| CLI flags and manager behavior | `cmd/manager.go`, `cmd/root.go` |
| Supported Make targets and image-build variables | `Makefile` |
| Deployment arguments and manager defaults | `config/manager/manager.yaml`, `config/default/` |
| Tested configurations | `config/samples/`, `internal/controller/*_test.go`, `test/e2e/` |

Do not treat existing documentation, old samples, or a generic Kubernetes
convention as evidence of current behavior.

## Critical implementation checks

Check these details whenever related content changes:

- `Infra.status.conditions` and `componentStatus` report reconciliation and
  provisioning, not Deployment availability. Use `kubectl rollout status` for
  runtime readiness.
- Disabling a DHCP, DNS, or proxy component stops reconciliation. It does not
  delete an existing child CR.
- Child CRs are owned by `Infra`; cross-namespace NetworkPolicies and
  cluster-scoped DHCP RBAC cannot be owned and require cleanup checks.
- Apps-ingress resources are created in the hosted cluster and are not owned by
  `Infra`; explain their lifecycle separately.
- If `networkAttachmentNamespace` is omitted, the reconciler uses the `Infra`
  namespace. It does not fall back to `default`.
- `InfraClusterAttachment` supplies the per-cluster control-plane namespace and
  optional API Service name; shared Infra configuration has no cluster-specific
  routing fields.
- The default DHCP and DNS component image is the oooi image. The proxy uses
  the Envoy image plus an oooi manager sidecar.
- `make container-build` uses `IMAGE_TAG_BASE`, not a caller-provided
  `KO_DOCKER_REPO`.
- `make run` does not forward arbitrary arguments. Use `go run ./main.go
  manager <flags>` for manager flags.

## Multi-cluster ownership rules

- `Infra` is network-scoped and is the only writer of the shared DHCPServer,
  DNSServer, and ProxyServer. Never document attachments writing those children.
- Cluster-specific DNS, control-plane, and apps-ingress settings belong on
  `InfraClusterAttachment`; there is no implicit single-cluster binding.
- One attachment per HostedCluster; duplicate domains or duplicate hosted
  cluster references exclude both sides from routing with Degraded conditions —
  never describe silent conflict resolution.
- Shared proxies answer fully qualified SNI names for every attachment. For
  KubeVirt attachments, the four unqualified Kubernetes aliases
  (`kubernetes`, `kubernetes.default`, `kubernetes.default.svc`, and
  `kubernetes.default.svc.cluster.local`) are emitted only when CAPI Machine
  status contains worker addresses inside the shared Infra CIDR. Envoy scopes
  those alias chains to the discovered worker source `/32`s; never describe
  them as a global catch-all or as authentication.
- `proxy.externalService.publishAttachmentOAuths` merges Ready attachments'
  oauth names into the hostname annotation (user names first, additions sorted).
- New CRD files must be added to `config/crd/kustomization.yaml`; omissions
  surface in E2E as an unsynced informer cache, not as a manifest error.

## Example policy

- Use `example-hcp`, `clusters.example.com`, RFC 5737 addresses such as
  `192.0.2.0/24` and `198.51.100.0/24`, and `registry.example.com`.
- State that users must replace documentation values before applying manifests.
- Never publish home-lab names, domains, IPs, zone IDs, credentials, or personal
  registry paths.
- Keep examples structurally valid for current `HostedCluster`, `NodePool`, and
  `Infra` APIs. Use `config/samples/` and current manifests as structural
  references only after removing environment-specific values.

## Writing requirements

- Use sentence-case headings, official Red Hat product names, concise active
  voice, inclusive language, and serial commas.
- Keep documentation operational rather than workshop-oriented. Do not add
  learning objectives, presenter notes, slide cues, ROI claims, or demo scripts
  unless explicitly requested.
- Include an expected result for verification commands. Link to troubleshooting
  rather than repeating unverified diagnosis.
- Preserve heading order (`h1` then `h2`, then `h3`), descriptive image alt
  text, and keyboard-independent instructions.
- Use official Red Hat documentation links for external product guidance. Keep
  release-specific references in `website/docs/references.md`.

## Validation

Run from the stated directory:

```bash
cd website
/private/var/folders/4v/4wfn31mx309csltn4hym9nvm0000gn/T/opencode/oooi-docs-venv/bin/mkdocs build --strict --site-dir site
```

For changes involving implementation claims, also run from the repository root:

```bash
make test
git diff --check
```

Before publishing, scan authored Markdown in `website/docs/` and rendered HTML
in `website/site/` for environment-specific identifiers and prohibited
language. Do not scan bundled third-party JavaScript. Do not commit the
generated `website/site/` directory.

## Publication

Commit the source changes, push `main`, and verify the `Docs` GitHub Actions
workflow succeeds. Confirm the relevant deployed page at
`https://cldmnky.github.io/oooi/` contains the new content.
