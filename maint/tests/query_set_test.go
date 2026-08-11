// SPDX-License-Identifier: GPL-3.0-only

package query_set

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IrineSistiana/mosdns/v5/pkg/concurrent_map"
	"github.com/IrineSistiana/mosdns/v5/pkg/query_context"
	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestSet(t *testing.T) (*QuerySet, string) {
	t.Helper()
	// The dir does not exist. NewQuerySet should create it.
	p := filepath.Join(t.TempDir(), "rule", "query_set.txt")
	s, err := NewQuerySet(zap.NewNop(), &Args{File: p})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s, p
}

// domains returns the domains of the file, in file order, without the
// header and the empty lines.
func domains(t *testing.T, p string) []string {
	t.Helper()
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if len(l) == 0 || l[0] == '#' {
			continue
		}
		out = append(out, l)
	}
	return out
}

func sortedDomains(t *testing.T, p string) []string {
	t.Helper()
	d := domains(t, p)
	sort.Strings(d)
	return d
}

func newQCtx(qName string) *query_context.Context {
	q := new(dns.Msg)
	q.SetQuestion(qName, dns.TypeA)
	return query_context.NewContext(q)
}

func (s *QuerySet) matched(d string) bool {
	_, ok := s.Match(d)
	return ok
}

func TestNewQuerySet_createsFile(t *testing.T) {
	_, p := newTestSet(t)
	assert.Equal(t, fileHeader+"\n", string(mustRead(t, p)))
}

func TestNewQuerySet_err(t *testing.T) {
	_, err := NewQuerySet(zap.NewNop(), &Args{File: "  "})
	assert.Error(t, err)

	// The path is a dir.
	_, err = NewQuerySet(zap.NewNop(), &Args{File: t.TempDir()})
	assert.Error(t, err)

	// Two sets can not share one file.
	p := filepath.Join(t.TempDir(), "query_set.txt")
	s, err := NewQuerySet(zap.NewNop(), &Args{File: p})
	require.NoError(t, err)
	_, err = NewQuerySet(zap.NewNop(), &Args{File: p})
	assert.Error(t, err)
	// After it is closed, the file is free again.
	require.NoError(t, s.Close())
	s2, err := NewQuerySet(zap.NewNop(), &Args{File: p})
	require.NoError(t, err)
	require.NoError(t, s2.Close())
}

func TestNewQuerySet_RefusesUnownedFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "query_set.txt")
	want := []byte("example.com\n")
	require.NoError(t, os.WriteFile(p, want, 0644))

	_, err := NewQuerySet(zap.NewNop(), &Args{File: p})
	assert.Error(t, err)
	assert.Equal(t, want, mustRead(t, p))
}

func TestQuerySet_CloseDoesNotReleaseAnotherInstance(t *testing.T) {
	p := filepath.Join(t.TempDir(), "query_set.txt")
	s1, err := NewQuerySet(zap.NewNop(), &Args{File: p})
	require.NoError(t, err)
	require.NoError(t, s1.Close())

	s2, err := NewQuerySet(zap.NewNop(), &Args{File: p})
	require.NoError(t, err)
	defer s2.Close()

	require.NoError(t, s1.Close())
	_, err = NewQuerySet(zap.NewNop(), &Args{File: p})
	assert.Error(t, err)
}

func TestQuerySet_ExecMatchAndFlush(t *testing.T) {
	s, p := newTestSet(t)

	assert.False(t, s.matched("a.com"))
	for _, n := range []string{"a.com.", "B.com.", "a.com.", "b.com."} {
		require.NoError(t, s.Exec(context.Background(), newQCtx(n)))
	}

	// A recorded domain matches at once, no need to wait for a flush.
	assert.True(t, s.matched("a.com"))
	assert.True(t, s.matched("A.com."))
	assert.True(t, s.matched("b.com"))
	assert.False(t, s.matched("c.com"))

	s.flush()
	assert.Equal(t, []string{"a.com", "b.com"}, domains(t, p))
	assert.True(t, strings.HasPrefix(string(mustRead(t, p)), fileHeader))
}

