package board

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// MaxPriority is the lowest priority; 1 is the highest.
const MaxPriority = 4

// PriorityFile is the file, in the deck's state folder, that keeps the
// priorities the user gave tasks. The board knows nothing of them.
const PriorityFile = "priorities.json"

// The priorities' names, as Linear names its own: 1 is the most urgent.
var priorityNames = [MaxPriority + 1]string{"No priority", "Urgent", "High", "Medium", "Low"}

// PriorityName is the priority's name: "Urgent" for 1, …, "Low" for 4,
// "No priority" for 0.
func PriorityName(p int) string {
	if p < 0 || p > MaxPriority {
		p = 0
	}
	return priorityNames[p]
}

// PriorityList is the deck's list heading for a priority: "P1 Urgent", or
// "No priority".
func PriorityList(p int) string {
	if p < 1 || p > MaxPriority {
		return PriorityName(0)
	}
	return fmt.Sprintf("P%d %s", p, PriorityName(p))
}

// Priorities keeps the deck's own priority per board task id
// ("webapp/7"), in a JSON file of the deck's state folder.
type Priorities struct {
	Path string
	mu   sync.Mutex
}

// Load reads the priorities; a missing file is none.
func (p *Priorities) Load() (map[string]int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.load()
}

func (p *Priorities) load() (map[string]int, error) {
	m := map[string]int{}
	data, err := os.ReadFile(p.Path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]int{}, fmt.Errorf("%s: %w", filepath.Base(p.Path), err)
	}
	return m, nil
}

// Set gives the task id priority prio, 1–MaxPriority; 0 takes its
// priority away. The file is replaced in one step.
func (p *Priorities) Set(id string, prio int) error {
	if prio < 0 || prio > MaxPriority {
		return fmt.Errorf("priority %d is not 0–%d", prio, MaxPriority)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	m, err := p.load()
	if err != nil {
		return err
	}
	if prio == 0 {
		delete(m, id)
	} else {
		m[id] = prio
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.Path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p.Path), ".priorities-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p.Path)
}
