package resources

import (
	"fmt"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	optimismv1alpha1 "github.com/ethereum-optimism/op-stack-operator/api/v1alpha1"
	"github.com/ethereum-optimism/op-stack-operator/pkg/config"
)

// CreateOpBatcherDeployment creates a Deployment for an OpBatcher instance
func CreateOpBatcherDeployment(
	opBatcher *optimismv1alpha1.OpBatcher,
	network *optimismv1alpha1.OptimismNetwork,
	sequencerServiceName string,
) *appsv1.Deployment {
	labels := map[string]string{
		"app.kubernetes.io/name":       "opbatcher",
		"app.kubernetes.io/instance":   opBatcher.Name,
		"app.kubernetes.io/component":  "batcher",
		"app.kubernetes.io/part-of":    "op-stack",
		"app.kubernetes.io/managed-by": "op-stack-operator",
	}

	// Default replica count
	replicas := int32(1)

	// Get container image
	imageConfig := config.DefaultImages
	containerImage := imageConfig.OpBatcher

	// Build op-batcher container
	container := corev1.Container{
		Name:  "op-batcher",
		Image: containerImage,
		Args:  buildOpBatcherArgs(opBatcher, network, sequencerServiceName),
		Ports: []corev1.ContainerPort{
			{
				Name:          "rpc",
				ContainerPort: getRPCPort(opBatcher),
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "metrics",
				ContainerPort: getMetricsPort(opBatcher),
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env:       buildOpBatcherEnvVars(opBatcher, network),
		Resources: getResourceRequirements(opBatcher),
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             &[]bool{true}[0],
			RunAsUser:                &[]int64{1000}[0],
			AllowPrivilegeEscalation: &[]bool{false}[0],
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz",
					Port: intstr.FromString("rpc"),
				},
			},
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
			FailureThreshold:    3,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/readyz",
					Port: intstr.FromString("rpc"),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       5,
			FailureThreshold:    3,
		},
	}

	// Add volume mounts for secrets
	container.VolumeMounts = []corev1.VolumeMount{
		{
			Name:      "private-key",
			MountPath: "/secrets",
			ReadOnly:  true,
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      opBatcher.Name,
			Namespace: opBatcher.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":     "opbatcher",
					"app.kubernetes.io/instance": opBatcher.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{container},
					Volumes: []corev1.Volume{
						{
							Name: "private-key",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: opBatcher.Spec.PrivateKey.SecretRef.Name,
									Items: []corev1.KeyToPath{
										{
											Key:  opBatcher.Spec.PrivateKey.SecretRef.Key,
											Path: "private-key",
										},
									},
								},
							},
						},
					},
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &[]bool{true}[0],
						RunAsUser:    &[]int64{1000}[0],
						FSGroup:      &[]int64{1000}[0],
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					RestartPolicy: corev1.RestartPolicyAlways,
				},
			},
		},
	}

	return deployment
}

