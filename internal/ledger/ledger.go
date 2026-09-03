// Package ledger implements an encrypted, cross-session environment
// ledger. Data is stored as a section-keyed JSON blob encrypted at
// rest with age (x25519). Concurrent-session safety is provided by
// an exclusive lock on a separate lock file (flock on Unix,
// LockFileEx on Windows).
package ledger

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"filippo.io/age"

	"shmorby/internal/xdg"
)

const (
	// dataFile is the encrypted ledger blob.
	dataFile = "ledger.json.age"

	// lockFile serialises read-modify-write across processes.
	lockFile = "ledger.lock"

	// keyFile holds the age x25519 identity (chmod 0600).
	keyFile = "ledger.key"

	// MaxSectionBytes is the maximum size of a single section's
	// JSON payload (64 KB). Oversized writes are rejected.
	MaxSectionBytes = 64 * 1024

	// MaxSections is the maximum number of sections allowed in
	// a single ledger. Oversized writes are rejected.
	MaxSections = 100

	// maxEncryptedBytes bounds the ciphertext ledger file size on
	// read. A legitimate file is at most MaxSections ×
	// MaxSectionBytes (~6.4 MB) plus JSON and age framing; 12 MiB
	// leaves generous headroom.
	maxEncryptedBytes int64 = 12 << 20

	// maxDecryptedBytes bounds the plaintext size after decryption,
	// just above the worst-case legitimate payload.
	maxDecryptedBytes int64 = 10 << 20
)

// Data is the on-disk schema (before encryption). Sections is a
// map of section name to raw JSON so the output stays jq-friendly
// and new sections need no schema change.
type Data struct {
	Sections map[string]json.RawMessage `json:"sections"`
}

// Ledger is an open, locked, decrypted environment ledger.
// Call Close when done to save (if dirty), unlock, and release.
type Ledger struct {
	dir      string
	data     *Data
	dirty    bool
	lockFd   *os.File
	identity *age.X25519Identity
}

// Open opens the ledger from the default data directory,
// generating the key file on first use.
func Open() (*Ledger, error) {

	return OpenAt(xdg.UserDataDir())
}

// OpenAt opens the ledger from dir, generating the key file on
// first use. The directory is created if missing.
func OpenAt(dir string) (*Ledger, error) {

	if err := os.MkdirAll(dir, 0o755); err != nil {

		return nil, fmt.Errorf("create ledger dir: %w", err)
	}

	// Acquire the lock BEFORE key load/create to prevent a
	// race where concurrent first-use processes generate
	// different keys (C1 fix).
	fd, err := acquireLock(dir)
	if err != nil {

		return nil, fmt.Errorf("acquire lock: %w", err)
	}

	id, err := loadOrCreateIdentity(dir)
	if err != nil {

		_ = releaseLock(fd)

		return nil, fmt.Errorf("load key: %w", err)
	}

	d, err := readData(dir, id)
	if err != nil {

		_ = releaseLock(fd)

		return nil, fmt.Errorf("read ledger: %w", err)
	}

	return &Ledger{
		dir:      dir,
		data:     d,
		lockFd:   fd,
		identity: id,
	}, nil
}

// Close saves the ledger if it was modified, releases the lock,
// and closes the lock file descriptor.
func (l *Ledger) Close() error {

	var errs []error

	if l.dirty {

		if err := writeData(l.dir, l.data, l.identity.Recipient()); err != nil {

			errs = append(errs, fmt.Errorf("save ledger: %w", err))
		}
	}

	if err := releaseLock(l.lockFd); err != nil {

		errs = append(errs, err)
	}

	if len(errs) > 0 {

		return errors.Join(errs...)
	}

	return nil
}

// Get returns the raw JSON for a section and whether it exists.
func (l *Ledger) Get(section string) (json.RawMessage, bool) {

	v, ok := l.data.Sections[section]

	return v, ok
}

// Set replaces a section's data. Marks the ledger dirty.
func (l *Ledger) Set(section string, data json.RawMessage) {

	if l.data.Sections == nil {

		l.data.Sections = make(map[string]json.RawMessage)
	}

	l.data.Sections[section] = data
	l.dirty = true
}

