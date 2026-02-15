package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type Param struct {
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
}

type Method struct {
	Method      string  `json:"method"`
	Title       string  `json:"title,omitempty"`
	URL         string  `json:"url,omitempty"`
	APIVersion  string  `json:"api_version,omitempty"`
	Description string  `json:"description,omitempty"`
	Params      []Param `json:"params,omitempty"`
}

func main() {
	var base string
	var out string
	var maxPages int
	var timeout time.Duration
	var sleep time.Duration

	flag.StringVar(&base, "base", "https://www.uiscom.ru/academiya/spravochnyj-centr/dokumentatsiya-api/data_api/", "Base docs URL to crawl")
	flag.StringVar(&out, "out", "", "Output JSON file path")
	flag.IntVar(&maxPages, "max-pages", 5000, "Hard cap for pages to fetch (safety)")
	flag.DurationVar(&timeout, "timeout", 30*time.Second, "HTTP request timeout")
	flag.DurationVar(&sleep, "sleep", 50*time.Millisecond, "Delay between requests (politeness)")
	flag.Parse()

	if out == "" {
		fatal("missing --out")
	}

	baseURL, err := url.Parse(base)
	if err != nil {
		fatal("invalid --base: %v", err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		fatal("invalid --base: missing scheme/host")
	}
	// Ensure trailing slash for prefix comparisons.
	if !strings.HasSuffix(baseURL.Path, "/") {
		baseURL.Path += "/"
	}

	httpClient := &http.Client{Timeout: timeout}
	seen := map[string]bool{}
	queue := []string{baseURL.String()}

	methods := map[string]Method{}

	for len(queue) > 0 {
		if len(seen) >= maxPages {
			fatal("hit --max-pages=%d, aborting (seen=%d)", maxPages, len(seen))
		}

		cur := queue[0]
		queue = queue[1:]
		if seen[cur] {
			continue
		}
		seen[cur] = true

		doc, err := fetchHTML(httpClient, cur)
		if err != nil {
			// Keep going; docs sometimes have transient errors.
			_, _ = fmt.Fprintf(os.Stderr, "warn: fetch %s: %v\n", cur, err)
			continue
		}

		for _, m := range extractMethods(doc, cur) {
			existing, ok := methods[m.Method]
			if !ok {
				methods[m.Method] = m
				continue
			}
			methods[m.Method] = mergeMethod(existing, m)
		}

		for _, href := range extractLinks(doc) {
			next := resolveLink(cur, href)
			if next == "" {
				continue
			}
			u, err := url.Parse(next)
			if err != nil {
				continue
			}
			if u.Host != baseURL.Host || u.Scheme != baseURL.Scheme {
				continue
			}
			if !strings.HasPrefix(u.Path, baseURL.Path) {
				continue
			}
			if shouldSkipPath(u.Path) {
				continue
			}
			queue = append(queue, u.String())
		}

		// Avoid hammering the site.
		if sleep > 0 && len(queue) > 0 {
			time.Sleep(sleep)
		}
	}

	list := make([]Method, 0, len(methods))
	for _, m := range methods {
		list = append(list, m)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Method < list[j].Method })

	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		fatal("marshal: %v", err)
	}
	b = append(b, '\n')
	if err := os.WriteFile(out, b, 0o644); err != nil {
		fatal("write %s: %v", out, err)
	}
	_, _ = fmt.Fprintf(os.Stderr, "wrote %d methods to %s\n", len(list), out)
}

func fetchHTML(c *http.Client, u string) (*goquery.Document, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "uis-cli-spec-scraper/0.1 (+https://example.invalid)")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		snip := strings.TrimSpace(string(bytes.TrimSpace(b)))
		if snip != "" {
			return nil, fmt.Errorf("http %d (%s)", resp.StatusCode, snip)
		}
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	return doc, nil
}

func extractLinks(doc *goquery.Document) []string {
	out := make([]string, 0, 256)
	doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
		h, _ := s.Attr("href")
		h = strings.TrimSpace(h)
		if h == "" {
			return
		}
		if strings.HasPrefix(h, "mailto:") || strings.HasPrefix(h, "tel:") || strings.HasPrefix(h, "javascript:") {
			return
		}
		out = append(out, h)
	})
	return out
}

func resolveLink(from string, href string) string {
	// Drop fragment/query for canonicalization.
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "#") {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	u.Fragment = ""
	u.RawQuery = ""
	base, err := url.Parse(from)
	if err != nil {
		return ""
	}
	res := base.ResolveReference(u)
	return res.String()
}

