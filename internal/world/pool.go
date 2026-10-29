package world

import (
	"sync"
	"sync/atomic"
)

// pool is a fixed set of goroutines draining an unbounded FIFO.
type pool struct {
	mu     sync.Mutex
	cond   *sync.Cond
	q      []func()
	head   int
	closed bool
	wg     sync.WaitGroup
	n      int
	active atomic.Int32
	done   atomic.Int64
}

func newPool(n int) *pool {
	p := &pool{n: n}
	p.cond = sync.NewCond(&p.mu)
	p.wg.Add(n)
	for range n {
		go p.run()
	}
	return p
}

func (p *pool) submit(f func()) bool {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return false
	}
	p.q = append(p.q, f)
	p.mu.Unlock()
	p.cond.Signal()
	return true
}

func (p *pool) queued() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.q) - p.head
}

func (p *pool) run() {
	defer p.wg.Done()
	for {
		p.mu.Lock()
		for p.head == len(p.q) && !p.closed {
			p.cond.Wait()
		}
		if p.head == len(p.q) {
			p.mu.Unlock()
			return
		}
		f := p.q[p.head]
		p.q[p.head] = nil
		p.head++
		if p.head == len(p.q) {
			p.q, p.head = p.q[:0], 0
		}
		p.mu.Unlock()
		p.active.Add(1)
		f()
		p.active.Add(-1)
		p.done.Add(1)
	}
}

// close drains queued jobs and waits for the workers to exit.
func (p *pool) close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.cond.Broadcast()
	p.wg.Wait()
}