// Delete removes a section. No-op if absent. Marks dirty.
func (l *Ledger) Delete(section string) {

	if _, ok := l.data.Sections[section]; !ok {

		return
	}

	delete(l.data.Sections, section)
	l.dirty = true
}

// Sections returns sorted section names.
func (l *Ledger) Sections() []string {

	names := make([]string, 0, len(l.data.Sections))
	for k := range l.data.Sections {

		names = append(names, k)
	}

	sort.Strings(names)

	return names
}

// ── key management ─────────────────────────────────────────

// loadOrCreateIdentity loads the age identity from the key file
// or generates a new one on first use.
func loadOrCreateIdentity(dir string) (*age.X25519Identity, error) {

	path := filepath.Join(dir, keyFile)

	id, err := loadIdentity(path)
	if err != nil {

		if !errors.Is(err, os.ErrNotExist) {

			return nil, err
		}

		id, err = generateIdentity(path)
		if err != nil {

			return nil, err
		}
	}

	return id, nil
}

// loadIdentity reads an age x25519 identity from path.
func loadIdentity(path string) (*age.X25519Identity, error) {

	f, err := os.Open(path)
	if err != nil {

		return nil, fmt.Errorf("open key: %w", err)
	}
	defer f.Close()

	ids, err := age.ParseIdentities(f)
	if err != nil {

		return nil, fmt.Errorf("parse key: %w", err)
	}

	if len(ids) == 0 {

		return nil, fmt.Errorf("no identities in %s", path)
	}

	xid, ok := ids[0].(*age.X25519Identity)
	if !ok {

		return nil, fmt.Errorf("key is not x25519")
	}

	return xid, nil
}

// generateIdentity creates a new age x25519 keypair, writes the
// identity to path with chmod 0600, and returns it.
func generateIdentity(path string) (*age.X25519Identity, error) {

	id, err := age.GenerateX25519Identity()
	if err != nil {

		return nil, fmt.Errorf("generate key: %w", err)
	}

	// Write the identity string (includes comment header).
	content := fmt.Sprintf(
		"# created: %s\n# public key: %s\n%s\n",
		time.Now().UTC().Format(time.RFC3339),
		id.Recipient().String(),
		id.String(),
	)

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {

		return nil, fmt.Errorf("write key: %w", err)
	}

	return id, nil
}

// ── read / write ───────────────────────────────────────────

// readData decrypts and parses the ledger data. Returns an empty
// Data when the file does not yet exist. Both the encrypted file and
// the decrypted stream are size-bounded: writes cap content at
// MaxSections × MaxSectionBytes (~6.4 MB), so a tampered or
// corrupted file far beyond that is rejected instead of exhausting
// memory.
func readData(dir string, id *age.X25519Identity) (*Data, error) {

	path := filepath.Join(dir, dataFile)

	info, err := os.Stat(path)
	if err != nil {

		if errors.Is(err, os.ErrNotExist) {

			return &Data{Sections: make(map[string]json.RawMessage)}, nil
		}

		return nil, fmt.Errorf("stat data: %w", err)
	}
	if info.Size() > maxEncryptedBytes {

		return nil, fmt.Errorf(
			"ledger file exceeds %d bytes", maxEncryptedBytes,
		)
	}

	raw, err := os.ReadFile(path)
	if err != nil {

		return nil, fmt.Errorf("read data: %w", err)
	}

	r, err := age.Decrypt(bytes.NewReader(raw), id)
	if err != nil {

		return nil, fmt.Errorf("decrypt: %w", err)
	}

	plain, err := io.ReadAll(io.LimitReader(r, maxDecryptedBytes+1))
	if err != nil {

		return nil, fmt.Errorf("read decrypted: %w", err)
	}
	if int64(len(plain)) > maxDecryptedBytes {

		return nil, fmt.Errorf(
			"decrypted ledger exceeds %d bytes", maxDecryptedBytes,
		)
	}

	var d Data
	if err := json.Unmarshal(plain, &d); err != nil {

		return nil, fmt.Errorf("parse ledger JSON: %w", err)
	}

	if d.Sections == nil {

		d.Sections = make(map[string]json.RawMessage)
	}

	return &d, nil
}

