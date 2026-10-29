package server

import (
	"bytes"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/levitskyyy/KremenMC/api"
)

type task struct {
	id        uint64
	owner     string
	repeating bool
	cancelled atomic.Bool
	stop      func()
	sched     *scheduler
}

func (t *task) ID() uint64      { return t.id }
func (t *task) Owner() string   { return t.owner }
func (t *task) Repeating() bool { return t.repeating }
func (t *task) Cancelled() bool { return t.cancelled.Load() }
func (t *task) Cancel() {
	if t.cancelled.Swap(true) {
		return
	}
	if t.stop != nil {
		t.stop()
	}
	t.sched.forget(t)
}

type pending struct {
	t      *task
	due    int64
	period int64
	run    func(*task)
}

// tickQueue holds tasks executed by a tick-driven owner (global, a player).
type tickQueue struct {
	mu    sync.Mutex
	items []pending
	spare []pending
}

func (q *tickQueue) add(p pending) {
	q.mu.Lock()
	q.items = append(q.items, p)
	q.mu.Unlock()
}

// run executes due tasks; reschedule re-queues repeating ones.
func (q *tickQueue) run(now int64) {
	q.mu.Lock()
	cur := q.items
	q.items = q.spare[:0]
	q.mu.Unlock()
	var keep []pending
	for _, p := range cur {
		if p.t.Cancelled() {
			continue
		}
		if p.due > now {
			keep = append(keep, p)
			continue
		}
		p.run(p.t)
		if p.t.repeating && !p.t.Cancelled() {
			p.due = now + max(p.period, 1)
			keep = append(keep, p)
		} else {
			p.t.sched.forget(p.t)
		}
	}
	q.mu.Lock()
	q.items = append(q.items, keep...)
	q.spare = cur[:0]
	q.mu.Unlock()
}

// drop cancels everything queued, calling retired hooks.
func (q *tickQueue) drop(retired func(*task)) {
	q.mu.Lock()
	cur := q.items
	q.items = nil
	q.mu.Unlock()
	for _, p := range cur {
		if retired != nil && !p.t.Cancelled() {
			retired(p.t)
		}
		p.t.Cancel()
	}
}

type scheduler struct {
	s      *Server
	nextID atomic.Uint64
	global tickQueue
	mu     sync.Mutex
	tasks  map[uint64]*task
	gid    atomic.Uint64
}

func newScheduler(s *Server) *scheduler { return &scheduler{s: s, tasks: map[uint64]*task{}} }

func (s *scheduler) newTask(owner string, repeating bool) *task {
	t := &task{id: s.nextID.Add(1), owner: owner, repeating: repeating, sched: s}
	s.mu.Lock()
	s.tasks[t.id] = t
	s.mu.Unlock()
	return t
}

func (s *scheduler) forget(t *task) {
	s.mu.Lock()
	delete(s.tasks, t.id)
	s.mu.Unlock()
}

func (s *scheduler) CancelAll(owner string) {
	s.mu.Lock()
	var ts []*task
	for _, t := range s.tasks {
		if t.owner == owner {
			ts = append(ts, t)
		}
	}
	s.mu.Unlock()
	for _, t := range ts {
		t.Cancel()
	}
}

func (s *scheduler) safe(owner string, fn func()) {
	defer func() {
		if v := recover(); v != nil {
			s.s.log.Error("scheduled task panicked", "plugin", owner, "panic", v)
		}
	}()
	fn()
}

func (s *scheduler) queue(q *tickQueue, owner string, delay, period api.Ticks, repeating bool, fn func(api.Task), wrap func(func())) api.Task {
	t := s.newTask(owner, repeating)
	q.add(pending{t: t, due: s.s.CurrentTick() + int64(max(delay, 1)), period: int64(period), run: func(t *task) {
		call := func() { s.safe(owner, func() { fn(t) }) }
		if wrap != nil {
			wrap(call)
		} else {
			call()
		}
	}})
	return t
}

type queueSched struct {
	s    *scheduler
	q    *tickQueue
	wrap func(func())
}

func (g queueSched) Run(owner string, fn func(api.Task)) api.Task {
	return g.s.queue(g.q, owner, 0, 0, false, fn, g.wrap)
}

func (g queueSched) RunDelayed(owner string, delay api.Ticks, fn func(api.Task)) api.Task {
	return g.s.queue(g.q, owner, delay, 0, false, fn, g.wrap)
}

func (g queueSched) RunRepeating(owner string, delay, period api.Ticks, fn func(api.Task)) api.Task {
	return g.s.queue(g.q, owner, delay, period, true, fn, g.wrap)
}

func (s *scheduler) Global() api.TaskScheduler { return queueSched{s: s, q: &s.global} }

// Region tasks are timed on the global tick and then run inside the owning
// region's phase of the same tick.
func (s *scheduler) Region(w api.World, cx, cz int) api.TaskScheduler {
	wd, ok := w.(*World)
	if !ok {
		return s.Global()
	}
	x, z := cx<<4, cz<<4
	return queueSched{s: s, q: &s.global, wrap: func(f func()) {
		wd.w.Submit(x, z, func(_ *worldTick) { f() })
	}}
}

type entitySched struct {
	queueSched
	valid func() bool
}

func (e entitySched) RunWithRetired(owner string, delay api.Ticks, fn func(api.Task), retired func()) api.Task {
	return e.s.queue(e.q, owner, delay, 0, false, func(t api.Task) {
		if !e.valid() {
			if retired != nil {
				retired()
			}
			return
		}
		fn(t)
	}, nil)
}

func (s *scheduler) Entity(e api.Entity) api.EntityScheduler { return e.Scheduler() }

func (s *scheduler) Async() api.AsyncScheduler { return asyncSched{s} }

type asyncSched struct{ s *scheduler }

func (a asyncSched) RunNow(owner string, fn func(api.Task)) api.Task {
	t := a.s.newTask(owner, false)
	go func() {
		defer a.s.forget(t)
		if !t.Cancelled() {
			a.s.safe(owner, func() { fn(t) })
		}
	}()
	return t
}

func (a asyncSched) RunDelayed(owner string, delay time.Duration, fn func(api.Task)) api.Task {
	t := a.s.newTask(owner, false)
	tm := time.AfterFunc(delay, func() {
		defer a.s.forget(t)
		if !t.Cancelled() {
			a.s.safe(owner, func() { fn(t) })
		}
	})
	t.stop = func() { tm.Stop() }
	return t
}

func (a asyncSched) RunAtFixedRate(owner string, delay, period time.Duration, fn func(api.Task)) api.Task {
	t := a.s.newTask(owner, true)
	stop := make(chan struct{})
	t.stop = func() { close(stop) }
	go func() {
		select {
		case <-time.After(delay):
		case <-stop:
			return
		}
		tk := time.NewTicker(max(period, time.Millisecond))
		defer tk.Stop()
		for {
			a.s.safe(owner, func() { fn(t) })
			select {
			case <-tk.C:
			case <-stop:
				return
			}
		}
	}()
	return t
}

func (s *scheduler) IsGlobalThread() bool { return goid() == s.gid.Load() }

func (s *scheduler) IsOwnedByCurrentRegion(api.World, int, int) bool { return s.IsGlobalThread() }

func goid() uint64 {
	var buf [32]byte
	n := runtime.Stack(buf[:], false)
	b := bytes.TrimPrefix(buf[:n], []byte("goroutine "))
	if i := bytes.IndexByte(b, ' '); i > 0 {
		b = b[:i]
	}
	id, _ := strconv.ParseUint(string(b), 10, 64)
	return id
}
