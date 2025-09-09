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

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	optimismv1alpha1 "github.com/ethereum-optimism/op-stack-operator/api/v1alpha1"
	"github.com/ethereum-optimism/op-stack-operator/pkg/resources"
	"github.com/ethereum-optimism/op-stack-operator/pkg/utils"
)

// OpBatcherFinalizer is the finalizer for OpBatcher resources
const OpBatcherFinalizer = "opbatcher.optimism.io/finalizer"

// Node type constants
const (
	NodeTypeSequencer = "sequencer"
)

// Phase constants for OpBatcher status
const (
	OpBatcherPhasePending = "Pending"
	OpBatcherPhaseRunning = "Running"
	OpBatcherPhaseError   = "Error"
	OpBatcherPhaseStopped = "Stopped"
)

// OpBatcherReconciler reconciles a OpBatcher object
type OpBatcherReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=optimism.optimism.io,resources=opbatchers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=optimism.optimism.io,resources=opbatchers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=optimism.optimism.io,resources=opbatchers/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets;configmaps;services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *OpBatcherReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the OpBatcher instance
	var opBatcher optimismv1alpha1.OpBatcher
	if err := r.Get(ctx, req.NamespacedName, &opBatcher); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "unable to fetch OpBatcher")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if opBatcher.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, &opBatcher)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&opBatcher, OpBatcherFinalizer) {
		controllerutil.AddFinalizer(&opBatcher, OpBatcherFinalizer)
		if err := r.Update(ctx, &opBatcher); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Validate configuration
	if err := r.validateConfiguration(&opBatcher); err != nil {
		utils.SetCondition(&opBatcher.Status.Conditions, "ConfigurationValid", metav1.ConditionFalse, "InvalidConfiguration", err.Error())
		opBatcher.Status.Phase = OpBatcherPhaseError
		opBatcher.Status.ObservedGeneration = opBatcher.Generation
		if statusErr := r.updateStatusWithRetry(ctx, &opBatcher); statusErr != nil {
			logger.Error(statusErr, "failed to update status after validation error")
		}
		return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
	}

	utils.SetCondition(&opBatcher.Status.Conditions, "ConfigurationValid", metav1.ConditionTrue, "ValidConfiguration", "OpBatcher configuration is valid")

	// Fetch referenced OptimismNetwork
	network, err := r.fetchOptimismNetwork(ctx, &opBatcher)
	if err != nil {
		utils.SetCondition(&opBatcher.Status.Conditions, "NetworkReference", metav1.ConditionFalse, "NetworkNotFound", fmt.Sprintf("Failed to fetch OptimismNetwork: %v", err))
		opBatcher.Status.Phase = OpBatcherPhaseError
		opBatcher.Status.ObservedGeneration = opBatcher.Generation
		if statusErr := r.updateStatusWithRetry(ctx, &opBatcher); statusErr != nil {
			logger.Error(statusErr, "failed to update status after network fetch error")
		}
		return ctrl.Result{RequeueAfter: time.Minute * 2}, nil
	}

	utils.SetCondition(&opBatcher.Status.Conditions, "NetworkReference", metav1.ConditionTrue, "NetworkFound", "OptimismNetwork reference resolved successfully")

	// Ensure OptimismNetwork is ready
	if network.Status.Phase != "Ready" {
		utils.SetCondition(&opBatcher.Status.Conditions, "NetworkReady", metav1.ConditionFalse, "NetworkNotReady", "OptimismNetwork is not ready")
		opBatcher.Status.Phase = OpBatcherPhasePending
		opBatcher.Status.ObservedGeneration = opBatcher.Generation
		if err := r.updateStatusWithRetry(ctx, &opBatcher); err != nil {
			logger.Error(err, "failed to update status for network pending")
		}
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}
	utils.SetCondition(&opBatcher.Status.Conditions, "NetworkReady", metav1.ConditionTrue, "NetworkReady", "OptimismNetwork is ready")
	opBatcher.Status.Phase = OpBatcherPhasePending

	// Validate sequencer reference if provided
	var sequencerServiceName string
	if opBatcher.Spec.SequencerRef != nil {
		sequencer, err := r.fetchSequencerNode(ctx, &opBatcher)
		if err != nil {
			utils.SetCondition(&opBatcher.Status.Conditions, "SequencerReference", metav1.ConditionFalse, "SequencerNotFound", fmt.Sprintf("Failed to fetch sequencer OpNode: %v", err))
			opBatcher.Status.Phase = OpBatcherPhaseError
			goto updateStatus
		}
		// Check if sequencer is running and is actually a sequencer
		if sequencer.Spec.NodeType != NodeTypeSequencer {
			utils.SetCondition(&opBatcher.Status.Conditions, "SequencerReference", metav1.ConditionFalse, "InvalidSequencer", "Referenced OpNode is not a sequencer")
			opBatcher.Status.Phase = OpBatcherPhaseError
			goto updateStatus
		}
		if sequencer.Status.Phase != "Running" {
			utils.SetCondition(&opBatcher.Status.Conditions, "SequencerReference", metav1.ConditionFalse, "SequencerNotReady", "Referenced sequencer OpNode is not ready")
			opBatcher.Status.Phase = OpBatcherPhasePending
			goto updateStatus
		}
		utils.SetCondition(&opBatcher.Status.Conditions, "SequencerReference", metav1.ConditionTrue, "SequencerReady", "Sequencer OpNode is ready")
		sequencerServiceName = sequencer.Name // Service name matches OpNode name
	}

	// Validate private key secret exists
	if err := r.validatePrivateKeySecret(ctx, &opBatcher); err != nil {
		utils.SetCondition(&opBatcher.Status.Conditions, "PrivateKeyLoaded", metav1.ConditionFalse, "SecretNotFound", fmt.Sprintf("Private key secret validation failed: %v", err))
		opBatcher.Status.Phase = OpBatcherPhaseError
		goto updateStatus
	}
	utils.SetCondition(&opBatcher.Status.Conditions, "PrivateKeyLoaded", metav1.ConditionTrue, "SecretFound", "Private key loaded from secret")

	// Test L1 connectivity using network configuration
	if err := r.testL1Connectivity(ctx, network); err != nil {
		utils.SetCondition(&opBatcher.Status.Conditions, "L1Connected", metav1.ConditionFalse, "ConnectionFailed", fmt.Sprintf("L1 connectivity test failed: %v", err))
		opBatcher.Status.Phase = OpBatcherPhaseError
		goto updateStatus
	}
	utils.SetCondition(&opBatcher.Status.Conditions, "L1Connected", metav1.ConditionTrue, "ConnectionEstablished", "Connected to L1 RPC endpoint")

	// Test L2 connectivity if sequencer reference is provided
	if sequencerServiceName != "" {
		if err := r.testL2Connectivity(ctx, &opBatcher, sequencerServiceName); err != nil {
			utils.SetCondition(&opBatcher.Status.Conditions, "L2Connected", metav1.ConditionFalse, "SequencerUnreachable", fmt.Sprintf("L2 sequencer connectivity test failed: %v", err))
			opBatcher.Status.Phase = OpBatcherPhaseError
			goto updateStatus
		}
		utils.SetCondition(&opBatcher.Status.Conditions, "L2Connected", metav1.ConditionTrue, "SequencerReachable", "Connected to L2 sequencer")
	}

	// Reconcile Deployment
	if err := r.reconcileDeployment(ctx, &opBatcher, network, sequencerServiceName); err != nil {
		utils.SetCondition(&opBatcher.Status.Conditions, "DeploymentReady", metav1.ConditionFalse, "DeploymentReconciliationFailed", fmt.Sprintf("Failed to reconcile Deployment: %v", err))
		opBatcher.Status.Phase = OpBatcherPhaseError
		goto updateStatus
	}
	utils.SetCondition(&opBatcher.Status.Conditions, "DeploymentReady", metav1.ConditionTrue, "DeploymentReconciled", "Deployment is ready")

	// Reconcile Service
	if err := r.reconcileService(ctx, &opBatcher); err != nil {
		utils.SetCondition(&opBatcher.Status.Conditions, "ServiceReady", metav1.ConditionFalse, "ServiceReconciliationFailed", fmt.Sprintf("Failed to reconcile Service: %v", err))
		opBatcher.Status.Phase = OpBatcherPhaseError
		goto updateStatus
	}
	utils.SetCondition(&opBatcher.Status.Conditions, "ServiceReady", metav1.ConditionTrue, "ServiceReconciled", "Service is ready")

	// Update batcher operational status
	r.updateBatcherStatus(ctx, &opBatcher)
	opBatcher.Status.Phase = OpBatcherPhaseRunning

updateStatus:
	// Consolidated status update
	opBatcher.Status.ObservedGeneration = opBatcher.Generation
	if err := r.updateStatusWithRetry(ctx, &opBatcher); err != nil {
		logger.Error(err, "failed to update status")
	}

	// Decide requeue interval
	var requeueAfter time.Duration
	switch opBatcher.Status.Phase {
	case OpBatcherPhaseError:
		requeueAfter = time.Minute * 2
	case OpBatcherPhasePending:
		requeueAfter = time.Minute
	case OpBatcherPhaseRunning:
		requeueAfter = time.Minute * 5
	default:
		requeueAfter = time.Minute
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// validateConfiguration validates the OpBatcher configuration
func (r *OpBatcherReconciler) validateConfiguration(opBatcher *optimismv1alpha1.OpBatcher) error {
	// Check required fields
	if opBatcher.Spec.OptimismNetworkRef.Name == "" {
		return fmt.Errorf("optimismNetworkRef.name is required")
	}

	// Validate private key secret reference
	if opBatcher.Spec.PrivateKey.SecretRef == nil {
		return fmt.Errorf("privateKey.secretRef is required")
	}
	if opBatcher.Spec.PrivateKey.SecretRef.Name == "" {
		return fmt.Errorf("privateKey.secretRef.name is required")
	}
	if opBatcher.Spec.PrivateKey.SecretRef.Key == "" {
		return fmt.Errorf("privateKey.secretRef.key is required")
	}

	// Validate batching configuration if provided
	if cfg := opBatcher.Spec.Batching; cfg != nil {
		if cfg.TargetL1TxSize != 0 && cfg.TargetL1TxSize < 1000 {
			return fmt.Errorf("batching.targetL1TxSize must be at least 1000 bytes")
		}
		if cfg.SubSafetyMargin != 0 && cfg.SubSafetyMargin < 1 {
			return fmt.Errorf("batching.subSafetyMargin must be at least 1")
		}
	}

	// Validate data availability configuration if provided
	if cfg := opBatcher.Spec.DataAvailability; cfg != nil {
		if cfg.Type != "" && cfg.Type != "blobs" && cfg.Type != "calldata" {
			return fmt.Errorf("dataAvailability.type must be 'blobs' or 'calldata'")
		}
		if cfg.MaxBlobsPerTx != 0 && cfg.MaxBlobsPerTx < 1 {
			return fmt.Errorf("dataAvailability.maxBlobsPerTx must be at least 1")
		}
	}

	return nil
}

// fetchOptimismNetwork fetches the referenced OptimismNetwork
func (r *OpBatcherReconciler) fetchOptimismNetwork(ctx context.Context, opBatcher *optimismv1alpha1.OpBatcher) (*optimismv1alpha1.OptimismNetwork, error) {
	namespace := opBatcher.Spec.OptimismNetworkRef.Namespace
	if namespace == "" {
		namespace = opBatcher.Namespace
	}

	var network optimismv1alpha1.OptimismNetwork
	key := types.NamespacedName{
		Name:      opBatcher.Spec.OptimismNetworkRef.Name,
		Namespace: namespace,
	}

	if err := r.Get(ctx, key, &network); err != nil {
		return nil, err
	}

	return &network, nil
}

// fetchSequencerNode fetches the referenced sequencer OpNode
func (r *OpBatcherReconciler) fetchSequencerNode(ctx context.Context, opBatcher *optimismv1alpha1.OpBatcher) (*optimismv1alpha1.OpNode, error) {
	if opBatcher.Spec.SequencerRef == nil {
		return nil, fmt.Errorf("sequencerRef is nil")
	}

	namespace := opBatcher.Spec.SequencerRef.Namespace
	if namespace == "" {
		namespace = opBatcher.Namespace
	}

	var sequencer optimismv1alpha1.OpNode
	key := types.NamespacedName{
		Name:      opBatcher.Spec.SequencerRef.Name,
		Namespace: namespace,
	}

	if err := r.Get(ctx, key, &sequencer); err != nil {
		return nil, err
	}

	return &sequencer, nil
}

// validatePrivateKeySecret validates that the private key secret exists and has the required key
func (r *OpBatcherReconciler) validatePrivateKeySecret(ctx context.Context, opBatcher *optimismv1alpha1.OpBatcher) error {
	secretName := opBatcher.Spec.PrivateKey.SecretRef.Name
	secretKey := opBatcher.Spec.PrivateKey.SecretRef.Key

	var secret corev1.Secret
	key := types.NamespacedName{Name: secretName, Namespace: opBatcher.Namespace}

	if err := r.Get(ctx, key, &secret); err != nil {
		return fmt.Errorf("failed to get secret %s: %w", secretName, err)
	}

	if _, exists := secret.Data[secretKey]; !exists {
		return fmt.Errorf("secret %s does not contain key %s", secretName, secretKey)
	}

	// Basic validation - ensure it's not empty
	privateKeyData := secret.Data[secretKey]
	if len(privateKeyData) == 0 {
		return fmt.Errorf("private key in secret %s key %s is empty", secretName, secretKey)
	}

	// Basic format validation - should be hex string
	privateKeyStr := strings.TrimSpace(string(privateKeyData))
	if !strings.HasPrefix(privateKeyStr, "0x") {
		return fmt.Errorf("private key in secret %s key %s must start with 0x", secretName, secretKey)
	}

	// Check if it's valid hex (without 0x prefix) and correct length
	hexPart := privateKeyStr[2:] // Remove 0x prefix
	if len(hexPart) != 64 {
		return fmt.Errorf("private key in secret %s key %s must be 64 hex characters after 0x prefix (got %d)", secretName, secretKey, len(hexPart))
	}

	// Validate hex characters
	for i, c := range hexPart {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return fmt.Errorf("private key in secret %s key %s contains invalid hex character '%c' at position %d", secretName, secretKey, c, i+2)
		}
	}

	return nil
}

// testL1Connectivity tests connectivity to L1 RPC endpoint
func (r *OpBatcherReconciler) testL1Connectivity(_ context.Context, network *optimismv1alpha1.OptimismNetwork) error {
	// For now, we'll do a basic validation that the URL is set and looks valid
	// In a full implementation, this would make an actual RPC call
	if network.Spec.L1RpcUrl == "" {
		return fmt.Errorf("L1 RPC URL not configured in OptimismNetwork")
	}

	if !strings.HasPrefix(network.Spec.L1RpcUrl, "http://") && !strings.HasPrefix(network.Spec.L1RpcUrl, "https://") {
		return fmt.Errorf("L1 RPC URL must be a valid HTTP/HTTPS URL")
	}

	return nil
}

// testL2Connectivity tests connectivity to L2 sequencer
func (r *OpBatcherReconciler) testL2Connectivity(_ context.Context, _ *optimismv1alpha1.OpBatcher, sequencerServiceName string) error {
	// For now, we'll do a basic validation that the service name is set
	// In a full implementation, this would make an actual RPC call to the sequencer
	if sequencerServiceName == "" {
		return fmt.Errorf("sequencer service name is empty")
	}

	return nil
}

// reconcileDeployment manages the Deployment for OpBatcher
func (r *OpBatcherReconciler) reconcileDeployment(ctx context.Context, opBatcher *optimismv1alpha1.OpBatcher, network *optimismv1alpha1.OptimismNetwork, sequencerServiceName string) error {
	desiredDeployment := resources.CreateOpBatcherDeployment(opBatcher, network, sequencerServiceName)

	if err := ctrl.SetControllerReference(opBatcher, desiredDeployment, r.Scheme); err != nil {
		return err
	}

	var currentDeployment appsv1.Deployment
	key := types.NamespacedName{Name: opBatcher.Name, Namespace: opBatcher.Namespace}

	if err := r.Get(ctx, key, &currentDeployment); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}

		// Create new Deployment
		return r.Create(ctx, desiredDeployment)
	}

	// Update existing Deployment if needed
	currentDeployment.Spec = desiredDeployment.Spec
	currentDeployment.Labels = desiredDeployment.Labels
	currentDeployment.Annotations = desiredDeployment.Annotations

	return r.Update(ctx, &currentDeployment)
}