// buildOpBatcherArgs builds command line arguments for op-batcher
func buildOpBatcherArgs(
	opBatcher *optimismv1alpha1.OpBatcher,
	network *optimismv1alpha1.OptimismNetwork,
	sequencerServiceName string,
) []string {
	args := []string{}

	// L1 RPC configuration from OptimismNetwork
	args = append(args, "--l1-eth-rpc", network.Spec.L1RpcUrl)

	// L2 RPC configuration - connect to sequencer if provided
	if sequencerServiceName != "" {
		// Use Kubernetes service discovery to connect to sequencer
		l2RpcUrl := fmt.Sprintf("http://%s.%s.svc.cluster.local:8545", sequencerServiceName, opBatcher.Namespace)
		args = append(args, "--l2-eth-rpc", l2RpcUrl)
	}

	// Private key from mounted secret
	args = append(args, "--private-key", "file:///secrets/private-key")

	// RPC configuration
	if opBatcher.Spec.RPC != nil && opBatcher.Spec.RPC.Enabled {
		args = append(args, "--rpc.enable-admin")
		args = append(args, "--rpc.addr", getRPCHost(opBatcher))
		args = append(args, "--rpc.port", strconv.Itoa(int(getRPCPort(opBatcher))))
	}

	// Metrics configuration
	if opBatcher.Spec.Metrics == nil || opBatcher.Spec.Metrics.Enabled {
		args = append(args, "--metrics.enabled")
		args = append(args, "--metrics.addr", getMetricsHost(opBatcher))
		args = append(args, "--metrics.port", strconv.Itoa(int(getMetricsPort(opBatcher))))
	}

	// Batching configuration
	if cfg := opBatcher.Spec.Batching; cfg != nil {
		if cfg.MaxChannelDuration != "" {
			args = append(args, "--max-channel-duration", cfg.MaxChannelDuration)
		}
		if cfg.SubSafetyMargin != 0 {
			args = append(args, "--sub-safety-margin", strconv.Itoa(int(cfg.SubSafetyMargin)))
		}
		if cfg.TargetL1TxSize != 0 {
			args = append(args, "--target-l1-tx-size-bytes", strconv.Itoa(int(cfg.TargetL1TxSize)))
		}
		if cfg.TargetNumFrames != 0 {
			args = append(args, "--target-num-frames", strconv.Itoa(int(cfg.TargetNumFrames)))
		}
		if cfg.ApproxComprRatio != "" {
			args = append(args, "--approx-compr-ratio", cfg.ApproxComprRatio)
		}
	}

	// Data availability configuration
	if cfg := opBatcher.Spec.DataAvailability; cfg != nil {
		if cfg.Type == "blobs" {
			args = append(args, "--data-availability-type", "blobs")
			if cfg.MaxBlobsPerTx != 0 {
				args = append(args, "--max-blobs-per-tx", strconv.Itoa(int(cfg.MaxBlobsPerTx)))
			}
		} else if cfg.Type == "calldata" {
			args = append(args, "--data-availability-type", "calldata")
		}
	}

	// Throttling configuration
	if cfg := opBatcher.Spec.Throttling; cfg != nil {
		if !cfg.Enabled {
			args = append(args, "--throttling.enabled=false")
		}
		if cfg.MaxPendingTx != 0 {
			args = append(args, "--max-pending-tx", strconv.Itoa(int(cfg.MaxPendingTx)))
		}
		if cfg.BacklogSafetyMargin != 0 {
			args = append(args, "--backlog-safety-margin", strconv.Itoa(int(cfg.BacklogSafetyMargin)))
		}
	}

	// L1 transaction management
	if cfg := opBatcher.Spec.L1Transaction; cfg != nil {
		if cfg.FeeLimitMultiplier != "" {
			args = append(args, "--txmgr.fee-limit-multiplier", cfg.FeeLimitMultiplier)
		}
		if cfg.ResubmissionTimeout != "" {
			args = append(args, "--txmgr.resubmission-timeout", cfg.ResubmissionTimeout)
		}
		if cfg.NumConfirmations != 0 {
			args = append(args, "--txmgr.num-confirmations", strconv.Itoa(int(cfg.NumConfirmations)))
		}
		if cfg.SafeAbortNonceTooLowCount != 0 {
			args = append(args, "--txmgr.safe-abort-nonce-too-low-count", strconv.Itoa(int(cfg.SafeAbortNonceTooLowCount)))
		}
	}

	// Logging configuration from shared config
	if network.Spec.SharedConfig != nil && network.Spec.SharedConfig.Logging != nil {
		if network.Spec.SharedConfig.Logging.Level != "" {
			args = append(args, "--log.level", network.Spec.SharedConfig.Logging.Level)
		}
		if network.Spec.SharedConfig.Logging.Format != "" {
			args = append(args, "--log.format", network.Spec.SharedConfig.Logging.Format)
		}
		if network.Spec.SharedConfig.Logging.Color {
			args = append(args, "--log.color")
		}
	}

	return args
}

