package store

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/andrew/kymaadapterstub/control-plane/models"
)

type MemoryStore struct {
	scenarios  map[string]*models.Scenario
	startupLog []string
	mu         sync.RWMutex
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		scenarios:  make(map[string]*models.Scenario),
		startupLog: []string{},
	}
}

func (s *MemoryStore) AddStartupLog(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startupLog = append(s.startupLog, msg)
}

func (s *MemoryStore) GetStartupLog() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]string, len(s.startupLog))
	copy(result, s.startupLog)
	return result
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
			// Seed LastActivity when launched so idle clock starts from launch time
			if status == "running" {
				now := time.Now()
				scenario.Adapters[i].LastActivity = &now
			}
			scenario.UpdatedAt = time.Now()
			return nil
		}
	}

	return fmt.Errorf("adapter not found: %s", adapterID)
}

// RecordAdapterActivity updates LastActivity for an adapter by ID, searching all scenarios.
func (s *MemoryStore) RecordAdapterActivity(adapterID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, scenario := range s.scenarios {
		for i := range scenario.Adapters {
			if scenario.Adapters[i].ID == adapterID {
				scenario.Adapters[i].LastActivity = &now
				return
			}
		}
	}
}

// LoadGitHubScenario creates a read-only scenario from a GitHub scenario file.
// Skips silently if a scenario with the same ID already exists.
func (s *MemoryStore) LoadGitHubScenario(file models.ScenarioFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := strings.ToLower(strings.ReplaceAll(file.Name, " ", "-")) + "-github"
	// Strip any other characters invalid in a map key / display
	id = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, id)

	if _, exists := s.scenarios[id]; exists {
		return nil // already loaded, skip
	}

	adapters := make([]models.Adapter, 0, len(file.Adapters))
	for i, a := range file.Adapters {
		adapters = append(adapters, models.Adapter{
			ID:             fmt.Sprintf("%s-%s-%d", id, strings.ToLower(a.Type), i),
			Name:           a.Name,
			Type:           a.Type,
			BehaviorMode:   a.BehaviorMode,
			Config:         a.Config,
			Status:         "stopped",
			Credentials:    a.Credentials,
			DeploymentName: a.Name + "-deployment",
		})
	}

	s.scenarios[id] = &models.Scenario{
		ID:          id,
		Name:        file.Name,
		Description: file.Description,
		Adapters:    adapters,
		Status:      "stopped",
		Source:      "github",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	return nil
}

// CloneScenario creates a user-owned copy of any scenario (including GitHub ones).
// If name is empty, defaults to "<source name> (copy)".
func (s *MemoryStore) CloneScenario(sourceID string, name string) (*models.Scenario, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	source, exists := s.scenarios[sourceID]
	if !exists {
		return nil, fmt.Errorf("scenario not found: %s", sourceID)
	}

	newName := name
	if newName == "" {
		newName = source.Name + " (copy)"
	}
	newID := strings.ToLower(strings.ReplaceAll(newName, " ", "-")) +
		"-" + fmt.Sprintf("%d", time.Now().Unix())

	adapters := make([]models.Adapter, len(source.Adapters))
	for i, a := range source.Adapters {
		adapters[i] = models.Adapter{
			ID:             fmt.Sprintf("%s-%s-%d", newID, strings.ToLower(a.Type), i),
			Name:           a.Name,
			Type:           a.Type,
			BehaviorMode:   a.BehaviorMode,
			Config:         a.Config,
			Status:         "stopped",
			Credentials:    a.Credentials,
			DeploymentName: a.Name + "-deployment",
		}
	}

	newScenario := &models.Scenario{
		ID:          newID,
		Name:        newName,
		Description: source.Description,
		Adapters:    adapters,
		Status:      "stopped",
		Source:      "user",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	s.scenarios[newID] = newScenario
	return newScenario, nil
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
