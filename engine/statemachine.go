package engine

import (
	"fmt"
	"time"

	"github.com/L1ttlebear/ippool/database/auditlog"
	"github.com/L1ttlebear/ippool/database/models"
	"gorm.io/gorm"
)

// validTransitions defines the legal state transition matrix.
// Legal transitions:
//
//	Ready -> Full:  traffic threshold exceeded
//	Ready -> Dead:  health check failed
//	Full  -> Ready: traffic recovered + health check passed
//	Full  -> Dead:  health check failed
//	Dead  -> Ready: health check passed + traffic not exceeded
var validTransitions = map[models.HostState][]models.HostState{
	models.StateReady: {models.StateFull, models.StateDead},
	models.StateFull:  {models.StateReady, models.StateDead},
	models.StateDead:  {models.StateReady},
}

// IsValidTransition checks whether a state transition is legal.
func IsValidTransition(from, to models.HostState) bool {
	targets, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, t := range targets {
		if t == to {
			return true
		}
	}
	return false
}

// StateMachine manages host state transitions.
type StateMachine struct {
	hub WSHub
}

// NewStateMachine creates a new StateMachine.
func NewStateMachine(hub WSHub) *StateMachine {
	return &StateMachine{hub: hub}
}

// Transition validates and applies a state transition, writes auditlog, and broadcasts a WS event.
func (sm *StateMachine) Transition(db *gorm.DB, hostID uint, newState models.HostState, reason string) error {
	var host models.Host
	if err := db.First(&host, hostID).Error; err != nil {
		return fmt.Errorf("host %d not found: %w", hostID, err)
	}

	if host.State == newState {
		return nil
	}

	if !IsValidTransition(host.State, newState) {
		return fmt.Errorf("invalid transition %s -> %s for host %d", host.State, newState, hostID)
	}

	oldState := host.State
	now := time.Now()

	if err := db.Model(&host).Updates(map[string]any{
		"state":             newState,
		"last_state_change": now,
	}).Error; err != nil {
		return fmt.Errorf("failed to update host state: %w", err)
	}

	msg := fmt.Sprintf("host %d (%s): %s -> %s | reason: %s", hostID, host.Name, oldState, newState, reason)
	auditlog.EventLog("state_transition", msg)

	if sm.hub != nil {
		sm.hub.Broadcast(map[string]any{
			"type": "state_change",
			"data": map[string]any{
				"host_id":   hostID,
				"old_state": oldState,
				"new_state": newState,
				"reason":    reason,
				"time":      now,
			},
		})
	}

	return nil
}

// ForceSet bypasses the transition matrix and directly sets the host state.
func (sm *StateMachine) ForceSet(db *gorm.DB, hostID uint, state models.HostState) error {
	now := time.Now()
	if err := db.Model(&models.Host{}).Where("id = ?", hostID).Updates(map[string]any{
		"state":             state,
		"last_state_change": now,
	}).Error; err != nil {
		return fmt.Errorf("failed to force-set host state: %w", err)
	}
	auditlog.EventLog("state_transition", fmt.Sprintf("host %d force-set to %s", hostID, state))
	return nil
}

// ApplyCheckResult decides state transitions based on a health check result.
func (sm *StateMachine) ApplyCheckResult(db *gorm.DB, host models.Host, result CheckResult) error {
	if !result.Reachable || !result.SSHReachable {
		reason := result.Error
		if reason == "" {
			reason = result.SSHError
		}
		if reason == "" {
			reason = "health/ssh check failed"
		}
		if host.State != models.StateDead {
			return sm.Transition(db, host.ID, models.StateDead, "health check failed: "+reason)
		}
		return nil
	}

	trafficExceeded := host.TrafficThreshold > 0 &&
		max64(result.TrafficIn, result.TrafficOut) > host.TrafficThreshold

	switch host.State {
	case models.StateReady:
		if trafficExceeded {
			return sm.Transition(db, host.ID, models.StateFull, "traffic threshold exceeded")
		}
	case models.StateFull:
		if !trafficExceeded {
			return sm.Transition(db, host.ID, models.StateReady, "traffic recovered")
		}
	case models.StateDead:
		if !trafficExceeded {
			return sm.Transition(db, host.ID, models.StateReady, "health check recovered")
		}
		return sm.Transition(db, host.ID, models.StateFull, "health check recovered but traffic exceeded")
	}
	return nil
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
