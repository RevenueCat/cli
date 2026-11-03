package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ListOptions are shared cursor-pagination params.
type ListOptions struct {
	Limit         int
	StartingAfter string
}

func (o *ListOptions) query() string {
	if o == nil {
		return ""
	}
	v := url.Values{}
	if o.Limit > 0 {
		v.Set("limit", strconv.Itoa(o.Limit))
	}
	if o.StartingAfter != "" {
		v.Set("starting_after", o.StartingAfter)
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

// NextCursor is the starting_after cursor for the next page, read from the
// next_page URL. Empty on the last page.
func (p Page[T]) NextCursor() string {
	if p.NextPage == "" {
		return ""
	}
	u, err := url.Parse(p.NextPage)
	if err != nil {
		return ""
	}
	return u.Query().Get("starting_after")
}

// listAll GETs path and follows next_page until drained, so callers that need
// the complete set (pickers, lookups) don't silently truncate at the first
// page. path may already carry a query string. Object/URL come from the first
// page; NextPage is always empty on return.
func listAll[T any](ctx context.Context, c *Client, path string) (*Page[T], error) {
	// Non-nil so an empty collection still encodes as "items": [].
	out := Page[T]{Items: []T{}}
	next := path
	for {
		var page Page[T]
		if err := c.do(ctx, http.MethodGet, next, nil, &page); err != nil {
			return nil, err
		}
		if out.Object == "" {
			out.Object, out.URL = page.Object, page.URL
		}
		out.Items = append(out.Items, page.Items...)
		cursor := page.NextCursor()
		if cursor == "" {
			return &out, nil
		}
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		next = path + sep + "starting_after=" + url.QueryEscape(cursor)
	}
}
