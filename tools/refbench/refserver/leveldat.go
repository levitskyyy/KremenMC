package refserver

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/levitskyyy/KremenMC/internal/nbt"
)

type Spawn struct {
	X, Y, Z int
	Yaw     float32
}

// ReadSpawn reads the world spawn from a vanilla level.dat: Data.spawn
// {pos: [I; x, y, z], yaw} since 1.21.9, SpawnX/Y/Z before.
func ReadSpawn(path string) (Spawn, error) {
	f, err := os.Open(path)
	if err != nil {
		return Spawn{}, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return Spawn{}, fmt.Errorf("refserver: %s: %w", path, err)
	}
	raw, err := io.ReadAll(io.LimitReader(zr, 64<<20))
	if err != nil {
		return Spawn{}, fmt.Errorf("refserver: %s: %w", path, err)
	}
	_, root, _, err := nbt.Unmarshal(raw, false)
	if err != nil {
		return Spawn{}, fmt.Errorf("refserver: %s: %w", path, err)
	}
	d, _ := root["Data"].(nbt.Compound)
	if sp, ok := d["spawn"].(nbt.Compound); ok {
		if pos, ok := sp["pos"].([]int32); ok && len(pos) == 3 {
			yaw, _ := sp["yaw"].(float32)
			return Spawn{X: int(pos[0]), Y: int(pos[1]), Z: int(pos[2]), Yaw: yaw}, nil
		}
	}
	x, ok1 := d["SpawnX"].(int32)
	y, ok2 := d["SpawnY"].(int32)
	z, ok3 := d["SpawnZ"].(int32)
	if ok1 && ok2 && ok3 {
		yaw, _ := d["SpawnAngle"].(float32)
		return Spawn{X: int(x), Y: int(y), Z: int(z), Yaw: yaw}, nil
	}
	return Spawn{}, errors.New("refserver: level.dat has no spawn")
}
