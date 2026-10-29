// Package refgen generates reference chunks with the official server: it
// boots a headless loopback instance under .refcache/<version>/worlds,
// freezes ticking, force-loads the requested chunks through console
// commands, waits for each to reach full status, saves and stops. Every run
// is recorded in manifest.json next to the world, including the order in
// which chunks were requested and the resulting status of every stored chunk,
// because vanilla feature placement can depend on neighbour generation order.
package refgen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/tools/refbench/chunkspec"
	"github.com/levitskyyy/KremenMC/tools/refbench/refserver"
)

type Options struct {
	Ref        *reftest.Ref
	Seed       int64
	Dim        string // overworld, the_nether, the_end (or minecraft:...)
	Spec       string
	Structures bool
	// Order is "sequential" (one target at a time, waiting until it is full;
	// reproducible) or "batch" (all targets force-loaded at once).
	Order        string
	Threads      int // worldgen worker threads (-Dmax.bg.threads); 0 = server default
	Memory       string
	Fresh        bool // delete an existing world first
	ChunkTimeout time.Duration
	Log          io.Writer
	Echo         io.Writer // console mirror
}

type Manifest struct {
	Version    string           `json:"version"`
	Seed       int64            `json:"seed"`
	Dimension  string           `json:"dimension"`
	Structures bool             `json:"structures"`
	World      string           `json:"world"`  // relative to the manifest
	Region     string           `json:"region"` // relative to the manifest
	Entities   string           `json:"entities"`
	Spawn      *refserver.Spawn `json:"spawn,omitempty"`
	Runs       []Run            `json:"runs"`
}

type Run struct {
	Started     time.Time         `json:"started"`
	Spec        string            `json:"spec"`
	Order       string            `json:"order"`
	Threads     int               `json:"threads"`
	Properties  map[string]string `json:"properties"`
	Setup       []string          `json:"setup"`       // console commands before the first request
	Preexisting []ChunkStatus     `json:"preexisting"` // stored chunks before the first request (world creation, spawn search, earlier runs)
	Targets     []chunkspec.Pos   `json:"targets"`
	Skipped     []chunkspec.Pos   `json:"skipped,omitempty"` // targets already full before this run
	Requests    []Request         `json:"requests"`
	Final       []ChunkStatus     `json:"final"` // every stored chunk after the run
	Entities    int               `json:"entityChunks"`
	DurationMs  int64             `json:"durationMs"`
}

// Request is one step of the generation order: the console commands sent
// and when the targets were observed loaded (ms since the run started).
type Request struct {
	Seq      int             `json:"seq"`
	Chunks   []chunkspec.Pos `json:"chunks"`
	Commands []string        `json:"commands"`
	IssuedMs int64           `json:"issuedMs"`
	LoadedMs []int64         `json:"loadedMs"`
}

type ChunkStatus struct {
	X      int32  `json:"x"`
	Z      int32  `json:"z"`
	Status string `json:"status"`
}

// DimID normalizes a dimension name to its resource location.
func DimID(dim string) string {
	if strings.Contains(dim, ":") {
		return dim
	}
	return "minecraft:" + dim
}

// DimName is the short dimension name used in directory names.
func DimName(dim string) string {
	_, p, ok := strings.Cut(dim, ":")
	if !ok {
		return dim
	}
	return p
}

// WorldDir is the instance directory for a seed and dimension.
func WorldDir(ref *reftest.Ref, seed int64, dim string, structures bool) string {
	name := fmt.Sprintf("%d-%s", seed, DimName(dim))
	if !structures {
		name += "-nostructures"
	}
	return filepath.Join(ref.Dir, "worlds", name)
}

func ManifestPath(dir string) string { return filepath.Join(dir, "manifest.json") }

func LoadManifest(dir string) (*Manifest, error) {
	b, err := os.ReadFile(ManifestPath(dir))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("refgen: %s: %w", ManifestPath(dir), err)
	}
	return &m, nil
}

func (m *Manifest) save(dir string) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := ManifestPath(dir) + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, ManifestPath(dir))
}

// SpawnPair returns the spawn as block x,z for chunkspec.
func (m *Manifest) SpawnPair() *[2]int {
	if m == nil || m.Spawn == nil {
		return nil
	}
	return &[2]int{m.Spawn.X, m.Spawn.Z}
}

func validDim(dim string) bool {
	switch DimID(dim) {
	case "minecraft:overworld", "minecraft:the_nether", "minecraft:the_end":
		return true
	}
	return false
}

