package mihomo

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
)

// CompilerVersion is part of every cache key. Increment it whenever compiler
// semantics change without changing the input document shape.
const CompilerVersion = "boxfleet-mihomo-v1"

// CompileCache is a bounded, concurrency-safe LRU for successful compilation
// results. Errors are never cached, so a timeout or cancellation cannot poison
// later subscription requests.
type CompileCache struct {
	mu     sync.Mutex
	items  *lru.Cache[[sha256.Size]byte, CompileResult]
	hits   uint64
	misses uint64
}

func NewCompileCache(capacity int) *CompileCache {
	if capacity < 1 {
		capacity = 1
	}
	items, _ := lru.New[[sha256.Size]byte, CompileResult](capacity)
	return &CompileCache{items: items}
}

func (c *CompileCache) Compile(
	ctx context.Context,
	compiler *Compiler,
	baseYAML []byte,
	rewrites []Rewrite,
) (CompileResult, error) {
	if err := ctx.Err(); err != nil {
		return CompileResult{}, &CompileError{Kind: ErrorCanceled, Err: err}
	}
	key := compileCacheKey(baseYAML, rewrites)
	if result, ok := c.items.Get(key); ok {
		c.mu.Lock()
		c.hits++
		c.mu.Unlock()
		return cloneCompileResult(result), nil
	}
	c.mu.Lock()
	c.misses++
	c.mu.Unlock()

	result, err := compiler.Compile(ctx, baseYAML, rewrites)
	if err != nil {
		return CompileResult{}, err
	}

	c.items.Add(key, cloneCompileResult(result))
	return result, nil
}

func (c *CompileCache) Stats() (hits, misses uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.misses
}

func compileCacheKey(baseYAML []byte, rewrites []Rewrite) [sha256.Size]byte {
	hash := sha256.New()
	hash.Write([]byte(CompilerVersion))
	hash.Write([]byte{0})
	hash.Write(baseYAML)
	hash.Write([]byte{0})
	encoded, _ := json.Marshal(rewrites)
	hash.Write(encoded)
	var key [sha256.Size]byte
	copy(key[:], hash.Sum(nil))
	return key
}

func cloneCompileResult(result CompileResult) CompileResult {
	return CompileResult{
		YAML:        append([]byte(nil), result.YAML...),
		Logs:        append([]LogEntry(nil), result.Logs...),
		Diagnostics: append([]Diagnostic(nil), result.Diagnostics...),
	}
}
