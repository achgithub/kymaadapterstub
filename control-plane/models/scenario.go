package models

import "time"

type Scenario struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Adapters  []Adapter  `json:"adapters"`
	Status    string     `json:"status"` // "stopped", "running"
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type CreateScenarioRequest struct {
	Name string `json:"name"`
}

type UpdateScenarioRequest struct {
	Name string `json:"name"`
}
