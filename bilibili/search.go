package bilibili

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var Verbose bool

func debugf(format string, args ...any) {
	if Verbose {
		log.Printf("[DEBUG] "+format, args...)
	}
}

type VideoResult struct {
	Index     int
	BvID      string
	Title     string
	Author    string
	Views     int64
	Likes     int64
	Favorites int64
	Duration  string
	URL       string
}

// SearchOrder represents the sort order for search results.
type SearchOrder string

const (
	OrderDefault SearchOrder = ""       // comprehensive ranking
	OrderViews   SearchOrder = "click"  // by play count DESC
	OrderNewest  SearchOrder = "pubdate" // by upload date DESC
)

// ParseOrder converts a user-provided string to a SearchOrder.
func ParseOrder(s string) (SearchOrder, bool) {
	switch strings.ToLower(s) {
	case "", "default":
		return OrderDefault, true
	case "views", "click", "play":
		return OrderViews, true
	case "new", "newest", "date", "pubdate", "time":
		return OrderNewest, true
	default:
		return "", false
	}
}

type SearchSession struct {
	client      *http.Client
	cookiesInit bool
	Keyword     string
	Uploader    string      // filter by uploader name (client-side)
	Order       SearchOrder // sort order
	Page        int
	Results     []VideoResult
	offset      int
	count       int
}

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

func NewSearchSession() *SearchSession {
	jar, _ := cookiejar.New(nil)
	return &SearchSession{
		client: &http.Client{Jar: jar},
		count:  3,
	}
}

