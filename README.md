# hax-plugin-discord

Official Discord plugin for the [Hyperax](https://github.com/runtime-dynamics/hyperax) platform. It enables Discord as a communication channel, allowing Hyperax agents to send messages, read history, manage threads, and receive real-time Discord events.

The plugin runs as a standalone [MCP](https://modelcontextprotocol.io/) server over stdio, communicating via JSON-RPC 2.0.

## Features

- Send messages with optional rich embeds
- Read channel message history
- List guilds and channels
- Create and manage threads
- React to messages
- Real-time event streaming (messages, reactions, member joins)
- Channel allowlisting for security

## Availability

This plugin is available in the [Hyperax plugin catalogue](https://github.com/runtime-dynamics/hyperax). You can browse and install it directly from the catalogue or use the commands below.

## Installation

### Via Hyperax Dashboard (Recommended)

The easiest way to install this plugin is through the Hyperax dashboard:

1. Open the dashboard at [https://localhost:9000](https://localhost:9000) (default address)
2. Navigate to the **Plugins** section
3. Select **Catalogue**
4. Find the Discord plugin and click **Install**

Once installed, the plugin must be configured before it will work — see the [Configuration](#configuration) section below.

### Via CLI

```bash
hyperax plugin install github.com/hyperax/hax-plugin-discord
```

Or using the MCP tool:

```
install_plugin name=discord source_repo=github.com/hyperax/hax-plugin-discord
```

### Build from Source

```bash
git clone https://github.com/runtime-dynamics/hax-plugin-discord.git
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
| `discord_poll_channels` | Poll monitored channels for new messages |

## Events

The plugin emits MCP notifications for real-time Discord events:

- `discord.message_received` -- New message in an allowed channel
- `discord.reaction_added` -- Reaction added to a message
- `discord.member_joined` -- New member joined a guild

## Development

### Prerequisites

- Go 1.22+
- [golangci-lint](https://golangci-lint.run/welcome/install/)

### Setup

```bash
make setup-hooks   # configure git pre-commit hook for linting
```

### Commands

```bash
make build    # build the binary
make test     # run tests
make lint     # run golangci-lint
```

## Contributing

Contributions are welcome! To get started:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-feature`)
3. Run `make setup-hooks` to enable the pre-commit linting hook
4. Make your changes and ensure `make lint` and `make test` pass
5. Commit your changes and open a pull request

### Bug Reports and Feature Requests

Please report bugs and request features through the [GitHub Issues](https://github.com/runtime-dynamics/hax-plugin-discord/issues) page. When filing a bug report, include:

- The plugin version (`hax-plugin-discord --version` or check the logs)
- Steps to reproduce the issue
- Expected vs actual behaviour
- Relevant log output (set `DISCORD_LOG_LEVEL=debug` for verbose logs)

### Bug Fixes

Bug fix PRs should reference the related issue (e.g. `Fixes #42`). If no issue exists yet, please create one first so the problem can be tracked and discussed before submitting a fix.

## License

Apache-2.0
