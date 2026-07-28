package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Donking-36/container-log-platform/internal/middleware"
	"github.com/Donking-36/container-log-platform/internal/model"
	"github.com/Donking-36/container-log-platform/internal/service"
	"github.com/gin-gonic/gin"
)

type fakeLogQueryService struct {
	listResult service.ListLogsResult
	listErr    error
	listCalls  int
	listCtx    context.Context
	listInput  service.ListLogsInput

	getResult model.Log
	getErr    error
	getCalls  int
	getCtx    context.Context
	getID     int64
}

func (f *fakeLogQueryService) ListLogs(
	ctx context.Context,
	input service.ListLogsInput,
) (service.ListLogsResult, error) {
	f.listCalls++
	f.listCtx = ctx
	f.listInput = input

	return f.listResult, f.listErr
}

func (f *fakeLogQueryService) GetLog(
	ctx context.Context,
	id int64,
) (model.Log, error) {
	f.getCalls++
	f.getCtx = ctx
	f.getID = id

	return f.getResult, f.getErr
}

type queryHandlerContextKey struct{}

func TestNewQueryHandlerRejectsNilService(t *testing.T) {
	queryHandler, err := NewQueryHandler(nil)
	if err == nil {
		t.Fatal("NewQueryHandler(nil) returned nil error")
	}

	if queryHandler != nil {
		t.Fatalf("handler = %v, want nil", queryHandler)
	}
}

func TestQueryHandlerListLogsReturnsStableResponse(
	t *testing.T,
) {
	rawEvent := `{"not":"returned by list"}`
	loggedAt := time.Date(
		2026,
		time.July,
		24,
		10,
		0,
		0,
		0,
		time.UTC,
	)

	fakeService := &fakeLogQueryService{
		listResult: service.ListLogsResult{
			Logs: []model.Log{
				{
					ID:            7,
					EventID:       "v1:event-007",
					ContainerName: "container-a",
					Service:       "service-a",
					Level:         "INFO",
					Message:       "list message",
					Source:        "stdout",
					LoggedAt:      loggedAt,
					IngestedAt:    loggedAt.Add(time.Second),
					RawEvent:      &rawEvent,
				},
			},
			Page:     1,
			PageSize: 20,
			Total:    1,
		},
	}
	router := newTestQueryRouter(t, fakeService)

	recorder := performQueryRequest(
		router,
		"/api/v1/logs",
		"upstream-query-id",
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code = %d, want %d; body = %s",
			recorder.Code,
			http.StatusOK,
			recorder.Body.String(),
		)
	}

	if got := recorder.Header().Get("Content-Type"); !strings.Contains(
		got,
		"application/json",
	) {
		t.Fatalf("Content-Type = %q, want JSON", got)
	}

	var response listLogsResponse
	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatalf("decode list response: %v", err)
	}

	if len(response.Data) != 1 {
		t.Fatalf(
			"response data length = %d, want 1",
			len(response.Data),
		)
	}

	if response.Data[0].EventID != "v1:event-007" ||
		response.Data[0].Message != "list message" {
		t.Fatalf("response data = %+v", response.Data[0])
	}

	if response.Pagination != (paginationData{
		Page:     1,
		PageSize: 20,
		Total:    1,
	}) {
		t.Fatalf(
			"pagination = %+v",
			response.Pagination,
		)
	}

	if response.RequestID != "upstream-query-id" {
		t.Fatalf(
			"request ID = %q, want upstream-query-id",
			response.RequestID,
		)
	}
	assertResponseRequestID(
		t,
		recorder,
		response.RequestID,
	)

	var rawResponse struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&rawResponse,
	); err != nil {
		t.Fatalf("decode raw list response: %v", err)
	}

	if _, exists := rawResponse.Data[0]["raw_event"]; exists {
		t.Fatal("list response exposed raw_event")
	}

	if fakeService.listCalls != 1 {
		t.Fatalf(
			"service calls = %d, want 1",
			fakeService.listCalls,
		)
	}

	if fakeService.listInput.Page != nil ||
		fakeService.listInput.PageSize != nil {
		t.Fatal("handler replaced omitted pagination parameters")
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&envelope,
	); err != nil {
		t.Fatalf("decode list envelope: %v", err)
	}
	assertExactJSONKeys(
		t,
		envelope,
		"data",
		"pagination",
		"request_id",
	)

	var pagination map[string]json.RawMessage
	if err := json.Unmarshal(
		envelope["pagination"],
		&pagination,
	); err != nil {
		t.Fatalf("decode pagination: %v", err)
	}
	assertExactJSONKeys(
		t,
		pagination,
		"page",
		"page_size",
		"total",
	)

	var listData []map[string]json.RawMessage
	if err := json.Unmarshal(
		envelope["data"],
		&listData,
	); err != nil {
		t.Fatalf("decode list data: %v", err)
	}
	assertExactJSONKeys(
		t,
		listData[0],
		"id",
		"event_id",
		"source_event_id",
		"container_name",
		"container_id",
		"service",
		"level",
		"message",
		"source",
		"log_path",
		"log_offset",
		"logged_at",
		"ingested_at",
	)

	for key := range listData[0] {
		normalized := strings.ReplaceAll(
			strings.ToLower(key),
			"_",
			"",
		)
		if normalized == "rawevent" {
			t.Fatalf("list response exposed raw event as %q", key)
		}
	}

	if strings.Contains(
		recorder.Body.String(),
		"not returned by list",
	) {
		t.Fatal("list response leaked raw event content")
	}
}