// ensureCookies fetches bilibili.com once to get session cookies (buvid3, b_nut, etc.)
func (s *SearchSession) ensureCookies(ctx context.Context) {
	if s.cookiesInit {
		return
	}
	s.cookiesInit = true
	debugf("Fetching cookies from bilibili.com...")

	req, _ := http.NewRequestWithContext(ctx, "GET", "https://www.bilibili.com", nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := s.client.Do(req)
	if err != nil {
		debugf("Cookie fetch failed: %v", err)
		return
	}
	resp.Body.Close()

	u, _ := url.Parse("https://www.bilibili.com")
	cookies := s.client.Jar.Cookies(u)
	debugf("Got %d cookies", len(cookies))
	for _, c := range cookies {
		debugf("  Cookie: %s = %s", c.Name, truncate(c.Value, 20))
	}
}

func (s *SearchSession) Search(ctx context.Context, keyword string, count int, order SearchOrder, uploader string) ([]VideoResult, error) {
	s.Keyword = keyword
	s.Order = order
	s.Uploader = uploader
	s.Page = 1
	s.offset = 0
	s.Results = nil
	s.count = count
	if uploader != "" {
		debugf("Filtering by uploader: %s", uploader)
	}
	if order != OrderDefault {
		debugf("Ordering by: %s", order)
	}
	return s.fetch(ctx, count)
}

func (s *SearchSession) SearchMore(ctx context.Context) ([]VideoResult, error) {
	if s.Keyword == "" {
		return nil, fmt.Errorf("no active search, use 'search' first")
	}
	return s.fetch(ctx, s.count)
}

func (s *SearchSession) fetch(ctx context.Context, count int) ([]VideoResult, error) {
	var collected []VideoResult

	for len(collected) < count {
		if err := ctx.Err(); err != nil {
			if len(collected) > 0 {
				break
			}
			return nil, fmt.Errorf("interrupted")
		}
		if s.offset >= len(s.Results) {
			if err := s.fetchPage(ctx); err != nil {
				if len(collected) > 0 {
					break
				}
				return nil, err
			}
		}

		for s.offset < len(s.Results) && len(collected) < count {
			collected = append(collected, s.Results[s.offset])
			s.offset++
		}
	}
	return collected, nil
}

func (s *SearchSession) fetchPage(ctx context.Context) error {
	s.ensureCookies(ctx)

	params := url.Values{
		"search_type": {"video"},
		"keyword":     {s.Keyword},
		"page":        {strconv.Itoa(s.Page)},
	}
	if s.Order != OrderDefault {
		params.Set("order", string(s.Order))
	}

	reqURL := "https://api.bilibili.com/x/web-interface/search/type?" + params.Encode()
	debugf("GET %s", reqURL)

	req, _ := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", "https://search.bilibili.com")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("search request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	debugf("Response status: %d, body length: %d", resp.StatusCode, len(body))

	if resp.StatusCode == 412 {
		debugf("Raw response: %s", truncate(string(body), 500))
		// Reset cookies so next request re-fetches them
		s.cookiesInit = false
		return fmt.Errorf("rate limited by Bilibili (HTTP 412). Wait a moment and try again")
	}
	if resp.StatusCode == 429 {
		return fmt.Errorf("too many requests (HTTP 429). Please wait a minute before searching again")
	}
	if resp.StatusCode != 200 {
		debugf("Raw response: %s", truncate(string(body), 500))
		return fmt.Errorf("request failed (HTTP %d), please try again later", resp.StatusCode)
	}

	// The /search/type endpoint returns results directly in data.result
	var apiResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			NumResults int `json:"numResults"`
			NumPages   int `json:"numPages"`
			Page       int `json:"page"`
			Result     []json.RawMessage `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		debugf("Raw response: %s", truncate(string(body), 500))
		return fmt.Errorf("failed to parse response")
	}

	debugf("API code: %d, message: %s, numResults: %d, page: %d/%d",
		apiResp.Code, apiResp.Message, apiResp.Data.NumResults, apiResp.Data.Page, apiResp.Data.NumPages)

	if apiResp.Code != 0 {
		return fmt.Errorf("API error %d: %s", apiResp.Code, apiResp.Message)
	}

	if len(apiResp.Data.Result) == 0 {
		return fmt.Errorf("no more results")
	}

	debugf("Found %d video results in page", len(apiResp.Data.Result))

	for _, raw := range apiResp.Data.Result {
		var item struct {
			BvID      string `json:"bvid"`
			Title     string `json:"title"`
			Author    string `json:"author"`
			Play      any    `json:"play"`
			Like      int64  `json:"like"`
			Favorites int64  `json:"favorites"`
			Duration  string `json:"duration"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			debugf("Failed to parse result item: %v", err)
			continue
		}

		views := parseIntField(item.Play)

		// Client-side uploader filter
		if s.Uploader != "" && !strings.Contains(strings.ToLower(item.Author), strings.ToLower(s.Uploader)) {
			debugf("  Skipping (uploader mismatch): %s by %s", stripHTML(item.Title), item.Author)
			continue
		}

		idx := len(s.Results) + 1
		debugf("  [%d] %s (bvid=%s, views=%d)", idx, stripHTML(item.Title), item.BvID, views)
		s.Results = append(s.Results, VideoResult{
			Index:     idx,
			BvID:      item.BvID,
			Title:     stripHTML(item.Title),
			Author:    item.Author,
			Views:     views,
			Likes:     item.Like,
			Favorites: item.Favorites,
			Duration:  item.Duration,
			URL:       "https://www.bilibili.com/video/" + item.BvID,
		})
	}

	s.Page++
	return nil
}

func (s *SearchSession) GetVideo(number int) *VideoResult {
	if number >= 1 && number <= len(s.Results) {
		v := s.Results[number-1]
		return &v
	}
	return nil
}

func parseIntField(v any) int64 {
	switch val := v.(type) {
	case float64:
		return int64(val)
	case string:
		if strings.TrimSpace(val) == "--" {
			return 0
		}
		n, _ := strconv.ParseInt(val, 10, 64)
		return n
	}
	return 0
}

func stripHTML(s string) string {
	return htmlTagRe.ReplaceAllString(s, "")
}

func FormatCount(n int64) string {
	if n >= 100000000 {
		return fmt.Sprintf("%.1f亿", float64(n)/100000000)
	}
	if n >= 10000 {
		return fmt.Sprintf("%.1f万", float64(n)/10000)
	}
	return strconv.FormatInt(n, 10)
}

func FormatResults(videos []VideoResult) string {
	if len(videos) == 0 {
		return "No results found."
	}
	var b strings.Builder
	for _, v := range videos {
		fmt.Fprintf(&b, "  [%d] %s\n", v.Index, v.Title)
		fmt.Fprintf(&b, "      UP: %s  |  播放: %s  |  收藏: %s  |  时长: %s\n",
			v.Author, FormatCount(v.Views), FormatCount(v.Favorites), v.Duration)
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