func TestQuerySet_RecordsExactDomains(t *testing.T) {
	s, p := newTestSet(t)

	require.NoError(t, s.Exec(context.Background(), newQCtx("example.com.")))
	// A sub domain is a new entry, it is recorded on its own.
	require.NoError(t, s.Exec(context.Background(), newQCtx("www.example.com.")))
	require.NoError(t, s.Exec(context.Background(), newQCtx("www.example.com.")))
	s.flush()

	assert.Equal(t, []string{"example.com", "www.example.com"}, domains(t, p))
	assert.True(t, s.matched("example.com"))
	assert.True(t, s.matched("www.example.com"))
	assert.False(t, s.matched("other.example.com"))
}

// The file that one run writes is loaded by the next run as it is.
func TestQuerySet_RoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "query_set.txt")
	s, err := NewQuerySet(zap.NewNop(), &Args{File: p})
	require.NoError(t, err)
	for _, n := range []string{"a.com.", "b.com.", "c.com."} {
		require.NoError(t, s.Exec(context.Background(), newQCtx(n)))
	}
	require.NoError(t, s.Close())
	before := mustRead(t, p)

	s2, err := NewQuerySet(zap.NewNop(), &Args{File: p})
	require.NoError(t, err)
	defer s2.Close()

	assert.True(t, s2.matched("a.com"))
	assert.True(t, s2.matched("c.com"))
	// Already known domains are not written again.
	require.NoError(t, s2.Exec(context.Background(), newQCtx("a.com.")))
	s2.flush()
	assert.Equal(t, string(before), string(mustRead(t, p)), "the file must not be rewritten")
}

// Malformed lines in an owned file are skipped and the file is repaired.
func TestQuerySet_BrokenFileIsRepaired(t *testing.T) {
	p := filepath.Join(t.TempDir(), "query_set.txt")
	content := []byte(fileHeader + "\n" +
		"good.com\n" +
		"has space.com\n" + // not a domain
		"\x00\x01\x02broken\n" + // torn by a power loss
		"UPPER.com\n" + // not normalized
		"good.com\n" + // duplicate
		"tail.com") // no line break at the end
	require.NoError(t, os.WriteFile(p, content, 0644))

	s, err := NewQuerySet(zap.NewNop(), &Args{File: p})
	require.NoError(t, err)
	defer s.Close()

	assert.True(t, s.matched("good.com"))
	assert.True(t, s.matched("upper.com"))
	assert.True(t, s.matched("tail.com"))

	// The file is repaired at load: no broken line, no duplicate, and
	// it ends with a line break.
	assert.Equal(t, []string{"good.com", "tail.com", "upper.com"}, sortedDomains(t, p))
	assert.True(t, strings.HasSuffix(string(mustRead(t, p)), "\n"))

	// Recording still works, and appends to the repaired file.
	require.NoError(t, s.Exec(context.Background(), newQCtx("new.org.")))
	s.flush()
	assert.Contains(t, domains(t, p), "new.org")
}

// The set in memory is the source of truth. A missing file or a size
// change is repaired from memory.
func TestQuerySet_RebuiltOnExternalChange(t *testing.T) {
	s, p := newTestSet(t)

	require.NoError(t, s.Exec(context.Background(), newQCtx("a.com.")))
	require.NoError(t, s.Exec(context.Background(), newQCtx("b.com.")))
	s.flush()
	require.Equal(t, []string{"a.com", "b.com"}, domains(t, p))

	// Emptied.
	require.NoError(t, os.WriteFile(p, nil, 0644))
	s.flush()
	assert.Equal(t, []string{"a.com", "b.com"}, sortedDomains(t, p))

	// Removed.
	require.NoError(t, os.Remove(p))
	s.flush()
	require.FileExists(t, p)
	assert.Equal(t, []string{"a.com", "b.com"}, sortedDomains(t, p))

	// Non-empty size drift.
	require.NoError(t, os.Truncate(p, 1))
	s.flush()
	assert.Equal(t, []string{"a.com", "b.com"}, sortedDomains(t, p))

	// Recording keeps working after a rebuild.
	require.NoError(t, s.Exec(context.Background(), newQCtx("c.com.")))
	s.flush()
	assert.Equal(t, []string{"a.com", "b.com", "c.com"}, sortedDomains(t, p))
}

