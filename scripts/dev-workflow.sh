#!/bin/bash

# OP Stack Operator - Development Workflow Helper
# This script provides common development tasks for the OP Stack Operator

set -euo pipefail

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

# Show usage
show_usage() {
    echo "🚀 OP Stack Operator Development Workflow"
    echo
    echo "Usage: $0 <command>"
    echo
    echo "Commands:"
    echo "  setup      - Complete development environment setup"
    echo "  deploy     - Build and deploy operator to Kind cluster"
    echo "  test       - Run integration and e2e tests"
    echo "  samples    - Deploy sample OpNode configurations"
    echo "  logs       - Show operator logs"
    echo "  cleanup    - Clean up test resources"
    echo "  status     - Show cluster and operator status"
    echo "  reset      - Reset cluster and redeploy operator"
    echo
    echo "Examples:"
    echo "  $0 setup    # First-time setup"
    echo "  $0 deploy   # Quick deploy during development"
    echo "  $0 test     # Run all tests"
    echo
}

# Complete development environment setup
setup() {
    log_info "Setting up complete development environment..."
    
    # Create Kind cluster
    log_info "Creating Kind cluster..."
    make kind-create
    
    # Setup test environment
    log_info "Setting up test environment..."
    make setup-test-env
    
    # Install CRDs
    log_info "Installing CRDs..."
    make install
    
    # Deploy operator
    log_info "Deploying operator..."
    make deploy
    
    log_success "Development environment setup complete!"
    log_info "Next steps:"
    echo "  1. Edit test/config/env.local with your L1 RPC URLs"
    echo "  2. Run: $0 test"
    echo "  3. Run: $0 samples"
}

# Build and deploy operator
deploy() {
    log_info "Building and deploying operator..."
    
    # Build and load image
    make kind-load
    
    # Deploy to cluster
    make deploy
    
    # Wait for deployment
    log_info "Waiting for operator to be ready..."
    kubectl wait --for=condition=available --timeout=300s deployment/op-stack-operator-controller-manager -n op-stack-operator-system
    
    log_success "Operator deployed successfully!"
}

# Run tests
test() {
    log_info "Running tests..."
    
    # Check if environment is configured
    if [[ ! -f "test/config/env.local" ]]; then
        log_warning "Test environment not configured. Running setup..."
        make setup-test-env
        log_warning "Please edit test/config/env.local with your L1 RPC URLs before running tests"
        return 1
    fi
    
    # Source environment variables
    if [[ -f "test/config/env.local" ]]; then
        set -a
        source test/config/env.local
        set +a
    fi
    
    # Run integration tests
    log_info "Running integration tests..."
    make test-integration
    
    # Run e2e tests
    log_info "Running e2e tests..."
    make test-e2e
    
    log_success "All tests completed!"
}

# Deploy samples
samples() {
    log_info "Deploying sample configurations..."
    
    # First, check if we have the env setup
    if [[ ! -f "test/config/env.local" ]]; then
        log_error "Environment not configured. Run: $0 setup"
        return 1
    fi
    
    # Source environment variables
    set -a
    source test/config/env.local
    set +a
    
    # Create a sample OptimismNetwork
    log_info "Creating sample OptimismNetwork..."
    kubectl apply -f - <<EOF
apiVersion: optimism.optimism.io/v1alpha1
kind: OptimismNetwork
metadata:
  name: dev-sepolia
  namespace: default
spec:
  networkName: "op-sepolia"
  chainID: 11155420
  l1ChainID: 11155111
  l1RpcUrl: "${TEST_L1_RPC_URL:-https://sepolia.infura.io/v3/YOUR-API-KEY}"
  l1BeaconUrl: "${TEST_L1_BEACON_URL:-http://localhost:5052}"
  l1RpcTimeout: 10000000000  # 10 seconds in nanoseconds
  rollupConfig:
    autoDiscover: true
  l2Genesis:
    autoDiscover: true
  contractAddresses:
    discoveryMethod: "well-known"
    cacheTimeout: 86400000000000  # 24 hours in nanoseconds
EOF
    
    # Create a sample OpNode replica
    log_info "Creating sample OpNode replica..."
    kubectl apply -f - <<EOF
apiVersion: optimism.optimism.io/v1alpha1
kind: OpNode
metadata:
  name: dev-replica
  namespace: default
spec:
  optimismNetworkRef:
    name: dev-sepolia
    namespace: default
  nodeType: "replica"
  opNode:
    syncMode: "execution-layer"
    p2p:
      enabled: true
      discovery:
        enabled: true
      privateKey:
        generate: true
    rpc:
      enabled: true
      host: "0.0.0.0"
      port: 9545
  opGeth:
    dataDir: "/data/geth"
    syncMode: "snap"
    storage:
      size: "10Gi"
      storageClass: "standard"
      accessMode: "ReadWriteOnce"
    networking:
      http:
        enabled: true
        port: 8545
        apis: ["web3", "eth", "net"]
  resources:
    opNode:
      requests:
        cpu: "100m"
        memory: "256Mi"
      limits:
        cpu: "500m"
        memory: "1Gi"
    opGeth:
      requests:
        cpu: "200m"
        memory: "1Gi"
      limits:
        cpu: "1000m"
        memory: "4Gi"
EOF
    
    log_success "Sample configurations deployed!"
    log_info "Monitor with:"
    echo "  kubectl get optimismnetwork,opnode -n default"
    echo "  kubectl describe opnode dev-replica -n default"
}

# Show operator logs
logs() {
    log_info "Showing operator logs..."
    kubectl logs -n op-stack-operator-system deployment/op-stack-operator-controller-manager -f
}

# Clean up test resources
cleanup() {
    log_info "Cleaning up test resources..."
    
    # Delete test resources in default namespace
    kubectl delete opnode,optimismnetwork --all -n default --ignore-not-found=true
    
    # Delete test resources in operator namespace
    kubectl delete opnode,optimismnetwork --all -n op-stack-operator-system --ignore-not-found=true
    
    log_success "Test resources cleaned up!"
}

# Show status
status() {
    log_info "Cluster and operator status:"
    echo
    
    # Cluster status
    echo "📋 Cluster Status:"
    make kind-status
    echo
    
    # Operator status
    echo "🤖 Operator Status:"
    kubectl get deployment -n op-stack-operator-system
    echo
    
    # CRDs status
    echo "📜 CRDs Status:"
    kubectl get crd | grep optimism.optimism.io
    echo
    
    # Resources status
    echo "🔧 OP Stack Resources:"
    kubectl get optimismnetwork,opnode --all-namespaces
}

# Reset everything
reset() {
    log_warning "This will reset the entire cluster and redeploy the operator"
    read -p "Are you sure? [y/N]: " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        log_info "Reset cancelled"
        return 0
    fi
    
    log_info "Resetting cluster..."
    
    # Undeploy operator
    make undeploy || true
    
    # Uninstall CRDs
    make uninstall || true
    
    # Clean up test resources
    cleanup
    
    # Wait a bit
    sleep 5
    
    # Redeploy
    deploy
    
    log_success "Reset complete!"
}

# Main function
main() {
    if [[ $# -eq 0 ]]; then
        show_usage
        exit 1
    fi
    
    case "$1" in
        setup)
            setup
            ;;
        deploy)
            deploy
            ;;
        test)
            test
            ;;
        samples)
            samples
            ;;
        logs)
            logs
            ;;
        cleanup)
            cleanup
            ;;
        status)
            status
            ;;
        reset)
            reset
            ;;
        *)
            log_error "Unknown command: $1"
            show_usage
            exit 1
            ;;
    esac
}

# Run main function
main "$@"