func shouldSkipPath(path string) bool {
	// Skip obvious assets.
	ext := strings.ToLower(strings.TrimPrefix(filepathExt(path), "."))
	switch ext {
	case "css", "js", "png", "jpg", "jpeg", "webp", "svg", "gif", "ico", "pdf", "zip", "md", "yml", "yaml", "json":
		return true
	}
	return false
}

func filepathExt(p string) string {
	// tiny ext helper, without importing filepath (keeps go:embed paths untouched)
	i := strings.LastIndexByte(p, '.')
	if i < 0 {
		return ""
	}
	// ignore dot in directory names
	if strings.Contains(p[i:], "/") {
		return ""
	}
	return p[i:]
}

func extractMethods(doc *goquery.Document, pageURL string) []Method {
	title := strings.TrimSpace(doc.Find("h1").First().Text())

	var methodName string
	var desc string

	// Find the "Метод" info table: usually a table where first header cell is "Метод"
	// and the second header cell contains <code>.
	doc.Find("table").EachWithBreak(func(i int, t *goquery.Selection) bool {
		ths := t.Find("thead th")
		if ths.Length() >= 2 {
			h0 := strings.TrimSpace(ths.Eq(0).Text())
			if strings.EqualFold(h0, "Метод") {
				methodName = strings.TrimSpace(ths.Eq(1).Find("code").First().Text())
			}
		}
		if methodName != "" {
			// Description row: first cell "Описание"
			t.Find("tbody tr").Each(func(_ int, tr *goquery.Selection) {
				tds := tr.Find("td")
				if tds.Length() < 2 {
					return
				}
				k := strings.TrimSpace(tds.Eq(0).Text())
				if strings.EqualFold(k, "Описание") {
					desc = strings.TrimSpace(tds.Eq(1).Text())
				}
			})
			return false // stop
		}
		return true
	})

	if methodName == "" {
		return nil
	}

	params := extractRequestParams(doc)

	return []Method{{
		Method:      methodName,
		Title:       title,
		URL:         pageURL,
		Description: desc,
		Params:      params,
	}}
}

func extractRequestParams(doc *goquery.Document) []Param {
	var table *goquery.Selection

	// Locate the table after a heading containing "Параметры запроса".
	doc.Find("h2,h3").EachWithBreak(func(i int, h *goquery.Selection) bool {
		txt := strings.TrimSpace(h.Text())
		if strings.Contains(txt, "Параметры запроса") {
			// Next table in DOM.
			n := h.Next()
			for n.Length() > 0 {
				if goquery.NodeName(n) == "table" {
					table = n
					return false
				}
				n = n.Next()
			}
		}
		return true
	})

	if table == nil {
		return nil
	}

	// Expected columns: Название | Тип | Обязательный | Описание
	var out []Param
	table.Find("tbody tr").Each(func(i int, tr *goquery.Selection) {
		tds := tr.Find("td")
		if tds.Length() < 4 {
			return
		}
		name := strings.TrimSpace(tds.Eq(0).Find("code").First().Text())
		if name == "" {
			name = strings.TrimSpace(tds.Eq(0).Text())
		}
		typ := strings.TrimSpace(tds.Eq(1).Text())
		reqTxt := strings.ToLower(strings.TrimSpace(tds.Eq(2).Text()))
		required := reqTxt == "да" || reqTxt == "yes" || reqTxt == "true"
		desc := strings.TrimSpace(tds.Eq(3).Text())

		if name == "" {
			return
		}
		out = append(out, Param{
			Name:        name,
			Required:    required,
			Type:        typ,
			Description: desc,
		})
	})
	return out
}

func mergeMethod(a, b Method) Method {
	// Prefer richer metadata.
	if a.Title == "" && b.Title != "" {
		a.Title = b.Title
	}
	if a.URL == "" && b.URL != "" {
		a.URL = b.URL
	}
	if a.Description == "" && b.Description != "" {
		a.Description = b.Description
	}

	byName := map[string]Param{}
	for _, p := range a.Params {
		byName[p.Name] = p
	}
	for _, p := range b.Params {
		ex, ok := byName[p.Name]
		if !ok {
			byName[p.Name] = p
			continue
		}
		if !ex.Required && p.Required {
			ex.Required = true
		}
		if ex.Type == "" && p.Type != "" {
			ex.Type = p.Type
		}
		if ex.Description == "" && p.Description != "" {
			ex.Description = p.Description
		}
		byName[p.Name] = ex
	}

	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	a.Params = a.Params[:0]
	for _, n := range names {
		a.Params = append(a.Params, byName[n])
	}
	return a
}

func fatal(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
