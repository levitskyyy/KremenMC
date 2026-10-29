package plugin

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/sdk/go/wire"
)

// FileKV is a small persistent map stored as one file and rewritten
// atomically on every mutation. It is meant for plugin settings and state,
// not bulk data.
type FileKV struct {
	path     string
	maxBytes int

	mu   sync.Mutex
	data map[string][]byte
	size int
}

var ErrKVFull = errors.New("plugin: kv store quota exceeded")

func OpenFileKV(path string, maxBytes int) (*FileKV, error) {
	kv := &FileKV{path: path, maxBytes: maxBytes, data: map[string][]byte{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return kv, nil
	}
	if err != nil {
		return nil, fmt.Errorf("kv: %w", err)
	}
	r := wire.NewReader(raw)
	n := r.Count()
	for i := 0; i < n && r.Err == nil; i++ {
		k, v := r.String(), r.Bytes()
		kv.data[k] = v
		kv.size += len(k) + len(v)
	}
	if r.Err != nil {
		return nil, fmt.Errorf("kv %s: corrupt: %w", path, r.Err)
	}
	return kv, nil
}

func (kv *FileKV) Get(key string) ([]byte, bool, error) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	v, ok := kv.data[key]
	return slices.Clone(v), ok, nil
}

func (kv *FileKV) Set(key string, value []byte) error {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	old, had := kv.data[key]
	size := kv.size + len(value) - len(old)
	if !had {
		size += len(key)
	}
	if kv.maxBytes > 0 && size > kv.maxBytes {
		return ErrKVFull
	}
	kv.data[key] = slices.Clone(value)
	kv.size = size
	return kv.flushLocked()
}

func (kv *FileKV) Delete(key string) error {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	v, ok := kv.data[key]
	if !ok {
		return nil
	}
	delete(kv.data, key)
	kv.size -= len(key) + len(v)
	return kv.flushLocked()
}

func (kv *FileKV) Keys(prefix string) ([]string, error) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	var out []string
	for k := range kv.data {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out, nil
}

func (kv *FileKV) flushLocked() error {
	var w wire.Writer
	keys := sortedKeys(kv.data)
	w.Uvarint(uint64(len(keys)))
	for _, k := range keys {
		w.String(k)
		w.Bytes(kv.data[k])
	}
	if err := os.MkdirAll(filepath.Dir(kv.path), 0o755); err != nil {
		return fmt.Errorf("kv: %w", err)
	}
	return writeFileAtomic(kv.path, w.B)
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
