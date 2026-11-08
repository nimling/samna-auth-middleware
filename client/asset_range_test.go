package client

import (
	"net/http"
	"testing"
)

func TestAnAssetRangeAnswersTheBytesARangeRequestAsksFor(t *testing.T) {
	const etag = `"v1"`
	const modified = "Tue, 13 Oct 2026 10:00:00 GMT"
	cases := []struct {
		name    string
		header  map[string]string
		size    int64
		status  int
		start   int64
		length  int64
		content string
	}{
		{"no range answers the whole body", nil, 10, http.StatusOK, 0, 10, ""},
		{"a closed range", map[string]string{"Range": "bytes=2-5"}, 10, http.StatusPartialContent, 2, 4, "bytes 2-5/10"},
		{"an open range runs to the end", map[string]string{"Range": "bytes=7-"}, 10, http.StatusPartialContent, 7, 3, "bytes 7-9/10"},
		{"a suffix range takes the last bytes", map[string]string{"Range": "bytes=-3"}, 10, http.StatusPartialContent, 7, 3, "bytes 7-9/10"},
		{"a suffix longer than the asset takes all of it", map[string]string{"Range": "bytes=-50"}, 10, http.StatusPartialContent, 0, 10, "bytes 0-9/10"},
		{"an end past the asset stops at its last byte", map[string]string{"Range": "bytes=8-100"}, 10, http.StatusPartialContent, 8, 2, "bytes 8-9/10"},
		{"a start past the asset is unsatisfiable", map[string]string{"Range": "bytes=10-12"}, 10, http.StatusRequestedRangeNotSatisfiable, 0, 0, "bytes */10"},
		{"an empty suffix is unsatisfiable", map[string]string{"Range": "bytes=-0"}, 10, http.StatusRequestedRangeNotSatisfiable, 0, 0, "bytes */10"},
		{"any range of an empty asset is unsatisfiable", map[string]string{"Range": "bytes=0-"}, 0, http.StatusRequestedRangeNotSatisfiable, 0, 0, "bytes */0"},
		{"several ranges answer the whole body", map[string]string{"Range": "bytes=0-1,4-5"}, 10, http.StatusOK, 0, 10, ""},
		{"a unit other than bytes answers the whole body", map[string]string{"Range": "items=0-1"}, 10, http.StatusOK, 0, 10, ""},
		{"a malformed range answers the whole body", map[string]string{"Range": "bytes=5-2"}, 10, http.StatusOK, 0, 10, ""},
		{"an unknown size answers the whole body", map[string]string{"Range": "bytes=0-1"}, -1, http.StatusOK, 0, -1, ""},
		{"If-Range with the current ETag keeps the range", map[string]string{"Range": "bytes=0-1", "If-Range": etag}, 10, http.StatusPartialContent, 0, 2, "bytes 0-1/10"},
		{"If-Range with an older ETag answers the whole body", map[string]string{"Range": "bytes=0-1", "If-Range": `"v0"`}, 10, http.StatusOK, 0, 10, ""},
		{"If-Range with a weak ETag answers the whole body", map[string]string{"Range": "bytes=0-1", "If-Range": `W/"v1"`}, 10, http.StatusOK, 0, 10, ""},
		{"If-Range with the current Last-Modified keeps the range", map[string]string{"Range": "bytes=0-1", "If-Range": modified}, 10, http.StatusPartialContent, 0, 2, "bytes 0-1/10"},
		{"If-Range with an older date answers the whole body", map[string]string{"Range": "bytes=0-1", "If-Range": "Mon, 12 Oct 2026 10:00:00 GMT"}, 10, http.StatusOK, 0, 10, ""},
	}
	for _, c := range cases {
		header := http.Header{}
		for k, v := range c.header {
			header.Set(k, v)
		}
		r := GetAssetRange(header, c.size, etag, modified)
		if r.Status != c.status || r.Start != c.start || r.Length != c.length {
			t.Fatalf("%s: got status %d start %d length %d", c.name, r.Status, r.Start, r.Length)
		}
		if c.content != "" && r.GetContentRange() != c.content {
			t.Fatalf("%s: Content-Range %q, want %q", c.name, r.GetContentRange(), c.content)
		}
	}
}