// writeData encrypts and atomically writes the ledger data.
// Uses the same tmp+rename pattern as config/migrate.go.
func writeData(dir string, d *Data, recip *age.X25519Recipient) error {

	plain, err := json.MarshalIndent(d, "", "  ")
	if err != nil {

		return fmt.Errorf("marshal ledger: %w", err)
	}

	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recip)
	if err != nil {

		return fmt.Errorf("init encrypt: %w", err)
	}

	if _, err := w.Write(plain); err != nil {

		return fmt.Errorf("encrypt: %w", err)
	}

	if err := w.Close(); err != nil {

		return fmt.Errorf("finalize encrypt: %w", err)
	}

	dst := filepath.Join(dir, dataFile)
	tmp := dst + ".tmp"

	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {

		os.Remove(tmp)

		return fmt.Errorf("write temp ledger: %w", err)
	}

	if err := os.Rename(tmp, dst); err != nil {

		// Windows: dst may be held by AV/indexer with
		// sharing violation; try Remove then Rename once.
		if runtime.GOOS == "windows" {

			if remErr := os.Remove(dst); remErr == nil {

				err = os.Rename(tmp, dst)
			}
		}

		if err != nil {

			os.Remove(tmp)

			return fmt.Errorf("rename ledger: %w", err)
		}
	}

	return nil
}

// ── helpers ────────────────────────────────────────────────

// ValidateSection rejects section names that contain path
// separators or are empty.
func ValidateSection(name string) error {

	if name == "" {

		return fmt.Errorf("section name is empty")
	}

	if strings.ContainsAny(name, "/\\. ") {

		return fmt.Errorf(
			"section name %q contains invalid characters",
			name,
		)
	}

	return nil
}

// ValidateData checks that a section payload does not exceed the
// per-section size cap and that the total section count does not
// exceed MaxSections. existingSections is the current count of
// sections in the ledger (before the write); pass -1 to skip the
// section-count check (e.g. when replacing an existing section).
func ValidateData(data json.RawMessage, existingSections int) error {

	if len(data) > MaxSectionBytes {

		return fmt.Errorf(
			"section payload %d bytes exceeds max %d",
			len(data), MaxSectionBytes,
		)
	}

	if existingSections >= 0 &&
		existingSections >= MaxSections {

		return fmt.Errorf(
			"ledger has %d sections (max %d); delete a "+
				"section before adding a new one",
			existingSections, MaxSections,
		)
	}

	return nil
}

// FormatContext builds a compact "Known environment (ledger):"
// block from the ledger's sections. The output is capped at
// maxBytes (0 = unlimited). Returns empty string when the ledger
// is empty or unreadable.
func FormatContext(l *Ledger, maxBytes int) string {

	sections := l.Sections()
	if len(sections) == 0 {

		return ""
	}

	const (
		header = "Known environment (ledger):\n"
		marker = "  (truncated)\n"
	)

	// The budget covers the whole block, header and truncation
	// marker included, so the output never exceeds maxBytes.
	if maxBytes > 0 && len(header) > maxBytes {

		return ""
	}

	var b strings.Builder
	b.WriteString(header)

	for _, name := range sections {

		data, ok := l.Get(name)
		if !ok {

			continue
		}

		// Compact JSON for context injection. Decode with
		// json.Number so integers keep full precision (values
		// > 2^53 are not rounded through float64).
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		var compact interface{}
		if err := dec.Decode(&compact); err != nil {

			continue
		}

		line, err := json.Marshal(compact)
		if err != nil {

			continue
		}

		entry := fmt.Sprintf("- %s: %s\n", name, string(line))

		// Check budget before writing. The truncation marker is
		// counted toward the budget; when even it does not fit,
		// the block ends without it.
		if maxBytes > 0 &&
			b.Len()+len(entry) > maxBytes {

			if b.Len()+len(marker) <= maxBytes {

				b.WriteString(marker)
			}

			break
		}

		b.WriteString(entry)
	}

	return b.String()
}
