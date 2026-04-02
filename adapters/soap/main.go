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
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Type            string            `json:"type"`
	BehaviorMode    string            `json:"behavior_mode"`
	StatusCode      int               `json:"status_code"`
	ResponseBody    string            `json:"response_body"`
	ResponseHeaders map[string]string `json:"response_headers"`
	SoapVersion     string            `json:"soap_version"`
	ResponseDelayMs int               `json:"response_delay_ms"`
}

const defaultSOAP11Response = `<?xml version="1.0" encoding="UTF-8"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Header/><soap:Body><Response><Status>OK</Status></Response></soap:Body></soap:Envelope>`
const defaultSOAP12Response = `<?xml version="1.0" encoding="UTF-8"?><env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"><env:Header/><env:Body><Response><Status>OK</Status></Response></env:Body></env:Envelope>`

func soapFault(version, message string) string {
	if version == "1.2" {
		return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"><env:Body><env:Fault><env:Code><env:Value>env:Sender</env:Value></env:Code><env:Reason><env:Text xml:lang="en">%s</env:Text></env:Reason></env:Fault></env:Body></env:Envelope>`, message)
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><soap:Fault><faultcode>soap:Client</faultcode><faultstring>%s</faultstring></soap:Fault></soap:Body></soap:Envelope>`, message)
}

func contentTypeForVersion(version string) string {
	if version == "1.2" {
		return "application/soap+xml; charset=utf-8"
	}
	return "text/xml; charset=utf-8"
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

	log.Printf("SOAP Adapter started (ID: %s)", adapterID)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handleRequest(w, r, adapterID, controlPlaneURL)
	})

	log.Printf("SOAP Adapter listening on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func handleRequest(w http.ResponseWriter, r *http.Request, adapterID, controlPlaneURL string) {
	config, err := fetchConfig(adapterID, controlPlaneURL)
	if err != nil {
		log.Printf("Error fetching config: %v", err)
		http.Error(w, "Failed to fetch configuration", http.StatusInternalServerError)
		return
	}

	version := config.SoapVersion
	if version == "" {
		version = "1.1"
	}
	ct := contentTypeForVersion(version)

	if r.Method == http.MethodPost {
		reqCT := r.Header.Get("Content-Type")
		if !strings.Contains(reqCT, "xml") && !strings.Contains(reqCT, "soap") {
			w.Header().Set("Content-Type", ct)
			w.WriteHeader(http.StatusUnsupportedMediaType)
			w.Write([]byte(soapFault(version, "Content-Type must be text/xml or application/soap+xml")))
			return
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "Envelope") {
			w.Header().Set("Content-Type", ct)
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(soapFault(version, "Request must contain a SOAP Envelope")))
			return
		}
	}

	if config.ResponseDelayMs > 0 {
		time.Sleep(time.Duration(config.ResponseDelayMs) * time.Millisecond)
	}

	for k, v := range config.ResponseHeaders {
		w.Header().Set(k, v)
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", ct)
	}

	statusCode := config.StatusCode
	if statusCode == 0 {
		statusCode = 200
	}
	responseBody := config.ResponseBody
	if responseBody == "" {
		if version == "1.2" {
			responseBody = defaultSOAP12Response
		} else {
			responseBody = defaultSOAP11Response
		}
	}

	w.WriteHeader(statusCode)
	w.Write([]byte(responseBody))
	log.Printf("[%s] %s - %d", r.Method, r.RequestURI, statusCode)
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