// buildOpBatcherEnvVars builds environment variables for op-batcher
func buildOpBatcherEnvVars(
	_ *optimismv1alpha1.OpBatcher,
	_ *optimismv1alpha1.OptimismNetwork,
) []corev1.EnvVar {
	envVars := []corev1.EnvVar{
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		{
			Name: "POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
	}

	return envVars
}

// Helper functions for configuration

func getRPCHost(opBatcher *optimismv1alpha1.OpBatcher) string {
	if opBatcher.Spec.RPC != nil && opBatcher.Spec.RPC.Host != "" {
		return opBatcher.Spec.RPC.Host
	}
	return "127.0.0.1"
}

func getRPCPort(opBatcher *optimismv1alpha1.OpBatcher) int32 {
	if opBatcher.Spec.RPC != nil && opBatcher.Spec.RPC.Port != 0 {
		return opBatcher.Spec.RPC.Port
	}
	return 8548
}

func getMetricsHost(opBatcher *optimismv1alpha1.OpBatcher) string {
	if opBatcher.Spec.Metrics != nil && opBatcher.Spec.Metrics.Host != "" {
		return opBatcher.Spec.Metrics.Host
	}
	return "0.0.0.0"
}

func getMetricsPort(opBatcher *optimismv1alpha1.OpBatcher) int32 {
	if opBatcher.Spec.Metrics != nil && opBatcher.Spec.Metrics.Port != 0 {
		return opBatcher.Spec.Metrics.Port
	}
	return 7300
}

func getResourceRequirements(opBatcher *optimismv1alpha1.OpBatcher) corev1.ResourceRequirements {
	if opBatcher.Spec.Resources != nil {
		return *opBatcher.Spec.Resources
	}

	// Default resource requirements for op-batcher
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("2Gi"),
		},
	}
}

// CreateOpBatcherService creates a Service for an OpBatcher instance
func CreateOpBatcherService(opBatcher *optimismv1alpha1.OpBatcher) *corev1.Service {
	labels := map[string]string{
		"app.kubernetes.io/name":       "opbatcher",
		"app.kubernetes.io/instance":   opBatcher.Name,
		"app.kubernetes.io/component":  "batcher",
		"app.kubernetes.io/part-of":    "op-stack",
		"app.kubernetes.io/managed-by": "op-stack-operator",
	}

	serviceType := corev1.ServiceTypeClusterIP
	if opBatcher.Spec.Service != nil && opBatcher.Spec.Service.Type != "" {
		serviceType = opBatcher.Spec.Service.Type
	}

	ports := []corev1.ServicePort{}

	// Add RPC port if enabled
	if opBatcher.Spec.RPC == nil || opBatcher.Spec.RPC.Enabled {
		ports = append(ports, corev1.ServicePort{
			Name:       "rpc",
			Port:       getRPCPort(opBatcher),
			TargetPort: intstr.FromString("rpc"),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	// Add metrics port if enabled
	if opBatcher.Spec.Metrics == nil || opBatcher.Spec.Metrics.Enabled {
		ports = append(ports, corev1.ServicePort{
			Name:       "metrics",
			Port:       getMetricsPort(opBatcher),
			TargetPort: intstr.FromString("metrics"),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	// Add custom ports if specified
	if opBatcher.Spec.Service != nil && opBatcher.Spec.Service.Ports != nil {
		for _, port := range opBatcher.Spec.Service.Ports {
			ports = append(ports, corev1.ServicePort{
				Name:       port.Name,
				Port:       port.Port,
				TargetPort: port.TargetPort,
				Protocol:   port.Protocol,
			})
		}
	}

	annotations := map[string]string{}
	if opBatcher.Spec.Service != nil && opBatcher.Spec.Service.Annotations != nil {
		annotations = opBatcher.Spec.Service.Annotations
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        opBatcher.Name,
			Namespace:   opBatcher.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app.kubernetes.io/name":     "opbatcher",
				"app.kubernetes.io/instance": opBatcher.Name,
			},
			Type:  serviceType,
			Ports: ports,
		},
	}

	return service
}
