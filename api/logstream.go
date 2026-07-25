package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// logStreamContentType marks a response using the framed log stream
	// protocol, where the server interleaves keepalives with log data.
	logStreamContentType = "application/x-ndjson"
	// keepaliveInterval is how often the server is asked to send a heartbeat on
	// an otherwise idle follow stream.
	keepaliveInterval = 20 * time.Second
	// maxLogFrameSize bounds a single framed message.
	maxLogFrameSize = 1 << 20

	reconnectBaseDelay = 250 * time.Millisecond
	reconnectMaxDelay  = 5 * time.Second
	// maxReconnectTries bounds consecutive reconnects that deliver nothing, so
	// a log that can no longer be read ends instead of looping.
	maxReconnectTries = 8
)

// logFrame mirrors the server's framed log stream protocol. Data is
// base64-encoded by encoding/json.
type logFrame struct {
	Data      []byte `json:"data,omitempty"`
	Keepalive bool   `json:"keepalive,omitempty"`
}

// lineWriter forwards whole log lines to w and counts them, holding back a
// trailing partial line. Counting complete lines lets an interrupted follow
// resume on a line boundary without duplicating or dropping output.
type lineWriter struct {
	w       io.Writer
	pending []byte
	lines   int
}

func (lw *lineWriter) Write(b []byte) (int, error) {
	lw.pending = append(lw.pending, b...)
	end := bytes.LastIndexByte(lw.pending, '\n')
	if end < 0 {
		return len(b), nil
	}
	complete := lw.pending[:end+1]
	if _, err := lw.w.Write(complete); err != nil {
		return 0, err
	}
	lw.lines += bytes.Count(complete, []byte{'\n'})
	lw.pending = append(lw.pending[:0], lw.pending[end+1:]...)
	return len(b), nil
}

// discardPartial drops the buffered partial line, which the server resends from
// the last complete line when the stream resumes.
func (lw *lineWriter) discardPartial() {
	lw.pending = lw.pending[:0]
}

// flush writes out a trailing line that never got a newline.
func (lw *lineWriter) flush() error {
	if len(lw.pending) == 0 {
		return nil
	}
	if _, err := lw.w.Write(lw.pending); err != nil {
		return err
	}
	lw.pending = lw.pending[:0]
	return nil
}

// followOptions describes a resumable log stream.
type followOptions struct {
	// path builds the request path for a connection that resumes after the
	// given number of already-delivered lines.
	path func(resumeAfter int) string
	// finite is true when the server ends the stream on purpose once the log is
	// complete, as it does for a finished build.
	finite bool
	// skipped is the number of lines the caller asked to skip up front.
	skipped int
}

// follow streams a log, reconnecting when the connection is cut. A follow can
// idle for minutes at a time and intermediaries routinely drop idle
// connections, so a cut is expected rather than fatal.
func (client *Client) follow(opts followOptions, w io.Writer) error {
	lw := &lineWriter{w: w, lines: opts.skipped}
	defer lw.flush()

	for tries := 0; ; {
		before := lw.lines
		err := client.streamOnce(opts.path(lw.lines), lw)
		if err == nil && opts.finite {
			return nil
		}
		if !retryable(err) {
			return err
		}
		if lw.lines > before {
			tries = 0
		} else if tries++; tries >= maxReconnectTries {
			// Nothing is coming through; report the cut rather than spin.
			return err
		}
		// A cut mid-line leaves a partial the server resends on resume.
		lw.discardPartial()
		time.Sleep(reconnectDelay(tries))
	}
}

// retryable reports whether a stream ended in a way a reconnect can recover
// from. A response the server rejected outright will be rejected again.
func retryable(err error) bool {
	var statusErr Error
	return !errors.As(err, &statusErr)
}

func reconnectDelay(tries int) time.Duration {
	delay := reconnectBaseDelay << tries
	if delay > reconnectMaxDelay || delay <= 0 {
		return reconnectMaxDelay
	}
	return delay
}

// streamOnce runs a single streaming request, decoding framed responses and
// copying raw ones. A nil error means the server closed the stream.
func (client *Client) streamOnce(path string, w io.Writer) error {
	req, err := http.NewRequest(http.MethodGet, client.Endpoint+path, nil)
	if err != nil {
		return fmt.Errorf("client request: %w", err)
	}
	req.Header.Add("Authorization", "Bearer "+client.Token)

	resp, err := client.stream.Do(req)
	if err != nil {
		return fmt.Errorf("submitting request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("fetching response: %w", err)
		}
		return Error{
			StatusCode:  resp.StatusCode,
			ServerError: parseErrorResponse(body),
		}
	}
	if mediaType(resp.Header.Get("Content-Type")) == logStreamContentType {
		return decodeFramedStream(resp.Body, w)
	}
	if _, err := io.Copy(w, resp.Body); err != nil && err != io.EOF {
		return fmt.Errorf("copying request: %w", err)
	}
	return nil
}

func mediaType(contentType string) string {
	if idx := bytes.IndexByte([]byte(contentType), ';'); idx >= 0 {
		return contentType[:idx]
	}
	return contentType
}

// decodeFramedStream writes framed log payloads to w and drops keepalives.
func decodeFramedStream(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxLogFrameSize)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var frame logFrame
		if err := json.Unmarshal(line, &frame); err != nil {
			return fmt.Errorf("decoding log frame: %w", err)
		}
		if frame.Keepalive {
			continue
		}
		if _, err := w.Write(frame.Data); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading log stream: %w", err)
	}
	return nil
}
