package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Donking-36/container-log-platform/internal/model"
)

type fakeLogQueryReader struct {
	logs      []model.Log
	total     int64
	listErr   error
	listCalls int
	listCtx   context.Context
	lastQuery model.LogListQuery

	logEntry  model.Log
	findErr   error
	findCalls int
	findCtx   context.Context
	lastID    int64
}

func (f *fakeLogQueryReader) List(
	ctx context.Context,
	query model.LogListQuery,
) ([]model.Log, int64, error) {
	f.listCalls++
	f.listCtx = ctx
	f.lastQuery = query

	return f.logs, f.total, f.listErr
}

func (f *fakeLogQueryReader) FindByID(
	ctx context.Context,
	id int64,
) (model.Log, error) {
	f.findCalls++
	f.findCtx = ctx
	f.lastID = id

	return f.logEntry, f.findErr
}

type fakeNotFoundQueryError struct {
	cause error
}

type queryServiceContextKey struct{}

func (e *fakeNotFoundQueryError) Error() string {
	return e.cause.Error()
}

func (e *fakeNotFoundQueryError) Unwrap() error {
	return e.cause
}

func (e *fakeNotFoundQueryError) NotFound() bool {
	return true
}

func TestNewQueryServiceValidatesDependencies(
	t *testing.T,
) {
	tests := []struct {
		name            string
		logs            LogQueryReader
		defaultPageSize int
		maxPageSize     int
	}{
		{
			name:            "nil repository",
			logs:            nil,
			defaultPageSize: 20,
			maxPageSize:     100,
		},
		{
			name:            "invalid default page size",
			logs:            &fakeLogQueryReader{},
			defaultPageSize: 0,
			maxPageSize:     100,
		},
		{
			name:            "invalid max page size",
			logs:            &fakeLogQueryReader{},
			defaultPageSize: 20,
			maxPageSize:     0,
		},
		{
			name:            "default exceeds maximum",
			logs:            &fakeLogQueryReader{},
			defaultPageSize: 101,
			maxPageSize:     100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queryService, err := NewQueryService(
				tt.logs,
				tt.defaultPageSize,
				tt.maxPageSize,
			)
			if err == nil {
				t.Fatal("NewQueryService() returned nil error")
			}

			if queryService != nil {
				t.Fatalf(
					"service = %v, want nil",
					queryService,
				)
			}
		})
	}
}

func TestNewQueryServiceAcceptsEqualPageLimits(
	t *testing.T,
) {
	queryService, err := NewQueryService(
		&fakeLogQueryReader{},
		100,
		100,
	)
	if err != nil {
		t.Fatalf("NewQueryService() returned an error: %v", err)
	}

	if queryService == nil {
		t.Fatal("NewQueryService() returned nil service")
	}
}

