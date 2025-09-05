# Kind Cluster Setup for OP Stack Operator

This guide explains how to set up a Kind (Kubernetes in Docker) cluster optimized for OP Stack Operator development and testing.

## Overview

The OP Stack Operator Kind cluster provides:

- **Multi-node cluster** with dedicated worker nodes for OP Stack components
- **Local Docker registry** for fast image development cycles
- **Optimized networking** with port mappings for OP Stack services
- **Storage classes** configured for blockchain workloads
- **Ingress controller** for external access
- **Development scripts** for common workflows

## Quick Start

### 1. Prerequisites

```bash
# Install Kind (if not already installed)
# macOS
brew install kind

# Linux
curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.20.0/kind-linux-amd64
chmod +x ./kind && sudo mv ./kind /usr/local/bin/kind

# Ensure Docker is running
docker info
```

### 2. Create the Cluster

```bash
# Create Kind cluster with all components
make kind-create

# Or use the script directly
./scripts/setup-kind-cluster.sh
```

### 3. Verify Setup

```bash
# Check cluster status
make kind-status

# Verify kubectl context
kubectl config current-context
# Should show: kind-op-stack-operator

# Check nodes
kubectl get nodes
```

## Cluster Configuration

### Node Configuration

The cluster consists of:

- **1 Control Plane Node**
  - Runs Kubernetes API server, etcd, controller-manager
  - Exposes ingress ports (80, 443)
  - Exposes OP Stack service ports (8545, 8546, 9003)
  
- **2 Worker Nodes**
  - Optimized for OP Stack workloads
  - Increased pod limits (200 pods per node)
  - Resource reservations for system components

### Network Configuration

```yaml
# Port Mappings (Host -> Container)
80    -> 80    # HTTP Ingress
443   -> 443   # HTTPS Ingress
18545 -> 30545 # OpNode RPC
18546 -> 30546 # OpNode WebSocket
19003 -> 30547 # OpNode P2P
```

### Storage Classes

| Name | Purpose | Configuration |
|------|---------|---------------|
| `standard` | Default storage | Local path provisioner |
| `fast-ssd` | OP Stack data | Optimized for blockchain workloads |

## Development Workflow

### Complete Setup

```bash
# First-time setup (cluster + operator + environment)
./scripts/dev-workflow.sh setup
```

### Build and Deploy Operator

```bash
# Build image and deploy to cluster
make kind-load
make deploy

# Or use the workflow script
./scripts/dev-workflow.sh deploy
```

### Run Tests

```bash
# Configure environment (first time only)
cp test/config/env.example test/config/env.local
# Edit test/config/env.local with your L1 RPC URLs

# Run all tests
make test-integration
make test-e2e

# Or use the workflow script
./scripts/dev-workflow.sh test
```

### Deploy Sample Resources

```bash
# Deploy sample OptimismNetwork and OpNode
./scripts/dev-workflow.sh samples

# Monitor resources
kubectl get optimismnetwork,opnode -w
```

## Available Make Targets

```bash
# Cluster Management
make kind-create    # Create Kind cluster with full setup
make kind-delete    # Delete cluster and cleanup
make kind-status    # Show cluster status
make kind-load      # Load operator image into cluster

# Development Workflow
make install        # Install CRDs
make deploy         # Deploy operator
make undeploy       # Remove operator
make test-e2e       # Run e2e tests (requires cluster)
```

## Local Registry

The setup includes a local Docker registry for fast development cycles:

- **Registry URL**: `localhost:5000`
- **Container Name**: `kind-registry`
- **Connected to Kind network** for internal access

### Using the Registry

```bash
# Tag and push images
docker tag my-image localhost:5000/my-image
docker push localhost:5000/my-image

# Use in Kubernetes manifests
image: localhost:5000/my-image
```

## Accessing Services

### OP Stack Services

Once you deploy OpNode resources, you can access them locally:

```bash
# OpNode RPC (if exposed via NodePort/LoadBalancer)
curl http://localhost:18545 -X POST -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'

# OpNode WebSocket
wscat -c ws://localhost:18546
```

### Kubernetes Dashboard (Optional)

