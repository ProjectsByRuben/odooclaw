package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nicolasramos/odooclaw/pkg/bus"
	"github.com/nicolasramos/odooclaw/pkg/config"
	"github.com/nicolasramos/odooclaw/pkg/providers"
)

type concurrentOdooProvider struct {
	started chan string
	release chan struct{}
	active  atomic.Int32
	peak    atomic.Int32
}

func (p *concurrentOdooProvider) GetDefaultModel() string { return "test-model" }
func (p *concurrentOdooProvider) Chat(ctx context.Context, messages []providers.Message, _ []providers.ToolDefinition, _ string, _ map[string]any) (*providers.LLMResponse, error) {
	n := p.active.Add(1)
	defer p.active.Add(-1)
	for old := p.peak.Load(); n > old && !p.peak.CompareAndSwap(old, n); old = p.peak.Load() {
	}
	text := ""
	for _, m := range messages {
		if m.Role == "user" {
			text = m.Content
		}
	}
	p.started <- text
	select {
	case <-p.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &providers.LLMResponse{Content: text}, nil
}
func testConcurrentLoop(t *testing.T, p *concurrentOdooProvider) (*AgentLoop, *bus.MessageBus) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = t.TempDir()
	cfg.Agents.Defaults.Model = "test-model"
	cfg.Tools.MCP.Enabled = false
	cfg.Session.DMScope = "per-channel-peer"
	b := bus.NewMessageBus()
	return NewAgentLoop(cfg, b, p), b
}
func odooTestMessage(uid, conversation int, content string) bus.InboundMessage {
	return bus.InboundMessage{Channel: "odoo", ChatID: fmt.Sprintf("mail.channel_%d", conversation), SenderID: fmt.Sprint(uid), Content: content,
		Peer: bus.Peer{Kind: "direct", ID: fmt.Sprintf("%d:conversation:%d", uid, conversation)}, Metadata: map[string]string{"request_token": "test"}}
}
func TestOdooConcurrentWorkersSeparateHistoriesAndBoundCapacity(t *testing.T) {
	t.Setenv("ODOOCLAW_ODOO_CONCURRENCY", "2")
	p := &concurrentOdooProvider{started: make(chan string, 3), release: make(chan struct{})}
	al, b := testConcurrentLoop(t, p)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stopped := make(chan struct{})
	go func() { defer close(stopped); _ = al.Run(ctx) }()
	for i := 0; i < 3; i++ {
		_ = b.PublishInbound(ctx, odooTestMessage(16+i, 70+i, fmt.Sprintf("PRIVATE_USER_%d", 16+i)))
	}
	for i := 0; i < 2; i++ {
		select {
		case <-p.started:
		case <-ctx.Done():
			t.Fatal("two requests did not start concurrently")
		}
	}
	select {
	case <-p.started:
		t.Fatal("worker limit exceeded")
	case <-time.After(30 * time.Millisecond):
	}
	if p.peak.Load() != 2 {
		t.Fatalf("peak %d", p.peak.Load())
	}
	close(p.release)
	seen := map[string]bool{}
	progressSeen := map[string]bool{}
	for len(seen) < 3 {
		msg, ok := b.SubscribeOutbound(ctx)
		if !ok {
			t.Fatal("missing reply")
		}
		if msg.Progress {
			progressSeen[msg.ChatID] = true
			continue
		}
		seen[msg.ChatID] = true
		var id int
		fmt.Sscanf(msg.ChatID, "mail.channel_%d", &id)
		if msg.Content != fmt.Sprintf("PRIVATE_USER_%d", id-54) {
			t.Fatalf("cross-chat reply %+v", msg)
		}
	}
	if len(seen) != 3 || len(progressSeen) != 3 {
		t.Fatal("wrong response or progress targets")
	}
	for chatID := range progressSeen {
		if !seen[chatID] {
			t.Fatal("progress crossed conversations")
		}
	}
	agent := al.registry.GetDefaultAgent()
	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("agent:main:odoo:direct:%d:conversation:%d", 16+i, 70+i)
		history := agent.Sessions.GetHistory(key)
		if len(history) != 2 {
			t.Fatalf("wrong history %s: %d", key, len(history))
		}
		for _, m := range history {
			if m.Content != fmt.Sprintf("PRIVATE_USER_%d", 16+i) {
				t.Fatal("history contamination")
			}
		}
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("workers failed to stop")
	}
}
func TestOdooSameSessionCannotRunTwoLLMsAtOnce(t *testing.T) {
	p := &concurrentOdooProvider{started: make(chan string, 2), release: make(chan struct{})}
	al, _ := testConcurrentLoop(t, p)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			_, _ = al.processMessage(ctx, odooTestMessage(16, 70, "same-user"))
		}()
	}
	select {
	case <-p.started:
	case <-ctx.Done():
		t.Fatal("first request did not start")
	}
	select {
	case <-p.started:
		t.Fatal("same session processed concurrently")
	case <-time.After(30 * time.Millisecond):
	}
	close(p.release)
	<-done
	<-done
	if p.peak.Load() != 1 {
		t.Fatal("session lock ineffective")
	}
}
func TestOdooCannotChangeSharedModelFromChat(t *testing.T) {
	p := &concurrentOdooProvider{}
	al, _ := testConcurrentLoop(t, p)
	before := al.registry.GetDefaultAgent().Model
	response, handled := al.handleCommand(context.Background(), bus.InboundMessage{Channel: "odoo", Content: "/switch model to another-model"})
	if !handled || !strings.Contains(response, "no se puede cambiar") || al.registry.GetDefaultAgent().Model != before {
		t.Fatal("user changed shared configuration")
	}
}

func TestOdooPromptNeverRecallsSharedPrivateMemory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "memory"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "memory", "MEMORY.md"), []byte("PRIVATE_OTHER_USER_MEMORY_742"), 0600); err != nil {
		t.Fatal(err)
	}
	cb := NewContextBuilder(dir, 10000, 4000)
	messages := cb.BuildMessages(nil, "", "own current request", nil, "odoo", "mail.channel_70", "16", nil)
	for _, msg := range messages {
		if strings.Contains(msg.Content, "PRIVATE_OTHER_USER_MEMORY_742") {
			t.Fatal("shared memory leaked into Odoo prompt")
		}
	}
	if got := cb.memory.GetRelevantContext(PromptMemoryOptions{Channel: "odoo", Query: "PRIVATE_OTHER_USER_MEMORY_742"}); got != "" {
		t.Fatal("auxiliary memories recalled")
	}
	if !strings.Contains(cb.BuildSystemPromptWithCache(), "PRIVATE_OTHER_USER_MEMORY_742") {
		t.Fatal("non-Odoo memory behavior unexpectedly changed")
	}
}
