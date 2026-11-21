package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestClient(endpoint string) *Client {
	return &Client{Endpoint: endpoint, stream: &http.Client{}}
}

// withFastReconnects shortens the reconnect backoff for the duration of a test.
func withFastReconnects(t *testing.T) func() {
	t.Helper()
	base, max := reconnectBaseDelay, reconnectMaxDelay
	reconnectBaseDelay, reconnectMaxDelay = time.Millisecond, 2*time.Millisecond
	return func() { reconnectBaseDelay, reconnectMaxDelay = base, max }
}

// cutConnection ends a response without a terminating chunk, the way an idle
// connection reaper does.
func cutConnection(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		t.Error("response writer is not hijackable")
		return
	}
	conn, _, err := hijacker.Hijack()
	if err != nil {
		t.Errorf("hijack: %v", err)
		return
	}
	conn.Close()
}

// TestCountingWriter_PassesBytesStraightThrough guards against reintroducing
// line buffering: output with no trailing newline must still appear as it
// arrives, not be withheld until a newline shows up.
func TestCountingWriter_PassesBytesStraightThrough(t *testing.T) {
	var out bytes.Buffer
	cw := &countingWriter{w: &out}

	for _, chunk := range []string{"progress: 10%\r", "progress: 20%\r"} {
		if _, err := cw.Write([]byte(chunk)); err != nil {
			t.Fatalf("write: %v", err)
		}
		if out.Len() == 0 {
			t.Fatal("writer withheld output that contained no newline")
		}
	}

	if got, want := out.String(), "progress: 10%\rprogress: 20%\r"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if cw.bytes != out.Len() {
		t.Errorf("got %d bytes counted, want %d", cw.bytes, out.Len())
	}
}

func TestDecodeFramedStream_DropsKeepalives(t *testing.T) {
	var body bytes.Buffer
	for _, frame := range []logFrame{
		{Data: []byte("hello\n")},
		{Keepalive: true},
		{Keepalive: true},
		{Data: []byte("world\n")},
	} {
		if err := json.NewEncoder(&body).Encode(frame); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}

	var out bytes.Buffer
	if err := decodeFramedStream(&body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, want := out.String(), "hello\nworld\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestDecodeFramedStream_WireFormat pins the bytes the API emits. The matching
// test lives in the platform repository; change both together.
func TestDecodeFramedStream_WireFormat(t *testing.T) {
	body := strings.NewReader(`{"data":"aGVsbG8K"}` + "\n" + `{"keepalive":true}` + "\n")

	var out bytes.Buffer
	if err := decodeFramedStream(body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, want := out.String(), "hello\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFollow_ResumesAfterCut is the regression test for log tails dying on an
// idle connection: the stream is cut mid-line and must resume at the exact byte
// it stopped at, without dropping or duplicating output.
func TestFollow_ResumesAfterCut(t *testing.T) {
	const log = "line1\nline2\nline3\nline4\n"

	var mu sync.Mutex
	var offsets []int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		if err != nil || offset < 0 || offset > len(log) {
			t.Errorf("bad offset %q: %v", r.URL.Query().Get("offset"), err)
			return
		}
		mu.Lock()
		attempt := len(offsets)
		offsets = append(offsets, offset)
		mu.Unlock()

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if attempt == 0 {
			// Stop mid-line, then cut without a terminating chunk.
			fmt.Fprint(w, log[:15])
			w.(http.Flusher).Flush()
			cutConnection(t, w)
			return
		}
		fmt.Fprint(w, log[offset:])
	}))
	defer srv.Close()

	var out bytes.Buffer
	client := newTestClient(srv.URL)
	err := client.follow(followOptions{
		finite: true,
		path: func(resumeAfter int) string {
			return "/logs?offset=" + strconv.Itoa(resumeAfter)
		},
	}, &out)
	if err != nil {
		t.Fatalf("follow: %v", err)
	}

	if got := out.String(); got != log {
		t.Errorf("got %q, want %q", got, log)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := []int{0, 15}; !equalInts(offsets, want) {
		t.Errorf("got offsets %v, want %v", offsets, want)
	}
}

// TestFollow_ReportsRepeatedEmptyCloses covers a server that keeps closing the
// stream without sending anything: the command must fail loudly rather than
// exit successfully as though the log had ended.
func TestFollow_ReportsRepeatedEmptyCloses(t *testing.T) {
	defer withFastReconnects(t)()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	err := client.follow(followOptions{
		path: func(resumeAfter int) string { return "/logs?offset=" + strconv.Itoa(resumeAfter) },
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected an error after repeated empty closes")
	}
}

func TestFollow_DoesNotRetryRejectedRequests(t *testing.T) {
	var mu sync.Mutex
	var requests int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"service not found"}`)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	err := client.follow(followOptions{
		path: func(int) string { return "/logs?offset=0" },
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected an error")
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 1 {
		t.Errorf("got %d requests, want 1", requests)
	}
}

func TestFollow_DecodesFramedResponses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", logStreamContentType)
		enc := json.NewEncoder(w)
		for _, frame := range []logFrame{
			{Keepalive: true},
			{Data: []byte("hello\n")},
			{Keepalive: true},
			{Data: []byte("world\n")},
		} {
			if err := enc.Encode(frame); err != nil {
				t.Errorf("encode: %v", err)
				return
			}
		}
	}))
	defer srv.Close()

	var out bytes.Buffer
	client := newTestClient(srv.URL)
	if err := client.follow(followOptions{
		finite: true,
		path:   func(int) string { return "/logs?offset=0" },
	}, &out); err != nil {
		t.Fatalf("follow: %v", err)
	}
	if got, want := out.String(), "hello\nworld\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestStreamServiceLogRecords_FollowReanchors covers the missing resume
// cursor: a cut record stream reconnects at the end of the newest log.
func TestStreamServiceLogRecords_FollowReanchors(t *testing.T) {
	var mu sync.Mutex
	var queries []url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempt := len(queries)
		queries = append(queries, r.URL.Query())
		mu.Unlock()
		w.Header().Set("Content-Type", logStreamContentType)
		switch attempt {
		case 0:
			fmt.Fprintln(w, `{"keepalive":true}`)
			fmt.Fprintln(w, `{"deployment":"1","stream":"output","line":"first"}`)
			w.(http.Flusher).Flush()
			cutConnection(t, w)
		case 1:
			fmt.Fprintln(w, `{"deployment":"2","stream":"output","line":"second"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	var lines []string
	client := newTestClient(srv.URL)
	err := client.StreamServiceLogRecords("p", "s", LogOptions{Follow: true, All: true}, func(record LogRecord) error {
		lines = append(lines, record.Deployment+":"+record.Line)
		return nil
	})
	var statusErr Error
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusNotFound {
		t.Fatalf("got %v, want the 404 that ends the test stream", err)
	}
	if want := []string{"1:first", "2:second"}; strings.Join(lines, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", lines, want)
	}
	mu.Lock()
	defer mu.Unlock()
	for i, want := range []url.Values{
		{"format": {"json"}, "all": {"true"}, "follow": {"true"}, "keepalive": {keepaliveInterval.String()}},
		{"format": {"json"}, "seek": {"end"}, "follow": {"true"}, "keepalive": {keepaliveInterval.String()}},
	} {
		if !reflect.DeepEqual(queries[i], want) {
			t.Errorf("request %d: got %v, want %v", i, queries[i], want)
		}
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
