package world

import (
	"container/heap"
	"context"
	"sync"
	"sync/atomic"
)

// pool is a fixed set of goroutines draining two queues: an unbounded FIFO
// of unpositioned jobs (saves), which always runs first, and chunk loads
// ordered by rank (lowest first, then submission order). Ranks are
// recomputed lazily when the generation reported by epoch changes.
type pool struct {
	mu     sync.Mutex
	cond   *sync.Cond
	q      []func()
	head   int
	loads  loadHeap
	seq    uint64
	ranked uint64 // epoch the load ranks were computed for
	closed bool
	wg     sync.WaitGroup
	n      int
	active atomic.Int32
	done   atomic.Int64

	rank  func(ChunkPos) int64
	epoch func() uint64
}

type loadItem struct {
	f    func()
	pos  ChunkPos
	ctx  context.Context
	rank int64
	seq  uint64
}

type loadHeap []loadItem

func (h loadHeap) Len() int { return len(h) }
func (h loadHeap) Less(i, k int) bool {
	if h[i].rank != h[k].rank {
		return h[i].rank < h[k].rank
	}
	return h[i].seq < h[k].seq
}
func (h loadHeap) Swap(i, k int) { h[i], h[k] = h[k], h[i] }
func (h *loadHeap) Push(x any)   { *h = append(*h, x.(loadItem)) }
func (h *loadHeap) Pop() any {
	old := *h
	it := old[len(old)-1]
	old[len(old)-1] = loadItem{}
	*h = old[:len(old)-1]
	return it
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

// submitLoad queues a chunk load ranked by pos. A load whose ctx is done
// ranks first so abandoned work drains quickly.
func (p *pool) submitLoad(pos ChunkPos, ctx context.Context, f func()) bool {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return false
	}
	p.seq++
	heap.Push(&p.loads, loadItem{f: f, pos: pos, ctx: ctx, rank: p.rankOf(pos, ctx), seq: p.seq})
	p.mu.Unlock()
	p.cond.Signal()
	return true
}

func (p *pool) rankOf(pos ChunkPos, ctx context.Context) int64 {
	if ctx != nil && ctx.Err() != nil {
		return -1
	}
	if p.rank == nil {
		return 0
	}
	return p.rank(pos)
}

// rerank recomputes load ranks if the epoch moved; p.mu must be held.
func (p *pool) rerank() {
	if p.epoch == nil || len(p.loads) == 0 {
		return
	}
	e := p.epoch()
	if e == p.ranked {
		return
	}
	p.ranked = e
	for i := range p.loads {
		p.loads[i].rank = p.rankOf(p.loads[i].pos, p.loads[i].ctx)
	}
	heap.Init(&p.loads)
}

func (p *pool) queued() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.q) - p.head + len(p.loads)
}

func (p *pool) run() {
	defer p.wg.Done()
	for {
		p.mu.Lock()
		for p.head == len(p.q) && len(p.loads) == 0 && !p.closed {
			p.cond.Wait()
		}
		var f func()
		if p.head < len(p.q) {
			f = p.q[p.head]
			p.q[p.head] = nil
			p.head++
			if p.head == len(p.q) {
				p.q, p.head = p.q[:0], 0
			}
		} else if len(p.loads) > 0 {
			p.rerank()
			f = heap.Pop(&p.loads).(loadItem).f
		} else {
			p.mu.Unlock()
			return
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
