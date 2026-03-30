# tilitili

A command-line video player for Bilibili and YouTube. Search, browse, and play videos without leaving the terminal.

## Features

- **Multi-source**: Search Bilibili and YouTube from one CLI
- **PiP playback**: Videos play in a small floating window via [mpv](https://mpv.io/)
- **Audio-only mode**: Listen without video (minimized/no-video)
- **Configurable**: Window size, position, always-on-top via `config` command
- **Prefix history search**: Type a prefix and press Up arrow to search command history
- **Persistent settings**: Source preference and config saved across sessions

## Prerequisites

- [mpv](https://mpv.io/) — video playback (required)
- [yt-dlp](https://github.com/yt-dlp/yt-dlp) — YouTube search and stream resolution (required for YouTube, used by mpv for Bilibili)

Install on macOS:

```bash
brew install mpv yt-dlp
```

## Install

### From source

```bash
go install github.com/lukecold/tilitili@latest
```

### From release

Download the binary for your platform from [Releases](https://github.com/lukecold/tilitili/releases).

### Build locally

```bash
git clone https://github.com/lukecold/tilitili.git
cd tilitili
go build -o tilitili .
```

## Usage

```bash
tilitili        # start the interactive CLI
tilitili start  # same as above
```

### Commands

| Command | Description |
|---|---|
| `search "keywords"` | Search videos (default: 3 results) |
| `search "keywords" -n 5` | Search with custom result count |
| `search "keywords" -o views` | Order by views (descending) |
| `search "keywords" -o time` | Order by upload time (descending) |
| `search "keywords" -u "name"` | Filter by uploader name |
| `search more` | Fetch next batch of results |
| `play <number>` | Play video in a small PiP window |
| `play -a <number>` | Play audio only (no video window) |
| `play -t <number>` | Open video in a new browser tab |
| `stop` | Stop current playback |
| `source` | Show current source |
| `source bilibili` | Switch to Bilibili |
| `source youtube` | Switch to YouTube |
| `config` | Configure settings interactively |
| `help` | Show help |
| `quit` / `exit` | Exit tilitili |

All commands support `-v` for verbose/debug output.

### Examples

```
tilitili> search "golang tutorial" -n 5
tilitili> play 1
tilitili> stop
tilitili> source youtube
tilitili> search "rust programming" -o views
tilitili> play -a 2
tilitili> config
```

### Configuration

Run `config` to interactively adjust:

| Setting | Default | Description |
|---|---|---|
| `video_width` | 25% | PiP window width as percentage of screen |
| `video_position` | bottom-right | Window position (bottom-right/left, top-right/left) |
| `ontop` | true | Keep PiP window always on top |

Settings are saved to `~/.tilitili/config`.

### Keyboard shortcuts

- **Up/Down arrows**: Navigate command history
- **Prefix + Up arrow**: Search history by prefix (type `sea` then press Up to find previous `search` commands)
- **Ctrl+C**: Stop current command, or exit if at prompt

## Source aliases

| Source | Aliases |
|---|---|
| Bilibili | `bilibili`, `bili`, `b` |
| YouTube | `youtube`, `yt`, `y` |

## License

MIT
