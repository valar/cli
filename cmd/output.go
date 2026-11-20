package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/valar/cli/api"
	"github.com/valar/cli/config"
)

// outputJSON is set by --json or VALAR_OUTPUT=json.
var outputJSON bool

// stdout receives command results; tests replace it.
var stdout io.Writer = os.Stdout

const exitCodeHelp = `Exit codes:
  0  success
  1  failed: the operation ran and failed, such as a build or deployment
  2  invalid: bad arguments, flags or configuration
  3  not found
  4  not allowed: missing or rejected credentials or permissions
  5  conflict with the current state
  6  unavailable: the server could not be reached or answered with an error`

// errorKind classifies a failure. Its value is the exit code.
type errorKind int

const (
	kindFailed errorKind = iota + 1
	kindInvalid
	kindNotFound
	kindNotAllowed
	kindConflict
	kindUnavailable
)

func (k errorKind) String() string {
	switch k {
	case kindInvalid:
		return "invalid"
	case kindNotFound:
		return "notFound"
	case kindNotAllowed:
		return "notAllowed"
	case kindConflict:
		return "conflict"
	case kindUnavailable:
		return "unavailable"
	default:
		return "failed"
	}
}

// cliError carries the kind of a failure. A silent error has already been
// reported in human output.
type cliError struct {
	kind   errorKind
	err    error
	silent bool
}

func (e *cliError) Error() string { return e.err.Error() }
func (e *cliError) Unwrap() error { return e.err }

func newError(kind errorKind, format string, args ...any) error {
	return &cliError{kind: kind, err: fmt.Errorf(format, args...)}
}

func withKind(kind errorKind, err error) error {
	if err == nil {
		return nil
	}
	return &cliError{kind: kind, err: err}
}

// classify finds the kind of err, or returns fallback for an untyped error.
func classify(err error, fallback errorKind) errorKind {
	var cliErr *cliError
	var apiErr api.Error
	var cfgErr *config.Error
	var urlErr *url.Error
	switch {
	case errors.As(err, &cliErr):
		return cliErr.kind
	case errors.As(err, &cfgErr):
		return kindInvalid
	case errors.As(err, &apiErr):
		return statusKind(apiErr.StatusCode)
	case errors.As(err, &urlErr):
		return kindUnavailable
	}
	return fallback
}

func statusKind(status int) errorKind {
	switch {
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		return kindInvalid
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return kindNotAllowed
	case status == http.StatusNotFound || status == http.StatusGone:
		return kindNotFound
	case status == http.StatusConflict:
		return kindConflict
	case status == http.StatusTooManyRequests || status >= 500:
		return kindUnavailable
	default:
		return kindFailed
	}
}

// reportError writes err to w, as JSON in JSON mode, and returns its exit
// code. printed means the human message was already written.
func reportError(w io.Writer, err error, fallback errorKind, printed bool) int {
	kind := classify(err, fallback)
	var cliErr *cliError
	silent := printed || errors.As(err, &cliErr) && cliErr.silent
	switch {
	case outputJSON:
		var doc struct {
			Error struct {
				Kind    string `json:"kind"`
				Message string `json:"message"`
			} `json:"error"`
		}
		doc.Error.Kind = kind.String()
		doc.Error.Message = err.Error()
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		enc.Encode(doc)
	case !silent:
		fmt.Fprintln(w, err)
	}
	return int(kind)
}

func exitWithError(err error, fallback errorKind, printed bool) {
	os.Exit(reportError(os.Stderr, err, fallback, printed))
}

// jsonRequested reports whether args or VALAR_OUTPUT ask for JSON output,
// before cobra has parsed the flags.
func jsonRequested(args []string, env string) bool {
	requested := env == "json"
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--json" {
			requested = true
		} else if value, ok := strings.CutPrefix(arg, "--json="); ok {
			if b, err := strconv.ParseBool(value); err == nil {
				requested = b
			}
		}
	}
	return requested
}

// result prints v to stdout as JSON in JSON mode and calls human otherwise.
func result(v any, human func(w io.Writer) error) error {
	return emit(stdout, v, human)
}

func emit(w io.Writer, v any, human func(w io.Writer) error) error {
	if outputJSON {
		return writeJSON(w, v)
	}
	return human(w)
}

// writeJSON writes v as one indented document. A nil slice becomes [].
func writeJSON(w io.Writer, v any) error {
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Slice && rv.IsNil() {
		v = reflect.MakeSlice(rv.Type(), 0, 0).Interface()
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// writeJSONLine writes v as a single line, for streams.
func writeJSONLine(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// logLine is a line of service or batch output in a JSON stream.
type logLine struct {
	Content string `json:"content"`
}

// jsonLineWriter turns raw output into one {"content": ...} object per line.
// Flush writes a final line that has no line break.
type jsonLineWriter struct {
	w   io.Writer
	buf []byte
}

func (lw *jsonLineWriter) Write(b []byte) (int, error) {
	lw.buf = append(lw.buf, b...)
	for {
		i := bytes.IndexByte(lw.buf, '\n')
		if i < 0 {
			return len(b), nil
		}
		line := bytes.TrimSuffix(lw.buf[:i], []byte("\r"))
		if err := writeJSONLine(lw.w, logLine{Content: string(line)}); err != nil {
			return 0, err
		}
		lw.buf = lw.buf[i+1:]
	}
}

func (lw *jsonLineWriter) Flush() error {
	if len(lw.buf) == 0 {
		return nil
	}
	line := string(lw.buf)
	lw.buf = nil
	return writeJSONLine(lw.w, logLine{Content: line})
}

// logOutput returns where raw log output goes, and a flush to call once it
// ends.
func logOutput() (io.Writer, func() error) {
	if !outputJSON {
		return stdout, func() error { return nil }
	}
	lw := &jsonLineWriter{w: stdout}
	return lw, lw.Flush
}
