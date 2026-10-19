package zerotier

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// MemoryStore is useful for temporary nodes and tests. It does NOT preserve
// network authorization identity across process restarts.
type MemoryStore struct {
	mu   sync.Mutex
	data map[StateKey][]byte
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{data: make(map[StateKey][]byte)} }
func (s *MemoryStore) Get(k StateKey) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.data[k]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), b...), nil
}
func (s *MemoryStore) Put(k StateKey, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[k] = append([]byte(nil), b...)
	return nil
}
func (s *MemoryStore) Delete(k StateKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, k)
	return nil
}

// FileStore provides per-object atomic replacement with private permissions.
// A state directory MUST belong to only one running Node instance.
type FileStore struct {
	mu  sync.Mutex
	Dir string
}

func NewFileStore(dir string) (*FileStore, error) {
	if dir == "" {
		return nil, fmt.Errorf("zerotier: empty state directory")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &FileStore{Dir: dir}, nil
}
func (s *FileStore) path(k StateKey) string {
	return filepath.Join(s.Dir, fmt.Sprintf("%d-%016x-%016x.state", k.Type, k.ID[0], k.ID[1]))
}
func (s *FileStore) Get(k StateKey) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.path(k))
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	return b, err
}
func (s *FileStore) Put(k StateKey, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.CreateTemp(s.Dir, ".zt-state-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if syncErr := f.Sync(); err == nil {
		err = syncErr
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, s.path(k))
}
func (s *FileStore) Delete(k StateKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.path(k))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}