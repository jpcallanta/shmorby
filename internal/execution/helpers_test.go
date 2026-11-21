package execution

import (
	"errors"
	"sync"
)

// memRecorder is an in-memory Recorder for manager tests.
type memRecorder struct {
	mu      sync.Mutex
	records map[string]Record
	order   []string
	output  map[string][]string
}

// Creates an empty in-memory recorder.
func newMemRecorder() *memRecorder {
	return &memRecorder{
		records: make(map[string]Record),
		output:  make(map[string][]string),
	}
}

// Inserts a record.
func (r *memRecorder) CreateExecution(rec Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.records[rec.ID] = rec
	r.order = append(r.order, rec.ID)

	return nil
}

// Updates a record.
func (r *memRecorder) UpdateExecution(rec Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.records[rec.ID]; !ok {
		return errors.New("not found")
	}
	r.records[rec.ID] = rec

	return nil
}

// Returns the stored record by id.
func (r *memRecorder) GetExecution(id string) (Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rec, ok := r.records[id]
	if !ok {
		return Record{}, ErrNotFound
	}

	return rec, nil
}

// Replaces the output chunks for id.
func (r *memRecorder) AppendExecutionOutput(
	id string, chunks []string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.records[id]; !ok {
		return ErrNotFound
	}
	r.output[id] = append([]string(nil), chunks...)

	return nil
}

// Returns one output chunk and whether it is the last.
func (r *memRecorder) ReadExecutionOutput(
	id string, cursor int,
) (string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.records[id]; !ok {
		return "", false, ErrNotFound
	}
	chunks := r.output[id]
	if cursor >= len(chunks) {
		return "", true, nil
	}

	return chunks[cursor], cursor == len(chunks)-1, nil
}

// Returns records matching f in reverse insertion order.
func (r *memRecorder) ListExecutions(
	f Filter,
) ([]Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	statuses := make(map[Status]bool, len(f.Statuses))
	for _, s := range f.Statuses {
		statuses[s] = true
	}

	out := make([]Record, 0, len(r.order))
	for i := len(r.order) - 1; i >= 0; i-- {
		rec := r.records[r.order[i]]
		if f.SessionID != "" && rec.SessionID != f.SessionID {
			continue
		}
		if len(statuses) > 0 && !statuses[rec.Status] {
			continue
		}
		out = append(out, rec)
		if f.Limit > 0 && len(out) >= f.Limit {
			break
		}
	}

	return out, nil
}

// Returns the stored record by id.
func (r *memRecorder) get(id string) Record {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.records[id]
}
