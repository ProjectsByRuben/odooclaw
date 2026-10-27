package providers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ModelQuota deliberately excludes account identity, credentials and billing data.
type ModelQuota struct {
	Available bool          `json:"available"`
	Scope     string        `json:"scope"`
	Windows   []QuotaWindow `json:"windows"`
	CheckedAt int64         `json:"checked_at"`
}
type QuotaWindow struct {
	Remaining       float64 `json:"remaining"`
	DurationMinutes int     `json:"duration_minutes"`
	ResetsAt        int64   `json:"resets_at"`
}

// NewQuotaReader uses the existing CLI login without running an inference turn.
// The cache is shared by every chat, including unsuccessful reads.
func (p *CodexCliProvider) NewQuotaReader() func(context.Context) ModelQuota {
	var mu sync.Mutex
	var snapshot ModelQuota
	var expires time.Time
	return func(ctx context.Context) ModelQuota {
		mu.Lock()
		defer mu.Unlock()
		if time.Now().Before(expires) {
			return snapshot
		}
		snapshot = ModelQuota{Scope: "shared_codex_account", CheckedAt: time.Now().Unix()}
		readCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		if result, err := p.readQuota(readCtx); err == nil {
			snapshot = result
		}
		expires = time.Now().Add(time.Minute)
		return snapshot
	}
}

func (p *CodexCliProvider) readQuota(ctx context.Context) (ModelQuota, error) {
	data, err := p.readCodexRPC(ctx, "account/rateLimits/read", nil)
	if err != nil {
		return ModelQuota{}, err
	}
	return parseQuota(data)
}

func (p *CodexCliProvider) readCodexRPC(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	cmd := exec.CommandContext(ctx, p.command, "app-server")
	prepareQuotaCommand(cmd)
	cmd.Cancel = func() error { return terminateQuotaCommand(cmd) }
	if p.workspace != "" {
		cmd.Dir = p.workspace
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "ODOO_") && !strings.HasPrefix(key, "ODOOCLAW_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Stderr = io.Discard
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	defer func() { input.Close(); terminateQuotaCommand(cmd); cmd.Wait() }()
	encoder := json.NewEncoder(input)
	if err = encoder.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "odooclaw_quota", "version": "1.0.0"}}}); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var event struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		if event.ID != 1 && event.ID != 2 {
			continue
		}
		if len(event.Error) > 0 && string(event.Error) != "null" {
			return nil, fmt.Errorf("quota RPC unavailable")
		}
		if event.ID == 1 {
			if err = encoder.Encode(map[string]any{"method": "initialized"}); err != nil {
				return nil, err
			}
			if err = encoder.Encode(map[string]any{"id": 2, "method": method, "params": params}); err != nil {
				return nil, err
			}
		} else {
			return event.Result, nil
		}
	}
	return nil, fmt.Errorf("quota RPC ended without a snapshot")
}

func parseQuota(data []byte) (ModelQuota, error) {
	type window struct {
		Used     *float64 `json:"usedPercent"`
		Duration int      `json:"windowDurationMins"`
		Reset    int64    `json:"resetsAt"`
	}
	type bucket struct {
		ID        string  `json:"limitId"`
		Primary   *window `json:"primary"`
		Secondary *window `json:"secondary"`
	}
	var result struct {
		Limits  bucket            `json:"rateLimits"`
		Buckets map[string]bucket `json:"rateLimitsByLimitId"`
	}
	snapshot := ModelQuota{Scope: "shared_codex_account", CheckedAt: time.Now().Unix()}
	if err := json.Unmarshal(data, &result); err != nil {
		return snapshot, err
	}
	limits := result.Limits
	if v, ok := result.Buckets["codex"]; ok {
		limits = v
	}
	// Other metered models are not a substitute for the Codex account bucket.
	if limits.ID != "" && limits.ID != "codex" {
		return snapshot, nil
	}
	for _, w := range []*window{limits.Primary, limits.Secondary} {
		if w == nil || w.Used == nil || math.IsNaN(*w.Used) || *w.Used < 0 || *w.Used > 100 || w.Duration <= 0 || w.Reset <= 0 {
			continue
		}
		snapshot.Windows = append(snapshot.Windows, QuotaWindow{Remaining: 100 - *w.Used, DurationMinutes: w.Duration, ResetsAt: w.Reset})
	}
	snapshot.Available = len(snapshot.Windows) > 0
	return snapshot, nil
}
