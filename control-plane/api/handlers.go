package api

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/andrew/kymaadapterstub/control-plane/k8s"
	"github.com/andrew/kymaadapterstub/control-plane/models"
	"github.com/andrew/kymaadapterstub/control-plane/store"
	"golang.org/x/crypto/ssh"
)

type Handler struct {
	store           *store.MemoryStore
	k8sClient       *k8s.Client
	namespace       string
	controlPlaneURL string
}

func NewHandler(s *store.MemoryStore, k8sClient *k8s.Client) *Handler {
	return &Handler{
		store:     s,
		k8sClient: k8sClient,
		namespace: "default",
	}
}

func (h *Handler) SetNamespace(ns string) {
	h.namespace = ns
}

func (h *Handler) SetControlPlaneURL(url string) {
	h.controlPlaneURL = url
}

// HandleScenarios handles GET /api/scenarios and POST /api/scenarios
func (h *Handler) HandleScenarios(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listScenarios(w, r)
	case http.MethodPost:
		h.createScenario(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) listScenarios(w http.ResponseWriter, r *http.Request) {
	scenarios, err := h.store.ListScenarios()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(scenarios)
}

func (h *Handler) createScenario(w http.ResponseWriter, r *http.Request) {
	var req models.CreateScenarioRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	// Generate ID from name + timestamp
	id := strings.ToLower(req.Name) + "-" + fmt.Sprintf("%d", time.Now().Unix())

	scenario, err := h.store.CreateScenario(id, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(scenario)
}

// HandleScenarioDetail handles GET/PUT/DELETE /api/scenarios/{id} and related operations
func (h *Handler) HandleScenarioDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/scenarios/")
	parts := strings.Split(path, "/")

	scenarioID := parts[0]

	// Check for adapter sub-routes
	if len(parts) > 1 {
		switch parts[1] {
		case "adapters":
			h.handleAdapters(w, r, scenarioID, parts)
			return
		case "launch":
			h.handleLaunchScenario(w, r, scenarioID)
			return
		case "stop":
			h.handleStopScenario(w, r, scenarioID)
			return
		case "clone":
			h.handleCloneScenario(w, r, scenarioID)
			return
		}
	}

	// Scenario detail operations
	switch r.Method {
	case http.MethodGet:
		h.getScenario(w, r, scenarioID)
	case http.MethodPut:
		h.updateScenario(w, r, scenarioID)
	case http.MethodDelete:
		h.deleteScenario(w, r, scenarioID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) getScenario(w http.ResponseWriter, r *http.Request, scenarioID string) {
	scenario, err := h.store.GetScenario(scenarioID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(scenario)
}

func (h *Handler) updateScenario(w http.ResponseWriter, r *http.Request, scenarioID string) {
	var req models.UpdateScenarioRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	scenario, err := h.store.UpdateScenario(scenarioID, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(scenario)
}

func (h *Handler) deleteScenario(w http.ResponseWriter, r *http.Request, scenarioID string) {
	scenario, err := h.store.GetScenario(scenarioID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if scenario.Source == "github" {
		http.Error(w, "Cannot delete a GitHub example scenario. Use 'Use as Template' to create an editable copy.", http.StatusForbidden)
		return
	}

	// Delete all adapters (Kubernetes resources)
	for _, adapter := range scenario.Adapters {
		if h.k8sClient != nil {
			if err := h.k8sClient.DeleteAdapterResources(h.namespace, adapter); err != nil {
				log.Printf("Error deleting adapter resources: %v", err)
			}
			if err := h.k8sClient.DeleteAdapterAPIRule(h.namespace, adapter); err != nil {
				log.Printf("Error deleting APIRule: %v", err)
			}
		}
	}

	// Delete scenario
	if err := h.store.DeleteScenario(scenarioID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleAdapters handles adapter routes
func (h *Handler) handleAdapters(w http.ResponseWriter, r *http.Request, scenarioID string, parts []string) {
	switch r.Method {
	case http.MethodPost:
		if len(parts) >= 4 && parts[3] == "stop" {
			h.stopAdapter(w, r, scenarioID, parts[2])
		} else if len(parts) >= 4 && parts[3] == "trigger" {
			h.triggerAdapter(w, r, scenarioID, parts[2])
		} else if len(parts) == 2 {
			h.addAdapter(w, r, scenarioID)
		}
	case http.MethodGet, http.MethodPut, http.MethodDelete:
		if len(parts) >= 4 && parts[3] == "logs" {
			h.getAdapterLogs(w, r, scenarioID, parts[2])
		} else if len(parts) >= 3 {
			adapterID := parts[2]
			switch r.Method {
			case http.MethodGet:
				h.getAdapter(w, r, scenarioID, adapterID)
			case http.MethodPut:
				h.updateAdapter(w, r, scenarioID, adapterID)
			case http.MethodDelete:
				h.deleteAdapter(w, r, scenarioID, adapterID)
			}
		}
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) addAdapter(w http.ResponseWriter, r *http.Request, scenarioID string) {
	scenario, err := h.store.GetScenario(scenarioID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if scenario.Source == "github" {
		http.Error(w, "Cannot modify a GitHub example scenario. Use 'Use as Template' to create an editable copy.", http.StatusForbidden)
		return
	}

	var req models.CreateAdapterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.Type == "" {
		http.Error(w, "Name and Type are required", http.StatusBadRequest)
		return
	}

	// Kubernetes service names must start with a letter (DNS-1035)
	if len(req.Name) == 0 || req.Name[0] < 'a' || req.Name[0] > 'z' {
		http.Error(w, "Adapter name must start with a lowercase letter (e.g. 'my-adapter')", http.StatusBadRequest)
		return
	}

	// Generate adapter ID
	adapterID := scenarioID + "-" + strings.ToLower(req.Type) + "-" + fmt.Sprintf("%d", time.Now().Unix())

	// For SFTP adapters, generate a stable SSH host key if one isn't already set
	if req.Type == "SFTP" && req.Config.SSHHostKey == "" {
		keyPEM, fingerprint, err := generateSSHHostKey()
		if err != nil {
			log.Printf("Warning: could not generate SSH host key: %v", err)
		} else {
			req.Config.SSHHostKey = keyPEM
			req.Config.SSHHostKeyFingerprint = fingerprint
		}
	}

	adapter := models.Adapter{
		ID:             adapterID,
		Name:           req.Name,
		Type:           req.Type,
		BehaviorMode:   req.BehaviorMode,
		Config:         req.Config,
		Status:         "stopped",
		Credentials:    req.Credentials,
		DeploymentName: req.Name + "-deployment",
	}

	_, err = h.store.AddAdapter(scenarioID, adapter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(adapter)
}

func (h *Handler) getAdapter(w http.ResponseWriter, r *http.Request, scenarioID, adapterID string) {
	adapter, err := h.store.GetAdapter(scenarioID, adapterID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(adapter)
}

func (h *Handler) updateAdapter(w http.ResponseWriter, r *http.Request, scenarioID, adapterID string) {
	var req models.UpdateAdapterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	adapter, err := h.store.GetAdapter(scenarioID, adapterID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Delete old pod if running (so it picks up new config)
	if h.k8sClient != nil && adapter.Status == "running" {
		if err := h.k8sClient.DeleteAdapterResources(h.namespace, *adapter); err != nil {
			log.Printf("Error deleting adapter resources for update: %v", err)
		}
		h.store.UpdateAdapterStatus(scenarioID, adapterID, "stopped")
	}

	updates := models.Adapter{
		BehaviorMode: req.BehaviorMode,
		Config:       req.Config,
	}

	adapter, err = h.store.UpdateAdapter(scenarioID, adapterID, updates)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(adapter)
}

func (h *Handler) deleteAdapter(w http.ResponseWriter, r *http.Request, scenarioID, adapterID string) {
	adapter, err := h.store.GetAdapter(scenarioID, adapterID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Delete Kubernetes resources
	if h.k8sClient != nil {
		if err := h.k8sClient.DeleteAdapterResources(h.namespace, *adapter); err != nil {
			log.Printf("Error deleting adapter resources: %v", err)
		}
		if err := h.k8sClient.DeleteAdapterAPIRule(h.namespace, *adapter); err != nil {
			log.Printf("Error deleting APIRule: %v", err)
		}
	}

	if err := h.store.DeleteAdapter(scenarioID, adapterID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleLaunchScenario launches all adapters in a scenario
func (h *Handler) handleLaunchScenario(w http.ResponseWriter, r *http.Request, scenarioID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	scenario, err := h.store.GetScenario(scenarioID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if h.k8sClient == nil {
		http.Error(w, "Kubernetes client not available", http.StatusInternalServerError)
		return
	}

	// Launch each adapter
	for i, adapter := range scenario.Adapters {
		if adapter.Status != "stopped" {
			continue
		}

		// Create deployment
		if err := h.k8sClient.CreateAdapterDeployment(h.namespace, adapter, h.controlPlaneURL); err != nil {
			log.Printf("Error creating deployment for adapter %s: %v", adapter.ID, err)
			continue
		}

		// Create service
		_, err = h.k8sClient.CreateAdapterService(h.namespace, adapter)
		if err != nil {
			log.Printf("Error creating service for adapter %s: %v", adapter.ID, err)
			continue
		}

		var adapterURL string

		if adapter.Type == "SFTP" {
			// Wait for LoadBalancer external hostname
			hostname, err := h.k8sClient.GetLoadBalancerHostname(h.namespace, adapter.Name)
			if err != nil {
				log.Printf("Warning: could not get LoadBalancer hostname for %s: %v", adapter.Name, err)
				adapterURL = fmt.Sprintf("%s.%s.svc.cluster.local", adapter.Name, h.namespace)
			} else {
				adapterURL = fmt.Sprintf("%s:22", hostname)
			}
		} else {
			// Create APIRule for public access (REST/OData)
			publicURL, err := h.k8sClient.CreateAdapterAPIRule(h.namespace, adapter)
			if err != nil {
				log.Printf("Error creating APIRule for adapter %s: %v", adapter.ID, err)
			}
			adapterURL = publicURL
			if adapterURL == "" {
				adapterURL = fmt.Sprintf("%s.%s.svc.cluster.local", adapter.Name, h.namespace)
			}
		}

		// Update adapter status and ingress URL
		h.store.UpdateAdapterStatus(scenarioID, adapter.ID, "running")
		h.store.UpdateAdapterIngressURL(scenarioID, adapter.ID, adapterURL)

		// Update local scenario reference
		scenario.Adapters[i].Status = "running"
		scenario.Adapters[i].IngressURL = adapterURL
	}

	h.store.UpdateScenarioStatus(scenarioID, "running")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(scenario)
}

// handleStopScenario stops all running adapters in a scenario
func (h *Handler) handleStopScenario(w http.ResponseWriter, r *http.Request, scenarioID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	scenario, err := h.store.GetScenario(scenarioID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if h.k8sClient == nil {
		http.Error(w, "Kubernetes client not available", http.StatusInternalServerError)
		return
	}

	for _, adapter := range scenario.Adapters {
		if adapter.Status != "running" {
			continue
		}
		if err := h.k8sClient.StopAdapterDeployment(h.namespace, adapter); err != nil {
			log.Printf("Error stopping adapter %s: %v", adapter.ID, err)
		}
		h.store.UpdateAdapterStatus(scenarioID, adapter.ID, "stopped")
	}

	h.store.UpdateScenarioStatus(scenarioID, "stopped")

	scenario, _ = h.store.GetScenario(scenarioID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(scenario)
}

// handleCloneScenario creates a user-owned copy of any scenario
func (h *Handler) handleCloneScenario(w http.ResponseWriter, r *http.Request, scenarioID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&req) // optional body — ignore decode error

	newScenario, err := h.store.CloneScenario(scenarioID, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(newScenario)
}

// stopAdapter stops a single adapter
func (h *Handler) stopAdapter(w http.ResponseWriter, r *http.Request, scenarioID, adapterID string) {
	adapter, err := h.store.GetAdapter(scenarioID, adapterID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if h.k8sClient != nil && adapter.Status == "running" {
		if err := h.k8sClient.StopAdapterDeployment(h.namespace, *adapter); err != nil {
			log.Printf("Error stopping adapter %s: %v", adapterID, err)
		}
	}

	h.store.UpdateAdapterStatus(scenarioID, adapterID, "stopped")

	adapter, _ = h.store.GetAdapter(scenarioID, adapterID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(adapter)
}

// triggerAdapter proxies a fire request to a running sender adapter pod
func (h *Handler) triggerAdapter(w http.ResponseWriter, r *http.Request, scenarioID, adapterID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	adapter, err := h.store.GetAdapter(scenarioID, adapterID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	senderTypes := map[string]bool{"REST-SENDER": true, "SOAP-SENDER": true, "XI-SENDER": true}
	if !senderTypes[adapter.Type] {
		http.Error(w, "Only sender adapters can be triggered", http.StatusBadRequest)
		return
	}

	if adapter.Status != "running" {
		http.Error(w, "Adapter is not running — launch it first", http.StatusConflict)
		return
	}

	// Reach the adapter via its in-cluster service name
	triggerURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:8080/trigger", adapter.Name, h.namespace)

	client := &http.Client{Timeout: 35 * time.Second}
	resp, err := client.Post(triggerURL, "application/json", nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to reach adapter: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

// HandleAdapterConfig serves adapter configuration to adapters
func (h *Handler) HandleAdapterConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	adapterID := strings.TrimPrefix(r.URL.Path, "/api/adapter-config/")
	if adapterID == "" {
		http.Error(w, "Adapter ID is required", http.StatusBadRequest)
		return
	}

	// Find the adapter across all scenarios
	scenarios, _ := h.store.ListScenarios()
	for _, scenario := range scenarios {
		for _, adapter := range scenario.Adapters {
			if adapter.ID == adapterID {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"id":                      adapter.ID,
					"name":                    adapter.Name,
					"type":                    adapter.Type,
					"behavior_mode":           adapter.BehaviorMode,
					"status_code":             adapter.Config.StatusCode,
					"response_body":           adapter.Config.ResponseBody,
					"response_headers":        adapter.Config.ResponseHeaders,
					"response_delay_ms":       adapter.Config.ResponseDelayMs,
					"files":                   adapter.Config.Files,
					"auth_mode":               adapter.Config.AuthMode,
					"ssh_host_key":            adapter.Config.SSHHostKey,
					"ssh_host_key_fingerprint": adapter.Config.SSHHostKeyFingerprint,
					"credentials":             adapter.Credentials,
					"soap_version":            adapter.Config.SoapVersion,
					"as2_from":                adapter.Config.AS2From,
					"as2_to":                  adapter.Config.AS2To,
					"as4_party_id":            adapter.Config.AS4PartyID,
					"edi_standard":            adapter.Config.EDIStandard,
					"target_url":              adapter.Config.TargetURL,
					"method":                  adapter.Config.Method,
					"request_body":            adapter.Config.RequestBody,
					"request_headers":         adapter.Config.RequestHeaders,
				})
				return
			}
		}
	}

	http.Error(w, "Adapter not found", http.StatusNotFound)
}

// HandleCleanup deletes all orphaned adapter k8s resources
func (h *Handler) HandleCleanup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.k8sClient == nil {
		http.Error(w, "Kubernetes client not available", http.StatusInternalServerError)
		return
	}

	if err := h.k8sClient.CleanupOrphanedResources(h.namespace); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "cleanup complete"})
}

// HandleHealth returns health status
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

// generateSSHHostKey creates a 2048-bit RSA key and returns (PEM string, SHA256 fingerprint, error)
func generateSSHHostKey() (string, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("generate RSA key: %w", err)
	}

	pemBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}
	keyPEM := string(pem.EncodeToMemory(pemBlock))

	pub, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("derive public key: %w", err)
	}
	hash := sha256.Sum256(pub.Marshal())
	fingerprint := "SHA256:" + base64.StdEncoding.EncodeToString(hash[:])

	return keyPEM, fingerprint, nil
}

// getAdapterLogs fetches recent pod logs for a running adapter.
// Supports ?tail=N (default 100).
func (h *Handler) getAdapterLogs(w http.ResponseWriter, r *http.Request, scenarioID, adapterID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.k8sClient == nil {
		http.Error(w, "Kubernetes client not available", http.StatusServiceUnavailable)
		return
	}

	adapter, err := h.store.GetAdapter(scenarioID, adapterID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	tail := int64(100)
	if t := r.URL.Query().Get("tail"); t != "" {
		fmt.Sscanf(t, "%d", &tail)
	}

	logs, err := h.k8sClient.GetAdapterLogs(h.namespace, adapter.ID, tail)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get logs: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"logs": logs})
}

// HandleAdapterActivity records that an adapter has received a request.
// Called by adapters as a fire-and-forget POST on each incoming request.
func (h *Handler) HandleAdapterActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	adapterID := strings.TrimPrefix(r.URL.Path, "/api/adapter-activity/")
	if adapterID == "" {
		http.Error(w, "Adapter ID required", http.StatusBadRequest)
		return
	}
	h.store.RecordAdapterActivity(adapterID)
	w.WriteHeader(http.StatusNoContent)
}

// HandleSystemLog returns system log entries. Supports ?tail=N to limit results.
func (h *Handler) HandleSystemLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tail := 0
	if t := r.URL.Query().Get("tail"); t != "" {
		fmt.Sscanf(t, "%d", &tail)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.store.GetSystemLog(tail))
}
