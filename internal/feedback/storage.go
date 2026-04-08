package feedback

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Observation represents a single feedback observation about a person
type Observation struct {
	ID        string    `json:"id"`
	PersonID  string    `json:"person_id"`
	Text      string    `json:"text"`
	Type      string    `json:"type"` // "strength" or "growth"
	TaskID    string    `json:"task_id,omitempty"`
	TaskSeq   int       `json:"task_seq,omitempty"`
	TaskTitle string    `json:"task_title,omitempty"`
	Week      string    `json:"week"`  // ISO week: "2026-W13"
	Cycle     string    `json:"cycle"` // "2026-april" or "2026-november"
	CreatedAt time.Time `json:"created_at"`
}

// Person represents a collaborator in the registry
type Person struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Aliases   []string  `json:"aliases,omitempty"`
	Team      string    `json:"team,omitempty"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

// WeekReview tracks whether a week's review has been completed
type WeekReview struct {
	Week        string    `json:"week"`
	Status      string    `json:"status"` // "pending" or "completed"
	TasksWorked []string  `json:"tasks_worked,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

// State holds the current feedback state
type State struct {
	CurrentCycle string       `json:"current_cycle"`
	WeekReviews  []WeekReview `json:"week_reviews"`
	StartedAt    time.Time    `json:"started_at,omitempty"` // When user started tracking
}

// PeopleRegistry holds all known collaborators
type PeopleRegistry struct {
	People []Person `json:"people"`
}

// PersonSummary is an aggregated view of a person for a cycle
type PersonSummary struct {
	Person       Person        `json:"person"`
	Observations []Observation `json:"observations"`
	Strengths    []Observation `json:"strengths"`
	GrowthAreas  []Observation `json:"growth_areas"`
	Tasks        []TaskInfo    `json:"tasks"`
	TotalCount   int           `json:"total_count"`
}

// TaskInfo holds minimal task information
type TaskInfo struct {
	ID    string `json:"id"`
	Seq   int    `json:"seq"`
	Title string `json:"title"`
}

// Store handles feedback data persistence
type Store struct {
	baseDir string
}

// NewStore creates a new feedback store
func NewStore(oxHome string) *Store {
	return &Store{
		baseDir: filepath.Join(oxHome, "feedback"),
	}
}

// Init creates the feedback directory structure
func (s *Store) Init() error {
	dirs := []string{
		s.baseDir,
		filepath.Join(s.baseDir, "cycles"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}

	// Initialize empty files if they don't exist
	if _, err := os.Stat(s.peopleFile()); os.IsNotExist(err) {
		if err := s.savePeople(&PeopleRegistry{People: []Person{}}); err != nil {
			return err
		}
	}
	if _, err := os.Stat(s.stateFile()); os.IsNotExist(err) {
		if err := s.saveState(&State{CurrentCycle: s.getCurrentCycle()}); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) peopleFile() string {
	return filepath.Join(s.baseDir, "people.json")
}

func (s *Store) stateFile() string {
	return filepath.Join(s.baseDir, "state.json")
}

func (s *Store) observationsFile() string {
	return filepath.Join(s.baseDir, "observations.jsonl")
}

// GetCurrentCycle returns the current feedback cycle identifier
func (s *Store) getCurrentCycle() string {
	now := time.Now()
	month := now.Month()
	year := now.Year()

	// April cycle: Oct 1 - Mar 31 (feedback for Oct-Mar period, submitted in April)
	// November cycle: Apr 1 - Sep 30 (feedback for Apr-Sep period, submitted in November)
	if month >= time.April && month <= time.September {
		return fmt.Sprintf("%d-november", year)
	}
	if month >= time.October {
		return fmt.Sprintf("%d-april", year+1)
	}
	// Jan-Mar
	return fmt.Sprintf("%d-april", year)
}

// GetCurrentWeek returns the current ISO week
func (s *Store) GetCurrentWeek() string {
	year, week := time.Now().ISOWeek()
	return fmt.Sprintf("%d-W%02d", year, week)
}

// GetRecentWeeks returns the last n weeks (excluding current)
func (s *Store) GetRecentWeeks(n int) []string {
	var weeks []string
	now := time.Now()
	for i := 1; i <= n; i++ {
		d := now.AddDate(0, 0, -7*i)
		year, week := d.ISOWeek()
		weeks = append(weeks, fmt.Sprintf("%d-W%02d", year, week))
	}
	return weeks
}

// LoadPeople loads the people registry
func (s *Store) LoadPeople() (*PeopleRegistry, error) {
	data, err := os.ReadFile(s.peopleFile())
	if err != nil {
		if os.IsNotExist(err) {
			return &PeopleRegistry{People: []Person{}}, nil
		}
		return nil, err
	}

	var registry PeopleRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, err
	}
	return &registry, nil
}

func (s *Store) savePeople(registry *PeopleRegistry) error {
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.peopleFile(), data, 0644)
}

