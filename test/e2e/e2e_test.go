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

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/ethereum-optimism/op-stack-operator/test/utils"
)

// namespace where the project is deployed in
const namespace = "op-stack-operator-system"

// serviceAccountName created for the project
const serviceAccountName = "op-stack-operator-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "op-stack-operator-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "op-stack-operator-metrics-binding"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the restricted security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up test OpNode resources")
		cmd := exec.Command("kubectl", "delete", "opnode", "--all", "-n", namespace, "--ignore-not-found")
		_, _ = utils.Run(cmd)

		By("cleaning up test OptimismNetwork resources")
		cmd = exec.Command("kubectl", "delete", "optimismnetwork", "--all", "-n", namespace, "--ignore-not-found")
		_, _ = utils.Run(cmd)

		By("cleaning up the curl pod for metrics")
		cmd = exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace)
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				// Get the name of the controller-manager pod
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// Validate the pod's status
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd := exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=op-stack-operator-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", namespace, serviceAccountName),
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("validating that the ServiceMonitor for Prometheus is applied in the namespace")
			cmd = exec.Command("kubectl", "get", "ServiceMonitor", "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "ServiceMonitor should exist")

			By("getting the service account token")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("waiting for the metrics endpoint to be ready")
			verifyMetricsEndpointReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "endpoints", metricsServiceName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("8443"), "Metrics endpoint is not ready")
			}
			Eventually(verifyMetricsEndpointReady).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("controller-runtime.metrics\tServing metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted).Should(Succeed())

			By("creating the curl-metrics pod to access the metrics endpoint")
			cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
				"--namespace", namespace,
				"--image=curlimages/curl:latest",
				"--overrides",
				fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": ["curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics"],
							"securityContext": {
								"allowPrivilegeEscalation": false,
								"capabilities": {
									"drop": ["ALL"]
								},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {
									"type": "RuntimeDefault"
								}
							}
						}],
						"serviceAccount": "%s"
					}
				}`, token, metricsServiceName, namespace, serviceAccountName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create curl-metrics pod")

			By("waiting for the curl-metrics pod to complete.")
			verifyCurlUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}",
					"-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			By("getting the metrics by checking curl-metrics logs")
			metricsOutput := getMetricsOutput()
			Expect(metricsOutput).To(ContainSubstring(
				"controller_runtime_reconcile_total",
			))
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks

		It("should successfully deploy and reconcile OptimismNetwork and OpNode resources", func() {
			// Load real L1 RPC URLs from environment (same as integration tests)
			testL1RpcUrl := os.Getenv("TEST_L1_RPC_URL")
			if testL1RpcUrl == "" {
				Skip("Skipping OpNode e2e tests - no TEST_L1_RPC_URL environment variable set")
			}

			// Use beacon URL from environment if available, otherwise fallback to localhost
			testL1BeaconUrl := os.Getenv("TEST_L1_BEACON_URL")
			if testL1BeaconUrl == "" {
				testL1BeaconUrl = "http://localhost:5052"
			}

			By("creating test OptimismNetwork resource")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(`
apiVersion: optimism.optimism.io/v1alpha1
kind: OptimismNetwork
metadata:
  name: test-network
  namespace: ` + namespace + `
spec:
  networkName: "op-sepolia"
  chainID: 11155420
  l1ChainID: 11155111
  l1RpcUrl: "` + testL1RpcUrl + `"
  l1BeaconUrl: "` + testL1BeaconUrl + `"
  l1RpcTimeout: "10s"
  rollupConfig:
    autoDiscover: true
  l2Genesis:
    autoDiscover: true
  contractAddresses:
    discoveryMethod: "well-known"
    cacheTimeout: "24h"
  sharedConfig:
    logging:
      level: "info"
      format: "logfmt"
    metrics:
      enabled: true
      port: 7300
`)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create OptimismNetwork")

			By("waiting for OptimismNetwork to be created")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "optimismnetwork", "test-network", "-n", namespace)
				_, err := utils.Run(cmd)
				return err == nil
			}, 2*time.Minute).Should(BeTrue())

			By("creating test OpNode replica resource")
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(`
apiVersion: optimism.optimism.io/v1alpha1
kind: OpNode
metadata:
  name: test-opnode-replica
  namespace: ` + namespace + `
