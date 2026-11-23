package collect

import (
	"bytes"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	spaceRun = regexp.MustCompile(`[ \t\x{00a0}]+`)
	blankRun = regexp.MustCompile(`\n\s*\n+`)
)

var blockTags = map[string]bool{
	"p": true, "div": true, "br": true, "tr": true, "li": true, "table": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"section": true, "article": true, "ul": true, "ol": true,
}

// HTMLText converts an HTML document to plain text, keeping table cells
// separated by " | " and block elements on their own lines so figures in
// financial tables stay distinguishable. If selectorID is non-empty and an
// element with that id exists, only its subtree is converted.
func HTMLText(doc []byte, selectorID string) string {
	root, err := html.Parse(bytes.NewReader(doc))
	if err != nil {
		return ""
	}
	if selectorID != "" {
		if n := findByID(root, selectorID); n != nil {
			root = n
		}
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
			return
		case html.ElementNode:
			switch n.Data {
			case "script", "style", "head", "noscript":
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode {
			switch {
			case n.Data == "td" || n.Data == "th":
				b.WriteString(" | ")
			case blockTags[n.Data]:
				b.WriteString("\n")
			}
		}
	}
	walk(root)

	lines := strings.Split(b.String(), "\n")
	for i, l := range lines {
		l = spaceRun.ReplaceAllString(l, " ")
		l = strings.TrimSpace(l)
		l = strings.TrimSuffix(l, " |")
		l = strings.TrimPrefix(l, "| ")
		if strings.Trim(l, "| ") == "" {
			l = ""
		}
		lines[i] = l
	}
	out := strings.Join(lines, "\n")
	out = blankRun.ReplaceAllString(out, "\n\n")
	return strings.TrimSpace(out)
}

func findByID(n *html.Node, id string) *html.Node {
	if n.Type == html.ElementNode {
		for _, a := range n.Attr {
			if a.Key == "id" && a.Val == id {
				return n
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := findByID(c, id); f != nil {
			return f
		}
	}
	return nil
}
