package engine

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"sync"
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
	var output strings.Builder
	res := ai.InstallWithProgress(host, serverURL, token, intervalSeconds, func(line string) {
		if line == "" {
			return
		}
		output.WriteString(line)
		output.WriteString("\n")
	})
	if strings.TrimSpace(res.Output) == "" {
		res.Output = output.String()
	}
	return res
}

func (ai *AgentInstaller) InstallWithProgress(host models.Host, serverURL, token string, intervalSeconds int, onProgress func(string)) AgentInstallResult {
	if strings.TrimSpace(serverURL) == "" {
		return AgentInstallResult{Success: false, Error: "server URL is empty"}
	}
	if strings.TrimSpace(token) == "" {
		return AgentInstallResult{Success: false, Error: "agent token is empty"}
	}
	if intervalSeconds <= 0 {
		intervalSeconds = 30
	}

	emit := func(line string) {
		if onProgress != nil {
			onProgress(strings.TrimSpace(line))
		}
	}

	client, err := dialSSH(host, 30*time.Second)
	if err != nil {
		return AgentInstallResult{Success: false, Error: fmt.Sprintf("ssh dial failed: %v", err)}
	}
	defer client.Close()

	script := fmt.Sprintf(`set -e
	echo "[1/8] 开始安装 Agent"
	echo "[2/8] 检查 curl 环境"
	if ! command -v curl >/dev/null 2>&1; then
	  echo "curl 未安装，尝试自动安装..."
	  if command -v apt-get >/dev/null 2>&1; then
	    DEBIAN_FRONTEND=noninteractive apt-get update -y
	    DEBIAN_FRONTEND=noninteractive apt-get install -y curl
	  elif command -v apt >/dev/null 2>&1; then
	    DEBIAN_FRONTEND=noninteractive apt update -y
	    DEBIAN_FRONTEND=noninteractive apt install -y curl
	  elif command -v yum >/dev/null 2>&1; then
	    yum install -y curl
	  elif command -v dnf >/dev/null 2>&1; then
	    dnf install -y curl
	  elif command -v apk >/dev/null 2>&1; then
	    apk add --no-cache curl
	  else
	    echo "未找到支持的包管理器，无法自动安装 curl"
	    exit 1
	  fi
	else
	  echo "curl 已安装"
	fi
	echo "[3/8] 写入 agent 脚本"
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
	echo "[4/8] 配置开机自启服务"
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
	  echo "[5/8] 重新加载 systemd"
	  systemctl daemon-reload
	  echo "[6/8] 启用并启动 ippool-agent.service"
	  systemctl enable --now ippool-agent.service
	  echo "[7/8] 检查服务状态"
	  systemctl --no-pager --full status ippool-agent.service || true
	else
	  echo "[5/8] 当前系统无 systemctl，使用 nohup 后台运行"
	  nohup /opt/ippool-agent/agent.sh >/opt/ippool-agent/agent.log 2>&1 &
	fi
	echo "[8/8] Agent 安装完成"
	`, host.ID, shellQuote(host.Name), shellQuote(serverURL), shellQuote(token), intervalSeconds)

	session, err := client.NewSession()
	if err != nil {
		return AgentInstallResult{Success: false, Error: fmt.Sprintf("ssh new session failed: %v", err)}
	}
	defer session.Close()

	stdout, err := session.StdoutPipe()
	if err != nil {
		return AgentInstallResult{Success: false, Error: fmt.Sprintf("stdout pipe failed: %v", err)}
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return AgentInstallResult{Success: false, Error: fmt.Sprintf("stderr pipe failed: %v", err)}
	}

	var output strings.Builder
	appendLine := func(line string) {
		line = strings.TrimSpace(line)
		if line == "" {
			return
		}
		output.WriteString(line)
		output.WriteString("\n")
		emit(line)
	}

	readStream := func(r io.Reader, wg *sync.WaitGroup) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)
		for scanner.Scan() {
			appendLine(scanner.Text())
		}
	}

	if err := session.Start(script); err != nil {
		return AgentInstallResult{Success: false, Error: fmt.Sprintf("start install script failed: %v", err)}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go readStream(stdout, &wg)
	go readStream(stderr, &wg)

	runErr := session.Wait()
	wg.Wait()

	if runErr != nil {
		return AgentInstallResult{Success: false, Error: fmt.Sprintf("install script failed: %v", runErr), Output: output.String()}
	}

	return AgentInstallResult{Success: true, Output: output.String()}
}

func shellQuote(s string) string {
	s = strings.ReplaceAll(s, "'", "'\\''")
	return "'" + s + "'"
}
