# CI/CD

## Workflows
- `CI`: runs on PRs and `main` to lint and test Go services.
- `Build and Push Images`: builds all `services/*/Dockerfile` images and pushes to GHCR.
- `Deploy Staging (Stub)`: manual workflow that deploys via Helm **only if** `KUBECONFIG_STAGING` is set.

## Required Secrets
- `GITHUB_TOKEN` (provided by GitHub Actions).
- `KUBECONFIG_STAGING` (optional; only required if enabling deploy workflow).

## Image Tagging
- `ghcr.io/<org>/<repo>/<service>:<sha>` (lowercase, per GHCR requirements)
- `ghcr.io/<org>/<repo>/<service>:latest`

## SBOM and Vulnerability Scans
- SBOM generated with Syft.
- Vulnerability scan with Grype.
- Artifacts uploaded as `sbom-and-scans`.

## Adding a Service
1. Add `services/<name>/Dockerfile`.
2. Ensure it builds with `go build`.
3. CI will auto-discover it and build/push on `main`.
