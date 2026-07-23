package mihomo

import (
	"context"
	"errors"
	"testing"
)

func TestCompileCacheHitsAndContentInvalidation(t *testing.T) {
	cache := NewCompileCache(2)
	compiler := NewCompiler(DefaultLimits())
	base := []byte("proxies: []\n")
	rewrites := []Rewrite{{Name: "mode", Kind: RewriteYAML, Content: "mode: rule\n"}}

	first, err := cache.Compile(context.Background(), compiler, base, rewrites)
	if err != nil {
		t.Fatal(err)
	}
	first.YAML[0] = 'X' // callers cannot mutate the cached copy
	second, err := cache.Compile(context.Background(), compiler, base, rewrites)
	if err != nil {
		t.Fatal(err)
	}
	if second.YAML[0] == 'X' {
		t.Fatal("cache returned aliased YAML")
	}
	hits, misses := cache.Stats()
	if hits != 1 || misses != 1 {
		t.Fatalf("stats = (%d, %d), want (1, 1)", hits, misses)
	}

	rewrites[0].Content = "mode: global\n"
	if _, err := cache.Compile(context.Background(), compiler, base, rewrites); err != nil {
		t.Fatal(err)
	}
	hits, misses = cache.Stats()
	if hits != 1 || misses != 2 {
		t.Fatalf("changed rewrite stats = (%d, %d), want (1, 2)", hits, misses)
	}
}

func TestCompileCacheHonorsCanceledContextBeforeHit(t *testing.T) {
	cache := NewCompileCache(1)
	compiler := NewCompiler(DefaultLimits())
	base := []byte("proxies: []\n")
	if _, err := cache.Compile(context.Background(), compiler, base, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := cache.Compile(ctx, compiler, base, nil)
	var compileErr *CompileError
	if !errors.As(err, &compileErr) || compileErr.Kind != ErrorCanceled {
		t.Fatalf("error = %#v, want canceled", err)
	}
	hits, misses := cache.Stats()
	if hits != 0 || misses != 1 {
		t.Fatalf("stats = (%d, %d), want (0, 1)", hits, misses)
	}
}

func TestCompileCacheEvictsLeastRecentlyUsedAndDoesNotCacheErrors(t *testing.T) {
	cache := NewCompileCache(1)
	compiler := NewCompiler(DefaultLimits())
	base := []byte("proxies: []\n")
	first := []Rewrite{{Name: "one", Kind: RewriteYAML, Content: "mode: rule\n"}}
	second := []Rewrite{{Name: "two", Kind: RewriteYAML, Content: "mode: global\n"}}
	if _, err := cache.Compile(context.Background(), compiler, base, first); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Compile(context.Background(), compiler, base, second); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Compile(context.Background(), compiler, base, first); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Compile(context.Background(), compiler, base, []Rewrite{{Name: "bad", Kind: RewriteYAML, Content: ":"}}); err == nil {
		t.Fatal("expected invalid rewrite error")
	}
	if _, err := cache.Compile(context.Background(), compiler, base, []Rewrite{{Name: "bad", Kind: RewriteYAML, Content: ":"}}); err == nil {
		t.Fatal("expected repeated invalid rewrite error")
	}
	hits, misses := cache.Stats()
	if hits != 0 || misses != 5 {
		t.Fatalf("stats = (%d, %d), want (0, 5)", hits, misses)
	}
}
