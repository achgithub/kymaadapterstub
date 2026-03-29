package store

import (
	"fmt"
	"sync"
	"time"

	"github.com/andrew/kymaadapterstub/control-plane/models"
)

type MemoryStore struct {
	scenarios map[string]*models.Scenario
	mu        sync.RWMutex
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		scenarios: make(map[string]*models.Scenario),
	}
}

// Scenario operations
func (s *MemoryStore) CreateScenario(id, name string) (*models.Scenario, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.scenarios[id]; exists {
		return nil, fmt.Errorf("scenario already exists: %s", id)
	}

	scenario := &models.Scenario{
		ID:        id,
		Name:      name,
		Adapters:  []models.Adapter{},
		Status:    "stopped",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	s.scenarios[id] = scenario
	return scenario, nil
}

func (s *MemoryStore) GetScenario(id string) (*models.Scenario, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	scenario, exists := s.scenarios[id]
	if !exists {
		return nil, fmt.Errorf("scenario not found: %s", id)
	}

	return scenario, nil
}

func (s *MemoryStore) ListScenarios() ([]models.Scenario, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	scenarios := make([]models.Scenario, 0, len(s.scenarios))
	for _, scenario := range s.scenarios {
		scenarios = append(scenarios, *scenario)
	}

	return scenarios, nil
}

func (s *MemoryStore) UpdateScenario(id, name string) (*models.Scenario, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	scenario, exists := s.scenarios[id]
	if !exists {
		return nil, fmt.Errorf("scenario not found: %s", id)
	}

	scenario.Name = name
	scenario.UpdatedAt = time.Now()

	return scenario, nil
}

func (s *MemoryStore) DeleteScenario(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.scenarios[id]; !exists {
		return fmt.Errorf("scenario not found: %s", id)
	}

	delete(s.scenarios, id)
	return nil
}

func (s *MemoryStore) UpdateScenarioStatus(id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	scenario, exists := s.scenarios[id]
	if !exists {
		return fmt.Errorf("scenario not found: %s", id)
	}

	scenario.Status = status
	scenario.UpdatedAt = time.Now()

	return nil
}

// Adapter operations
func (s *MemoryStore) AddAdapter(scenarioID string, adapter models.Adapter) (*models.Adapter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	scenario, exists := s.scenarios[scenarioID]
	if !exists {
		return nil, fmt.Errorf("scenario not found: %s", scenarioID)
	}

	scenario.Adapters = append(scenario.Adapters, adapter)
	scenario.UpdatedAt = time.Now()

	return &adapter, nil
}

func (s *MemoryStore) GetAdapter(scenarioID, adapterID string) (*models.Adapter, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	scenario, exists := s.scenarios[scenarioID]
	if !exists {
		return nil, fmt.Errorf("scenario not found: %s", scenarioID)
	}

	for i := range scenario.Adapters {
		if scenario.Adapters[i].ID == adapterID {
			return &scenario.Adapters[i], nil
		}
	}

	return nil, fmt.Errorf("adapter not found: %s", adapterID)
}

func (s *MemoryStore) UpdateAdapter(scenarioID, adapterID string, updates models.Adapter) (*models.Adapter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	scenario, exists := s.scenarios[scenarioID]
	if !exists {
		return nil, fmt.Errorf("scenario not found: %s", scenarioID)
	}

	for i := range scenario.Adapters {
		if scenario.Adapters[i].ID == adapterID {
			scenario.Adapters[i].BehaviorMode = updates.BehaviorMode
			scenario.Adapters[i].Config = updates.Config
			scenario.UpdatedAt = time.Now()
			return &scenario.Adapters[i], nil
		}
	}

	return nil, fmt.Errorf("adapter not found: %s", adapterID)
}

func (s *MemoryStore) DeleteAdapter(scenarioID, adapterID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	scenario, exists := s.scenarios[scenarioID]
	if !exists {
		return fmt.Errorf("scenario not found: %s", scenarioID)
	}

	for i := range scenario.Adapters {
		if scenario.Adapters[i].ID == adapterID {
			scenario.Adapters = append(scenario.Adapters[:i], scenario.Adapters[i+1:]...)
			scenario.UpdatedAt = time.Now()
			return nil
		}
	}

	return fmt.Errorf("adapter not found: %s", adapterID)
}

func (s *MemoryStore) UpdateAdapterStatus(scenarioID, adapterID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	scenario, exists := s.scenarios[scenarioID]
	if !exists {
		return fmt.Errorf("scenario not found: %s", scenarioID)
	}

	for i := range scenario.Adapters {
		if scenario.Adapters[i].ID == adapterID {
			scenario.Adapters[i].Status = status
			scenario.UpdatedAt = time.Now()
			return nil
		}
	}

	return fmt.Errorf("adapter not found: %s", adapterID)
}

func (s *MemoryStore) UpdateAdapterIngressURL(scenarioID, adapterID, url string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	scenario, exists := s.scenarios[scenarioID]
	if !exists {
		return fmt.Errorf("scenario not found: %s", scenarioID)
	}

	for i := range scenario.Adapters {
		if scenario.Adapters[i].ID == adapterID {
			scenario.Adapters[i].IngressURL = url
			scenario.UpdatedAt = time.Now()
			return nil
		}
	}

	return fmt.Errorf("adapter not found: %s", adapterID)
}
