package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func newTestClient(endpoint string) *Client {
	return &Client{Endpoint: endpoint, stream: &http.Client{}}
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

func TestLineWriter_HoldsBackPartialLines(t *testing.T) {
	var out bytes.Buffer
	lw := &lineWriter{w: &out}

	for _, chunk := range []string{"one\ntw", "o\nthree"} {
		if _, err := lw.Write([]byte(chunk)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	if got, want := out.String(), "one\ntwo\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if lw.lines != 2 {
		t.Errorf("got %d lines, want 2", lw.lines)
	}
	if err := lw.flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if got, want := out.String(), "one\ntwo\nthree"; got != want {
		t.Errorf("after flush got %q, want %q", got, want)
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
// idle connection: the stream is cut mid-line and must resume on the next line
// boundary without dropping or duplicating output.
func TestFollow_ResumesAfterCut(t *testing.T) {
	log := []string{"line1\n", "line2\n", "line3\n", "line4\n"}

	var mu sync.Mutex
	var skips []int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		skip, err := strconv.Atoi(r.URL.Query().Get("skip"))
		if err != nil {
			t.Errorf("bad skip %q: %v", r.URL.Query().Get("skip"), err)
			return
		}
		mu.Lock()
		attempt := len(skips)
		skips = append(skips, skip)
		mu.Unlock()

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if attempt == 0 {
			// Two whole lines plus a partial one, then a cut.
			fmt.Fprint(w, log[0], log[1], "lin")
			w.(http.Flusher).Flush()
			cutConnection(t, w)
			return
		}
		for _, line := range log[skip:] {
			fmt.Fprint(w, line)
		}
	}))
	defer srv.Close()

	var out bytes.Buffer
	client := newTestClient(srv.URL)
	err := client.follow(followOptions{
		finite: true,
		path: func(resumeAfter int) string {
			return "/logs?skip=" + strconv.Itoa(resumeAfter)
		},
	}, &out)
	if err != nil {
		t.Fatalf("follow: %v", err)
	}

	if got, want := out.String(), strings.Join(log, ""); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := []int{0, 2}; !equalInts(skips, want) {
		t.Errorf("got skips %v, want %v", skips, want)
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
		path: func(int) string { return "/logs?skip=0" },
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
		path:   func(int) string { return "/logs?skip=0" },
	}, &out); err != nil {
		t.Fatalf("follow: %v", err)
	}
	if got, want := out.String(), "hello\nworld\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
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
