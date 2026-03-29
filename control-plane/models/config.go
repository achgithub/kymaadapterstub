package models

type AdapterConfig struct {
	// REST/OData specific
	StatusCode      int               `json:"status_code"`     // e.g., 200, 401, 404
	ResponseBody    string            `json:"response_body"`   // JSON/XML payload
	ResponseHeaders map[string]string `json:"response_headers"`

	// SFTP specific
	Files    []FileConfig `json:"files"`
	AuthMode string       `json:"auth_mode"` // "success", "failure"
}

type FileConfig struct {
	Name    string `json:"name"`
	Content string `json:"content"` // base64 encoded or plain text
}
