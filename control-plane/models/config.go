package models

type AdapterConfig struct {
	// REST/OData/SOAP/AS2/AS4/EDIFACT — shared HTTP response fields
	StatusCode      int               `json:"status_code"`
	ResponseBody    string            `json:"response_body"`
	ResponseHeaders map[string]string `json:"response_headers"`
	ResponseDelayMs int               `json:"response_delay_ms"`

	// SFTP specific
	Files                []FileConfig `json:"files"`
	AuthMode             string       `json:"auth_mode"`              // "success", "failure"
	SSHHostKey            string       `json:"ssh_host_key"`            // PEM-encoded RSA private key
	SSHHostKeyFingerprint string       `json:"ssh_host_key_fingerprint"` // SHA256 fingerprint for display
	SSHPublicKey          string       `json:"ssh_public_key"`           // authorized_keys format; if empty, accept any public key

	// SOAP/XI specific
	SoapVersion string `json:"soap_version"` // "1.1" or "1.2"

	// AS2 specific
	AS2From string `json:"as2_from"` // Expected sender AS2 ID
	AS2To   string `json:"as2_to"`   // Our AS2 ID

	// AS4 specific
	AS4PartyID string `json:"as4_party_id"` // Our ebMS3 PartyId

	// EDIFACT/X12 specific
	EDIStandard  string `json:"edi_standard"`   // "EDIFACT" or "X12"
	EDISenderID  string `json:"edi_sender_id"`  // ACK sender ID (default: STUBSND)
	EDIReceiverID string `json:"edi_receiver_id"` // ACK receiver ID (default: STUBRCV)

	// Sender adapter specific (REST-SENDER, SOAP-SENDER, XI-SENDER)
	TargetURL      string            `json:"target_url"`
	Method         string            `json:"method"`          // HTTP method, default POST
	RequestBody    string            `json:"request_body"`
	RequestHeaders map[string]string `json:"request_headers"`

	// CSRF token pre-fetch (SAP OData / CPI pattern)
	CSRFEnabled     bool   `json:"csrf_enabled"`
	CSRFFetchURL    string `json:"csrf_fetch_url"`    // defaults to target_url if empty
	CSRFFetchMethod string `json:"csrf_fetch_method"` // HEAD or GET, default HEAD
}

type FileConfig struct {
	Name    string `json:"name"`
	Content string `json:"content"` // base64 encoded or plain text
}
