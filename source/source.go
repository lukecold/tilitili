package source

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
)

// Verbose controls debug logging across all sources and the player.
var Verbose bool

func debugf(format string, args ...any) {
	if Verbose {
		log.Printf("[DEBUG] "+format, args...)
	}
}

// VideoResult represents a single video from any source.
type VideoResult struct {
	Index     int
	ID        string // platform-specific ID (BvID, YouTube video ID, etc.)
	Title     string
	Author    string
	Views     int64
	Likes     int64
	Favorites int64
	Duration  string
	URL       string
}

// SearchOrder represents sort order for search results.
type SearchOrder string

const (
	OrderDefault SearchOrder = ""
	OrderViews  SearchOrder = "views"
	OrderNewest SearchOrder = "newest"
)

// Source is the interface that all video sources must implement.
type Source interface {
	// Name returns the display name of this source (e.g. "Bilibili", "YouTube").
	Name() string

	// Search performs a new search with the given parameters.
	Search(ctx context.Context, keyword string, count int, order SearchOrder, uploader string) ([]VideoResult, error)

	// SearchMore fetches the next batch of results from the current search.
	SearchMore(ctx context.Context) ([]VideoResult, error)

	// GetVideo returns a video by its display index number.
	GetVideo(number int) *VideoResult

	// ParseOrder converts a user-provided order string to a SearchOrder.
	// Returns the order and whether it was valid.
	ParseOrder(s string) (SearchOrder, bool)

	// SetVerbose enables or disables debug logging.
	SetVerbose(v bool)
}

// FormatResults formats video results for display.
func FormatResults(videos []VideoResult) string {
	if len(videos) == 0 {
		return "No results found."
	}
	var b strings.Builder
	for _, v := range videos {
		fmt.Fprintf(&b, "  [%d] %s\n", v.Index, v.Title)
		fmt.Fprintf(&b, "      UP: %s  |  Views: %s  |  Duration: %s\n",
			v.Author, FormatCount(v.Views), v.Duration)
	}
	return b.String()
}

// FormatCount formats large numbers with K/M suffixes.
func FormatCount(n int64) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return strconv.FormatInt(n, 10)
}

// uploaderMatches checks if an author name fuzzy-matches the uploader query.
// Each query word must fuzzy-match at least one word in the author name
// (edit distance <= 2, or substring match).
func uploaderMatches(author, query string) bool {
	authorWords := strings.Fields(strings.ToLower(author))
	queryWords := strings.Fields(strings.ToLower(query))
	if len(queryWords) == 0 {
		return true
	}
	for _, qw := range queryWords {
		found := false
		for _, aw := range authorWords {
			if strings.Contains(aw, qw) || strings.Contains(qw, aw) || levenshtein(aw, qw) <= 2 {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// levenshtein computes the edit distance between two strings.
func levenshtein(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// AvailableSources returns the list of supported source names.
func AvailableSources() []string {
	return []string{"Bilibili", "YouTube", "Niconico"}
}
