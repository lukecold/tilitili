package source

import (
	"context"

	"tilitili/bilibili"
)

// Bilibili implements Source for bilibili.com.
type Bilibili struct {
	session *bilibili.SearchSession
}

func NewBilibili() *Bilibili {
	return &Bilibili{session: bilibili.NewSearchSession()}
}

func (b *Bilibili) Name() string { return "Bilibili" }

func (b *Bilibili) Search(ctx context.Context, keyword string, count int, order SearchOrder, uploader string) ([]VideoResult, error) {
	biliOrder := toBiliOrder(order)
	results, err := b.session.Search(ctx, keyword, count, biliOrder, uploader)
	if err != nil {
		return nil, err
	}
	return convertResults(results), nil
}

func (b *Bilibili) SearchMore(ctx context.Context) ([]VideoResult, error) {
	results, err := b.session.SearchMore(ctx)
	if err != nil {
		return nil, err
	}
	return convertResults(results), nil
}

func (b *Bilibili) GetVideo(number int) *VideoResult {
	v := b.session.GetVideo(number)
	if v == nil {
		return nil
	}
	return &VideoResult{
		Index:     v.Index,
		ID:        v.BvID,
		Title:     v.Title,
		Author:    v.Author,
		Views:     v.Views,
		Likes:     v.Likes,
		Favorites: v.Favorites,
		Duration:  v.Duration,
		URL:       v.URL,
	}
}

func (b *Bilibili) ParseOrder(s string) (SearchOrder, bool) {
	o, ok := bilibili.ParseOrder(s)
	if !ok {
		return "", false
	}
	switch o {
	case bilibili.OrderViews:
		return OrderViews, true
	case bilibili.OrderNewest:
		return OrderNewest, true
	default:
		return OrderDefault, true
	}
}

func (b *Bilibili) SetVerbose(v bool) {
	bilibili.Verbose = v
}

func toBiliOrder(o SearchOrder) bilibili.SearchOrder {
	switch o {
	case OrderViews:
		return bilibili.OrderViews
	case OrderNewest:
		return bilibili.OrderNewest
	default:
		return bilibili.OrderDefault
	}
}

func convertResults(biliResults []bilibili.VideoResult) []VideoResult {
	results := make([]VideoResult, len(biliResults))
	for i, v := range biliResults {
		results[i] = VideoResult{
			Index:     v.Index,
			ID:        v.BvID,
			Title:     v.Title,
			Author:    v.Author,
			Views:     v.Views,
			Likes:     v.Likes,
			Favorites: v.Favorites,
			Duration:  v.Duration,
			URL:       v.URL,
		}
	}
	return results
}
