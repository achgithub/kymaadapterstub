package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type AdapterConfig struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Type           string            `json:"type"` // REST-SENDER, SOAP-SENDER, XI-SENDER
	BehaviorMode   string            `json:"behavior_mode"`
	TargetURL      string            `json:"target_url"`
	Method         string            `json:"method"`          // HTTP method, default POST
	RequestBody    string            `json:"request_body"`    // payload to send
	RequestHeaders map[string]string `json:"request_headers"` // additional headers
}

type TriggerResult struct {
	StatusCode      int               `json:"status_code"`
	ResponseBody    string            `json:"response_body"`
	ResponseHeaders map[string]string `json:"response_headers"`
	Error           string            `json:"error,omitempty"`
	SentTo          string            `json:"sent_to"`
	Protocol        string            `json:"protocol"`
}

func main() {
	adapterID := os.Getenv("ADAPTER_ID")
	controlPlaneURL := os.Getenv("CONTROL_PLANE_URL")

	if adapterID == "" {
		log.Fatal("ADAPTER_ID environment variable is required")
	}
	if controlPlaneURL == "" {
		controlPlaneURL = "http://control-plane:8080"
	}

	log.Printf("Sender Adapter started")
	log.Printf("Adapter ID: %s", adapterID)
	log.Printf("Control Plane URL: %s", controlPlaneURL)

	mux := http.NewServeMux()

	mux.HandleFunc("/trigger", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		reportActivity(adapterID, controlPlaneURL)
		handleTrigger(w, r, adapterID, controlPlaneURL)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Root handler — return adapter info
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"adapter": adapterID,
			"type":    "sender",
			"trigger": "POST /trigger",
		})
	})

	port := ":8080"
	log.Printf("Sender Adapter listening on %s", port)
	if err := http.ListenAndServe(port, mux); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func handleTrigger(w http.ResponseWriter, r *http.Request, adapterID, controlPlaneURL string) {
	config, err := fetchConfig(adapterID, controlPlaneURL)
	if err != nil {
		log.Printf("Error fetching config: %v", err)
		writeResult(w, TriggerResult{Error: fmt.Sprintf("Failed to fetch config: %v", err)})
		return
	}

	if config.TargetURL == "" {
		writeResult(w, TriggerResult{Error: "No target_url configured"})
		return
	}

	protocol := strings.TrimSuffix(config.Type, "-SENDER") // "REST", "SOAP", "XI"
	method := config.Method
	if method == "" {
		method = "POST"
	}

	var bodyReader io.Reader
	if config.RequestBody != "" {
		bodyReader = strings.NewReader(config.RequestBody)
	}

	req, err := http.NewRequest(method, config.TargetURL, bodyReader)
	if err != nil {
		writeResult(w, TriggerResult{Error: fmt.Sprintf("Failed to build request: %v", err), SentTo: config.TargetURL, Protocol: protocol})
		return
	}

	// Set Content-Type based on protocol
	switch protocol {
	case "SOAP":
		req.Header.Set("Content-Type", "text/xml; charset=utf-8")
		req.Header.Set("SOAPAction", "\"\"")
	case "XI":
		req.Header.Set("Content-Type", "text/xml; charset=utf-8")
		req.Header.Set("SOAPAction", "\"\"")
		// SAP XI routing headers
		req.Header.Set("sap-xi-version", "1.0")
	default: // REST
		if config.RequestBody != "" {
			req.Header.Set("Content-Type", "application/json")
		}
	}

	// Apply any custom headers (these override defaults above)
	for k, v := range config.RequestHeaders {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Trigger failed: %v", err)
		writeResult(w, TriggerResult{Error: fmt.Sprintf("Request failed: %v", err), SentTo: config.TargetURL, Protocol: protocol})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	// Flatten response headers
	respHeaders := make(map[string]string)
	for k, vals := range resp.Header {
		respHeaders[k] = strings.Join(vals, ", ")
	}

	result := TriggerResult{
		StatusCode:      resp.StatusCode,
		ResponseBody:    string(respBody),
		ResponseHeaders: respHeaders,
		SentTo:          config.TargetURL,
		Protocol:        protocol,
	}

	log.Printf("Trigger: %s %s → %d", method, config.TargetURL, resp.StatusCode)
	writeResult(w, result)
}

func writeResult(w http.ResponseWriter, result TriggerResult) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func fetchConfig(adapterID, controlPlaneURL string) (*AdapterConfig, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s/api/adapter-config/%s", controlPlaneURL, adapterID))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("config endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var config AdapterConfig
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}
	return &config, nil
}

func reportActivity(adapterID, controlPlaneURL string) {
	go func() {
		c := &http.Client{Timeout: 2 * time.Second}
		c.Post(fmt.Sprintf("%s/api/adapter-activity/%s", controlPlaneURL, adapterID), "application/json", nil)
	}()
}
