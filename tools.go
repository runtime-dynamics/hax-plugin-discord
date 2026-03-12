package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/bwmarrin/discordgo"
)

func registerTools(s *Server) {
	s.RegisterTool("discord_send_message", "Send a message to a Discord channel. Supports optional rich embed.",
		json.RawMessage(`{"type":"object","properties":{"channel_id":{"type":"string","description":"Discord channel ID to send the message to"},"content":{"type":"string","description":"Message text content"},"embed":{"type":"object","description":"Optional rich embed object","properties":{"title":{"type":"string"},"description":{"type":"string"},"color":{"type":"integer"},"url":{"type":"string"},"footer":{"type":"object","properties":{"text":{"type":"string"}}},"fields":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"value":{"type":"string"},"inline":{"type":"boolean"}}}}}}},"required":["channel_id","content"]}`),
		s.toolSendMessage)

	s.RegisterTool("discord_read_history", "Read message history from a Discord channel.",
		json.RawMessage(`{"type":"object","properties":{"channel_id":{"type":"string","description":"Discord channel ID"},"limit":{"type":"integer","description":"Number of messages to fetch (1-100, default 25)"},"before_id":{"type":"string","description":"Fetch messages before this message ID"}},"required":["channel_id"]}`),
		s.toolReadHistory)

	s.RegisterTool("discord_list_channels", "List channels in a Discord guild.",
		json.RawMessage(`{"type":"object","properties":{"guild_id":{"type":"string","description":"Guild ID (uses default if omitted)"}}}`),
		s.toolListChannels)

	s.RegisterTool("discord_list_guilds", "List all guilds (servers) the bot is a member of.",
		json.RawMessage(`{"type":"object","properties":{}}`),
		s.toolListGuilds)

	s.RegisterTool("discord_get_message", "Get a specific message by ID from a channel.",
		json.RawMessage(`{"type":"object","properties":{"channel_id":{"type":"string","description":"Discord channel ID"},"message_id":{"type":"string","description":"Message ID to retrieve"}},"required":["channel_id","message_id"]}`),
		s.toolGetMessage)

	s.RegisterTool("discord_react", "Add a reaction emoji to a message.",
		json.RawMessage(`{"type":"object","properties":{"channel_id":{"type":"string","description":"Discord channel ID"},"message_id":{"type":"string","description":"Message ID to react to"},"emoji":{"type":"string","description":"Emoji to react with (Unicode or custom format name:id)"}},"required":["channel_id","message_id","emoji"]}`),
		s.toolReact)

	s.RegisterTool("discord_create_thread", "Create a new thread in a channel, optionally as a reply to a message.",
		json.RawMessage(`{"type":"object","properties":{"channel_id":{"type":"string","description":"Parent channel ID"},"name":{"type":"string","description":"Thread name"},"message_id":{"type":"string","description":"Optional message ID to create thread from"}},"required":["channel_id","name"]}`),
		s.toolCreateThread)

	s.RegisterTool("discord_poll_channels", "Poll monitored channels for new messages (used by cron watcher).",
		json.RawMessage(`{"type":"object","properties":{}}`),
		s.toolPollChannels)

	s.RegisterTool("discord_verify_user", "Validate a user's DM verification auth key and mark them as verified.",
		json.RawMessage(`{"type":"object","properties":{"user_id":{"type":"string","description":"Discord user ID to verify"},"auth_key":{"type":"string","description":"Auth key the user received via DM"}},"required":["user_id","auth_key"]}`),
		s.toolVerifyUser)

	s.RegisterTool("discord_list_pending_verifications", "List all pending DM verifications that have not yet expired.",
		json.RawMessage(`{"type":"object","properties":{}}`),
		s.toolListPendingVerifications)

	s.RegisterTool("discord_send_dm", "Send a direct message to a verified user.",
		json.RawMessage(`{"type":"object","properties":{"user_id":{"type":"string","description":"Discord user ID (must be verified)"},"content":{"type":"string","description":"Message text to send"}},"required":["user_id","content"]}`),
		s.toolSendDM)
}

// --- Tool Implementations ---

type sendMessageArgs struct {
	ChannelID string        `json:"channel_id"`
	Content   string        `json:"content"`
	Embed     *discordEmbed `json:"embed,omitempty"`
}

type discordEmbed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       int          `json:"color,omitempty"`
	URL         string       `json:"url,omitempty"`
	Footer      *embedFooter `json:"footer,omitempty"`
	Fields      []embedField `json:"fields,omitempty"`
}

type embedFooter struct{ Text string `json:"text"` }
type embedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

