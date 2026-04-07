package engine

import (
	"fmt"
	"sort"

	"github.com/L1ttlebear/ippool/database/auditlog"
	"github.com/L1ttlebear/ippool/database/models"
)

// ElectionResult holds the outcome of a leader election round.
type ElectionResult struct {
	Leader     *models.Host
	Changed    bool
	PrevLeader *models.Host
}

// Elect selects the Ready host with the lowest Priority as leader.
// Returns ElectionResult{Leader: nil} if no Ready hosts exist.
func Elect(hosts []models.Host, currentLeaderID uint) ElectionResult {
	var ready []models.Host
	for _, h := range hosts {
		if h.State == models.StateReady {
			ready = append(ready, h)
		}
	}

	if len(ready) == 0 {
		return ElectionResult{Leader: nil}
	}

	sort.Slice(ready, func(i, j int) bool {
		return ready[i].Priority < ready[j].Priority
	})

	newLeader := &ready[0]

	var prevLeader *models.Host
	for i := range hosts {
		if hosts[i].ID == currentLeaderID {
			prevLeader = &hosts[i]
			break
		}
	}

	changed := currentLeaderID != newLeader.ID

	if changed {
		prevID := uint(0)
		if prevLeader != nil {
			prevID = prevLeader.ID
		}
		auditlog.EventLog("leader_changed", fmt.Sprintf(
			"leader changed: %d -> %d (priority %d)", prevID, newLeader.ID, newLeader.Priority,
		))
	}

	return ElectionResult{
		Leader:     newLeader,
		Changed:    changed,
		PrevLeader: prevLeader,
	}
}
