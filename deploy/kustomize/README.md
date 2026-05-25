# Warden Kubernetes Deployment

## Quick Start

```bash
# Apply to cluster
kubectl apply -k deploy/kustomize/

# Or build and apply
kubectl kustomize deploy/kustomize/ | kubectl apply -f -
```

## Configuration

Edit `deploy/kustomize/config.yaml` to configure providers.
