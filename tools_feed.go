package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type FeedArgs struct {
	NumPosts int `json:"num_posts,omitempty" jsonschema:"Number of posts to fetch (1-50, default 10)"`
}

type feedPost struct {
	PostURL string `json:"post_url"`
	Text    string `json:"text"`
}

func (h *handler) GetFeed(ctx context.Context, _ *mcp.CallToolRequest, a FeedArgs) (*mcp.CallToolResult, any, error) {
	numPosts := clampInt(a.NumPosts, 1, 50, 10)
	return withPage(ctx, func(pctx context.Context) (any, error) {
		if err := navigate(pctx, "https://www.linkedin.com/feed/"); err != nil {
			return nil, err
		}
		if err := scrollToBottom(pctx, (numPosts/3)+2); err != nil {
			return nil, err
		}
		cards, err := extractCards(pctx, `div[data-urn^="urn:li:activity:"]`, numPosts)
		if err != nil {
			return nil, err
		}
		out := make([]feedPost, 0, len(cards))
		for _, c := range cards {
			out = append(out, feedPost{PostURL: c.Href, Text: c.Text})
		}
		return out, nil
	})
}
