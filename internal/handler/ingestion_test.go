package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Donking-36/container-log-platform/internal/ingestion"
	"github.com/Donking-36/container-log-platform/internal/middleware"
	"github.com/Donking-36/container-log-platform/internal/service"
	"github.com/gin-gonic/gin"
)

const validEventJSON = `{
	"source_event_id": "event-1",
	"agent_id": "agent-1",
	"container_name": "producer-1",
	"container_id": "container-1",
	"service": "log-producer",
	"level": "INFO",
	"message": "hello from handler test",
	"source": "stdout",
	"logged_at": "2026-07-24T10:00:00Z"
}`

type fakeLogIngestionService struct {
	oneResult service.IngestResult
	oneErr    error
	oneCalls  int
	gotInput  ingestion.EventInput

	batchResult service.IngestResult
	batchErr    error
	batchCalls  int
	gotBatch    []ingestion.EventInput
}

func (f *fakeLogIngestionService) IngestOne(
	_ context.Context,
	input ingestion.EventInput,
) (service.IngestResult, error) {
	f.oneCalls++
	f.gotInput = input

	return f.oneResult, f.oneErr
}

func (f *fakeLogIngestionService) IngestBatch(
	_ context.Context,
	inputs []ingestion.EventInput,
) (service.IngestResult, error) {
	f.batchCalls++
	f.gotBatch = append(
		[]ingestion.EventInput(nil),
		inputs...,
	)

	return f.batchResult, f.batchErr
}

func TestNewIngestionHandlerRejectsInvalidDependencies(
	t *testing.T,
) {
	tests := []struct {
		name                    string
		service                 LogIngestionService
		maxRequestBodyBytes     int64
		maxBatchEvents          int
		wantErrorMessageContain string
	}{
		{
			name:                    "nil service",
			maxRequestBodyBytes:     1024,
			maxBatchEvents:          1000,
			wantErrorMessageContain: "service must not be nil",
		},
		{
			name:                    "non-positive body limit",
			service:                 &fakeLogIngestionService{},
			maxRequestBodyBytes:     0,
			maxBatchEvents:          1000,
			wantErrorMessageContain: "max request body bytes",
		},
		{
			name:                    "non-positive batch limit",
			service:                 &fakeLogIngestionService{},
			maxRequestBodyBytes:     1024,
			maxBatchEvents:          0,
			wantErrorMessageContain: "max batch events",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewIngestionHandler(
				tt.service,
				tt.maxRequestBodyBytes,
				tt.maxBatchEvents,
			)
			if err == nil {
				t.Fatal("expected constructor error")
			}

			if !strings.Contains(
				err.Error(),
				tt.wantErrorMessageContain,
			) {
				t.Fatalf(
					"error = %q, want it to contain %q",
					err,
					tt.wantErrorMessageContain,
				)
			}
		})
	}
}

