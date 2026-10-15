# AGENTS.md

## What this is
`helmi` is a Go CLI that converts a rendered Kustomize application into a Helm
chart. Its core guarantee: the generated chart must render back to exactly the
same Kubernetes objects. It is a Go reimplementation of
`kustomize-to-helm` (Python); keep CLI behavior aligned with upstream.

## Commands
No Makefile or test CI job exists. Run tools directly:
- `go build -o helmi .`
- `go test ./...`
- `go vet ./...`
- single test: `go test ./internal/cli/ -run TestForceRemovesStaleFiles`

A *verified* migration (the default) shells out to `kustomize` (or
`kubectl kustomize`) and `helm`. Integration/CLI tests skip silently when those
binaries are absent — a skip is not a pass. Building needs Go 1.23+ (go.mod).
`.gitignore` excludes the `/helmi` binary and `/dist/`; never commit build output.

## Version
- Single source of truth: `internal/version/version.go` (`version.Version`).
- `.github/workflows/release.yml` overrides it via
  `-ldflags "-X helmi/internal/version.Version=<tag-without-v>"`. If you move the
  variable, update that ldflags path too.

## Pipeline — do not break the guarantee
`migrate` runs: `kustomize build` -> parse/index -> generate chart ->
`helm lint --strict` -> `helm template` -> `resources.AssertResourceEquivalence`.
Any added/missing/changed resource fails the migration; nothing is written unless
verification passes.

- Generation is **fidelity-first, not templating**. Each resource is stored whole
  as a literal block scalar in `values.yaml` under `resources.<key>.manifest`;
  `templates/resources.yaml` only ranges over them. Do NOT "improve" this into
  conventional Helm value interpolation — map merging cannot represent field
  deletions, list replacement, or CRDs. `internal/templateconv` may inspect
  fields but must never rewrite names, selectors, or references.
- Resource identity = `apiVersion:kind:namespace:name` (`internal/resources`).
  Duplicate identities are fatal. Values keys are
  `<Kind>-<ns>-<name>-<sha256[0:10]>`; changing the hashing causes golden churn.
- Equivalence compares decoded Kubernetes objects (whitespace/quote/key-order
  differences are fine), not raw YAML text.
- Generation is transactional: the chart is built in `.helmi-stage-*` inside the
  output dir, verified, then swapped in by rename. With `--force` the old chart
  moves to a backup and is restored if the swap fails. Existing charts require
  `--force`.

## Errors
- Use the categorized errors in `internal/errs` (`Configurationf`, `Buildf`,
  `Generationf`, `Validationf`, `Wrap`) for user-actionable failures instead of
  `fmt.Errorf`. Categories drive CLI messaging and test expectations.
- `helm.sh/hook` and `helm.sh/resource-policy` annotations are intentionally
  blocked: inert in Kustomize but behavioral in Helm.

## Layout
- `main.go` -> `internal/cli` (cobra: `migrate`, `analyze`, `validate`, `init`).
- `internal/migrator` handles one config; `internal/multioverlay` handles
  base+overlays. Every immediate non-hidden subdir of `--overlays-dir` is an
  overlay, and any single overlay failure aborts the whole run.
- `internal/kustomize` parser (runs the builder), `internal/generator` chart
  writer, `internal/helm` validator, `internal/overlay` diff analyzer,
  `internal/yamlutil` node helpers.
- Fixtures: `testdata/application` (basic) and `testdata/critical` (patches,
  CRDs, secrets, Gateway API, configmaps). Integration tests assert exact
  equivalence against them.
