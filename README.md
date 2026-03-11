# hax-plugin-discord

A Hyperax plugin that provides bi-directional Discord communication. It runs as a standalone MCP server over stdio, enabling Hyperax agents to send messages, read history, manage threads, and receive real-time Discord events.

## Installation

### Via Hyperax

```bash
hyperax plugin install github.com/hyperax/hax-plugin-discord
```

Or using the MCP tool:

```
install_plugin name=discord source_repo=github.com/hyperax/hax-plugin-discord
```

### Local Binary

Download a pre-built binary from [Releases](https://github.com/hyperax/hax-plugin-discord/releases), or build from source:

```bash
git clone https://github.com/hyperax/hax-plugin-discord.git
cd hax-plugin-discord
make build
```

Place the `hax-plugin-discord` binary on your `PATH` or in Hyperax's plugin directory.

## Configuration

| Variable | Required | Description |
|---|---|---|
| `DISCORD_BOT_TOKEN` | Yes | Discord bot authentication token |
| `DISCORD_GUILD_ID` | No | Default guild ID for operations that don't specify one |
| `DISCORD_ALLOWED_CHANNELS` | No | Comma-separated channel IDs to allowlist (empty = all) |
| `DISCORD_LOG_LEVEL` | No | Log level: `debug`, `info`, `warn`, `error` (default: `info`) |

Configuration can also be passed via the MCP `initialize` handshake params.

## Tools

| Tool | Description |
|---|---|
| `discord_send_message` | Send a message to a channel (supports rich embeds) |
| `discord_read_history` | Read message history from a channel |
| `discord_list_channels` | List channels in a guild |
| `discord_list_guilds` | List all guilds the bot belongs to |
| `discord_get_message` | Get a specific message by ID |
| `discord_react` | Add a reaction to a message |
| `discord_create_thread` | Create a thread, optionally from a message |

## Events

The plugin emits MCP notifications for real-time Discord events:

- `discord.message_received` -- New message in an allowed channel
- `discord.reaction_added` -- Reaction added to a message
- `discord.member_joined` -- New member joined a guild

## License

Apache-2.0