// AddPerson adds a new person to the registry
func (s *Store) AddPerson(name, team string) (*Person, error) {
	registry, err := s.LoadPeople()
	if err != nil {
		return nil, err
	}

	// Check if already exists
	id := slugify(name)
	for _, p := range registry.People {
		if p.ID == id {
			// Update last seen
			p.LastSeen = time.Now()
			return &p, nil
		}
	}

	person := Person{
		ID:        id,
		Name:      name,
		Team:      team,
		FirstSeen: time.Now(),
		LastSeen:  time.Now(),
	}
	registry.People = append(registry.People, person)

	if err := s.savePeople(registry); err != nil {
		return nil, err
	}
	return &person, nil
}

// GetPerson retrieves a person by ID
func (s *Store) GetPerson(id string) (*Person, error) {
	registry, err := s.LoadPeople()
	if err != nil {
		return nil, err
	}

	for _, p := range registry.People {
		if p.ID == id {
			return &p, nil
		}
	}
	return nil, fmt.Errorf("person not found: %s", id)
}

// SearchPeople searches for people by name (fuzzy match)
func (s *Store) SearchPeople(query string) ([]Person, error) {
	registry, err := s.LoadPeople()
	if err != nil {
		return nil, err
	}

	query = strings.ToLower(query)
	var results []Person
	for _, p := range registry.People {
		if strings.Contains(strings.ToLower(p.Name), query) {
			results = append(results, p)
			continue
		}
		for _, alias := range p.Aliases {
			if strings.Contains(strings.ToLower(alias), query) {
				results = append(results, p)
				break
			}
		}
	}
	return results, nil
}

// LoadState loads the feedback state
func (s *Store) LoadState() (*State, error) {
	data, err := os.ReadFile(s.stateFile())
	if err != nil {
		if os.IsNotExist(err) {
			return &State{CurrentCycle: s.getCurrentCycle()}, nil
		}
		return nil, err
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (s *Store) saveState(state *State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.stateFile(), data, 0644)
}

// AddObservation adds a new observation
func (s *Store) AddObservation(obs Observation) error {
	if obs.ID == "" {
		obs.ID = uuid.New().String()
	}
	if obs.CreatedAt.IsZero() {
		obs.CreatedAt = time.Now()
	}
	if obs.Week == "" {
		obs.Week = s.GetCurrentWeek()
	}
	if obs.Cycle == "" {
		obs.Cycle = s.getCurrentCycle()
	}

	// Update person's last seen
	if obs.PersonID != "" {
		registry, err := s.LoadPeople()
		if err == nil {
			for i, p := range registry.People {
				if p.ID == obs.PersonID {
					registry.People[i].LastSeen = time.Now()
					s.savePeople(registry)
					break
				}
			}
		}
	}

	// Append to observations file
	f, err := os.OpenFile(s.observationsFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(obs)
	if err != nil {
		return err
	}
	_, err = f.WriteString(string(data) + "\n")
	return err
}

// LoadObservations loads all observations
func (s *Store) LoadObservations() ([]Observation, error) {
	data, err := os.ReadFile(s.observationsFile())
	if err != nil {
		if os.IsNotExist(err) {
			return []Observation{}, nil
		}
		return nil, err
	}

	var observations []Observation
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		var obs Observation
		if err := json.Unmarshal([]byte(line), &obs); err != nil {
			continue // Skip malformed lines
		}
		observations = append(observations, obs)
	}
	return observations, nil
}

// GetObservationsForCycle returns observations for a specific cycle
func (s *Store) GetObservationsForCycle(cycle string) ([]Observation, error) {
	all, err := s.LoadObservations()
	if err != nil {
		return nil, err
	}

	var filtered []Observation
	for _, obs := range all {
		if obs.Cycle == cycle {
			filtered = append(filtered, obs)
		}
	}
	return filtered, nil
}

// GetObservationsForPerson returns observations for a specific person in current cycle
func (s *Store) GetObservationsForPerson(personID string) ([]Observation, error) {
	cycle := s.getCurrentCycle()
	all, err := s.GetObservationsForCycle(cycle)
	if err != nil {
		return nil, err
	}

	var filtered []Observation
	for _, obs := range all {
		if obs.PersonID == personID {
			filtered = append(filtered, obs)
		}
	}
	return filtered, nil
}

// GetPersonSummaries returns aggregated summaries for all people in current cycle
func (s *Store) GetPersonSummaries() ([]PersonSummary, error) {
	cycle := s.getCurrentCycle()
	observations, err := s.GetObservationsForCycle(cycle)
	if err != nil {
		return nil, err
	}

	registry, err := s.LoadPeople()
	if err != nil {
		return nil, err
	}

	// Group observations by person
	byPerson := make(map[string][]Observation)
	for _, obs := range observations {
		byPerson[obs.PersonID] = append(byPerson[obs.PersonID], obs)
	}

	// Build summaries
	var summaries []PersonSummary
	for _, person := range registry.People {
		obs := byPerson[person.ID]
		if len(obs) == 0 {
			continue
		}

		summary := PersonSummary{
			Person:       person,
			Observations: obs,
			TotalCount:   len(obs),
		}

		// Separate strengths and growth areas
		taskMap := make(map[string]TaskInfo)
		for _, o := range obs {
			if o.Type == "strength" {
				summary.Strengths = append(summary.Strengths, o)
			} else {
				summary.GrowthAreas = append(summary.GrowthAreas, o)
			}
			if o.TaskID != "" {
				taskMap[o.TaskID] = TaskInfo{
					ID:    o.TaskID,
					Seq:   o.TaskSeq,
					Title: o.TaskTitle,
				}
			}
		}

		for _, t := range taskMap {
			summary.Tasks = append(summary.Tasks, t)
		}

		summaries = append(summaries, summary)
	}

	// Sort by total observations (most first)
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].TotalCount > summaries[j].TotalCount
	})

	return summaries, nil
}

