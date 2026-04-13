package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type payload struct {
	HostID      uint   `json:"host_id"`
	HostName    string `json:"host_name"`
	NetworkOK   bool   `json:"network_ok"`
	SSHOK       bool   `json:"ssh_ok"`
	NetIface    string `json:"net_iface"`
	TrafficIn   int64  `json:"traffic_in"`
	TrafficOut  int64  `json:"traffic_out"`
	ProbeTarget string `json:"probe_target"`
	Error       string `json:"error"`
}

type configResp struct {
	HostID                uint   `json:"host_id"`
	HostName              string `json:"host_name"`
	HeartbeatURL          string `json:"heartbeat_url"`
	HeartbeatIntervalSecs int    `json:"heartbeat_interval_seconds"`
	ProbeTarget           string `json:"probe_target"`
}

func envOr(k, def string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	return v
}

func probeExternal(target string) (bool, string) {
	t := strings.TrimSpace(target)
	if t == "" {
		t = "https://www.hkt.com/"
	}
	cmd := exec.Command("sh", "-lc", "curl -fsS --max-time 8 "+shellQuote(t)+" >/dev/null || wget -q --timeout=8 --tries=1 -O - "+shellQuote(t)+" >/dev/null")
	if err := cmd.Run(); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func netDev() (iface string, in, out int64, err error) {
	b, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return "", 0, 0, err
	}
	lines := strings.Split(string(b), "\n")
	for i := 2; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		iface := strings.TrimSpace(parts[0])
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 9 {
			continue
		}
		rx, e1 := strconv.ParseInt(fields[0], 10, 64)
		tx, e2 := strconv.ParseInt(fields[8], 10, 64)
		if e1 != nil || e2 != nil {
			continue
		}
		return iface, rx, tx, nil
	}
	return "", 0, 0, fmt.Errorf("no suitable iface")
}

func fetchPanelConfig(client *http.Client, server string, token string, hostID uint) (configResp, error) {
	cfgURL := strings.TrimRight(server, "/") + "/api/agent/config?host_id=" + strconv.FormatUint(uint64(hostID), 10)
	req, err := http.NewRequest(http.MethodGet, cfgURL, nil)
	if err != nil {
		return configResp{}, err
	}
	req.Header.Set("X-Agent-Token", token)
	resp, err := client.Do(req)
	if err != nil {
		return configResp{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return configResp{}, fmt.Errorf("config status %d", resp.StatusCode)
	}
	var cfg configResp
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return configResp{}, err
	}
	if strings.TrimSpace(cfg.HeartbeatURL) == "" {
		cfg.HeartbeatURL = strings.TrimRight(server, "/") + "/api/agent/heartbeat"
	}
	if cfg.HeartbeatIntervalSecs < 5 {
		cfg.HeartbeatIntervalSecs = 5
	}
	if strings.TrimSpace(cfg.ProbeTarget) == "" {
		cfg.ProbeTarget = "https://www.hkt.com/"
	}
	return cfg, nil
}

func shellQuote(s string) string {
	s = strings.ReplaceAll(s, "'", "'\\''")
	return "'" + s + "'"
}

func main() {
	server := envOr("AGENT_SERVER", "http://127.0.0.1:8080")
	token := envOr("AGENT_TOKEN", "")
	hostIDRaw := envOr("AGENT_HOST_ID", "0")
	hostNameFallback := envOr("AGENT_HOST_NAME", "")
	intervalRaw := envOr("AGENT_INTERVAL", "30")

	hID64, _ := strconv.ParseUint(hostIDRaw, 10, 64)
	hostID := uint(hID64)
	fallbackInterval, _ := strconv.Atoi(intervalRaw)
	if fallbackInterval < 5 {
		fallbackInterval = 5
	}

	if hostID == 0 || token == "" {
		fmt.Println("AGENT_HOST_ID and AGENT_TOKEN are required")
		os.Exit(1)
	}

	client := &http.Client{Timeout: 12 * time.Second}
	currentHeartbeatURL := strings.TrimRight(server, "/") + "/api/agent/heartbeat"
	currentHostName := hostNameFallback
	currentIntervalSec := fallbackInterval
	currentProbeTarget := "https://www.hkt.com/"

	for {
		if cfg, err := fetchPanelConfig(client, server, token, hostID); err == nil {
			currentHeartbeatURL = cfg.HeartbeatURL
			if strings.TrimSpace(cfg.HostName) != "" {
				currentHostName = cfg.HostName
			}
			currentIntervalSec = cfg.HeartbeatIntervalSecs
			currentProbeTarget = cfg.ProbeTarget
		}

		netOK, probeErr := probeExternal(currentProbeTarget)
		iface, rx, tx, ifaceErr := netDev()
		errMsg := strings.TrimSpace(strings.Join([]string{probeErr, func() string {
			if ifaceErr != nil {
				return ifaceErr.Error()
			}
			return ""
		}()}, "; "))
		errMsg = strings.Trim(errMsg, "; ")

		pl := payload{
			HostID:      hostID,
			HostName:    currentHostName,
			NetworkOK:   netOK,
			SSHOK:       true,
			NetIface:    iface,
			TrafficIn:   rx,
			TrafficOut:  tx,
			ProbeTarget: currentProbeTarget,
			Error:       errMsg,
		}

		if b, err := json.Marshal(pl); err == nil {
			if req, err := http.NewRequest(http.MethodPost, currentHeartbeatURL, bytes.NewReader(b)); err == nil {
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Agent-Token", token)
				_, _ = client.Do(req)
			}
		}

		time.Sleep(time.Duration(currentIntervalSec) * time.Second)
	}
}
