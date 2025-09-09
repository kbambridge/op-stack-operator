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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// OpBatcherSpec defines the desired state of OpBatcher
type OpBatcherSpec struct {
	// OptimismNetworkRef references the OptimismNetwork for this batcher
	OptimismNetworkRef OptimismNetworkRef `json:"optimismNetworkRef"`

	// SequencerRef references the sequencer OpNode instance for L2 connectivity
	SequencerRef *SequencerReference `json:"sequencerRef,omitempty"`

	// PrivateKey for L1 transaction signing
	PrivateKey SecretKeyRef `json:"privateKey"`

	// Batching configuration
	Batching *BatchingConfig `json:"batching,omitempty"`

	// Data availability configuration
	DataAvailability *DataAvailabilityConfig `json:"dataAvailability,omitempty"`

	// Throttling configuration
	Throttling *ThrottlingConfig `json:"throttling,omitempty"`

	// L1 transaction management
	L1Transaction *L1TransactionConfig `json:"l1Transaction,omitempty"`

	// RPC configuration
	RPC *RPCConfig `json:"rpc,omitempty"`

	// Metrics configuration
	Metrics *MetricsConfig `json:"metrics,omitempty"`

	// Resources defines resource requirements
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Service configuration
	Service *ServiceConfig `json:"service,omitempty"`
}

// BatchingConfig defines batching behavior configuration
type BatchingConfig struct {
	// MaxChannelDuration is the maximum duration for a channel
	// +kubebuilder:default="10m"
	MaxChannelDuration string `json:"maxChannelDuration,omitempty"`

	// SubSafetyMargin is the safety margin for L1 confirmations
	// +kubebuilder:default=10
	SubSafetyMargin int32 `json:"subSafetyMargin,omitempty"`

	// TargetL1TxSize is the target size for L1 transactions in bytes
	// +kubebuilder:default=120000
	TargetL1TxSize int32 `json:"targetL1TxSize,omitempty"`

	// TargetNumFrames is the target number of frames per transaction
	// +kubebuilder:default=1
	TargetNumFrames int32 `json:"targetNumFrames,omitempty"`

	// ApproxComprRatio is the approximate compression ratio
	// +kubebuilder:default="0.4"
	ApproxComprRatio string `json:"approxComprRatio,omitempty"`
}

// DataAvailabilityConfig defines data availability settings
type DataAvailabilityConfig struct {
	// Type specifies the data availability type
	// +kubebuilder:validation:Enum=blobs;calldata
	// +kubebuilder:default="blobs"
	Type string `json:"type,omitempty"`

	// MaxBlobsPerTx is the maximum blobs per transaction for EIP-4844
	// +kubebuilder:default=6
	MaxBlobsPerTx int32 `json:"maxBlobsPerTx,omitempty"`
}

// ThrottlingConfig defines throttling behavior
type ThrottlingConfig struct {
	// Enabled determines if throttling is active
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// MaxPendingTx is the maximum pending transactions
	// +kubebuilder:default=10
	MaxPendingTx int32 `json:"maxPendingTx,omitempty"`

	// BacklogSafetyMargin is the safety margin for backlog
	// +kubebuilder:default=10
	BacklogSafetyMargin int32 `json:"backlogSafetyMargin,omitempty"`
}

// L1TransactionConfig defines L1 transaction management settings
type L1TransactionConfig struct {
	// FeeLimitMultiplier is the fee limit multiplier for dynamic fees
	// +kubebuilder:default="5"
	FeeLimitMultiplier string `json:"feeLimitMultiplier,omitempty"`

	// ResubmissionTimeout is the timeout before resubmitting transaction
	// +kubebuilder:default="48s"
	ResubmissionTimeout string `json:"resubmissionTimeout,omitempty"`

	// NumConfirmations is the number of confirmations to wait
	// +kubebuilder:default=10
	NumConfirmations int32 `json:"numConfirmations,omitempty"`

	// SafeAbortNonceTooLowCount is the abort threshold for nonce too low errors
	// +kubebuilder:default=3
	SafeAbortNonceTooLowCount int32 `json:"safeAbortNonceTooLowCount,omitempty"`
}

// OpBatcherStatus defines the observed state of OpBatcher
type OpBatcherStatus struct {
	// Phase represents the overall state of the OpBatcher
	Phase string `json:"phase,omitempty"` // Pending, Running, Error, Stopped

	// Conditions represent detailed status conditions
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration reflects the generation of the most recently observed spec
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// BatcherInfo contains operational information about the batcher
	BatcherInfo *BatcherInfo `json:"batcherInfo,omitempty"`
}

// BatcherInfo contains operational information about the running batcher
type BatcherInfo struct {
	// LastBatchSubmitted contains information about the last submitted batch
	LastBatchSubmitted *BatchSubmissionInfo `json:"lastBatchSubmitted,omitempty"`

	// PendingBatches is the number of pending batches
	PendingBatches int32 `json:"pendingBatches,omitempty"`

	// TotalBatchesSubmitted is the total number of batches submitted
	TotalBatchesSubmitted int64 `json:"totalBatchesSubmitted,omitempty"`
}

// BatchSubmissionInfo contains information about a batch submission
type BatchSubmissionInfo struct {
	// BlockNumber is the L2 block number of the batch
	BlockNumber int64 `json:"blockNumber,omitempty"`

	// TransactionHash is the L1 transaction hash
	TransactionHash string `json:"transactionHash,omitempty"`

	// Timestamp is when the batch was submitted
	Timestamp metav1.Time `json:"timestamp,omitempty"`

	// GasUsed is the gas used for the transaction
	GasUsed int64 `json:"gasUsed,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Network",type=string,JSONPath=`.spec.optimismNetworkRef.name`
// +kubebuilder:printcolumn:name="Sequencer",type=string,JSONPath=`.spec.sequencerRef.name`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Batches",type=integer,JSONPath=`.status.batcherInfo.totalBatchesSubmitted`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// OpBatcher is the Schema for the opbatchers API
type OpBatcher struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OpBatcherSpec   `json:"spec,omitempty"`
	Status OpBatcherStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OpBatcherList contains a list of OpBatcher
type OpBatcherList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OpBatcher `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OpBatcher{}, &OpBatcherList{})
}