func TestQueryServiceListLogsAppliesDefaultsAndNormalizes(
	t *testing.T,
) {
	repo := &fakeLogQueryReader{
		logs: []model.Log{
			{
				ID:      7,
				EventID: "v1:event-007",
			},
		},
		total: 1,
	}
	queryService := newQueryServiceForTest(t, repo)

	location := time.FixedZone("UTC+8", 8*60*60)
	start := time.Date(
		2026,
		time.July,
		24,
		10,
		0,
		0,
		0,
		location,
	)
	end := start.Add(time.Hour)

	ctx := context.WithValue(
		context.Background(),
		queryServiceContextKey{},
		"list-request",
	)

	result, err := queryService.ListLogs(
		ctx,
		ListLogsInput{
			ContainerName: " container-a ",
			Service:       " service-a ",
			Level:         " error ",
			Start:         &start,
			End:           &end,
		},
	)
	if err != nil {
		t.Fatalf("ListLogs() returned an error: %v", err)
	}

	if result.Page != 1 || result.PageSize != 20 {
		t.Fatalf(
			"pagination = page %d size %d, want page 1 size 20",
			result.Page,
			result.PageSize,
		)
	}

	if result.Total != 1 || len(result.Logs) != 1 {
		t.Fatalf(
			"result = %+v, want one log and total 1",
			result,
		)
	}

	if result.Logs[0].EventID != "v1:event-007" {
		t.Fatalf(
			"result EventID = %q, want %q",
			result.Logs[0].EventID,
			"v1:event-007",
		)
	}

	result.Logs[0].EventID = "changed-by-caller"
	if repo.logs[0].EventID != "v1:event-007" {
		t.Fatal("result slice aliases repository slice")
	}

	if repo.listCalls != 1 {
		t.Fatalf(
			"repository calls = %d, want 1",
			repo.listCalls,
		)
	}

	if repo.listCtx.Value(queryServiceContextKey{}) !=
		"list-request" {
		t.Fatal("ListLogs() did not preserve caller context")
	}

	query := repo.lastQuery
	if query.Limit != 20 || query.Offset != 0 {
		t.Fatalf(
			"repository pagination = limit %d offset %d",
			query.Limit,
			query.Offset,
		)
	}

	if query.Filter.ContainerName != "container-a" ||
		query.Filter.Service != "service-a" ||
		query.Filter.Level != "ERROR" {
		t.Fatalf(
			"repository filter = %+v",
			query.Filter,
		)
	}

	if query.Filter.Start == nil ||
		!query.Filter.Start.Equal(start.UTC()) {
		t.Fatalf(
			"repository start = %v, want %v",
			query.Filter.Start,
			start.UTC(),
		)
	}

	if query.Filter.End == nil ||
		!query.Filter.End.Equal(end.UTC()) {
		t.Fatalf(
			"repository end = %v, want %v",
			query.Filter.End,
			end.UTC(),
		)
	}

	if query.Filter.Start.Location() != time.UTC ||
		query.Filter.End.Location() != time.UTC {
		t.Fatal("repository time filters are not UTC")
	}

	if start.Location() != location || end.Location() != location {
		t.Fatal("ListLogs() changed caller time values")
	}
}

func TestQueryServiceListLogsCalculatesOffset(
	t *testing.T,
) {
	maxInt := int(^uint(0) >> 1)
	tests := []struct {
		name       string
		page       int
		pageSize   int
		wantOffset int
	}{
		{
			name:       "ordinary page",
			page:       3,
			pageSize:   25,
			wantOffset: 50,
		},
		{
			name:       "maximum page size",
			page:       1,
			pageSize:   100,
			wantOffset: 0,
		},
		{
			name:       "maximum safe offset",
			page:       maxInt/2 + 1,
			pageSize:   2,
			wantOffset: maxInt - 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeLogQueryReader{}
			queryService := newQueryServiceForTest(t, repo)

			result, err := queryService.ListLogs(
				context.Background(),
				ListLogsInput{
					Page:     &tt.page,
					PageSize: &tt.pageSize,
				},
			)
			if err != nil {
				t.Fatalf(
					"ListLogs() returned an error: %v",
					err,
				)
			}

			if result.Page != tt.page ||
				result.PageSize != tt.pageSize {
				t.Fatalf(
					"pagination = page %d size %d",
					result.Page,
					result.PageSize,
				)
			}

			if result.Logs == nil {
				t.Fatal("ListLogs() returned a nil log slice")
			}

			if repo.lastQuery.Limit != tt.pageSize ||
				repo.lastQuery.Offset != tt.wantOffset {
				t.Fatalf(
					"repository pagination = limit %d offset %d",
					repo.lastQuery.Limit,
					repo.lastQuery.Offset,
				)
			}
		})
	}
}

