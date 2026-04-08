package engine

import (
	"sync"

	"github.com/L1ttlebear/ippool/database/auditlog"
	"github.com/L1ttlebear/ippool/database/models"
)

// CircuitBreaker tracks whether all hosts are unavailable.
type CircuitBreaker struct {
	mu     sync.Mutex
	isOpen bool
}

// Check evaluates the host list and updates the circuit state.
// Returns (open, changed) where changed=true means the state flipped.
func (cb *CircuitBreaker) Check(hosts []models.Host) (open bool, changed bool) {
	// No hosts configured is not a circuit-open condition.
	if len(hosts) == 0 {
		cb.mu.Lock()
		defer cb.mu.Unlock()
		prev := cb.isOpen
		cb.isOpen = false
		changed = prev != cb.isOpen
		if changed {
			auditlog.EventLog("circuit_close", "circuit breaker closed: no hosts configured")
		}
		return cb.isOpen, changed
	}

	allDown := true
	for _, h := range hosts {
		if h.State == models.StateReady {
			allDown = false
			break
		}
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	prev := cb.isOpen
	cb.isOpen = allDown
	changed = prev != cb.isOpen

	if changed {
		if cb.isOpen {
			auditlog.EventLog("circuit_open", "circuit breaker opened: all hosts are Full or Dead")
		} else {
			auditlog.EventLog("circuit_close", "circuit breaker closed: at least one host is Ready")
		}
	}

	return cb.isOpen, changed
}

// IsOpen returns the current circuit state.
func (cb *CircuitBreaker) IsOpen() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.isOpen
}