func TestIngestionHandlerIngestOneSuccess(t *testing.T) {
	tests := []struct {
		name       string
		result     service.IngestResult
		wantStatus int
	}{
		{
			name: "new event",
			result: service.IngestResult{
				Received: 1,
				Inserted: 1,
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "duplicate event",
			result: service.IngestResult{
				Received:   1,
				Duplicated: 1,
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeService := &fakeLogIngestionService{
				oneResult: tt.result,
			}
			router := newTestIngestionRouter(
				t,
				fakeService,
				1024*1024,
				1000,
			)

			recorder := performJSONRequest(
				router,
				"/internal/v1/logs",
				validEventJSON,
			)

			if recorder.Code != tt.wantStatus {
				t.Fatalf(
					"status code = %d, want %d; body = %s",
					recorder.Code,
					tt.wantStatus,
					recorder.Body.String(),
				)
			}

			if fakeService.oneCalls != 1 {
				t.Fatalf(
					"service calls = %d, want 1",
					fakeService.oneCalls,
				)
			}

			if fakeService.gotInput.Message !=
				"hello from handler test" {
				t.Fatalf(
					"service input message = %q",
					fakeService.gotInput.Message,
				)
			}

			response := decodeIngestionResponse(
				t,
				recorder,
			)

			wantData := ingestionResultData{
				Received:   tt.result.Received,
				Inserted:   tt.result.Inserted,
				Duplicated: tt.result.Duplicated,
				Rejected:   tt.result.Rejected,
			}

			if response.Data != wantData {
				t.Fatalf(
					"response data = %+v, want %+v",
					response.Data,
					wantData,
				)
			}

			assertResponseRequestID(t, recorder, response.RequestID)
		})
	}
}

func TestIngestionHandlerIngestBatchSuccess(t *testing.T) {
	fakeService := &fakeLogIngestionService{
		batchResult: service.IngestResult{
			Received:   3,
			Inserted:   1,
			Duplicated: 1,
			Rejected:   1,
		},
	}
	router := newTestIngestionRouter(
		t,
		fakeService,
		1024*1024,
		1000,
	)

	requestBody := fmt.Sprintf(
		"[%s,%s,%s]",
		validEventJSON,
		validEventJSON,
		validEventJSON,
	)
	recorder := performJSONRequest(
		router,
		"/internal/v1/logs/bulk",
		requestBody,
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code = %d, want %d; body = %s",
			recorder.Code,
			http.StatusOK,
			recorder.Body.String(),
		)
	}

	if fakeService.batchCalls != 1 {
		t.Fatalf(
			"batch service calls = %d, want 1",
			fakeService.batchCalls,
		)
	}

	if len(fakeService.gotBatch) != 3 {
		t.Fatalf(
			"service batch length = %d, want 3",
			len(fakeService.gotBatch),
		)
	}

	response := decodeIngestionResponse(t, recorder)
	wantData := ingestionResultData{
		Received:   3,
		Inserted:   1,
		Duplicated: 1,
		Rejected:   1,
	}

	if response.Data != wantData {
		t.Fatalf(
			"response data = %+v, want %+v",
			response.Data,
			wantData,
		)
	}

	assertResponseRequestID(t, recorder, response.RequestID)
}

func TestIngestionHandlerLogsBatchRejectionsAsWarning(
	t *testing.T,
) {
	gin.SetMode(gin.TestMode)

	var logOutput bytes.Buffer
	logger := slog.New(
		slog.NewJSONHandler(&logOutput, nil),
	).With("service", "api")

	fakeService := &fakeLogIngestionService{
		batchResult: service.IngestResult{
			Received: 2,
			Inserted: 1,
			Rejected: 1,
		},
	}
	ingestionHandler, err := NewIngestionHandler(
		fakeService,
		1024*1024,
		1000,
	)
	if err != nil {
		t.Fatalf("create ingestion handler: %v", err)
	}

	router := gin.New()
	router.Use(
		middleware.RequestID(),
		middleware.RequestLogger(logger),
	)
	router.POST(
		"/internal/v1/logs/bulk",
		ingestionHandler.IngestBatch,
	)

	requestBody := fmt.Sprintf(
		"[%s,%s]",
		validEventJSON,
		validEventJSON,
	)
	recorder := performJSONRequest(
		router,
		"/internal/v1/logs/bulk",
		requestBody,
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code = %d, want %d; body = %s",
			recorder.Code,
			http.StatusOK,
			recorder.Body.String(),
		)
	}

	var logEntry map[string]any
	if err := json.Unmarshal(
		logOutput.Bytes(),
		&logEntry,
	); err != nil {
		t.Fatalf("decode request log: %v", err)
	}

	wantStrings := map[string]string{
		"level":     "WARN",
		"event":     "http_request",
		"service":   "api",
		"operation": "log_ingestion",
	}
	for key, want := range wantStrings {
		if got := logEntry[key]; got != want {
			t.Fatalf(
				"log field %q = %#v, want %q",
				key,
				got,
				want,
			)
		}
	}

	if got := logEntry["rejected"]; got != float64(1) {
		t.Fatalf(
			"log rejected = %#v, want 1",
			got,
		)
	}

	if strings.Contains(
		logOutput.String(),
		"hello from handler test",
	) {
		t.Fatal("request log leaked log message body")
	}
}