func TestQueryServiceListLogsValidatesInput(
	t *testing.T,
) {
	zero := 0
	negative := -1
	negativePageSize := -1
	tooLargePageSize := 101
	maxInt := int(^uint(0) >> 1)
	two := 2

	validStart := time.Date(
		2026,
		time.July,
		24,
		11,
		0,
		0,
		0,
		time.UTC,
	)
	earlierEnd := validStart.Add(-time.Second)

	tests := []struct {
		name    string
		input   ListLogsInput
		wantErr error
	}{
		{
			name: "zero page",
			input: ListLogsInput{
				Page: &zero,
			},
			wantErr: ErrInvalidPagination,
		},
		{
			name: "negative page",
			input: ListLogsInput{
				Page: &negative,
			},
			wantErr: ErrInvalidPagination,
		},
		{
			name: "zero page size",
			input: ListLogsInput{
				PageSize: &zero,
			},
			wantErr: ErrInvalidPagination,
		},
		{
			name: "negative page size",
			input: ListLogsInput{
				PageSize: &negativePageSize,
			},
			wantErr: ErrInvalidPagination,
		},
		{
			name: "page size exceeds maximum",
			input: ListLogsInput{
				PageSize: &tooLargePageSize,
			},
			wantErr: ErrInvalidPagination,
		},
		{
			name: "offset overflows int",
			input: ListLogsInput{
				Page:     &maxInt,
				PageSize: &two,
			},
			wantErr: ErrInvalidPagination,
		},
		{
			name: "start is later than end",
			input: ListLogsInput{
				Start: &validStart,
				End:   &earlierEnd,
			},
			wantErr: ErrInvalidTimeRange,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeLogQueryReader{}
			queryService := newQueryServiceForTest(t, repo)

			result, err := queryService.ListLogs(
				context.Background(),
				tt.input,
			)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf(
					"error = %v, want %v",
					err,
					tt.wantErr,
				)
			}

			assertZeroListLogsResult(t, result)

			if repo.listCalls != 0 {
				t.Fatalf(
					"repository calls = %d, want 0",
					repo.listCalls,
				)
			}
		})
	}
}

func TestQueryServiceListLogsAllowsEqualTimeBounds(
	t *testing.T,
) {
	repo := &fakeLogQueryReader{}
	queryService := newQueryServiceForTest(t, repo)

	boundary := time.Date(
		2026,
		time.July,
		24,
		10,
		0,
		0,
		0,
		time.UTC,
	)

	_, err := queryService.ListLogs(
		context.Background(),
		ListLogsInput{
			Start: &boundary,
			End:   &boundary,
		},
	)
	if err != nil {
		t.Fatalf("ListLogs() returned an error: %v", err)
	}
}

func TestQueryServiceListLogsNormalizesLevels(
	t *testing.T,
) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "", want: ""},
		{input: " info ", want: "INFO"},
		{input: "UNKNOWN", want: "UNKNOWN"},
		{input: "custom", want: "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			repo := &fakeLogQueryReader{}
			queryService := newQueryServiceForTest(t, repo)

			_, err := queryService.ListLogs(
				context.Background(),
				ListLogsInput{
					Level: tt.input,
				},
			)
			if err != nil {
				t.Fatalf(
					"ListLogs() returned an error: %v",
					err,
				)
			}

			if repo.lastQuery.Filter.Level != tt.want {
				t.Fatalf(
					"normalized level = %q, want %q",
					repo.lastQuery.Filter.Level,
					tt.want,
				)
			}
		})
	}
}

func TestQueryServiceListLogsHandlesRepositoryErrors(
	t *testing.T,
) {
	tests := []struct {
		name          string
		repositoryErr error
		wantTemporary bool
	}{
		{
			name:          "ordinary error",
			repositoryErr: errors.New("query failed"),
			wantTemporary: false,
		},
		{
			name: "temporary error",
			repositoryErr: &fakeTemporaryRepositoryError{
				cause: errors.New("database is busy"),
			},
			wantTemporary: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeLogQueryReader{
				listErr: tt.repositoryErr,
			}
			queryService := newQueryServiceForTest(t, repo)

			_, err := queryService.ListLogs(
				context.Background(),
				ListLogsInput{},
			)
			if !errors.Is(err, tt.repositoryErr) {
				t.Fatalf(
					"error = %v, want wrapped repository error",
					err,
				)
			}

			isTemporary := errors.Is(
				err,
				ErrTemporarilyUnavailable,
			)
			if isTemporary != tt.wantTemporary {
				t.Fatalf(
					"temporary = %t, want %t",
					isTemporary,
					tt.wantTemporary,
				)
			}
		})
	}
}

func TestQueryServiceListLogsRejectsInvalidRepositoryResult(
	t *testing.T,
) {
	tooManyLogs := make([]model.Log, 21)

	tests := []struct {
		name  string
		logs  []model.Log
		total int64
	}{
		{
			name:  "negative total",
			total: -1,
		},
		{
			name: "more logs than total",
			logs: []model.Log{
				{ID: 1},
				{ID: 2},
			},
			total: 1,
		},
		{
			name:  "more logs than page size",
			logs:  tooManyLogs,
			total: int64(len(tooManyLogs)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeLogQueryReader{
				logs:  tt.logs,
				total: tt.total,
			}
			queryService := newQueryServiceForTest(t, repo)

			result, err := queryService.ListLogs(
				context.Background(),
				ListLogsInput{},
			)
			if err == nil {
				t.Fatal("ListLogs() returned nil error")
			}

			assertZeroListLogsResult(t, result)
		})
	}
}

