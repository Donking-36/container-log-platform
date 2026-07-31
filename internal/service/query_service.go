package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Donking-36/container-log-platform/internal/model"
)

var (
	// ErrInvalidPagination 表示页码或每页数量超出查询规则。
	ErrInvalidPagination = errors.New(
		"invalid log query pagination",
	)
	// ErrInvalidTimeRange 表示查询开始时间晚于结束时间。
	ErrInvalidTimeRange = errors.New(
		"invalid log query time range",
	)
	// ErrInvalidLogID 表示日志 ID 不是正整数。
	ErrInvalidLogID = errors.New(
		"invalid log ID",
	)
	// ErrLogNotFound 表示指定 ID 的日志不存在。
	ErrLogNotFound = errors.New(
		"log not found",
	)
)

// LogQueryReader 描述 QueryService 需要的最小日志读取能力。
type LogQueryReader interface {
	List(
		ctx context.Context,
		query model.LogListQuery,
	) ([]model.Log, int64, error)

	FindByID(
		ctx context.Context,
		id int64,
	) (model.Log, error)
}

// ListLogsInput 描述已经完成 HTTP 语法解析的列表查询输入。
// nil 页码表示调用方没有提供该参数，应使用 Service 默认值。
type ListLogsInput struct {
	ContainerName string
	Service       string
	Level         string
	Start         *time.Time
	End           *time.Time
	Page          *int
	PageSize      *int
}

// ListLogsResult 表示日志列表和对应的分页信息。
type ListLogsResult struct {
	Logs     []model.Log
	Page     int
	PageSize int
	Total    int64
}

// QueryService 组织日志列表和详情查询业务。
type QueryService struct {
	logs            LogQueryReader
	defaultPageSize int
	maxPageSize     int
}

// NewQueryService 创建日志查询 Service。
func NewQueryService(
	logs LogQueryReader,
	defaultPageSize int,
	maxPageSize int,
) (*QueryService, error) {
	if logs == nil {
		return nil, errors.New(
			"create query service: log repository must not be nil",
		)
	}

	if defaultPageSize <= 0 {
		return nil, errors.New(
			"create query service: " +
				"default page size must be greater than zero",
		)
	}

	if maxPageSize <= 0 {
		return nil, errors.New(
			"create query service: " +
				"max page size must be greater than zero",
		)
	}

	if defaultPageSize > maxPageSize {
		return nil, errors.New(
			"create query service: " +
				"default page size must not exceed max page size",
		)
	}

	return &QueryService{
		logs:            logs,
		defaultPageSize: defaultPageSize,
		maxPageSize:     maxPageSize,
	}, nil
}

// ListLogs 校验并规范化查询条件，然后读取一页日志。
// HTTP 层只负责语法解析；默认分页、UTC 时间和级别规范化等业务规则
// 在这里统一完成，其他传输层也可复用。
func (s *QueryService) ListLogs(
	ctx context.Context,
	input ListLogsInput,
) (ListLogsResult, error) {
	page, pageSize, offset, err := s.resolvePagination(
		input.Page,
		input.PageSize,
	)
	if err != nil {
		return ListLogsResult{}, err
	}

	start := cloneTimeAsUTC(input.Start)
	end := cloneTimeAsUTC(input.End)

	if start != nil && end != nil && start.After(*end) {
		return ListLogsResult{}, fmt.Errorf(
			"%w: start must not be later than end",
			ErrInvalidTimeRange,
		)
	}

	query := model.LogListQuery{
		Filter: model.LogFilter{
			ContainerName: strings.TrimSpace(
				input.ContainerName,
			),
			Service: strings.TrimSpace(input.Service),
			Level:   normalizeQueryLevel(input.Level),
			Start:   start,
			End:     end,
		},
		Limit:  pageSize,
		Offset: offset,
	}

	logs, total, err := s.logs.List(ctx, query)
	if err != nil {
		return ListLogsResult{}, wrapRepositoryError(
			"list logs",
			err,
		)
	}

	if total < 0 ||
		len(logs) > pageSize ||
		int64(len(logs)) > total {
		return ListLogsResult{}, fmt.Errorf(
			"list logs: repository returned "+
				"invalid result with %d logs, page size %d, "+
				"and total %d",
			len(logs),
			pageSize,
			total,
		)
	}

	// 即使 Repository 返回 nil，也保持 JSON 数组所需的非 nil 空切片。
	resultLogs := append([]model.Log{}, logs...)

	return ListLogsResult{
		Logs:     resultLogs,
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	}, nil
}

// GetLog 按数据库 ID 读取一条完整日志，并把存储层的缺失语义适配为 ErrLogNotFound。
func (s *QueryService) GetLog(
	ctx context.Context,
	id int64,
) (model.Log, error) {
	if id <= 0 {
		return model.Log{}, fmt.Errorf(
			"%w: id must be greater than zero",
			ErrInvalidLogID,
		)
	}

	logEntry, err := s.logs.FindByID(ctx, id)
	if err == nil {
		return logEntry, nil
	}

	var notFound interface {
		error
		NotFound() bool
	}
	// 通过能力接口而不是依赖 GORM 的具体错误类型，保持 Service 与存储实现解耦。
	// errors.As 会穿透 Repository 添加的上下文包装。
	if errors.As(err, &notFound) && notFound.NotFound() {
		err = errors.Join(
			ErrLogNotFound,
			err,
		)
	}

	return model.Log{}, wrapRepositoryError(
		"get log",
		err,
	)
}

func (s *QueryService) resolvePagination(
	pageInput *int,
	pageSizeInput *int,
) (int, int, int, error) {
	page := 1
	if pageInput != nil {
		page = *pageInput
	}

	if page <= 0 {
		return 0, 0, 0, fmt.Errorf(
			"%w: page must be greater than zero",
			ErrInvalidPagination,
		)
	}

	pageSize := s.defaultPageSize
	if pageSizeInput != nil {
		pageSize = *pageSizeInput
	}

	if pageSize <= 0 {
		return 0, 0, 0, fmt.Errorf(
			"%w: page_size must be greater than zero",
			ErrInvalidPagination,
		)
	}

	if pageSize > s.maxPageSize {
		return 0, 0, 0, fmt.Errorf(
			"%w: page_size must not exceed %d",
			ErrInvalidPagination,
			s.maxPageSize,
		)
	}

	// 在执行乘法前检查 int 溢出，防止超大 page 绕回负数并形成错误的 SQL OFFSET。
	maxInt := int(^uint(0) >> 1)
	if page-1 > maxInt/pageSize {
		return 0, 0, 0, fmt.Errorf(
			"%w: pagination offset is too large",
			ErrInvalidPagination,
		)
	}

	return page, pageSize, (page - 1) * pageSize, nil
}

func normalizeQueryLevel(value string) string {
	level := strings.ToUpper(strings.TrimSpace(value))

	switch level {
	case "":
		return ""
	case "DEBUG", "INFO", "WARN", "ERROR", "UNKNOWN":
		return level
	default:
		// 未知非空级别统一映射到 UNKNOWN，与接收阶段的级别规范化保持一致。
		// 这样客户端仍能查询未落入标准枚举的来源日志。
		return "UNKNOWN"
	}
}

func cloneTimeAsUTC(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	utc := value.UTC()

	return &utc
}
