# tilitili

A command-line video player for Bilibili and YouTube. Search, browse, and play videos without leaving the terminal.

## Features

- **Multi-source**: Search Bilibili and YouTube from one CLI
- **PiP playback**: Videos play in a small floating window via [mpv](https://mpv.io/)
- **Audio-only mode**: Listen without video
- **Configurable**: Window size, position, always-on-top via `config` command
- **Prefix history search**: Type a prefix and press Up arrow to search command history
- **Persistent settings**: Source preference and config saved across sessions

## Quick Start

### 1. Download tilitili

Click to download for your platform:

| Platform | Download |
|---|---|
| macOS (Apple Silicon) | [tilitili-darwin-arm64](https://github.com/lukecold/tilitili/releases/latest/download/tilitili-darwin-arm64) |
| macOS (Intel) | [tilitili-darwin-amd64](https://github.com/lukecold/tilitili/releases/latest/download/tilitili-darwin-amd64) |
| Linux (x86_64) | [tilitili-linux-amd64](https://github.com/lukecold/tilitili/releases/latest/download/tilitili-linux-amd64) |
| Linux (ARM64) | [tilitili-linux-arm64](https://github.com/lukecold/tilitili/releases/latest/download/tilitili-linux-arm64) |
| Windows | [tilitili-windows-amd64.exe](https://github.com/lukecold/tilitili/releases/latest/download/tilitili-windows-amd64.exe) |

**macOS / Linux** — make it executable and move to PATH:
```bash
chmod +x tilitili-*
sudo mv tilitili-* /usr/local/bin/tilitili
```

**Windows** — rename to `tilitili.exe` and add the folder to your PATH.

### 2. Run it

```bash
tilitili
```

On first run, tilitili will automatically download [mpv](https://mpv.io/) and [yt-dlp](https://github.com/yt-dlp/yt-dlp) if they're not already installed. Everything is stored in `~/.tilitili/bin/`.

Type `help` inside tilitili to see all commands.

## Usage

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
| `source bilibili` | Switch to Bilibili (aliases: `bili`, `b`) |
| `source youtube` | Switch to YouTube (aliases: `yt`, `y`) |
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

---

## Building from source

For developers who want to build from source or contribute:

```bash
# Requires Go 1.22+
git clone https://github.com/lukecold/tilitili.git
cd tilitili
go build -o tilitili .
```

Or install directly:
```bash
go install github.com/lukecold/tilitili@latest
```

### Runtime dependencies

tilitili relies on two external tools for video playback and stream resolution. They are auto-downloaded on first run, but you can also install them manually:

- **[mpv](https://mpv.io/)** — A free, open-source media player. tilitili uses it to play video in a small floating PiP window and for audio-only playback. mpv handles hardware-accelerated decoding, on-screen display, and window management.

- **[yt-dlp](https://github.com/yt-dlp/yt-dlp)** — A command-line tool for extracting streaming URLs from video sites. mpv uses it under the hood (via its `ytdl_hook`) to resolve Bilibili and YouTube URLs into playable streams. tilitili also calls yt-dlp directly for YouTube search.

## License

[MIT](LICENSE)
