package task

import (
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTask(t *testing.T) {
	ts := Task{Execute: func() error {
		log.Println("q")
		return nil
	}, Interval: time.Second}
	ts.Start(false)
}

// TestTaskExecuteCanCloseAndRestartItself guards the nodeInfoMonitor use case:
// the task body changes its own interval, closes and restarts itself. With the
// old implementation Execute ran while holding Task.access, so Close() blocked
// on the non reentrant mutex and the node monitor goroutine hung forever.
func TestTaskExecuteCanCloseAndRestartItself(t *testing.T) {
	var (
		tk    *Task
		count int32
	)
	tk = &Task{Interval: 10 * time.Millisecond}
	tk.Execute = func() error {
		if atomic.AddInt32(&count, 1) == 1 {
			tk.Close()
			tk.Interval = 10 * time.Second
			_ = tk.Start(false)
		}
		return nil
	}

	done := make(chan error, 1)
	go func() {
		done <- tk.Start(true)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start error: %s", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("deadlock: Execute() could not Close/Start its own task")
	}
	t.Cleanup(tk.Close)

	// The restart armed exactly one timer with the new 10s interval. A duplicated
	// arm would keep firing every 10ms.
	time.Sleep(150 * time.Millisecond)
	if n := atomic.LoadInt32(&count); n != 1 {
		t.Fatalf("task re-armed more than once: %d executions, want 1", n)
	}
}

// TestTaskCloseDuringExecuteStopsTheTask makes sure a task that closes itself
// from its own body is not re-armed afterwards.
func TestTaskCloseDuringExecuteStopsTheTask(t *testing.T) {
	var (
		tk    *Task
		count int32
	)
	tk = &Task{Interval: 5 * time.Millisecond}
	tk.Execute = func() error {
		if atomic.AddInt32(&count, 1) == 1 {
			tk.Close()
		}
		return nil
	}

	if err := tk.Start(true); err != nil {
		t.Fatalf("Start error: %s", err)
	}
	time.Sleep(80 * time.Millisecond)
	if n := atomic.LoadInt32(&count); n != 1 {
		t.Fatalf("closed task kept running: %d executions, want 1", n)
	}
}

// TestTaskRunsPeriodicallyAndStops checks the plain periodic loop and Close.
func TestTaskRunsPeriodicallyAndStops(t *testing.T) {
	var count int32
	tk := &Task{
		Interval: 10 * time.Millisecond,
		Execute: func() error {
			atomic.AddInt32(&count, 1)
			return nil
		},
	}

	if err := tk.Start(true); err != nil {
		t.Fatalf("Start error: %s", err)
	}
	time.Sleep(150 * time.Millisecond)
	if n := atomic.LoadInt32(&count); n < 3 {
		t.Fatalf("task did not run periodically: %d executions", n)
	}
	tk.Close()

	time.Sleep(50 * time.Millisecond)
	first := atomic.LoadInt32(&count)
	time.Sleep(80 * time.Millisecond)
	if second := atomic.LoadInt32(&count); second != first {
		t.Fatalf("task kept running after Close: %d -> %d", first, second)
	}
}

// TestTaskExecuteErrorStopsTheTask keeps the historical contract: a failing
// task body disables the periodic loop instead of retrying forever.
func TestTaskExecuteErrorStopsTheTask(t *testing.T) {
	var count int32
	tk := &Task{
		Interval: 5 * time.Millisecond,
		Execute: func() error {
			atomic.AddInt32(&count, 1)
			return errors.New("boom")
		},
	}

	if err := tk.Start(true); err == nil {
		t.Fatal("Start should return the Execute error")
	}
	time.Sleep(50 * time.Millisecond)
	if n := atomic.LoadInt32(&count); n != 1 {
		t.Fatalf("failing task kept running: %d executions, want 1", n)
	}
}

// TestTaskStartIsIdempotent keeps Start(first) from double arming a running task.
func TestTaskStartIsIdempotent(t *testing.T) {
	var count int32
	tk := &Task{
		Interval: 10 * time.Millisecond,
		Execute: func() error {
			atomic.AddInt32(&count, 1)
			return nil
		},
	}
	t.Cleanup(tk.Close)

	if err := tk.Start(false); err != nil {
		t.Fatalf("Start error: %s", err)
	}
	if err := tk.Start(false); err != nil {
		t.Fatalf("second Start error: %s", err)
	}

	time.Sleep(150 * time.Millisecond)
	// A single timer chain should have produced roughly 15 runs, two chains
	// would roughly double that.
	if n := atomic.LoadInt32(&count); n > 25 {
		t.Fatalf("task armed more than once: %d executions", n)
	}
}

// TestTaskSerializesARestartWhileTheBodyRuns covers the nodeInfoMonitor ->
// reportUserTrafficTask restart: the panel changes the push interval while a
// report is still running and the report task is closed and started again with
// first=true. Without execMu the restarted body runs next to the running one and
// reports the same traffic and the same online state twice.
func TestTaskSerializesARestartWhileTheBodyRuns(t *testing.T) {
	var (
		mu        sync.Mutex
		inBody    int
		maxInBody int
	)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	tk := &Task{
		Interval: time.Hour,
		Execute: func() error {
			mu.Lock()
			inBody++
			if inBody > maxInBody {
				maxInBody = inBody
			}
			mu.Unlock()

			started <- struct{}{}
			<-release

			mu.Lock()
			inBody--
			mu.Unlock()
			return nil
		},
	}
	t.Cleanup(tk.Close)

	go func() { _ = tk.Start(true) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("the task body did not start")
	}

	// The panel changed the interval while the report was running.
	tk.Close()
	go func() { _ = tk.Start(true) }()

	select {
	case <-started:
		t.Fatal("the restarted body ran next to the running one")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("the restarted body never ran")
	}

	mu.Lock()
	defer mu.Unlock()
	if maxInBody != 1 {
		t.Fatalf("%d bodies ran at the same time, want 1", maxInBody)
	}
}
