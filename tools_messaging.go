package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type conversationSummary struct {
	ThreadURL string `json:"thread_url"`
	Text      string `json:"text"`
}

// ---- get_inbox ----

type InboxArgs struct {
	Limit int `json:"limit,omitempty" jsonschema:"Maximum number of conversations to load (1-50, default 20)"`
}

func (h *handler) GetInbox(ctx context.Context, _ *mcp.CallToolRequest, a InboxArgs) (*mcp.CallToolResult, any, error) {
	limit := clampInt(a.Limit, 1, 50, 20)
	return withPage(ctx, func(pctx context.Context) (any, error) {
		if err := navigate(pctx, "https://www.linkedin.com/messaging/"); err != nil {
			return nil, err
		}
		if err := scrollToBottom(pctx, 2); err != nil {
			return nil, err
		}
		cards, err := extractCards(pctx, `a[href*="/messaging/thread/"]`, limit)
		if err != nil {
			return nil, err
		}
		out := make([]conversationSummary, 0, len(cards))
		for _, c := range cards {
			out = append(out, conversationSummary{ThreadURL: c.Href, Text: c.Text})
		}
		return out, nil
	})
}

// ---- search_conversations ----

type SearchConversationsArgs struct {
	Keywords string `json:"keywords" jsonschema:"Search keywords to filter conversations"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum number of matching conversations to return (1-50, default 20)"`
}

// SearchConversations loads the inbox and filters locally by keyword, since
// LinkedIn's messaging search box has no reliable URL query parameter.
func (h *handler) SearchConversations(ctx context.Context, _ *mcp.CallToolRequest, a SearchConversationsArgs) (*mcp.CallToolResult, any, error) {
	limit := clampInt(a.Limit, 1, 50, 20)
	needle := strings.ToLower(a.Keywords)
	return withPage(ctx, func(pctx context.Context) (any, error) {
		if err := navigate(pctx, "https://www.linkedin.com/messaging/"); err != nil {
			return nil, err
		}
		if err := scrollToBottom(pctx, 5); err != nil {
			return nil, err
		}
		cards, err := extractCards(pctx, `a[href*="/messaging/thread/"]`, 300)
		if err != nil {
			return nil, err
		}
		out := make([]conversationSummary, 0, limit)
		for _, c := range cards {
			if !strings.Contains(strings.ToLower(c.Text), needle) {
				continue
			}
			out = append(out, conversationSummary{ThreadURL: c.Href, Text: c.Text})
			if len(out) >= limit {
				break
			}
		}
		return out, nil
	})
}

// ---- get_conversation ----

type ConversationArgs struct {
	LinkedInUsername string `json:"linkedin_username,omitempty" jsonschema:"LinkedIn /in/ public identifier of the conversation participant"`
	ThreadID         string `json:"thread_id,omitempty" jsonschema:"LinkedIn messaging thread id"`
	Index            int    `json:"index,omitempty" jsonschema:"0-based selector for which inbox thread to open when neither linkedin_username nor thread_id is given"`
}

func (h *handler) GetConversation(ctx context.Context, _ *mcp.CallToolRequest, a ConversationArgs) (*mcp.CallToolResult, any, error) {
	return withPage(ctx, func(pctx context.Context) (any, error) {
		switch {
		case a.ThreadID != "":
			id, err := normalizeThreadID(a.ThreadID)
			if err != nil {
				return nil, err
			}
			if err := navigate(pctx, messagingThreadURL(id)); err != nil {
				return nil, err
			}
		case a.LinkedInUsername != "":
			pid, err := normalizePersonIdentifier(a.LinkedInUsername, false)
			if err != nil {
				return nil, err
			}
			if err := navigate(pctx, personProfileURL(pid)); err != nil {
				return nil, err
			}
			clicked, err := clickButtonWithText(pctx, "message")
			if err != nil {
				return nil, err
			}
			if !clicked {
				return nil, fmt.Errorf("no Message button found on %s's profile — you may not be connected, or LinkedIn requires opening the conversation from the inbox instead", pid)
			}
			if err := chromedp.Run(pctx, chromedp.Sleep(800*time.Millisecond)); err != nil {
				return nil, err
			}
		default:
			if err := navigate(pctx, "https://www.linkedin.com/messaging/"); err != nil {
				return nil, err
			}
			cards, err := extractCards(pctx, `a[href*="/messaging/thread/"]`, a.Index+1)
			if err != nil {
				return nil, err
			}
			if len(cards) <= a.Index {
				return nil, fmt.Errorf("no conversation at index %d (inbox has %d loaded)", a.Index, len(cards))
			}
			if err := navigate(pctx, cards[a.Index].Href); err != nil {
				return nil, err
			}
		}
		// Scroll the message panel itself to the top to load LinkedIn's
		// lazy-loaded older messages before reading it, then read just that
		// panel rather than the whole page (which, opened via a profile's
		// Message button, would otherwise dwarf the chat with profile text).
		if err := scrollConversationToTop(pctx, 5); err != nil {
			return nil, err
		}
		text, err := conversationText(pctx)
		if err != nil {
			return nil, err
		}
		return map[string]string{"text": text}, nil
	})
}

// ---- send_message ----

type SendMessageArgs struct {
	LinkedInUsername string `json:"linkedin_username" jsonschema:"LinkedIn /in/ public identifier of the recipient"`
	Message          string `json:"message" jsonschema:"The message text to send"`
	ConfirmSend      bool   `json:"confirm_send" jsonschema:"Must be true to send the message"`
	// ProfileURN mirrors the Python tool's argument for parity but is unused
	// here: this port always reaches the composer via the profile's Message
	// button rather than a URN-addressed compose deep link.
	ProfileURN string `json:"profile_urn,omitempty" jsonschema:"Unused in this port; accepted for compatibility"`
}

func (h *handler) SendMessage(ctx context.Context, _ *mcp.CallToolRequest, a SendMessageArgs) (*mcp.CallToolResult, any, error) {
	if !a.ConfirmSend {
		return errResult(fmt.Errorf("confirm_send must be true to send a message — this notifies a real person and cannot be undone")), nil, nil
	}
	if strings.TrimSpace(a.Message) == "" {
		return errResult(fmt.Errorf("message is required")), nil, nil
	}
	id, err := normalizePersonIdentifier(a.LinkedInUsername, false)
	if err != nil {
		return errResult(err), nil, nil
	}
	return withPage(ctx, func(pctx context.Context) (any, error) {
		if err := navigate(pctx, personProfileURL(id)); err != nil {
			return nil, err
		}
		clicked, err := clickButtonWithText(pctx, "message")
		if err != nil {
			return nil, err
		}
		if !clicked {
			return nil, fmt.Errorf("no Message button found on %s's profile — you may not be connected", id)
		}
		if err := chromedp.Run(pctx, chromedp.Sleep(800*time.Millisecond)); err != nil {
			return nil, err
		}
		if err := typeIntoActiveEditable(pctx, `div.msg-form__contenteditable[contenteditable="true"]`, a.Message); err != nil {
			return nil, err
		}
		sent, err := clickButtonWithText(pctx, "send")
		if err != nil {
			return nil, err
		}
		status := "sent"
		if !sent {
			status = "typed_but_send_button_not_found"
		}
		return map[string]string{"linkedin_username": id, "status": status}, nil
	})
}