func (s *Server) toolSendMessage(_ context.Context, raw json.RawMessage) (*ToolResult, error) {
	var args sendMessageArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.ChannelID == "" {
		return nil, fmt.Errorf("channel_id is required")
	}
	if args.Content == "" {
		return nil, fmt.Errorf("content is required")
	}
	if !s.cfg.IsChannelAllowed(args.ChannelID) {
		return nil, fmt.Errorf("channel %s is not in the allowed channels list", args.ChannelID)
	}

	msgSend := &discordgo.MessageSend{Content: args.Content}
	if args.Embed != nil {
		embed := &discordgo.MessageEmbed{
			Title: args.Embed.Title, Description: args.Embed.Description,
			Color: args.Embed.Color, URL: args.Embed.URL,
		}
		if args.Embed.Footer != nil {
			embed.Footer = &discordgo.MessageEmbedFooter{Text: args.Embed.Footer.Text}
		}
		for _, f := range args.Embed.Fields {
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: f.Name, Value: f.Value, Inline: f.Inline})
		}
		msgSend.Embeds = []*discordgo.MessageEmbed{embed}
	}

	msg, err := s.session.ChannelMessageSendComplex(args.ChannelID, msgSend)
	if err != nil {
		return nil, fmt.Errorf("discord API error: %w", err)
	}

	return &ToolResult{
		Content: []ToolContent{{Type: "text", Text: jsonStr(map[string]string{"message_id": msg.ID, "timestamp": formatTimestamp(msg.Timestamp)})}},
	}, nil
}

type readHistoryArgs struct {
	ChannelID string `json:"channel_id"`
	Limit     int    `json:"limit,omitempty"`
	BeforeID  string `json:"before_id,omitempty"`
}

func (s *Server) toolReadHistory(_ context.Context, raw json.RawMessage) (*ToolResult, error) {
	var args readHistoryArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.ChannelID == "" {
		return nil, fmt.Errorf("channel_id is required")
	}
	if !s.cfg.IsChannelAllowed(args.ChannelID) {
		return nil, fmt.Errorf("channel %s is not in the allowed channels list", args.ChannelID)
	}

	limit := args.Limit
	if limit <= 0 { limit = 25 }
	if limit > 100 { limit = 100 }

	messages, err := s.session.ChannelMessages(args.ChannelID, limit, args.BeforeID, "", "")
	if err != nil {
		return nil, fmt.Errorf("discord API error: %w", err)
	}

	type messageInfo struct {
		ID string `json:"id"`; Author string `json:"author"`; AuthorID string `json:"author_id"`
		Content string `json:"content"`; Timestamp string `json:"timestamp"`
	}
	var results []messageInfo
	for _, m := range messages {
		authorName, authorID := "unknown", ""
		if m.Author != nil { authorName = m.Author.Username; authorID = m.Author.ID }
		results = append(results, messageInfo{ID: m.ID, Author: authorName, AuthorID: authorID, Content: m.Content, Timestamp: formatTimestamp(m.Timestamp)})
	}

	return &ToolResult{Content: []ToolContent{{Type: "text", Text: jsonStr(results)}}}, nil
}

type listChannelsArgs struct{ GuildID string `json:"guild_id,omitempty"` }

func (s *Server) toolListChannels(_ context.Context, raw json.RawMessage) (*ToolResult, error) {
	var args listChannelsArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	guildID := args.GuildID
	if guildID == "" { guildID = s.cfg.GuildID }
	if guildID == "" { return nil, fmt.Errorf("guild_id is required (not configured as default)") }

	channels, err := s.session.GuildChannels(guildID)
	if err != nil {
		return nil, fmt.Errorf("discord API error: %w", err)
	}

	type channelInfo struct {
		ID string `json:"id"`; Name string `json:"name"`; Type string `json:"type"`
		Topic string `json:"topic,omitempty"`; Position int `json:"position"`
	}
	var results []channelInfo
	for _, ch := range channels {
		results = append(results, channelInfo{ID: ch.ID, Name: ch.Name, Type: channelTypeName(ch.Type), Topic: ch.Topic, Position: ch.Position})
	}
	return &ToolResult{Content: []ToolContent{{Type: "text", Text: jsonStr(results)}}}, nil
}

func (s *Server) toolListGuilds(_ context.Context, _ json.RawMessage) (*ToolResult, error) {
	type guildInfo struct { ID string `json:"id"`; Name string `json:"name"`; MemberCount int `json:"member_count"` }
	var results []guildInfo
	for _, g := range s.session.State.Guilds {
		results = append(results, guildInfo{ID: g.ID, Name: g.Name, MemberCount: g.MemberCount})
	}
	return &ToolResult{Content: []ToolContent{{Type: "text", Text: jsonStr(results)}}}, nil
}

type getMessageArgs struct { ChannelID string `json:"channel_id"`; MessageID string `json:"message_id"` }

