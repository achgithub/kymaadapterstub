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
	SoapVersion     string            `json:"soap_version"`
	ResponseDelayMs int               `json:"response_delay_ms"`
	Credentials     *Credentials      `json:"credentials"`
}

const defaultSOAP11Response = `<?xml version="1.0" encoding="UTF-8"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Header/><soap:Body><Response><Status>OK</Status></Response></soap:Body></soap:Envelope>`
const defaultSOAP12Response = `<?xml version="1.0" encoding="UTF-8"?><env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"><env:Header/><env:Body><Response><Status>OK</Status></Response></env:Body></env:Envelope>`

// SAP RM namespace for detecting SAP Reliable Messaging messages
const sapRMNamespace = "http://sap.com/xi/XI/System/"

func soapFault(version, message string) string {
	if version == "1.2" {
		return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"><env:Body><env:Fault><env:Code><env:Value>env:Sender</env:Value></env:Code><env:Reason><env:Text xml:lang="en">%s</env:Text></env:Reason></env:Fault></env:Body></env:Envelope>`, message)
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><soap:Fault><faultcode>soap:Client</faultcode><faultstring>%s</faultstring></soap:Fault></soap:Body></soap:Envelope>`, message)
}

// sapRMResponse returns a SOAP 1.1 envelope with an SAP RM header echoing the request MessageId.
func sapRMResponse(refMessageID string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>`+
		`<SOAP:Envelope xmlns:SOAP="http://schemas.xmlsoap.org/soap/envelope/" xmlns:SAP-RM="http://sap.com/xi/XI/System/">`+
		`<SOAP:Header>`+
		`<SAP-RM:MessageHeader SOAP:mustUnderstand="0">`+
		`<SAP-RM:Id>stub-response-%d</SAP-RM:Id>`+
		`<SAP-RM:RefToMessageId>%s</SAP-RM:RefToMessageId>`+
		`</SAP-RM:MessageHeader>`+
		`</SOAP:Header>`+
		`<SOAP:Body/>`+
		`</SOAP:Envelope>`, time.Now().UnixNano(), refMessageID)
}

func contentTypeForVersion(version string) string {
	if version == "1.2" {
		return "application/soap+xml; charset=utf-8"
	}
	return "text/xml; charset=utf-8"
}

// checkInboundAuth validates Basic Auth if credentials are configured.
// Returns false and writes 401 if auth fails.
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

	log.Printf("SOAP Adapter started (ID: %s)", adapterID)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handleRequest(w, r, resolveAdapterID(r, adapterID), controlPlaneURL)
	})

	log.Printf("SOAP Adapter listening on :8080")
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
	config, err := fetchConfig(adapterID, controlPlaneURL)
	if err != nil {
		log.Printf("Error fetching config: %v", err)
		http.Error(w, "Failed to fetch configuration", http.StatusInternalServerError)
		return
	}

	if !checkInboundAuth(w, r, config) {
		return
	}

	version := config.SoapVersion
	if version == "" {
		version = "1.1"
	}
	ct := contentTypeForVersion(version)

	var bodyStr string
	if r.Method == http.MethodPost {
		reqCT := r.Header.Get("Content-Type")
		if !strings.Contains(reqCT, "xml") && !strings.Contains(reqCT, "soap") {
			w.Header().Set("Content-Type", ct)
			w.WriteHeader(http.StatusUnsupportedMediaType)
			w.Write([]byte(soapFault(version, "Content-Type must be text/xml or application/soap+xml")))
			return
		}
		body, _ := io.ReadAll(r.Body)
		bodyStr = string(body)
		if !strings.Contains(bodyStr, "Envelope") {
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

	// Failure mode: auto-generate SOAP fault if no custom body set
	if config.BehaviorMode == "failure" && config.ResponseBody == "" {
		if statusCode == 200 {
			statusCode = 500
		}
		w.WriteHeader(statusCode)
		w.Write([]byte(soapFault(version, "Stub configured to return failure")))
		log.Printf("[%s] %s - %d (fault)", r.Method, r.RequestURI, statusCode)
		return
	}

	responseBody := config.ResponseBody

	// SAP RM: if no custom body and message contains SAP RM headers, echo the MessageId
	if responseBody == "" && strings.Contains(bodyStr, sapRMNamespace) {
		msgID := extractBetween(bodyStr, "<SAP-RM:Id>", "</SAP-RM:Id>")
		if msgID == "" {
			msgID = extractBetween(bodyStr, "<SAP-RM:MessageId>", "</SAP-RM:MessageId>")
		}
		if msgID != "" {
			log.Printf("SAP RM message detected, RefMsgId=%s", msgID)
			responseBody = sapRMResponse(msgID)
		}
	}

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
