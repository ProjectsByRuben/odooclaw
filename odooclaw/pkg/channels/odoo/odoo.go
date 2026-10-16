package odoo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nicolasramos/odooclaw/pkg/providers"
	"sync"

	"github.com/nicolasramos/odooclaw/pkg/bus"
	"github.com/nicolasramos/odooclaw/pkg/channels"
	"github.com/nicolasramos/odooclaw/pkg/config"
	"github.com/nicolasramos/odooclaw/pkg/utils"
)

type OdooChannel struct {
	*channels.BaseChannel
	config               config.OdooConfig
	client               *http.Client
	sendMu               sync.Mutex
	progressLast         sync.Map
	progressActivity     sync.Map
	progressSent         sync.Map
	requestRunning       sync.Map
	pendingTokens        sync.Map // replyChatID -> replyToken (string), single-use
	workspace            string   // agent workspace for MODULES.md (system webhook)
	allowlistResetter    func(ctx context.Context) error
	conversationMu       sync.Mutex
	retiredConversations sync.Map
	conversationDeleter  func(userID, channelID, conversationID int) error
	quotaReader          func(context.Context) providers.ModelQuota
}

func (c *OdooChannel) SetQuotaReader(fn func(context.Context) providers.ModelQuota) {
	c.quotaReader = fn
}

func (c *OdooChannel) MarkRequestRunning(chatID string) {
	c.requestRunning.Store(chatID, true)
	c.progressActivity.Store(chatID, "Analizando tu solicitud")
}

func (c *OdooChannel) SetConversationDeleter(fn func(int, int, int) error) {
	c.conversationDeleter = fn
}

// SetAllowlistCacheResetter wires a callback that invalidates the MCP
// odoo-mcp allowlist cache (Go→MCP mechanism). It is injected by the agent
// loop after MCP initialization; when nil, the TTL-60s cache stays as the
// fallback and the system webhook still answers 200.
func (c *OdooChannel) SetAllowlistCacheResetter(fn func(ctx context.Context) error) {
	c.allowlistResetter = fn
}

// systemWebhookPath returns the system-events sub-route served by this
// channel (e.g. /webhook/odoo/system).
func (c *OdooChannel) systemWebhookPath() string {
	return strings.TrimSuffix(c.WebhookPath(), "/") + "/system"
}

// WebhookExtraPaths implements channels.WebhookExtraPaths so the shared HTTP
// server also mounts the system-events endpoint on the same handler.
func (c *OdooChannel) WebhookExtraPaths() []string {
	return []string{c.systemWebhookPath()}
}

// ChatterMessage represents a single message from the Odoo record chatter,
// sent in the webhook payload as context for the LLM.
type ChatterMessage struct {
	Date   string `json:"date"`
	Author string `json:"author"`
	Body   string `json:"body"`
}

type OdooWebhookPayload struct {
	MessageID         int              `json:"message_id"`
	Model             string           `json:"model"`
	ResID             int              `json:"res_id"`
	ReplyModel        string           `json:"reply_model"`
	ReplyResID        int              `json:"reply_res_id"`
	AuthorID          int              `json:"author_id"`
	AuthorUserID      int              `json:"author_user_id"`
	AuthorName        string           `json:"author_name"`
	SessionInfo       map[string]any   `json:"session_info,omitempty"`
	Body              string           `json:"body"`
	IsDM              bool             `json:"is_dm"`
	ConversationID    int              `json:"conversation_id,omitempty"`
	CompanyID         int              `json:"company_id"`
	AllowedCompanyIDs []int            `json:"allowed_company_ids"`
	ReplyToken        string           `json:"reply_token,omitempty"`
	ChatterContext    []ChatterMessage `json:"chatter_context,omitempty"`
}

type OdooReplyPayload struct {
	TokenUsage *bus.TokenUsage `json:"token_usage,omitempty"`
	Progress   bool            `json:"progress,omitempty"`
	Model      string          `json:"model"`
	ResID      int             `json:"res_id"`
	Message    string          `json:"message"`
	ReplyToken string          `json:"reply_token,omitempty"`
}

