package workbuddy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

type Pool struct {
	mu           sync.Mutex
	channelID    int
	raw          string
	config       Config
	proxy        string
	client       Client
	cursor       int
	cooldown     map[string]time.Time
	checkinDay   map[int]string
	checkinRetry map[int]time.Time
}

var pools = struct {
	sync.Mutex
	byID map[int]*Pool
}{byID: make(map[int]*Pool)}

func ForChannel(channel *model.Channel) (*Pool, error) {
	pools.Lock()
	defer pools.Unlock()
	p := pools.byID[channel.Id]
	if p != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
	}
	if channel.Id > 0 {
		fresh, err := model.GetChannelById(channel.Id, true)
		if err != nil {
			return nil, errors.New("WorkBuddy channel does not exist")
		}
		channel = fresh
	}
	if channel.Type != constant.ChannelTypeWorkBuddy || channel.ChannelInfo.IsMultiKey {
		return nil, errors.New("WorkBuddy requires a single channel account configuration")
	}
	if p != nil && p.raw == channel.Key && p.proxy == channel.GetSetting().Proxy {
		return p, nil
	}
	cfg, err := ParseConfig(channel.Key)
	if err != nil {
		return nil, err
	}
	client, err := service.NewProxyHttpClient(channel.GetSetting().Proxy)
	if err != nil {
		return nil, errors.New("invalid WorkBuddy proxy configuration")
	}
	next := &Pool{channelID: channel.Id, raw: channel.Key, config: cfg, proxy: channel.GetSetting().Proxy, client: Client{HTTP: client}, cooldown: make(map[string]time.Time), checkinDay: make(map[int]string), checkinRetry: make(map[int]time.Time)}
	if channel.Id > 0 {
		pools.byID[channel.Id] = next
	}
	return next, nil
}

func (p *Pool) fresh(ctx context.Context, index int, force bool) (Credential, error) {
	c := p.config.Accounts[index]
	if !force && (c.ExpiresAtMs == 0 || c.ExpiresAtMs > time.Now().Add(60*time.Second).UnixMilli()) {
		return c, nil
	}
	next, err := p.client.Refresh(ctx, c)
	if err != nil {
		return c, err
	}
	cfg := p.config
	cfg.Accounts = append([]Credential(nil), cfg.Accounts...)
	cfg.Accounts[index] = next
	encoded, err := common.Marshal(cfg)
	if err != nil {
		return c, err
	}
	if p.channelID > 0 {
		if err = model.SaveWorkBuddyCredentials(model.DB, p.channelID, p.raw, string(encoded)); err != nil {
			return c, err
		}
		if channel, e := model.GetChannelById(p.channelID, true); e == nil {
			model.CacheUpdateChannel(channel)
		}
	}
	p.config = cfg
	p.raw = string(encoded)
	return next, nil
}
func (p *Pool) Models(ctx context.Context) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, c := range p.config.Accounts {
		if c.Enabled != nil && !*c.Enabled {
			continue
		}
		credential, err := p.fresh(ctx, i, false)
		if err != nil {
			continue
		}
		models, err := p.client.Models(ctx, credential)
		if err == nil {
			return models, nil
		}
	}
	return nil, errors.New("no WorkBuddy account could fetch models; check credentials")
}
func PrepareChat(raw []byte) ([]byte, error) {
	var body map[string]any
	if common.Unmarshal(raw, &body) != nil || body == nil {
		return nil, errors.New("invalid WorkBuddy chat request")
	}
	body["stream"] = true
	delete(body, "stream_options")
	if messages, ok := body["messages"].([]any); ok {
		for _, item := range messages {
			if m, ok := item.(map[string]any); ok && m["role"] == "developer" {
				m["role"] = "system"
			}
		}
	}
	if choice, ok := body["tool_choice"].(map[string]any); ok {
		switch choice["type"] {
		case "none":
			body["tool_choice"] = "none"
		case "auto", "required":
			body["tool_choice"] = choice["type"]
		case "function":
			if f, ok := choice["function"].(map[string]any); ok {
				body["tool_choice"] = f["name"]
			} else {
				body["tool_choice"] = choice["name"]
			}
		default:
			delete(body, "tool_choice")
		}
	}
	if body["tool_choice"] == "none" {
		delete(body, "tool_choice")
		delete(body, "tools")
		delete(body, "functions")
	}
	return common.Marshal(body)
}
func (p *Pool) Chat(ctx context.Context, modelName string, raw []byte) (*http.Response, error) {
	prepared, err := PrepareChat(raw)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	n := len(p.config.Accounts)
	start := 0
	if p.config.Distribution == "round-robin" {
		start = p.cursor % n
		p.cursor++
	}
	p.mu.Unlock()
	lastStatus := http.StatusServiceUnavailable
	for offset := range n {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		i := (start + offset) % n
		cooldownKey := strconv.Itoa(i) + ":" + modelName
		p.mu.Lock()
		c := p.config.Accounts[i]
		skip := (c.Enabled != nil && !*c.Enabled) || time.Now().Before(p.cooldown[cooldownKey])
		if !skip {
			c, err = p.fresh(ctx, i, false)
		}
		p.mu.Unlock()
		if skip || err != nil {
			continue
		}
		for attempt := range 2 {
			response, e := p.client.do(ctx, c, false, "POST", "/v2/chat/completions", prepared, false)
			if e != nil {
				break
			}
			if response.StatusCode == 200 {
				if response.Header.Get("Content-Type") == "" || !bytes.Contains([]byte(response.Header.Get("Content-Type")), []byte("text/event-stream")) {
					response.Body.Close()
					return nil, errors.New("WorkBuddy upstream did not return SSE")
				}
				return response, nil
			}
			errorBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
			response.Body.Close()
			lastStatus = response.StatusCode
			if attempt == 0 && (response.StatusCode == 401 || response.StatusCode == 403 || bytes.Contains(errorBody, []byte("12153"))) {
				p.mu.Lock()
				if current := p.config.Accounts[i]; current.AccessToken != c.AccessToken {
					c, err = current, nil
				} else {
					c, err = p.fresh(ctx, i, true)
				}
				p.mu.Unlock()
				if err == nil {
					continue
				}
			}
			if response.StatusCode == 429 || response.StatusCode == 402 {
				p.mu.Lock()
				p.cooldown[cooldownKey] = time.Now().Add(15 * time.Minute)
				p.mu.Unlock()
			}
			break
		}
	}
	if lastStatus < 400 || lastStatus > 599 {
		lastStatus = 502
	}
	body, _ := common.Marshal(map[string]any{"error": map[string]any{"message": "WorkBuddy accounts are unavailable; check sign-in, credits or cooldown", "type": "workbuddy_error", "code": "workbuddy_unavailable"}})
	return &http.Response{StatusCode: lastStatus, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(body))}, nil
}
func (p *Pool) Status() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	accounts := make([]map[string]any, 0, len(p.config.Accounts))
	for i, c := range p.config.Accounts {
		accounts = append(accounts, map[string]any{"index": i, "nickname": c.Nickname, "region": c.Region(), "enabled": c.Enabled == nil || *c.Enabled, "expiresAtMs": c.ExpiresAtMs, "checkinDay": p.checkinDay[i]})
	}
	return map[string]any{"accounts": accounts, "distribution": p.config.Distribution, "autoCheckin": p.config.AutoCheckin == nil || *p.config.AutoCheckin, "checkinTimezone": "Asia/Shanghai", "checkinHour": 8}
}
func (p *Pool) AccountAction(ctx context.Context, index int, action string) (any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index < 0 || index >= len(p.config.Accounts) {
		return nil, errors.New("WorkBuddy account does not exist")
	}
	if action != "refresh" && action != "credits" && action != "checkin" {
		return nil, errors.New("unsupported WorkBuddy account action")
	}
	if action == "checkin" && p.config.Accounts[index].Enabled != nil && !*p.config.Accounts[index].Enabled {
		return nil, errors.New("WorkBuddy account is disabled")
	}
	c, err := p.fresh(ctx, index, action == "refresh")
	if err != nil {
		return nil, err
	}
	switch action {
	case "refresh":
		return map[string]bool{"ok": true}, nil
	case "credits":
		return p.client.Credits(ctx, c)
	case "checkin":
		status, err := p.client.Checkin(ctx, c)
		if err == nil && status["today_checked_in"] == true {
			p.checkinDay[index] = time.Now().In(time.FixedZone("China", 8*3600)).Format("2006-01-02")
		}
		return status, err
	default:
		return nil, errors.New("unsupported WorkBuddy account action")
	}
}
func (p *Pool) tick(ctx context.Context, now time.Time) {
	zone := time.FixedZone("China", 8*3600)
	local := now.In(zone)
	if local.Hour() < 8 {
		return
	}
	day := local.Format("2006-01-02")
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.config.AutoCheckin != nil && !*p.config.AutoCheckin {
		return
	}
	for i, c := range p.config.Accounts {
		if c.Enabled != nil && !*c.Enabled || p.checkinDay[i] == day || now.Before(p.checkinRetry[i]) {
			continue
		}
		credential, err := p.fresh(ctx, i, false)
		if err == nil {
			_, err = p.client.Checkin(ctx, credential)
		}
		if err == nil {
			p.checkinDay[i] = day
			delete(p.checkinRetry, i)
		} else {
			p.checkinRetry[i] = now.Add(15 * time.Minute)
		}
	}
}

