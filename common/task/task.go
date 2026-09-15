package task

import (
	"sync"
	"time"
)

// Task is a task that runs periodically.
type Task struct {
	// Interval of the task being run
	Interval time.Duration
	// Execute is the task function
	Execute func() error

	// execMu serializes the body. A task can be restarted while its body is still
	// running: nodeInfoMonitor lowers the push interval and re-arms the report
	// task, which would otherwise run two reports (and two /push requests) at the
	// same time.
	//
	// It is deliberately not access: Close and Start are called from the task
	// body itself (a task re-arms itself when the panel changes its interval),
	// and they must not block on a mutex the same goroutine holds. For the same
	// reason a body must not call Start(true) on itself.
	execMu sync.Mutex

	access  sync.Mutex
	timer   *time.Timer
	running bool
}

func (t *Task) hasClosed() bool {
	t.access.Lock()
	defer t.access.Unlock()

	return !t.running
}

// checkedExecute runs the task body (when first is true) and arms the next run.
//
// Execute is deliberately called without holding access: the task body is
// allowed to Close/Start its own task (nodeInfoMonitor re-arms itself when the
// panel changes the pull interval). Calling it while holding the mutex made the
// task deadlock on itself forever, because sync.Mutex is not reentrant.
func (t *Task) checkedExecute(first bool) error {
	if t.hasClosed() {
		return nil
	}

	if first {
		t.execMu.Lock()
		// The task may have been closed while waiting for a body that is already
		// running (a restart arrives with the same first=true).
		if t.hasClosed() {
			t.execMu.Unlock()
			return nil
		}
		err := t.Execute()
		t.execMu.Unlock()
		if err != nil {
			t.access.Lock()
			t.running = false
			t.access.Unlock()
			return err
		}
	}

	t.access.Lock()
	defer t.access.Unlock()
	if !t.running {
		return nil
	}
	// Execute may have closed and restarted the task (interval change) while it
	// was running. A timer is already armed in that case, arming another one
	// would make the task run N times per interval.
	if t.timer != nil {
		return nil
	}
	t.timer = time.AfterFunc(t.Interval, t.fire)

	return nil
}

// fire consumes the armed timer and runs the task.
func (t *Task) fire() {
	t.access.Lock()
	t.timer = nil
	t.access.Unlock()

	_ = t.checkedExecute(true)
}

// Start implements common.Runnable.
func (t *Task) Start(first bool) error {
	t.access.Lock()
	if t.running {
		t.access.Unlock()
		return nil
	}
	t.running = true
	t.access.Unlock()
	if err := t.checkedExecute(first); err != nil {
		t.access.Lock()
		t.running = false
		if t.timer != nil {
			t.timer.Stop()
			t.timer = nil
		}
		t.access.Unlock()
		return err
	}
	return nil
}

// Close implements common.Closable.
func (t *Task) Close() {
	t.access.Lock()
	defer t.access.Unlock()

	t.running = false
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
}
