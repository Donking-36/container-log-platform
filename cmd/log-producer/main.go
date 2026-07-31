package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := execute(); err != nil {
		fmt.Fprintf(
			os.Stderr,
			"log-producer: %v\n",
			err,
		)
		os.Exit(1)
	}
}

// execute 完成配置加载与信号接管。Docker stop 发送的 SIGTERM 会取消上下文，
// 使生产器停止生成新事件并在退出前同步文件日志。
func execute() error {
	cfg, err := loadConfig(os.LookupEnv)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	return runProducer(
		ctx,
		cfg,
		os.Stdout,
		os.Stderr,
		time.Now,
	)
}