func TestQueryServiceGetLog(
	t *testing.T,
) {
	repo := &fakeLogQueryReader{
		logEntry: model.Log{
			ID:      42,
			EventID: "v1:event-042",
		},
	}
	queryService := newQueryServiceForTest(t, repo)

	ctx := context.WithValue(
		context.Background(),
		queryServiceContextKey{},
		"detail-request",
	)

	got, err := queryService.GetLog(
		ctx,
		42,
	)
	if err != nil {
		t.Fatalf("GetLog() returned an error: %v", err)
	}

	if got.ID != 42 || got.EventID != "v1:event-042" {
		t.Fatalf("GetLog() = %+v", got)
	}

	if repo.findCalls != 1 || repo.lastID != 42 {
		t.Fatalf(
			"repository calls = %d, ID = %d",
			repo.findCalls,
			repo.lastID,
		)
	}

	if repo.findCtx.Value(queryServiceContextKey{}) !=
		"detail-request" {
		t.Fatal("GetLog() did not preserve caller context")
	}
}

func TestQueryServiceGetLogRejectsInvalidID(
	t *testing.T,
) {
	for _, id := range []int64{-1, 0} {
		t.Run("invalid ID", func(t *testing.T) {
			repo := &fakeLogQueryReader{}
			queryService := newQueryServiceForTest(t, repo)

			_, err := queryService.GetLog(
				context.Background(),
				id,
			)
			if !errors.Is(err, ErrInvalidLogID) {
				t.Fatalf(
					"error = %v, want ErrInvalidLogID",
					err,
				)
			}

			if repo.findCalls != 0 {
				t.Fatalf(
					"repository calls = %d, want 0",
					repo.findCalls,
				)
			}
		})
	}
}

func TestQueryServiceGetLogMapsRepositoryErrors(
	t *testing.T,
) {
	cause := errors.New("repository failure")

	tests := []struct {
		name          string
		repositoryErr error
		wantNotFound  bool
		wantTemporary bool
	}{
		{
			name:          "ordinary error",
			repositoryErr: cause,
		},
		{
			name: "not found",
			repositoryErr: &fakeNotFoundQueryError{
				cause: cause,
			},
			wantNotFound: true,
		},
		{
			name: "temporary error",
			repositoryErr: &fakeTemporaryRepositoryError{
				cause: cause,
			},
			wantTemporary: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeLogQueryReader{
				findErr: tt.repositoryErr,
			}
			queryService := newQueryServiceForTest(t, repo)

			_, err := queryService.GetLog(
				context.Background(),
				42,
			)
			if !errors.Is(err, cause) {
				t.Fatalf(
					"error = %v, want wrapped cause",
					err,
				)
			}

			isNotFound := errors.Is(err, ErrLogNotFound)
			if isNotFound != tt.wantNotFound {
				t.Fatalf(
					"not found = %t, want %t",
					isNotFound,
					tt.wantNotFound,
				)
			}

			isTemporary := errors.Is(
				err,
				ErrTemporarilyUnavailable,
			)
			if isTemporary != tt.wantTemporary {
				t.Fatalf(
					"temporary = %t, want %t",
					isTemporary,
					tt.wantTemporary,
				)
			}
		})
	}
}

func newQueryServiceForTest(
	t *testing.T,
	repo LogQueryReader,
) *QueryService {
	t.Helper()

	queryService, err := NewQueryService(
		repo,
		20,
		100,
	)
	if err != nil {
		t.Fatalf("NewQueryService() returned an error: %v", err)
	}

	return queryService
}

func assertZeroListLogsResult(
	t *testing.T,
	result ListLogsResult,
) {
	t.Helper()

	if result.Logs != nil ||
		result.Page != 0 ||
		result.PageSize != 0 ||
		result.Total != 0 {
		t.Fatalf(
			"result = %+v, want zero result",
			result,
		)
	}
}
