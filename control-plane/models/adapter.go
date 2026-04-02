package models

import "time"

type Adapter struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`            // e.g., "payment-service"
	Type           string        `json:"type"`            // "REST", "SFTP", "OData"
	BehaviorMode   string        `json:"behavior_mode"`   // "success", "failure"
	Config         AdapterConfig `json:"config"`
	Status         string        `json:"status"`          // "stopped", "starting", "running"
	IngressURL     string        `json:"ingress_url"`     // Generated after deployment
	Credentials    *Credentials  `json:"credentials"`     // For SFTP
	DeploymentName string        `json:"deployment_name"` // K8s resource name
	LastActivity   *time.Time    `json:"last_activity"`   // Last time a request was received
}

type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type CreateAdapterRequest struct {
	Name         string        `json:"name"`
	Type         string        `json:"type"`
	BehaviorMode string        `json:"behavior_mode"`
	Config       AdapterConfig `json:"config"`
	Credentials  *Credentials  `json:"credentials"`
}

type UpdateAdapterRequest struct {
	BehaviorMode string        `json:"behavior_mode"`
	Config       AdapterConfig `json:"config"`
}
