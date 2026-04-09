package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/L1ttlebear/ippool/database/models"
)

// AgentInstaller installs and starts heartbeat agent on target host via SSH.
type AgentInstaller struct{}

type AgentInstallResult struct {
	Success bool
	Output  string
	Error   string
}

func (ai *AgentInstaller) Install(host models.Host, serverURL, token string, intervalSeconds int) AgentInstallResult {
	if strings.TrimSpace(serverURL) == "" {
		return AgentInstallResult{Success: false, Error: "server URL is empty"}
	}
	if strings.TrimSpace(token) == "" {
		return AgentInstallResult{Success: false, Error: "agent token is empty"}
	}
	if intervalSeconds <= 0 {
		intervalSeconds = 30
	}

	client, err := dialSSH(host, 30*time.Second)
	if err != nil {
		return AgentInstallResult{Success: false, Error: fmt.Sprintf("ssh dial failed: %v", err)}
	}
	defer client.Close()

	script := fmt.Sprintf(`set -e
if ! command -v curl >/dev/null 2>&1; then
  (apt install -y curl >/dev/null 2>&1 || true)
  if ! command -v curl >/dev/null 2>&1; then
    if command -v apt-get >/dev/null 2>&1; then
      DEBIAN_FRONTEND=noninteractive apt-get update -y >/dev/null 2>&1 || true
      DEBIAN_FRONTEND=noninteractive apt-get install -y curl >/dev/null 2>&1 || true
    elif command -v yum >/dev/null 2>&1; then
      yum install -y curl >/dev/null 2>&1 || true
    elif command -v dnf >/dev/null 2>&1; then
      dnf install -y curl >/dev/null 2>&1 || true
    elif command -v apk >/dev/null 2>&1; then
      apk add --no-cache curl >/dev/null 2>&1 || true
    fi
  fi
fi
mkdir -p /opt/ippool-agent
cat >/opt/ippool-agent/agent.sh <<'EOF'
#!/usr/bin/env sh
set -e
HOST_ID=%d
HOST_NAME=%s
SERVER_URL=%s
TOKEN=%s
INTERVAL=%d
while true; do
  IFACE=$(awk -F: 'NR>2 {gsub(/ /, "", $1); if ($1 != "lo") {print $1; exit}}' /proc/net/dev)
  RX=0
  TX=0
  if [ -n "$IFACE" ]; then
    LINE=$(grep "$IFACE:" /proc/net/dev || true)
    if [ -n "$LINE" ]; then
      RX=$(echo "$LINE" | awk -F: '{print $2}' | awk '{print $1}')
      TX=$(echo "$LINE" | awk -F: '{print $2}' | awk '{print $9}')
    fi
  fi
  NET_OK=false
  if command -v curl >/dev/null 2>&1; then
    if curl -sS -L --max-time 8 https://www.hkt.com/ >/dev/null 2>&1; then NET_OK=true; fi
  elif command -v wget >/dev/null 2>&1; then
    if wget -q -T 8 -O /dev/null https://www.hkt.com/ >/dev/null 2>&1; then NET_OK=true; fi
  fi
  SSH_OK=true
  PAYLOAD=$(cat <<JSON
{"host_id":$HOST_ID,"host_name":"$HOST_NAME","network_ok":$NET_OK,"ssh_ok":$SSH_OK,"net_iface":"$IFACE","traffic_in":$RX,"traffic_out":$TX,"probe_target":"https://www.hkt.com/","error":""}
JSON
)
  if command -v curl >/dev/null 2>&1; then
    curl -sS -X POST "$SERVER_URL/api/agent/heartbeat" -H "Content-Type: application/json" -H "X-Agent-Token: $TOKEN" -d "$PAYLOAD" >/dev/null 2>&1 || true
  elif command -v wget >/dev/null 2>&1; then
    wget -q -O /dev/null --header="Content-Type: application/json" --header="X-Agent-Token: $TOKEN" --post-data="$PAYLOAD" "$SERVER_URL/api/agent/heartbeat" >/dev/null 2>&1 || true
  fi
  sleep "$INTERVAL"
done
EOF
chmod +x /opt/ippool-agent/agent.sh
if command -v systemctl >/dev/null 2>&1; then
  cat >/etc/systemd/system/ippool-agent.service <<'EOF'
[Unit]
Description=IPPool Agent
After=network.target

[Service]
Type=simple
ExecStart=/opt/ippool-agent/agent.sh
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable --now ippool-agent.service
else
  nohup /opt/ippool-agent/agent.sh >/opt/ippool-agent/agent.log 2>&1 &
fi
`, host.ID, shellQuote(host.Name), shellQuote(serverURL), shellQuote(token), intervalSeconds)

	session, err := client.NewSession()
	if err != nil {
		return AgentInstallResult{Success: false, Error: fmt.Sprintf("ssh new session failed: %v", err)}
	}
	defer session.Close()

	out, runErr := session.CombinedOutput(script)
	if runErr != nil {
		return AgentInstallResult{Success: false, Error: fmt.Sprintf("install script failed: %v", runErr), Output: string(out)}
	}

	return AgentInstallResult{Success: true, Output: string(out)}
}

func shellQuote(s string) string {
	s = strings.ReplaceAll(s, "'", "'\\''")
	return "'" + s + "'"
}
