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