func (s *Server) toolGetMessage(_ context.Context, raw json.RawMessage) (*ToolResult, error) {
	var args getMessageArgs
	if err := json.Unmarshal(raw, &args); err != nil { return nil, fmt.Errorf("invalid arguments: %w", err) }
	if args.ChannelID == "" { return nil, fmt.Errorf("channel_id is required") }
	if args.MessageID == "" { return nil, fmt.Errorf("message_id is required") }
	if !s.cfg.IsChannelAllowed(args.ChannelID) { return nil, fmt.Errorf("channel %s is not in the allowed channels list", args.ChannelID) }

	msg, err := s.session.ChannelMessage(args.ChannelID, args.MessageID)
	if err != nil { return nil, fmt.Errorf("discord API error: %w", err) }

	type messageDetail struct {
		ID string `json:"id"`; ChannelID string `json:"channel_id"`; GuildID string `json:"guild_id"`
		Author string `json:"author"`; AuthorID string `json:"author_id"`; Content string `json:"content"`
		Timestamp string `json:"timestamp"`; Pinned bool `json:"pinned"`; Attachments []string `json:"attachments,omitempty"`
	}
	authorName, authorID := "unknown", ""
	if msg.Author != nil { authorName = msg.Author.Username; authorID = msg.Author.ID }
	var attachments []string
	for _, a := range msg.Attachments { attachments = append(attachments, a.URL) }

	return &ToolResult{Content: []ToolContent{{Type: "text", Text: jsonStr(messageDetail{
		ID: msg.ID, ChannelID: msg.ChannelID, GuildID: msg.GuildID,
		Author: authorName, AuthorID: authorID, Content: msg.Content,
		Timestamp: formatTimestamp(msg.Timestamp), Pinned: msg.Pinned, Attachments: attachments,
	})}}}, nil
}

type reactArgs struct { ChannelID string `json:"channel_id"`; MessageID string `json:"message_id"`; Emoji string `json:"emoji"` }

func (s *Server) toolReact(_ context.Context, raw json.RawMessage) (*ToolResult, error) {
	var args reactArgs
	if err := json.Unmarshal(raw, &args); err != nil { return nil, fmt.Errorf("invalid arguments: %w", err) }
	if args.ChannelID == "" { return nil, fmt.Errorf("channel_id is required") }
	if args.MessageID == "" { return nil, fmt.Errorf("message_id is required") }
	if args.Emoji == "" { return nil, fmt.Errorf("emoji is required") }
	if !s.cfg.IsChannelAllowed(args.ChannelID) { return nil, fmt.Errorf("channel %s is not in the allowed channels list", args.ChannelID) }

	if err := s.session.MessageReactionAdd(args.ChannelID, args.MessageID, args.Emoji); err != nil {
		return nil, fmt.Errorf("discord API error: %w", err)
	}
	return &ToolResult{Content: []ToolContent{{Type: "text", Text: `{"status":"ok"}`}}}, nil
}

type createThreadArgs struct { ChannelID string `json:"channel_id"`; Name string `json:"name"`; MessageID string `json:"message_id,omitempty"` }

func (s *Server) toolCreateThread(_ context.Context, raw json.RawMessage) (*ToolResult, error) {
	var args createThreadArgs
	if err := json.Unmarshal(raw, &args); err != nil { return nil, fmt.Errorf("invalid arguments: %w", err) }
	if args.ChannelID == "" { return nil, fmt.Errorf("channel_id is required") }
	if args.Name == "" { return nil, fmt.Errorf("name is required") }
	if !s.cfg.IsChannelAllowed(args.ChannelID) { return nil, fmt.Errorf("channel %s is not in the allowed channels list", args.ChannelID) }

	var thread *discordgo.Channel
	var err error
	if args.MessageID != "" {
		thread, err = s.session.MessageThreadStartComplex(args.ChannelID, args.MessageID, &discordgo.ThreadStart{Name: args.Name, AutoArchiveDuration: 1440, Type: discordgo.ChannelTypeGuildPublicThread})
	} else {
		thread, err = s.session.ThreadStartComplex(args.ChannelID, &discordgo.ThreadStart{Name: args.Name, AutoArchiveDuration: 1440, Type: discordgo.ChannelTypeGuildPublicThread})
	}
	if err != nil { return nil, fmt.Errorf("discord API error: %w", err) }

	return &ToolResult{Content: []ToolContent{{Type: "text", Text: jsonStr(map[string]string{"thread_id": thread.ID, "name": thread.Name})}}}, nil
}