func NewOdooChannel(cfg config.OdooConfig, messageBus *bus.MessageBus, workspace string) (*OdooChannel, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	base := channels.NewBaseChannel("odoo", cfg, messageBus, cfg.AllowFrom,
		channels.WithReasoningChannelID(cfg.ReasoningChannelID),
	)

	if workspace == "" {
		workspace = defaultWorkspace()
	}

	ch := &OdooChannel{
		BaseChannel: base,
		config:      cfg,
		client:      &http.Client{Timeout: 10 * time.Second, Jar: jar},
		workspace:   workspace,
	}

	base.SetOwner(ch)
	return ch, nil
}

// defaultWorkspace returns the agent workspace used by the system webhook
// to write MODULES.md when agents.defaults.workspace is empty.
func defaultWorkspace() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".odooclaw", "workspace")
	}
	return filepath.Join(home, ".odooclaw", "workspace")
}

func (c *OdooChannel) Start(ctx context.Context) error {
	c.SetRunning(true)
	slog.Info("Odoo channel started (Webhook Mode)")
	return nil
}

func (c *OdooChannel) Stop(ctx context.Context) error {
	c.SetRunning(false)
	return nil
}

func (c *OdooChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	return c.send(ctx, msg, "")
}

func (c *OdooChannel) send(ctx context.Context, msg bus.OutboundMessage, expectedToken string) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if msg.Progress {
		active, ok := c.pendingTokens.Load(msg.ChatID)
		if !ok || (expectedToken != "" && active != expectedToken) {
			return nil
		}
		previous, _ := c.progressSent.Load(msg.ChatID)
		if msg.Content == "" {
			if activity, ok := c.progressActivity.Load(msg.ChatID); ok {
				msg.Content = activity.(string)
			}
		} else {
			c.progressActivity.Store(msg.ChatID, msg.Content)
		}
		if last, ok := c.progressLast.Load(msg.ChatID); ok && time.Since(last.(time.Time)) < 2*time.Second && previous == msg.Content {
			return nil
		}
	}
	parts := strings.SplitN(msg.ChatID, "_", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid odoo chatID format: %s", msg.ChatID)
	}

	modelName := parts[0]
	resID, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("invalid res_id in chatID: %s", parts[1])
	}

	odooURL := os.Getenv("ODOO_URL")
	if odooURL == "" {
		slog.Warn("ODOO_URL env var not set, cannot send message back to Odoo")
		return nil
	}

	// Select the target database and retain its session cookie before replying.
	// A query parameter alone does not select a database for public Odoo routes.
	targetDB := strings.TrimSpace(c.config.TargetDB)
	if targetDB == "" {
		targetDB = strings.TrimSpace(os.Getenv("ODOO_DB"))
	}
	if targetDB != "" {
		selectionURL := strings.TrimSuffix(odooURL, "/") + "/web/login?db=" + url.QueryEscape(targetDB)
		selectionRequest, err := http.NewRequestWithContext(ctx, "GET", selectionURL, nil)
		if err != nil {
			return err
		}
		selected, err := c.client.Do(selectionRequest)
		if err != nil {
			return fmt.Errorf("select odoo database: %w", err)
		}
		selected.Body.Close()
		if selected.StatusCode != http.StatusOK {
			return fmt.Errorf("select odoo database: status %d", selected.StatusCode)
		}
	}

	reply := OdooReplyPayload{
		Model:      modelName,
		ResID:      resID,
		Message:    utils.RemoveReasoning(msg.Content),
		Progress:   msg.Progress,
		TokenUsage: msg.TokenUsage,
	}

	// Include reply token if one was registered for this chatID (single-use)
	if token, ok := c.pendingTokens.Load(msg.ChatID); ok {
		reply.ReplyToken = token.(string)
	} else {
		// No token — Odoo would reject this reply anyway, skip LLM cost
		slog.Warn("No reply token for chatID, skipping send", "chatID", msg.ChatID)
		return nil
	}

	jsonData, err := json.Marshal(reply)
	if err != nil {
		return err
	}

	endpoint := buildReplyEndpoint(odooURL, c.config.TargetDB, os.Getenv("ODOO_DB"))
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	if token := os.Getenv("ODOOCLAW_REPLY_TOKEN"); token != "" {
		req.Header.Set("X-OdooClaw-Token", token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		slog.Error("Failed to send message to Odoo", "error", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Odoo returned non-200 status", "status", resp.StatusCode)
		return fmt.Errorf("odoo api error: %d", resp.StatusCode)
	}

	if !msg.Progress {
		c.pendingTokens.CompareAndDelete(msg.ChatID, reply.ReplyToken)
		c.progressLast.Delete(msg.ChatID)
		c.progressActivity.Delete(msg.ChatID)
		c.progressSent.Delete(msg.ChatID)
		c.requestRunning.Delete(msg.ChatID)
	}
	if msg.Progress {
		c.progressLast.Store(msg.ChatID, time.Now())
		c.progressSent.Store(msg.ChatID, msg.Content)
	}
	slog.Info("Message sent to Odoo successfully", "chatID", msg.ChatID)
	return nil
}

func buildReplyEndpoint(odooURL, targetDB, fallbackEnvDB string) string {
	endpoint := fmt.Sprintf("%s/odooclaw/reply", strings.TrimSuffix(odooURL, "/"))

	resolvedDB := strings.TrimSpace(targetDB)
	if resolvedDB == "" {
		resolvedDB = strings.TrimSpace(fallbackEnvDB)
	}

	if resolvedDB != "" {
		endpoint = fmt.Sprintf("%s?db=%s", endpoint, resolvedDB)
	}

	return endpoint
}

func (c *OdooChannel) WebhookPath() string {
	if c.config.WebhookPath != "" {
		return c.config.WebhookPath
	}
	return "/webhook/odoo"
}

func (c *OdooChannel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == c.systemWebhookPath() {
		c.serveSystemWebhook(w, r)
		return
	}
	c.conversationMu.Lock()
	defer c.conversationMu.Unlock()

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Verify webhook token if configured
	token := r.Header.Get("X-OdooClaw-Token")
	if c.config.WebhookToken != "" && token != c.config.WebhookToken {
		slog.Warn("Rejected webhook: invalid token", "remote", r.RemoteAddr)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var payload OdooWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		slog.Error("Failed to parse Odoo webhook", "error", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if !payload.IsDM && !c.config.AllowGroupMentions {
		slog.Info("Ignoring Odoo group mention because allow_group_mentions is disabled", "model", payload.Model, "res_id", payload.ResID)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ignored","reason":"group_mentions_disabled"}`))
		return
	}

	sourceChatID := fmt.Sprintf("%s_%d", payload.Model, payload.ResID)
	replyModel := strings.TrimSpace(payload.ReplyModel)
	if replyModel == "" {
		replyModel = payload.Model
	}
	replyResID := payload.ReplyResID
	if replyResID <= 0 {
		replyResID = payload.ResID
	}
	replyChatID := fmt.Sprintf("%s_%d", replyModel, replyResID)
	if _, retired := c.retiredConversations.Load(replyChatID); retired {
		http.Error(w, "Conversation was deleted", http.StatusGone)
		return
	}
	hasPrivateReplyTarget := !payload.IsDM && payload.ReplyModel != "" && payload.ReplyResID > 0 &&
		(payload.ReplyModel != payload.Model || payload.ReplyResID != payload.ResID)

	senderNumericID := payload.AuthorUserID
	if senderNumericID <= 1 {
		http.Error(w, "Authenticated Odoo user is required", http.StatusBadRequest)
		return
	}
	senderID := fmt.Sprintf("%d", senderNumericID)

	sender := bus.SenderInfo{
		Platform:    "odoo",
		PlatformID:  senderID,
		Username:    payload.AuthorName,
		DisplayName: payload.AuthorName,
	}

	peerKind := "group"
	peerID := sourceChatID
	if payload.IsDM || hasPrivateReplyTarget {
		peerKind = "direct"
		peerID = senderID
		// New OdooClaw threads have independent context, bound to both the
		// authenticated sender and the server-side channel ID. Legacy DMs
		// keep their existing session and history.
		if payload.IsDM && payload.ConversationID > 0 && payload.ConversationID == payload.ResID && payload.Model == "mail.channel" {
			peerID = fmt.Sprintf("%s:conversation:%d", senderID, payload.ConversationID)
		}
	}

	peer := bus.Peer{
		Kind: peerKind,
		ID:   peerID,
	}

	content := strings.TrimSpace(payload.Body)

	// Enrich message with record context when coming from a non-channel model
	if payload.Model != "" && payload.Model != "discuss.channel" && payload.Model != "mail.channel" && payload.ResID > 0 {
		content = fmt.Sprintf(
			"[Odoo Context: %s ID=%d]\n%s",
			payload.Model,
			payload.ResID,
			content,
		)
	}

	// Prepend chatter context: messages posted since last OdooClaw reply,
	// in chronological order. This gives the LLM visibility into what
	// happened between its last intervention and the current message.
	if len(payload.ChatterContext) > 0 {
		var sb strings.Builder
		sb.WriteString("[Chatter history since last OdooClaw reply]\n")
		for _, msg := range payload.ChatterContext {
			sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", msg.Date, msg.Author, msg.Body))
		}
		sb.WriteString("[End of chatter history]\n\n")
		content = sb.String() + content
	}

	// Odoo filters mentions server-side before sending to the webhook.
	var mediaPaths []string
	metadata := map[string]string{
		"model":        payload.Model,
		"res_id":       strconv.Itoa(payload.ResID),
		"reply_model":  replyModel,
		"reply_res_id": strconv.Itoa(replyResID),
	}
	metadata["sender_name"] = payload.AuthorName
	if len(payload.SessionInfo) > 0 {
		if b, err := json.Marshal(payload.SessionInfo); err == nil {
			metadata["session_info"] = string(b)
		}
	}
	if payload.CompanyID > 0 {
		metadata["company_id"] = strconv.Itoa(payload.CompanyID)
	}
	if len(payload.AllowedCompanyIDs) > 0 {
		if b, err := json.Marshal(payload.AllowedCompanyIDs); err == nil {
			metadata["allowed_company_ids"] = string(b)
		}
	}

	// Reject webhook if no reply token — Odoo did not generate one
	if payload.ReplyToken == "" {
		slog.Warn("Rejected Odoo webhook: missing reply_token", "model", payload.Model, "res_id", payload.ResID)
		http.Error(w, "Missing reply_token", http.StatusBadRequest)
		return
	}
	if _, busy := c.pendingTokens.LoadOrStore(replyChatID, payload.ReplyToken); busy {
		temporary, err := NewOdooChannel(c.config, nil, c.workspace)
		if err != nil {
			http.Error(w, "Cannot send busy response", 500)
			return
		}
		temporary.pendingTokens.Store(replyChatID, payload.ReplyToken)
		if err := temporary.Send(r.Context(), bus.OutboundMessage{Channel: "odoo", ChatID: replyChatID, Content: "Ya estoy atendiendo una solicitud en esta conversación. Espera a que termine antes de enviar otra."}); err != nil {
			http.Error(w, "Cannot send busy response", 502)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"busy"}`))
		return
	}
	metadata["request_token"] = payload.ReplyToken
	c.progressActivity.Store(replyChatID, "Solicitud recibida · En cola")
	go func(chatID, token string) {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		// Reply capabilities expire after 15 minutes by default. Bound the
		// heartbeat lifetime even if a failed agent never sends its final reply.
		deadline := time.NewTimer(15 * time.Minute)
		defer deadline.Stop()
		for {
			active, ok := c.pendingTokens.Load(chatID)
			if !ok || active != token {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			if err := c.send(ctx, bus.OutboundMessage{Channel: "odoo", ChatID: chatID, Progress: true}, token); err != nil {
				slog.Warn("Could not update Odoo request status", "chatID", chatID, "error", err)
			}
			cancel()
			select {
			case <-ticker.C:
			case <-deadline.C:
				return
			}
		}
	}(replyChatID, payload.ReplyToken)

	c.HandleMessage(r.Context(), peer, strconv.Itoa(payload.MessageID), senderID, replyChatID, content, mediaPaths, metadata, sender)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// OdooSystemWebhookPayload is the JSON body of the system-events webhook
// (POST /webhook/odoo/system). It carries no reply_token — these events do
// not come from a chat.
type OdooSystemWebhookPayload struct {
	Event          string   `json:"event"`
	Keys           []string `json:"keys"`
	Content        string   `json:"content"`
	UserID         int      `json:"user_id,omitempty"`
	ChannelID      int      `json:"channel_id,omitempty"`
	ConversationID int      `json:"conversation_id,omitempty"`
}

// serveSystemWebhook handles Odoo system events: modules_changed and
// config_changed invalidate the MCP allowlist cache; modules_md_rebuild
// writes the provided markdown verbatim to <workspace>/memory/MODULES.md.
func (c *OdooChannel) serveSystemWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Verify webhook token if configured — same behavior as the chat webhook.
	token := r.Header.Get("X-OdooClaw-Token")
	if c.config.WebhookToken != "" && token != c.config.WebhookToken {
		slog.Warn("Rejected Odoo system webhook: invalid token", "remote", r.RemoteAddr)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var payload OdooSystemWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		slog.Error("Failed to parse Odoo system webhook", "error", err)
		writeSystemError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	switch payload.Event {
	case "model_usage":
		if c.config.WebhookToken == "" {
			writeSystemError(w, http.StatusUnauthorized, "shared secret required")
			return
		}
		quota := providers.ModelQuota{Scope: "shared_codex_account"}
		if c.quotaReader != nil {
			quota = c.quotaReader(r.Context())
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(quota)
		return
	case "conversation_delete", "conversation_restore":
		// Destructive events require a configured shared secret, even where
		// ordinary system notifications permit an unconfigured installation.
		if c.config.WebhookToken == "" {
			writeSystemError(w, http.StatusUnauthorized, "shared secret required")
			return
		}
		if payload.UserID <= 1 || payload.ChannelID <= 0 || (payload.ConversationID != 0 && payload.ConversationID != payload.ChannelID) {
			writeSystemError(w, http.StatusBadRequest, "invalid conversation identity")
			return
		}
		c.conversationMu.Lock()
		defer c.conversationMu.Unlock()
		chatID := fmt.Sprintf("mail.channel_%d", payload.ChannelID)
		if payload.Event == "conversation_restore" {
			c.retiredConversations.Delete(chatID)
			break
		}
		if _, busy := c.pendingTokens.Load(chatID); busy {
			writeSystemError(w, http.StatusConflict, "conversation is busy")
			return
		}
		if c.conversationDeleter == nil {
			writeSystemError(w, http.StatusServiceUnavailable, "context deletion unavailable")
			return
		}
		if err := c.conversationDeleter(payload.UserID, payload.ChannelID, payload.ConversationID); err != nil {
			writeSystemError(w, http.StatusInternalServerError, "context deletion failed")
			return
		}
		c.retiredConversations.Store(chatID, true)
	case "modules_changed", "config_changed":
		// config_changed invalidates regardless of keys (the Odoo module only
		// sends it when odooclaw.denied_models changes).
		c.resetAllowlistCache(r.Context())
	case "modules_md_rebuild":
		if strings.TrimSpace(payload.Content) == "" {
			writeSystemError(w, http.StatusBadRequest, "missing content")
			return
		}
		if err := writeModulesMD(c.workspace, payload.Content); err != nil {
			slog.Error("Failed to write MODULES.md", "error", err)
			writeSystemError(w, http.StatusInternalServerError, "failed to write MODULES.md")
			return
		}
	default:
		writeSystemError(w, http.StatusBadRequest, "unknown event")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// resetAllowlistCache invalidates the MCP odoo-mcp allowlist cache via the
// injected resetter. When no resetter is wired (MCP unavailable), the
// TTL-60s cache remains as the degradation fallback and the request still
// succeeds — events are fire-and-forget.
func (c *OdooChannel) resetAllowlistCache(ctx context.Context) {
	if c.allowlistResetter == nil {
		slog.Warn("Odoo system webhook: no allowlist cache resetter wired; relying on 60s TTL")
		return
	}
	resetCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.allowlistResetter(resetCtx); err != nil {
		slog.Warn("Odoo system webhook: allowlist cache reset failed; relying on 60s TTL", "error", err)
	}
}

// writeModulesMD writes content verbatim (no merge, no append, no LLM) to
// <workspace>/memory/MODULES.md, creating the directory as needed.
func writeModulesMD(workspace, content string) error {
	dir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "MODULES.md"), []byte(content), 0o644)
}

// writeSystemError writes a JSON error body.
func writeSystemError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(fmt.Sprintf(`{"error":%q}`, message)))
}
