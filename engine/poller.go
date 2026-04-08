package engine

import (
	"log/slog"
	"time"

	"github.com/L1ttlebear/ippool/config"
	"github.com/L1ttlebear/ippool/database/models"
	"gorm.io/gorm"
)

const (
	defaultPollInterval = 60 * time.Second
	minPollInterval     = 10 * time.Second
	maxElectRetries     = 3
)

// Notifier sends event notifications.
type Notifier interface {
	Send(eventType string, payload map[string]any)
}

// WSHub broadcasts messages to all WebSocket clients.
type WSHub interface {
	Broadcast(msg any)
}

// Poller orchestrates the full polling cycle.
type Poller struct {
	sm       *StateMachine
	hc       *HealthChecker
	exec     *CommandExecutor
	ddns     *DDNSUpdater
	cb       *CircuitBreaker
	notifier Notifier
	hub      WSHub
	stopCh   chan struct{}
}

// NewPoller creates a new Poller.
func NewPoller(sm *StateMachine, hc *HealthChecker, exec *CommandExecutor, ddns *DDNSUpdater, cb *CircuitBreaker, notifier Notifier, hub WSHub) *Poller {
	return &Poller{
		sm:       sm,
		hc:       hc,
		exec:     exec,
		ddns:     ddns,
		cb:       cb,
		notifier: notifier,
		hub:      hub,
		stopCh:   make(chan struct{}),
	}
}

// Start begins the polling loop in a goroutine.
func (p *Poller) Start(db *gorm.DB) {
	go func() {
		for {
			interval := p.pollInterval()
			select {
			case <-p.stopCh:
				return
			case <-time.After(interval):
				p.RunOnce(db)
			}
		}
	}()
}

// Stop signals the polling loop to exit.
func (p *Poller) Stop() {
	close(p.stopCh)
}

func (p *Poller) pollInterval() time.Duration {
	secs, err := config.GetAs[int](config.PollIntervalKey, 60)
	if err != nil || secs <= 0 {
		return defaultPollInterval
	}
	d := time.Duration(secs) * time.Second
	if d < minPollInterval {
		return minPollInterval
	}
	return d
}

