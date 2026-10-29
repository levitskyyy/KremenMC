package selfcheck

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/tools/refbench/compare"
	"github.com/levitskyyy/KremenMC/tools/refbench/gens"
)

// PipelineBackend drives internal/world/gen directly: workers is the
// pipeline's worker count and dir holds its proto-chunk storage, so a
// restart resumes half-generated neighbours (status persisted).
type PipelineBackend struct {
	Label  string
	Seed   int64
	Dim    string
	Stages func() []gen.Stage
	// CacheSize bounds in-memory chunks when a directory is given
	// (0: pipeline default); small values exercise evict/reload.
	CacheSize int
	// Storage selects the on-disk format with a directory: "file" (gen's
	// per-chunk codec, default) or "anvil" (vanilla 26.3 region files).
	Storage string
}

// StageSets are the pipeline stage sets selectable by name (refbench
// selfcheck -backend pipeline -gen NAME). Generator packages add theirs.
var StageSets = map[string]func() []gen.Stage{
	"synthetic": func() []gen.Stage { return gen.SyntheticStages(gen.SyntheticOptions{}) },
}

// NewPipelineBackend returns the backend for a registered stage set.
func NewPipelineBackend(name string, seed int64, dim string) (*PipelineBackend, error) {
	f, ok := StageSets[name]
	if !ok {
		var have []string
		for k := range StageSets {
			have = append(have, k)
		}
		return nil, fmt.Errorf("selfcheck: unknown pipeline stage set %q (have %v)", name, have)
	}
	return &PipelineBackend{Label: name, Seed: seed, Dim: dim, Stages: f}, nil
}

// SyntheticBackend is the pipeline with its synthetic stage set.
func SyntheticBackend(seed int64, dim string) *PipelineBackend {
	return &PipelineBackend{Label: "synthetic", Seed: seed, Dim: dim, Stages: func() []gen.Stage {
		return gen.SyntheticStages(gen.SyntheticOptions{})
	}}
}

func (b *PipelineBackend) Name() string {
	if b.Storage == "anvil" {
		return "pipeline+" + b.Label + "+anvil"
	}
	return "pipeline+" + b.Label
}

func (b *PipelineBackend) Open(dir string, workers int) (Session, error) {
	dim, err := gens.Dimension(b.Dim)
	if err != nil {
		return nil, err
	}
	cfg := gen.Config{Seed: b.Seed, Dim: dim, Workers: workers, Stages: b.Stages(), CacheSize: b.CacheSize}
	var closer func() error
	if dir != "" {
		switch b.Storage {
		case "", "file":
			st, err := gen.NewFileStorage(filepath.Join(dir, "gen"), dim)
			if err != nil {
				return nil, err
			}
			cfg.Storage = st
		case "anvil":
			st := gen.NewAnvilStorage(filepath.Join(dir, "region"), dim)
			cfg.Storage, closer = st, st.Close
		default:
			return nil, fmt.Errorf("selfcheck: storage %q: want file or anvil", b.Storage)
		}
	}
	p, err := gen.New(cfg)
	if err != nil {
		if closer != nil {
			closer()
		}
		return nil, err
	}
	return &pipelineSession{p: p, dim: dim, closer: closer}, nil
}

type pipelineSession struct {
	p      *gen.Pipeline
	dim    world.Dimension
	closer func() error
}

func (s *pipelineSession) Load(ctx context.Context, p compare.Pos) error {
	_, err := s.p.Get(ctx, world.ChunkPos{X: p.X, Z: p.Z}, gen.StatusFull)
	return err
}

func (s *pipelineSession) Snapshot(ps []compare.Pos) (compare.Set, error) {
	set := compare.Set{}
	plains, _ := data.Canonical().BiomeByName("plains")
	for _, p := range ps {
		pc, err := s.p.Get(context.Background(), world.ChunkPos{X: p.X, Z: p.Z}, gen.StatusFull)
		if err != nil {
			return nil, fmt.Errorf("selfcheck: chunk %v: %w", p, err)
		}
		c := world.NewChunk(p.X, p.Z, s.dim, plains)
		pc.Promote(c)
		out := compare.FromWorld(c, gen.StatusFull.ID())
		out.AddEntities(rawEntities(pc.Entities))
		set[p] = out
	}
	return set, nil
}

func rawEntities(es []nbt.Compound) []anvil.RawEntity {
	var out []anvil.RawEntity
	for _, e := range es {
		r := anvil.RawEntity{Root: e}
		r.ID, _ = e["id"].(string)
		if l, ok := e["Pos"].(nbt.List); ok && len(l.Items) == 3 {
			for i, v := range l.Items {
				r.Pos[i], _ = v.(float64)
			}
		}
		out = append(out, r)
	}
	return out
}

func (s *pipelineSession) Save() error { return s.p.Flush() }
func (s *pipelineSession) Close() error {
	err := s.p.Close()
	if s.closer != nil {
		err = errors.Join(err, s.closer())
	}
	return err
}

// gen-synth runs the same pipeline behind world.World (one pipeline per
// world, GOMAXPROCS pipeline workers), for `refbench selfcheck -gen gen-synth`.
func init() {
	gens.Register("gen-synth", func(seed int64, dim string) world.Generator {
		d, err := gens.Dimension(dim)
		if err != nil {
			return nil
		}
		p, err := gen.New(gen.Config{Seed: seed, Dim: d, Stages: gen.SyntheticStages(gen.SyntheticOptions{})})
		if err != nil {
			return nil
		}
		return gen.WorldGenerator{P: p}
	})
}
