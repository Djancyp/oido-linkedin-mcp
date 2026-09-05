package main

import (
	"context"
	"fmt"

	"github.com/chromedp/chromedp"
)

// card is one list entry scraped off a search/list page: its visible text
// and the first link found inside it (or the element itself, if it is the
// link). Per-tool code turns hrefs into ids via identifiers.go's
// *FromHref helpers.
type card struct {
	Text string `json:"text"`
	Href string `json:"href"`
}

// extractCards runs a small in-page script that maps every element matching
// cardSelector to its innerText and nearest link href. This targets stable
// structural anchors (e.g. `a[href*="/in/"]`) rather than LinkedIn's
// obfuscated CSS classes, mirroring the Python server's innerText-first
// extraction philosophy.
func extractCards(ctx context.Context, cardSelector string, limit int) ([]card, error) {
	js := fmt.Sprintf(`
		(() => {
			const cards = Array.from(document.querySelectorAll(%q)).slice(0, %d);
			return cards.map(el => {
				const link = el.tagName === 'A' ? el : el.querySelector('a[href]');
				return {
					text: (el.innerText || '').trim(),
					href: link ? link.href : '',
				};
			});
		})()
	`, cardSelector, limit)
	var out []card
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &out)); err != nil {
		return nil, fmt.Errorf("extract cards %q: %w", cardSelector, err)
	}
	// A single list card is normally short, but a malformed selector can
	// match a large container; cap defensively at a fraction of the
	// whole-page limit so one bad card can't dominate the response.
	for i := range out {
		if len(out[i].Text) > maxFieldChars/5 {
			out[i].Text = out[i].Text[:maxFieldChars/5] + "… [truncated]"
		}
	}
	return out, nil
}

// extractField reads a single value out of the page via a CSS selector's
// innerText, or "" if nothing matches.
func extractField(ctx context.Context, selector string) (string, error) {
	js := fmt.Sprintf(`
		(() => {
			const el = document.querySelector(%q);
			return el ? (el.innerText || '').trim() : '';
		})()
	`, selector)
	var out string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &out)); err != nil {
		return "", fmt.Errorf("extract field %q: %w", selector, err)
	}
	return capText(out), nil
}
