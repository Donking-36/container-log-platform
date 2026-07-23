package server

import (
	"context"
	"sync/atomic"
)

// Readiness 管理服务是否可以接收流量，并检查依赖是否健康。
type Readiness struct {
	acceptingTraffic atomic.Bool
	dependencyCheck  func(context.Context) error
}

// NewReadiness 创建就绪状态管理器。
func NewReadiness(
	dependencyCheck func(context.Context) error,
) *Readiness {
	return &Readiness{
		dependencyCheck: dependencyCheck,
	}
}

// SetReady 设置服务是否已经准备好接收流量。
func (r *Readiness) SetReady(ready bool) {
	if r == nil {
		return
	}

	r.acceptingTraffic.Store(ready)
}

// Ready 判断服务当前是否就绪。
func (r *Readiness) Ready(ctx context.Context) bool {
	if r == nil || !r.acceptingTraffic.Load() {
		return false
	}

	if r.dependencyCheck == nil {
		return false
	}

	return r.dependencyCheck(ctx) == nil
}
