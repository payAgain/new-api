// WorkBuddy protocol behavior is based on local.pi-desktop-workbuddy 1.0.7
// (MIT), derived from aosi526/dsh-workbuddy-xdpool. See LICENSE.workbuddy.
package workbuddy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type Credential struct {
	AccessToken     string `json:"accessToken"`
	RefreshToken    string `json:"refreshToken"`
	ExpiresAtMs     int64  `json:"expiresAtMs"`
	LastRefreshAtMs int64  `json:"lastRefreshAtMs,omitempty"`
	UID             string `json:"uid,omitempty"`
	UIN             string `json:"uin,omitempty"`
	EnterpriseID    string `json:"enterpriseId,omitempty"`
	Domain          string `json:"domain"`
	Nickname        string `json:"nickname,omitempty"`
	Enabled         *bool  `json:"enabled,omitempty"`
}
type Config struct {
	Accounts     []Credential `json:"accounts"`
	Distribution string       `json:"distribution,omitempty"`
	AutoCheckin  *bool        `json:"autoCheckin,omitempty"`
}

func ParseConfig(raw string) (Config, error) {
	var cfg Config
	if strings.HasPrefix(strings.TrimSpace(raw), "[") {
		if common.UnmarshalJsonStr(raw, &cfg.Accounts) != nil {
			return cfg, errors.New("invalid WorkBuddy account JSON")
		}
	} else {
		if common.UnmarshalJsonStr(raw, &cfg) != nil {
			return cfg, errors.New("invalid WorkBuddy account JSON")
		}
		if len(cfg.Accounts) == 0 {
			var c Credential
			if common.UnmarshalJsonStr(raw, &c) == nil && c.AccessToken != "" {
				cfg.Accounts = []Credential{c}
			}
		}
	}
	if len(cfg.Accounts) == 0 || len(cfg.Accounts) > 64 {
		return cfg, errors.New("WorkBuddy requires 1 to 64 accounts")
	}
	if cfg.Distribution == "" {
		cfg.Distribution = "priority"
	}
	if cfg.Distribution != "priority" && cfg.Distribution != "round-robin" {
		return cfg, errors.New("invalid WorkBuddy account distribution")
	}
	region := ""
	for i := range cfg.Accounts {
		cfg.Accounts[i].Domain = strings.ToLower(strings.TrimSpace(cfg.Accounts[i].Domain))
		c := cfg.Accounts[i]
		for _, value := range []string{c.AccessToken, c.RefreshToken, c.UID, c.UIN, c.EnterpriseID} {
			if len(value) > 16384 || strings.ContainsAny(value, "\r\n") {
				return cfg, errors.New("invalid WorkBuddy credential field")
			}
		}
		if strings.TrimSpace(c.AccessToken) == "" || strings.TrimSpace(c.RefreshToken) == "" {
			return cfg, errors.New("WorkBuddy accessToken and refreshToken are required")
		}
		if c.UID == "" && c.UIN == "" {
			return cfg, errors.New("WorkBuddy uid or uin is required")
		}
		if strings.ContainsAny(c.Domain, "\r\n/") || len(c.Domain) > 255 {
			return cfg, errors.New("invalid WorkBuddy domain")
		}
		if region != "" && region != c.Region() {
			return cfg, errors.New("use separate channels for domestic and international WorkBuddy accounts")
		}
		region = c.Region()
	}
	return cfg, nil
}
func (c Credential) Region() string {
	d := strings.ToLower(strings.TrimSpace(c.Domain))
	for _, host := range []string{"workbuddy.ai", "workbuddy.cc", "codebuddy.ai"} {
		if d == host || strings.HasSuffix(d, "."+host) {
			return "global"
		}
	}
	return "cn"
}
func (c Credential) base(billing bool) string {
	if c.Region() == "global" {
		if c.Domain == "codebuddy.ai" || strings.HasSuffix(c.Domain, ".codebuddy.ai") {
			return "https://www.codebuddy.ai"
		}
		return "https://www.workbuddy.ai"
	}
	if billing {
		return "https://www.codebuddy.cn"
	}
	return "https://copilot.tencent.com"
}
func (c Credential) headers(billing bool) http.Header {
	h := http.Header{"Authorization": []string{"Bearer " + c.AccessToken}, "Content-Type": []string{"application/json"}, "Accept": []string{"application/json"}, "User-Agent": []string{"CLI/2.63.2 CodeBuddy/2.63.2"}}
	if c.UID != "" {
		h.Set("X-User-Id", c.UID)
	} else {
		h.Set("X-No-User-Id", "1")
	}
	if c.EnterpriseID != "" {
		h.Set("X-Enterprise-Id", c.EnterpriseID)
		if billing {
			h.Set("X-Tenant-Id", c.EnterpriseID)
		}
	} else {
		h.Set("X-No-Enterprise-Id", "1")
	}
	if c.Domain != "" {
		h.Set("X-Domain", c.Domain)
	} else {
		h.Set("X-No-Department-Info", "1")
	}
	if !billing {
		h.Set("X-Product", "SaaS")
		h.Set("X-Requested-With", "XMLHttpRequest")
		h.Set("Origin", c.base(true))
		h.Set("Referer", c.base(true)+"/")
	}
	return h
}

type Client struct{ HTTP *http.Client }

