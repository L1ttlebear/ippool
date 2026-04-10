package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/L1ttlebear/ippool/database/models"
)

// SyncRemoteScript uploads and runs a DDNS script on target host via SSH once.
func (d *DDNSUpdater) SyncRemoteScript(host models.Host, apiToken, zoneID, recordName string) error {
	apiToken = strings.TrimSpace(apiToken)
	zoneID = strings.TrimSpace(zoneID)
	recordName = strings.TrimSpace(recordName)
	if apiToken == "" || zoneID == "" || recordName == "" {
		return fmt.Errorf("missing DDNS params for remote sync")
	}

	client, err := dialSSH(host, 30*time.Second)
	if err != nil {
		return fmt.Errorf("ssh dial failed: %w", err)
	}
	defer client.Close()

	script := fmt.Sprintf(`set -e
	echo "[ddns-remote] preparing environment"
	if ! command -v curl >/dev/null 2>&1; then
	  echo "[ddns-remote] curl not found, installing..."
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
	    echo "[ddns-remote] no supported package manager for curl"
	    exit 1
	  fi
	fi

	mkdir -p /opt/ippool-ddns
	cat >/opt/ippool-ddns/cf-v4-ddns.sh <<'EOF'
	#!/usr/bin/env sh
	set -eu

	CF_API_TOKEN=%s
	CF_ZONE_ID=%s
	CF_RECORD_NAME=%s

	WAN_IP=$(curl -fsSL https://api.ipify.org)
	LIST_JSON=$(curl -fsSL -X GET "https://api.cloudflare.com/client/v4/zones/${CF_ZONE_ID}/dns_records?name=${CF_RECORD_NAME}&type=A" \
	  -H "Authorization: Bearer ${CF_API_TOKEN}" \
	  -H "Content-Type: application/json")

	REC_ID=$(echo "$LIST_JSON" | tr -d '\n' | sed -n 's/.*"result":\[{"id":"\([^"]*\)".*/\1/p')
	if [ -z "${REC_ID}" ]; then
	  echo "[ddns-remote] DNS record not found: ${CF_RECORD_NAME}"
	  echo "$LIST_JSON"
	  exit 1
	fi

	UPDATE_JSON=$(curl -fsSL -X PUT "https://api.cloudflare.com/client/v4/zones/${CF_ZONE_ID}/dns_records/${REC_ID}" \
	  -H "Authorization: Bearer ${CF_API_TOKEN}" \
	  -H "Content-Type: application/json" \
	  --data "{\"type\":\"A\",\"name\":\"${CF_RECORD_NAME}\",\"content\":\"${WAN_IP}\",\"ttl\":1}")

	echo "$UPDATE_JSON" | grep -q '"success":true' || { echo "[ddns-remote] update failed"; echo "$UPDATE_JSON"; exit 1; }
	echo "[ddns-remote] updated ${CF_RECORD_NAME} => ${WAN_IP}"
	EOF

	chmod +x /opt/ippool-ddns/cf-v4-ddns.sh
	echo "[ddns-remote] running script once"
	/opt/ippool-ddns/cf-v4-ddns.sh

	echo "[ddns-remote] done"
	`, shellQuote(apiToken), shellQuote(zoneID), shellQuote(recordName))

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh new session failed: %w", err)
	}
	defer session.Close()

	out, runErr := session.CombinedOutput(script)
	if runErr != nil {
		return fmt.Errorf("remote ddns script failed: %v | output: %s", runErr, string(out))
	}
	return nil
}