func TestQuerySet_AppendDoesNotCreateMissingFile(t *testing.T) {
	s, p := newTestSet(t)
	require.NoError(t, os.Remove(p))

	err := s.appendFile([]byte("x.com\n"))
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.NoFileExists(t, p)

	s.flush()
	assert.FileExists(t, p)
}

// A rebuild covers the domains that are not written yet.
func TestQuerySet_PendingKeptOnRebuild(t *testing.T) {
	s, p := newTestSet(t)

	require.NoError(t, s.Exec(context.Background(), newQCtx("a.com.")))
	require.NoError(t, os.WriteFile(p, []byte("x.com\n"), 0644))
	s.flush()
	assert.Equal(t, []string{"a.com"}, domains(t, p))

	// a.com is in the set, it must not be written twice.
	require.NoError(t, s.Exec(context.Background(), newQCtx("a.com.")))
	s.flush()
	assert.Equal(t, []string{"a.com"}, domains(t, p))
}

func TestQuerySet_Concurrent(t *testing.T) {
	s, p := newTestSet(t)

	const goroutines = 16
	const count = 500
	wg := new(sync.WaitGroup)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < count; j++ {
				_ = s.Exec(context.Background(), newQCtx(fmt.Sprintf("d%d.com.", j)))
				s.matched(fmt.Sprintf("d%d.com", j))
			}
		}()
	}
	wg.Wait()
	s.flush()

	lines := domains(t, p)
	assert.Len(t, lines, count)
	set := make(map[string]struct{}, len(lines))
	for _, l := range lines {
		set[l] = struct{}{}
	}
	assert.Len(t, set, count)
}

func TestQuerySet_ConcurrentLimit(t *testing.T) {
	for attempt := 0; attempt < 20; attempt++ {
		s := &QuerySet{
			maxEntries: 1,
			logger:     zap.NewNop(),
			set:        newDomainSet(),
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < 256; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				s.add(fmt.Sprintf("d-%d-%d.example", attempt, i))
			}(i)
		}
		close(start)
		wg.Wait()
		assert.EqualValues(t, 1, s.set.len())
	}
}

func domainInShard(shard uint64) string {
	for i := 0; ; i++ {
		d := fmt.Sprintf("shard-%d.example", i)
		if domainKey(d).Sum()%uint64(concurrent_map.MapShardSize) == shard {
			return d
		}
	}
}

