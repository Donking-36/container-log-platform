package server

import (
	"context"
	"errors"
	"testing"
)

func TestReadinessRequiresTrafficGateAndHealthyDependency(
	t *testing.T,
) {
	var dependencyErr error
	checkCalls := 0

	readiness := NewReadiness(
		func(context.Context) error {
			checkCalls++
			return dependencyErr
		},
	)

	if readiness.Ready(context.Background()) {
		t.Fatal("readiness should be false before SetReady(true)")
	}

	if checkCalls != 0 {
		t.Fatalf(
			"dependency check called %d times before ready, want 0",
			checkCalls,
		)
	}

	readiness.SetReady(true)

	if !readiness.Ready(context.Background()) {
		t.Fatal("readiness should be true for healthy dependency")
	}

	if checkCalls != 1 {
		t.Fatalf(
			"dependency check called %d times, want 1",
			checkCalls,
		)
	}

	dependencyErr = errors.New("database unavailable")

	if readiness.Ready(context.Background()) {
		t.Fatal("readiness should be false when dependency fails")
	}

	readiness.SetReady(false)
	dependencyErr = nil

	if readiness.Ready(context.Background()) {
		t.Fatal("readiness should be false after SetReady(false)")
	}

	if checkCalls != 2 {
		t.Fatalf(
			"dependency check called %d times, want 2",
			checkCalls,
		)
	}
}

func TestReadinessWithoutDependencyCheckIsNotReady(
	t *testing.T,
) {
	readiness := NewReadiness(nil)
	readiness.SetReady(true)

	if readiness.Ready(context.Background()) {
		t.Fatal("readiness without dependency check should be false")
	}
}