// Generate runs the reference server for one batch of chunks and returns the
// updated manifest.
func Generate(ctx context.Context, o Options) (*Manifest, error) {
	if !validDim(o.Dim) {
		return nil, fmt.Errorf("refgen: unknown dimension %q", o.Dim)
	}
	if o.Order == "" {
		o.Order = "sequential"
	}
	if o.Order != "sequential" && o.Order != "batch" {
		return nil, fmt.Errorf("refgen: order %q: want sequential or batch", o.Order)
	}
	if o.ChunkTimeout <= 0 {
		o.ChunkTimeout = 5 * time.Minute
	}
	if o.Log == nil {
		o.Log = io.Discard
	}
	dim := DimID(o.Dim)
	dir := WorldDir(o.Ref, o.Seed, dim, o.Structures)
	if o.Fresh {
		if err := os.RemoveAll(dir); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	m, err := LoadManifest(dir)
	if errors.Is(err, os.ErrNotExist) {
		m = &Manifest{Version: reftest.Version, Seed: o.Seed, Dimension: dim, Structures: o.Structures}
	} else if err != nil {
		return nil, err
	} else if m.Seed != o.Seed || m.Dimension != dim || m.Structures != o.Structures {
		return nil, fmt.Errorf("refgen: %s belongs to seed %d %s structures=%v", dir, m.Seed, m.Dimension, m.Structures)
	}
	m.World = "world"
	m.Region = filepath.ToSlash(filepath.Join(m.World, "dimensions", "minecraft", DimName(dim), "region"))
	m.Entities = filepath.ToSlash(filepath.Join(m.World, "dimensions", "minecraft", DimName(dim), "entities"))
	if !chunkspec.NeedsSpawn(o.Spec) {
		if _, err := chunkspec.Parse(o.Spec, nil); err != nil {
			return nil, err
		}
	}

	cfg := refserver.Config{
		Classpath:  append([]string{o.Ref.Jar}, o.Ref.Libs...),
		Dir:        dir,
		Seed:       o.Seed,
		Structures: o.Structures,
		Memory:     o.Memory,
		Threads:    o.Threads,
		Echo:       o.Echo,
	}
	began := time.Now()
	run := Run{Started: began.UTC(), Spec: o.Spec, Order: o.Order, Threads: o.Threads}
	fmt.Fprintf(o.Log, "starting reference server in %s\n", dir)
	srv, err := refserver.Start(ctx, cfg)
	if err != nil {
		return nil, err
	}
	run.Properties = cfg.Properties(srv.Port)
	delete(run.Properties, "server-port")
	ok := false
	defer func() {
		if !ok {
			srv.Kill()
		}
	}()
	ms := func() int64 { return time.Since(began).Milliseconds() }

	// Freeze ticking so loaded chunks keep their generated state (no fluid
	// flow, random ticks, mob movement); chunk generation still proceeds.
	run.Setup = []string{"tick freeze", "gamerule random_tick_speed 0", "save-all flush"}
	out, err := srv.Exec(ctx, run.Setup...)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(o.Log, "setup: %s\n", strings.Join(out, " | "))
	if sp, err := refserver.ReadSpawn(filepath.Join(dir, m.World, "level.dat")); err == nil {
		m.Spawn = &sp
	} else if chunkspec.NeedsSpawn(o.Spec) {
		return nil, fmt.Errorf("refgen: spawn: %w", err)
	}
	targets, err := chunkspec.Parse(o.Spec, m.SpawnPair())
	if err != nil {
		return nil, err
	}
	run.Targets = targets
	regionDir := filepath.Join(dir, filepath.FromSlash(m.Region))
	if run.Preexisting, err = scanSettled(ctx, regionDir); err != nil {
		return nil, err
	}
	full := map[chunkspec.Pos]bool{}
	for _, c := range run.Preexisting {
		if c.Status == "minecraft:full" {
			full[chunkspec.Pos{X: c.X, Z: c.Z}] = true
		}
	}
	var todo []chunkspec.Pos
	for _, p := range targets {
		if full[p] {
			run.Skipped = append(run.Skipped, p)
		} else {
			todo = append(todo, p)
		}
	}
	fmt.Fprintf(o.Log, "%d targets, %d already full, %d stored chunks before the run\n", len(targets), len(run.Skipped), len(run.Preexisting))

	in := "execute in " + dim + " "
	loadCmd := func(p chunkspec.Pos) string { return fmt.Sprintf("%srun forceload add %d %d", in, p.X*16, p.Z*16) }
	probe := func(p chunkspec.Pos) string { return fmt.Sprintf("%sif loaded %d 0 %d", in, p.X*16, p.Z*16) }
	wait := func(req *Request) error {
		pending := slices.Clone(req.Chunks)
		idx := make([]int, len(pending))
		for i := range idx {
			idx[i] = i
		}
		req.LoadedMs = make([]int64, len(req.Chunks))
		deadline := time.Now().Add(o.ChunkTimeout * time.Duration(max(1, len(pending)/4)))
		for len(pending) > 0 {
			cmds := make([]string, len(pending))
			for i, p := range pending {
				cmds[i] = probe(p)
			}
			res, err := srv.Exec(ctx, cmds...)
			if err != nil {
				return err
			}
			if len(res) != len(cmds) {
				return fmt.Errorf("refgen: %d probe results for %d chunks: %q", len(res), len(cmds), res)
			}
			now := ms()
			var np []chunkspec.Pos
			var ni []int
			for i, r := range res {
				switch r {
				case "Test passed":
					req.LoadedMs[idx[i]] = now
				case "Test failed":
					np, ni = append(np, pending[i]), append(ni, idx[i])
				default:
					return fmt.Errorf("refgen: unexpected probe output %q", r)
				}
			}
			pending, idx = np, ni
			if len(pending) == 0 {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("refgen: timed out waiting for chunks %v", pending)
			}
			select {
			case <-time.After(100 * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			case <-srv.Exited():
				return refserver.ErrExited
			}
		}
		return nil
	}
	issue := func(chunks []chunkspec.Pos) error {
		req := Request{Seq: len(run.Requests), Chunks: chunks, IssuedMs: ms()}
		for _, p := range chunks {
			req.Commands = append(req.Commands, loadCmd(p))
		}
		res, err := srv.Exec(ctx, req.Commands...)
		if err != nil {
			return err
		}
		for _, r := range res {
			if !strings.Contains(r, "force loaded") {
				return fmt.Errorf("refgen: forceload: %q", r)
			}
		}
		if err := wait(&req); err != nil {
			return err
		}
		run.Requests = append(run.Requests, req)
		return nil
	}
	if o.Order == "batch" && len(todo) > 0 {
		if err := issue(todo); err != nil {
			return nil, err
		}
	} else {
		for i, p := range todo {
			if err := issue([]chunkspec.Pos{p}); err != nil {
				return nil, err
			}
			if (i+1)%16 == 0 || i+1 == len(todo) {
				fmt.Fprintf(o.Log, "  %d/%d full (%.1fs)\n", i+1, len(todo), time.Since(began).Seconds())
			}
		}
	}
	if out, err = srv.Exec(ctx, "save-all flush"); err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(out, func(s string) bool { return strings.Contains(s, "Saved the game") }) {
		return nil, fmt.Errorf("refgen: save-all flush: %q", out)
	}
	if err := srv.Stop(2 * time.Minute); err != nil {
		fmt.Fprintf(o.Log, "warning: %v\n", err)
	}
	ok = true
	if run.Final, err = scanStatuses(regionDir); err != nil {
		return nil, err
	}
	if ents, err := anvil.ListChunks(filepath.Join(dir, filepath.FromSlash(m.Entities))); err == nil {
		run.Entities = len(ents)
	}
	final := map[chunkspec.Pos]string{}
	for _, c := range run.Final {
		final[chunkspec.Pos{X: c.X, Z: c.Z}] = c.Status
	}
	for _, p := range targets {
		if final[p] != "minecraft:full" {
			return nil, fmt.Errorf("refgen: target %v saved with status %q", p, final[p])
		}
	}
	run.DurationMs = ms()
	m.Runs = append(m.Runs, run)
	if err := m.save(dir); err != nil {
		return nil, err
	}
	fmt.Fprintf(o.Log, "done: %d targets full, %d stored chunks, %.1fs; manifest %s\n", len(targets), len(run.Final), time.Since(began).Seconds(), ManifestPath(dir))
	return m, nil
}

// scanSettled scans while the server is running: region writes complete
// asynchronously after "save-all flush", so wait until the chunk count stops
// changing.
func scanSettled(ctx context.Context, regionDir string) ([]ChunkStatus, error) {
	var last []ChunkStatus
	var lastErr error
	stable := 0
	for range 60 {
		cur, err := scanStatuses(regionDir)
		lastErr = err
		if err == nil && len(cur) == len(last) && len(cur) > 0 {
			if stable++; stable >= 4 {
				return cur, nil
			}
		} else {
			stable = 0
		}
		last = cur
		select {
		case <-time.After(250 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return last, lastErr
}

func scanStatuses(regionDir string) ([]ChunkStatus, error) {
	cs, err := anvil.ListChunks(regionDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]ChunkStatus, 0, len(cs))
	for _, p := range cs {
		root, err := anvil.ReadRawNBT(regionDir, p[0], p[1])
		if err != nil {
			return nil, err
		}
		st, _ := root["Status"].(string)
		out = append(out, ChunkStatus{X: p[0], Z: p[1], Status: st})
	}
	slices.SortFunc(out, func(a, b ChunkStatus) int {
		if a.Z != b.Z {
			return int(a.Z - b.Z)
		}
		return int(a.X - b.X)
	})
	return out, nil
}
