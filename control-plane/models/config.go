package models

type AdapterConfig struct {
	// REST/OData/SOAP/AS2/AS4/EDIFACT — shared HTTP response fields
	StatusCode      int               `json:"status_code"`
	ResponseBody    string            `json:"response_body"`
	ResponseHeaders map[string]string `json:"response_headers"`
	ResponseDelayMs int               `json:"response_delay_ms"`

	// SFTP specific
	Files    []FileConfig `json:"files"`
	AuthMode string       `json:"auth_mode"` // "success", "failure"

	// SOAP/XI specific
	SoapVersion string `json:"soap_version"` // "1.1" or "1.2"

	// AS2 specific
	AS2From string `json:"as2_from"` // Expected sender AS2 ID
	AS2To   string `json:"as2_to"`   // Our AS2 ID

	// AS4 specific
	AS4PartyID string `json:"as4_party_id"` // Our ebMS3 PartyId

	// EDIFACT/X12 specific
	EDIStandard string `json:"edi_standard"` // "EDIFACT" or "X12"
}

type FileConfig struct {
	Name    string `json:"name"`
	Content string `json:"content"` // base64 encoded or plain text
}
