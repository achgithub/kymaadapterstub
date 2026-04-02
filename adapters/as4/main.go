package main

// AS4 adapter — implements AS4 (ebMS3) message reception.
// AS4 is SOAP 1.2 based messaging with ebMS3 envelope headers. CPI sends an AS4 UserMessage
// inside a SOAP 1.2 envelope with eb3:Messaging in the SOAP header. This stub validates
// the envelope and returns an AS4 Receipt Signal to acknowledge receipt.

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
	AS4PartyID      string            `json:"as4_party_id"`
}

// ebMS3 namespace used in AS4 messages
const ebms3NS = "http://docs.oasis-open.org/ebxml-msg/ebms/v3.0/ns/core/200704/"

func receiptSignal(partyID, refMessageID string) string {
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	msgID := fmt.Sprintf("receipt-%d@%s", time.Now().UnixNano(), partyID)
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>`+
		`<S12:Envelope xmlns:S12="http://www.w3.org/2003/05/soap-envelope" `+
		`xmlns:eb3="http://docs.oasis-open.org/ebxml-msg/ebms/v3.0/ns/core/200704/">`+
		`<S12:Header>`+
		`<eb3:Messaging S12:mustUnderstand="true">`+
		`<eb3:SignalMessage>`+
		`<eb3:MessageInfo><eb3:Timestamp>%s</eb3:Timestamp><eb3:MessageId>%s</eb3:MessageId><eb3:RefToMessageId>%s</eb3:RefToMessageId></eb3:MessageInfo>`+
		`<eb3:Receipt><ebbp:NonRepudiationInformation xmlns:ebbp="http://docs.oasis-open.org/ebxml-bp/ebbp-signals-2.0"/></eb3:Receipt>`+
		`</eb3:SignalMessage>`+
		`</eb3:Messaging>`+
		`</S12:Header>`+
		`<S12:Body/>`+
		`</S12:Envelope>`,
		ts, msgID, refMessageID)
}

func soapFault(message string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"><env:Body><env:Fault><env:Code><env:Value>env:Sender</env:Value></env:Code><env:Reason><env:Text xml:lang="en">%s</env:Text></env:Reason></env:Fault></env:Body></env:Envelope>`, message)
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

	log.Printf("AS4 Adapter started (ID: %s)", adapterID)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handleRequest(w, r, adapterID, controlPlaneURL)
	})

	log.Printf("AS4 Adapter listening on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func reportActivity(adapterID, controlPlaneURL string) {
	go func() {
		c := &http.Client{Timeout: 2 * time.Second}
		c.Post(fmt.Sprintf("%s/api/adapter-activity/%s", controlPlaneURL, adapterID), "application/json", nil)
	}()
}

func handleRequest(w http.ResponseWriter, r *http.Request, adapterID, controlPlaneURL string) {
	reportActivity(adapterID, controlPlaneURL)
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(soapFault("AS4 requires HTTP POST")))
		return
	}

	config, err := fetchConfig(adapterID, controlPlaneURL)
	if err != nil {
		log.Printf("Error fetching config: %v", err)
		http.Error(w, "Failed to fetch configuration", http.StatusInternalServerError)
		return
	}

	const ct = "application/soap+xml; charset=utf-8"

	// Validate Content-Type
	reqCT := r.Header.Get("Content-Type")
	if !strings.Contains(reqCT, "xml") && !strings.Contains(reqCT, "soap") {
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(http.StatusUnsupportedMediaType)
		w.Write([]byte(soapFault("Content-Type must be application/soap+xml")))
		return
	}

	body, _ := io.ReadAll(r.Body)
	bodyStr := string(body)

	// Validate SOAP 1.2 envelope
	if !strings.Contains(bodyStr, "Envelope") {
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(soapFault("Request must contain a SOAP Envelope")))
		return
	}

	// Check for ebMS3 Messaging header
	if !strings.Contains(bodyStr, ebms3NS) && !strings.Contains(bodyStr, "eb3:Messaging") && !strings.Contains(bodyStr, "eb:Messaging") {
		log.Printf("Warning: ebMS3 Messaging header not found — accepting anyway (stub mode)")
	}

	// Extract RefToMessageId from the body (simple string search)
	refMessageID := extractBetween(bodyStr, "<eb3:MessageId>", "</eb3:MessageId>")
	if refMessageID == "" {
		refMessageID = extractBetween(bodyStr, "<eb:MessageId>", "</eb:MessageId>")
	}
	if refMessageID == "" {
		refMessageID = "unknown"
	}

	log.Printf("AS4 UserMessage received: RefMsgID=%s", refMessageID)

	if config.ResponseDelayMs > 0 {
		time.Sleep(time.Duration(config.ResponseDelayMs) * time.Millisecond)
	}

	partyID := config.AS4PartyID
	if partyID == "" {
		partyID = "kyma-stub"
	}

	// If a custom response body is configured, return it
	if config.ResponseBody != "" {
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
		w.WriteHeader(statusCode)
		w.Write([]byte(config.ResponseBody))
		return
	}

	// Return AS4 Receipt Signal
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(receiptSignal(partyID, refMessageID)))
	log.Printf("[POST] %s - 200 (AS4 Receipt sent)", r.RequestURI)
}

// extractBetween returns the content between two string markers (first occurrence)
func extractBetween(s, start, end string) string {
	si := strings.Index(s, start)
	if si == -1 {
		return ""
	}
	si += len(start)
	ei := strings.Index(s[si:], end)
	if ei == -1 {
		return ""
	}
	return s[si : si+ei]
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
