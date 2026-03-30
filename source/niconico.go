package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const nicoSearchURL = "https://api.search.nicovideo.jp/api/v2/snapshot/video/contents/search"
const nicoContext = "tilitili"

// Niconico implements Source for nicovideo.jp.
type Niconico struct {
	keyword  string
	uploader string // not supported by API, applied client-side if needed
	order    string // API sort field (e.g. "-viewCounter")
	results  []VideoResult
	offset   int // API offset for pagination
	count    int
}

func NewNiconico() *Niconico {
	return &Niconico{count: 3}
}

func (n *Niconico) Name() string { return "Niconico" }

func (n *Niconico) Search(ctx context.Context, keyword string, count int, order SearchOrder, uploader string) ([]VideoResult, error) {
	n.keyword = keyword
	n.uploader = uploader
	n.order = toNicoOrder(order)
	n.count = count
	n.offset = 0
	n.results = nil
	return n.fetch(ctx, count)
}

func (n *Niconico) SearchMore(ctx context.Context) ([]VideoResult, error) {
	if n.keyword == "" {
		return nil, fmt.Errorf("no active search, use 'search' first")
	}
	return n.fetch(ctx, n.count)
}

func (n *Niconico) GetVideo(number int) *VideoResult {
	if number >= 1 && number <= len(n.results) {
		v := n.results[number-1]
		return &v
	}
	return nil
}

func (n *Niconico) ParseOrder(s string) (SearchOrder, bool) {
	switch strings.ToLower(s) {
	case "", "default":
		return OrderDefault, true
	case "views", "view", "play":
		return OrderViews, true
	case "new", "newest", "date", "time":
		return OrderNewest, true
	default:
		return "", false
	}
}

func (n *Niconico) SetVerbose(v bool) {
	Verbose = v
}

// --- internal ---

func (n *Niconico) fetch(ctx context.Context, count int) ([]VideoResult, error) {
	// Fetch more if uploader filter is active
	limit := count
	if n.uploader != "" {
		limit = count * 5
	}
	if limit > 100 {
		limit = 100
	}

	params := url.Values{
		"q":        {n.keyword},
		"targets":  {"title,tags"},
		"fields":   {"contentId,title,viewCounter,mylistCounter,commentCounter,lengthSeconds,startTime"},
		"_sort":    {n.order},
		"_limit":   {strconv.Itoa(limit)},
		"_offset":  {strconv.Itoa(n.offset)},
		"_context": {nicoContext},
	}

	reqURL := nicoSearchURL + "?" + params.Encode()
	debugf("GET %s", reqURL)

	req, _ := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	req.Header.Set("User-Agent", "tilitili/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	debugf("Response status: %d, body length: %d", resp.StatusCode, len(body))

	if resp.StatusCode == 403 {
		return nil, fmt.Errorf("Niconico search is region-restricted (Japan only). Use a VPN or set up a proxy")
	}
	if resp.StatusCode != 200 {
		debugf("Raw response: %s", truncate(string(body), 500))
		return nil, fmt.Errorf("request failed (HTTP %d)", resp.StatusCode)
	}

	var apiResp struct {
		Meta struct {
			Status       int    `json:"status"`
			TotalCount   int    `json:"totalCount"`
			ErrorCode    string `json:"errorCode"`
			ErrorMessage string `json:"errorMessage"`
		} `json:"meta"`
		Data []nicoVideoItem `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		debugf("Raw response: %s", truncate(string(body), 500))
		return nil, fmt.Errorf("failed to parse response")
	}

	if apiResp.Meta.Status != 200 {
		return nil, fmt.Errorf("API error: %s — %s", apiResp.Meta.ErrorCode, apiResp.Meta.ErrorMessage)
	}

	debugf("totalCount: %d, returned: %d", apiResp.Meta.TotalCount, len(apiResp.Data))

	if len(apiResp.Data) == 0 {
		return nil, fmt.Errorf("no more results")
	}

	// Advance the API offset for next page
	n.offset += len(apiResp.Data)

	var collected []VideoResult
	for _, item := range apiResp.Data {
		// Client-side uploader filter (API doesn't support filtering by uploader)
		// Niconico doesn't return uploader in search results, so -u is limited
		idx := len(n.results) + 1
		v := VideoResult{
			Index:     idx,
			ID:        item.ContentID,
			Title:     item.Title,
			Author:    "", // Not available from snapshot search API
			Views:     int64(item.ViewCounter),
			Likes:     int64(item.MylistCounter), // mylist ≈ favorites/bookmarks
			Favorites: int64(item.CommentCounter),
			Duration:  formatNicoDuration(item.LengthSeconds),
			URL:       "https://www.nicovideo.jp/watch/" + item.ContentID,
		}
		debugf("  [%d] %s (id=%s, views=%d)", idx, item.Title, item.ContentID, item.ViewCounter)
		n.results = append(n.results, v)
		collected = append(collected, v)
		if len(collected) >= count {
			break
		}
	}

	if len(collected) == 0 {
		return nil, fmt.Errorf("no more results")
	}
	return collected, nil
}

type nicoVideoItem struct {
	ContentID      string `json:"contentId"`
	Title          string `json:"title"`
	ViewCounter    int    `json:"viewCounter"`
	MylistCounter  int    `json:"mylistCounter"`
	CommentCounter int    `json:"commentCounter"`
	LengthSeconds  int    `json:"lengthSeconds"`
	StartTime      string `json:"startTime"`
}

func toNicoOrder(o SearchOrder) string {
	switch o {
	case OrderViews:
		return "-viewCounter"
	case OrderNewest:
		return "-startTime"
	default:
		return "-viewCounter" // default to most viewed
	}
}

func formatNicoDuration(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("0:%02d", seconds)
	}
	if seconds < 3600 {
		return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
	}
	return fmt.Sprintf("%d:%02d:%02d", seconds/3600, (seconds%3600)/60, seconds%60)
}