func TestQueryHandlerListLogsReturnsEmptyArray(
	t *testing.T,
) {
	fakeService := &fakeLogQueryService{
		listResult: service.ListLogsResult{
			Logs:     nil,
			Page:     1,
			PageSize: 20,
			Total:    0,
		},
	}
	router := newTestQueryRouter(t, fakeService)

	recorder := performQueryRequest(
		router,
		"/api/v1/logs",
		"",
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code = %d, want %d; body = %s",
			recorder.Code,
			http.StatusOK,
			recorder.Body.String(),
		)
	}

	var response struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if string(response.Data) != "[]" {
		t.Fatalf(
			"data = %s, want []",
			response.Data,
		)
	}
}

func TestQueryHandlerListLogsAddsSafeRequestLogAttributes(
	t *testing.T,
) {
	rawEvent := `{"secret":"raw event"}`
	fakeService := &fakeLogQueryService{
		listResult: service.ListLogsResult{
			Logs: []model.Log{
				{
					ID:       1,
					EventID:  "v1:event-001",
					Message:  "secret log message",
					RawEvent: &rawEvent,
				},
			},
			Page:     1,
			PageSize: 20,
			Total:    1,
		},
	}

	queryHandler, err := NewQueryHandler(fakeService)
	if err != nil {
		t.Fatalf("create query handler: %v", err)
	}

	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(
		middleware.RequestID(),
		middleware.RequestLogger(logger),
	)
	router.GET("/api/v1/logs", queryHandler.ListLogs)

	recorder := performQueryRequest(
		router,
		"/api/v1/logs",
		"",
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code = %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}

	var entry map[string]any
	if err := json.Unmarshal(
		bytes.TrimSpace(output.Bytes()),
		&entry,
	); err != nil {
		t.Fatalf("decode request log: %v", err)
	}

	if entry["operation"] != "log_query" {
		t.Fatalf(
			"operation = %#v, want log_query",
			entry["operation"],
		)
	}

	if entry["result_count"] != float64(1) ||
		entry["total"] != float64(1) {
		t.Fatalf(
			"result_count = %#v, total = %#v; want 1 and 1",
			entry["result_count"],
			entry["total"],
		)
	}

	logText := output.String()
	if strings.Contains(logText, "secret log message") ||
		strings.Contains(logText, "raw event") {
		t.Fatal("request log leaked log event content")
	}
}

