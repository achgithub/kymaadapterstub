package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

type AdapterConfig struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Type             string            `json:"type"`
	BehaviorMode     string            `json:"behavior_mode"`
	StatusCode       int               `json:"status_code"`
	ResponseBody     string            `json:"response_body"`
	ResponseHeaders  map[string]string `json:"response_headers"`
	Credentials      interface{}       `json:"credentials"`
}

func main() {
	// Read environment variables
	adapterID := os.Getenv("ADAPTER_ID")
	controlPlaneURL := os.Getenv("CONTROL_PLANE_URL")

	if adapterID == "" {
		log.Fatal("ADAPTER_ID environment variable is required")
	}

	if controlPlaneURL == "" {
		controlPlaneURL = "http://control-plane:8080"
	}

	log.Printf("REST Adapter started")
	log.Printf("Adapter ID: %s", adapterID)
	log.Printf("Control Plane URL: %s", controlPlaneURL)

	// Create HTTP client with timeout
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Setup HTTP handlers
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handleRequest(w, r, adapterID, controlPlaneURL, httpClient)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Start server
	port := ":8080"
	log.Printf("REST Adapter listening on %s", port)

	if err := http.ListenAndServe(port, mux); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func reportActivity(adapterID, controlPlaneURL string) {
	go func() {
		c := &http.Client{Timeout: 2 * time.Second}
		c.Post(fmt.Sprintf("%s/api/adapter-activity/%s", controlPlaneURL, adapterID), "application/json", nil)
	}()
}

func handleRequest(w http.ResponseWriter, r *http.Request, adapterID, controlPlaneURL string, client *http.Client) {
	reportActivity(adapterID, controlPlaneURL)

	// Fetch configuration from control plane
	config, err := fetchConfig(adapterID, controlPlaneURL, client)
	if err != nil {
		log.Printf("Error fetching config: %v", err)
		http.Error(w, fmt.Sprintf("Failed to fetch configuration: %v", err), http.StatusInternalServerError)
		return
	}

	// Set response headers
	if config.ResponseHeaders != nil {
		for key, value := range config.ResponseHeaders {
			w.Header().Set(key, value)
		}
	}

	// Set content type if not already set
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}

	// Write status code and body
	w.WriteHeader(config.StatusCode)
	w.Write([]byte(config.ResponseBody))

	// Log request
	log.Printf("[%s] %s %s - %d", r.Method, r.RequestURI, r.RemoteAddr, config.StatusCode)
}

func fetchConfig(adapterID, controlPlaneURL string, client *http.Client) (*AdapterConfig, error) {
	url := fmt.Sprintf("%s/api/adapter-config/%s", controlPlaneURL, adapterID)

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("config endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var config AdapterConfig
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}

	return &config, nil
}
