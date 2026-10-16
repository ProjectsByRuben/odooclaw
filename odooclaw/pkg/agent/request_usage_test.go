package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/nicolasramos/odooclaw/pkg/providers"
)

func TestRequestUsageTotalsAndUnknownAttempts(t *testing.T) {
	ctx := withRequestUsage(context.Background())
	response := &providers.LLMResponse{Usage: &providers.UsageInfo{PromptTokens: 100, CachedPromptTokens: 40, CompletionTokens: 20, TotalTokens: 120}}
	for i := 0; i < 3; i++ {
		recordRequestUsage(ctx, response)
	}
	got := usageSnapshot(ctx)
	if got.TotalTokens != 360 || got.InputTokens != 300 || got.CachedInputTokens != 120 || got.OutputTokens != 60 || got.ModelCalls != 3 || !got.Complete {
		t.Fatalf("wrong totals: %+v", got)
	}
	recordRequestUsage(ctx, nil)
	partial := usageSnapshot(ctx)
	if partial.Complete || partial.TotalTokens != 360 || partial.ModelCalls != 4 {
		t.Fatalf("missing usage must be partial: %+v", partial)
	}
	if !got.Complete || got.ModelCalls != 3 {
		t.Fatal("snapshot changed after publication")
	}
	if usageSnapshot(context.Background()) != nil {
		t.Fatal("unrelated context has telemetry")
	}
}

func TestRequestUsageParallelIsolation(t *testing.T) {
	var requests sync.WaitGroup
	for request := 1; request <= 16; request++ {
		requests.Add(1)
		go func(tokens int) {
			defer requests.Done()
			ctx := withRequestUsage(context.Background())
			var calls sync.WaitGroup
			for i := 0; i < 5; i++ {
				calls.Add(1)
				go func() {
					defer calls.Done()
					recordRequestUsage(ctx, &providers.LLMResponse{Usage: &providers.UsageInfo{PromptTokens: tokens, CompletionTokens: 2}})
				}()
			}
			calls.Wait()
			usage := usageSnapshot(ctx)
			if usage.TotalTokens != 5*(tokens+2) || usage.ModelCalls != 5 || !usage.Complete {
				t.Errorf("request %d contaminated: %+v", tokens, usage)
			}
		}(request)
	}
	requests.Wait()
}
