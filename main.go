// Package main implements the Hyperax Discord plugin — a standalone MCP server
// that provides bi-directional Discord communication over stdio.
//
// Protocol: JSON-RPC 2.0 on stdin/stdout. All logging goes to stderr.
// Configuration: environment variables or MCP initialize params.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// Config holds the plugin configuration, populated from environment variables
// and optionally overridden by MCP initialize params.
type Config struct {
	BotToken        string
	GuildID         string
	AllowedChannels map[string]bool
	LogLevel        string
	OwnerID         string
}

// LoadConfigFromEnv reads plugin configuration from environment variables.
//
// Required:
//   - DISCORD_BOT_TOKEN: Discord bot authentication token.
//
// Optional:
//   - DISCORD_GUILD_ID: Default guild for operations that don't specify one.
//   - DISCORD_ALLOWED_CHANNELS: Comma-separated channel IDs to allowlist.
//     Empty means all channels are permitted.
//   - DISCORD_LOG_LEVEL: One of debug, info, warn, error. Defaults to info.
func LoadConfigFromEnv() *Config {
	cfg := &Config{
		BotToken:        os.Getenv("DISCORD_BOT_TOKEN"),
		GuildID:         os.Getenv("DISCORD_GUILD_ID"),
		AllowedChannels: make(map[string]bool),
		LogLevel:        os.Getenv("DISCORD_LOG_LEVEL"),
	}

	cfg.OwnerID = os.Getenv("DISCORD_OWNER_ID")

	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}

	if raw := os.Getenv("DISCORD_ALLOWED_CHANNELS"); raw != "" {
		for _, ch := range strings.Split(raw, ",") {
			ch = strings.TrimSpace(ch)
			if ch != "" {
				cfg.AllowedChannels[ch] = true
			}
		}
	}

	return cfg
}

// IsChannelAllowed returns true if the channel is in the allowlist,
// or if the allowlist is empty (all channels permitted).
func (c *Config) IsChannelAllowed(channelID string) bool {
	if len(c.AllowedChannels) == 0 {
		return true
	}
	return c.AllowedChannels[channelID]
}

func main() {
	cfg := LoadConfigFromEnv()

	// Build logger — MUST write to stderr (stdout is MCP protocol).
	logger := newLogger(cfg.LogLevel)
	logger.Info("hax-plugin-discord starting",
		"version", version,
		"commit", commit,
		"date", date,
	)

	if cfg.BotToken == "" {
		logger.Error("DISCORD_BOT_TOKEN is required")
		os.Exit(1)
	}

	// Connect to Discord.
	session, err := discordgo.New("Bot " + cfg.BotToken)
	if err != nil {
		logger.Error("failed to create Discord session", "error", err)
		os.Exit(1)
	}

	// Request necessary intents for message content and guild members.
	session.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildMessageReactions |
		discordgo.IntentsGuildMembers |
		discordgo.IntentsMessageContent |
		discordgo.IntentsDirectMessages

	if err := session.Open(); err != nil {
		logger.Error("failed to connect to Discord", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := session.Close(); err != nil {
			logger.Error("failed to close Discord session", "error", err)
		}
	}()

	logger.Info("connected to Discord", "user", session.State.User.Username)

	// Build the MCP server with Discord tools.
	server := NewServer(cfg, session, logger)

	// Register Discord event handlers that emit MCP notifications.
	listener := NewEventListener(cfg, session, server, logger)
	listener.Register()

	// If an owner ID is configured, initiate DM verification at startup.
	if cfg.OwnerID != "" {
		if err := server.InitiateOwnerVerification(); err != nil {
			logger.Warn("failed to initiate owner verification at startup", "error", err)
		}
	}

	// Signal-driven shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Run the MCP server — blocks until stdin closes or context is cancelled.
	if err := server.Run(ctx); err != nil {
		logger.Error("server exited with error", "error", err)
		os.Exit(1)
	}

	logger.Info("hax-plugin-discord stopped")
}

// newLogger creates an slog.Logger that writes JSON to stderr.
func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: lvl,
	})
	return slog.New(handler)
}
