# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Architecture Overview

This is a Kubernetes Operator built with Kubebuilder for managing OP Stack-based Layer 2 rollup chains. The operator uses a multi-CRD approach with separate controllers for different OP Stack components:

### Custom Resource Definitions (CRDs)
- **OptimismNetwork**: Central configuration resource defining network-wide parameters, L1 connectivity, and contract addresses
- **OpNode**: Manages op-node (consensus layer) paired with op-geth (execution layer) for both sequencer and replica configurations
- **OpBatcher**: Manages op-batcher instances for L2 transaction batch submission to L1
- **OpProposer**: Manages op-proposer instances for L2 output root proposals to L1
- **OpChallenger**: Manages op-challenger instances for monitoring and participating in dispute games

### Controller Structure
Controllers are located in `internal/controller/` with each CRD having its dedicated controller:
- `optimismnetwork_controller.go` - Handles network configuration, contract discovery, and shared resources
- `opnode_controller.go` - Manages consensus/execution layer deployments
- `opbatcher_controller.go` - Handles batch submission services
- `opproposer_controller.go` - Manages output proposal services  
- `opchallenger_controller.go` - Handles dispute monitoring services

### Key Design Patterns
- **Configuration Inheritance**: OptimismNetwork provides shared config consumed by other components
- **Service Discovery**: L2 connectivity handled through Kubernetes service discovery, not centralized config
- **Container Co-location**: op-node and op-geth run in the same pod for simplified networking
- **Contract Discovery**: Automatic discovery of L1 contract addresses from multiple sources

## Common Development Commands

### Building and Testing
```bash
# Build the manager binary
make build

# Generate code and manifests
make generate-all  # Alias for: manifests generate fmt vet

# Run tests
make test          # All tests (unit + integration)
make test-unit     # Unit tests only
make test-integration-with-env  # Integration tests with environment

# Set up test environment
make setup-test-env

# Linting
make lint          # Run golangci-lint
make lint-fix      # Run golangci-lint with fixes
```

### Docker and Kubernetes
```bash
# Build and push Docker image
make docker-build docker-push IMG=<registry>/op-stack-operator:tag

# Install/Deploy to cluster
make install       # Install CRDs
make deploy IMG=<registry>/op-stack-operator:tag

# Uninstall from cluster  
make uninstall     # Remove CRDs
make undeploy      # Remove controller

# Kind cluster operations
make kind-load     # Load image into kind cluster
```

### Development Tools
```bash
# Run controller locally
make run

# Deploy sample configurations
make deploy-samples

# Build installer YAML bundle
make build-installer IMG=<registry>/op-stack-operator:tag
```

## Testing Setup

The project uses a comprehensive testing approach following Kubebuilder best practices:

### Unit Tests (Recommended for Complex Controllers)
- Located alongside controller files (`*_test.go`)
- Use envtest for Kubernetes API integration without controller managers
- **Key Pattern**: Controllers with external dependencies (L1 RPC, complex validation) should use unit tests
- Run with: `make test-unit`

#### Unit Testing Best Practices:
```go
// 1. Test reconciler directly, not through controller manager
controllerReconciler := &YourReconciler{
    Client: k8sClient,
    Scheme: k8sClient.Scheme(),
}

// 2. Always call reconcile twice to handle finalizer pattern
req := reconcile.Request{NamespacedName: types.NamespacedName{...}}
result, err := controllerReconciler.Reconcile(ctx, req) // Adds finalizer
result, err = controllerReconciler.Reconcile(ctx, req)  // Main logic

// 3. Manually set dependency status (no controller interference)
dependency.Status = DependencyStatus{Phase: "Ready", Conditions: [...]}
Expect(k8sClient.Status().Update(ctx, dependency)).To(Succeed())

// 4. Handle cleanup properly in DeferCleanup
DeferCleanup(func() {
    // Trigger reconciliation for deletion to remove finalizers
    testReconciler.Reconcile(ctx, reconcile.Request{...})
    Eventually(func() bool {
        return apierrors.IsNotFound(k8sClient.Get(ctx, key, resource))
    }).Should(BeTrue())
})
```

### Integration Tests  
- Located in `test/integration/`
- **Key Pattern**: Test real controller behavior with actual external dependencies
- **All controllers run simultaneously** in test environment with controller manager
- **Natural resource interactions** - controllers automatically respond to changes
- **External dependencies**: Require real L1 RPC via `TEST_L1_RPC_URL` environment variable
- Set up test environment: `make setup-test-env`
- Load environment: `export $(cat test/config/env.local | xargs)`
- Run with: `make test-integration-with-env`

