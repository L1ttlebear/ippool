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

func envOr(k, def string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	return v
}

func probeExternal() (bool, string) {
	cmd := exec.Command("sh", "-lc", "curl -fsS --max-time 8 https://www.hkt.com/ >/dev/null || wget -q --timeout=8 --tries=1 -O - https://www.hkt.com/ >/dev/null")
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

func main() {
	server := envOr("AGENT_SERVER", "http://127.0.0.1:8080")
	token := envOr("AGENT_TOKEN", "")
	hostIDRaw := envOr("AGENT_HOST_ID", "0")
	hostName := envOr("AGENT_HOST_NAME", "")
	intervalRaw := envOr("AGENT_INTERVAL", "15")

	hID64, _ := strconv.ParseUint(hostIDRaw, 10, 64)
	hostID := uint(hID64)
	intervalSec, _ := strconv.Atoi(intervalRaw)
	if intervalSec < 5 {
		intervalSec = 5
	}

	if hostID == 0 || token == "" {
		fmt.Println("AGENT_HOST_ID and AGENT_TOKEN are required")
		os.Exit(1)
	}

	client := &http.Client{Timeout: 12 * time.Second}
	url := strings.TrimRight(server, "/") + "/api/agent/heartbeat"

	for {
		netOK, probeErr := probeExternal()
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
			HostName:    hostName,
			NetworkOK:   netOK,
			SSHOK:       true,
			NetIface:    iface,
			TrafficIn:   rx,
			TrafficOut:  tx,
			ProbeTarget: "https://www.hkt.com/",
			Error:       errMsg,
		}

		b, _ := json.Marshal(pl)
		req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agent-Token", token)
		_, _ = client.Do(req)

		time.Sleep(time.Duration(intervalSec) * time.Second)
	}
}