func TestIngestionHandlerRejectsInvalidJSON(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "empty body",
			body: "",
		},
		{
			name: "malformed JSON",
			body: `{"message":`,
		},
		{
			name: "unknown field",
			body: `{"unexpected":true}`,
		},
		{
			name: "trailing JSON value",
			body: validEventJSON + validEventJSON,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeService := &fakeLogIngestionService{}
			router := newTestIngestionRouter(
				t,
				fakeService,
				1024*1024,
				1000,
			)

			recorder := performJSONRequest(
				router,
				"/internal/v1/logs",
				tt.body,
			)

			assertAPIError(
				t,
				recorder,
				http.StatusBadRequest,
				"INVALID_ARGUMENT",
			)

			if fakeService.oneCalls != 0 {
				t.Fatalf(
					"service calls = %d, want 0",
					fakeService.oneCalls,
				)
			}
		})
	}
}

func TestIngestionHandlerRejectsOversizedBody(t *testing.T) {
	fakeService := &fakeLogIngestionService{}
	router := newTestIngestionRouter(
		t,
		fakeService,
		32,
		1000,
	)

	recorder := performJSONRequest(
		router,
		"/internal/v1/logs",
		validEventJSON,
	)

	assertAPIError(
		t,
		recorder,
		http.StatusRequestEntityTooLarge,
		"PAYLOAD_TOO_LARGE",
	)

	if fakeService.oneCalls != 0 {
		t.Fatalf(
			"service calls = %d, want 0",
			fakeService.oneCalls,
		)
	}
}

func TestIngestionHandlerRejectsOversizedBatch(t *testing.T) {
	fakeService := &fakeLogIngestionService{}
	router := newTestIngestionRouter(
		t,
		fakeService,
		1024*1024,
		1,
	)

	requestBody := fmt.Sprintf(
		"[%s,%s]",
		validEventJSON,
		validEventJSON,
	)
	recorder := performJSONRequest(
		router,
		"/internal/v1/logs/bulk",
		requestBody,
	)

	assertAPIError(
		t,
		recorder,
		http.StatusRequestEntityTooLarge,
		"PAYLOAD_TOO_LARGE",
	)

	if fakeService.batchCalls != 0 {
		t.Fatalf(
			"batch service calls = %d, want 0",
			fakeService.batchCalls,
		)
	}
}

func TestIngestionHandlerClassifiesValidationErrorAsRejected(
	t *testing.T,
) {
	fakeService := &fakeLogIngestionService{
		oneErr: fmt.Errorf(
			"ingest one: %w",
			&ingestion.ValidationError{
				Field:  "message",
				Reason: "must not be empty",
			},
		),
	}
	router := newTestIngestionRouter(
		t,
		fakeService,
		1024*1024,
		1000,
	)

	recorder := performJSONRequest(
		router,
		"/internal/v1/logs",
		validEventJSON,
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code = %d, want %d; body = %s",
			recorder.Code,
			http.StatusOK,
			recorder.Body.String(),
		)
	}

	response := decodeIngestionResponse(t, recorder)
	wantData := ingestionResultData{
		Received: 1,
		Rejected: 1,
	}

	if response.Data != wantData {
		t.Fatalf(
			"response data = %+v, want %+v",
			response.Data,
			wantData,
		)
	}

	assertResponseRequestID(t, recorder, response.RequestID)
}

func TestIngestionHandlerMapsEmptyBatchError(t *testing.T) {
	fakeService := &fakeLogIngestionService{
		batchErr: service.ErrEmptyBatch,
	}
	router := newTestIngestionRouter(
		t,
		fakeService,
		1024*1024,
		1000,
	)

	recorder := performJSONRequest(
		router,
		"/internal/v1/logs/bulk",
		"[]",
	)

	assertAPIError(
		t,
		recorder,
		http.StatusBadRequest,
		"INVALID_ARGUMENT",
	)
}

