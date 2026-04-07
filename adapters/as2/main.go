package main

// AS2 adapter — implements AS2 (Applicability Statement 2) message reception.
// AS2 is HTTP-based messaging used for B2B EDI exchanges. CPI sends an AS2 message
// as an HTTP POST with AS2-From, AS2-To, and Message-ID headers. This stub validates
// those headers and returns a synchronous MDN (Message Disposition Notification)
// to tell the sender the message was received successfully.

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
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
	ResponseDelayMs int               `json:"response_delay_ms"`
	AS2From         string            `json:"as2_from"` // Expected sender ID
	AS2To           string            `json:"as2_to"`   // Our AS2 ID
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
		handleRequest(w, r, resolveAdapterID(r, adapterID), controlPlaneURL)
	})

	log.Printf("AS2 Adapter listening on :8080")
	var serverHandler http.Handler = mux
	if os.Getenv("PATH_PREFIX_MODE") == "true" {
		serverHandler = withPathPrefixMiddleware(mux)
	}
	if err := http.ListenAndServe(":8080", serverHandler); err != nil {
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

	if !checkInboundAuth(w, r, config) {
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

	// Compute MIC (Message Integrity Check) — SHA-1 of body, base64 encoded
	h := sha1.New()
	h.Write(body)
	mic := base64.StdEncoding.EncodeToString(h.Sum(nil))

	origMsgID := messageID
	if origMsgID == "" {
		origMsgID = "unknown"
	}
	boundary := "KYMA_AS2_MDN_BOUNDARY"
	mdnBody := fmt.Sprintf(
		"--%s\r\nContent-Type: text/plain\r\n\r\nThe AS2 message was received and processed successfully.\r\n"+
			"--%s\r\nContent-Type: message/disposition-notification\r\n\r\n"+
			"Reporting-UA: KymaAdapterStub/1.0\r\n"+
			"Original-Recipient: rfc822; %s\r\n"+
			"Final-Recipient: rfc822; %s\r\n"+
			"Original-Message-ID: %s\r\n"+
			"Disposition: automatic-action/MDN-sent-automatically; processed\r\n"+
			"Received-content-MIC: %s, sha1\r\n"+
			"--%s--\r\n",
		boundary, boundary, ourID, ourID, origMsgID, mic, boundary,
	)

	w.Header().Set("AS2-Version", "1.2")
	w.Header().Set("AS2-From", ourID)
	w.Header().Set("AS2-To", as2From)
	w.Header().Set("Message-ID", fmt.Sprintf("<mdn-%d@kyma-stub>", time.Now().UnixNano()))
	w.Header().Set("MIME-Version", "1.0")
	w.Header().Set("Content-Type", fmt.Sprintf("multipart/report; report-type=disposition-notification; boundary=%q", boundary))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(mdnBody))

	log.Printf("[POST] %s - 200 (MDN sent, MIC=%s)", r.RequestURI, mic)
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