func TestQueryHandlerListLogsParsesParameters(
	t *testing.T,
) {
	fakeService := &fakeLogQueryService{
		listResult: service.ListLogsResult{
			Logs:     []model.Log{},
			Page:     2,
			PageSize: 25,
		},
	}
	router := newTestQueryRouter(t, fakeService)

	query := url.Values{
		"container": {" container-a "},
		"service":   {" service-a "},
		"level":     {"error"},
		"start":     {"2026-07-24T10:00:00+08:00"},
		"end":       {"2026-07-24T12:00:00+08:00"},
		"page":      {"2"},
		"page_size": {"25"},
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/logs?"+query.Encode(),
		nil,
	)
	request = request.WithContext(
		context.WithValue(
			request.Context(),
			queryHandlerContextKey{},
			"query-context",
		),
	)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code = %d, want %d; body = %s",
			recorder.Code,
			http.StatusOK,
			recorder.Body.String(),
		)
	}

	input := fakeService.listInput
	if input.ContainerName != " container-a " ||
		input.Service != " service-a " ||
		input.Level != "error" {
		t.Fatalf("service input = %+v", input)
	}

	if input.Page == nil || *input.Page != 2 {
		t.Fatalf("page = %v, want 2", input.Page)
	}

	if input.PageSize == nil || *input.PageSize != 25 {
		t.Fatalf("page size = %v, want 25", input.PageSize)
	}

	wantStart, _ := time.Parse(
		time.RFC3339,
		"2026-07-24T10:00:00+08:00",
	)
	wantEnd, _ := time.Parse(
		time.RFC3339,
		"2026-07-24T12:00:00+08:00",
	)

	if input.Start == nil || !input.Start.Equal(wantStart) {
		t.Fatalf("start = %v, want %v", input.Start, wantStart)
	}

	if input.End == nil || !input.End.Equal(wantEnd) {
		t.Fatalf("end = %v, want %v", input.End, wantEnd)
	}

	if fakeService.listCtx.Value(
		queryHandlerContextKey{},
	) != "query-context" {
		t.Fatal("handler did not preserve request context")
	}
}

func TestQueryHandlerListLogsRejectsInvalidParameters(
	t *testing.T,
) {
	tests := []struct {
		name string
		path string
	}{
		{
			name: "unsupported parameter",
			path: "/api/v1/logs?source=stdout",
		},
		{
			name: "duplicate parameter",
			path: "/api/v1/logs?page=1&page=2",
		},
		{
			name: "malformed query string",
			path: "/api/v1/logs?container=private;bad=x",
		},
		{
			name: "empty start",
			path: "/api/v1/logs?start=",
		},
		{
			name: "invalid start",
			path: "/api/v1/logs?start=not-a-time",
		},
		{
			name: "invalid end",
			path: "/api/v1/logs?end=not-a-time",
		},
		{
			name: "empty page",
			path: "/api/v1/logs?page=",
		},
		{
			name: "non-number page",
			path: "/api/v1/logs?page=one",
		},
		{
			name: "zero page",
			path: "/api/v1/logs?page=0",
		},
		{
			name: "negative page size",
			path: "/api/v1/logs?page_size=-1",
		},
		{
			name: "overflow page",
			path: "/api/v1/logs?page=999999999999999999999999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeService := &fakeLogQueryService{}
			router := newTestQueryRouter(t, fakeService)

			recorder := performQueryRequest(
				router,
				tt.path,
				"",
			)

			assertAPIError(
				t,
				recorder,
				http.StatusBadRequest,
				"INVALID_ARGUMENT",
			)

			if fakeService.listCalls != 0 {
				t.Fatalf(
					"service calls = %d, want 0",
					fakeService.listCalls,
				)
			}
		})
	}
}

func TestQueryHandlerListLogsRejectsInvalidPercentEncoding(
	t *testing.T,
) {
	fakeService := &fakeLogQueryService{}
	router := newTestQueryRouter(t, fakeService)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/logs",
		nil,
	)
	request.URL.RawQuery = "container=private&page=%ZZ"
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	assertAPIError(
		t,
		recorder,
		http.StatusBadRequest,
		"INVALID_ARGUMENT",
	)

	if fakeService.listCalls != 0 {
		t.Fatalf(
			"service calls = %d, want 0",
			fakeService.listCalls,
		)
	}
}

func TestQueryHandlerListLogsMapsServiceErrors(
	t *testing.T,
) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name: "invalid pagination",
			err: errors.Join(
				errors.New("wrapped pagination error"),
				service.ErrInvalidPagination,
			),
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_ARGUMENT",
		},
		{
			name: "invalid time range",
			err: errors.Join(
				errors.New("wrapped time range error"),
				service.ErrInvalidTimeRange,
			),
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_TIME_RANGE",
		},
		{
			name: "temporary database error",
			err: errors.Join(
				service.ErrTemporarilyUnavailable,
				errors.New("database busy at secret path"),
			),
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "SERVICE_UNAVAILABLE",
		},
		{
			name:       "internal error",
			err:        errors.New("database failed at secret path"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeService := &fakeLogQueryService{
				listErr: tt.err,
			}
			router := newTestQueryRouter(t, fakeService)

			recorder := performQueryRequest(
				router,
				"/api/v1/logs",
				"",
			)

			assertAPIError(
				t,
				recorder,
				tt.wantStatus,
				tt.wantCode,
			)

			if strings.Contains(
				recorder.Body.String(),
				"secret path",
			) {
				t.Fatal("response leaked service error")
			}
		})
	}
}

