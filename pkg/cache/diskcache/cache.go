package diskcache

import (
	"container/list"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

type Cache struct {
	baseDir      string
	items        map[Key]*list.Element
	order        *list.List
	maxSize      int64
	currentSize  int64
	maxEntrySize int64
	mu           sync.Mutex
	fileMode     *os.FileMode
	hashFunc     func([]byte) (uint64, error)
	onEviction   func(key Key)
}
type Key struct {
	Key  string
	Path string
}

type entry struct {
	key          Key
	size         int64
	contentHash  uint64
	valid        bool
	refCount     int
	pendingEvict bool
}

type WalkFunc func(path string) (key Key, ok bool)

type WarmUpError struct {
	Errs []error
}

func (e *WarmUpError) Error() string {
	errStrs := make([]string, len(e.Errs))
	for i, err := range e.Errs {
		errStrs[i] = err.Error()
	}
	return fmt.Sprintf("warm up encountered errors: %s", strings.Join(errStrs, "; "))
}

var (
	ErrEntryTooLarge = errors.New("entry size exceeds maximum allowed size")
	ErrCacheFull     = errors.New("cache is full, unable to add new entry")
)

type CacheOption func(*Cache)

func WithHashFunc(hashFunc func([]byte) (uint64, error)) CacheOption {
	return func(c *Cache) {
		c.hashFunc = hashFunc
	}
}

func WithMaxEntrySize(maxEntrySize int64) CacheOption {
	return func(c *Cache) {
		c.maxEntrySize = maxEntrySize
	}
}

func WithFileMode(mode os.FileMode) CacheOption {
	return func(c *Cache) {
		c.fileMode = &mode
	}
}

func WithOnEviction(callback func(key Key)) CacheOption {
	return func(c *Cache) {
		c.onEviction = callback
	}
}

func NewCache(baseDir string, maxSize int64, opts ...CacheOption) (*Cache, error) {
	if maxSize <= 0 {
		return nil, fmt.Errorf("maxSize must be greater than 0")
	}
	info, err := os.Stat(baseDir)
	switch {
	case err == nil:
		if !info.IsDir() {
			return nil, fmt.Errorf("baseDir %q is a file, not a directory", baseDir)
		}
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(baseDir, 0o750); err != nil {
			return nil, fmt.Errorf("failed to create baseDir: %w", err)
		}
	default:
		return nil, fmt.Errorf("failed to stat baseDir: %w", err)
	}
	c := &Cache{
		baseDir: baseDir,
		items:   make(map[Key]*list.Element),
		order:   list.New(),
		maxSize: maxSize,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Set adds a new entry to cache or updates an existing entry size of bytes <= maxEntrySize.
// if current size > max size before add or updating , it will attempt to reconcile by evicting old entries until the cache is under budget
// if it cannot be reconciled, the new entry will not be added and an error will be returned.
func (c *Cache) Set(key Key, bytes []byte) error {
	c.mu.Lock()
	var evicted []Key
	if elem, ok := c.items[key]; ok {
		err := c.update(elem, key, bytes, key.Path, &evicted)
		c.mu.Unlock()
		c.notifyEvicted(evicted)
		return err
	}
	err := c.add(key.Path, key, bytes, &evicted)
	c.mu.Unlock()
	c.notifyEvicted(evicted)
	return err
}

func (c *Cache) notifyEvicted(evicted []Key) {
	if c.onEviction != nil {
		for _, evictedKey := range evicted {
			c.onEviction(evictedKey)
		}
	}
}

func (c *Cache) overBudget(evicted *[]Key) error {
	if c.availableSpace() <= 0 {
		c.evictFromBack(c.maxSize, nil, evicted)
		if c.availableSpace() <= 0 {
			return ErrCacheFull
		}
	}
	return nil
}

// updates an existing entry in the cache with new bytes.
// if the new bytes size exceeds maxEntrySize, the entry will be removed and an error will be returned.
// before update if the cache is over budget (current size > max size), it will attempt to evict old entries to make room for the update.
// if it cannot be reconciled, the entry will be removed and an error will be returned.
func (c *Cache) update(elem *list.Element, key Key, bytes []byte, path string, evicted *[]Key) error {
	size := int64(len(bytes))
	entry, ok := elem.Value.(*entry)
	if ok {
		if size > c.maxEntrySizeEffective() {
			c.removeEntry(entry, evicted)
			return ErrEntryTooLarge
		}
		c.order.MoveToFront(elem)
		sizeDiff := size - entry.size
		if skip := c.skipWrite(bytes, entry.contentHash); skip {
			return nil
		}

		if c.currentSize+sizeDiff > c.maxSize && sizeDiff > 0 {
			c.evictFromBack(c.maxSize-sizeDiff, elem, evicted)
			if c.currentSize+sizeDiff > c.maxSize {
				c.removeEntry(entry, evicted)
				return ErrCacheFull
			}
		}
		c.setHash(entry, bytes)
		if err := c.writeToDisk(path, bytes); err != nil {
			return err
		}
		if _, ok := c.items[key]; !ok {
			return c.add(path, key, bytes, evicted)
		}

		entry.valid = true
		c.currentSize += sizeDiff
		entry.pendingEvict = false
		entry.size = size
		c.evictFromBack(c.maxSize, elem, evicted)
		return nil
	}
	return nil
}

func (c *Cache) Get(key Key) (found bool, bytes []byte) {
	var evicted []Key
	c.mu.Lock()
	elem, ok := c.items[key]
	if !ok {
		c.mu.Unlock()
		return false, nil
	}

	e, _ := elem.Value.(*entry)
	if !e.valid {
		c.removeEntry(e, &evicted)
		c.mu.Unlock()
		c.notifyEvicted(evicted)
		return false, nil
	}
	c.order.MoveToFront(elem)
	e.refCount++
	c.mu.Unlock()
	// lock free read from disk, if the entry is evicted while reading, the pendingEvict flag will ensure it gets cleaned up after the read completes
	found, bytes = c.readEntry(key.Path)
	c.mu.Lock()
	e.refCount--
	if e.pendingEvict && e.refCount == 0 {
		c.removeEntry(e, &evicted)
	}
	c.mu.Unlock()
	c.notifyEvicted(evicted)
	return found, bytes
}

func (c *Cache) readEntry(filePath string) (bool, []byte) {
	fullPath := filepath.Join(c.baseDir, filePath)
	f, err := os.Stat(fullPath)
	if err != nil {
		return false, nil
	}
	if f.IsDir() {
		return false, nil
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return false, nil
	}
	return true, content
}

func (c *Cache) writeToDisk(path string, bytes []byte) error {
	fullPath := filepath.Join(c.baseDir, path)
	dir := filepath.Dir(fullPath)
	err := os.MkdirAll(dir, 0o755)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck

	_, err = tmp.Write(bytes)
	if err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if c.fileMode != nil {
		if err := tmp.Chmod(*c.fileMode); err != nil {
			tmp.Close() //nolint:errcheck
			return err
		}
	}

	if err := tmp.Sync(); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}

	if err := tmp.Close(); err != nil {
		return err
	}

	return atomicRename(tmp.Name(), fullPath)
}

func (c *Cache) add(path string, key Key, bytes []byte, evicted *[]Key) error {
	size := int64(len(bytes))
	if size > c.maxEntrySizeEffective() {
		return ErrEntryTooLarge
	}
	if err := c.overBudget(evicted); err != nil {
		return err
	}

	if err := c.writeToDisk(path, bytes); err != nil {
		return err
	}
	entry := &entry{key: key, size: size, valid: true}
	c.setHash(entry, bytes)
	elem := c.order.PushFront(entry)
	c.items[key] = elem
	c.currentSize += size
	c.evictFromBack(c.maxSize, elem, evicted)
	return nil
}

func (c *Cache) skipWrite(bytes []byte, expectedHash uint64) bool {
	if c.hashFunc == nil {
		return false
	}
	if expectedHash == 0 {
		return false
	}
	hash, err := c.hashFunc(bytes)
	if err != nil {
		return false
	}
	return hash == expectedHash
}

func (c *Cache) setHash(entry *entry, bytes []byte) {
	if c.hashFunc == nil {
		return
	}
	hash, err := c.hashFunc(bytes)
	if err != nil {
		return
	}
	entry.contentHash = hash
}

func (c *Cache) maxEntrySizeEffective() int64 {
	if c.maxEntrySize > 0 {
		return c.maxEntrySize
	}
	return c.maxSize
}

// evictFromBack attempts to remove the least recently used items from the cache until the cache is under its max size limit.
// If the stop element is provided, it will only evict items that are behind the stop element, allowing for eviction of all items up to but not including the stop element
func (c *Cache) evictFromBack(target int64, stop *list.Element, evicted *[]Key) {
	back := c.order.Back()
	var prev *list.Element
	for back != nil && c.currentSize > target {
		if back == stop {
			return
		}
		prev = back.Prev()
		entry, ok := back.Value.(*entry)
		if !ok {
			c.order.Remove(back)
			back = prev
			continue
		}
		if !entry.valid {
			back = prev
			continue
		}
		c.removeEntry(entry, evicted)
		back = prev
	}
}

// removeEntry removes the given entry from the cache and deletes the corresponding file from disk.
// if the file cannot be deleted, the entry is marked as invalid and will attempt deletion on the next access. not thread-safe,
// should be called with the cache's mutex locked.
func (c *Cache) removeEntry(entry *entry, evicted *[]Key) {
	if entry == nil {
		return
	}

	if entry.refCount > 0 {
		entry.pendingEvict = true
		return
	}
	fullPath := filepath.Join(c.baseDir, entry.key.Path)
	err := os.Remove(fullPath)
	if err != nil && !os.IsNotExist(err) {
		entry.valid = false
		return
	}
	if evicted != nil {
		*evicted = append(*evicted, entry.key)
	}

	if elem, ok := c.items[entry.key]; ok {
		c.currentSize -= entry.size
		c.order.Remove(elem)
		delete(c.items, entry.key)
	}
}

func (c *Cache) availableSpace() int64 {
	return c.maxSize - c.currentSize
}

// Pin marks a cache entry as in-use and returns its on-disk path.
// The entry cannot be evicted until Release is called.
// Caution: the caller must call the release function when it is done with the entry to avoid memory leaks.
// Caution: the file content may be modified on disk after Pin returns, so the caller should not assume the content is immutable or consistent with the content at the time of the Pin call.
// Use Get if you need a consistent snapshot of the content. Pin is intended for use cases where the caller needs to access the file on disk and is able to handle potential modifications to the file content, such as by re-reading the file after a modification is detected.
// Returns ok=false if the key is not in the cache.
func (c *Cache) Pin(key Key) (path string, release func(), ok bool) {
	var evicted []Key
	c.mu.Lock()
	elem, exists := c.items[key]
	if !exists {
		c.mu.Unlock()
		return "", nil, false
	}

	e, _ := elem.Value.(*entry)
	if !e.valid {
		c.removeEntry(e, &evicted)
		c.mu.Unlock()
		c.notifyEvicted(evicted)
		return "", nil, false
	}

	c.order.MoveToFront(elem)
	e.refCount++
	c.mu.Unlock()
	c.notifyEvicted(evicted)
	fullPath := filepath.Join(c.baseDir, key.Path)
	var released atomic.Bool
	release = func() {
		if !released.CompareAndSwap(false, true) {
			return
		}
		var releaseEvicted []Key
		c.mu.Lock()
		e.refCount--
		if e.pendingEvict && e.refCount == 0 {
			c.removeEntry(e, &releaseEvicted)
		}
		c.mu.Unlock()
		c.notifyEvicted(releaseEvicted)
	}

	return fullPath, release, true
}

// Delete removes the entry associated with the given key from the cache and deletes the corresponding file from disk.
// Returns true if the entry was found and deleted, false if the key was not found in the cache.
// If the file cannot be deleted, the entry will be marked as invalid and will attempt deletion on the next access.
func (c *Cache) Delete(key Key) bool {
	c.mu.Lock()
	elem, ok := c.items[key]
	if !ok {
		c.mu.Unlock()
		return false
	}
	var evicted []Key
	e, _ := elem.Value.(*entry)
	c.removeEntry(e, &evicted)
	c.mu.Unlock()
	c.notifyEvicted(evicted)
	return true
}

// Adopt attempts to take ownership of an existing file on disk and add it to the cache under the given key, without copying the file content.
// This is useful for cases where the file content is already on disk and can be added to the cache without needing to be written again,
// such as when the file is generated by an external process or when migrating existing files into the cache.
// note: sizing rules on entry's and cache do not apply with this function. so no eviction will occur and the entry will be added even if it exceeds maxEntrySize or maxSize.
func (c *Cache) Adopt(key Key) {
	fullPath := filepath.Join(c.baseDir, key.Path)
	info, err := os.Stat(fullPath)
	if err != nil {
		return
	}
	size := info.Size()
	c.mu.Lock()
	c.adopt(key, size)
	c.mu.Unlock()
}

func (c *Cache) adopt(key Key, size int64) {
	if elem, ok := c.items[key]; ok {
		e, ok := elem.Value.(*entry)
		if ok && !e.valid {
			c.order.MoveToFront(elem)
			e.valid = true
			c.currentSize -= e.size
			c.currentSize += size
		}
		return
	}

	entry := &entry{key: key, size: size, valid: true}
	elem := c.order.PushFront(entry)
	c.items[key] = elem
	c.currentSize += size
}

// WarmUp walks the cache's base directory and attempts to adopt files into the cache using the provided walkFunc to determine the corresponding cache key for each file.
// This is useful for pre-populating the cache with existing files on disk, such as after a restart or when initializing the cache with pre-generated content.
// The walkFunc should return the cache key corresponding to the given file path, and a boolean indicating whether the file should be adopted into the cache (true) or skipped (false).
func (c *Cache) WarmUp(walkFunc WalkFunc) error {
	var warmUpErrs []error
	c.mu.Lock()
	defer c.mu.Unlock()
	err := filepath.WalkDir(c.baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // skip inaccessible entries without aborting the walk
		}
		relativePath, err := filepath.Rel(c.baseDir, path)
		if err != nil {
			return nil //nolint:nilerr // skip unparseable paths without aborting the walk
		}
		key, ok := walkFunc(relativePath)
		if !ok {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil //nolint:nilerr // skip files whose info can't be read
		}
		c.adopt(key, info.Size())
		return nil
	})
	if err != nil {
		warmUpErrs = append(warmUpErrs, err)
	}
	if len(warmUpErrs) > 0 {
		return &WarmUpError{Errs: warmUpErrs}
	}
	return nil
}

