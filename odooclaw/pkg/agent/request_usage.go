package agent

import (
	"context"
	"sync"

	"github.com/nicolasramos/odooclaw/pkg/bus"
	"github.com/nicolasramos/odooclaw/pkg/providers"
)

type requestUsageKey struct{}

type requestUsage struct {
	mu    sync.Mutex
	usage bus.TokenUsage
}

func withRequestUsage(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestUsageKey{}, &requestUsage{usage: bus.TokenUsage{Complete: true}})
}

func usageSnapshot(ctx context.Context) *bus.TokenUsage {
	collector, ok := ctx.Value(requestUsageKey{}).(*requestUsage)
	if !ok {
		return nil
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	snapshot := collector.usage
	return &snapshot
}

// Record each actual provider attempt, including retry/fallback attempts. Unknown
// consumption must remain visible rather than be reported as a definitive zero.
func recordRequestUsage(ctx context.Context, response *providers.LLMResponse) {
	collector, ok := ctx.Value(requestUsageKey{}).(*requestUsage)
	if !ok {
		return
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	collector.usage.ModelCalls++
	if response == nil || response.Usage == nil {
		collector.usage.Complete = false
		return
	}
	usage := response.Usage
	if usage.PromptTokens < 0 || usage.CompletionTokens < 0 || usage.CachedPromptTokens < 0 || usage.CachedPromptTokens > usage.PromptTokens {
		collector.usage.Complete = false
		return
	}
	collector.usage.InputTokens += usage.PromptTokens
	collector.usage.OutputTokens += usage.CompletionTokens
	collector.usage.CachedInputTokens += usage.CachedPromptTokens
	collector.usage.TotalTokens = collector.usage.InputTokens + collector.usage.OutputTokens
}

func chatWithRequestUsage(ctx context.Context, provider providers.LLMProvider, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]any) (*providers.LLMResponse, error) {
	response, err := provider.Chat(ctx, messages, tools, model, options)
	recordRequestUsage(ctx, response)
	return response, err
}
