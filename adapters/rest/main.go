package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AdapterConfig struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Type            string            `json:"type"`
	BehaviorMode    string            `json:"behavior_mode"`
	StatusCode      int               `json:"status_code"`
	ResponseBody    string            `json:"response_body"`
	ResponseHeaders map[string]string `json:"response_headers"`
	Credentials     *Credentials      `json:"credentials"`
}

// checkInboundAuth validates Basic Auth if credentials are configured.
func checkInboundAuth(w http.ResponseWriter, r *http.Request, config *AdapterConfig) bool {
	if config.Credentials == nil || config.Credentials.Username == "" {
		return true
	}
	user, pass, ok := r.BasicAuth()
	if !ok || user != config.Credentials.Username || pass != config.Credentials.Password {
		w.Header().Set("WWW-Authenticate", `Basic realm="stub"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

type contextKey string

const pathAdapterIDKey contextKey = "pathAdapterID"

// resolveAdapterID returns the effective adapter ID for a request.
// In PATH_PREFIX_MODE the first URL path segment carries the adapter ID;
// otherwise falls back to the env-var ID used in Kyma deployments.
func resolveAdapterID(r *http.Request, envID string) string {
	if id, ok := r.Context().Value(pathAdapterIDKey).(string); ok && id != "" {
		return id
	}
	return envID
}

// withPathPrefixMiddleware strips the first path segment as the adapter ID and
// stores it in context. Enables one container to serve multiple scenario adapter
// instances in LOCAL_MODE (PATH_PREFIX_MODE=true).
func withPathPrefixMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
		if len(parts) == 0 || parts[0] == "" {
			next.ServeHTTP(w, r)
			return
		}
		r2 := r.Clone(context.WithValue(r.Context(), pathAdapterIDKey, parts[0]))
		if len(parts) > 1 {
			r2.URL.Path = "/" + parts[1]
		} else {
			r2.URL.Path = "/"
		}
		r2.URL.RawPath = ""
		next.ServeHTTP(w, r2)
	})
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
		handleRequest(w, r, resolveAdapterID(r, adapterID), controlPlaneURL, httpClient)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Start server
	port := ":8080"
	log.Printf("REST Adapter listening on %s", port)

	var serverHandler http.Handler = mux
	if os.Getenv("PATH_PREFIX_MODE") == "true" {
		serverHandler = withPathPrefixMiddleware(mux)
	}
	if err := http.ListenAndServe(port, serverHandler); err != nil {
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

	if !checkInboundAuth(w, r, config) {
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