func (c *Cache) BaseDir() string {
	return c.baseDir
}

func (c *Cache) SetPin(key Key, bytes []byte) (string, func(), error) {
	var evicted []Key
	var err error
	c.mu.Lock()
	if elem, exists := c.items[key]; exists {
		err = c.update(elem, key, bytes, key.Path, &evicted)
	} else {
		err = c.add(key.Path, key, bytes, &evicted)
	}
	if err != nil {
		c.mu.Unlock()
		c.notifyEvicted(evicted)
		return "", nil, err
	}

	e, ok := c.items[key].Value.(*entry)
	if !ok {
		c.mu.Unlock()
		c.notifyEvicted(evicted)
		return "", nil, fmt.Errorf("failed to cast cache entry")
	}
	e.refCount++
	path := filepath.Join(c.baseDir, e.key.Path)
	c.mu.Unlock()
	c.notifyEvicted(evicted)
	var released atomic.Bool
	release := func() {
		var releaseEvicted []Key
		if !released.CompareAndSwap(false, true) {
			return
		}
		c.mu.Lock()
		e.refCount--
		if e.pendingEvict && e.refCount == 0 {
			c.removeEntry(e, &releaseEvicted)
		}
		c.mu.Unlock()
		c.notifyEvicted(releaseEvicted)
	}
	return path, release, nil
}
