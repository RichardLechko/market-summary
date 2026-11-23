package collect

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"

	"github.com/RichardLechko/market-summary/internal/httpx"
	"github.com/RichardLechko/market-summary/internal/model"
)

// Fed reads Federal Reserve press release RSS feeds.
type Fed struct {
	HTTP *httpx.Client
}

// Docs returns items from feedURL published after since, skipping titles
// that contain any phrase in exclude (case-insensitive). Each document's page
// text is fetched into Body.
func (f *Fed) Docs(ctx context.Context, feedURL string, exclude []string, since time.Time) ([]model.FedDoc, error) {
	body, err := f.HTTP.Get(ctx, feedURL)
	if err != nil {
		return nil, err
	}
	feed, err := gofeed.NewParser().Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", feedURL, err)
	}
	var out []model.FedDoc
	for _, it := range feed.Items {
		if it.PublishedParsed == nil || !it.PublishedParsed.After(since) {
			continue
		}
		if ExcludedTitle(it.Title, exclude) {
			continue
		}
		doc := model.FedDoc{
			Link:      it.Link,
			Title:     strings.TrimSpace(it.Title),
			Kind:      ClassifyFedTitle(it.Title),
			Published: it.PublishedParsed.Local(),
		}
		page, err := f.HTTP.Get(ctx, it.Link)
		if err == nil {
			doc.Body = HTMLText(page, "article")
			// The minutes press release only announces them; read the minutes.
			if doc.Kind == "minutes" {
				if full := f.followMinutes(ctx, it.Link, page); full != "" {
					doc.Body = full
				}
			}
		}
		if doc.Body == "" {
			doc.Body = strings.TrimSpace(it.Description)
		}
		out = append(out, doc)
	}
	return out, nil
}

var minutesLink = regexp.MustCompile(`/monetarypolicy/fomcminutes\d{8}\.htm`)

// followMinutes returns the text of the full FOMC minutes linked from a
// minutes press release, or "" if there is no such link.
func (f *Fed) followMinutes(ctx context.Context, pressURL string, page []byte) string {
	m := minutesLink.Find(page)
	if m == nil {
		return ""
	}
	u, err := url.Parse(pressURL)
	if err != nil {
		return ""
	}
	body, err := f.HTTP.Get(ctx, u.Scheme+"://"+u.Host+string(m))
	if err != nil {
		return ""
	}
	return HTMLText(body, "article")
}

var (
	beigeBookLink = regexp.MustCompile(`/monetarypolicy/beigebook(\d{6})\.htm`)
	lastUpdate    = regexp.MustCompile(`Last Update:\s*([A-Z][a-z]+ \d{1,2}, \d{4})`)
)

// LatestBeigeBook returns the most recent Beige Book report listed on the
// Fed's Beige Book index page. The Beige Book has no RSS feed; its release
// date is read from the report page's "Last Update" line.
func (f *Fed) LatestBeigeBook(ctx context.Context, indexURL string) (model.FedDoc, error) {
	idx, err := f.HTTP.Get(ctx, indexURL)
	if err != nil {
		return model.FedDoc{}, err
	}
	m := beigeBookLink.FindSubmatch(idx)
	if m == nil {
		return model.FedDoc{}, fmt.Errorf("no Beige Book link on %s", indexURL)
	}
	u, err := url.Parse(indexURL)
	if err != nil {
		return model.FedDoc{}, err
	}
	link := u.Scheme + "://" + u.Host + string(m[0])
	page, err := f.HTTP.Get(ctx, link)
	if err != nil {
		return model.FedDoc{}, err
	}
	month, err := time.Parse("200601", string(m[1]))
	if err != nil {
		return model.FedDoc{}, err
	}
	doc := model.FedDoc{
		Link:  link,
		Title: "Beige Book, " + month.Format("January 2006"),
		Kind:  "beige_book",
		Body:  HTMLText(page, "article"),
	}
	lu := lastUpdate.FindSubmatch(page)
	if lu == nil {
		return model.FedDoc{}, fmt.Errorf("no release date on %s", link)
	}
	if doc.Published, err = time.ParseInLocation("January 2, 2006", string(lu[1]), time.Local); err != nil {
		return model.FedDoc{}, err
	}
	// Released at 2:00 p.m. Eastern; noon local keeps it inside the right day.
	doc.Published = doc.Published.Add(12 * time.Hour)
	return doc, nil
}

// ExcludedTitle reports whether title contains any of the excluded phrases.
func ExcludedTitle(title string, exclude []string) bool {
	t := strings.ToLower(title)
	for _, e := range exclude {
		if e != "" && strings.Contains(t, strings.ToLower(e)) {
			return true
		}
	}
	return false
}

// ClassifyFedTitle maps a press release title to a document kind.
func ClassifyFedTitle(title string) string {
	t := strings.ToLower(title)
	switch {
	case strings.Contains(t, "beige book"):
		return "beige_book"
	case strings.Contains(t, "minutes"):
		return "minutes"
	case strings.Contains(t, "federal reserve issues fomc statement"), strings.Contains(t, "fomc statement"):
		return "statement"
	default:
		return "other"
	}
}
