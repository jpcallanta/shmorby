package memory

import (
	"context"
	"strings"
)

// Retriever performs vector similarity search on the memory store.
type Retriever struct {
	store    Store
	topK     int
	vector   *VectorStore
	embedder Embedder

	// Stats tracks retrieval statistics.
	hits   int
	misses int

	// lastResult stores the most recent retrieval result.
	lastResult *RetrievalResult
}

// RetrievalResult holds the result of a memory retrieval operation.
type RetrievalResult struct {
	Entries []MemoryEntry
	Query   string
	Method  string
	Hit     bool
}

// RetrievalStats holds retrieval statistics for display.
type RetrievalStats struct {
	Hits      int
	Misses    int
	LastHit   bool
	LastCount int
	LastQuery string
}

// NewRetriever creates a Retriever with the given store and top-K limit.
func NewRetriever(store Store, topK int) *Retriever {
	if topK <= 0 {
		topK = 5
	}

	return &Retriever{store: store, topK: topK}
}

// SetVectorSearch enables vector similarity search.
func (r *Retriever) SetVectorSearch(
	vs *VectorStore, emb Embedder,
) {
	r.vector = vs
	r.embedder = emb
}

// Retrieve runs a similarity search and returns ranked results.
// Falls back to LIKE keyword search when no embedder is configured.
func (r *Retriever) Retrieve(
	ctx context.Context, query string,
) (*RetrievalResult, error) {
	// Try vector search first.
	if r.vector != nil && r.embedder != nil {
		embeddings, err := r.embedder.Embed(ctx, []string{query})
		if err == nil && len(embeddings) > 0 {
			ids, sErr := r.vector.Search(
				ctx, embeddings[0], r.topK, nil,
			)
			if sErr == nil && len(ids) > 0 {
				entries := r.fetchByID(ids)

				hit := len(entries) > 0
				if hit {
					r.hits++
				} else {
					r.misses++
				}

				result := &RetrievalResult{
					Entries: entries,
					Query:   query,
					Method:  "vector",
					Hit:     hit,
				}
				r.lastResult = result

				return result, nil
			}
		}
	}

	// Fallback: LIKE keyword search (FTS5 removed).
	return r.keywordFallback(ctx, query)
}

// keywordFallback searches using substring match for entries containing
// the query text. Used when no embedder is configured.
func (r *Retriever) keywordFallback(
	ctx context.Context, query string,
) (*RetrievalResult, error) {
	if query == "" {
		r.misses++

		result := &RetrievalResult{
			Entries: nil,
			Query:   query,
			Method:  "keyword",
			Hit:     false,
		}
		r.lastResult = result

		return result, nil
	}

	entries, err := r.store.List(1000, 0)
	if err != nil {
		return nil, err
	}

	var matches []MemoryEntry
	for _, e := range entries {
		if queryMatch(e, query) {
			matches = append(matches, e)
			if len(matches) >= r.topK {
				break
			}
		}
	}

	hit := len(matches) > 0
	if hit {
		r.hits++
	} else {
		r.misses++
	}

	result := &RetrievalResult{
		Entries: matches,
		Query:   query,
		Method:  "keyword",
		Hit:     hit,
	}
	r.lastResult = result

	return result, nil
}

// queryMatch checks if an entry matches the query. Splits the query
// on whitespace, strips punctuation, and returns true if any word
// is a case-insensitive substring of an entry field.
func queryMatch(e MemoryEntry, query string) bool {
	for _, word := range strings.Fields(query) {
		word = strings.Trim(word, ".,;:!?\"'()[]{}")
		if len(word) < 2 {
			continue
		}
		lw := strings.ToLower(word)
		if containsLower(e.Command, lw) ||
			containsLower(e.Summary, lw) ||
			containsLower(e.Result, lw) {
			return true
		}
	}

	return false
}

func containsLower(s, substr string) bool {
	return len(s) >= len(substr) &&
		searchSubstring(strings.ToLower(s), substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}

// fetchByID retrieves entries from SQLite by their vector IDs.
func (r *Retriever) fetchByID(ids []string) []MemoryEntry {
	var entries []MemoryEntry

	for _, id := range ids {
		e, err := r.store.Get(id)
		if err != nil {
			continue
		}
		entries = append(entries, e)
	}

	return entries
}

// Stats returns the current retrieval statistics.
func (r *Retriever) Stats() RetrievalStats {
	s := RetrievalStats{
		Hits:   r.hits,
		Misses: r.misses,
	}
	if r.lastResult != nil {
		s.LastHit = r.lastResult.Hit
		s.LastCount = len(r.lastResult.Entries)
		s.LastQuery = r.lastResult.Query
	}

	return s
}

// Total returns the total number of retrieval operations.
func (r *Retriever) Total() int {
	return r.hits + r.misses
}
