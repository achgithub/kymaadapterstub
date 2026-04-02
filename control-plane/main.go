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

const idleShutdownAfter = 30 * time.Minute
const idleCheckInterval = 10 * time.Minute

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
	mux.HandleFunc("/api/adapter-activity/", handler.HandleAdapterActivity)
	mux.HandleFunc("/api/cleanup", handler.HandleCleanup)
	mux.HandleFunc("/api/system/log", handler.HandleSystemLog)
	mux.HandleFunc("/health", handler.HandleHealth)

	// Start background idle-shutdown goroutine
	go runIdleShutdown(s, k8sClient, namespace)

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

// runIdleShutdown checks every idleCheckInterval for running scenarios where all
// adapters have been idle for idleShutdownAfter, and gracefully stops them.
func runIdleShutdown(s *store.MemoryStore, k8sClient *k8s.Client, namespace string) {
	ticker := time.NewTicker(idleCheckInterval)
	defer ticker.Stop()

	for range ticker.C {
		scenarios, err := s.ListScenarios()
		if err != nil {
			continue
		}

		running := 0
		for _, sc := range scenarios {
			if sc.Status == "running" {
				running++
			}
		}

		checkMsg := fmt.Sprintf("Idle check: %d scenario(s) running", running)
		log.Printf(checkMsg)
		s.AddSystemLog(checkMsg)

		for _, scenario := range scenarios {
			if scenario.Status != "running" {
				continue
			}

			runningAdapters := 0
			allIdle := true
			var mostRecentActivity time.Time
			for _, adapter := range scenario.Adapters {
				if adapter.Status != "running" {
					continue
				}
				runningAdapters++
				if adapter.LastActivity == nil || time.Since(*adapter.LastActivity) < idleShutdownAfter {
					allIdle = false
				}
				if adapter.LastActivity != nil && adapter.LastActivity.After(mostRecentActivity) {
					mostRecentActivity = *adapter.LastActivity
				}
			}

			if runningAdapters == 0 {
				continue
			}

			if !allIdle {
				idleFor := time.Since(mostRecentActivity).Round(time.Second)
				msg := fmt.Sprintf("  '%s': active (last call %s ago, shutdown after %.0fm)", scenario.Name, idleFor, idleShutdownAfter.Minutes())
				log.Printf(msg)
				s.AddSystemLog(msg)
				continue
			}

			msg := fmt.Sprintf("Auto-stopping idle scenario '%s' (no activity for %.0f minutes)", scenario.Name, idleShutdownAfter.Minutes())
			log.Printf(msg)
			s.AddSystemLog(msg)

			for _, adapter := range scenario.Adapters {
				if adapter.Status != "running" {
					continue
				}
				if k8sClient != nil {
					if err := k8sClient.StopAdapterDeployment(namespace, adapter); err != nil {
						errMsg := fmt.Sprintf("Auto-stop: error stopping adapter %s: %v", adapter.ID, err)
						log.Printf(errMsg)
						s.AddSystemLog(errMsg)
					}
				}
				s.UpdateAdapterStatus(scenario.ID, adapter.ID, "stopped")
			}
			s.UpdateScenarioStatus(scenario.ID, "stopped")
		}
	}
}

// loadScenariosFromGitHub fetches the manifest and each scenario file from the repo.
// All errors are logged as warnings — a failure here never prevents startup.
func loadScenariosFromGitHub(s *store.MemoryStore, repoURL string) {
	msg := fmt.Sprintf("Loading example scenarios from %s", repoURL)
	log.Printf(msg)
	s.AddStartupLog(msg)

	client := &http.Client{Timeout: 10 * time.Second}

	// Fetch manifest
	manifestURL := fmt.Sprintf("%s/manifest.json", repoURL)
	resp, err := client.Get(manifestURL)
	if err != nil {
		msg = fmt.Sprintf("Warning: could not fetch scenario manifest: %v", err)
		log.Printf(msg)
		s.AddStartupLog(msg)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg = fmt.Sprintf("Warning: scenario manifest returned HTTP %d — skipping example scenarios", resp.StatusCode)
		log.Printf(msg)
		s.AddStartupLog(msg)
		return
	}

	var manifest models.ScenarioManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		msg = fmt.Sprintf("Warning: could not parse scenario manifest: %v", err)
		log.Printf(msg)
		s.AddStartupLog(msg)
		return
	}

	loaded, failed := 0, 0
	for _, path := range manifest.Scenarios {
		fileURL := fmt.Sprintf("%s/%s", repoURL, path)
		if err := loadScenarioFile(s, client, fileURL); err != nil {
			msg = fmt.Sprintf("Warning: could not load scenario %s: %v", path, err)
			log.Printf(msg)
			s.AddStartupLog(msg)
			failed++
		} else {
			loaded++
		}
	}

	msg = fmt.Sprintf("Example scenarios: %d loaded, %d failed", loaded, failed)
	log.Printf(msg)
	s.AddStartupLog(msg)
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
