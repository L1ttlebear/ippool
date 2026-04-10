package api

import (
	"fmt"
	"strings"

	"github.com/L1ttlebear/ippool/config"
	"github.com/L1ttlebear/ippool/database/dbcore"
	"github.com/L1ttlebear/ippool/database/models"
	"github.com/L1ttlebear/ippool/engine"
)

type ddnsRuleApplyStatus struct {
	Pool       string   `json:"pool"`
	Domain     string   `json:"domain"`
	LeaderID   uint     `json:"leader_id,omitempty"`
	LeaderName string   `json:"leader_name,omitempty"`
	ExpectedIP string   `json:"expected_ip,omitempty"`
	ResolvedIP []string `json:"resolved_ips,omitempty"`
	OK         bool     `json:"ok"`
	Message    string   `json:"message"`
}

func applyDDNSRulesNow(rules []config.DdnsPoolRule) []ddnsRuleApplyStatus {
	updater := &engine.DDNSUpdater{}
	res := make([]ddnsRuleApplyStatus, 0, len(rules))

	for _, r := range rules {
		pool := strings.TrimSpace(r.Pool)
		domain := strings.TrimSpace(r.RecordName)
		item := ddnsRuleApplyStatus{Pool: pool, Domain: domain, OK: false}

		if !r.Enabled {
			item.Message = "规则已禁用"
			res = append(res, item)
			continue
		}
		if pool == "" || domain == "" || strings.TrimSpace(r.CFApiToken) == "" || strings.TrimSpace(r.CFZoneID) == "" {
			item.Message = "规则不完整，需填写 pool/token/zone/record"
			res = append(res, item)
			continue
		}

		leader, err := findPoolLeader(pool)
		if err != nil {
			item.Message = err.Error()
			res = append(res, item)
			continue
		}
		item.LeaderID = leader.ID
		item.LeaderName = leader.Name
		item.ExpectedIP = leader.IP

		if err := updater.Update(strings.TrimSpace(r.CFApiToken), strings.TrimSpace(r.CFZoneID), domain, leader.IP); err != nil {
			item.Message = fmt.Sprintf("更新 DDNS 失败: %v", err)
			res = append(res, item)
			continue
		}

		ok, ips, err := updater.VerifyResolvedIP(domain, leader.IP)
		item.ResolvedIP = ips
		item.OK = ok
		if err != nil {
			item.Message = fmt.Sprintf("更新成功，但解析校验失败: %v", err)
		} else if ok {
			item.Message = "DDNS 成功：域名解析已指向当前 Pool Leader"
		} else {
			item.Message = "DDNS 失败：域名解析 IP 与当前 Pool Leader 不一致"
		}
		res = append(res, item)
	}

	return res
}

func getDDNSRuleStatuses(rules []config.DdnsPoolRule) []ddnsRuleApplyStatus {
	updater := &engine.DDNSUpdater{}
	res := make([]ddnsRuleApplyStatus, 0, len(rules))

	for _, r := range rules {
		pool := strings.TrimSpace(r.Pool)
		domain := strings.TrimSpace(r.RecordName)
		item := ddnsRuleApplyStatus{Pool: pool, Domain: domain, OK: false}

		if !r.Enabled {
			item.Message = "规则已禁用"
			res = append(res, item)
			continue
		}
		if pool == "" || domain == "" {
			item.Message = "规则不完整"
			res = append(res, item)
			continue
		}

		leader, err := findPoolLeader(pool)
		if err != nil {
			item.Message = err.Error()
			res = append(res, item)
			continue
		}
		item.LeaderID = leader.ID
		item.LeaderName = leader.Name
		item.ExpectedIP = leader.IP

		ok, ips, err := updater.VerifyResolvedIP(domain, leader.IP)
		item.ResolvedIP = ips
		item.OK = ok
		if err != nil {
			item.Message = fmt.Sprintf("校验失败: %v", err)
		} else if ok {
			item.Message = "DDNS 正常"
		} else {
			item.Message = "DDNS 未指向当前 Leader"
		}
		res = append(res, item)
	}
	return res
}

func findPoolLeader(pool string) (*models.Host, error) {
	gdb := dbcore.GetDBInstance()
	var host models.Host
	err := gdb.Where("pool = ? AND state = ?", pool, models.StateReady).Order("priority asc, id asc").First(&host).Error
	if err != nil {
		return nil, fmt.Errorf("pool %s 无可用 leader", pool)
	}
	return &host, nil
}