spec:
  optimismNetworkRef:
    name: test-network
    namespace: ` + namespace + `
  nodeType: "replica"
  opNode:
    syncMode: "execution-layer"
    p2p:
      enabled: true
      listenPort: 9003
      discovery:
        enabled: true
      privateKey:
        generate: true
    rpc:
      enabled: true
      host: "0.0.0.0"
      port: 9545
      enableAdmin: false
    sequencer:
      enabled: false
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
        host: "0.0.0.0"
        port: 8545
        apis: ["web3", "eth", "net"]
      ws:
        enabled: true
        host: "0.0.0.0"
        port: 8546
        apis: ["web3", "eth"]
      authrpc:
        host: "127.0.0.1"
        port: 8551
        apis: ["engine", "eth"]
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
        memory: "512Mi"
      limits:
        cpu: "1000m"
        memory: "2Gi"
  service:
    type: "ClusterIP"
`)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create OpNode")

			By("waiting for OpNode to be created")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "opnode", "test-opnode-replica", "-n", namespace)
				_, err := utils.Run(cmd)
				return err == nil
			}, 2*time.Minute).Should(BeTrue())

			By("verifying StatefulSet is created for OpNode")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "statefulset", "test-opnode-replica", "-n", namespace)
				_, err := utils.Run(cmd)
				return err == nil
			}, 3*time.Minute).Should(BeTrue())

			By("verifying Service is created for OpNode")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "service", "test-opnode-replica", "-n", namespace)
				_, err := utils.Run(cmd)
				return err == nil
			}, 2*time.Minute).Should(BeTrue())

			By("verifying JWT secret is created")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "secret", "test-opnode-replica-jwt", "-n", namespace)
				_, err := utils.Run(cmd)
				return err == nil
			}, 2*time.Minute).Should(BeTrue())

			By("verifying P2P secret is created")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "secret", "test-opnode-replica-p2p", "-n", namespace)
				_, err := utils.Run(cmd)
				return err == nil
			}, 2*time.Minute).Should(BeTrue())

			By("checking OpNode status conditions")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "opnode", "test-opnode-replica", "-n", namespace,
					"-o", "jsonpath={.status.conditions}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				return strings.Contains(output, "ConfigurationValid") || strings.Contains(output, "SecretsReady")
			}, 3*time.Minute).Should(BeTrue())

			By("verifying controller reconciliation metrics")
			metricsOutput := getMetricsOutput()
			Expect(metricsOutput).To(ContainSubstring(
				`controller_runtime_reconcile_total{controller="opnode"`),
				"OpNode controller should have reconciliation metrics")
			Expect(metricsOutput).To(ContainSubstring(
				`controller_runtime_reconcile_total{controller="optimismnetwork"`),
				"OptimismNetwork controller should have reconciliation metrics")
		})

		It("should handle OpNode deletion and cleanup properly", func() {
			// Ensure we have L1 RPC URL configured (same as other test)
			testL1RpcUrl := os.Getenv("TEST_L1_RPC_URL")
			if testL1RpcUrl == "" {
				Skip("Skipping OpNode deletion e2e test - no TEST_L1_RPC_URL environment variable set")
			}

			By("creating a temporary OpNode for deletion test")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(`
apiVersion: optimism.optimism.io/v1alpha1
kind: OpNode
metadata:
  name: test-opnode-delete
  namespace: ` + namespace + `
spec:
  optimismNetworkRef:
    name: test-network
    namespace: ` + namespace + `
  nodeType: "replica"
  opNode:
    syncMode: "execution-layer"
    sequencer:
      enabled: false
  opGeth:
    dataDir: "/data/geth"
    storage:
      size: "1Gi"
      storageClass: "standard"
      accessMode: "ReadWriteOnce"
`)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create OpNode for deletion test")

			By("waiting for OpNode to be created")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "opnode", "test-opnode-delete", "-n", namespace)
				_, err := utils.Run(cmd)
				return err == nil
			}, 2*time.Minute).Should(BeTrue())

			By("deleting the OpNode")
			cmd = exec.Command("kubectl", "delete", "opnode", "test-opnode-delete", "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete OpNode")

			By("verifying OpNode is fully deleted")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "opnode", "test-opnode-delete", "-n", namespace)
				_, err := utils.Run(cmd)
				return err != nil // Should fail when resource is deleted
			}, 3*time.Minute).Should(BeTrue())

			By("verifying associated resources are cleaned up")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "statefulset", "test-opnode-delete", "-n", namespace)
				_, err := utils.Run(cmd)
				return err != nil // Should fail when StatefulSet is deleted
			}, 2*time.Minute).Should(BeTrue())
		})
	})
})

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	// Temporary file to store the token request
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		// Execute kubectl command to create the token
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		// Parse the JSON output to extract the token
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() string {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	metricsOutput, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
	Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
	return metricsOutput
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