// The master performs startup catch-up and daily checks. Upstream status is read
// before every claim, so restart does not duplicate a day's reward.
func StartAutoCheckin() {
	if !common.IsMasterNode {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			var channels []*model.Channel
			if model.DB.Where("type = ? AND status = ?", constant.ChannelTypeWorkBuddy, common.ChannelStatusEnabled).Find(&channels).Error == nil {
				for _, channel := range channels {
					p, err := ForChannel(channel)
					if err == nil {
						ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
						p.tick(ctx, time.Now())
						cancel()
					}
				}
			}
			<-ticker.C
		}
	}()
}

func (p *Pool) Settings(distribution *string, autoCheckin *bool, index *int, enabled *bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	cfg := p.config
	cfg.Accounts = append([]Credential(nil), cfg.Accounts...)
	if distribution != nil {
		if *distribution != "priority" && *distribution != "round-robin" {
			return errors.New("invalid account distribution")
		}
		cfg.Distribution = *distribution
	}
	if autoCheckin != nil {
		cfg.AutoCheckin = autoCheckin
	}
	if (index == nil) != (enabled == nil) {
		return errors.New("invalid account settings")
	}
	if index != nil {
		if *index < 0 || *index >= len(cfg.Accounts) || enabled == nil {
			return errors.New("invalid account settings")
		}
		cfg.Accounts[*index].Enabled = enabled
	}
	raw, err := common.Marshal(cfg)
	if err != nil {
		return err
	}
	if p.channelID > 0 {
		if err = model.SaveWorkBuddyCredentials(model.DB, p.channelID, p.raw, string(raw)); err != nil {
			return err
		}
		if ch, e := model.GetChannelById(p.channelID, true); e == nil {
			model.CacheUpdateChannel(ch)
		}
	}
	p.raw = string(raw)
	p.config = cfg
	return nil
}