func (s *Server) toolPollChannels(_ context.Context, _ json.RawMessage) (*ToolResult, error) {
	if len(s.cfg.AllowedChannels) == 0 {
		return &ToolResult{Content: []ToolContent{{Type: "text", Text: `{"status":"no_channels","message":"No monitored channels configured"}`}}}, nil
	}
	polled := 0
	for chID := range s.cfg.AllowedChannels {
		messages, err := s.session.ChannelMessages(chID, 5, "", "", "")
		if err != nil { s.logger.Warn("poll failed for channel", "channel_id", chID, "error", err); continue }
		for _, m := range messages {
			if m.Author != nil && m.Author.ID == s.session.State.User.ID { continue }
			if time.Since(m.Timestamp) < 60*time.Second {
				authorName, authorID := "unknown", ""
				if m.Author != nil { authorName = m.Author.Username; authorID = m.Author.ID }
				s.SendNotification("notifications/event", map[string]any{
					"type": "discord.message_received",
					"data": map[string]any{
						"message_id": m.ID, "channel_id": m.ChannelID, "guild_id": m.GuildID,
						"author": authorName, "author_id": authorID, "content": m.Content,
						"timestamp": formatTimestamp(m.Timestamp), "source": "poll",
					},
				})
			}
		}
		polled++
	}
	return &ToolResult{Content: []ToolContent{{Type: "text", Text: fmt.Sprintf(`{"status":"ok","channels_polled":%d}`, polled)}}}, nil
}

// --- DM Verification Tool Implementations ---

type verifyUserArgs struct {
	UserID  string `json:"user_id"`
	AuthKey string `json:"auth_key"`
}

func (s *Server) toolVerifyUser(_ context.Context, raw json.RawMessage) (*ToolResult, error) {
	var args verifyUserArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.UserID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	if args.AuthKey == "" {
		return nil, fmt.Errorf("auth_key is required")
	}

	if !s.ValidateVerification(args.UserID, args.AuthKey) {
		return &ToolResult{
			Content: []ToolContent{{Type: "text", Text: `{"verified":false,"reason":"invalid or expired auth key"}`}},
			IsError: true,
		}, nil
	}

	// Send confirmation DM to the user.
	if dmCh, ok := s.GetVerifiedDMChannel(args.UserID); ok {
		confirmMsg := "You have been verified successfully. You can now communicate with the bot via DM."
		if _, err := s.session.ChannelMessageSend(dmCh, confirmMsg); err != nil {
			s.logger.Warn("failed to send verification confirmation DM", "user_id", args.UserID, "error", err)
		}
	}

	return &ToolResult{
		Content: []ToolContent{{Type: "text", Text: `{"verified":true}`}},
	}, nil
}

func (s *Server) toolListPendingVerifications(_ context.Context, _ json.RawMessage) (*ToolResult, error) {
	pending := s.GetPendingVerifications()

	type pendingInfo struct {
		UserID      string `json:"user_id"`
		UserName    string `json:"user_name"`
		DMChannelID string `json:"dm_channel_id"`
		ExpiresAt   string `json:"expires_at"`
	}

	var results []pendingInfo
	for uid, pv := range pending {
		results = append(results, pendingInfo{
			UserID:      uid,
			UserName:    pv.UserName,
			DMChannelID: pv.DMChannelID,
			ExpiresAt:   pv.ExpiresAt.Format(time.RFC3339),
		})
	}

	return &ToolResult{Content: []ToolContent{{Type: "text", Text: jsonStr(results)}}}, nil
}

type sendDMArgs struct {
	UserID  string `json:"user_id"`
	Content string `json:"content"`
}

func (s *Server) toolSendDM(_ context.Context, raw json.RawMessage) (*ToolResult, error) {
	var args sendDMArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.UserID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	if args.Content == "" {
		return nil, fmt.Errorf("content is required")
	}

	dmCh, ok := s.GetVerifiedDMChannel(args.UserID)
	if !ok {
		return &ToolResult{
			Content: []ToolContent{{Type: "text", Text: `{"sent":false,"reason":"user is not verified"}`}},
			IsError: true,
		}, nil
	}

	msg, err := s.session.ChannelMessageSend(dmCh, args.Content)
	if err != nil {
		return nil, fmt.Errorf("discord API error: %w", err)
	}

	return &ToolResult{
		Content: []ToolContent{{Type: "text", Text: jsonStr(map[string]string{"sent": "true", "message_id": msg.ID})}},
	}, nil
}

func channelTypeName(ct discordgo.ChannelType) string {
	switch ct {
	case discordgo.ChannelTypeGuildText: return "text"
	case discordgo.ChannelTypeGuildVoice: return "voice"
	case discordgo.ChannelTypeGuildCategory: return "category"
	case discordgo.ChannelTypeGuildNews: return "news"
	case discordgo.ChannelTypeGuildForum: return "forum"
	case discordgo.ChannelTypeGuildStageVoice: return "stage"
	default: return strconv.Itoa(int(ct))
	}
}

func formatTimestamp(t time.Time) string {
	if t.IsZero() { return "" }
	return t.Format(time.RFC3339)
}
