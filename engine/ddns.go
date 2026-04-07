package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/L1ttlebear/ippool/database/auditlog"
)

const (
	cfAPIBase    = "https://api.cloudflare.com/client/v4"
	ddnsTimeout  = 15 * time.Second
	ddnsRetries  = 3
	ddnsRetryGap = 5 * time.Second
)

// DDNSUpdater updates Cloudflare DNS A records.
type DDNSUpdater struct{}

type cfDNSRecord struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

type cfListResponse struct {
	Result []cfDNSRecord `json:"result"`
}

// Update sets the A record for recordName in zoneID to ip.
func (d *DDNSUpdater) Update(apiToken, zoneID, recordName, ip string) error {
	var lastErr error
	for attempt := 1; attempt <= ddnsRetries; attempt++ {
		err := d.doUpdate(apiToken, zoneID, recordName, ip)
		if err == nil {
			auditlog.EventLog("ddns_update", fmt.Sprintf("DDNS updated: %s -> %s", recordName, ip))
			return nil
		}
		lastErr = err
		if attempt < ddnsRetries {
			time.Sleep(ddnsRetryGap)
		}
	}
	auditlog.EventLog("ddns_update", fmt.Sprintf("DDNS update failed after %d attempts: %s -> %s: %v", ddnsRetries, recordName, ip, lastErr))
	return fmt.Errorf("DDNS update failed after %d attempts: %w", ddnsRetries, lastErr)
}

func (d *DDNSUpdater) doUpdate(apiToken, zoneID, recordName, ip string) error {
	client := &http.Client{Timeout: ddnsTimeout}

	listURL := fmt.Sprintf("%s/zones/%s/dns_records?name=%s&type=A", cfAPIBase, zoneID, recordName)
	req, err := http.NewRequest(http.MethodGet, listURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("list DNS records: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("list DNS records: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var listResp cfListResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		return fmt.Errorf("parse list response: %w", err)
	}
	if len(listResp.Result) == 0 {
		return fmt.Errorf("DNS record %q not found in zone %s", recordName, zoneID)
	}

	recordID := listResp.Result[0].ID

	putURL := fmt.Sprintf("%s/zones/%s/dns_records/%s", cfAPIBase, zoneID, recordID)
	payload := map[string]any{
		"type":    "A",
		"name":    recordName,
		"content": ip,
		"ttl":     1,
	}
	payloadBytes, _ := json.Marshal(payload)

	putReq, err := http.NewRequest(http.MethodPut, putURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return err
	}
	putReq.Header.Set("Authorization", "Bearer "+apiToken)
	putReq.Header.Set("Content-Type", "application/json")

	putResp, err := client.Do(putReq)
	if err != nil {
		return fmt.Errorf("update DNS record: %w", err)
	}
	defer putResp.Body.Close()

	putBody, _ := io.ReadAll(putResp.Body)
	if putResp.StatusCode != http.StatusOK {
		return fmt.Errorf("update DNS record: HTTP %d: %s", putResp.StatusCode, string(putBody))
	}

	return nil
}