func (c Client) do(ctx context.Context, credential Credential, billing bool, method, path string, data []byte, refresh bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, credential.base(billing)+path, bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("invalid WorkBuddy request")
	}
	req.Header = credential.headers(billing)
	if refresh {
		req.Header.Del("Authorization")
		req.Header.Set("X-Refresh-Token", credential.RefreshToken)
		req.Header.Set("X-Auth-Refresh-Source", "workbuddy")
	}
	if credential.Region() == "global" && path == "/v3/config" {
		req.Header.Set("User-Agent", "WorkBuddy/5.5.2")
	}
	response, err := c.HTTP.Do(req)
	if err != nil {
		return nil, errors.New("WorkBuddy upstream transport failed")
	}
	return response, nil
}
func (c Client) envelope(ctx context.Context, credential Credential, billing bool, method, path string, data []byte, refresh bool, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := c.do(ctx, credential, billing, method, path, data, refresh)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil || len(raw) > 8<<20 {
		return errors.New("WorkBuddy upstream read failed")
	}
	var envelope struct {
		Code int `json:"code"`
		Data any `json:"data"`
	}
	if common.Unmarshal(raw, &envelope) != nil || resp.StatusCode != 200 || envelope.Code != 0 {
		return fmt.Errorf("WorkBuddy operation failed (HTTP %d)", resp.StatusCode)
	}
	encoded, err := common.Marshal(envelope.Data)
	if err != nil {
		return errors.New("WorkBuddy upstream data invalid")
	}
	return common.Unmarshal(encoded, out)
}
func (c Client) Refresh(ctx context.Context, credential Credential) (Credential, error) {
	var data struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int64  `json:"expiresIn"`
		Domain       string `json:"domain"`
	}
	err := c.envelope(ctx, credential, false, "POST", "/v2/plugin/auth/token/refresh", nil, true, &data)
	if err != nil {
		return credential, err
	}
	if data.AccessToken == "" {
		return credential, errors.New("WorkBuddy refresh returned no access token; sign in again")
	}
	originalRegion := credential.Region()
	credential.AccessToken = data.AccessToken
	if data.RefreshToken != "" {
		credential.RefreshToken = data.RefreshToken
	}
	if data.Domain != "" {
		credential.Domain = data.Domain
	}
	encoded, err := common.Marshal(credential)
	if err != nil {
		return credential, errors.New("invalid WorkBuddy credential data")
	}
	normalized, err := ParseConfig(string(encoded))
	if err != nil || normalized.Accounts[0].Region() != originalRegion {
		return credential, errors.New("WorkBuddy refresh returned invalid credential data")
	}
	credential = normalized.Accounts[0]
	credential.LastRefreshAtMs = time.Now().UnixMilli()
	credential.ExpiresAtMs = 0
	if data.ExpiresIn > 0 && data.ExpiresIn <= 365*86400 {
		credential.ExpiresAtMs = time.Now().Add(time.Duration(data.ExpiresIn) * time.Second).UnixMilli()
	}
	return credential, nil
}
func (c Client) Models(ctx context.Context, credential Credential) ([]string, error) {
	path := "/v2/enterprises/personal/models"
	if credential.Region() == "global" {
		path = "/v3/config"
	}
	var data struct {
		Models []struct {
			ID       string `json:"id"`
			Disabled bool   `json:"disabled"`
			Input    int64  `json:"maxInputTokens"`
			Output   int64  `json:"maxOutputTokens"`
		} `json:"models"`
		Agents []struct {
			Name   string   `json:"name"`
			Models []string `json:"models"`
		} `json:"agents"`
	}
	if err := c.envelope(ctx, credential, false, "GET", path, nil, false, &data); err != nil {
		return nil, err
	}
	var cli []string
	for _, a := range data.Agents {
		if a.Name == "cli" {
			cli = a.Models
			break
		}
	}
	var models []string
	for _, m := range data.Models {
		if m.ID != "" && !m.Disabled && m.Input > 0 && m.Output > 0 && (len(cli) == 0 || slices.Contains(cli, m.ID)) {
			models = append(models, m.ID)
		}
	}
	if len(models) == 0 {
		return nil, errors.New("WorkBuddy model catalog is empty")
	}
	return models, nil
}
func (c Client) Checkin(ctx context.Context, credential Credential) (map[string]any, error) {
	var status map[string]any
	err := c.envelope(ctx, credential, true, "POST", "/v2/billing/meter/checkin-activity-status", []byte("{}"), false, &status)
	if err != nil {
		return nil, err
	}
	if status["today_checked_in"] == true || status["active"] != true {
		return status, nil
	}
	var claim map[string]any
	if err = c.envelope(ctx, credential, true, "POST", "/v2/billing/meter/daily-checkin", []byte("{}"), false, &claim); err != nil {
		return nil, err
	}
	if err = c.envelope(ctx, credential, true, "POST", "/v2/billing/meter/checkin-activity-status", []byte("{}"), false, &status); err != nil {
		return nil, err
	}
	if status["today_checked_in"] != true {
		return nil, errors.New("WorkBuddy checkin has not been confirmed")
	}
	return status, nil
}
func (c Client) Credits(ctx context.Context, credential Credential) (map[string]any, error) {
	now := time.Now()
	body, _ := common.Marshal(map[string]any{"PageNumber": 1, "PageSize": 100, "ProductCode": "p_tcaca", "Status": []int{0, 3}, "PackageEndTimeRangeBegin": now.Format("2006-01-02 15:04:05"), "PackageEndTimeRangeEnd": now.AddDate(101, 0, 0).Format("2006-01-02 15:04:05")})
	var data map[string]any
	err := c.envelope(ctx, credential, true, "POST", "/v2/billing/meter/get-user-resource", body, false, &data)
	return data, err
}
