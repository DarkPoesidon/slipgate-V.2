package actions

import (
	"fmt"
	"sync"
)

var (
	mu       sync.RWMutex
	registry = make(map[string]*Action)
	order    []string
)

// Register adds an action to the global registry.
func Register(a *Action) {
	mu.Lock()
	defer mu.Unlock()
	if _, exists := registry[a.ID]; exists {
		panic(fmt.Sprintf("action %q already registered", a.ID))
	}
	registry[a.ID] = a
	order = append(order, a.ID)
}

// Get returns an action by ID.
func Get(id string) (*Action, bool) {
	mu.RLock()
	defer mu.RUnlock()
	a, ok := registry[id]
	return a, ok
}

// All returns all registered actions in registration order.
func All() []*Action {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]*Action, 0, len(order))
	for _, id := range order {
		out = append(out, registry[id])
	}
	return out
}

// ByCategory returns actions grouped by category.
func ByCategory(category string) []*Action {
	mu.RLock()
	defer mu.RUnlock()
	var out []*Action
	for _, id := range order {
		if registry[id].Category == category {
			out = append(out, registry[id])
		}
	}
	return out
}
