package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/L1ttlebear/ippool/database/models"
)

// SyncRemoteScript uploads and runs a DDNS script on target host via SSH once.
// This follows cf-v4-ddns.sh style params: email + global api key + zone name + record name.
func (d *DDNSUpdater) SyncRemoteScript(host models.Host, cfEmail, cfAPIKey, zoneName, recordName string) error {
	cfEmail = strings.TrimSpace(cfEmail)
	cfAPIKey = strings.TrimSpace(cfAPIKey)
	zoneName = strings.TrimSpace(zoneName)
	recordName = strings.TrimSpace(recordName)
	if cfEmail == "" || cfAPIKey == "" || zoneName == "" || recordName == "" {
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

	CFUSER=%s
	CFKEY=%s
	CFZONE_NAME=%s
	CFRECORD_NAME=%s
	CFRECORD_TYPE=A
	CFTTL=1
	WANIPSITE="https://api.ipify.org"

	WAN_IP=$(curl -fsSL "$WANIPSITE")
	ZONE_JSON=$(curl -fsSL -X GET "https://api.cloudflare.com/client/v4/zones?name=${CFZONE_NAME}" \
	  -H "X-Auth-Email: ${CFUSER}" \
	  -H "X-Auth-Key: ${CFKEY}" \
	  -H "Content-Type: application/json")
	CFZONE_ID=$(echo "$ZONE_JSON" | tr -d '\n' | sed -n 's/.*"result":\[{"id":"\([^"]*\)".*/\1/p')
	if [ -z "${CFZONE_ID}" ]; then
	  echo "[ddns-remote] zone not found: ${CFZONE_NAME}"
	  echo "$ZONE_JSON"
	  exit 1
	fi

	RECORD_JSON=$(curl -fsSL -X GET "https://api.cloudflare.com/client/v4/zones/${CFZONE_ID}/dns_records?name=${CFRECORD_NAME}" \
	  -H "X-Auth-Email: ${CFUSER}" \
	  -H "X-Auth-Key: ${CFKEY}" \
	  -H "Content-Type: application/json")
	CFRECORD_ID=$(echo "$RECORD_JSON" | tr -d '\n' | sed -n 's/.*"result":\[{"id":"\([^"]*\)".*/\1/p')
	if [ -z "${CFRECORD_ID}" ]; then
	  echo "[ddns-remote] DNS record not found: ${CFRECORD_NAME}"
	  echo "$RECORD_JSON"
	  exit 1
	fi

	UPDATE_JSON=$(curl -fsSL -X PUT "https://api.cloudflare.com/client/v4/zones/${CFZONE_ID}/dns_records/${CFRECORD_ID}" \
	  -H "X-Auth-Email: ${CFUSER}" \
	  -H "X-Auth-Key: ${CFKEY}" \
	  -H "Content-Type: application/json" \
	  --data "{\"type\":\"${CFRECORD_TYPE}\",\"name\":\"${CFRECORD_NAME}\",\"content\":\"${WAN_IP}\",\"ttl\":${CFTTL}}")

	echo "$UPDATE_JSON" | grep -q '"success":true' || { echo "[ddns-remote] update failed"; echo "$UPDATE_JSON"; exit 1; }
	echo "[ddns-remote] updated ${CFRECORD_NAME} => ${WAN_IP}"
	EOF

	chmod +x /opt/ippool-ddns/cf-v4-ddns.sh
	echo "[ddns-remote] running script once"
	/opt/ippool-ddns/cf-v4-ddns.sh

	echo "[ddns-remote] done"
	`, shellQuote(cfEmail), shellQuote(cfAPIKey), shellQuote(zoneName), shellQuote(recordName))

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
