package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/andrew/kymaadapterstub/control-plane/k8s"
	"github.com/andrew/kymaadapterstub/control-plane/models"
	"github.com/andrew/kymaadapterstub/control-plane/store"
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

	// Delete all adapters (Kubernetes resources)
	for _, adapter := range scenario.Adapters {
		if h.k8sClient != nil {
			if err := h.k8sClient.DeleteAdapterResources(h.namespace, adapter); err != nil {
				log.Printf("Error deleting adapter resources: %v", err)
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
		if len(parts) == 2 {
			// POST /api/scenarios/{id}/adapters - add adapter
			h.addAdapter(w, r, scenarioID)
		}
	case http.MethodGet, http.MethodPut, http.MethodDelete:
		if len(parts) >= 3 {
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
	var req models.CreateAdapterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.Type == "" {
		http.Error(w, "Name and Type are required", http.StatusBadRequest)
		return
	}

	// Generate adapter ID
	adapterID := scenarioID + "-" + strings.ToLower(req.Type) + "-" + fmt.Sprintf("%d", time.Now().Unix())

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

	_, err := h.store.AddAdapter(scenarioID, adapter)
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
		serviceDNS, err := h.k8sClient.CreateAdapterService(h.namespace, adapter)
		if err != nil {
			log.Printf("Error creating service for adapter %s: %v", adapter.ID, err)
			continue
		}

		// Update adapter status and ingress URL
		h.store.UpdateAdapterStatus(scenarioID, adapter.ID, "running")
		h.store.UpdateAdapterIngressURL(scenarioID, adapter.ID, serviceDNS)

		// Update local scenario reference
		scenario.Adapters[i].Status = "running"
		scenario.Adapters[i].IngressURL = serviceDNS
	}

	h.store.UpdateScenarioStatus(scenarioID, "running")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(scenario)
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
					"id":             adapter.ID,
					"name":           adapter.Name,
					"type":           adapter.Type,
					"behavior_mode":  adapter.BehaviorMode,
					"config":         adapter.Config,
					"credentials":    adapter.Credentials,
				})
				return
			}
		}
	}

	http.Error(w, "Adapter not found", http.StatusNotFound)
}

// HandleHealth returns health status
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}
