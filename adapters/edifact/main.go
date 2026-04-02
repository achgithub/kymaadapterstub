package main

// EDIFACT/X12 adapter — accepts raw EDI payloads over HTTP and returns acknowledgements.
// EDIFACT messages start with a UNB segment. X12 messages start with an ISA segment.
// This stub auto-detects the standard from the body and returns:
//   - EDIFACT: CONTRL acknowledgement (functional ACK)
//   - X12: 997 Functional Acknowledgement
//
// A custom response_body in the config overrides the default ACK.

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
	ResponseDelayMs int               `json:"response_delay_ms"`
	EDIStandard     string            `json:"edi_standard"` // "EDIFACT" or "X12"
}

// CONTRL is the EDIFACT functional acknowledgement
// UCI+<interchange-ref>+<sender>:<qual>+<receiver>:<qual>+8 means "Accepted"
const defaultEDIFACTAck = "UNB+UNOA:3+STUBRCV:1+STUBSND:1+%s+00001'\nUNH+1+CONTRL:3:1:UN'\nUCI+00001+STUBSND:1+STUBRCV:1+8'\nUNT+2+1'\nUNZ+1+00001'\n"

// 997 is the X12 Functional Acknowledgement
const defaultX12Ack = "ISA*00*          *00*          *ZZ*STUBRCV        *ZZ*STUBSND        *%s*^*00501*000000001*0*P*:~\nGS*FA*STUBRCV*STUBSND*%s*1*X*005010X231A1~\nST*997*0001~\nAK1*ST*1~\nAK9*A*1*1*1~\nSE*4*0001~\nGE*1*1~\nIEA*1*000000001~\n"

func main() {
	adapterID := os.Getenv("ADAPTER_ID")
	controlPlaneURL := os.Getenv("CONTROL_PLANE_URL")

	if adapterID == "" {
		log.Fatal("ADAPTER_ID environment variable is required")
	}
	if controlPlaneURL == "" {
		controlPlaneURL = "http://control-plane:8080"
	}

	log.Printf("EDIFACT/X12 Adapter started (ID: %s)", adapterID)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handleRequest(w, r, adapterID, controlPlaneURL)
	})

	log.Printf("EDIFACT/X12 Adapter listening on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func handleRequest(w http.ResponseWriter, r *http.Request, adapterID, controlPlaneURL string) {
	if r.Method != http.MethodPost {
		http.Error(w, "EDI adapter requires POST", http.StatusMethodNotAllowed)
		return
	}

	config, err := fetchConfig(adapterID, controlPlaneURL)
	if err != nil {
		log.Printf("Error fetching config: %v", err)
		http.Error(w, "Failed to fetch configuration", http.StatusInternalServerError)
		return
	}

	body, _ := io.ReadAll(r.Body)
	bodyStr := strings.TrimSpace(string(body))

	// Auto-detect EDI standard if not configured
	standard := strings.ToUpper(config.EDIStandard)
	if standard == "" {
		if strings.HasPrefix(bodyStr, "UNB") || strings.HasPrefix(bodyStr, "unb") {
			standard = "EDIFACT"
		} else if strings.HasPrefix(bodyStr, "ISA") || strings.HasPrefix(bodyStr, "isa") {
			standard = "X12"
		} else {
			// Unknown format — accept it anyway (stub mode)
			standard = "EDIFACT"
			log.Printf("Warning: could not detect EDI standard from body, defaulting to EDIFACT")
		}
	}

	log.Printf("EDI %s message received, size=%d bytes", standard, len(body))

	if config.ResponseDelayMs > 0 {
		time.Sleep(time.Duration(config.ResponseDelayMs) * time.Millisecond)
	}

	// If custom response body is configured, return it
	if config.ResponseBody != "" {
		for k, v := range config.ResponseHeaders {
			w.Header().Set(k, v)
		}
		statusCode := config.StatusCode
		if statusCode == 0 {
			statusCode = 200
		}
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "text/plain")
		}
		w.WriteHeader(statusCode)
		w.Write([]byte(config.ResponseBody))
		return
	}

	// Return default acknowledgement
	now := time.Now().UTC()
	var ackBody string
	if standard == "X12" {
		dateStr := now.Format("060102")
		timeStr := now.Format("1504")
		ackBody = fmt.Sprintf(defaultX12Ack, dateStr+"*"+timeStr, now.Format("20060102"))
		w.Header().Set("Content-Type", "application/edi-x12")
	} else {
		dateStr := now.Format("060102") + ":" + now.Format("1504")
		ackBody = fmt.Sprintf(defaultEDIFACTAck, dateStr)
		w.Header().Set("Content-Type", "application/edifact")
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(ackBody))
	log.Printf("[POST] %s - 200 (%s ACK sent)", r.RequestURI, standard)
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
