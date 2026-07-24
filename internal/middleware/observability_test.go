package middleware

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestLoggerWritesStructuredFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var output bytes.Buffer
	logger := slog.New(
		slog.NewJSONHandler(&output, nil),
	).With("service", "api")

	router := gin.New()
	router.Use(
		RequestID(),
		RequestLogger(logger),
	)
	router.GET("/items/:id", func(c *gin.Context) {
		AddLogAttributes(
			c,
			slog.String("event_type", "test_request"),
		)
		_ = c.Error(errors.New("test warning"))
		c.Status(http.StatusOK)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/items/42",
		nil,
	)

	router.ServeHTTP(recorder, request)

	entry := findLogEntry(
		t,
		decodeLogEntries(t, output.String()),
		"HTTP request completed",
	)

	assertLogString(
		t,
		entry,
		"request_id",
		recorder.Header().Get(RequestIDHeader),
	)
	assertLogString(t, entry, "method", http.MethodGet)
	assertLogString(t, entry, "path", "/items/:id")
	assertLogNumber(t, entry, "status", http.StatusOK)
	assertLogString(t, entry, "event", "http_request")
	assertLogString(t, entry, "service", "api")
	assertLogString(t, entry, "event_type", "test_request")
	assertLogString(t, entry, "level", "WARN")

	if _, exists := entry["latency_ms"]; !exists {
		t.Fatal("structured log is missing latency_ms")
	}

	errorText, ok := entry["error"].(string)
	if !ok || !strings.Contains(errorText, "test warning") {
		t.Fatalf(
			"error field = %#v, want test warning",
			entry["error"],
		)
	}
}

func TestRecoveryReturnsJSONAndLogsSameRequestID(
	t *testing.T,
) {
	gin.SetMode(gin.TestMode)

	var output bytes.Buffer
	logger := slog.New(
		slog.NewJSONHandler(&output, nil),
	).With("service", "api")

	router := gin.New()
	router.Use(
		RequestID(),
		RequestLogger(logger),
		Recovery(logger),
	)
	router.POST("/panic", func(c *gin.Context) {
		panic("controlled panic")
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/panic",
		strings.NewReader("secret request body"),
	)
	request.Header.Set(
		"Authorization",
		"Bearer secret-token",
	)

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf(
			"status code = %d, want %d",
			recorder.Code,
			http.StatusInternalServerError,
		)
	}

	var response ErrorResponse

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatalf("decode recovery response: %v", err)
	}

	if response.Error.Code != "INTERNAL_ERROR" {
		t.Fatalf(
			"error code = %q, want %q",
			response.Error.Code,
			"INTERNAL_ERROR",
		)
	}

	headerRequestID := recorder.Header().Get(RequestIDHeader)
	if response.RequestID == "" ||
		response.RequestID != headerRequestID {
		t.Fatalf(
			"body request ID = %q, header request ID = %q",
			response.RequestID,
			headerRequestID,
		)
	}

	entries := decodeLogEntries(t, output.String())
	panicEntry := findLogEntry(
		t,
		entries,
		"HTTP panic recovered",
	)
	requestEntry := findLogEntry(
		t,
		entries,
		"HTTP request completed",
	)

	assertLogString(
		t,
		panicEntry,
		"request_id",
		headerRequestID,
	)
	assertLogString(
		t,
		requestEntry,
		"request_id",
		headerRequestID,
	)
	assertLogString(t, requestEntry, "level", "ERROR")
	assertLogString(
		t,
		requestEntry,
		"error_code",
		"INTERNAL_ERROR",
	)
	assertLogNumber(
		t,
		requestEntry,
		"status",
		http.StatusInternalServerError,
	)

	logText := output.String()
	if strings.Contains(logText, "secret request body") {
		t.Fatal("structured log leaked request body")
	}

	if strings.Contains(logText, "secret-token") {
		t.Fatal("structured log leaked authorization header")
	}
}

func decodeLogEntries(
	t *testing.T,
	logText string,
) []map[string]any {
	t.Helper()

	var entries []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(logText))

	for scanner.Scan() {
		var entry map[string]any

		if err := json.Unmarshal(
			scanner.Bytes(),
			&entry,
		); err != nil {
			t.Fatalf(
				"decode structured log %q: %v",
				scanner.Text(),
				err,
			)
		}

		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("scan structured logs: %v", err)
	}

	return entries
}

func findLogEntry(
	t *testing.T,
	entries []map[string]any,
	message string,
) map[string]any {
	t.Helper()

	for _, entry := range entries {
		if entry["msg"] == message {
			return entry
		}
	}

	t.Fatalf("log entry %q was not found", message)
	return nil
}

func assertLogString(
	t *testing.T,
	entry map[string]any,
	key string,
	want string,
) {
	t.Helper()

	got, ok := entry[key].(string)
	if !ok || got != want {
		t.Fatalf(
			"log field %q = %#v, want %q",
			key,
			entry[key],
			want,
		)
	}
}

func assertLogNumber(
	t *testing.T,
	entry map[string]any,
	key string,
	want int,
) {
	t.Helper()

	got, ok := entry[key].(float64)
	if !ok || got != float64(want) {
		t.Fatalf(
			"log field %q = %#v, want %d",
			key,
			entry[key],
			want,
		)
	}
}
