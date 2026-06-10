// Package sources fetches REAL content from public APIs — this is what makes the
// AI Digest agent do actual work instead of imagining it. Each adapter hits a
// real endpoint (arXiv, Hacker News, Reddit, generic web) and returns normalized
// Items the agent feeds to the LLM for synthesis.
//
// These are HTTP/JSON APIs, not browser scraping — for news/papers that's the
// right tool (clean, fast, no key). The browser layer (Browserbase) is for
// JS-heavy / auth-gated sites; see the Web adapter's fallback note.
package sources

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Item is a normalized piece of fetched content.
type Item struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Summary string `json:"summary"`
	Source  string `json:"source"`
	Score   int    `json:"score,omitempty"`
}

var httpClient = &http.Client{Timeout: 25 * time.Second}

func get(ctx context.Context, url string, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Agently/1.0 (+https://agently.local)")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4MB cap
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("status %d from %s", resp.StatusCode, url)
	}
	return body, nil
}

/* -------------------------------- arXiv ---------------------------------- */

// ArXiv fetches the latest papers for a query (Atom feed API). Default query
// targets AI/ML categories.
func ArXiv(ctx context.Context, query string, max int) ([]Item, error) {
	if query == "" {
		query = "cat:cs.AI OR cat:cs.LG OR cat:cs.CL"
	}
	if max <= 0 {
		max = 10
	}
	u := "http://export.arxiv.org/api/query?search_query=" + url.QueryEscape(query) +
		"&sortBy=submittedDate&sortOrder=descending&max_results=" + fmt.Sprint(max)
	body, err := get(ctx, u, "application/atom+xml")
	if err != nil {
		return nil, err
	}
	var feed struct {
		Entries []struct {
			Title   string `xml:"title"`
			Summary string `xml:"summary"`
			ID      string `xml:"id"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("arxiv parse: %w", err)
	}
	out := make([]Item, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		out = append(out, Item{
			Title:   clean(e.Title),
			URL:     strings.TrimSpace(e.ID),
			Summary: truncate(clean(e.Summary), 400),
			Source:  "arXiv",
		})
	}
	return out, nil
}

/* ----------------------------- Hacker News ------------------------------- */

// HackerNews fetches top stories matching a query via the Algolia HN API.
func HackerNews(ctx context.Context, query string, max int) ([]Item, error) {
	if query == "" {
		query = "AI"
	}
	if max <= 0 {
		max = 10
	}
	u := "https://hn.algolia.com/api/v1/search?tags=story&query=" + url.QueryEscape(query) +
		"&hitsPerPage=" + fmt.Sprint(max)
	body, err := get(ctx, u, "application/json")
	if err != nil {
		return nil, err
	}
	var res struct {
		Hits []struct {
			Title     string `json:"title"`
			URL       string `json:"url"`
			ObjectID  string `json:"objectID"`
			Points    int    `json:"points"`
			NumCommnt int    `json:"num_comments"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("hn parse: %w", err)
	}
	out := make([]Item, 0, len(res.Hits))
	for _, h := range res.Hits {
		link := h.URL
		if link == "" {
			link = "https://news.ycombinator.com/item?id=" + h.ObjectID
		}
		out = append(out, Item{
			Title:   clean(h.Title),
			URL:     link,
			Summary: fmt.Sprintf("%d points, %d comments", h.Points, h.NumCommnt),
			Source:  "Hacker News",
			Score:   h.Points,
		})
	}
	return out, nil
}

/* ----------------------------- Google News ------------------------------- */

// GoogleNews fetches recent news headlines for a query via Google News' public RSS
// feed (keyless). This is the "AI news" / "Google News" source — a plain RSS/XML
// parse, no scraping, no key. For sites that need a real browser (JS/auth) the
// browser layer (Browserbase) handles arbitrary URLs instead.
func GoogleNews(ctx context.Context, query string, max int) ([]Item, error) {
	if query == "" {
		query = "AI"
	}
	if max <= 0 {
		max = 10
	}
	u := "https://news.google.com/rss/search?q=" + url.QueryEscape(query) + "&hl=en-US&gl=US&ceid=US:en"
	body, err := get(ctx, u, "application/rss+xml")
	if err != nil {
		return nil, err
	}
	var feed struct {
		Items []struct {
			Title   string `xml:"title"`
			Link    string `xml:"link"`
			PubDate string `xml:"pubDate"`
			Source  string `xml:"source"`
		} `xml:"channel>item"`
	}
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("google news parse: %w", err)
	}
	out := make([]Item, 0, len(feed.Items))
	for i, it := range feed.Items {
		if i >= max {
			break
		}
		summary := it.Source
		if it.PubDate != "" {
			summary = clean(it.Source + " · " + it.PubDate)
		}
		out = append(out, Item{
			Title:   clean(it.Title),
			URL:     strings.TrimSpace(it.Link),
			Summary: summary,
			Source:  "Google News",
		})
	}
	return out, nil
}

