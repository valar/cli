package api

import (
	"bufio"
	"bytes"
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

// follow streams a log, reconnecting when the connection is cut.
func (client *Client) follow(opts followOptions, w io.Writer) error {
	counter := &countingWriter{w: w}
	return client.reconnect(func() error {
		return client.streamOnce(opts.path(counter.bytes), counter)
	}, func() int { return counter.bytes }, opts.finite)
}

// reconnect runs once again whenever its stream is cut, until the stream ends
// for good. delivered reports how much has come through so far. A follow can
// idle for minutes at a time and intermediaries routinely drop idle
// connections, so a cut is expected rather than fatal.
func (client *Client) reconnect(once func() error, delivered func() int, finite bool) error {
	for tries := 0; ; {
		before := delivered()
		err := once()
		if err == nil && finite {
			return nil
		}
		if !retryable(err) {
			return err
		}
		if delivered() > before {
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

// openStream starts a streaming request and reports whether its response is
// framed. A response the server refused becomes an error.
func (client *Client) openStream(path string) (io.ReadCloser, bool, error) {
	req, err := http.NewRequest(http.MethodGet, client.Endpoint+path, nil)
	if err != nil {
		return nil, false, fmt.Errorf("client request: %w", err)
	}
	req.Header.Add("Authorization", "Bearer "+client.Token)

	resp, err := client.stream.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("submitting request: %w", err)
	}
	client.noteTokenExpiry(resp.Header)
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, false, fmt.Errorf("fetching response: %w", err)
		}
		return nil, false, client.responseError(resp, body)
	}
	contentType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	return resp.Body, err == nil && contentType == logStreamContentType, nil
}

// streamOnce runs a single streaming request, decoding framed responses and
// copying raw ones. A nil error means the server closed the stream.
func (client *Client) streamOnce(path string, w io.Writer) error {
	body, framed, err := client.openStream(path)
	if err != nil {
		return err
	}
	defer body.Close()
	if framed {
		return decodeFramedStream(body, w)
	}
	if _, err := io.Copy(w, body); err != nil && err != io.EOF {
		return fmt.Errorf("copying request: %w", err)
	}
	return nil
}

// streamRecordsOnce runs a single streaming request for log records.
func (client *Client) streamRecordsOnce(path string, emit func(LogRecord) error) error {
	body, framed, err := client.openStream(path)
	if err != nil {
		return err
	}
	defer body.Close()
	return decodeRecords(body, framed, emit)
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

// LogRecord is one line of a service log. Raw is the record exactly as the
// server sent it, unknown fields included; it is nil for a line the server
// sent as plain text.
type LogRecord struct {
	TS         string          `json:"ts,omitempty"`
	Deployment string          `json:"deployment,omitempty"`
	Stream     string          `json:"stream,omitempty"`
	Line       string          `json:"line"`
	Raw        json.RawMessage `json:"-"`
}

// recordLine is any line of a newline-delimited JSON log response: a record,
// a keepalive or, from a server without records, a frame of text.
type recordLine struct {
	TS         string  `json:"ts"`
	Deployment string  `json:"deployment"`
	Stream     string  `json:"stream"`
	Line       *string `json:"line"`
	Keepalive  bool    `json:"keepalive"`
	Data       []byte  `json:"data"`
}

// decodeRecords passes the records of a log response to emit. A server that
// predates records answers in plain text, or in text frames when framed;
// each line of that text becomes a record holding only the line. An unframed
// response is taken as records if its first non-empty line is one.
func decodeRecords(r io.Reader, framed bool, emit func(LogRecord) error) error {
	text := &TextRecords{Emit: emit}
	var pending []string
	decided := framed
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxLogFrameSize)
	for scanner.Scan() {
		line := scanner.Bytes()
		if !decided {
			if len(bytes.TrimSpace(line)) == 0 {
				pending = append(pending, string(line))
				continue
			}
			var probe recordLine
			framed = json.Unmarshal(line, &probe) == nil && probe.Line != nil
			decided = true
			if !framed {
				for _, blank := range pending {
					if _, err := text.Write([]byte(blank + "\n")); err != nil {
						return err
					}
				}
			}
		}
		if !framed {
			if _, err := text.Write(append(line, '\n')); err != nil {
				return err
			}
			continue
		}
		if len(line) == 0 {
			continue
		}
		var decoded recordLine
		if err := json.Unmarshal(line, &decoded); err != nil {
			return fmt.Errorf("decoding log record: %w", err)
		}
		switch {
		case decoded.Keepalive:
		case decoded.Line != nil:
			record := LogRecord{
				TS:         decoded.TS,
				Deployment: decoded.Deployment,
				Stream:     decoded.Stream,
				Line:       *decoded.Line,
				Raw:        bytes.Clone(line),
			}
			if err := emit(record); err != nil {
				return err
			}
		case decoded.Data != nil:
			if _, err := text.Write(decoded.Data); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading log stream: %w", err)
	}
	return text.Flush()
}

// TextRecords turns text into one record per line, passed to Emit. Flush
// emits a final line that has no line break.
type TextRecords struct {
	Emit func(LogRecord) error
	buf  []byte
}

func (tr *TextRecords) Write(b []byte) (int, error) {
	tr.buf = append(tr.buf, b...)
	for {
		i := bytes.IndexByte(tr.buf, '\n')
		if i < 0 {
			return len(b), nil
		}
		line := bytes.TrimSuffix(tr.buf[:i], []byte("\r"))
		if err := tr.Emit(LogRecord{Line: string(line)}); err != nil {
			return 0, err
		}
		tr.buf = tr.buf[i+1:]
	}
}

func (tr *TextRecords) Flush() error {
	if len(tr.buf) == 0 {
		return nil
	}
	line := string(tr.buf)
	tr.buf = nil
	return tr.Emit(LogRecord{Line: line})
}
