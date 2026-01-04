package main

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
)

// CachedFile stores parsed entries with mod time for incremental parsing
type CachedFile struct {
	ModTime int64
	Entries []WordIndentEntry
}

// FileCache stores all cached file data
type FileCache struct {
	Version int // cache format version for invalidation
	Files   map[string]CachedFile
}

const cacheVersion = 1

func loadCache(dir string, strategyName string) *FileCache {
	// Cache only works with word-indent strategy (uses WordIndentEntry)
	if strategyName != "word-indent" {
		return nil
	}

	cachePath := filepath.Join(dir, ".quickdup", strategyName+"-cache.gob")
	file, err := os.Open(cachePath)
	if err != nil {
		return nil
	}
	defer file.Close()

	var cache FileCache
	decoder := gob.NewDecoder(file)
	if err := decoder.Decode(&cache); err != nil {
		return nil
	}

	// Check version
	if cache.Version != cacheVersion {
		return nil
	}

	return &cache
}

// saveCache saves the file cache to disk
func saveCache(dir string, strategyName string, files []string, fileData map[string][]Entry) {
	// Cache only works with word-indent strategy (uses WordIndentEntry)
	if strategyName != "word-indent" {
		return
	}

	// Build cache from current file data
	cache := FileCache{
		Version: cacheVersion,
		Files:   make(map[string]CachedFile),
	}

	for _, path := range files {
		entries, ok := fileData[path]
		if !ok {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		// Convert []Entry to []WordIndentEntry for serialization
		concrete := make([]WordIndentEntry, len(entries))
		for i, e := range entries {
			concrete[i] = *e.(*WordIndentEntry)
		}
		cache.Files[path] = CachedFile{
			ModTime: info.ModTime().UnixNano(),
			Entries: concrete,
		}
	}

	// Ensure directory exists
	cacheDir := filepath.Join(dir, ".quickdup")
	os.MkdirAll(cacheDir, 0755)

	cachePath := filepath.Join(cacheDir, strategyName+"-cache.gob")
	file, err := os.Create(cachePath)
	if err != nil {
		return // silently fail
	}
	defer file.Close()

	encoder := gob.NewEncoder(file)
	encoder.Encode(cache)
}

// parseFilesWithCache parses files using cache when possible
func parseFilesWithCache(files []string, cache *FileCache) (map[string][]Entry, int, int) {
	numWorkers := runtime.NumCPU()
	results := make(map[string][]Entry)
	var mu sync.Mutex
	var cacheHits atomic.Int64
	var cacheMisses atomic.Int64

	// Create work channel
	work := make(chan string, len(files))
	for _, f := range files {
		work <- f
	}
	close(work)

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range work {
				var entries []Entry
				var fromCache bool

				// Check cache
				if cache != nil {
					if cached, ok := cache.Files[path]; ok {
						info, err := os.Stat(path)
						if err == nil && info.ModTime().UnixNano() == cached.ModTime {
							// Convert []WordIndentEntry to []Entry
							entries = make([]Entry, len(cached.Entries))
							for i := range cached.Entries {
								entries[i] = &cached.Entries[i]
							}
							fromCache = true
						}
					}
				}

				// Parse if not cached
				if !fromCache {
					var err error
					entries, err = parseFile(path)
					if err != nil {
						continue // skip files that fail to parse
					}
					cacheMisses.Add(1)
				} else {
					cacheHits.Add(1)
				}

				mu.Lock()
				results[path] = entries
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return results, int(cacheHits.Load()), int(cacheMisses.Load())
}