func TestQueryHandlerGetLogReturnsRawJSON(
	t *testing.T,
) {
	rawEvent := `{"nested":{"ok":true},"sequence":7}`
	sourceEventID := "source-event-007"
	containerID := "container-id-007"
	logPath := "/var/log/app.log"
	logOffset := int64(42)
	loggedAt := time.Date(
		2026,
		time.July,
		24,
		10,
		0,
		0,
		0,
		time.UTC,
	)

	fakeService := &fakeLogQueryService{
		getResult: model.Log{
			ID:            7,
			EventID:       "v1:event-007",
			SourceEventID: &sourceEventID,
			ContainerName: "container-a",
			ContainerID:   &containerID,
			Service:       "service-a",
			Level:         "INFO",
			Message:       "detail message",
			Source:        "stdout",
			LogPath:       &logPath,
			LogOffset:     &logOffset,
			LoggedAt:      loggedAt,
			IngestedAt:    loggedAt.Add(time.Second),
			RawEvent:      &rawEvent,
		},
	}
	router := newTestQueryRouter(t, fakeService)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/logs/7",
		nil,
	)
	request = request.WithContext(
		context.WithValue(
			request.Context(),
			queryHandlerContextKey{},
			"detail-context",
		),
	)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code = %d, want %d; body = %s",
			recorder.Code,
			http.StatusOK,
			recorder.Body.String(),
		)
	}

	var response struct {
		Data      map[string]json.RawMessage `json:"data"`
		RequestID string                     `json:"request_id"`
	}
	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatalf("decode detail response: %v", err)
	}

	wantKeys := []string{
		"id",
		"event_id",
		"source_event_id",
		"container_name",
		"container_id",
		"service",
		"level",
		"message",
		"source",
		"log_path",
		"log_offset",
		"logged_at",
		"ingested_at",
		"raw_event",
	}
	if len(response.Data) != len(wantKeys) {
		t.Fatalf(
			"detail field count = %d, want %d; data = %v",
			len(response.Data),
			len(wantKeys),
			response.Data,
		)
	}
	for _, key := range wantKeys {
		if _, exists := response.Data[key]; !exists {
			t.Fatalf("detail response is missing %q", key)
		}
	}

	var decodedRawEvent map[string]any
	if err := json.Unmarshal(
		response.Data["raw_event"],
		&decodedRawEvent,
	); err != nil {
		t.Fatalf("raw_event is not JSON: %v", err)
	}

	nested, ok := decodedRawEvent["nested"].(map[string]any)
	if !ok || nested["ok"] != true {
		t.Fatalf("raw_event = %#v", decodedRawEvent)
	}

	var eventID string
	if err := json.Unmarshal(
		response.Data["event_id"],
		&eventID,
	); err != nil {
		t.Fatalf("decode event_id: %v", err)
	}
	if eventID != "v1:event-007" {
		t.Fatalf("event_id = %q", eventID)
	}

	assertResponseRequestID(
		t,
		recorder,
		response.RequestID,
	)

	if fakeService.getCalls != 1 || fakeService.getID != 7 {
		t.Fatalf(
			"service calls = %d, ID = %d",
			fakeService.getCalls,
			fakeService.getID,
		)
	}

	if fakeService.getCtx.Value(
		queryHandlerContextKey{},
	) != "detail-context" {
		t.Fatal("handler did not preserve request context")
	}
}

func TestQueryHandlerGetLogReturnsNullRawEvent(
	t *testing.T,
) {
	fakeService := &fakeLogQueryService{
		getResult: model.Log{
			ID:      1,
			EventID: "v1:event-001",
		},
	}
	router := newTestQueryRouter(t, fakeService)

	recorder := performQueryRequest(
		router,
		"/api/v1/logs/1",
		"",
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code = %d, want %d; body = %s",
			recorder.Code,
			http.StatusOK,
			recorder.Body.String(),
		)
	}

	var response struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if string(response.Data["raw_event"]) != "null" {
		t.Fatalf(
			"raw_event = %s, want null",
			response.Data["raw_event"],
		)
	}
}

