package engine

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/L1ttlebear/ippool/config"
	"github.com/L1ttlebear/ippool/database/models"
	"gorm.io/gorm"
)

const (
	defaultPollInterval = 60 * time.Second
	minPollInterval     = 10 * time.Second
	maxElectRetries     = 3

	// hostFailThreshold controls how many consecutive failed heartbeat snapshots
	// are required before we treat a host as offline/dead.
	hostFailThreshold = 3
	// hostRecoverThreshold controls how many consecutive successful heartbeat snapshots
	// are required before we treat a host as recovered/online.
	hostRecoverThreshold = 2
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

	// flapping guards for heartbeat-only health input
	hostFailStreak    map[uint]int
	hostRecoverStreak map[uint]int
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

		hostFailStreak:    make(map[uint]int),
		hostRecoverStreak: make(map[uint]int),
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

	timeoutSecs, _ := config.GetAs[int](config.HeartbeatTimeoutSecondsKey, 90)
	if timeoutSecs <= 0 {
		timeoutSecs = 90
	}
	timeout := time.Duration(timeoutSecs) * time.Second

	hostIDs := make([]uint, 0, len(hosts))
	for _, h := range hosts {
		hostIDs = append(hostIDs, h.ID)
	}

	hbMap := map[uint]models.HostHeartbeat{}
	if len(hostIDs) > 0 {
		var heartbeats []models.HostHeartbeat
		if err := db.Where("host_id IN ?", hostIDs).Find(&heartbeats).Error; err != nil {
			slog.Error("poller: failed to load heartbeats", "error", err)
			return
		}
		for _, hb := range heartbeats {
			hbMap[hb.HostID] = hb
		}
	}

	results := make([]CheckResult, 0, len(hosts))
	for _, h := range hosts {
		hb, ok := hbMap[h.ID]
		r := CheckResult{HostID: h.ID}

		// base snapshot from current heartbeat
		baseReachable := false
		if !ok {
			r.Reachable = false
			r.SSHReachable = false
			r.Error = "heartbeat missing"
			r.SSHError = "heartbeat missing"
		} else if time.Since(hb.UpdatedAt) > timeout {
			r.Reachable = false
			r.SSHReachable = false
			r.Error = fmt.Sprintf("heartbeat timeout: last update %s", hb.UpdatedAt.Format(time.RFC3339))
			r.SSHError = r.Error
			r.NetIface = hb.NetIface
			r.TrafficIn = hb.TrafficIn
			r.TrafficOut = hb.TrafficOut
		} else {
			baseReachable = hb.NetworkOK && hb.SSHOK
			r.Reachable = baseReachable
			r.SSHReachable = hb.SSHOK
			r.SSHError = hb.Error
			r.NetIface = hb.NetIface
			r.TrafficIn = hb.TrafficIn
			r.TrafficOut = hb.TrafficOut
			if !r.Reachable {
				r.Error = hb.Error
				if r.Error == "" {
					r.Error = "heartbeat reported network/ssh failure"
				}
			}
		}

		// anti-flapping hysteresis: require consecutive failures/recoveries
		if baseReachable {
			p.hostRecoverStreak[h.ID]++
			p.hostFailStreak[h.ID] = 0
		} else {
			p.hostFailStreak[h.ID]++
			p.hostRecoverStreak[h.ID] = 0
		}

		effectiveReachable := baseReachable
		switch h.State {
		case models.StateDead:
			if baseReachable && p.hostRecoverStreak[h.ID] < hostRecoverThreshold {
				effectiveReachable = false
				if r.Error == "" {
					r.Error = fmt.Sprintf("waiting recovery confirmation (%d/%d)", p.hostRecoverStreak[h.ID], hostRecoverThreshold)
				}
			}
		default:
			if !baseReachable && p.hostFailStreak[h.ID] < hostFailThreshold {
				effectiveReachable = true
				r.Error = ""
				r.SSHError = ""
			}
		}

		r.Reachable = effectiveReachable
		r.SSHReachable = effectiveReachable

		results = append(results, r)
		if err := p.sm.ApplyCheckResult(db, h, r); err != nil {
			slog.Warn("poller: apply check result failed", "host_id", h.ID, "error", err)
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

	reachability := make(map[uint]bool, len(results))
	for _, r := range results {
		reachability[r.HostID] = r.Reachable && r.SSHReachable
	}

	for attempt := 0; attempt < maxElectRetries; attempt++ {
		electionResult := Elect(retryHosts, currentLeaderID)
		if electionResult.PrevLeader != nil {
			electionResult.PrevReachable = reachability[electionResult.PrevLeader.ID]
		}

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

		if electionResult.PrevLeader != nil && electionResult.PrevReachable {
			disconnectResult := p.exec.ExecuteDisconnect(*electionResult.PrevLeader)
			if disconnectResult.ExitCode != 0 {
				slog.Warn("poller: disconnect command failed on previous leader",
					"host_id", electionResult.PrevLeader.ID,
					"exit_code", disconnectResult.ExitCode,
				)
			}

			if electionResult.PrevLeader.State == models.StateFull || electionResult.PrevLeader.State == models.StateDead {
				if err := db.Model(&models.Host{}).Where("id = ?", electionResult.PrevLeader.ID).Updates(map[string]any{
					"pre_command":        "",
					"disconnect_command": "",
				}).Error; err != nil {
					slog.Warn("poller: clear commands failed for previous exhausted host", "host_id", electionResult.PrevLeader.ID, "error", err)
				} else {
					slog.Info("poller: cleared commands for previous exhausted host", "host_id", electionResult.PrevLeader.ID)
				}
			}
		}

		if !reachability[electionResult.Leader.ID] {
			slog.Warn("poller: elected leader is not reachable/ssh-available, marking dead and re-electing",
				"host_id", electionResult.Leader.ID,
				"attempt", attempt+1,
			)
			_ = p.sm.Transition(db, electionResult.Leader.ID, models.StateDead, "leader health/ssh check failed before connect command")
			if err := db.Find(&retryHosts).Error; err != nil {
				break
			}
			currentLeaderID = 0
			continue
		}

		execResult := p.exec.Execute(*electionResult.Leader)
		if execResult.ExitCode == 0 && (electionResult.Leader.State == models.StateFull || electionResult.Leader.State == models.StateDead) {
			if err := db.Model(&models.Host{}).Where("id = ?", electionResult.Leader.ID).Updates(map[string]any{
				"pre_command":        "",
				"disconnect_command": "",
			}).Error; err != nil {
				slog.Warn("poller: clear commands failed for exhausted leader", "host_id", electionResult.Leader.ID, "error", err)
			} else {
				slog.Info("poller: cleared commands for exhausted leader", "host_id", electionResult.Leader.ID)
			}
		}

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

		domainMatched := false
		var resolvedIPs []string
		ddnsEnabled := false
		ddnsDomain := ""
		ddnsRemoteSynced := false
		ddnsRemoteError := ""

		rules, _ := config.GetAs[[]config.DdnsPoolRule](config.DDNSPoolRulesKey, []config.DdnsPoolRule{})
		for _, rule := range rules {
			if !rule.Enabled {
				continue
			}
			if strings.TrimSpace(rule.Pool) != strings.TrimSpace(electionResult.Leader.Pool) {
				continue
			}
			cfRecord := strings.TrimSpace(rule.RecordName)
			if cfRecord == "" {
				continue
			}

			ddnsEnabled = true
			ddnsDomain = cfRecord

			cfEmail := strings.TrimSpace(rule.CFEmail)
			cfKey := strings.TrimSpace(rule.CFApiKey)
			cfZoneName := strings.TrimSpace(rule.CFZoneName)
			cfToken := strings.TrimSpace(rule.CFApiToken)
			cfZoneID := strings.TrimSpace(rule.CFZoneID)

			var ddnsErr error
			if cfEmail != "" && cfKey != "" && cfZoneName != "" {
				ddnsErr = p.ddns.UpdateWithGlobalKey(cfEmail, cfKey, cfZoneName, cfRecord, electionResult.Leader.IP)
			} else if cfToken != "" && cfZoneID != "" {
				ddnsErr = p.ddns.Update(cfToken, cfZoneID, cfRecord, electionResult.Leader.IP)
			} else {
				ddnsErr = fmt.Errorf("missing ddns credentials")
			}
			if ddnsErr != nil {
				slog.Error("poller: DDNS update failed", "pool", electionResult.Leader.Pool, "domain", cfRecord, "error", ddnsErr)
				continue
			}

			if err := p.ddns.SyncRemoteScript(*electionResult.Leader, cfEmail, cfKey, cfZoneName, cfRecord); err != nil {
				ddnsRemoteError = err.Error()
				slog.Warn("poller: DDNS remote script sync failed", "host_id", electionResult.Leader.ID, "error", err)
			} else {
				ddnsRemoteSynced = true
			}

			ok, ips, err := p.ddns.VerifyResolvedIP(cfRecord, electionResult.Leader.IP)
			resolvedIPs = ips
			domainMatched = ok
			if err != nil {
				slog.Warn("poller: DDNS verify failed", "domain", cfRecord, "expected_ip", electionResult.Leader.IP, "error", err)
			}
			break
		}

		if !ddnsEnabled {
			cfRecord, _ := config.GetAs[string](config.CFRecordNameKey, "")
			cfRecord = strings.TrimSpace(cfRecord)
			cfEmail, _ := config.GetAs[string](config.CFEmailKey, "")
			cfKey, _ := config.GetAs[string](config.CFApiKeyKey, "")
			cfZoneName, _ := config.GetAs[string](config.CFZoneNameKey, "")
			cfEmail = strings.TrimSpace(cfEmail)
			cfKey = strings.TrimSpace(cfKey)
			cfZoneName = strings.TrimSpace(cfZoneName)
			cfToken, _ := config.GetAs[string](config.CFApiTokenKey, "")
			cfZoneID, _ := config.GetAs[string](config.CFZoneIDKey, "")
			cfToken = strings.TrimSpace(cfToken)
			cfZoneID = strings.TrimSpace(cfZoneID)

			if cfRecord != "" {
				ddnsEnabled = true
				ddnsDomain = cfRecord
				var err error
				if cfEmail != "" && cfKey != "" && cfZoneName != "" {
					err = p.ddns.UpdateWithGlobalKey(cfEmail, cfKey, cfZoneName, cfRecord, electionResult.Leader.IP)
				} else if cfToken != "" && cfZoneID != "" {
					err = p.ddns.Update(cfToken, cfZoneID, cfRecord, electionResult.Leader.IP)
				} else {
					err = fmt.Errorf("missing ddns credentials")
				}
				if err != nil {
					slog.Error("poller: DDNS update failed", "error", err)
				}
				if cfEmail != "" && cfKey != "" && cfZoneName != "" {
					if err := p.ddns.SyncRemoteScript(*electionResult.Leader, cfEmail, cfKey, cfZoneName, cfRecord); err != nil {
						ddnsRemoteError = err.Error()
						slog.Warn("poller: DDNS remote script sync failed", "host_id", electionResult.Leader.ID, "error", err)
					} else {
						ddnsRemoteSynced = true
					}
				}
				ok, ips, err := p.ddns.VerifyResolvedIP(cfRecord, electionResult.Leader.IP)
				resolvedIPs = ips
				domainMatched = ok
				if err != nil {
					slog.Warn("poller: DDNS verify failed", "domain", cfRecord, "expected_ip", electionResult.Leader.IP, "error", err)
				}
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
				payload["ddns_domain"] = ddnsDomain
				payload["ddns_expected_ip"] = electionResult.Leader.IP
				payload["ddns_resolved_ips"] = resolvedIPs
				payload["ddns_match"] = domainMatched
				payload["ddns_remote_synced"] = ddnsRemoteSynced
				if ddnsRemoteError != "" {
					payload["ddns_remote_error"] = ddnsRemoteError
				}
			}
			p.notifier.Send("leader_changed", payload)

			if ddnsEnabled {
				eventType := "ddns_match"
				if !domainMatched {
					eventType = "ddns_mismatch"
				}
				p.notifier.Send(eventType, map[string]any{
					"domain":       ddnsDomain,
					"expected_ip":  electionResult.Leader.IP,
					"resolved_ips": resolvedIPs,
					"ddns_match":   domainMatched,
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

	trafficMap := make(map[uint]map[string]any, len(results))
	for _, r := range results {
		trafficMap[r.HostID] = map[string]any{
			"reachable":     r.Reachable,
			"in":            r.TrafficIn,
			"out":           r.TrafficOut,
			"ssh_reachable": r.SSHReachable,
			"ssh_error":     r.SSHError,
			"net_iface":     r.NetIface,
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