// RunOnce executes a complete polling cycle.
func (p *Poller) RunOnce(db *gorm.DB) {
	var hosts []models.Host
	if err := db.Find(&hosts).Error; err != nil {
		slog.Error("poller: failed to load hosts", "error", err)
		return
	}

	results := p.hc.CheckAll(hosts)

	hostMap := make(map[uint]models.Host, len(hosts))
	for _, h := range hosts {
		hostMap[h.ID] = h
	}
	for _, r := range results {
		h, ok := hostMap[r.HostID]
		if !ok {
			continue
		}
		if err := p.sm.ApplyCheckResult(db, h, r); err != nil {
			slog.Warn("poller: apply check result failed", "host_id", r.HostID, "error", err)
		}
	}

	if err := db.Find(&hosts).Error; err != nil {
		slog.Error("poller: failed to reload hosts", "error", err)
		return
	}

	open, changed := p.cb.Check(hosts)
	if changed {
		eventType := "circuit_close"
		if open {
			eventType = "circuit_open"
		}
		if p.notifier != nil {
			p.notifier.Send(eventType, map[string]any{"circuit_open": open})
		}
	}
	if open {
		p.broadcastSummary(hosts, results)
		return
	}

	currentLeaderID, _ := config.GetAs[uint](config.CurrentLeaderIDKey, uint(0))
	retryHosts := hosts

	for attempt := 0; attempt < maxElectRetries; attempt++ {
		electionResult := Elect(retryHosts, currentLeaderID)

		if electionResult.Leader == nil {
			p.cb.Check(retryHosts)
			if p.notifier != nil {
				p.notifier.Send("circuit_open", map[string]any{"circuit_open": true})
			}
			p.broadcastSummary(retryHosts, results)
			return
		}

		if !electionResult.Changed {
			break
		}

		execResult := p.exec.Execute(*electionResult.Leader)
		if execResult.ExitCode != 0 {
			slog.Warn("poller: pre-command failed, marking dead and re-electing",
				"host_id", electionResult.Leader.ID,
				"exit_code", execResult.ExitCode,
				"attempt", attempt+1,
			)
			_ = p.sm.Transition(db, electionResult.Leader.ID, models.StateDead, "pre-command failed")
			if err := db.Find(&retryHosts).Error; err != nil {
				break
			}
			currentLeaderID = 0
			continue
		}

		cfToken, _ := config.GetAs[string](config.CFApiTokenKey, "")
		cfZone, _ := config.GetAs[string](config.CFZoneIDKey, "")
		cfRecord, _ := config.GetAs[string](config.CFRecordNameKey, "")

		domainMatched := false
		var resolvedIPs []string
		ddnsEnabled := cfToken != "" && cfZone != "" && cfRecord != ""
		if ddnsEnabled {
			if err := p.ddns.Update(cfToken, cfZone, cfRecord, electionResult.Leader.IP); err != nil {
				slog.Error("poller: DDNS update failed", "error", err)
			}

			ok, ips, err := p.ddns.VerifyResolvedIP(cfRecord, electionResult.Leader.IP)
			resolvedIPs = ips
			domainMatched = ok
			if err != nil {
				slog.Warn("poller: DDNS verify failed", "domain", cfRecord, "expected_ip", electionResult.Leader.IP, "error", err)
			}
		}

		db.Model(&models.Host{}).Where("id != ?", electionResult.Leader.ID).Update("is_leader", false)
		db.Model(&models.Host{}).Where("id = ?", electionResult.Leader.ID).Update("is_leader", true)
		_ = config.Set(config.CurrentLeaderIDKey, electionResult.Leader.ID)

		if p.notifier != nil {
			payload := map[string]any{
				"new_leader_id": electionResult.Leader.ID,
				"new_leader_ip": electionResult.Leader.IP,
			}
			if electionResult.PrevLeader != nil {
				payload["prev_leader_id"] = electionResult.PrevLeader.ID
			}
			if ddnsEnabled {
				payload["ddns_domain"] = cfRecord
				payload["ddns_expected_ip"] = electionResult.Leader.IP
				payload["ddns_resolved_ips"] = resolvedIPs
				payload["ddns_match"] = domainMatched
			}
			p.notifier.Send("leader_changed", payload)

			if ddnsEnabled {
				eventType := "ddns_match"
				if !domainMatched {
					eventType = "ddns_mismatch"
				}
				p.notifier.Send(eventType, map[string]any{
					"domain":      cfRecord,
					"expected_ip": electionResult.Leader.IP,
					"resolved_ips": resolvedIPs,
					"ddns_match":  domainMatched,
				})
			}
		}
		break
	}

	p.broadcastSummary(hosts, results)
}

func (p *Poller) broadcastSummary(hosts []models.Host, results []CheckResult) {
	if p.hub == nil {
		return
	}

	leaderID, _ := config.GetAs[uint](config.CurrentLeaderIDKey, uint(0))

	trafficMap := make(map[uint]map[string]int64, len(results))
	for _, r := range results {
		trafficMap[r.HostID] = map[string]int64{
			"in":  r.TrafficIn,
			"out": r.TrafficOut,
		}
	}

	hostSummary := make([]map[string]any, len(hosts))
	for i, h := range hosts {
		hostSummary[i] = map[string]any{
			"id":                h.ID,
			"state":             h.State,
			"traffic_threshold": h.TrafficThreshold,
		}
	}

	p.hub.Broadcast(map[string]any{
		"type": "poll_summary",
		"data": map[string]any{
			"leader_id":    leaderID,
			"circuit_open": p.cb.IsOpen(),
			"hosts":        hostSummary,
			"traffic":      trafficMap,
		},
	})
}
