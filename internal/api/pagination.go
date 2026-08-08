package api

import (
	"net/url"
	"strconv"
)

// ListOptions are the shared cursor-pagination params for list endpoints.
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
