package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"github.com/chzyer/readline"

	"tilitili/config"
	"tilitili/player"
	"tilitili/source"
)

const banner = `
  _   _ _ _ _   _ _ _
 | |_(_) (_) |_(_) (_)
 | __| | | | __| | | |
 | |_| | | | |_| | | |
  \__|_|_|_|\__|_|_|_|

  Bilibili CLI Player
  Type 'help' for commands, 'quit' to exit.
`

const helpText = `
Commands:
  search "keywords"              Search videos (default: 3 results)
  search "keywords" -n 5         Search with custom result count
  search "keywords" -o views     Order by: views, time (both descending)
  search "keywords" -u "upname"  Filter by uploader name
  search more                    Fetch next batch of results
  play <number>                  Play video in a new browser window
  play -t <number>               Play video in a new browser tab
  play -a <number>               Play audio only (minimized browser)
  source                         Show current source and available sources
  source <name>                  Switch source (bilibili, youtube)
  config                         Configure settings (video size, position, etc.)
  help                           Show this help
  quit / exit                    Exit tilitili

All commands support -v for verbose/debug output.
`

// prefixHistorySearch implements readline.Listener to provide prefix-based
// history search: type a prefix then press up/down arrow to cycle through
// matching history entries.
//
// Key insight: readline processes up/down arrows BEFORE calling the Listener,
// replacing the line with the previous/next history entry. So we must track
// what the user typed on every regular keypress, and use that saved prefix
// when an arrow key arrives.
type prefixHistorySearch struct {
	history   []string // full history list
	typedText string   // what the user has typed (tracked on every non-arrow key)
	prefix    string   // the prefix being searched (captured from typedText on first arrow)
	index     int      // current position in filtered matches
	matches   []string // history entries matching the prefix
	active    bool     // whether a prefix search is in progress
}

func newPrefixHistorySearch() *prefixHistorySearch {
	return &prefixHistorySearch{index: -1}
}

// AppendHistory adds a new entry to the history.
func (p *prefixHistorySearch) AppendHistory(entry string) {
	p.history = append(p.history, entry)
}

func (p *prefixHistorySearch) OnChange(line []rune, pos int, key rune) ([]rune, int, bool) {
	switch key {
	case 0: // init call
		p.typedText = ""
		return nil, 0, false
	case readline.CharPrev: // up arrow
		return p.searchUp()
	case readline.CharNext: // down arrow
		return p.searchDown()
	default:
		// Track what the user is typing and reset search state
		p.typedText = string(line)
		p.reset()
		return nil, 0, false
	}
}

func (p *prefixHistorySearch) searchUp() ([]rune, int, bool) {
	if !p.active {
		// Start a new prefix search using the saved typed text
		p.prefix = p.typedText
		p.active = true
		// Build matches: history entries starting with prefix, newest first, deduplicated
		p.matches = nil
		seen := map[string]bool{}
		for i := len(p.history) - 1; i >= 0; i-- {
			entry := p.history[i]
			if strings.HasPrefix(entry, p.prefix) && !seen[entry] {
				seen[entry] = true
				p.matches = append(p.matches, entry)
			}
		}
		p.index = -1
	}

	if len(p.matches) == 0 {
		return nil, 0, false
	}

	p.index++
	if p.index >= len(p.matches) {
		p.index = len(p.matches) - 1
		return nil, 0, false
	}

	match := []rune(p.matches[p.index])
	return match, len(match), true
}

func (p *prefixHistorySearch) searchDown() ([]rune, int, bool) {
	if !p.active || len(p.matches) == 0 {
		return nil, 0, false
	}

	p.index--
	if p.index < 0 {
		// Back to the original typed prefix
		p.index = -1
		prefix := []rune(p.prefix)
		return prefix, len(prefix), true
	}

	match := []rune(p.matches[p.index])
	return match, len(match), true
}

func (p *prefixHistorySearch) reset() {
	p.active = false
	p.prefix = ""
	p.index = -1
	p.matches = nil
}

