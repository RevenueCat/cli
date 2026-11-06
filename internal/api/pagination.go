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

// maxPageSize is the largest limit the API accepts; drains use it to minimise round trips.
const maxPageSize = 100

// listAll GETs path and follows next_page until drained, so callers that need
// the complete set (pickers, lookups) don't silently truncate at the first
// page. path may already carry a query string. pageSize > 0 sends it as limit;
// 0 omits it for endpoints without pagination params. Object/URL come from the
// first page; NextPage is always empty on return.
func listAll[T any](ctx context.Context, c *Client, path string, pageSize int) (*Page[T], error) {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	if pageSize > 0 {
		path += sep + "limit=" + strconv.Itoa(pageSize)
		sep = "&"
	}
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
		next = path + sep + "starting_after=" + url.QueryEscape(cursor)
	}
}
