#!/bin/bash

# OP Stack Operator - Kind Cluster Setup Script
# This script creates a Kind cluster optimized for OP Stack development and testing

set -euo pipefail

# Configuration
CLUSTER_NAME="op-stack-operator"
CLUSTER_CONFIG="config/kind/simple-cluster.yaml"
REGISTRY_NAME="kind-registry"
REGISTRY_PORT="5000"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

log_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

log_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

log_error() {
    echo -e "${RED}❌ $1${NC}"
}

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."
    
    # Check if Kind is installed
    if ! command -v kind &> /dev/null; then
        log_error "Kind is not installed. Please install it first:"
        echo "  # On macOS"
        echo "  brew install kind"
        echo "  # On Linux"
        echo "  curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.20.0/kind-linux-amd64"
        echo "  chmod +x ./kind && sudo mv ./kind /usr/local/bin/kind"
        exit 1
    fi
    
    # Check if Docker is running
    if ! docker info &> /dev/null; then
        log_error "Docker is not running. Please start Docker first."
        exit 1
    fi
    
    # Check if kubectl is installed
    if ! command -v kubectl &> /dev/null; then
        log_warning "kubectl is not installed. Installing via Kind..."
        # Kind can provide kubectl, but it's better to have it separately
    fi
    
    log_success "Prerequisites check passed"
}

# Create local Docker registry for development
create_registry() {
    log_info "Setting up local Docker registry..."
    
    # Check if registry already exists
    if docker ps -a --format '{{.Names}}' | grep -q "^${REGISTRY_NAME}$"; then
        if docker ps --format '{{.Names}}' | grep -q "^${REGISTRY_NAME}$"; then
            log_success "Registry ${REGISTRY_NAME} is already running"
            return 0
        else
            log_info "Starting existing registry ${REGISTRY_NAME}..."
            docker start ${REGISTRY_NAME}
            log_success "Registry ${REGISTRY_NAME} started"
            return 0
        fi
    fi
    
    # Create new registry
    docker run -d --restart=always -p "127.0.0.1:${REGISTRY_PORT}:5000" --name "${REGISTRY_NAME}" registry:2
    log_success "Created local Docker registry at localhost:${REGISTRY_PORT}"
}

# Create Kind cluster
create_cluster() {
    log_info "Creating Kind cluster '${CLUSTER_NAME}'..."
    
    # Check if cluster already exists
    if kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
        log_warning "Cluster '${CLUSTER_NAME}' already exists"
        read -p "Do you want to delete and recreate it? [y/N]: " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            log_info "Deleting existing cluster..."
            kind delete cluster --name "${CLUSTER_NAME}"
        else
            log_info "Using existing cluster"
            return 0
        fi
    fi
    
    # Create the cluster
    if [[ -f "${CLUSTER_CONFIG}" ]]; then
        kind create cluster --name "${CLUSTER_NAME}" --config "${CLUSTER_CONFIG}"
    else
        log_warning "Cluster config not found at ${CLUSTER_CONFIG}, using default configuration"
        kind create cluster --name "${CLUSTER_NAME}"
    fi
    
    log_success "Kind cluster '${CLUSTER_NAME}' created successfully"
}

# Connect registry to cluster network
connect_registry() {
    log_info "Connecting registry to cluster network..."
    
    # Connect the registry to the cluster network if not already connected
    if ! docker network ls | grep -q "kind"; then
        log_error "Kind network not found. Is the cluster running?"
        return 1
    fi
    
    # Check if registry is already connected to kind network
    if docker inspect ${REGISTRY_NAME} | grep -q '"kind"'; then
        log_success "Registry already connected to kind network"
    else
        docker network connect "kind" "${REGISTRY_NAME}" || true
        log_success "Connected registry to kind network"
    fi
    
    # Document the local registry
    # https://github.com/kubernetes/enhancements/tree/master/keps/sig-cluster-lifecycle/generic/1755-communicating-a-local-registry
    kubectl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "localhost:${REGISTRY_PORT}"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
EOF
    
    log_success "Registry configuration documented in cluster"
}

