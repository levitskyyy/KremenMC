package anvil

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// Storage implements world.Storage over a vanilla region directory
// (e.g. world/region). Safe for concurrent use.
type Storage struct {
	Dir         string
	Dim         world.Dimension
	Compression byte
	ReadOnly    bool
	// GameTime supplies LastUpdate for saved chunks; may be nil.
	GameTime func() int64
	// EntitiesDir holds entities/ region files for full chunks saved with
	// SaveProto; default <Dir>/../entities.
	EntitiesDir string

	mu      sync.Mutex
	regions map[[2]int32]*regionRef
	// missing remembers region files found absent, so loads of ungenerated
	// chunks skip the open syscall. Only this Storage creates files in a
	// writable Dir; read-only storage never caches absence.
	missing map[[2]int32]struct{}
	ents    *Storage
	clock   uint64
	maxOpen int
}

type regionRef struct {
	r    *Region
	refs int
	used uint64
}

func NewStorage(dir string, dim world.Dimension) *Storage {
	return &Storage{Dir: dir, Dim: dim, Compression: CompressionZlib, regions: map[[2]int32]*regionRef{}, maxOpen: 64}
}

func (s *Storage) acquire(rx, rz int32, create bool) (*Region, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := [2]int32{rx, rz}
	s.clock++
	if ref, ok := s.regions[k]; ok {
		ref.refs++
		ref.used = s.clock
		return ref.r, nil
	}
	if _, ok := s.missing[k]; ok && !create {
		return nil, os.ErrNotExist
	}
	if create {
		if err := os.MkdirAll(s.Dir, 0o755); err != nil {
			return nil, err
		}
	}
	var r *Region
	var err error
	if s.ReadOnly {
		r, err = OpenRegionReadOnly(s.Dir, rx, rz)
	} else {
		r, err = OpenRegion(s.Dir, rx, rz, create)
	}
	if err != nil {
		if !create && !s.ReadOnly && errors.Is(err, os.ErrNotExist) {
			if s.missing == nil {
				s.missing = map[[2]int32]struct{}{}
			}
			s.missing[k] = struct{}{}
		}
		return nil, err
	}
	delete(s.missing, k)
	s.evict()
	s.regions[k] = &regionRef{r: r, refs: 1, used: s.clock}
	return r, nil
}

func (s *Storage) evict() {
	for len(s.regions) >= s.maxOpen {
		var oldest [2]int32
		var best *regionRef
		for k, ref := range s.regions {
			if ref.refs == 0 && (best == nil || ref.used < best.used) {
				oldest, best = k, ref
			}
		}
		if best == nil {
			return
		}
		best.r.Close()
		delete(s.regions, oldest)
	}
}

func (s *Storage) release(rx, rz int32) {
	s.mu.Lock()
	if ref, ok := s.regions[[2]int32{rx, rz}]; ok {
		ref.refs--
	}
	s.mu.Unlock()
}

// LoadChunk returns nil, nil when the chunk is absent or not fully generated.
func (s *Storage) LoadChunk(p world.ChunkPos) (*world.Chunk, error) {
	rx, rz := p.X>>5, p.Z>>5
	r, err := s.acquire(rx, rz, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer s.release(rx, rz)
	raw, err := r.Read(int(p.X&31), int(p.Z&31))
	if err != nil || raw == nil {
		return nil, err
	}
	_, root, _, err := nbt.Unmarshal(raw, false)
	if err != nil {
		return nil, fmt.Errorf("anvil: chunk %d,%d: %w", p.X, p.Z, err)
	}
	c, _, err := DecodeChunk(root, s.Dim)
	if err != nil {
		return nil, err
	}
	if c != nil && (c.X != p.X || c.Z != p.Z) {
		return nil, fmt.Errorf("anvil: chunk at %d,%d claims %d,%d", p.X, p.Z, c.X, c.Z)
	}
	return c, nil
}

// SaveChunk encodes c under its read lock and writes it.
func (s *Storage) SaveChunk(c *world.Chunk) error {
	if s.ReadOnly {
		return nil
	}
	var t int64
	if s.GameTime != nil {
		t = s.GameTime()
	}
	c.RLock()
	root := EncodeChunk(c, t)
	c.RUnlock()
	buf, err := nbt.Marshal(nil, "", root, false)
	if err != nil {
		return fmt.Errorf("anvil: encode chunk %d,%d: %w", c.X, c.Z, err)
	}
	rx, rz := c.X>>5, c.Z>>5
	r, err := s.acquire(rx, rz, true)
	if err != nil {
		return err
	}
	defer s.release(rx, rz)
	return r.Write(int(c.X&31), int(c.Z&31), buf, s.Compression)
}

func (s *Storage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	if s.ents != nil {
		errs = append(errs, s.ents.Close())
		s.ents = nil
	}
	for k, ref := range s.regions {
		if err := ref.r.Sync(); err != nil {
			errs = append(errs, err)
		}
		if err := ref.r.Close(); err != nil {
			errs = append(errs, err)
		}
		delete(s.regions, k)
	}
	return errors.Join(errs...)
}