func TestQueryHandlerGetLogRejectsInvalidID(
	t *testing.T,
) {
	tests := []string{
		"not-a-number",
		"0",
		"-1",
		"9223372036854775808",
	}

	for _, id := range tests {
		t.Run(id, func(t *testing.T) {
			fakeService := &fakeLogQueryService{}
			router := newTestQueryRouter(t, fakeService)

			recorder := performQueryRequest(
				router,
				"/api/v1/logs/"+id,
				"",
			)

			assertAPIError(
				t,
				recorder,
				http.StatusBadRequest,
				"INVALID_ARGUMENT",
			)

			if fakeService.getCalls != 0 {
				t.Fatalf(
					"service calls = %d, want 0",
					fakeService.getCalls,
				)
			}
		})
	}
}

func TestQueryHandlerGetLogMapsServiceErrors(
	t *testing.T,
) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name: "not found",
			err: errors.Join(
				errors.New("wrapped missing log error"),
				service.ErrLogNotFound,
			),
			wantStatus: http.StatusNotFound,
			wantCode:   "LOG_NOT_FOUND",
		},
		{
			name: "invalid ID from service",
			err: errors.Join(
				errors.New("wrapped invalid ID"),
				service.ErrInvalidLogID,
			),
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_ARGUMENT",
		},
		{
			name: "temporary database error",
			err: errors.Join(
				service.ErrTemporarilyUnavailable,
				errors.New("database busy at secret path"),
			),
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "SERVICE_UNAVAILABLE",
		},
		{
			name:       "internal error",
			err:        errors.New("database failed at secret path"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeService := &fakeLogQueryService{
				getErr: tt.err,
			}
			router := newTestQueryRouter(t, fakeService)

			recorder := performQueryRequest(
				router,
				"/api/v1/logs/7",
				"",
			)

			assertAPIError(
				t,
				recorder,
				tt.wantStatus,
				tt.wantCode,
			)

			if strings.Contains(
				recorder.Body.String(),
				"secret path",
			) {
				t.Fatal("response leaked service error")
			}
		})
	}
}

func TestQueryHandlerGetLogRejectsInvalidStoredRawEvent(
	t *testing.T,
) {
	invalidRawEvent := `{"secret":`
	fakeService := &fakeLogQueryService{
		getResult: model.Log{
			ID:       7,
			EventID:  "v1:event-007",
			RawEvent: &invalidRawEvent,
		},
	}
	router := newTestQueryRouter(t, fakeService)

	recorder := performQueryRequest(
		router,
		"/api/v1/logs/7",
		"",
	)

	assertAPIError(
		t,
		recorder,
		http.StatusInternalServerError,
		"INTERNAL_ERROR",
	)

	if strings.Contains(
		recorder.Body.String(),
		"secret",
	) {
		t.Fatal("response leaked invalid raw_event")
	}
}

func assertExactJSONKeys(
	t *testing.T,
	object map[string]json.RawMessage,
	wantKeys ...string,
) {
	t.Helper()

	if len(object) != len(wantKeys) {
		t.Fatalf(
			"JSON key count = %d, want %d; object = %v",
			len(object),
			len(wantKeys),
			object,
		)
	}

	for _, key := range wantKeys {
		if _, exists := object[key]; !exists {
			t.Fatalf("JSON object is missing key %q", key)
		}
	}
}

func newTestQueryRouter(
	t *testing.T,
	queryService LogQueryService,
) *gin.Engine {
	t.Helper()

	gin.SetMode(gin.TestMode)

	queryHandler, err := NewQueryHandler(queryService)
	if err != nil {
		t.Fatalf("create query handler: %v", err)
	}

	router := gin.New()
	router.Use(middleware.RequestID())
	router.GET("/api/v1/logs", queryHandler.ListLogs)
	router.GET("/api/v1/logs/:id", queryHandler.GetLog)

	return router
}

func performQueryRequest(
	router http.Handler,
	path string,
	requestID string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(
		http.MethodGet,
		path,
		nil,
	)
	if requestID != "" {
		request.Header.Set(
			middleware.RequestIDHeader,
			requestID,
		)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	return recorder
}
