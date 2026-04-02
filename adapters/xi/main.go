package main

// XI adapter — extends SOAP with SAP Process Integration (PI/XI) header validation.
// CPI sends messages over SOAP with SAP XI routing headers in the SOAP header block.
// This adapter validates the XI headers are present and returns a configured SOAP response.

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

// SAP XI uses SOAP 1.1 with the XI message namespace in the header block
const xiNamespace = "http://sap.com/xi/XI/Message/30"

const defaultXIResponse = `<?xml version="1.0" encoding="UTF-8"?><SOAP:Envelope xmlns:SOAP="http://schemas.xmlsoap.org/soap/envelope/" xmlns:xi="http://sap.com/xi/XI/Message/30"><SOAP:Header><xi:Main versionMajor="003" versionMinor="1" SOAP:mustUnderstand="1"><xi:MessageClass>ApplicationResponse</xi:MessageClass><xi:ProcessingMode>synchronous</xi:ProcessingMode><xi:MessageId>stub-response-id</xi:MessageId></xi:Main></SOAP:Header><SOAP:Body><Response><Status>OK</Status></Response></SOAP:Body></SOAP:Envelope>`

func soapFault(message string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><soap:Fault><faultcode>soap:Client</faultcode><faultstring>%s</faultstring></soap:Fault></soap:Body></soap:Envelope>`, message)
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

	log.Printf("XI Adapter started (ID: %s)", adapterID)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handleRequest(w, r, adapterID, controlPlaneURL)
	})

	log.Printf("XI Adapter listening on :8080")
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

	const ct = "text/xml; charset=utf-8"

	if r.Method == http.MethodPost {
		reqCT := r.Header.Get("Content-Type")
		if !strings.Contains(reqCT, "xml") && !strings.Contains(reqCT, "soap") {
			w.Header().Set("Content-Type", ct)
			w.WriteHeader(http.StatusUnsupportedMediaType)
			w.Write([]byte(soapFault("Content-Type must be text/xml")))
			return
		}

		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)

		if !strings.Contains(bodyStr, "Envelope") {
			w.Header().Set("Content-Type", ct)
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(soapFault("Request must contain a SOAP Envelope")))
			return
		}

		// XI messages must contain the SAP PI/XI namespace in the header
		if !strings.Contains(bodyStr, xiNamespace) && !strings.Contains(bodyStr, "xi:Main") && !strings.Contains(bodyStr, "XI/Message") {
			log.Printf("Warning: XI headers not found in request — accepting anyway (stub mode)")
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
		responseBody = defaultXIResponse
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
