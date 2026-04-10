package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
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

// Update (legacy token+zoneID route)
func (d *DDNSUpdater) Update(apiToken, zoneID, recordName, ip string) error {
	var lastErr error
	for attempt := 1; attempt <= ddnsRetries; attempt++ {
		err := d.doUpdateWithToken(apiToken, zoneID, recordName, ip)
		if err == nil {
			auditlog.EventLog("ddns_update", fmt.Sprintf("DDNS updated(token): %s -> %s", recordName, ip))
			return nil
		}
		lastErr = err
		if attempt < ddnsRetries {
			time.Sleep(ddnsRetryGap)
		}
	}
	auditlog.EventLog("ddns_update", fmt.Sprintf("DDNS update failed(token) after %d attempts: %s -> %s: %v", ddnsRetries, recordName, ip, lastErr))
	return fmt.Errorf("DDNS update failed after %d attempts: %w", ddnsRetries, lastErr)
}

// UpdateWithGlobalKey matches cf-v4-ddns.sh model: email + global api key + zone name + record.
func (d *DDNSUpdater) UpdateWithGlobalKey(cfEmail, cfAPIKey, zoneName, recordName, ip string) error {
	var lastErr error
	for attempt := 1; attempt <= ddnsRetries; attempt++ {
		err := d.doUpdateWithGlobalKey(cfEmail, cfAPIKey, zoneName, recordName, ip)
		if err == nil {
			auditlog.EventLog("ddns_update", fmt.Sprintf("DDNS updated(global-key): %s -> %s", recordName, ip))
			return nil
		}
		lastErr = err
		if attempt < ddnsRetries {
			time.Sleep(ddnsRetryGap)
		}
	}
	auditlog.EventLog("ddns_update", fmt.Sprintf("DDNS update failed(global-key) after %d attempts: %s -> %s: %v", ddnsRetries, recordName, ip, lastErr))
	return fmt.Errorf("DDNS update failed after %d attempts: %w", ddnsRetries, lastErr)
}

func (d *DDNSUpdater) doUpdateWithToken(apiToken, zoneID, recordName, ip string) error {
	client := &http.Client{Timeout: ddnsTimeout}

	listURL := fmt.Sprintf("%s/zones/%s/dns_records?name=%s&type=A", cfAPIBase, zoneID, url.QueryEscape(recordName))
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
	return d.putRecordWithToken(client, apiToken, zoneID, recordID, recordName, ip)
}

func (d *DDNSUpdater) doUpdateWithGlobalKey(cfEmail, cfAPIKey, zoneName, recordName, ip string) error {
	client := &http.Client{Timeout: ddnsTimeout}

	zoneID, err := d.getZoneIDByName(client, cfEmail, cfAPIKey, zoneName)
	if err != nil {
		return err
	}
	recordID, err := d.getRecordIDByNameWithGlobalKey(client, cfEmail, cfAPIKey, zoneID, recordName)
	if err != nil {
		return err
	}
	return d.putRecordWithGlobalKey(client, cfEmail, cfAPIKey, zoneID, recordID, recordName, ip)
}

func (d *DDNSUpdater) getZoneIDByName(client *http.Client, cfEmail, cfAPIKey, zoneName string) (string, error) {
	u := fmt.Sprintf("%s/zones?name=%s", cfAPIBase, url.QueryEscape(zoneName))
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Auth-Email", cfEmail)
	req.Header.Set("X-Auth-Key", cfAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("list zones: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("list zones: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Result []struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("parse zones response: %w", err)
	}
	if len(parsed.Result) == 0 || strings.TrimSpace(parsed.Result[0].ID) == "" {
		return "", fmt.Errorf("zone %q not found", zoneName)
	}
	return parsed.Result[0].ID, nil
}

func (d *DDNSUpdater) getRecordIDByNameWithGlobalKey(client *http.Client, cfEmail, cfAPIKey, zoneID, recordName string) (string, error) {
	listURL := fmt.Sprintf("%s/zones/%s/dns_records?name=%s&type=A", cfAPIBase, zoneID, url.QueryEscape(recordName))
	req, err := http.NewRequest(http.MethodGet, listURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Auth-Email", cfEmail)
	req.Header.Set("X-Auth-Key", cfAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("list DNS records: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("list DNS records: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var listResp cfListResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		return "", fmt.Errorf("parse list response: %w", err)
	}
	if len(listResp.Result) == 0 || strings.TrimSpace(listResp.Result[0].ID) == "" {
		return "", fmt.Errorf("DNS record %q not found in zone %s", recordName, zoneID)
	}
	return listResp.Result[0].ID, nil
}

func (d *DDNSUpdater) putRecordWithToken(client *http.Client, apiToken, zoneID, recordID, recordName, ip string) error {
	putURL := fmt.Sprintf("%s/zones/%s/dns_records/%s", cfAPIBase, zoneID, recordID)
	payload := map[string]any{"type": "A", "name": recordName, "content": ip, "ttl": 1}
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

func (d *DDNSUpdater) putRecordWithGlobalKey(client *http.Client, cfEmail, cfAPIKey, zoneID, recordID, recordName, ip string) error {
	putURL := fmt.Sprintf("%s/zones/%s/dns_records/%s", cfAPIBase, zoneID, recordID)
	payload := map[string]any{"type": "A", "name": recordName, "content": ip, "ttl": 1}
	payloadBytes, _ := json.Marshal(payload)
	putReq, err := http.NewRequest(http.MethodPut, putURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return err
	}
	putReq.Header.Set("X-Auth-Email", cfEmail)
	putReq.Header.Set("X-Auth-Key", cfAPIKey)
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

// VerifyResolvedIP resolves the domain and checks whether one of the A/AAAA results matches expectedIP.
func (d *DDNSUpdater) VerifyResolvedIP(domain, expectedIP string) (bool, []string, error) {
	domain = strings.TrimSpace(domain)
	expectedIP = strings.TrimSpace(expectedIP)
	if domain == "" {
		return false, nil, fmt.Errorf("empty domain")
	}
	if expectedIP == "" {
		return false, nil, fmt.Errorf("empty expected IP")
	}

	lookupIPs, err := net.LookupIP(domain)
	if err != nil {
		return false, nil, fmt.Errorf("lookup domain %s: %w", domain, err)
	}

	resolved := make([]string, 0, len(lookupIPs))
	expected := net.ParseIP(expectedIP)
	matched := false
	for _, ip := range lookupIPs {
		if ip == nil {
			continue
		}
		s := ip.String()
		resolved = append(resolved, s)
		if expected != nil && ip.Equal(expected) {
			matched = true
		}
	}
	sort.Strings(resolved)

	if len(resolved) == 0 {
		return false, nil, fmt.Errorf("domain %s has no resolved IP", domain)
	}
	return matched, resolved, nil
}