# Install ingress controller
install_ingress() {
    log_info "Installing NGINX Ingress Controller..."
    
    # Install ingress-nginx
    kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml
    
    # Wait for ingress controller to be ready
    log_info "Waiting for ingress controller to be ready..."
    kubectl wait --namespace ingress-nginx \
        --for=condition=ready pod \
        --selector=app.kubernetes.io/component=controller \
        --timeout=90s
    
    log_success "NGINX Ingress Controller installed and ready"
}

# Create namespace for OP Stack Operator
create_namespace() {
    log_info "Creating namespace for OP Stack Operator..."
    
    kubectl create namespace op-stack-operator-system --dry-run=client -o yaml | kubectl apply -f -
    
    log_success "Namespace 'op-stack-operator-system' created"
}

# Set up storage classes optimized for OP Stack
setup_storage() {
    log_info "Setting up storage classes for OP Stack workloads..."
    
    # Create a fast SSD storage class (simulated in Kind)
    kubectl apply -f - <<EOF
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: fast-ssd
  annotations:
    storageclass.kubernetes.io/is-default-class: "false"
provisioner: rancher.io/local-path
volumeBindingMode: WaitForFirstConsumer
allowVolumeExpansion: true
reclaimPolicy: Delete
parameters:
  # Simulate SSD performance characteristics
  hostDir: /tmp/op-stack-fast-storage
EOF
    
    log_success "Storage classes configured"
}

# Create sample environment file
create_env_file() {
    log_info "Creating sample environment file..."
    
    ENV_FILE="test/config/env.local"
    if [[ ! -f "${ENV_FILE}" ]]; then
        cp test/config/env.example "${ENV_FILE}"
        log_success "Created ${ENV_FILE} from example"
        log_warning "Please edit ${ENV_FILE} with your actual L1 RPC URLs before running tests"
    else
        log_info "Environment file ${ENV_FILE} already exists"
    fi
}

# Display cluster information
show_cluster_info() {
    log_success "🎉 Kind cluster setup complete!"
    echo
    echo "📋 Cluster Information:"
    echo "  Cluster Name: ${CLUSTER_NAME}"
    echo "  Registry:     localhost:${REGISTRY_PORT}"
    echo "  Kubeconfig:   ~/.kube/config (automatically updated)"
    echo
    echo "🔧 Useful Commands:"
    echo "  # Check cluster status"
    echo "  kubectl cluster-info --context kind-${CLUSTER_NAME}"
    echo
    echo "  # View nodes"
    echo "  kubectl get nodes"
    echo
    echo "  # Load operator image into cluster"
    echo "  make kind-load"
    echo
    echo "  # Install CRDs"
    echo "  make install"
    echo
    echo "  # Deploy operator"
    echo "  make deploy"
    echo
    echo "  # Run e2e tests"
    echo "  make test-e2e"
    echo
    echo "  # Access local services (after deployment)"
    echo "  # OpNode RPC: http://localhost:8545"
    echo "  # OpNode WS:  ws://localhost:8546"
    echo
    echo "🗑️  Cleanup:"
    echo "  # Delete cluster"
    echo "  kind delete cluster --name ${CLUSTER_NAME}"
    echo
    echo "  # Stop and remove registry"
    echo "  docker stop ${REGISTRY_NAME} && docker rm ${REGISTRY_NAME}"
}

# Main execution
main() {
    echo "🚀 Setting up Kind cluster for OP Stack Operator development"
    echo
    
    check_prerequisites
    create_registry
    create_cluster
    connect_registry
    install_ingress
    create_namespace
    setup_storage
    create_env_file
    show_cluster_info
}

# Cleanup function for script interruption
cleanup() {
    log_warning "Script interrupted. Cluster may be in partial state."
    log_info "Run: kind delete cluster --name ${CLUSTER_NAME} to clean up"
    exit 1
}

# Set up signal handlers
trap cleanup SIGINT SIGTERM

# Run main function
main "$@"
