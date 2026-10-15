# helmi

`helmi` converts a Kustomize application into a Helm chart.

The command-line tool is called `helmi`. It is a Go reimplementation of
[`kustomize-to-helm`](https://github.com/gokulapap/kustomize-to-helm).

Its main goal is safety. A migration should not look correct while quietly
changing your Kubernetes resources. The tool therefore builds the source with
Kustomize, creates the Helm chart, renders that chart with Helm, and compares the
two results before reporting success.

## Origin

This project is a Go reimplementation of
[`kustomize-to-helm`](https://github.com/gokulapap/kustomize-to-helm)
by gokulapap. The original tool inspired this project; functionality and
CLI behavior follow the upstream project.

## How it works

For a normal migration, the tool follows these steps:

1. Run `kustomize build` on the source.
2. Read and validate every rendered Kubernetes resource.
3. Generate a Helm chart from those final resources.
4. Run strict `helm lint` on the generated chart.
5. Run `helm template`.
6. Compare the complete Helm output with the Kustomize output.

If a resource is missing, added unexpectedly, or changed, the migration fails.
The destination is not replaced until validation passes.

The YAML text may use different whitespace, quotes, or key ordering. The parsed
Kubernetes objects - including names, namespaces, fields, lists, and values - must
be the same.

## What it supports

The tool works with final resources rendered by Kustomize, so it can preserve:

- strategic merge, JSON 6902, and unified patches;
- name prefixes, suffixes, namespaces, labels, and annotations;
- image names, tags, digests, and replica changes;
- ConfigMap and Secret generators, including generated name hashes;
- base and overlay resource additions, changes, renames, and deletions;
- Deployments, StatefulSets, DaemonSets, Jobs, CronJobs, and Services;
- RBAC, Ingress, Gateway API, autoscaling, policies, and storage resources;
- CRDs and arbitrary custom resources;
- multiple containers, init containers, volumes, probes, and security settings;
- multiline data and application text containing literal `{{ ... }}` expressions.

The tool does not try to recreate Kustomize behavior itself. Kustomize produces
the source manifests used for migration.

## Requirements

- Go 1.23 or newer to build (only for building from source)
- `kustomize` on `PATH`, or `kubectl` with `kubectl kustomize`
- Helm on `PATH`

Helm is required for the normal verified migration. You can explicitly use
`--no-verify`, but that removes the equivalence guarantee.

## Build

```bash
go build -o helmi .
```

## Usage

### Single application

```bash
helmi migrate ./kustomize ./charts --chart-name my-app
```

### Base with overlays

```bash
helmi migrate ./app/base ./charts \
  --overlays-dir ./app/overlays \
  --chart-name my-app
```

Every immediate, non-hidden directory under `--overlays-dir` is treated as an
overlay. If one overlay is broken, the complete migration fails; broken overlays
are never silently skipped.

### Other commands

```bash
helmi analyze ./kustomize                      # analyze without generating
helmi validate ./charts/my-app --strict        # lint an existing chart
helmi init -n my-app -o ./charts               # create a chart scaffold
```

### Useful flags

```bash
helmi migrate ./kustomize ./charts -n my-app --dry-run          # write nothing
helmi migrate ./kustomize ./charts -n my-app --output-format json
helmi migrate ./kustomize ./charts -n my-app --force            # transactional replace
helmi migrate ./kustomize ./charts -n my-app --no-verify
```

## Why manifests are stored in values

Each Kubernetes resource is stored as a complete manifest in `values.yaml`:

```yaml
resources:
  deployment-api-2f89d8c1a0:
    enabled: true
    manifest: |
      apiVersion: apps/v1
      kind: Deployment
      metadata:
        name: api
      # ...the rest of the rendered resource...
```

This is intentional. Normal Helm map merging cannot faithfully represent every
field deletion, list replacement, generated resource, or custom resource.

Overlay values files enable or disable the correct resources and replace only the
manifests that changed. This keeps the first migration accurate. After the chart
is verified, teams can gradually move selected fields into conventional Helm
values under their normal review process.

## Safe overwrite behavior

The tool will not overwrite an existing chart unless you pass `--force`. The
replacement is transactional: the existing chart remains in place while the new
chart is generated and validated, and is kept if validation fails.

## Errors the tool handles

- invalid or duplicate YAML keys;
- missing files and broken Kustomize references;
- failed or timed-out Kustomize builds;
- duplicate Kubernetes resource identities;
- invalid Helm chart names or versions;
- overlay values filename collisions;
- failed Helm lint or rendering;
- any difference between Helm and Kustomize output;
- an existing destination without `--force`.

The tool also blocks source `helm.sh/hook` and `helm.sh/resource-policy`
annotations. These annotations do nothing in Kustomize but change resource
lifecycle behavior in Helm, so automatically migrating them would be misleading.

## Secrets and CRDs

Rendered Secret data is copied into the generated values files. Treat the chart as
sensitive and use your normal secret-management process before committing it.

CRDs are kept in the chart templates so the generated output stays equal to the
Kustomize output. Review your organization's CRD installation and upgrade policy
before deploying the chart.

## Tests

```bash
go test ./...
go vet ./...
```

Integration tests build, generate, and render with the bundled fixtures under
`testdata/`. They skip automatically when Kustomize (or `kubectl`) or Helm is not
installed.
