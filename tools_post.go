package main

import (
	"context"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var postDateFilter = map[string]string{
	"past-24h":   "past-24h",
	"past-week":  "past-week",
	"past-month": "past-month",
}

type SearchPostsArgs struct {
	Keywords   string `json:"keywords" jsonschema:"Search keywords, e.g. \"Buscamos Unity\", \"AI automation hiring\""`
	DatePosted string `json:"date_posted,omitempty" jsonschema:"Optional recency filter: past-24h, past-week or past-month"`
	MaxPages   int    `json:"max_pages,omitempty" jsonschema:"Scroll depth as result pages of ~5 scrolls each (1-10, default 3)"`
}

type postSearchResult struct {
	PostURL string `json:"post_url"`
	Text    string `json:"text"`
}

func (h *handler) SearchPosts(ctx context.Context, _ *mcp.CallToolRequest, a SearchPostsArgs) (*mcp.CallToolResult, any, error) {
	q := url.Values{"keywords": {a.Keywords}}
	if code, ok := postDateFilter[a.DatePosted]; ok {
		q.Set("datePosted", code)
	}
	maxPages := clampInt(a.MaxPages, 1, 10, 3)
	searchURL := "https://www.linkedin.com/search/results/content/?" + q.Encode()
	return withPage(ctx, func(pctx context.Context) (any, error) {
		if err := navigate(pctx, searchURL); err != nil {
			return nil, err
		}
		if err := scrollToBottom(pctx, maxPages*5); err != nil {
			return nil, err
		}
		cards, err := extractCards(pctx, `div[data-urn^="urn:li:activity:"]`, 100)
		if err != nil {
			return nil, err
		}
		out := make([]postSearchResult, 0, len(cards))
		for _, c := range cards {
			out = append(out, postSearchResult{PostURL: c.Href, Text: c.Text})
		}
		return out, nil
	})
}
