package main

import (
	"log"
	"net/http"
	"os"

	"github.com/andrew/kymaadapterstub/control-plane/api"
	"github.com/andrew/kymaadapterstub/control-plane/k8s"
	"github.com/andrew/kymaadapterstub/control-plane/store"
)

func main() {
	// Initialize in-memory store
	s := store.NewMemoryStore()

	// Cluster domain for building public adapter URLs (e.g. c-6b6fad5.kyma.ondemand.com)
	clusterDomain := os.Getenv("KYMA_DOMAIN")

	// Initialize Kubernetes client
	k8sClient, err := k8s.NewClient(clusterDomain)
	if err != nil {
		log.Printf("Warning: Kubernetes client initialization failed: %v. Continuing without K8s integration.", err)
	}

	// Initialize API handlers
	handler := api.NewHandler(s, k8sClient)

	// Determine namespace for resources (from env or default)
	namespace := os.Getenv("KYMA_NAMESPACE")
	if namespace == "" {
		namespace = "default"
	}

	// Determine control plane URL for adapter config fetching
	controlPlaneURL := os.Getenv("CONTROL_PLANE_URL")
	if controlPlaneURL == "" {
		controlPlaneURL = "http://control-plane:8080"
	}

	handler.SetNamespace(namespace)
	handler.SetControlPlaneURL(controlPlaneURL)

	// Setup routes
	mux := http.NewServeMux()

	// UI routes
	fs := http.FileServer(http.Dir("./ui"))
	mux.Handle("/", fs)

	// API routes
	mux.HandleFunc("/api/scenarios", handler.HandleScenarios)
	mux.HandleFunc("/api/scenarios/", handler.HandleScenarioDetail)
	mux.HandleFunc("/api/adapter-config/", handler.HandleAdapterConfig)
	mux.HandleFunc("/api/cleanup", handler.HandleCleanup)

	// Health check
	mux.HandleFunc("/health", handler.HandleHealth)

	// CORS middleware
	httpHandler := api.CORSMiddleware(mux)

	port := ":8080"
	log.Printf("Control Plane starting on %s", port)
	log.Printf("UI available at http://localhost:8080")
	log.Printf("API available at http://localhost:8080/api")
	log.Printf("Namespace: %s", namespace)
	log.Printf("Control Plane URL: %s", controlPlaneURL)
	log.Printf("Cluster Domain: %s", clusterDomain)

	if err := http.ListenAndServe(port, httpHandler); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
