// Package ai holds the server-independent parts of mob AI: the goal
// selector, look/move/jump controls, block-grid pathfinding and path
// following. The concrete goals live with the server (internal/server
// mob_ai*.go) because they read and act on the world.
package ai

// Flag is a control channel a goal claims while it runs; two running goals
// never share a flag.
type Flag uint8

const (
	FlagMove Flag = 1 << iota
	FlagLook
	FlagJump
	FlagTarget
)

// Goal is one behaviour. CanUse is polled while the goal is idle and
// CanContinue while it runs; Tick runs every tick while it runs.
type Goal interface {
	CanUse() bool
	CanContinue() bool
	Start()
	Stop()
	Tick()
}

// Uninterruptible goals keep their flags against higher-priority goals
// (creeper swelling, a running panic).
type Uninterruptible interface{ Uninterruptible() bool }

// Base gives a goal default no-op Start/Stop/Tick.
type Base struct{}

func (Base) Start() {}
func (Base) Stop()  {}
func (Base) Tick()  {}

type entry struct {
	prio    int
	flags   Flag
	g       Goal
	running bool
}

// Selector runs the goals of one selector (behaviour or target). Lower
// priority numbers win; a goal may start when each of its flags is free or
// held by an interruptible goal with a strictly larger priority number,
// which is then stopped. Idle goals are polled every evalEvery ticks,
// running goals are checked and ticked every tick.
type Selector struct {
	goals    []entry
	owner    [4]int8 // goal index holding each flag, -1 free
	disabled Flag
	n        uint32
}

const evalEvery = 2

func (s *Selector) Add(prio int, flags Flag, g Goal) {
	if len(s.goals) == 0 {
		s.owner = [4]int8{-1, -1, -1, -1}
	}
	i := len(s.goals)
	for i > 0 && s.goals[i-1].prio > prio {
		i--
	}
	s.goals = append(s.goals, entry{})
	copy(s.goals[i+1:], s.goals[i:])
	s.goals[i] = entry{prio: prio, flags: flags, g: g}
	for f := range s.owner {
		if int(s.owner[f]) >= i {
			s.owner[f]++
		}
	}
}

// Disable blocks goals using f (and stops running ones) until Enable.
func (s *Selector) Disable(f Flag) { s.disabled |= f }
func (s *Selector) Enable(f Flag)  { s.disabled &^= f }

// Running reports whether g is currently running.
func (s *Selector) Running(g Goal) bool {
	for i := range s.goals {
		if s.goals[i].g == g {
			return s.goals[i].running
		}
	}
	return false
}

// Each calls fn for every running goal.
func (s *Selector) Each(fn func(g Goal)) {
	for i := range s.goals {
		if s.goals[i].running {
			fn(s.goals[i].g)
		}
	}
}

func (s *Selector) stop(i int) {
	e := &s.goals[i]
	if !e.running {
		return
	}
	e.running = false
	e.g.Stop()
	for f := range s.owner {
		if int(s.owner[f]) == i {
			s.owner[f] = -1
		}
	}
}

// StopAll stops every running goal.
func (s *Selector) StopAll() {
	for i := range s.goals {
		s.stop(i)
	}
}

func bits(f Flag) func(func(int) bool) {
	return func(yield func(int) bool) {
		for b := 0; b < 4; b++ {
			if f&(1<<b) != 0 && !yield(b) {
				return
			}
		}
	}
}

func (s *Selector) canTake(i int) bool {
	e := &s.goals[i]
	if e.flags&s.disabled != 0 {
		return false
	}
	for b := range bits(e.flags) {
		o := s.owner[b]
		if o < 0 {
			continue
		}
		h := &s.goals[o]
		if h.prio <= e.prio {
			return false
		}
		if u, ok := h.g.(Uninterruptible); ok && u.Uninterruptible() {
			return false
		}
	}
	return true
}

// Tick stops goals that can no longer run, starts eligible idle goals and
// ticks the running ones.
func (s *Selector) Tick() {
	s.n++
	for i := range s.goals {
		e := &s.goals[i]
		if e.running && (e.flags&s.disabled != 0 || !e.g.CanContinue()) {
			s.stop(i)
		}
	}
	if s.n%evalEvery == 0 {
		for i := range s.goals {
			e := &s.goals[i]
			if e.running || !s.canTake(i) || !e.g.CanUse() {
				continue
			}
			for b := range bits(e.flags) {
				if o := s.owner[b]; o >= 0 {
					s.stop(int(o))
				}
				s.owner[b] = int8(i)
			}
			e.running = true
			e.g.Start()
		}
	}
	for i := range s.goals {
		if s.goals[i].running {
			s.goals[i].g.Tick()
		}
	}
}
