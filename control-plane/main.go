package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/andrew/kymaadapterstub/control-plane/api"
	"github.com/andrew/kymaadapterstub/control-plane/k8s"
	"github.com/andrew/kymaadapterstub/control-plane/models"
	"github.com/andrew/kymaadapterstub/control-plane/store"
)

func main() {
	// Initialize in-memory store
	s := store.NewMemoryStore()

	// Load example scenarios from GitHub (non-fatal if unavailable)
	repoURL := os.Getenv("SCENARIO_REPO_URL")
	if repoURL != "" {
		loadScenariosFromGitHub(s, repoURL)
	} else {
		log.Printf("SCENARIO_REPO_URL not set — skipping example scenario loading")
	}

	// Cluster domain for building public adapter URLs
	clusterDomain := os.Getenv("KYMA_DOMAIN")

	// Initialize Kubernetes client
	k8sClient, err := k8s.NewClient(clusterDomain)
	if err != nil {
		log.Printf("Warning: Kubernetes client initialization failed: %v. Continuing without K8s integration.", err)
	}

	// Initialize API handlers
	handler := api.NewHandler(s, k8sClient)

	namespace := os.Getenv("KYMA_NAMESPACE")
	if namespace == "" {
		namespace = "default"
	}

	controlPlaneURL := os.Getenv("CONTROL_PLANE_URL")
	if controlPlaneURL == "" {
		controlPlaneURL = "http://control-plane:8080"
	}

	handler.SetNamespace(namespace)
	handler.SetControlPlaneURL(controlPlaneURL)

	// Setup routes
	mux := http.NewServeMux()
	fs := http.FileServer(http.Dir("./ui"))
	mux.Handle("/", fs)

	mux.HandleFunc("/api/scenarios", handler.HandleScenarios)
	mux.HandleFunc("/api/scenarios/", handler.HandleScenarioDetail)
	mux.HandleFunc("/api/adapter-config/", handler.HandleAdapterConfig)
	mux.HandleFunc("/api/cleanup", handler.HandleCleanup)
	mux.HandleFunc("/health", handler.HandleHealth)

	httpHandler := api.CORSMiddleware(mux)

	port := ":8080"
	log.Printf("Control Plane starting on %s", port)
	log.Printf("Namespace: %s", namespace)
	log.Printf("Control Plane URL: %s", controlPlaneURL)
	log.Printf("Cluster Domain: %s", clusterDomain)

	if err := http.ListenAndServe(port, httpHandler); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// loadScenariosFromGitHub fetches the manifest and each scenario file from the repo.
// All errors are logged as warnings — a failure here never prevents startup.
func loadScenariosFromGitHub(s *store.MemoryStore, repoURL string) {
	log.Printf("Loading example scenarios from %s", repoURL)

	client := &http.Client{Timeout: 10 * time.Second}

	// Fetch manifest
	manifestURL := fmt.Sprintf("%s/manifest.json", repoURL)
	resp, err := client.Get(manifestURL)
	if err != nil {
		log.Printf("Warning: could not fetch scenario manifest from GitHub: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Warning: scenario manifest returned HTTP %d — skipping example scenarios", resp.StatusCode)
		return
	}

	var manifest models.ScenarioManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		log.Printf("Warning: could not parse scenario manifest: %v", err)
		return
	}

	loaded, failed := 0, 0
	for _, path := range manifest.Scenarios {
		fileURL := fmt.Sprintf("%s/%s", repoURL, path)
		if err := loadScenarioFile(s, client, fileURL); err != nil {
			log.Printf("Warning: could not load scenario %s: %v", path, err)
			failed++
		} else {
			loaded++
		}
	}

	log.Printf("Example scenarios: %d loaded, %d failed", loaded, failed)
}

func loadScenarioFile(s *store.MemoryStore, client *http.Client, url string) error {
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var file models.ScenarioFile
	if err := json.NewDecoder(resp.Body).Decode(&file); err != nil {
		return fmt.Errorf("parse failed: %w", err)
	}

	if file.Name == "" {
		return fmt.Errorf("scenario file has no name")
	}

	return s.LoadGitHubScenario(file)
}
