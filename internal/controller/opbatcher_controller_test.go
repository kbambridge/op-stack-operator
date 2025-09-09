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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	optimismv1alpha1 "github.com/ethereum-optimism/op-stack-operator/api/v1alpha1"
)

const (
	timeout  = time.Second * 10
	interval = time.Millisecond * 250
)

var _ = Describe("OpBatcher Controller", func() {
	Context("When reconciling a resource", func() {
		var (
			resourceName  string
			networkName   string
			sequencerName string
			secretName    string
		)

		ctx := context.Background()

		BeforeEach(func() {
			// Use unique names for each test to avoid conflicts
			uniqueID := time.Now().UnixNano()
			resourceName = fmt.Sprintf("test-opbatcher-%d", uniqueID)
			networkName = fmt.Sprintf("test-network-%d", uniqueID)
			sequencerName = fmt.Sprintf("test-sequencer-%d", uniqueID)
			secretName = fmt.Sprintf("batcher-private-key-%d", uniqueID)

			By("Creating the OptimismNetwork")
			network := &optimismv1alpha1.OptimismNetwork{
				ObjectMeta: metav1.ObjectMeta{
					Name:      networkName,
					Namespace: "default",
				},
				Spec: optimismv1alpha1.OptimismNetworkSpec{
					NetworkName: "test-network",
					ChainID:     10,
					L1ChainID:   1,
					L1RpcUrl:    "https://ethereum-sepolia-rpc.publicnode.com",
					SharedConfig: &optimismv1alpha1.SharedConfig{
						Logging: &optimismv1alpha1.LoggingConfig{
							Level:  "info",
							Format: "logfmt",
							Color:  false,
						},
						Metrics: &optimismv1alpha1.MetricsConfig{
							Enabled: true,
							Port:    7300,
						},
						Resources: &optimismv1alpha1.ResourceConfig{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1000m"),
								corev1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					},
				},
				Status: optimismv1alpha1.OptimismNetworkStatus{
					Phase: "Ready",
					Conditions: []metav1.Condition{
						{
							Type:   "ConfigurationValid",
							Status: metav1.ConditionTrue,
							Reason: "ValidConfiguration",
						},
						{
							Type:   "L1Connected",
							Status: metav1.ConditionTrue,
							Reason: "RPCEndpointReachable",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, network)).To(Succeed())

			// Manually set status for unit testing (no controller interference)
			network.Status = optimismv1alpha1.OptimismNetworkStatus{
				Phase: "Ready",
				Conditions: []metav1.Condition{
					{
						Type:               "ConfigurationValid",
						Status:             metav1.ConditionTrue,
						Reason:             "ValidConfiguration",
						LastTransitionTime: metav1.Now(),
					},
					{
						Type:               "L1Connected",
						Status:             metav1.ConditionTrue,
						Reason:             "RPCEndpointReachable",
						LastTransitionTime: metav1.Now(),
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, network)).To(Succeed())

			// Register cleanup for network
			DeferCleanup(func() {
				networkToDelete := &optimismv1alpha1.OptimismNetwork{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: networkName, Namespace: "default"}, networkToDelete)
				if err == nil {
					Expect(k8sClient.Delete(ctx, networkToDelete)).To(Succeed())
					Eventually(func() bool {
						err := k8sClient.Get(ctx, types.NamespacedName{Name: networkName, Namespace: "default"}, networkToDelete)
						return apierrors.IsNotFound(err)
					}, timeout, interval).Should(BeTrue())
				}
			})

			By("Creating the sequencer OpNode")
			sequencer := &optimismv1alpha1.OpNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sequencerName,
					Namespace: "default",
				},
				Spec: optimismv1alpha1.OpNodeSpec{
					OptimismNetworkRef: optimismv1alpha1.OptimismNetworkRef{
						Name: networkName,
					},
					NodeType: "sequencer",
					OpNode: optimismv1alpha1.OpNodeConfig{
						Sequencer: &optimismv1alpha1.SequencerConfig{
							Enabled: true,
						},
					},
				},
				Status: optimismv1alpha1.OpNodeStatus{
					Phase: "Running",
					Conditions: []metav1.Condition{
						{
							Type:   "ConfigurationValid",
							Status: metav1.ConditionTrue,
							Reason: "ValidConfiguration",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, sequencer)).To(Succeed())

			// Manually set status for unit testing (no controller interference)
			sequencer.Status = optimismv1alpha1.OpNodeStatus{
				Phase: "Running",
				Conditions: []metav1.Condition{
					{
						Type:               "ConfigurationValid",
						Status:             metav1.ConditionTrue,
						Reason:             "ValidConfiguration",
						LastTransitionTime: metav1.Now(),
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, sequencer)).To(Succeed())

			// Register cleanup for sequencer
			DeferCleanup(func() {
				sequencerToDelete := &optimismv1alpha1.OpNode{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: sequencerName, Namespace: "default"}, sequencerToDelete)
				if err == nil {
					Expect(k8sClient.Delete(ctx, sequencerToDelete)).To(Succeed())
					Eventually(func() bool {
						err := k8sClient.Get(ctx, types.NamespacedName{Name: sequencerName, Namespace: "default"}, sequencerToDelete)
						return apierrors.IsNotFound(err)
					}, timeout, interval).Should(BeTrue())
				}
			})

			By("Creating the private key secret")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: "default",
				},
				Data: map[string][]byte{
					"private-key": []byte("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			// Register cleanup for secret
			DeferCleanup(func() {
				secretToDelete := &corev1.Secret{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: "default"}, secretToDelete)
				if err == nil {
					Expect(k8sClient.Delete(ctx, secretToDelete)).To(Succeed())
					Eventually(func() bool {
						err := k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: "default"}, secretToDelete)
						return apierrors.IsNotFound(err)
					}, timeout, interval).Should(BeTrue())
				}
			})
		})

		It("should successfully reconcile the resource", func() {
			By("Creating a new OpBatcher")
			opbatcher := &optimismv1alpha1.OpBatcher{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: optimismv1alpha1.OpBatcherSpec{
					OptimismNetworkRef: optimismv1alpha1.OptimismNetworkRef{
						Name: networkName,
					},
					SequencerRef: &optimismv1alpha1.SequencerReference{
						Name: sequencerName,
					},
					PrivateKey: optimismv1alpha1.SecretKeyRef{
						SecretRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: secretName,
							},
							Key: "private-key",
						},
					},
					Batching: &optimismv1alpha1.BatchingConfig{
						MaxChannelDuration: "10m",
						SubSafetyMargin:    10,
						TargetL1TxSize:     120000,
						TargetNumFrames:    1,
						ApproxComprRatio:   "0.4",
					},
					DataAvailability: &optimismv1alpha1.DataAvailabilityConfig{
						Type:          "blobs",
						MaxBlobsPerTx: 6,
					},
					RPC: &optimismv1alpha1.RPCConfig{
						Enabled: true,
						Host:    "127.0.0.1",
						Port:    8548,
					},
					Metrics: &optimismv1alpha1.MetricsConfig{
						Enabled: true,
						Host:    "0.0.0.0",
						Port:    7300,
					},
				},
			}
			Expect(k8sClient.Create(ctx, opbatcher)).To(Succeed())

			// Register cleanup for OpBatcher
			DeferCleanup(func() {
				opbatcherToDelete := &optimismv1alpha1.OpBatcher{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, opbatcherToDelete)
				if err == nil {
					Expect(k8sClient.Delete(ctx, opbatcherToDelete)).To(Succeed())

					// For unit tests, manually trigger reconciliation to handle deletion
					testReconciler := &OpBatcherReconciler{
						Client: k8sClient,
						Scheme: k8sClient.Scheme(),
					}
					_, err := testReconciler.Reconcile(ctx, reconcile.Request{
						NamespacedName: types.NamespacedName{Name: resourceName, Namespace: "default"},
					})
					Expect(err).NotTo(HaveOccurred())

					// Now verify deletion completed
					Eventually(func() bool {
						err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, opbatcherToDelete)
						return apierrors.IsNotFound(err)
					}, timeout, interval).Should(BeTrue())
				}
			})

			By("Directly testing the OpBatcher controller reconciler")
			// Use unit testing approach for complex controllers with external dependencies
			controllerReconciler := &OpBatcherReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// Test reconciliation directly (multiple times to handle requeues)
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      resourceName,
					Namespace: "default",
				},
			}

			// First reconcile - typically adds finalizer
			result, err := controllerReconciler.Reconcile(ctx, req)
			_, _ = fmt.Fprintf(GinkgoWriter, "First reconcile result: %+v, error: %v\n", result, err)
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile - handles main logic
			result, err = controllerReconciler.Reconcile(ctx, req)
			_, _ = fmt.Fprintf(GinkgoWriter, "Second reconcile result: %+v, error: %v\n", result, err)
			Expect(err).NotTo(HaveOccurred())

			By("Checking that OpBatcher was updated with proper conditions")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      resourceName,
					Namespace: "default",
				}, opbatcher)
			}, timeout, interval).Should(Succeed())

			// Debug: Print actual conditions and phase
			_, _ = fmt.Fprintf(GinkgoWriter, "Actual Phase: %s\n", opbatcher.Status.Phase)
			_, _ = fmt.Fprintf(GinkgoWriter, "Actual Conditions:\n")
			for _, condition := range opbatcher.Status.Conditions {
				_, _ = fmt.Fprintf(GinkgoWriter, "  - Type: %s, Status: %s, Reason: %s, Message: %s\n",
					condition.Type, condition.Status, condition.Reason, condition.Message)
			}

			// Should have configuration valid condition and network reference condition
			Expect(opbatcher.Status.Conditions).To(ContainElement(HaveField("Type", "ConfigurationValid")))
			Expect(opbatcher.Status.Conditions).To(ContainElement(HaveField("Type", "NetworkReference")))
			// Since all dependencies are ready in the test, we expect it to be running
			Expect(opbatcher.Status.Phase).To(Equal(OpBatcherPhaseRunning))
		})

		It("should create a Deployment", func() {
			By("Creating a new OpBatcher")
			opbatcher := &optimismv1alpha1.OpBatcher{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: optimismv1alpha1.OpBatcherSpec{
					OptimismNetworkRef: optimismv1alpha1.OptimismNetworkRef{
						Name: networkName,
					},
					SequencerRef: &optimismv1alpha1.SequencerReference{
						Name: sequencerName,
					},
					PrivateKey: optimismv1alpha1.SecretKeyRef{
						SecretRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: secretName,
							},
							Key: "private-key",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, opbatcher)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &OpBatcherReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      resourceName,
					Namespace: "default",
				},
			}

			// First reconcile - adds finalizer
			_, err := controllerReconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile - handles main logic and creates deployment
			_, err = controllerReconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			By("Checking that a Deployment was created")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      resourceName,
					Namespace: "default",
				}, deployment)
			}, timeout, interval).Should(Succeed())

			Expect(deployment.Name).To(Equal(resourceName))
			Expect(deployment.Namespace).To(Equal("default"))
			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Containers[0].Name).To(Equal("op-batcher"))
		})

		It("should create a Service", func() {
			By("Creating a new OpBatcher")
			opbatcher := &optimismv1alpha1.OpBatcher{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: optimismv1alpha1.OpBatcherSpec{
					OptimismNetworkRef: optimismv1alpha1.OptimismNetworkRef{
						Name: networkName,
					},
					SequencerRef: &optimismv1alpha1.SequencerReference{
						Name: sequencerName,
					},
					PrivateKey: optimismv1alpha1.SecretKeyRef{
						SecretRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: secretName,
							},
							Key: "private-key",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, opbatcher)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &OpBatcherReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      resourceName,
					Namespace: "default",
				},
			}

			// First reconcile - adds finalizer
			_, err := controllerReconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile - handles main logic and creates service
			_, err = controllerReconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			By("Checking that a Service was created")
			service := &corev1.Service{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      resourceName,
					Namespace: "default",
				}, service)
			}, timeout, interval).Should(Succeed())

			Expect(service.Name).To(Equal(resourceName))
			Expect(service.Namespace).To(Equal("default"))
			Expect(service.Spec.Ports).NotTo(BeEmpty())
		})

		It("should handle validation errors gracefully", func() {
			By("Creating an OpBatcher with invalid configuration")
			opbatcher := &optimismv1alpha1.OpBatcher{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: optimismv1alpha1.OpBatcherSpec{
					OptimismNetworkRef: optimismv1alpha1.OptimismNetworkRef{
						Name: "", // Invalid: empty network name
					},
					PrivateKey: optimismv1alpha1.SecretKeyRef{
						SecretRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "nonexistent-secret",
							},
							Key: "private-key",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, opbatcher)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &OpBatcherReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      resourceName,
					Namespace: "default",
				},
			}

			// First reconcile - adds finalizer
			_, err := controllerReconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile - handles validation and should set error conditions
			_, err = controllerReconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred()) // Should not error, but should set error conditions

			By("Checking that OpBatcher has error condition")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      resourceName,
					Namespace: "default",
				}, opbatcher)
			}, timeout, interval).Should(Succeed())

			Expect(opbatcher.Status.Phase).To(Equal(OpBatcherPhaseError))
			configValid := false
			for _, condition := range opbatcher.Status.Conditions {
				if condition.Type == "ConfigurationValid" && condition.Status == metav1.ConditionFalse {
					configValid = true
					break
				}
			}
			Expect(configValid).To(BeTrue())
		})

		It("should handle missing OptimismNetwork gracefully", func() {
			By("Creating an OpBatcher with nonexistent network reference")
			opbatcher := &optimismv1alpha1.OpBatcher{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: optimismv1alpha1.OpBatcherSpec{
					OptimismNetworkRef: optimismv1alpha1.OptimismNetworkRef{
						Name: "nonexistent-network",
					},
					PrivateKey: optimismv1alpha1.SecretKeyRef{
						SecretRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: secretName,
							},
							Key: "private-key",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, opbatcher)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &OpBatcherReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      resourceName,
					Namespace: "default",
				},
			}

			// First reconcile - adds finalizer
			_, err := controllerReconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile - should detect missing network and set error conditions
			_, err = controllerReconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			By("Checking that OpBatcher has network error condition")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      resourceName,
					Namespace: "default",
				}, opbatcher)
			}, timeout, interval).Should(Succeed())

			Expect(opbatcher.Status.Phase).To(Equal(OpBatcherPhaseError))
			networkRef := false
			for _, condition := range opbatcher.Status.Conditions {
				if condition.Type == "NetworkReference" && condition.Status == metav1.ConditionFalse {
					networkRef = true
					break
				}
			}
			Expect(networkRef).To(BeTrue())
		})

		It("should wait for OptimismNetwork to be ready", func() {
			By("Creating an OpBatcher with network that's not ready")
			// Update network to not be ready
			network := &optimismv1alpha1.OptimismNetwork{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: networkName, Namespace: "default"}, network)).To(Succeed())
			network.Status.Phase = "Pending"
			Expect(k8sClient.Status().Update(ctx, network)).To(Succeed())

			opbatcher := &optimismv1alpha1.OpBatcher{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: optimismv1alpha1.OpBatcherSpec{
					OptimismNetworkRef: optimismv1alpha1.OptimismNetworkRef{
						Name: networkName,
					},
					PrivateKey: optimismv1alpha1.SecretKeyRef{
						SecretRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: secretName,
							},
							Key: "private-key",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, opbatcher)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &OpBatcherReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      resourceName,
					Namespace: "default",
				},
			}

			// First reconcile - adds finalizer
			_, err := controllerReconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile - should detect network not ready and set pending state
			_, err = controllerReconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			By("Checking that OpBatcher is pending")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      resourceName,
					Namespace: "default",
				}, opbatcher)
			}, timeout, interval).Should(Succeed())

			Expect(opbatcher.Status.Phase).To(Equal(OpBatcherPhasePending))
			networkReady := false
			for _, condition := range opbatcher.Status.Conditions {
				if condition.Type == "NetworkReady" && condition.Status == metav1.ConditionFalse {
					networkReady = true
					break
				}
			}
			Expect(networkReady).To(BeTrue())
		})

		It("should handle finalizer correctly", func() {
			By("Creating a new OpBatcher")
			opbatcher := &optimismv1alpha1.OpBatcher{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: optimismv1alpha1.OpBatcherSpec{
					OptimismNetworkRef: optimismv1alpha1.OptimismNetworkRef{
						Name: networkName,
					},
					PrivateKey: optimismv1alpha1.SecretKeyRef{
						SecretRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: secretName,
							},
							Key: "private-key",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, opbatcher)).To(Succeed())

			By("Reconciling the created resource twice (finalizer, then logic)")
			controllerReconciler := &OpBatcherReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconcile - adds finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      resourceName,
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile - actual logic
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      resourceName,
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking that finalizer was added")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      resourceName,
					Namespace: "default",
				}, opbatcher)
			}, timeout, interval).Should(Succeed())

			found := false
			for _, finalizer := range opbatcher.Finalizers {
				if finalizer == OpBatcherFinalizer {
					found = true
					break
				}
			}
			Expect(found).To(BeTrue())

			By("Deleting the OpBatcher")
			Expect(k8sClient.Delete(ctx, opbatcher)).To(Succeed())

			By("Reconciling deletion")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      resourceName,
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking that resource was deleted")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      resourceName,
					Namespace: "default",
				}, opbatcher)
				return apierrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("Configuration validation", func() {
		var reconciler *OpBatcherReconciler

		BeforeEach(func() {
			reconciler = &OpBatcherReconciler{}
		})

		It("should validate required fields", func() {
			opbatcher := &optimismv1alpha1.OpBatcher{
				Spec: optimismv1alpha1.OpBatcherSpec{
					// Missing OptimismNetworkRef
					PrivateKey: optimismv1alpha1.SecretKeyRef{
						SecretRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-secret",
							},
							Key: "private-key",
						},
					},
				},
			}

			err := reconciler.validateConfiguration(opbatcher)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("optimismNetworkRef.name is required"))
		})

		It("should validate private key configuration", func() {
			opbatcher := &optimismv1alpha1.OpBatcher{
				Spec: optimismv1alpha1.OpBatcherSpec{
					OptimismNetworkRef: optimismv1alpha1.OptimismNetworkRef{
						Name: "test-network",
					},
					// Missing PrivateKey
				},
			}

			err := reconciler.validateConfiguration(opbatcher)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("privateKey.secretRef is required"))
		})

		It("should validate batching configuration", func() {
			opbatcher := &optimismv1alpha1.OpBatcher{
				Spec: optimismv1alpha1.OpBatcherSpec{
					OptimismNetworkRef: optimismv1alpha1.OptimismNetworkRef{
						Name: "test-network",
					},
					PrivateKey: optimismv1alpha1.SecretKeyRef{
						SecretRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-secret",
							},
							Key: "private-key",
						},
					},
					Batching: &optimismv1alpha1.BatchingConfig{
						TargetL1TxSize: 500, // Too small
					},
				},
			}

			err := reconciler.validateConfiguration(opbatcher)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("batching.targetL1TxSize must be at least 1000 bytes"))
		})

		It("should validate data availability configuration", func() {
			opbatcher := &optimismv1alpha1.OpBatcher{
				Spec: optimismv1alpha1.OpBatcherSpec{
					OptimismNetworkRef: optimismv1alpha1.OptimismNetworkRef{
						Name: "test-network",
					},
					PrivateKey: optimismv1alpha1.SecretKeyRef{
						SecretRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-secret",
							},
							Key: "private-key",
						},
					},
					DataAvailability: &optimismv1alpha1.DataAvailabilityConfig{
						Type: "invalid-type",
					},
				},
			}

			err := reconciler.validateConfiguration(opbatcher)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("dataAvailability.type must be 'blobs' or 'calldata'"))
		})

		It("should accept valid configuration", func() {
			opbatcher := &optimismv1alpha1.OpBatcher{
				Spec: optimismv1alpha1.OpBatcherSpec{
					OptimismNetworkRef: optimismv1alpha1.OptimismNetworkRef{
						Name: "test-network",
					},
					PrivateKey: optimismv1alpha1.SecretKeyRef{
						SecretRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-secret",
							},
							Key: "private-key",
						},
					},
					Batching: &optimismv1alpha1.BatchingConfig{
						MaxChannelDuration: "10m",
						SubSafetyMargin:    10,
						TargetL1TxSize:     120000,
						TargetNumFrames:    1,
						ApproxComprRatio:   "0.4",
					},
					DataAvailability: &optimismv1alpha1.DataAvailabilityConfig{
						Type:          "blobs",
						MaxBlobsPerTx: 6,
					},
				},
			}

			err := reconciler.validateConfiguration(opbatcher)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
