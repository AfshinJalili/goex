# Kubernetes Base Infrastructure

This directory contains baseline Kubernetes manifests and a Kong Helm chart to model production-like infrastructure without requiring a live cluster.

## Apply Order
1. Namespaces
2. Network policies
3. Kong Helm chart

### Namespaces
```bash
kubectl apply -f deploy/k8s/namespaces/
```

### Network Policies
```bash
kubectl apply -f deploy/k8s/network-policies/
```

### Kong (Helm)
```bash
helm upgrade --install kong deploy/helm/kong \
  --namespace cex-ops --create-namespace \
  --values deploy/helm/kong/values.yaml
```

## Service Type Overrides (Local)
For local testing, you can override the proxy service type:
```bash
helm upgrade --install kong deploy/helm/kong \
  --namespace cex-ops --create-namespace \
  --set proxy.type=NodePort
```

## TLS Notes
- Default TLS secret: `kong-proxy-cert` in `cex-ops`.
- Provide your own cert secret or update `values.yaml`.

## Base Domain
Define a base domain in your ingress resources, e.g. `api.local` or `cex.local`.