#### Integration Testing Patterns:
```go
// 1. Create resources and wait for controllers to process them
optimismNetwork := &optimismv1alpha1.OptimismNetwork{
    Spec: optimismv1alpha1.OptimismNetworkSpec{
        L1RpcUrl: testL1RpcUrl, // Real RPC endpoint required
    },
}
k8sClient.Create(ctx, optimismNetwork)

// 2. Wait for natural controller reconciliation
Eventually(func() bool {
    k8sClient.Get(ctx, key, &optimismNetwork)
    return optimismNetwork.Status.Phase == "Ready" // Set by controller
}).Should(BeTrue())

// 3. Test real external connectivity and validation
// Controllers perform actual L1 RPC calls, contract discovery, etc.
```

### E2E Tests
- Use Kind cluster for isolated testing
- Located in `test/e2e/`
- Require Kind cluster to be running
- Run with: `make test-e2e`

### Test Suite Configuration (suite_test.go)
```go
// For unit testing: Don't register controllers in BeforeSuite
By("setting up controllers")
// Note: Only register controllers needed for integration tests
// Complex controllers with external dependencies should use unit tests instead

// For integration testing: Register specific controllers only
err = (&SimpleControllerReconciler{
    Client: mgr.GetClient(),
    Scheme: mgr.GetScheme(),
}).SetupWithManager(mgr)
```

### Choosing Test Approach:

| Test Type | When to Use | What it Tests | Environment |
|-----------|-------------|---------------|-------------|
| **Unit Tests** | Complex controllers, external dependencies, fast feedback | Controller logic, validation, error handling, resource creation | Mock/controlled dependencies |
| **Integration Tests** | Real external connectivity, controller interactions | Actual L1 RPC calls, contract discovery, cross-controller workflows | Real external services required |
| **E2E Tests** | Full system validation, deployment scenarios | Complete workflows, Kubernetes deployment, operator lifecycle | Complete cluster environment |

**Current Project Usage:**
- **Unit Tests** (`internal/controller/*_test.go`): OpBatcher, OpProposer, OpChallenger controllers
- **Integration Tests** (`test/integration/*_test.go`): OptimismNetwork, OpNode controllers  
- **E2E Tests** (`test/e2e/*`): Full operator deployment and workflow validation

### Test Validation Patterns:

#### Essential Controller Functionality Tests:
```go
// Test happy path - all dependencies ready
It("should successfully reconcile the resource", func() {
    // Validates: full reconciliation, status conditions, phase transitions
})

// Test resource creation
It("should create a Deployment", func() {
    // Validates: Kubernetes resource creation, ownership, labels
})

It("should create a Service", func() {
    // Validates: Service configuration, selectors, ports
})

// Test error handling
It("should handle validation errors gracefully", func() {
    // Validates: error conditions, graceful degradation, no crashes
})

It("should handle missing dependencies gracefully", func() {
    // Validates: dependency resolution, appropriate error states
})

// Test dependency waiting
It("should wait for dependencies to be ready", func() {
    // Validates: pending states, condition management, requeue logic
})
```

#### Status Condition Validation:
Controllers should properly manage status conditions for observability:
- `ConfigurationValid` - Configuration validation results
- `NetworkReference` - Dependency resolution status  
- `NetworkReady` - Dependency readiness state
- `DeploymentReady` - Kubernetes resource creation status
- `ServiceReady` - Service availability status
- Domain-specific conditions (e.g., `L1Connected`, `L2Connected`)

### Prerequisites
- Go 1.23.0+
- Docker
- kubectl
- Kind (for e2e testing)
- Access to Kubernetes cluster

## Code Generation

The project uses Kubebuilder for scaffolding and code generation:
- API types in `api/v1alpha1/`
- Generated code in `api/v1alpha1/zz_generated.deepcopy.go`
- CRD manifests in `config/crd/bases/`
- RBAC in `config/rbac/`

Run `make generate-all` after modifying API types or adding controller methods.

## Development Guidelines

- Follow Kubebuilder patterns and conventions
- Ensure all new code has corresponding tests
- Use `make lint` to verify code style
- Reference the comprehensive SPEC.md for detailed architecture
- Track development progress in PROGRESS.md
- Validate against latest Optimism specifications

## Project Structure

```
├── api/v1alpha1/           # CRD definitions and types
├── internal/controller/    # Controller implementations
├── config/                 # Kubernetes manifests (CRDs, RBAC, etc.)
├── test/                   # Integration and e2e tests
├── cmd/                    # Main application entry point
└── docs/                   # Documentation
```