// GetWeekReview gets or creates a week review
func (s *Store) GetWeekReview(week string) (*WeekReview, error) {
	state, err := s.LoadState()
	if err != nil {
		return nil, err
	}

	for _, wr := range state.WeekReviews {
		if wr.Week == week {
			return &wr, nil
		}
	}

	return &WeekReview{
		Week:   week,
		Status: "pending",
	}, nil
}

// CompleteWeekReview marks a week as reviewed
func (s *Store) CompleteWeekReview(week string, tasks []string) error {
	state, err := s.LoadState()
	if err != nil {
		return err
	}

	found := false
	for i, wr := range state.WeekReviews {
		if wr.Week == week {
			state.WeekReviews[i].Status = "completed"
			state.WeekReviews[i].CompletedAt = time.Now()
			state.WeekReviews[i].TasksWorked = tasks
			found = true
			break
		}
	}

	if !found {
		state.WeekReviews = append(state.WeekReviews, WeekReview{
			Week:        week,
			Status:      "completed",
			CompletedAt: time.Now(),
			TasksWorked: tasks,
		})
	}

	return s.saveState(state)
}

// GetPendingWeeks returns weeks that haven't been reviewed
func (s *Store) GetPendingWeeks() ([]string, error) {
	state, err := s.LoadState()
	if err != nil {
		return nil, err
	}

	completedWeeks := make(map[string]bool)
	for _, wr := range state.WeekReviews {
		if wr.Status == "completed" {
			completedWeeks[wr.Week] = true
		}
	}

	// Only track from when user started (no historical backlog)
	startDate := state.StartedAt
	if startDate.IsZero() {
		// First time - set started_at to now (no backlog)
		state.StartedAt = time.Now()
		s.saveState(state)
		return []string{}, nil
	}

	now := time.Now()
	var pending []string
	for d := startDate; d.Before(now); d = d.AddDate(0, 0, 7) {
		year, week := d.ISOWeek()
		weekStr := fmt.Sprintf("%d-W%02d", year, week)
		if !completedWeeks[weekStr] {
			pending = append(pending, weekStr)
		}
	}

	return pending, nil
}

func (s *Store) getCycleStartDate() time.Time {
	now := time.Now()
	month := now.Month()
	year := now.Year()

	if month >= time.April && month <= time.September {
		// November cycle started Apr 1
		return time.Date(year, time.April, 1, 0, 0, 0, 0, time.Local)
	}
	if month >= time.October {
		// April cycle started Oct 1
		return time.Date(year, time.October, 1, 0, 0, 0, 0, time.Local)
	}
	// Jan-Mar: April cycle started Oct 1 of previous year
	return time.Date(year-1, time.October, 1, 0, 0, 0, 0, time.Local)
}

