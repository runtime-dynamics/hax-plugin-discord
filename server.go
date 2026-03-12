package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// --- JSON-RPC 2.0 wire types ---

// JSONRPCRequest represents an incoming JSON-RPC 2.0 request or notification.
type JSONRPCRequest struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"` // nil for notifications
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

// JSONRPCResponse is a JSON-RPC 2.0 response sent back to the client.
type JSONRPCResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Result  any              `json:"result,omitempty"`
	Error   *JSONRPCError    `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// JSONRPCNotification is a server-initiated notification (no id field).
type JSONRPCNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// --- MCP protocol types ---

// InitializeParams carries client info during the initialize handshake.
type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities,omitempty"`
	ClientInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"clientInfo"`
	Config *InitializeConfig `json:"config,omitempty"`
}

// InitializeConfig allows the MCP client (Hyperax) to pass configuration
// overrides during the initialize handshake.
type InitializeConfig struct {
	BotToken        string   `json:"bot_token,omitempty"`
	GuildID         string   `json:"guild_id,omitempty"`
	AllowedChannels []string `json:"allowed_channels,omitempty"`
	LogLevel        string   `json:"log_level,omitempty"`
}

// ToolCallParams is the params object for tools/call.
type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ToolResult is the MCP tool result format.
type ToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent is a single content block in a tool result.
type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ToolDefinition describes a tool for the tools/list response.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// --- Verification ---

// PendingVerification holds state for a user who has DM'd the bot but not yet
// been verified by Hyperax.
type PendingVerification struct {
	AuthKey     string    `json:"auth_key"`
	DMChannelID string    `json:"dm_channel_id"`
	UserName    string    `json:"user_name"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// --- Server ---

// Server is the MCP server that reads JSON-RPC from stdin and writes to stdout.
type Server struct {
	cfg     *Config
	session *discordgo.Session
	logger  *slog.Logger

	tools   map[string]ToolHandlerFunc
	schemas []ToolDefinition

	writeMu sync.Mutex
	encoder *json.Encoder

	verifyMu      sync.RWMutex
	pendingVerify map[string]*PendingVerification // keyed by user ID
	verifiedUsers map[string]string               // user ID → DM channel ID

	// Dynamic config verification fields — set via notifications/configChanged.
	configVerifyUserID  string
	configVerifyAuthKey string
}

// ToolHandlerFunc is the signature for a Discord tool implementation.
type ToolHandlerFunc func(ctx context.Context, args json.RawMessage) (*ToolResult, error)

// NewServer creates an MCP server wired to the Discord session.
func NewServer(cfg *Config, session *discordgo.Session, logger *slog.Logger) *Server {
	s := &Server{
		cfg:           cfg,
		session:       session,
		logger:        logger,
		tools:         make(map[string]ToolHandlerFunc),
		encoder:       json.NewEncoder(os.Stdout),
		pendingVerify: make(map[string]*PendingVerification),
		verifiedUsers: make(map[string]string),
	}
	registerTools(s)
	return s
}

// RegisterTool adds a tool to the server's registry.
func (s *Server) RegisterTool(name, description string, inputSchema json.RawMessage, handler ToolHandlerFunc) {
	s.tools[name] = handler
	s.schemas = append(s.schemas, ToolDefinition{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
	})
}

// Run starts the MCP server loop, reading from stdin until EOF or context cancellation.
func (s *Server) Run(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	s.logger.Info("MCP server listening on stdin")

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("context cancelled, shutting down")
			return nil
		default:
		}

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("stdin read error: %w", err)
			}
			s.logger.Info("stdin closed, shutting down")
			return nil
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		s.handleMessage(ctx, line)
	}
}

// SendNotification writes a server-initiated notification to stdout.
func (s *Server) SendNotification(method string, params any) {
	notif := JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if err := s.encoder.Encode(notif); err != nil {
		s.logger.Error("failed to send notification", "method", method, "error", err)
	}
}

// CreatePendingVerification generates a random auth key for the given user and
// stores it as a pending verification with a 10-minute expiry.
func (s *Server) CreatePendingVerification(userID, dmChannelID, userName string) (string, error) {
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		return "", fmt.Errorf("failed to generate auth key: %w", err)
	}
	authKey := hex.EncodeToString(keyBytes)

	s.verifyMu.Lock()
	defer s.verifyMu.Unlock()

	s.pendingVerify[userID] = &PendingVerification{
		AuthKey:     authKey,
		DMChannelID: dmChannelID,
		UserName:    userName,
		ExpiresAt:   time.Now().Add(10 * time.Minute),
	}

	return authKey, nil
}

