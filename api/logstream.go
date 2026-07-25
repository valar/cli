package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
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
)

// Reconnect pacing, relaxed by tests.
var (
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

// countingWriter forwards bytes untouched and records how many have been
// delivered, so an interrupted follow can resume at the exact byte it stopped
// at. Bytes pass straight through: a log that writes without trailing newlines
// still appears as it arrives.
type countingWriter struct {
	w     io.Writer
	bytes int
}

func (cw *countingWriter) Write(b []byte) (int, error) {
	n, err := cw.w.Write(b)
	cw.bytes += n
	return n, err
}

// followOptions describes a resumable log stream.
type followOptions struct {
	// path builds the request path for a connection that resumes after the
	// given number of already-delivered log bytes.
	path func(resumeAfter int) string
	// finite is true when the server ends the stream on purpose once the log is
	// complete, as it does for a finished build.
	finite bool
}

// follow streams a log, reconnecting when the connection is cut. A follow can
// idle for minutes at a time and intermediaries routinely drop idle
// connections, so a cut is expected rather than fatal.
func (client *Client) follow(opts followOptions, w io.Writer) error {
	counter := &countingWriter{w: w}
	for tries := 0; ; {
		before := counter.bytes
		err := client.streamOnce(opts.path(counter.bytes), counter)
		if err == nil && opts.finite {
			return nil
		}
		if !retryable(err) {
			return err
		}
		if counter.bytes > before {
			tries = 0
		} else if tries++; tries >= maxReconnectTries {
			// Nothing is coming through; report it rather than spin or, worse,
			// exit successfully as though the log had ended.
			if err == nil {
				return fmt.Errorf("log stream closed %d times without delivering data", tries)
			}
			return err
		}
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
	if contentType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err == nil && contentType == logStreamContentType {
		return decodeFramedStream(resp.Body, w)
	}
	if _, err := io.Copy(w, resp.Body); err != nil && err != io.EOF {
		return fmt.Errorf("copying request: %w", err)
	}
	return nil
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
