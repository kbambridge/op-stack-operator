/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// OptimismNetworkSpec defines the desired state of OptimismNetwork
type OptimismNetworkSpec struct {
	// Network Configuration
	NetworkName string `json:"networkName,omitempty"`
	ChainID     int64  `json:"chainID"`
	L1ChainID   int64  `json:"l1ChainID"`

	// L1 RPC Configuration (required by all components)
	L1RpcUrl     string        `json:"l1RpcUrl"`
	L1BeaconUrl  string        `json:"l1BeaconUrl,omitempty"`
	L1RpcTimeout time.Duration `json:"l1RpcTimeout,omitempty"`

	// Network-specific Configuration Files
	RollupConfig *ConfigSource `json:"rollupConfig,omitempty"`
	L2Genesis    *ConfigSource `json:"l2Genesis,omitempty"`

	// Contract Address Discovery
	ContractAddresses *ContractAddressConfig `json:"contractAddresses,omitempty"`

	// Shared Configuration
	SharedConfig *SharedConfig `json:"sharedConfig,omitempty"`
}

// ConfigSource defines how configuration data is provided
type ConfigSource struct {
	Inline       string                       `json:"inline,omitempty"`
	ConfigMapRef *corev1.ConfigMapKeySelector `json:"configMapRef,omitempty"`
	AutoDiscover bool                         `json:"autoDiscover,omitempty"`
}

// ContractAddressConfig defines contract address discovery configuration
type ContractAddressConfig struct {
	// L1 Contract Addresses (optional - helps with discovery)
	SystemConfigAddr       string `json:"systemConfigAddr,omitempty"`
	L2OutputOracleAddr     string `json:"l2OutputOracleAddr,omitempty"`
	DisputeGameFactoryAddr string `json:"disputeGameFactoryAddr,omitempty"`
	OptimismPortalAddr     string `json:"optimismPortalAddr,omitempty"`

	// Discovery configuration
	DiscoveryMethod string        `json:"discoveryMethod,omitempty"` // auto, superchain-registry, well-known, manual
	CacheTimeout    time.Duration `json:"cacheTimeout,omitempty"`
}

// SharedConfig defines configuration shared across all components
type SharedConfig struct {
	// Logging
	Logging *LoggingConfig `json:"logging,omitempty"`

	// Metrics
	Metrics *MetricsConfig `json:"metrics,omitempty"`

	// Resource Defaults
	Resources *ResourceConfig `json:"resources,omitempty"`

	// Security
	Security *SecurityConfig `json:"security,omitempty"`
}

// LoggingConfig defines logging configuration
type LoggingConfig struct {
	Level  string `json:"level,omitempty"`  // trace, debug, info, warn, error
	Format string `json:"format,omitempty"` // logfmt, json
	Color  bool   `json:"color,omitempty"`
}

// MetricsConfig defines metrics configuration
type MetricsConfig struct {
	Enabled bool   `json:"enabled,omitempty"`
	Host    string `json:"host,omitempty"`
	Port    int32  `json:"port,omitempty"`
	Path    string `json:"path,omitempty"`
}

// ResourceConfig defines default resource requirements
type ResourceConfig struct {
	Requests corev1.ResourceList `json:"requests,omitempty"`
	Limits   corev1.ResourceList `json:"limits,omitempty"`
}

// SecurityConfig defines security configuration
type SecurityConfig struct {
	RunAsNonRoot   *bool                  `json:"runAsNonRoot,omitempty"`
	RunAsUser      *int64                 `json:"runAsUser,omitempty"`
	FSGroup        *int64                 `json:"fsGroup,omitempty"`
	SeccompProfile *corev1.SeccompProfile `json:"seccompProfile,omitempty"`
}

// OptimismNetworkStatus defines the observed state of OptimismNetwork
type OptimismNetworkStatus struct {
	// Phase represents the overall state of the network configuration
	Phase string `json:"phase,omitempty"` // Pending, Ready, Error

	// Conditions represent detailed status conditions
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration reflects the generation of the most recently observed spec
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// NetworkInfo contains discovered network information
	NetworkInfo *NetworkInfo `json:"networkInfo,omitempty"`
}

// NetworkInfo contains discovered network information and contract addresses
type NetworkInfo struct {
	DeploymentTimestamp metav1.Time `json:"deploymentTimestamp,omitempty"`
	LastUpdated         metav1.Time `json:"lastUpdated,omitempty"`

	// Discovered contract addresses (populated by controller)
	DiscoveredContracts *NetworkContractAddresses `json:"discoveredContracts,omitempty"`
}

// NetworkContractAddresses contains all discovered contract addresses
type NetworkContractAddresses struct {
	// L1 Contracts
	L2OutputOracleAddr         string `json:"l2OutputOracleAddr,omitempty"`
	DisputeGameFactoryAddr     string `json:"disputeGameFactoryAddr,omitempty"`
	OptimismPortalAddr         string `json:"optimismPortalAddr,omitempty"`
	SystemConfigAddr           string `json:"systemConfigAddr,omitempty"`
	L1CrossDomainMessengerAddr string `json:"l1CrossDomainMessengerAddr,omitempty"`
	L1StandardBridgeAddr       string `json:"l1StandardBridgeAddr,omitempty"`

	// L2 Contracts (predeploys - same across all OP Stack chains)
	L2CrossDomainMessengerAddr string `json:"l2CrossDomainMessengerAddr,omitempty"`
	L2StandardBridgeAddr       string `json:"l2StandardBridgeAddr,omitempty"`
	L2ToL1MessagePasserAddr    string `json:"l2ToL1MessagePasserAddr,omitempty"`

	// Discovery metadata
	LastDiscoveryTime metav1.Time `json:"lastDiscoveryTime,omitempty"`
	DiscoveryMethod   string      `json:"discoveryMethod,omitempty"` // system-config, superchain-registry, well-known
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Network",type=string,JSONPath=`.spec.networkName`
// +kubebuilder:printcolumn:name="ChainID",type=integer,JSONPath=`.spec.chainID`
// +kubebuilder:printcolumn:name="L1ChainID",type=integer,JSONPath=`.spec.l1ChainID`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// OptimismNetwork is the Schema for the optimismnetworks API
type OptimismNetwork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OptimismNetworkSpec   `json:"spec,omitempty"`
	Status OptimismNetworkStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OptimismNetworkList contains a list of OptimismNetwork
type OptimismNetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OptimismNetwork `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OptimismNetwork{}, &OptimismNetworkList{})
}
