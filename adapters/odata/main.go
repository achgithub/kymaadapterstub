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
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Type            string            `json:"type"`
	BehaviorMode    string            `json:"behavior_mode"`
	StatusCode      int               `json:"status_code"`
	ResponseBody    string            `json:"response_body"`
	ResponseHeaders map[string]string `json:"response_headers"`
	Credentials     interface{}       `json:"credentials"`
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

	log.Printf("OData Adapter started")
	log.Printf("Adapter ID: %s", adapterID)
	log.Printf("Control Plane URL: %s", controlPlaneURL)

	// Create HTTP client with timeout
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Setup HTTP handlers
	mux := http.NewServeMux()

	// OData metadata endpoint
	mux.HandleFunc("/$metadata", func(w http.ResponseWriter, r *http.Request) {
		handleMetadata(w, r, adapterID, controlPlaneURL, httpClient)
	})

	// OData data endpoint
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handleRequest(w, r, adapterID, controlPlaneURL, httpClient)
	})

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Start server
	port := ":8080"
	log.Printf("OData Adapter listening on %s", port)

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

	// CSRF token pre-fetch: CPI sends X-CSRF-Token: Fetch before write operations.
	// Return a token so CPI can proceed with the actual request.
	if r.Header.Get("X-CSRF-Token") == "Fetch" {
		w.Header().Set("X-CSRF-Token", "stub-csrf-token")
		log.Printf("[%s] %s - CSRF token fetch", r.Method, r.RequestURI)
	}

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
		w.Header().Set("Content-Type", "application/xml")
	}

	// Write status code and body
	w.WriteHeader(config.StatusCode)
	w.Write([]byte(config.ResponseBody))

	// Log request
	log.Printf("[%s] %s %s - %d", r.Method, r.RequestURI, r.RemoteAddr, config.StatusCode)
}

func handleMetadata(w http.ResponseWriter, r *http.Request, adapterID, controlPlaneURL string, client *http.Client) {
	// CSRF token pre-fetch: CPI targets /$metadata for the preflight HEAD request.
	if r.Header.Get("X-CSRF-Token") == "Fetch" {
		w.Header().Set("X-CSRF-Token", "stub-csrf-token")
		log.Printf("[%s] /$metadata - CSRF token fetch", r.Method)
	}

	// Return OData metadata
	// For MVP, return a minimal metadata response
	metadata := `<?xml version="1.0" encoding="utf-8"?>
<edmx:Edmx Version="1.0" xmlns:edmx="http://schemas.microsoft.com/ado/2007/06/edmx">
  <edmx:DataServices m:DataServiceVersion="2.0" xmlns:m="http://schemas.microsoft.com/ado/2007/08/dataservices/metadata">
    <Schema Namespace="TestService" xmlns="http://schemas.microsoft.com/ado/2008/09/edm">
      <EntityType Name="Entity">
        <Key>
          <PropertyRef Name="ID"/>
        </Key>
        <Property Name="ID" Type="Edm.String" Nullable="false"/>
        <Property Name="Name" Type="Edm.String"/>
      </EntityType>
      <EntityContainer Name="TestServiceEntities" m:IsDefaultEntityContainer="true">
        <EntitySet Name="Entities" EntityType="TestService.Entity"/>
      </EntityContainer>
    </Schema>
  </edmx:DataServices>
</edmx:Edmx>`

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(metadata))

	log.Printf("[GET] /$metadata %s - 200", r.RemoteAddr)
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