// ValidateVerification checks the auth key for a user and, if valid, marks them
// as verified. Returns true on success. Uses constant-time comparison.
func (s *Server) ValidateVerification(userID, authKey string) bool {
	s.verifyMu.Lock()
	defer s.verifyMu.Unlock()

	pending, ok := s.pendingVerify[userID]
	if !ok {
		return false
	}
	if time.Now().After(pending.ExpiresAt) {
		delete(s.pendingVerify, userID)
		return false
	}
	if subtle.ConstantTimeCompare([]byte(pending.AuthKey), []byte(authKey)) != 1 {
		return false
	}

	s.verifiedUsers[userID] = pending.DMChannelID
	delete(s.pendingVerify, userID)
	return true
}

// IsUserVerified returns true if the user has completed DM verification.
func (s *Server) IsUserVerified(userID string) bool {
	s.verifyMu.RLock()
	defer s.verifyMu.RUnlock()
	_, ok := s.verifiedUsers[userID]
	return ok
}

// GetVerifiedDMChannel returns the DM channel ID for a verified user.
func (s *Server) GetVerifiedDMChannel(userID string) (string, bool) {
	s.verifyMu.RLock()
	defer s.verifyMu.RUnlock()
	ch, ok := s.verifiedUsers[userID]
	return ch, ok
}

// GetPendingVerifications returns a snapshot of all non-expired pending verifications.
func (s *Server) GetPendingVerifications() map[string]*PendingVerification {
	s.verifyMu.RLock()
	defer s.verifyMu.RUnlock()

	now := time.Now()
	result := make(map[string]*PendingVerification)
	for uid, pv := range s.pendingVerify {
		if now.Before(pv.ExpiresAt) {
			result[uid] = pv
		}
	}
	return result
}

func (s *Server) handleMessage(ctx context.Context, data []byte) {
	var req JSONRPCRequest
	if err := json.Unmarshal(data, &req); err != nil {
		s.logger.Error("invalid JSON-RPC message", "error", err)
		s.sendError(nil, -32700, "Parse error", nil)
		return
	}

	if req.JSONRPC != "2.0" {
		s.sendError(req.ID, -32600, "Invalid Request: jsonrpc must be 2.0", nil)
		return
	}

	s.logger.Debug("received request", "method", req.Method, "id", req.ID)

	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "initialized", "notifications/initialized":
		s.logger.Info("client initialized")
	case "notifications/configChanged":
		s.handleConfigChanged(req)
	case "tools/list":
		s.handleToolsList(req)
	case "tools/call":
		s.handleToolsCall(ctx, req)
	case "ping":
		s.sendResult(req.ID, map[string]string{})
	default:
		s.sendError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method), nil)
	}
}

func (s *Server) handleInitialize(req JSONRPCRequest) {
	var params InitializeParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			s.logger.Warn("failed to parse initialize params", "error", err)
		}
	}

	if params.Config != nil {
		s.applyConfigOverrides(params.Config)
	}

	s.logger.Info("initialize",
		"client", params.ClientInfo.Name,
		"client_version", params.ClientInfo.Version,
		"protocol_version", params.ProtocolVersion,
	)

	result := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"serverInfo": map[string]any{
			"name":    "hax-plugin-discord",
			"version": version,
		},
	}

	s.sendResult(req.ID, result)
}

func (s *Server) applyConfigOverrides(overrides *InitializeConfig) {
	if overrides.GuildID != "" {
		s.cfg.GuildID = overrides.GuildID
	}
	if overrides.LogLevel != "" {
		s.cfg.LogLevel = overrides.LogLevel
	}
	if len(overrides.AllowedChannels) > 0 {
		s.cfg.AllowedChannels = make(map[string]bool)
		for _, ch := range overrides.AllowedChannels {
			s.cfg.AllowedChannels[ch] = true
		}
	}
}

