package execution

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestControlTruncatedObservationInvalid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", int(HTTPMaxResponseBytes)+1)))
	}))
	defer srv.Close()
	f, a := localHTTPFixture(t, srv.URL)
	result, err := a.Run(t.Context(), f.record.TaskID, f.record.ID)
	if err == nil || result.Complete || len(result.Hops) != 1 || result.Hops[0].Complete || len(result.Hops[0].Body) != int(HTTPMaxResponseBytes) {
		t.Fatal("truncation was mislabelled complete", result, err)
	}
}

func TestControlObservationModelViewRedactsHeaders(t *testing.T) {
	raw := HTTPResult{Hops: []HTTPHop{{RawHeaders: map[string][]string{"Set-Cookie": {"private"}}, Headers: map[string][]string{"Location": {"http://localhost/?token=private"}, "Content-Type": {"text/plain"}}}}}
	view := RedactedHTTPResult(raw)
	if len(view.Hops[0].RawHeaders) != 0 || len(view.Hops[0].Headers) != 1 || len(raw.Hops[0].RawHeaders) != 1 {
		t.Fatal("model redaction lost boundary or mutated raw capture")
	}
}
