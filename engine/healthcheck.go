package engine

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/L1ttlebear/ippool/database/models"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

const (
	defaultHealthConcurrency = 10
	tcpDialTimeout           = 10 * time.Second
	healthRetryDelay         = 5 * time.Second
)

// CheckResult holds the result of a single host health check.
type CheckResult struct {
	HostID       uint
	Reachable    bool
	LatencyMs    int64
	SSHReachable bool
	SSHError     string
	NetIface     string
	TrafficIn    int64
	TrafficOut   int64
	Error        string
}

// HealthChecker performs concurrent health checks on hosts.
type HealthChecker struct {
	sem chan struct{}
	db  *gorm.DB
}

// NewHealthChecker creates a HealthChecker with the given max concurrency.
func NewHealthChecker(maxConcurrency int, db *gorm.DB) *HealthChecker {
	if maxConcurrency <= 0 {
		maxConcurrency = defaultHealthConcurrency
	}
	return &HealthChecker{
		sem: make(chan struct{}, maxConcurrency),
		db:  db,
	}
}

// CheckAll concurrently checks all hosts and returns results.
func (hc *HealthChecker) CheckAll(hosts []models.Host) []CheckResult {
	results := make([]CheckResult, len(hosts))
	var wg sync.WaitGroup
	for i, host := range hosts {
		wg.Add(1)
		hc.sem <- struct{}{}
		go func(idx int, h models.Host) {
			defer wg.Done()
			defer func() { <-hc.sem }()
			results[idx] = hc.checkOne(h)
		}(i, host)
	}
	wg.Wait()

	// Persist check records
	if hc.db != nil {
		records := make([]models.CheckRecord, len(results))
		now := time.Now()
		for i, r := range results {
			records[i] = models.CheckRecord{
				HostID:       r.HostID,
				Time:         now,
				Reachable:    r.Reachable,
				LatencyMs:    r.LatencyMs,
				SSHReachable: r.SSHReachable,
				SSHError:     r.SSHError,
				NetIface:     r.NetIface,
				TrafficIn:    r.TrafficIn,
				TrafficOut:   r.TrafficOut,
				Error:        r.Error,
			}
		}
		hc.db.Create(&records)
	}

	return results
}

// checkOne performs a single host health check with one retry.
func (hc *HealthChecker) checkOne(host models.Host) CheckResult {
	result := hc.tcpCheck(host)
	if !result.Reachable {
		time.Sleep(healthRetryDelay)
		result = hc.tcpCheck(host)
	}

	if result.Reachable {
		sshRes := hc.CheckHostSSH(host)
		result.SSHReachable = sshRes.SSHReachable
		result.SSHError = sshRes.SSHError
		result.LatencyMs = sshRes.LatencyMs

		if sshRes.SSHReachable {
			iface, in, out, err := hc.checkTraffic(host)
			if err == nil {
				result.NetIface = iface
				result.TrafficIn = in
				result.TrafficOut = out
			} else {
				result.SSHReachable = false
				result.SSHError = err.Error()
			}
		}
	}

	return result
}

// tcpCheck dials the SSH port and measures latency.
func (hc *HealthChecker) tcpCheck(host models.Host) CheckResult {
	addr := fmt.Sprintf("%s:%d", host.IP, host.SSHPort)
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, tcpDialTimeout)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return CheckResult{
			HostID:    host.ID,
			Reachable: false,
			LatencyMs: latency,
			Error:     err.Error(),
		}
	}
	conn.Close()
	return CheckResult{
		HostID:    host.ID,
		Reachable: true,
		LatencyMs: latency,
	}
}

// checkTraffic reads /proc/net/dev via SSH and returns cumulative bytes for the default non-lo interface.
func (hc *HealthChecker) checkTraffic(host models.Host) (iface string, in, out int64, err error) {
	client, err := dialSSH(host, 30*time.Second)
	if err != nil {
		return "", 0, 0, fmt.Errorf("ssh dial failed: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", 0, 0, fmt.Errorf("ssh new session failed: %w", err)
	}
	defer session.Close()

	out2, err := session.Output("cat /proc/net/dev")
	if err != nil {
		return "", 0, 0, fmt.Errorf("cat /proc/net/dev failed: %w", err)
	}

	return parseNetDev(string(out2))
}

// parseNetDev parses /proc/net/dev output and returns bytes for the first non-lo interface.
func parseNetDev(content string) (iface string, in, out int64, err error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	// Skip header lines (first 2)
	lineNum := 0
	for scanner.Scan() {
		line := scanner.Text()
		lineNum++
		if lineNum <= 2 {
			continue
		}
		line = strings.TrimSpace(line)
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		iface := strings.TrimSpace(line[:colonIdx])
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(line[colonIdx+1:])
		if len(fields) < 9 {
			continue
		}
		rxBytes, e1 := strconv.ParseInt(fields[0], 10, 64)
		txBytes, e2 := strconv.ParseInt(fields[8], 10, 64)
		if e1 != nil || e2 != nil {
			continue
		}
		return iface, rxBytes, txBytes, nil
	}
	return "", 0, 0, fmt.Errorf("no suitable network interface found in /proc/net/dev")
}

// CheckHostSSH performs a standalone SSH connectivity check for a single host.
func (hc *HealthChecker) CheckHostSSH(host models.Host) CheckResult {
	start := time.Now()
	client, err := dialSSH(host, 10*time.Second)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return CheckResult{
			HostID:       host.ID,
			Reachable:    true,
			LatencyMs:    latency,
			SSHReachable: false,
			SSHError:     err.Error(),
		}
	}
	_ = client.Close()
	return CheckResult{
		HostID:       host.ID,
		Reachable:    true,
		LatencyMs:    latency,
		SSHReachable: true,
	}
}

// dialSSH establishes an SSH connection to the host.
func dialSSH(host models.Host, timeout time.Duration) (*ssh.Client, error) {
	var authMethods []ssh.AuthMethod

	if host.SSHPrivateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(host.SSHPrivateKey))
		if err == nil {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}
	if host.SSHPassword != "" {
		authMethods = append(authMethods, ssh.Password(host.SSHPassword))
	}

	config := &ssh.ClientConfig{
		User:            host.SSHUser,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
		Timeout:         timeout,
	}

	addr := fmt.Sprintf("%s:%d", host.IP, host.SSHPort)
	return ssh.Dial("tcp", addr, config)
}
