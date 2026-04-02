package models

import "time"

type Scenario struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Adapters    []Adapter `json:"adapters"`
	Status      string    `json:"status"`  // "stopped", "running"
	Source      string    `json:"source"`  // "user" or "github"
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateScenarioRequest struct {
	Name string `json:"name"`
}

type UpdateScenarioRequest struct {
	Name string `json:"name"`
}

// ScenarioFile is the on-disk/GitHub format for a scenario.
// It contains no runtime fields (no IDs, status, ingress URLs).
type ScenarioFile struct {
	Version     int                   `json:"version"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Adapters    []ScenarioFileAdapter `json:"adapters"`
}

type ScenarioFileAdapter struct {
	Name         string        `json:"name"`
	Type         string        `json:"type"`
	Description  string        `json:"description"`
	BehaviorMode string        `json:"behavior_mode"`
	Config       AdapterConfig `json:"config"`
	Credentials  *Credentials  `json:"credentials"`
}

// ScenarioManifest is the index file fetched from the GitHub repo at startup.
type ScenarioManifest struct {
	Version   int      `json:"version"`
	Scenarios []string `json:"scenarios"` // relative paths to scenario JSON files
}