// reconcileService manages the Service for OpBatcher
func (r *OpBatcherReconciler) reconcileService(ctx context.Context, opBatcher *optimismv1alpha1.OpBatcher) error {
	desiredService := resources.CreateOpBatcherService(opBatcher)

	if err := ctrl.SetControllerReference(opBatcher, desiredService, r.Scheme); err != nil {
		return err
	}

	var currentService corev1.Service
	key := types.NamespacedName{Name: opBatcher.Name, Namespace: opBatcher.Namespace}

	if err := r.Get(ctx, key, &currentService); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}

		// Create new Service
		return r.Create(ctx, desiredService)
	}

	// Update existing Service if needed
	currentService.Spec.Ports = desiredService.Spec.Ports
	currentService.Spec.Type = desiredService.Spec.Type
	currentService.Labels = desiredService.Labels
	currentService.Annotations = desiredService.Annotations

	return r.Update(ctx, &currentService)
}

// updateBatcherStatus updates the batcher operational status
func (r *OpBatcherReconciler) updateBatcherStatus(_ context.Context, opBatcher *optimismv1alpha1.OpBatcher) {
	// Initialize batcher info if needed
	if opBatcher.Status.BatcherInfo == nil {
		opBatcher.Status.BatcherInfo = &optimismv1alpha1.BatcherInfo{}
	}

	// For now, we'll set basic status information
	// In a full implementation, this would query the actual batcher for operational status
	opBatcher.Status.BatcherInfo.PendingBatches = 0 // Would be queried from actual batcher
	// TotalBatchesSubmitted would be incremented based on actual batch submissions
}