```bash
# Install dashboard
kubectl apply -f https://raw.githubusercontent.com/kubernetes/dashboard/v2.7.0/aio/deploy/recommended.yaml

# Create service account and get token
kubectl apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: admin-user
  namespace: kubernetes-dashboard
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: admin-user
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
- kind: ServiceAccount
  name: admin-user
  namespace: kubernetes-dashboard
EOF

# Get token
kubectl -n kubernetes-dashboard create token admin-user

# Proxy to dashboard
kubectl proxy
# Access: http://localhost:8001/api/v1/namespaces/kubernetes-dashboard/services/https:kubernetes-dashboard:/proxy/
```

## Troubleshooting

### Common Issues

#### 1. Cluster Creation Fails

```bash
# Check Docker is running
docker info

# Check available resources
docker system df

# Clean up old clusters
kind delete cluster --name op-stack-operator
docker system prune
```

#### 2. Image Loading Fails

```bash
# Verify cluster exists
kind get clusters

# Verify image exists locally
docker images | grep op-stack-operator

# Load with explicit cluster name
kind load docker-image example.com/op-stack-operator:v0.0.1 --name op-stack-operator
```

#### 3. Registry Connection Issues

```bash
# Check registry is running
docker ps | grep kind-registry

# Restart registry if needed
docker restart kind-registry

# Reconnect to kind network
docker network connect kind kind-registry
```

#### 4. kubectl Context Issues

```bash
# List available contexts
kubectl config get-contexts

# Switch to correct context
kubectl config use-context kind-op-stack-operator

# Verify connection
kubectl cluster-info
```

### Debug Commands

```bash
# Check cluster status
./scripts/dev-workflow.sh status

# View operator logs
kubectl logs -n op-stack-operator-system deployment/op-stack-operator-controller-manager -f

# Check events
kubectl get events --sort-by=.metadata.creationTimestamp

# Describe problematic resources
kubectl describe opnode <name>
kubectl describe optimismnetwork <name>
```

## Cleanup

### Temporary Cleanup

```bash
# Remove test resources only
./scripts/dev-workflow.sh cleanup
```

### Complete Cleanup

```bash
# Remove cluster and registry
make kind-delete

# Or manually
kind delete cluster --name op-stack-operator
docker stop kind-registry && docker rm kind-registry
```

## Advanced Configuration

### Custom Cluster Configuration

To modify the cluster configuration, edit `config/kind/cluster-config.yaml`:

```yaml
# Add more nodes
nodes:
  - role: control-plane
  - role: worker
  - role: worker  
  - role: worker  # Additional worker

# Change resource limits
kubeadmConfigPatches:
  - |
    kind: JoinConfiguration
    nodeRegistration:
      kubeletExtraArgs:
        max-pods: "300"  # Increase pod limit
```

### Custom Port Mappings

```yaml
extraPortMappings:
  - containerPort: 30080  # Custom service port
    hostPort: 8080
    protocol: TCP
```

### Persistent Data Directory

The cluster mounts `/tmp/op-stack-data` from the host for persistent storage during development. This can be customized in the cluster configuration.

## Performance Considerations

### Resource Requirements

- **Minimum**: 4 CPU cores, 8GB RAM
- **Recommended**: 8 CPU cores, 16GB RAM
- **Storage**: 50GB available disk space

### Optimization Tips

1. **Increase Docker resources** in Docker Desktop settings
2. **Use SSD storage** for Docker data directory
3. **Close unnecessary applications** to free up resources
4. **Use smaller OP Stack images** during development

## Integration with CI/CD

The Kind setup is designed to work in CI/CD environments:

```yaml
# GitHub Actions example
- name: Create Kind cluster
  run: |
    make kind-create
    
- name: Run tests
  env:
    TEST_L1_RPC_URL: ${{ secrets.TEST_L1_RPC_URL }}
  run: |
    make test-e2e
```

## Next Steps

After setting up the Kind cluster:

1. **Configure test environment**: Edit `test/config/env.local`
2. **Deploy sample resources**: Run `./scripts/dev-workflow.sh samples`
3. **Start development**: Use `./scripts/dev-workflow.sh deploy` for rapid iteration
4. **Run tests**: Use `./scripts/dev-workflow.sh test` to validate changes

For more information, see:
- [Testing Guide](../guides/testing-setup.md)
- [Development Guide](./development-guide.md)
- [Operator Architecture](../architecture/overview.md)
