package memory

import (
	"context"
	"fmt"

	chromem "github.com/philippgille/chromem-go"
)

// VectorStore wraps a chromem-go collection for vector similarity search.
type VectorStore struct {
	Collection *chromem.Collection
}

// NewVectorStore creates a vector store backed by chromem-go.
func NewVectorStore(db *chromem.DB, name string, embed chromem.EmbeddingFunc) (*VectorStore, error) {
	col, err := db.GetOrCreateCollection(name, nil, embed)
	if err != nil {
		return nil, fmt.Errorf("create vector collection: %w", err)
	}

	return &VectorStore{Collection: col}, nil
}

// Upsert adds or updates a vector with metadata.
// chromem-go overwrites on Add with the same ID.
func (v *VectorStore) Upsert(
	ctx context.Context,
	id string,
	embedding []float32,
	metadata map[string]string,
) error {
	return v.Collection.Add(ctx,
		[]string{id},
		[][]float32{embedding},
		[]map[string]string{metadata},
		nil,
	)
}

// Search returns IDs ordered by cosine similarity.
func (v *VectorStore) Search(
	ctx context.Context,
	embedding []float32,
	limit int,
	where map[string]string,
) ([]string, error) {
	count := v.Collection.Count()
	if count == 0 {
		return nil, nil
	}
	if limit > count {
		limit = count
	}

	results, err := v.Collection.QueryEmbedding(
		ctx, embedding, limit, where, nil,
	)
	if err != nil {
		return nil, err
	}

	ids := make([]string, len(results))
	for i, r := range results {
		ids[i] = r.ID
	}

	return ids, nil
}

// Delete removes vectors by ID.
func (v *VectorStore) Delete(ctx context.Context, ids ...string) error {
	return v.Collection.Delete(ctx, nil, nil, ids...)
}

// Count returns the number of vectors in the collection.
func (v *VectorStore) Count() int {
	return v.Collection.Count()
}

// entryText builds the text to embed from a memory entry.
func entryText(e MemoryEntry) string {
	text := e.Tool + ": " + e.Command
	if e.Summary != "" {
		text += " " + e.Summary
	}

	return text
}

// entryMetadata builds the metadata map for a memory entry.
func entryMetadata(e MemoryEntry) map[string]string {
	m := map[string]string{
		"session_id": e.SessionID,
	}

	if e.Tool != "" {
		m["tool"] = e.Tool
	}

	if len(e.Tags) > 0 {
		tagStr := ""
		for i, t := range e.Tags {
			if i > 0 {
				tagStr += ","
			}
			tagStr += t
		}
		m["tags"] = tagStr
	}

	return m
}