/* ------------------------------- Reddit ---------------------------------- */

// Reddit fetches hot posts from a subreddit via its public JSON endpoint.
func Reddit(ctx context.Context, subreddit string, max int) ([]Item, error) {
	if subreddit == "" {
		subreddit = "MachineLearning"
	}
	if max <= 0 {
		max = 10
	}
	// Reddit blocks generic UAs/datacenter IPs on www; old.reddit is more permissive.
	// Try a couple of hosts, then give up gracefully (the agent proceeds without it).
	hosts := []string{"https://old.reddit.com", "https://www.reddit.com"}
	var body []byte
	var err error
	for _, h := range hosts {
		u := fmt.Sprintf("%s/r/%s/hot.json?limit=%d", h, url.PathEscape(subreddit), max)
		body, err = get(ctx, u, "application/json")
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	var res struct {
		Data struct {
			Children []struct {
				Data struct {
					Title     string `json:"title"`
					Permalink string `json:"permalink"`
					URL       string `json:"url"`
					Selftext  string `json:"selftext"`
					Ups       int    `json:"ups"`
				} `json:"data"`
			} `json:"children"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("reddit parse: %w", err)
	}
	out := make([]Item, 0, len(res.Data.Children))
	for _, c := range res.Data.Children {
		d := c.Data
		link := "https://www.reddit.com" + d.Permalink
		out = append(out, Item{
			Title:   clean(d.Title),
			URL:     link,
			Summary: truncate(clean(d.Selftext), 300),
			Source:  "r/" + subreddit,
			Score:   d.Ups,
		})
	}
	return out, nil
}

/* ----------------------------- generic web ------------------------------- */

// Web fetches a page and returns its text content (tags stripped). For JS-heavy
// or auth-gated sites this won't see client-rendered content — that's the
// browser layer's job (Browserbase). Used for simple article/news pages.
func Web(ctx context.Context, pageURL string) (Item, error) {
	body, err := get(ctx, pageURL, "text/html")
	if err != nil {
		return Item{}, err
	}
	html := string(body)
	title := extractTitle(html)
	text := stripHTML(html)
	return Item{
		Title:   firstNonEmpty(title, pageURL),
		URL:     pageURL,
		Summary: truncate(text, 800),
		Source:  "Web",
	}, nil
}

/* -------------------------------- helpers -------------------------------- */

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func extractTitle(html string) string {
	lo := strings.ToLower(html)
	i := strings.Index(lo, "<title")
	if i < 0 {
		return ""
	}
	j := strings.IndexByte(html[i:], '>')
	if j < 0 {
		return ""
	}
	start := i + j + 1
	end := strings.Index(strings.ToLower(html[start:]), "</title>")
	if end < 0 {
		return ""
	}
	return clean(html[start : start+end])
}

// stripHTML removes script/style blocks and tags, returning plain text.
func stripHTML(html string) string {
	for _, tag := range []string{"script", "style", "noscript", "svg"} {
		for {
			lo := strings.ToLower(html)
			open := strings.Index(lo, "<"+tag)
			if open < 0 {
				break
			}
			close := strings.Index(lo[open:], "</"+tag+">")
			if close < 0 {
				html = html[:open]
				break
			}
			html = html[:open] + " " + html[open+close+len(tag)+3:]
		}
	}
	var b strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return clean(b.String())
}
