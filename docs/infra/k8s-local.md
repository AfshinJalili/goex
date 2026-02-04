# Local Kubernetes (Kind/K3d)

This repo includes Helm charts and a deploy stub, but no live cluster is required. For local testing:

## Option 1: Kind
```bash
kind create cluster --name cex
kubectl create namespace cex-core
helm upgrade --install cex deploy/helm/cex --namespace cex-core
```

## Option 2: K3d
```bash
k3d cluster create cex
kubectl create namespace cex-core
helm upgrade --install cex deploy/helm/cex --namespace cex-core
```

## Notes
- Images must be available in GHCR or loaded locally into the cluster.
- Update `deploy/helm/cex/values.yaml` for custom image tags.
