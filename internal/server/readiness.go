package server

import (
	"context"
	"sync/atomic"
)

// Readiness 同时管理进程接流量开关和依赖健康状态。
// 只有两个 HTTP 监听器均已绑定且数据库探测成功时，服务才对外就绪。
type Readiness struct {
	acceptingTraffic atomic.Bool
	dependencyCheck  func(context.Context) error
}

// NewReadiness 创建就绪状态管理器。
// dependencyCheck 为 nil 时采用 fail-closed 策略，Ready 始终返回 false。
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
// 先检查原子流量开关可以在关闭阶段立即摘流量，再执行数据库等依赖探测。
func (r *Readiness) Ready(ctx context.Context) bool {
	if r == nil || !r.acceptingTraffic.Load() {
		return false
	}

	if r.dependencyCheck == nil {
		return false
	}

	return r.dependencyCheck(ctx) == nil
}