func (s *Server) handleConfigChanged(req JSONRPCRequest) {
	var params struct {
		Variable string `json:"variable"`
		Value    string `json:"value"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			s.logger.Warn("failed to parse configChanged params", "error", err)
			return
		}
	}

	s.logger.Info("config changed", "variable", params.Variable, "value_len", len(params.Value))

	switch params.Variable {
	case "DISCORD_VERIFY_USER_ID":
		s.configVerifyUserID = params.Value
	case "DISCORD_VERIFY_AUTH_KEY":
		s.configVerifyAuthKey = params.Value
	case "DISCORD_CHANNELS":
		s.cfg.AllowedChannels = make(map[string]bool)
		if params.Value != "" {
			var channels []string
			if err := json.Unmarshal([]byte(params.Value), &channels); err == nil {
				for _, ch := range channels {
					if ch != "" {
						s.cfg.AllowedChannels[ch] = true
					}
				}
			}
		}
		s.logger.Info("allowed channels updated", "count", len(s.cfg.AllowedChannels))
		return
	default:
		s.logger.Debug("ignoring config change for unknown variable", "variable", params.Variable)
		return
	}

	// Attempt verification when both values are present.
	s.tryConfigVerification()
}

// tryConfigVerification checks if both DISCORD_VERIFY_USER_ID and DISCORD_VERIFY_AUTH_KEY
// have been set via config, and if so, attempts to validate the verification.
func (s *Server) tryConfigVerification() {
	userID := s.configVerifyUserID
	authKey := s.configVerifyAuthKey
	if userID == "" || authKey == "" {
		return
	}

	s.logger.Info("attempting DM verification from config", "user_id", userID)

	if !s.ValidateVerification(userID, authKey) {
		s.logger.Warn("config-driven verification failed", "user_id", userID)
		s.SendNotification("notifications/event", map[string]any{
			"type": "discord.dm_verification_failed",
			"data": map[string]any{
				"user_id": userID,
				"reason":  "invalid or expired auth key",
			},
		})
		return
	}

	s.logger.Info("config-driven verification succeeded", "user_id", userID)

	// Send confirmation DM to the verified user.
	if dmCh, ok := s.GetVerifiedDMChannel(userID); ok {
		confirmMsg := "You have been verified successfully. You can now communicate with the bot via DM."
		if _, err := s.session.ChannelMessageSend(dmCh, confirmMsg); err != nil {
			s.logger.Warn("failed to send verification confirmation DM", "user_id", userID, "error", err)
		}
	}

	s.SendNotification("notifications/event", map[string]any{
		"type": "discord.dm_verification_completed",
		"data": map[string]any{
			"user_id": userID,
		},
	})

	// Clear the config values so they don't re-trigger.
	s.configVerifyUserID = ""
	s.configVerifyAuthKey = ""
}

func (s *Server) handleToolsList(req JSONRPCRequest) {
	s.sendResult(req.ID, map[string]any{"tools": s.schemas})
}

func (s *Server) handleToolsCall(ctx context.Context, req JSONRPCRequest) {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.sendError(req.ID, -32602, "Invalid params: "+err.Error(), nil)
		return
	}

	handler, ok := s.tools[params.Name]
	if !ok {
		s.sendError(req.ID, -32602, fmt.Sprintf("Unknown tool: %s", params.Name), nil)
		return
	}

	result, err := handler(ctx, params.Arguments)
	if err != nil {
		s.logger.Error("tool error", "tool", params.Name, "error", err)
		s.sendResult(req.ID, &ToolResult{
			Content: []ToolContent{{Type: "text", Text: fmt.Sprintf("Error: %s", err.Error())}},
			IsError: true,
		})
		return
	}

	s.sendResult(req.ID, result)
}

func (s *Server) sendResult(id *json.RawMessage, result any) {
	resp := JSONRPCResponse{JSONRPC: "2.0", ID: id, Result: result}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.encoder.Encode(resp); err != nil {
		s.logger.Error("failed to write response", "error", err)
	}
}

func (s *Server) sendError(id *json.RawMessage, code int, message string, data any) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &JSONRPCError{Code: code, Message: message, Data: data},
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.encoder.Encode(resp); err != nil {
		s.logger.Error("failed to write error response", "error", err)
	}
}

func jsonStr(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"error":"marshal failed: %s"}`, err.Error())
	}
	return string(b)
}

var _ io.Reader = os.Stdin
