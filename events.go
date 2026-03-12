package main

import (
	"log/slog"

	"github.com/bwmarrin/discordgo"
)

// EventListener registers Discord event handlers that emit MCP notifications
// to stdout. Events are only emitted for channels in the allowlist.
type EventListener struct {
	cfg     *Config
	session *discordgo.Session
	server  *Server
	logger  *slog.Logger
}

// NewEventListener creates an event listener bound to the Discord session and MCP server.
func NewEventListener(cfg *Config, session *discordgo.Session, server *Server, logger *slog.Logger) *EventListener {
	return &EventListener{
		cfg:     cfg,
		session: session,
		server:  server,
		logger:  logger,
	}
}

// Register adds all Discord event handlers to the session.
func (el *EventListener) Register() {
	el.session.AddHandler(el.onMessageCreate)
	el.session.AddHandler(el.onMessageReactionAdd)
	el.session.AddHandler(el.onGuildMemberAdd)
	el.logger.Info("Discord event listeners registered")
}

func (el *EventListener) onMessageCreate(_ *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author != nil && m.Author.ID == el.session.State.User.ID {
		return
	}

	// DMs have an empty GuildID — route to DM handler.
	if m.GuildID == "" {
		el.handleDMMessage(m)
		return
	}

	if !el.cfg.IsChannelAllowed(m.ChannelID) {
		return
	}

	authorName := "unknown"
	authorID := ""
	if m.Author != nil {
		authorName = m.Author.Username
		authorID = m.Author.ID
	}

	el.server.SendNotification("notifications/event", map[string]any{
		"type": "discord.message_received",
		"data": map[string]any{
			"message_id": m.ID,
			"channel_id": m.ChannelID,
			"guild_id":   m.GuildID,
			"author":     authorName,
			"author_id":  authorID,
			"content":    m.Content,
			"timestamp":  formatTimestamp(m.Timestamp),
		},
	})
}

// handleDMMessage processes a direct message. If the user is already verified,
// it emits a dm_message_received notification. Otherwise it generates an auth
// key and sends a challenge response.
func (el *EventListener) handleDMMessage(m *discordgo.MessageCreate) {
	userID := ""
	userName := "unknown"
	if m.Author != nil {
		userID = m.Author.ID
		userName = m.Author.Username
	}

	// Already-verified user — forward the DM content.
	if el.server.IsUserVerified(userID) {
		el.server.SendNotification("notifications/event", map[string]any{
			"type": "discord.dm_message_received",
			"data": map[string]any{
				"message_id": m.ID,
				"channel_id": m.ChannelID,
				"user_id":    userID,
				"user_name":  userName,
				"content":    m.Content,
				"timestamp":  formatTimestamp(m.Timestamp),
			},
		})
		return
	}

	// Unverified user — generate auth key and send challenge.
	authKey, err := el.server.CreatePendingVerification(userID, m.ChannelID, userName)
	if err != nil {
		el.logger.Error("failed to create pending verification", "user_id", userID, "error", err)
		return
	}

	challengeMsg := "**Verification Required**\n\n" +
		"Your auth key: `" + authKey + "`\n\n" +
		"Please provide this key to the system to complete verification. " +
		"This key expires in 10 minutes."

	if _, err := el.session.ChannelMessageSend(m.ChannelID, challengeMsg); err != nil {
		el.logger.Error("failed to send DM challenge", "user_id", userID, "error", err)
		return
	}

	el.logger.Info("DM verification challenge sent", "user_id", userID, "user_name", userName)

	el.server.SendNotification("notifications/event", map[string]any{
		"type": "discord.dm_verification_requested",
		"data": map[string]any{
			"user_id":    userID,
			"user_name":  userName,
			"auth_key":   authKey,
			"channel_id": m.ChannelID,
		},
	})
}

func (el *EventListener) onMessageReactionAdd(_ *discordgo.Session, r *discordgo.MessageReactionAdd) {
	if !el.cfg.IsChannelAllowed(r.ChannelID) {
		return
	}

	emoji := r.Emoji.Name
	if r.Emoji.ID != "" {
		emoji = r.Emoji.Name + ":" + r.Emoji.ID
	}

	el.server.SendNotification("notifications/event", map[string]any{
		"type": "discord.reaction_added",
		"data": map[string]any{
			"message_id": r.MessageID,
			"channel_id": r.ChannelID,
			"guild_id":   r.GuildID,
			"user_id":    r.UserID,
			"emoji":      emoji,
		},
	})
}

func (el *EventListener) onGuildMemberAdd(_ *discordgo.Session, m *discordgo.GuildMemberAdd) {
	userName := "unknown"
	userID := ""
	if m.User != nil {
		userName = m.User.Username
		userID = m.User.ID
	}

	el.server.SendNotification("notifications/event", map[string]any{
		"type": "discord.member_joined",
		"data": map[string]any{
			"guild_id": m.GuildID,
			"user":     userName,
			"user_id":  userID,
		},
	})
}
