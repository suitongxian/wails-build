package similarity

import (
	"context"
	"testing"
)

// BenchmarkBuildFamilies_CacheMiss runs BuildFamilies with all features cleared
// each iteration (worst case: every file needs live extraction).
func BenchmarkBuildFamilies_CacheMiss(b *testing.B) {
	fixture := setupDeterminismFixture(&testing.T{})
	defer fixture.cleanup()
	SetDB(fixture.db)
	defer SetDB(nil)

	cfg := defaultConfig()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		clearAllFeatures(&testing.T{}, fixture.db)
		b.StartTimer()
		_, _ = BuildFamilies(context.Background(), fixture.inputs, cfg)
	}
}

// BenchmarkBuildFamilies_CacheHit runs BuildFamilies repeatedly with cache
// already warm (best case: zero live extraction).
func BenchmarkBuildFamilies_CacheHit(b *testing.B) {
	fixture := setupDeterminismFixture(&testing.T{})
	defer fixture.cleanup()
	SetDB(fixture.db)
	defer SetDB(nil)

	cfg := defaultConfig()
	// Prime the cache: run once with miss → features written back to DB
	clearAllFeatures(&testing.T{}, fixture.db)
	_, _ = BuildFamilies(context.Background(), fixture.inputs, cfg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = BuildFamilies(context.Background(), fixture.inputs, cfg)
	}
}
