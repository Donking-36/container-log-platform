package server_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Donking-36/container-log-platform/internal/handler"
	"github.com/Donking-36/container-log-platform/internal/middleware"
	"github.com/Donking-36/container-log-platform/internal/repository"
	"github.com/Donking-36/container-log-platform/internal/server"
	"github.com/Donking-36/container-log-platform/internal/service"
	"github.com/gin-gonic/gin"
)

func TestLogIngestionAndPublicQueryIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	gormDB, err := repository.OpenSQLite(
		filepath.Join(t.TempDir(), "logs.db"),
	)
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("access SQLite connection pool: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close SQLite: %v", err)
		}
	})

	if err := repository.Migrate(gormDB); err != nil {
		t.Fatalf("migrate SQLite: %v", err)
	}

	logRepository, err := repository.NewLogRepository(gormDB)
	if err != nil {
		t.Fatalf("create log repository: %v", err)
	}

	ingestionService, err := service.NewIngestionService(
		logRepository,
		64*1024,
	)
	if err != nil {
		t.Fatalf("create ingestion service: %v", err)
	}

	ingestionHandler, err := handler.NewIngestionHandler(
		ingestionService,
		1024*1024,
		100,
	)
	if err != nil {
		t.Fatalf("create ingestion handler: %v", err)
	}

	queryService, err := service.NewQueryService(
		logRepository,
		20,
		100,
	)
	if err != nil {
		t.Fatalf("create query service: %v", err)
	}

	queryHandler, err := handler.NewQueryHandler(queryService)
	if err != nil {
		t.Fatalf("create query handler: %v", err)
	}

	readiness := server.NewReadiness(sqlDB.PingContext)
	readiness.SetReady(true)

	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	publicRouter, err := server.NewPublicRouter(
		queryHandler,
		readiness,
		logger,
	)
	if err != nil {
		t.Fatalf("create public router: %v", err)
	}

	internalRouter, err := server.NewInternalRouter(
		ingestionHandler,
		readiness,
		logger,
	)
	if err != nil {
		t.Fatalf("create internal router: %v", err)
	}

	ingestBody := `{
		"source_event_id": "integration-event-001",
		"agent_id": "integration-agent",
		"container_name": "integration-container",
		"container_id": "integration-container-id",
		"service": "integration-service",
		"level": "info",
		"message": "integration query message",
		"source": "stdout",
		"logged_at": "2026-07-24T10:00:00Z",
		"raw_event": {
			"nested": {"ok": true},
			"sequence": 1
		}
	}`
	ingestRecorder := httptest.NewRecorder()
	ingestRequest := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/logs",
		strings.NewReader(ingestBody),
	)
	ingestRequest.Header.Set("Content-Type", "application/json")

	internalRouter.ServeHTTP(ingestRecorder, ingestRequest)

	if ingestRecorder.Code != http.StatusCreated {
		t.Fatalf(
			"ingestion status = %d, want %d; body = %s",
			ingestRecorder.Code,
			http.StatusCreated,
			ingestRecorder.Body.String(),
		)
	}

	listRecorder := httptest.NewRecorder()
	listRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/logs?"+
			"container=integration-container&"+
			"level=INFO&page=1&page_size=10",
		nil,
	)

	publicRouter.ServeHTTP(listRecorder, listRequest)

	if listRecorder.Code != http.StatusOK {
		t.Fatalf(
			"list status = %d, want %d; body = %s",
			listRecorder.Code,
			http.StatusOK,
			listRecorder.Body.String(),
		)
	}

	var listResponse struct {
		Data       []map[string]json.RawMessage `json:"data"`
		Pagination struct {
			Total int64 `json:"total"`
		} `json:"pagination"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(
		listRecorder.Body.Bytes(),
		&listResponse,
	); err != nil {
		t.Fatalf("decode list response: %v", err)
	}

	if listResponse.Pagination.Total != 1 ||
		len(listResponse.Data) != 1 {
		t.Fatalf(
			"list total = %d, data length = %d; want 1 and 1",
			listResponse.Pagination.Total,
			len(listResponse.Data),
		)
	}

	if _, exists := listResponse.Data[0]["raw_event"]; exists {
		t.Fatal("list response exposed raw_event")
	}

	if listResponse.RequestID == "" ||
		listResponse.RequestID != listRecorder.Header().Get(
			middleware.RequestIDHeader,
		) {
		t.Fatal("list response request ID does not match header")
	}

	var logID int64
	if err := json.Unmarshal(
		listResponse.Data[0]["id"],
		&logID,
	); err != nil {
		t.Fatalf("decode list log ID: %v", err)
	}

	detailRecorder := httptest.NewRecorder()
	detailRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/logs/"+strconv.FormatInt(logID, 10),
		nil,
	)

	publicRouter.ServeHTTP(detailRecorder, detailRequest)

	if detailRecorder.Code != http.StatusOK {
		t.Fatalf(
			"detail status = %d, want %d; body = %s",
			detailRecorder.Code,
			http.StatusOK,
			detailRecorder.Body.String(),
		)
	}

	var detailResponse struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(
		detailRecorder.Body.Bytes(),
		&detailResponse,
	); err != nil {
		t.Fatalf("decode detail response: %v", err)
	}

	var rawEvent struct {
		Nested struct {
			OK bool `json:"ok"`
		} `json:"nested"`
		Sequence int `json:"sequence"`
	}
	if err := json.Unmarshal(
		detailResponse.Data["raw_event"],
		&rawEvent,
	); err != nil {
		t.Fatalf("decode detail raw_event: %v", err)
	}

	if !rawEvent.Nested.OK || rawEvent.Sequence != 1 {
		t.Fatalf("detail raw_event = %+v", rawEvent)
	}
}
