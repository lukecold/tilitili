package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const biliUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

// Bilibili search order values (API-specific).
const (
	biliOrderDefault = ""
	biliOrderViews   = "click"
	biliOrderNewest  = "pubdate"
)

// Bilibili implements Source for bilibili.com.
type Bilibili struct {
	client      *http.Client
	cookiesInit bool
	keyword     string
	uploader    string
	order       string // bilibili API order value
	page        int
	results     []VideoResult
	offset      int
	count       int
}

func NewBilibili() *Bilibili {
	jar, _ := cookiejar.New(nil)
	return &Bilibili{
		client: &http.Client{Jar: jar},
		count:  3,
	}
}

func (b *Bilibili) Name() string { return "Bilibili" }

func (b *Bilibili) Search(ctx context.Context, keyword string, count int, order SearchOrder, uploader string) ([]VideoResult, error) {
	b.keyword = keyword
	b.order = toBiliOrder(order)
	b.uploader = uploader
	b.page = 1
	b.offset = 0
	b.results = nil
	b.count = count
	if uploader != "" {
		debugf("Filtering by uploader: %s", uploader)
	}
	if order != OrderDefault {
		debugf("Ordering by: %s", b.order)
	}
	return b.fetch(ctx, count)
}

func (b *Bilibili) SearchMore(ctx context.Context) ([]VideoResult, error) {
	if b.keyword == "" {
		return nil, fmt.Errorf("no active search, use 'search' first")
	}
	return b.fetch(ctx, b.count)
}

func (b *Bilibili) GetVideo(number int) *VideoResult {
	if number >= 1 && number <= len(b.results) {
		v := b.results[number-1]
		return &v
	}
	return nil
}

func (b *Bilibili) ParseOrder(s string) (SearchOrder, bool) {
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

func (b *Bilibili) SetVerbose(v bool) {
	Verbose = v
}

// --- internal ---

func (b *Bilibili) ensureCookies(ctx context.Context) {
	if b.cookiesInit {
		return
	}
	b.cookiesInit = true
	debugf("Fetching cookies from bilibili.com...")

	req, _ := http.NewRequestWithContext(ctx, "GET", "https://www.bilibili.com", nil)
	req.Header.Set("User-Agent", biliUserAgent)
	resp, err := b.client.Do(req)
	if err != nil {
		debugf("Cookie fetch failed: %v", err)
		return
	}
	resp.Body.Close()

	u, _ := url.Parse("https://www.bilibili.com")
	cookies := b.client.Jar.Cookies(u)
	debugf("Got %d cookies", len(cookies))
	for _, c := range cookies {
		debugf("  Cookie: %s = %s", c.Name, truncate(c.Value, 20))
	}
}

func (b *Bilibili) fetch(ctx context.Context, count int) ([]VideoResult, error) {
	var collected []VideoResult

	for len(collected) < count {
		if err := ctx.Err(); err != nil {
			if len(collected) > 0 {
				break
			}
			return nil, fmt.Errorf("interrupted")
		}
		if b.offset >= len(b.results) {
			if err := b.fetchPage(ctx); err != nil {
				if len(collected) > 0 {
					break
				}
				return nil, err
			}
		}

		for b.offset < len(b.results) && len(collected) < count {
			collected = append(collected, b.results[b.offset])
			b.offset++
		}
	}
	return collected, nil
}

func (b *Bilibili) fetchPage(ctx context.Context) error {
	b.ensureCookies(ctx)

	params := url.Values{
		"search_type": {"video"},
		"keyword":     {b.keyword},
		"page":        {strconv.Itoa(b.page)},
	}
	if b.order != biliOrderDefault {
		params.Set("order", b.order)
	}

	reqURL := "https://api.bilibili.com/x/web-interface/search/type?" + params.Encode()
	debugf("GET %s", reqURL)

	req, _ := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	req.Header.Set("User-Agent", biliUserAgent)
	req.Header.Set("Referer", "https://search.bilibili.com")

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("search request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	debugf("Response status: %d, body length: %d", resp.StatusCode, len(body))

	if resp.StatusCode == 412 {
		debugf("Raw response: %s", truncate(string(body), 500))
		b.cookiesInit = false
		return fmt.Errorf("rate limited by Bilibili (HTTP 412). Wait a moment and try again")
	}
	if resp.StatusCode == 429 {
		return fmt.Errorf("too many requests (HTTP 429). Please wait a minute before searching again")
	}
	if resp.StatusCode != 200 {
		debugf("Raw response: %s", truncate(string(body), 500))
		return fmt.Errorf("request failed (HTTP %d), please try again later", resp.StatusCode)
	}

	var apiResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			NumResults int               `json:"numResults"`
			NumPages   int               `json:"numPages"`
			Page       int               `json:"page"`
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

		if b.uploader != "" && !strings.Contains(strings.ToLower(item.Author), strings.ToLower(b.uploader)) {
			debugf("  Skipping (uploader mismatch): %s by %s", stripHTML(item.Title), item.Author)
			continue
		}

		idx := len(b.results) + 1
		debugf("  [%d] %s (bvid=%s, views=%d)", idx, stripHTML(item.Title), item.BvID, views)
		b.results = append(b.results, VideoResult{
			Index:     idx,
			ID:        item.BvID,
			Title:     stripHTML(item.Title),
			Author:    item.Author,
			Views:     views,
			Likes:     item.Like,
			Favorites: item.Favorites,
			Duration:  item.Duration,
			URL:       "https://www.bilibili.com/video/" + item.BvID,
		})
	}

	b.page++
	return nil
}

func toBiliOrder(o SearchOrder) string {
	switch o {
	case OrderViews:
		return biliOrderViews
	case OrderNewest:
		return biliOrderNewest
	default:
		return biliOrderDefault
	}
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
