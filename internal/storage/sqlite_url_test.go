package storage

import (
	"net/url"
	"testing"
)

func TestSQLiteFileURLPath(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"/tmp/a #.db", "file:///tmp/a%20%23.db"},
		{"C:/exam data/a #.db", "file:///C:/exam%20data/a%20%23.db"},
		{"//server/share/exam.db", "file:////server/share/exam.db"},
	} {
		u := url.URL{Scheme: "file", Path: sqliteURLPath(tc.path)}
		if got := u.String(); got != tc.want {
			t.Errorf("%q = %q; want %q", tc.path, got, tc.want)
		}
	}
}