// extractVerbose strips -v/--verbose from the line, returns cleaned line and whether -v was present.
func extractVerbose(line string) (string, bool) {
	tokens := tokenize(line)
	verbose := false
	var cleaned []string
	for _, t := range tokens {
		if t == "-v" || t == "--verbose" {
			verbose = true
		} else {
			cleaned = append(cleaned, t)
		}
	}
	if !verbose {
		return line, false
	}
	// Rebuild the line, re-quoting tokens that contain spaces
	var parts []string
	for _, t := range cleaned {
		if strings.ContainsAny(t, " \t") {
			parts = append(parts, `"`+t+`"`)
		} else {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " "), true
}

type searchOpts struct {
	keyword  string
	count    int
	order    source.SearchOrder
	uploader string
	isMore   bool
}

// tokenize splits a command line respecting quoted strings.
func tokenize(line string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := byte(0)

	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case inQuote != 0:
			if ch == inQuote {
				inQuote = 0
			} else {
				current.WriteByte(ch)
			}
		case ch == '"' || ch == '\'':
			inQuote = ch
		case ch == ' ' || ch == '\t':
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func parseSearch(line string, src source.Source) (opts searchOpts, ok bool) {
	tokens := tokenize(line)
	if len(tokens) < 2 {
		return opts, false
	}

	// "search more"
	if len(tokens) == 2 && strings.EqualFold(tokens[1], "more") {
		return searchOpts{isMore: true}, true
	}

	opts.count = 3

	i := 1
	for i < len(tokens) {
		t := tokens[i]
		switch {
		case t == "-n" && i+1 < len(tokens):
			if n, err := strconv.Atoi(tokens[i+1]); err == nil {
				opts.count = n
			}
			i += 2
		case t == "-o" && i+1 < len(tokens):
			order, valid := src.ParseOrder(tokens[i+1])
			if !valid {
				fmt.Printf("Unknown order: %s (options: views, time, newest, danmaku, favorites)\n", tokens[i+1])
				return opts, false
			}
			opts.order = order
			i += 2
		case t == "-u" && i+1 < len(tokens):
			opts.uploader = tokens[i+1]
			i += 2
		default:
			if opts.keyword == "" {
				opts.keyword = t
			}
			i++
		}
	}

	// If -u is given without a keyword, use the uploader name as the keyword
	if opts.keyword == "" && opts.uploader != "" {
		opts.keyword = opts.uploader
	}
	if opts.keyword == "" {
		return opts, false
	}
	return opts, true
}

type playOpts struct {
	number    int
	audioOnly bool
	newTab    bool
}

func parsePlay(line string) (opts playOpts, ok bool) {
	parts := strings.Fields(strings.TrimSpace(line))
	if len(parts) == 0 || !strings.EqualFold(parts[0], "play") {
		return opts, false
	}

	for _, p := range parts[1:] {
		switch p {
		case "-a", "--audio-only":
			opts.audioOnly = true
		case "-t", "--tab":
			opts.newTab = true
		default:
			if n, err := strconv.Atoi(p); err == nil {
				opts.number = n
				ok = true
			}
		}
	}
	return opts, ok
}

func main() {
	// Allow "bilibili [start]"
	for _, arg := range os.Args[1:] {
		if arg != "start" {
			fmt.Println("Usage: bilibili [start]")
			os.Exit(1)
		}
	}

	fmt.Print(banner)

	cfg := config.Load()
	var src source.Source
	switch strings.ToLower(cfg.Source) {
	case "youtube", "yt":
		src = source.NewYouTube()
	default:
		src = source.NewBilibili()
	}
	p := player.New(cfg)

	// Command cancellation: during command execution, Ctrl+C cancels the command.
	// At the prompt, first Ctrl+C prints a hint, second Ctrl+C (within 2s) exits.
	var (
		cmdCancel context.CancelFunc
		cmdMu     sync.Mutex
	)

	// Intercept SIGINT for command cancellation
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for range sigCh {
			cmdMu.Lock()
			cancel := cmdCancel
			cmdMu.Unlock()
			if cancel != nil {
				cancel()
			}
		}
	}()

	// Helper: run a function with a cancellable context.
	// First Ctrl+C during execution cancels the context.
	runCmd := func(fn func(ctx context.Context)) {
		ctx, cancel := context.WithCancel(context.Background())
		cmdMu.Lock()
		cmdCancel = cancel
		cmdMu.Unlock()

		fn(ctx)

		cmdMu.Lock()
		cmdCancel = nil
		cmdMu.Unlock()
		cancel()
	}

	prefixSearch := newPrefixHistorySearch()
	historyPath := os.ExpandEnv("$HOME/.tilitili_history")
	rl, err := readline.NewEx(&readline.Config{
		Prompt:          "tilitili> ",
		HistoryFile:     historyPath,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
		Listener:        prefixSearch,
	})
	if err != nil {
		fmt.Println("Failed to init readline:", err)
		os.Exit(1)
	}
	defer rl.Close()

	// Load existing history into prefix searcher
	if data, err := os.ReadFile(historyPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if line != "" {
				prefixSearch.AppendHistory(line)
			}
		}
	}

	for {
		rawLine, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt || err == io.EOF {
				p.Cleanup()
				fmt.Println("\nBye!")
				return
			}
			continue
		}
		rawLine = strings.TrimSpace(rawLine)
		if rawLine == "" {
			continue
		}

		// Track for prefix history search
		prefixSearch.AppendHistory(rawLine)

		// Extract -v from the command line, set verbose for this command only
		line, verbose := extractVerbose(rawLine)
		src.SetVerbose(verbose)

		cmd := strings.Fields(line)[0]
		switch strings.ToLower(cmd) {
		case "quit", "exit":
			p.Cleanup()
			fmt.Println("Bye!")
			return

		case "search":
			opts, valid := parseSearch(line, src)
			if !valid {
				fmt.Println(`Usage: search "keywords" [-n count] [-o order] [-u "uploader"]`)
				continue
			}
			runCmd(func(ctx context.Context) {
				if opts.isMore {
					results, err := src.SearchMore(ctx)
					if err != nil {
						fmt.Println(err)
						return
					}
					fmt.Print(source.FormatResults(results))
				} else {
					fmt.Printf("Searching %s for \"%s\"...\n", src.Name(), opts.keyword)
					results, err := src.Search(ctx, opts.keyword, opts.count, opts.order, opts.uploader)
					if err != nil {
						fmt.Println("Search error:", err)
						return
					}
					fmt.Print(source.FormatResults(results))
				}
			})

		case "play":
			popts, ok := parsePlay(line)
			if !ok {
				fmt.Println("Usage: play <number> [-a|--audio-only] [-t|--tab]")
				continue
			}
			video := src.GetVideo(popts.number)
			if video == nil {
				fmt.Printf("No video at index %d. Search first.\n", popts.number)
				continue
			}
			fmt.Println(p.Play(video.URL, video.Title, popts.audioOnly, popts.newTab))

		case "source":
			args := strings.Fields(line)
			if len(args) == 1 {
				fmt.Printf("  Current source: %s\n", src.Name())
				fmt.Printf("  Available: %s\n", strings.Join(source.AvailableSources(), ", "))
				continue
			}
			name := strings.ToLower(args[1])
			switch name {
			case "bilibili", "bili", "b":
				src = source.NewBilibili()
				cfg.Source = "bilibili"
				cfg.Save()
				fmt.Println("Switched to Bilibili.")
			case "youtube", "yt", "y":
				src = source.NewYouTube()
				cfg.Source = "youtube"
				cfg.Save()
				fmt.Println("Switched to YouTube.")
			default:
				fmt.Printf("Unknown source: %s. Available: %s\n", args[1], strings.Join(source.AvailableSources(), ", "))
			}

		case "stop":
			fmt.Println(p.Stop())

		case "help":
			fmt.Print(helpText)

		case "config":
			cfg = config.RunInteractive(cfg)
			p.Cfg = cfg

		default:
			fmt.Printf("Unknown command: %s. Type 'help' for commands.\n", cmd)
		}
	}

	p.Cleanup()
	fmt.Println("Bye!")
}
