package source

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

// YouTube implements Source using yt-dlp for search and playback.
type YouTube struct {
	verbose  bool
	keyword  string
	uploader string
	order    SearchOrder
	count    int
	page     int
	results  []VideoResult
	offset   int
}

func NewYouTube() *YouTube {
	return &YouTube{count: 3}
}

func (y *YouTube) Name() string { return "YouTube" }

func (y *YouTube) Search(ctx context.Context, keyword string, count int, order SearchOrder, uploader string) ([]VideoResult, error) {
	y.keyword = keyword
	y.uploader = uploader
	y.order = order
	y.count = count
	y.page = 1
	y.results = nil
	y.offset = 0
	return y.fetch(ctx, count)
}

func (y *YouTube) SearchMore(ctx context.Context) ([]VideoResult, error) {
	if y.keyword == "" {
		return nil, fmt.Errorf("no previous search")
	}
	return y.fetch(ctx, y.count)
}

func (y *YouTube) fetch(ctx context.Context, count int) ([]VideoResult, error) {
	// yt-dlp uses ytsearchN: prefix for YouTube search
	// Fetch more than needed to allow for uploader filtering
	fetchCount := count
	if y.uploader != "" {
		fetchCount = count * 5
	}

	totalNeeded := y.offset + fetchCount
	query := fmt.Sprintf("ytsearch%d:%s", totalNeeded, y.keyword)

	if y.verbose {
		log.Printf("[DEBUG] Running: yt-dlp --flat-playlist --dump-json %q", query)
	}

	args := []string{"--flat-playlist", "--dump-json", query}

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("yt-dlp search failed: %w", err)
	}

	// yt-dlp outputs one JSON object per line
	var allResults []VideoResult
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var item ytResult
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			continue
		}

		// Apply uploader filter
		if y.uploader != "" && !strings.Contains(
			strings.ToLower(item.Uploader), strings.ToLower(y.uploader)) {
			continue
		}

		allResults = append(allResults, VideoResult{
			ID:       item.ID,
			Title:    item.Title,
			Author:   item.Uploader,
			Views:    item.ViewCount,
			Duration: formatDuration(item.Duration),
			URL:      fmt.Sprintf("https://www.youtube.com/watch?v=%s", item.ID),
		})
	}

	// Client-side sort by views (descending) if requested
	if y.order == OrderViews {
		sort.Slice(allResults, func(i, j int) bool {
			return allResults[i].Views > allResults[j].Views
		})
	}

	// Return only the new results (skip ones we already returned)
	var newResults []VideoResult
	startIdx := y.offset
	endIdx := startIdx + count
	if endIdx > len(allResults) {
		endIdx = len(allResults)
	}

	baseIndex := len(y.results) + 1
	for i := startIdx; i < endIdx; i++ {
		r := allResults[i]
		r.Index = baseIndex + (i - startIdx)
		newResults = append(newResults, r)
	}

	y.results = append(y.results, newResults...)
	y.offset = endIdx

	if len(newResults) == 0 {
		return nil, fmt.Errorf("no more results")
	}
	return newResults, nil
}

func (y *YouTube) GetVideo(number int) *VideoResult {
	for i := range y.results {
		if y.results[i].Index == number {
			return &y.results[i]
		}
	}
	return nil
}

func (y *YouTube) ParseOrder(s string) (SearchOrder, bool) {
	switch strings.ToLower(s) {
	case "", "default":
		return OrderDefault, true
	case "views", "view", "play":
		return OrderViews, true
	case "new", "newest", "date", "time":
		// YouTube search via yt-dlp returns by relevance.
		// We accept the flag but results won't truly be sorted by date.
		return OrderNewest, true
	default:
		return "", false
	}
}

func (y *YouTube) SetVerbose(v bool) {
	y.verbose = v
}

type ytResult struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Uploader  string  `json:"uploader"`
	ViewCount int64   `json:"view_count"`
	Duration  float64 `json:"duration"`
}

func formatDuration(seconds float64) string {
	s := int(seconds)
	if s < 60 {
		return fmt.Sprintf("0:%02d", s)
	}
	if s < 3600 {
		return fmt.Sprintf("%d:%02d", s/60, s%60)
	}
	return fmt.Sprintf("%d:%02d:%02d", s/3600, (s%3600)/60, s%60)
}
