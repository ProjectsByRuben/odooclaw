package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type ChatModelSettings struct {
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort"`
	ServiceTier     string `json:"service_tier"`
}
type ModelChoice struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Default          bool     `json:"is_default"`
	DefaultReasoning string   `json:"default_reasoning"`
	Efforts          []string `json:"efforts"`
	Tiers            []string `json:"tiers"`
}
type ModelCatalog struct {
	Available bool          `json:"available"`
	Models    []ModelChoice `json:"models"`
}
type chatSettingsKey struct{}

func WithCodexSettings(ctx context.Context, s ChatModelSettings) context.Context {
	return context.WithValue(ctx, chatSettingsKey{}, s)
}

// Account catalog only: no inference, account identity, billing or credentials.
func (p *CodexCliProvider) ModelCatalog(ctx context.Context) ModelCatalog {
	p.catalogOnce.Do(func() {
		var mu sync.Mutex
		var value ModelCatalog
		var expiry time.Time
		p.catalogReader = func(ctx context.Context) ModelCatalog {
			mu.Lock()
			defer mu.Unlock()
			if time.Now().Before(expiry) {
				return value
			}
			readCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()
			value = ModelCatalog{}
			models := []ModelChoice{}
			cursor := ""
			for page := 0; page < 20; page++ {
				params := map[string]any{"limit": 100, "includeHidden": false}
				if cursor != "" {
					params["cursor"] = cursor
				}
				data, err := p.readCodexRPC(readCtx, "model/list", params)
				if err != nil {
					expiry = time.Now().Add(30 * time.Second)
					return value
				}
				choices, next, err := parseModelChoices(data)
				if err != nil {
					expiry = time.Now().Add(30 * time.Second)
					return value
				}
				models = append(models, choices...)
				if next == "" {
					value = ModelCatalog{Available: len(models) > 0, Models: models}
					break
				}
				if next == cursor {
					break
				}
				cursor = next
			}
			expiry = time.Now().Add(5 * time.Minute)
			return value
		}
	})
	return p.catalogReader(ctx)
}
func parseModelChoices(data []byte) ([]ModelChoice, string, error) {
	var raw struct {
		Data []struct {
			ID            string `json:"id"`
			Model         string `json:"model"`
			Name          string `json:"displayName"`
			Hidden        bool   `json:"hidden"`
			Default       bool   `json:"isDefault"`
			DefaultEffort string `json:"defaultReasoningEffort"`
			Efforts       []struct {
				Effort string `json:"reasoningEffort"`
			} `json:"supportedReasoningEfforts"`
			Tiers []struct {
				ID string `json:"id"`
			} `json:"serviceTiers"`
			Speeds []string `json:"additionalSpeedTiers"`
		} `json:"data"`
		Next string `json:"nextCursor"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, "", err
	}
	result := []ModelChoice{}
	for _, m := range raw.Data {
		id := m.Model
		if id == "" {
			id = m.ID
		}
		if m.Hidden || !safeModelValue(id) {
			continue
		}
		item := ModelChoice{ID: id, Name: m.Name, Default: m.Default, DefaultReasoning: m.DefaultEffort, Efforts: []string{}, Tiers: []string{"default"}}
		for _, e := range m.Efforts {
			if safeModelValue(e.Effort) {
				item.Efforts = append(item.Efforts, e.Effort)
			}
		}
		for _, t := range m.Tiers {
			if safeModelValue(t.ID) && !containsModelValue(item.Tiers, t.ID) {
				item.Tiers = append(item.Tiers, t.ID)
			}
		}
		for _, v := range m.Speeds {
			if v == "fast" && !containsModelValue(item.Tiers, "priority") {
				item.Tiers = append(item.Tiers, "priority")
			}
		}
		if item.Name == "" {
			item.Name = id
		}
		result = append(result, item)
	}
	return result, raw.Next, nil
}
func safeModelValue(v string) bool {
	if v == "" || len(v) > 128 || strings.HasPrefix(v, "-") {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.:/", c)) {
			return false
		}
	}
	return true
}
func containsModelValue(v []string, s string) bool {
	for _, x := range v {
		if x == s {
			return true
		}
	}
	return false
}
func (p *CodexCliProvider) validateChatSettings(ctx context.Context) (ChatModelSettings, error) {
	s, _ := ctx.Value(chatSettingsKey{}).(ChatModelSettings)
	if s == (ChatModelSettings{}) {
		return s, nil
	}
	catalog := p.ModelCatalog(ctx)
	if !catalog.Available {
		return s, fmt.Errorf("no se puede verificar el catálogo de modelos de la suscripción")
	}
	for _, m := range catalog.Models {
		if m.ID == s.Model || s.Model == "" && m.Default {
			if s.ReasoningEffort != "" && !containsModelValue(m.Efforts, s.ReasoningEffort) {
				break
			}
			if s.ServiceTier != "" && !containsModelValue(m.Tiers, s.ServiceTier) {
				break
			}
			s.Model = m.ID
			return s, nil
		}
	}
	return s, fmt.Errorf("modelo, razonamiento o rapidez no disponibles en la suscripción")
}
