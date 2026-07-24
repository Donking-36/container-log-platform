package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Donking-36/container-log-platform/internal/ingestion"
	"github.com/Donking-36/container-log-platform/internal/model"
)

var ErrEmptyBatch = errors.New(
	"ingest batch: events must not be empty",
)

// LogBatchInserter描述Service需要的最小存储能力。
type LogBatchInserter interface {
	InsertBatch(
		ctx context.Context,
		logs []model.Log,
	) (int64, error)
}

// IngestResult表示一次接收操作的业务结果。
type IngestResult struct {
	Received   int64
	Inserted   int64
	Duplicated int64
	Rejected   int64
}

// IngestionService组织日志接收业务。
type IngestionService struct {
	logs               LogBatchInserter
	maxLogMessageBytes int64
	now                func() time.Time
}

// NewIngestionService创建日志接收Service。
func NewIngestionService(
	logs LogBatchInserter,
	maxLogMessageBytes int64,
) (*IngestionService, error) {
	if logs == nil {
		return nil, errors.New(
			"create ingestion service: log repository must not be nil",
		)
	}

	if maxLogMessageBytes <= 0 {
		return nil, errors.New(
			"create ingestion service: " +
				"max log message bytes must be greater than zero",
		)
	}

	return &IngestionService{
		logs:               logs,
		maxLogMessageBytes: maxLogMessageBytes,
		now:                time.Now,
	}, nil
}

// IngestOne接收并幂等存储一条日志。
func (s *IngestionService) IngestOne(
	ctx context.Context,
	input ingestion.EventInput,
) (IngestResult, error) {
	entry, err := ingestion.PrepareLog(
		input,
		s.maxLogMessageBytes,
		s.now(),
	)
	if err != nil {
		return IngestResult{}, fmt.Errorf(
			"ingest one log: %w",
			err,
		)
	}

	inserted, err := s.logs.InsertBatch(
		ctx,
		[]model.Log{entry},
	)
	if err != nil {
		return IngestResult{}, fmt.Errorf(
			"ingest one log: persist event: %w",
			err,
		)
	}

	if inserted < 0 || inserted > 1 {
		return IngestResult{}, fmt.Errorf(
			"ingest one log: repository reported "+
				"invalid inserted count %d",
			inserted,
		)
	}

	return IngestResult{
		Received:   1,
		Inserted:   inserted,
		Duplicated: 1 - inserted,
		Rejected:   0,
	}, nil
}

// IngestBatch接收并幂等存储一批日志。
func (s *IngestionService) IngestBatch(
	ctx context.Context,
	inputs []ingestion.EventInput,
) (IngestResult, error) {
	if len(inputs) == 0 {
		return IngestResult{}, ErrEmptyBatch
	}

	// 一个批次只读取一次当前时间，
	// 保证同批事件具有相同的ingested_at。
	ingestedAt := s.now()

	result := IngestResult{
		Received: int64(len(inputs)),
	}

	entries := make(
		[]model.Log,
		0,
		len(inputs),
	)

	for index, input := range inputs {
		entry, err := ingestion.PrepareLog(
			input,
			s.maxLogMessageBytes,
			ingestedAt,
		)
		if err != nil {
			var validationErr *ingestion.ValidationError

			// 永久非法事件不会因为重试而变好，
			// 因此计入rejected并继续处理其他事件。
			if errors.As(err, &validationErr) {
				result.Rejected++
				continue
			}

			// 配置、时间源或内部不变量错误不是普通拒绝，
			// 必须让整个批次失败。
			return IngestResult{}, fmt.Errorf(
				"ingest batch: prepare event %d: %w",
				index,
				err,
			)
		}

		entries = append(entries, entry)
	}

	// 全部事件都永久非法时，不需要访问数据库。
	if len(entries) == 0 {
		return result, nil
	}

	inserted, err := s.logs.InsertBatch(
		ctx,
		entries,
	)
	if err != nil {
		// 数据库失败时不返回部分统计，
		// 避免调用方误以为批次已经确认成功。
		return IngestResult{}, fmt.Errorf(
			"ingest batch: persist events: %w",
			err,
		)
	}

	validCount := int64(len(entries))

	if inserted < 0 || inserted > validCount {
		return IngestResult{}, fmt.Errorf(
			"ingest batch: repository reported "+
				"invalid inserted count %d for %d events",
			inserted,
			validCount,
		)
	}

	result.Inserted = inserted
	result.Duplicated = validCount - inserted

	return result, nil
}