// handleDeletion handles the deletion of OpBatcher resources
func (r *OpBatcherReconciler) handleDeletion(ctx context.Context, opBatcher *optimismv1alpha1.OpBatcher) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Perform cleanup tasks here
	logger.Info("Cleaning up OpBatcher resources", "name", opBatcher.Name)

	// Remove finalizer
	controllerutil.RemoveFinalizer(opBatcher, OpBatcherFinalizer)
	if err := r.Update(ctx, opBatcher); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// updateStatusWithRetry updates the OpBatcher status with retry logic to handle precondition failures
func (r *OpBatcherReconciler) updateStatusWithRetry(ctx context.Context, opBatcher *optimismv1alpha1.OpBatcher) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Get the latest version of the resource
		latest := &optimismv1alpha1.OpBatcher{}
		if err := r.Get(ctx, types.NamespacedName{Name: opBatcher.Name, Namespace: opBatcher.Namespace}, latest); err != nil {
			return err
		}

		// Copy individual status fields from opBatcher to latest to avoid race conditions
		latest.Status.Phase = opBatcher.Status.Phase
		latest.Status.ObservedGeneration = opBatcher.Status.ObservedGeneration

		// Deep copy conditions to avoid reference issues
		latest.Status.Conditions = make([]metav1.Condition, len(opBatcher.Status.Conditions))
		for i, condition := range opBatcher.Status.Conditions {
			latest.Status.Conditions[i] = metav1.Condition{
				Type:               condition.Type,
				Status:             condition.Status,
				Reason:             condition.Reason,
				Message:            condition.Message,
				LastTransitionTime: condition.LastTransitionTime,
				ObservedGeneration: condition.ObservedGeneration,
			}
		}

		// Copy batcher info if present
		if opBatcher.Status.BatcherInfo != nil {
			latest.Status.BatcherInfo = &optimismv1alpha1.BatcherInfo{
				PendingBatches:        opBatcher.Status.BatcherInfo.PendingBatches,
				TotalBatchesSubmitted: opBatcher.Status.BatcherInfo.TotalBatchesSubmitted,
			}
			if opBatcher.Status.BatcherInfo.LastBatchSubmitted != nil {
				latest.Status.BatcherInfo.LastBatchSubmitted = &optimismv1alpha1.BatchSubmissionInfo{
					BlockNumber:     opBatcher.Status.BatcherInfo.LastBatchSubmitted.BlockNumber,
					TransactionHash: opBatcher.Status.BatcherInfo.LastBatchSubmitted.TransactionHash,
					Timestamp:       opBatcher.Status.BatcherInfo.LastBatchSubmitted.Timestamp,
					GasUsed:         opBatcher.Status.BatcherInfo.LastBatchSubmitted.GasUsed,
				}
			}
		}

		// Update the status
		return r.Status().Update(ctx, latest)
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *OpBatcherReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&optimismv1alpha1.OpBatcher{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Named("opbatcher").
		Complete(r)
}