func TestIngestionHandlerMapsTemporaryServiceError(
	t *testing.T,
) {
	tests := []struct {
		name      string
		path      string
		body      string
		configure func(*fakeLogIngestionService, error)
	}{
		{
			name: "single event",
			path: "/internal/v1/logs",
			body: validEventJSON,
			configure: func(
				fake *fakeLogIngestionService,
				err error,
			) {
				fake.oneErr = err
			},
		},
		{
			name: "batch",
			path: "/internal/v1/logs/bulk",
			body: "[" + validEventJSON + "]",
			configure: func(
				fake *fakeLogIngestionService,
				err error,
			) {
				fake.batchErr = err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeService := &fakeLogIngestionService{}
			temporaryErr := fmt.Errorf(
				"database locked at secret path: %w",
				service.ErrTemporarilyUnavailable,
			)
			tt.configure(fakeService, temporaryErr)

			router := newTestIngestionRouter(
				t,
				fakeService,
				1024*1024,
				1000,
			)
			recorder := performJSONRequest(
				router,
				tt.path,
				tt.body,
			)

			response := assertAPIError(
				t,
				recorder,
				http.StatusServiceUnavailable,
				"SERVICE_UNAVAILABLE",
			)

			if response.Error.Message !=
				"service is temporarily unavailable" {
				t.Fatalf(
					"error message = %q, want generic message",
					response.Error.Message,
				)
			}

			if strings.Contains(
				recorder.Body.String(),
				"secret path",
			) {
				t.Fatal(
					"response leaked temporary error details",
				)
			}
		})
	}
}

func TestIngestionHandlerHidesInternalError(t *testing.T) {
	fakeService := &fakeLogIngestionService{
		oneErr: errors.New(
			"database failed at /secret/database/path",
		),
	}
	router := newTestIngestionRouter(
		t,
		fakeService,
		1024*1024,
		1000,
	)

	recorder := performJSONRequest(
		router,
		"/internal/v1/logs",
		validEventJSON,
	)

	response := assertAPIError(
		t,
		recorder,
		http.StatusInternalServerError,
		"INTERNAL_ERROR",
	)

	if response.Error.Message != "internal server error" {
		t.Fatalf(
			"error message = %q, want generic message",
			response.Error.Message,
		)
	}

	if strings.Contains(
		recorder.Body.String(),
		"/secret/database/path",
	) {
		t.Fatal("response leaked internal error details")
	}
}

func newTestIngestionRouter(
	t *testing.T,
	ingestionService LogIngestionService,
	maxRequestBodyBytes int64,
	maxBatchEvents int,
) *gin.Engine {
	t.Helper()

	gin.SetMode(gin.TestMode)

	ingestionHandler, err := NewIngestionHandler(
		ingestionService,
		maxRequestBodyBytes,
		maxBatchEvents,
	)
	if err != nil {
		t.Fatalf("create ingestion handler: %v", err)
	}

	router := gin.New()
	router.Use(middleware.RequestID())
	router.POST(
		"/internal/v1/logs",
		ingestionHandler.IngestOne,
	)
	router.POST(
		"/internal/v1/logs/bulk",
		ingestionHandler.IngestBatch,
	)

	return router
}

func performJSONRequest(
	router http.Handler,
	path string,
	body string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(
		http.MethodPost,
		path,
		strings.NewReader(body),
	)
	request.Header.Set(
		"Content-Type",
		"application/json",
	)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	return recorder
}

func decodeIngestionResponse(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
) ingestionResponse {
	t.Helper()

	var response ingestionResponse

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatalf("decode ingestion response: %v", err)
	}

	return response
}

func assertAPIError(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
	wantStatus int,
	wantCode string,
) middleware.ErrorResponse {
	t.Helper()

	if recorder.Code != wantStatus {
		t.Fatalf(
			"status code = %d, want %d; body = %s",
			recorder.Code,
			wantStatus,
			recorder.Body.String(),
		)
	}

	var response middleware.ErrorResponse

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatalf("decode error response: %v", err)
	}

	if response.Error.Code != wantCode {
		t.Fatalf(
			"error code = %q, want %q",
			response.Error.Code,
			wantCode,
		)
	}

	assertResponseRequestID(t, recorder, response.RequestID)

	return response
}

func assertResponseRequestID(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
	bodyRequestID string,
) {
	t.Helper()

	headerRequestID := recorder.Header().Get(
		middleware.RequestIDHeader,
	)

	if bodyRequestID == "" {
		t.Fatal("body request ID must not be empty")
	}

	if bodyRequestID != headerRequestID {
		t.Fatalf(
			"body request ID = %q, header request ID = %q",
			bodyRequestID,
			headerRequestID,
		)
	}
}
