package inflight

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func waitShared(t *testing.T, group *Group[string, int], want uint64) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if _, shared := group.Stats(); shared >= want {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("follower did not join shared work")
		case <-ticker.C:
		}
	}
}

func TestLeaderCancellationDoesNotCancelFollower(t *testing.T) {
	var group Group[string, int]
	var calls atomic.Int32
	started, finish := make(chan struct{}), make(chan struct{})
	work := func(ctx context.Context) (int, error) {
		calls.Add(1)
		close(started)
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-finish:
			return 42, nil
		}
	}
	leaderCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leader := make(chan error, 1)
	go func() { _, err := group.Do(leaderCtx, "body", time.Second, work); leader <- err }()
	<-started
	follower := make(chan int, 1)
	go func() {
		value, err := group.Do(context.Background(), "body", time.Second, work)
		if err != nil {
			value = -1
		}
		follower <- value
	}()
	waitShared(t, &group, 1)
	cancel()
	if err := <-leader; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader cancellation lost: %v", err)
	}
	close(finish)
	if value := <-follower; value != 42 || calls.Load() != 1 {
		t.Fatalf("live follower lost shared result: value=%d calls=%d", value, calls.Load())
	}
}

func TestAllWaitersCancelAndOldCompletionCannotRemoveReplacement(t *testing.T) {
	var group Group[string, int]
	ctx, cancel := context.WithCancel(context.Background())
	started, cancelled, oldFinish := make(chan struct{}), make(chan struct{}), make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := group.Do(ctx, "same", time.Second, func(workCtx context.Context) (int, error) {
			close(started)
			<-workCtx.Done()
			close(cancelled)
			<-oldFinish
			return 0, workCtx.Err()
		})
		firstDone <- err
	}()
	<-started
	cancel()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	<-cancelled
	newStarted, newFinish := make(chan struct{}), make(chan struct{})
	result := make(chan int, 2)
	var calls atomic.Int32
	work := func(context.Context) (int, error) {
		calls.Add(1)
		close(newStarted)
		<-newFinish
		return 7, nil
	}
	go func() { value, _ := group.Do(context.Background(), "same", time.Second, work); result <- value }()
	<-newStarted
	close(oldFinish)
	go func() { value, _ := group.Do(context.Background(), "same", time.Second, work); result <- value }()
	waitShared(t, &group, 1)
	close(newFinish)
	if <-result != 7 || <-result != 7 || calls.Load() != 1 {
		t.Fatal("old completion discarded the replacement")
	}
}

func TestErrorsAreNotCachedAndIdentityKeysStaySeparate(t *testing.T) {
	var group Group[string, int]
	failed := errors.New("temporary failure")
	if _, err := group.Do(context.Background(), "old", time.Second, func(context.Context) (int, error) { return 0, failed }); !errors.Is(err, failed) {
		t.Fatal(err)
	}
	if value, err := group.Do(context.Background(), "old", time.Second, func(context.Context) (int, error) { return 5, nil }); err != nil || value != 5 {
		t.Fatalf("failed work was cached: %d %v", value, err)
	}
	started, finish := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = group.Do(context.Background(), "old", time.Second, func(context.Context) (int, error) {
			close(started)
			<-finish
			return 1, nil
		})
	}()
	<-started
	if value, err := group.Do(context.Background(), "new", time.Second, func(context.Context) (int, error) { return 2, nil }); err != nil || value != 2 {
		t.Fatalf("identity keys collided: %d %v", value, err)
	}
	close(finish)
	<-done
	if active, _ := group.Stats(); active != 0 {
		t.Fatalf("completed work retained: %d", active)
	}
}

func TestSharedWorkHasIndependentBoundedDeadline(t *testing.T) {
	var group Group[string, int]
	_, err := group.Do(context.Background(), "timeout", 10*time.Millisecond, func(ctx context.Context) (int, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shared work was not bounded: %v", err)
	}
}