func TestQuerySet_RebuildDoesNotDuplicateConcurrentAdd(t *testing.T) {
	s, p := newTestSet(t)
	blocker := domainKey(domainInShard(0))
	late := domainInShard(concurrent_map.MapShardSize - 1)
	blocked := make(chan struct{})
	release := make(chan struct{})
	go s.set.m.TestAndSet(blocker, func(_ struct{}, _ bool) (struct{}, bool, bool) {
		close(blocked)
		<-release
		return struct{}{}, false, false
	})
	<-blocked

	writeDone := make(chan error, 1)
	go func() { writeDone <- s.write() }()
	require.Eventually(t, func() bool {
		_, err := os.Stat(p + ".tmp")
		return err == nil
	}, time.Second, time.Millisecond)

	addDone := make(chan struct{})
	go func() {
		s.add(late)
		close(addDone)
	}()
	select {
	case <-addDone:
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-writeDone)
	<-addDone

	s.flush()
	count := 0
	for _, d := range domains(t, p) {
		if d == late {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestQuerySet_MaxEntriesArg(t *testing.T) {
	dir := t.TempDir()
	for i, tt := range []struct {
		max int
		ok  bool
	}{
		{0, true},
		{1, true},
		{hardMaxEntries, true},
		{hardMaxEntries + 1, false},
		{-1, false},
	} {
		p := filepath.Join(dir, fmt.Sprintf("g%d.txt", i))
		s, err := NewQuerySet(zap.NewNop(), &Args{File: p, MaxEntries: tt.max})
		if !tt.ok {
			assert.Error(t, err, "max_entries %d", tt.max)
			continue
		}
		require.NoError(t, err, "max_entries %d", tt.max)
		want := tt.max
		if want == 0 {
			want = defaultMaxEntries
		}
		assert.Equal(t, want, s.maxEntries)
		require.NoError(t, s.Close())
	}
}

// Hitting the limit stops recording, it does not break matching.
func TestQuerySet_LimitReached(t *testing.T) {
	p := filepath.Join(t.TempDir(), "query_set.txt")
	s, err := NewQuerySet(zap.NewNop(), &Args{File: p, MaxEntries: 2})
	require.NoError(t, err)
	defer s.Close()

	for _, n := range []string{"a.com.", "b.com.", "c.com.", "d.com."} {
		require.NoError(t, s.Exec(context.Background(), newQCtx(n)))
	}
	s.flush()

	assert.Equal(t, []string{"a.com", "b.com"}, domains(t, p))
	assert.True(t, s.matched("a.com"))
	assert.True(t, s.matched("b.com"))
	assert.False(t, s.matched("c.com"))
}

// A file that is over the limit is cut at load, and the file is put
// back in sync with what is kept.
func TestQuerySet_LimitOnLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "query_set.txt")
	require.NoError(t, os.WriteFile(p, []byte(fileHeader+"\na.com\nb.com\nc.com\n"), 0644))

	s, err := NewQuerySet(zap.NewNop(), &Args{File: p, MaxEntries: 2})
	require.NoError(t, err)
	defer s.Close()

	assert.True(t, s.matched("a.com"))
	assert.True(t, s.matched("b.com"))
	assert.False(t, s.matched("c.com"))
	assert.Equal(t, []string{"a.com", "b.com"}, sortedDomains(t, p))
}

func TestNormalizeDomain(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"example.com.", "example.com", true},
		{"WWW.Example.COM.", "www.example.com", true},
		{"example.com", "example.com", true},
		{"_dns.example.com.", "_dns.example.com", true},
		{"xn--fiqs8s.xn--fiqs8s.", "xn--fiqs8s.xn--fiqs8s", true},
		{"1.0.0.127.in-addr.arpa.", "1.0.0.127.in-addr.arpa", true},
		{".", "", false},
		{"", "", false},
		{"..", "", false},
		{"a..com.", "", false},
		{".com.", "", false},
		{"a b.com.", "", false},
		{"a\tb.com.", "", false},
		{"full:a.com.", "", false},
		{"a#b.com.", "", false},
		{"a\\000b.com.", "", false},
		{"测试.com.", "", false},
		{strings.Repeat("a", 254) + ".", "", false},
	}
	for _, tt := range tests {
		got, ok := normalizeDomain(tt.in)
		assert.Equal(t, tt.ok, ok, "normalizeDomain(%q)", tt.in)
		assert.Equal(t, tt.want, got, "normalizeDomain(%q)", tt.in)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	return b
}

const benchPreload = 100000

func benchNames(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("d%d.example.com.", i)
	}
	return names
}

func newBenchSet(b *testing.B, preload int) *QuerySet {
	b.Helper()
	p := filepath.Join(b.TempDir(), "query_set.txt")
	s, err := NewQuerySet(zap.NewNop(), &Args{File: p})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = s.Close() })
	for _, n := range benchNames(preload) {
		s.add(n)
	}
	s.flush()
	return s
}

// BenchmarkQuerySet_Match is the matcher path, used by every query that
// matches on this set.
func BenchmarkQuerySet_Match(b *testing.B) {
	s := newBenchSet(b, benchPreload)
	names := benchNames(benchPreload)
	for i := range names {
		names[i] = strings.TrimSuffix(names[i], ".")
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			s.Match(names[i])
			if i++; i == len(names) {
				i = 0
			}
		}
	})
}

// BenchmarkQuerySet_MatchMiss is the same path when the domain has not
// been recorded, which is the common case.
func BenchmarkQuerySet_MatchMiss(b *testing.B) {
	s := newBenchSet(b, benchPreload)
	names := benchNames(benchPreload)
	for i := range names {
		names[i] = "x" + strings.TrimSuffix(names[i], ".")
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			s.Match(names[i])
			if i++; i == len(names) {
				i = 0
			}
		}
	})
}

// BenchmarkQuerySet_New is the cold path: every domain is new.
func BenchmarkQuerySet_New(b *testing.B) {
	s := newBenchSet(b, 0)
	names := benchNames(b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.add(names[i])
	}
}
