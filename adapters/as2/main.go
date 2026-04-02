package main

// AS2 adapter — implements AS2 (Applicability Statement 2) message reception.
// AS2 is HTTP-based messaging used for B2B EDI exchanges. CPI sends an AS2 message
// as an HTTP POST with AS2-From, AS2-To, and Message-ID headers. This stub validates
// those headers and returns a synchronous MDN (Message Disposition Notification)
// to tell the sender the message was received successfully.

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
	AS2From         string            `json:"as2_from"` // Expected sender ID
	AS2To           string            `json:"as2_to"`   // Our AS2 ID
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

	log.Printf("AS2 Adapter started (ID: %s)", adapterID)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handleRequest(w, r, adapterID, controlPlaneURL)
	})

	log.Printf("AS2 Adapter listening on :8080")
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
		http.Error(w, "AS2 requires POST", http.StatusMethodNotAllowed)
		return
	}

	config, err := fetchConfig(adapterID, controlPlaneURL)
	if err != nil {
		log.Printf("Error fetching config: %v", err)
		http.Error(w, "Failed to fetch configuration", http.StatusInternalServerError)
		return
	}

	// AS2 requires these headers
	as2From := r.Header.Get("AS2-From")
	as2To := r.Header.Get("AS2-To")
	messageID := r.Header.Get("Message-ID")

	if as2From == "" || as2To == "" {
		http.Error(w, "Missing required AS2 headers: AS2-From, AS2-To", http.StatusBadRequest)
		return
	}

	// Validate sender if configured
	if config.AS2From != "" && !strings.EqualFold(as2From, config.AS2From) {
		log.Printf("AS2-From mismatch: got %q, expected %q", as2From, config.AS2From)
		http.Error(w, "AS2-From identity mismatch", http.StatusForbidden)
		return
	}

	// Read the message body (we don't need to process it, just acknowledge)
	body, _ := io.ReadAll(r.Body)
	log.Printf("AS2 message received: From=%s To=%s MsgID=%s Size=%d", as2From, as2To, messageID, len(body))

	if config.ResponseDelayMs > 0 {
		time.Sleep(time.Duration(config.ResponseDelayMs) * time.Millisecond)
	}

	ourID := config.AS2To
	if ourID == "" {
		ourID = "kyma-stub"
	}

	// If a custom response body is configured, return it directly
	if config.ResponseBody != "" {
		for k, v := range config.ResponseHeaders {
			w.Header().Set(k, v)
		}
		statusCode := config.StatusCode
		if statusCode == 0 {
			statusCode = 200
		}
		w.WriteHeader(statusCode)
		w.Write([]byte(config.ResponseBody))
		return
	}

	// Return a proper synchronous MDN
	origMsgID := messageID
	if origMsgID == "" {
		origMsgID = "unknown"
	}
	boundary := "KYMA_AS2_MDN_BOUNDARY"
	mdnBody := fmt.Sprintf(
		"--%s\r\nContent-Type: text/plain\r\n\r\nThe AS2 message was received and processed successfully.\r\n--%s\r\nContent-Type: message/disposition-notification\r\n\r\nReporting-UA: KymaAdapterStub/1.0\r\nOriginal-Recipient: rfc822; %s\r\nFinal-Recipient: rfc822; %s\r\nOriginal-Message-ID: %s\r\nDisposition: automatic-action/MDN-sent-automatically; processed\r\n--%s--\r\n",
		boundary, boundary, ourID, ourID, origMsgID, boundary,
	)

	w.Header().Set("AS2-Version", "1.2")
	w.Header().Set("AS2-From", ourID)
	w.Header().Set("AS2-To", as2From)
	w.Header().Set("Message-ID", fmt.Sprintf("<mdn-%d@kyma-stub>", time.Now().UnixNano()))
	w.Header().Set("MIME-Version", "1.0")
	w.Header().Set("Content-Type", fmt.Sprintf("multipart/report; report-type=disposition-notification; boundary=%q", boundary))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(mdnBody))

	log.Printf("[POST] %s - 200 (MDN sent)", r.RequestURI)
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
