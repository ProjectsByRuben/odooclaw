package providers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModelCatalogUsesAdvertisedCapabilities(t *testing.T) {
	data := []byte(`{"data":[{"id":"m1","displayName":"One","isDefault":true,"defaultReasoningEffort":"medium","supportedReasoningEfforts":[{"reasoningEffort":"medium"},{"reasoningEffort":"high"}],"serviceTiers":[{"id":"priority"}]},{"id":"hidden","hidden":true},{"id":"--config=evil"}],"nextCursor":"next"}`)
	choices, next, err := parseModelChoices(data)
	if err != nil || len(choices) != 1 || next != "next" || len(choices[0].Efforts) != 2 || !containsModelValue(choices[0].Tiers, "priority") {
		t.Fatalf("invalid catalog: %+v %s %v", choices, next, err)
	}
}
func TestModelSettingsRejectUnsupportedAndStayRequestLocal(t *testing.T) {
	p := &CodexCliProvider{}
	p.catalogOnce.Do(func() {})
	p.catalogReader = func(context.Context) ModelCatalog {
		return ModelCatalog{Available: true, Models: []ModelChoice{{ID: "m1", Default: true, Efforts: []string{"medium"}, Tiers: []string{"default", "priority"}}}}
	}
	custom := WithCodexSettings(context.Background(), ChatModelSettings{Model: "m1", ReasoningEffort: "medium", ServiceTier: "priority"})
	got, err := p.validateChatSettings(custom)
	if err != nil || got.Model != "m1" {
		t.Fatal(got, err)
	}
	plain, err := p.validateChatSettings(context.Background())
	if err != nil || plain != (ChatModelSettings{}) {
		t.Fatal("settings leaked across chats", plain, err)
	}
	for _, s := range []ChatModelSettings{{Model: "other"}, {Model: "m1", ReasoningEffort: "ultra"}, {Model: "m1", ServiceTier: "unknown"}} {
		if _, err := p.validateChatSettings(WithCodexSettings(context.Background(), s)); err == nil {
			t.Fatal("unsupported selection accepted", s)
		}
	}
}
func TestCodexSettingsArePerProcessArguments(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "codex")
	argsFile := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + argsFile + "'\ncat >/dev/null\nprintf '%s\\n' '{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"ok\"}}'\n"
	if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	p := &CodexCliProvider{command: exe}
	p.catalogOnce.Do(func() {})
	p.catalogReader = func(context.Context) ModelCatalog {
		return ModelCatalog{Available: true, Models: []ModelChoice{{ID: "m1", Efforts: []string{"high"}, Tiers: []string{"default", "priority"}}}}
	}
	ctx := WithCodexSettings(context.Background(), ChatModelSettings{Model: "m1", ReasoningEffort: "high", ServiceTier: "priority"})
	if _, err := p.Chat(ctx, nil, nil, "codex-cli", nil); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(argsFile)
	for _, part := range []string{"-m\nm1", `model_reasoning_effort="high"`, `service_tier="priority"`} {
		if !strings.Contains(string(args), part) {
			t.Fatal("missing CLI setting", part, string(args))
		}
	}
	if _, err := p.Chat(context.Background(), nil, nil, "codex-cli", nil); err != nil {
		t.Fatal(err)
	}
	args, _ = os.ReadFile(argsFile)
	if strings.Contains(string(args), "reasoning_effort") || strings.Contains(string(args), "service_tier") || strings.Contains(string(args), "-m\n") {
		t.Fatal("other chat inherited override", string(args))
	}
}