// GetAvailableCycles returns all cycles that have observations
func (s *Store) GetAvailableCycles() ([]string, error) {
	observations, err := s.LoadObservations()
	if err != nil {
		return nil, err
	}

	cycleSet := make(map[string]bool)
	for _, obs := range observations {
		if obs.Cycle != "" {
			cycleSet[obs.Cycle] = true
		}
	}

	// Add current cycle even if no observations
	cycleSet[s.getCurrentCycle()] = true

	var cycles []string
	for c := range cycleSet {
		cycles = append(cycles, c)
	}

	// Sort cycles (newest first)
	sort.Slice(cycles, func(i, j int) bool {
		return cycles[i] > cycles[j]
	})

	return cycles, nil
}

// GetObservation returns a single observation by ID
func (s *Store) GetObservation(id string) (*Observation, error) {
	observations, err := s.LoadObservations()
	if err != nil {
		return nil, err
	}

	for _, obs := range observations {
		if obs.ID == id {
			return &obs, nil
		}
	}
	return nil, fmt.Errorf("observation not found: %s", id)
}

// UpdateObservation updates an existing observation
func (s *Store) UpdateObservation(updated Observation) error {
	observations, err := s.LoadObservations()
	if err != nil {
		return err
	}

	found := false
	for i, obs := range observations {
		if obs.ID == updated.ID {
			// Preserve created_at
			updated.CreatedAt = obs.CreatedAt
			observations[i] = updated
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("observation not found: %s", updated.ID)
	}

	return s.saveAllObservations(observations)
}

// DeleteObservation removes an observation by ID
func (s *Store) DeleteObservation(id string) error {
	observations, err := s.LoadObservations()
	if err != nil {
		return err
	}

	var filtered []Observation
	found := false
	for _, obs := range observations {
		if obs.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, obs)
	}

	if !found {
		return fmt.Errorf("observation not found: %s", id)
	}

	return s.saveAllObservations(filtered)
}

// saveAllObservations rewrites the observations file
func (s *Store) saveAllObservations(observations []Observation) error {
	f, err := os.Create(s.observationsFile())
	if err != nil {
		return err
	}
	defer f.Close()

	for _, obs := range observations {
		data, err := json.Marshal(obs)
		if err != nil {
			return err
		}
		if _, err := f.WriteString(string(data) + "\n"); err != nil {
			return err
		}
	}
	return nil
}

// GetPersonSummariesForCycle returns aggregated summaries for a specific cycle
func (s *Store) GetPersonSummariesForCycle(cycle string) ([]PersonSummary, error) {
	observations, err := s.GetObservationsForCycle(cycle)
	if err != nil {
		return nil, err
	}

	registry, err := s.LoadPeople()
	if err != nil {
		return nil, err
	}

	// Group observations by person
	byPerson := make(map[string][]Observation)
	for _, obs := range observations {
		byPerson[obs.PersonID] = append(byPerson[obs.PersonID], obs)
	}

	// Build summaries
	var summaries []PersonSummary
	for _, person := range registry.People {
		obs := byPerson[person.ID]
		if len(obs) == 0 {
			continue
		}

		summary := PersonSummary{
			Person:       person,
			Observations: obs,
			TotalCount:   len(obs),
		}

		// Separate strengths and growth areas
		taskMap := make(map[string]TaskInfo)
		for _, o := range obs {
			if o.Type == "strength" {
				summary.Strengths = append(summary.Strengths, o)
			} else {
				summary.GrowthAreas = append(summary.GrowthAreas, o)
			}
			if o.TaskID != "" {
				taskMap[o.TaskID] = TaskInfo{
					ID:    o.TaskID,
					Seq:   o.TaskSeq,
					Title: o.TaskTitle,
				}
			}
		}

		for _, t := range taskMap {
			summary.Tasks = append(summary.Tasks, t)
		}

		summaries = append(summaries, summary)
	}

	// Sort by total observations (most first)
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].TotalCount > summaries[j].TotalCount
	})

	return summaries, nil
}

// GetObservationsForPersonInCycle returns observations for a specific person in a specific cycle
func (s *Store) GetObservationsForPersonInCycle(personID, cycle string) ([]Observation, error) {
	all, err := s.GetObservationsForCycle(cycle)
	if err != nil {
		return nil, err
	}

	var filtered []Observation
	for _, obs := range all {
		if obs.PersonID == personID {
			filtered = append(filtered, obs)
		}
	}
	return filtered, nil
}

// GetCurrentCyclePublic returns the current cycle (public method)
func (s *Store) GetCurrentCyclePublic() string {
	return s.getCurrentCycle()
}

func slugify(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, "'", "")
	return s
}
