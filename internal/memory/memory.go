// Package memory is ox's long-term store: distilled knowledge (learnings,
// gotchas, conventions, architecture facts) retrievable by hybrid search —
// FTS5 keyword rank fused with embedding cosine similarity. Vectors live as
// BLOBs and are scanned brute-force: at the realistic scale (thousands of
// rows) that is milliseconds, and it keeps the store dependency-free.
package memory

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"

	"github.com/ashvinbhat/ox/internal/memory/embed"
)

var Kinds = []string{"learning", "gotcha", "convention", "architecture", "decision", "tool", "profile", "failure"}

type Memory struct {
	ID             int64      `json:"-"`
	UID            string     `json:"uid"`
	Kind           string     `json:"kind"`
	Scope          string     `json:"scope"`
	Title          string     `json:"title"`
	Content        string     `json:"content"`
	Tags           []string   `json:"tags,omitempty"`
	Source         string     `json:"source,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	UseCount       int        `json:"use_count"`
	SupersededBy   *int64     `json:"-"`
	ArchivedAt     *time.Time `json:"-"`
	Score          float64    `json:"score,omitempty"`
	embedding      []float32
	embeddingModel string
}

type Store struct {
	db       *sql.DB
	embedder embed.Embedder
}

func Open(oxHome string, embedder embed.Embedder) (*Store, error) {
	db, err := sql.Open("sqlite", filepath.Join(oxHome, "memory.db"))
	if err != nil {
		return nil, fmt.Errorf("open memory.db: %w", err)
	}
	db.SetMaxOpenConns(1)

	s := &Store{db: db, embedder: embedder}
	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) init() error {
	_, err := s.db.Exec(`
PRAGMA journal_mode=WAL;

CREATE TABLE IF NOT EXISTS memories (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  uid             TEXT NOT NULL UNIQUE,
  kind            TEXT NOT NULL,
  scope           TEXT NOT NULL DEFAULT 'global',
  title           TEXT NOT NULL,
  content         TEXT NOT NULL,
  tags            TEXT NOT NULL DEFAULT '[]',
  source          TEXT NOT NULL DEFAULT '',
  embedding       BLOB,
  embedding_model TEXT,
  created_at      TEXT NOT NULL,
  last_used_at    TEXT,
  use_count       INTEGER NOT NULL DEFAULT 0,
  superseded_by   INTEGER REFERENCES memories(id),
  archived_at     TEXT
);
CREATE INDEX IF NOT EXISTS idx_mem_scope ON memories(scope) WHERE archived_at IS NULL AND superseded_by IS NULL;
CREATE INDEX IF NOT EXISTS idx_mem_kind  ON memories(kind);

CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
  title, content, tags,
  content='memories', content_rowid='id',
  tokenize='porter unicode61'
);

CREATE TRIGGER IF NOT EXISTS mem_ai AFTER INSERT ON memories BEGIN
  INSERT INTO memories_fts(rowid, title, content, tags) VALUES (new.id, new.title, new.content, new.tags);
END;
CREATE TRIGGER IF NOT EXISTS mem_ad AFTER DELETE ON memories BEGIN
  INSERT INTO memories_fts(memories_fts, rowid, title, content, tags) VALUES ('delete', old.id, old.title, old.content, old.tags);
END;
CREATE TRIGGER IF NOT EXISTS mem_au AFTER UPDATE OF title, content, tags ON memories BEGIN
  INSERT INTO memories_fts(memories_fts, rowid, title, content, tags) VALUES ('delete', old.id, old.title, old.content, old.tags);
  INSERT INTO memories_fts(rowid, title, content, tags) VALUES (new.id, new.title, new.content, new.tags);
END;

CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT);
`)
	return err
}

// RememberResult reports what the dedupe gate decided.
type RememberResult struct {
	UID    string `json:"uid"`
	Status string `json:"status"` // created | duplicate_of:<uid> | superseded:<uid>
}

type RememberInput struct {
	Content    string
	Kind       string
	Scope      string
	Title      string
	Tags       []string
	Source     string
	Supersedes string // uid
}

// Remember stores a memory through the dedupe gate: near-identical content is
// skipped (existing row's usage bumped), close-but-newer content supersedes.
func (s *Store) Remember(ctx context.Context, in RememberInput) (*RememberResult, error) {
	if in.Content == "" {
		return nil, fmt.Errorf("content required")
	}
	if !validKind(in.Kind) {
		return nil, fmt.Errorf("kind must be one of %s", strings.Join(Kinds, "|"))
	}
	if in.Scope == "" {
		in.Scope = "global"
	}
	if in.Title == "" {
		in.Title = firstSentence(in.Content)
	}

	var vec []float32
	if s.embedder != nil {
		ectx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if vecs, err := s.embedder.Embed(ectx, []string{in.Title + "\n" + in.Content}, embed.InputDocument); err == nil && len(vecs) == 1 {
			vec = vecs[0]
		}
	}

	// Explicit supersede wins regardless of similarity.
	if in.Supersedes != "" {
		old, err := s.getByUID(in.Supersedes)
		if err != nil {
			return nil, fmt.Errorf("supersedes target %s: %w", in.Supersedes, err)
		}
		newUID, err := s.insert(in, vec)
		if err != nil {
			return nil, err
		}
		if err := s.markSuperseded(old.UID, newUID); err != nil {
			return nil, err
		}
		return &RememberResult{UID: newUID, Status: "superseded:" + old.UID}, nil
	}

	// Dedupe gate against nearest existing memories in the same scope family.
	if vec != nil {
		near := s.nearest(vec, []string{in.Scope, "global"}, 3)
		for _, cand := range near {
			if cand.Score >= 0.90 {
				s.bumpUsage([]int64{cand.ID})
				return &RememberResult{UID: cand.UID, Status: "duplicate_of:" + cand.UID}, nil
			}
			if cand.Score >= 0.78 && cand.Kind == in.Kind {
				newUID, err := s.insert(in, vec)
				if err != nil {
					return nil, err
				}
				if err := s.markSuperseded(cand.UID, newUID); err != nil {
					return nil, err
				}
				return &RememberResult{UID: newUID, Status: "superseded:" + cand.UID}, nil
			}
		}
	} else if uid, ok := s.exactTitleMatch(in.Title, in.Scope); ok {
		// Degraded mode: only exact-title dedupe is safe without vectors.
		return &RememberResult{UID: uid, Status: "duplicate_of:" + uid}, nil
	}

	uid, err := s.insert(in, vec)
	if err != nil {
		return nil, err
	}
	return &RememberResult{UID: uid, Status: "created"}, nil
}

type SearchOptions struct {
	Scopes          []string // always unioned with "global"
	Kinds           []string
	K               int
	IncludeArchived bool
}

// Search runs hybrid retrieval: FTS5 rank + vector cosine, fused with
// reciprocal-rank fusion, damped by staleness, boosted for exact repo scope.
func (s *Store) Search(ctx context.Context, query string, opts SearchOptions) ([]Memory, bool, error) {
	if opts.K <= 0 {
		opts.K = 8
	}
	if opts.K > 25 {
		opts.K = 25
	}
	// Explicit scopes widen to include global; no scopes means search all.
	scopes := opts.Scopes
	if len(scopes) > 0 {
		scopes = unionGlobal(scopes)
	}

	ftsRank, err := s.ftsCandidates(query, scopes, opts, 50)
	if err != nil {
		return nil, false, err
	}

	degraded := s.embedder == nil
	var vecRank []Memory
	if !degraded {
		ectx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if vecs, err := s.embedder.Embed(ectx, []string{query}, embed.InputQuery); err == nil && len(vecs) == 1 {
			vecRank = s.vectorCandidates(vecs[0], scopes, opts, 50)
		} else {
			degraded = true
		}
	}

	// Reciprocal rank fusion over the two lists.
	type scored struct {
		mem  *Memory
		rrf  float64
		best float64 // best cosine seen (for near-dup collapse)
	}
	byID := map[int64]*scored{}
	addList := func(list []Memory, weight float64) {
		for rank, m := range list {
			sc, ok := byID[m.ID]
			if !ok {
				mm := m
				sc = &scored{mem: &mm}
				byID[m.ID] = sc
			}
			sc.rrf += weight / float64(60+rank+1)
			if m.Score > sc.best {
				sc.best = m.Score
			}
		}
	}
	addList(ftsRank, 1.0)
	if !degraded {
		addList(vecRank, 1.0)
	}

	now := time.Now()
	var results []*scored
	for _, sc := range byID {
		ref := sc.mem.CreatedAt
		if sc.mem.LastUsedAt != nil {
			ref = *sc.mem.LastUsedAt
		}
		days := now.Sub(ref).Hours() / 24
		fresh := 0.7 + 0.3*math.Exp(-days/270)
		boost := 1.0
		for _, scope := range opts.Scopes {
			if sc.mem.Scope == scope && strings.HasPrefix(scope, "repo:") {
				boost = 1.15
				break
			}
		}
		sc.mem.Score = sc.rrf * fresh * boost
		results = append(results, sc)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].mem.Score > results[j].mem.Score })

	// Near-duplicate collapse among the head of the list.
	var out []Memory
	var kept []*scored
	for _, sc := range results {
		dup := false
		if sc.mem.embedding != nil {
			for _, k := range kept {
				if k.mem.embedding != nil && dot(sc.mem.embedding, k.mem.embedding) >= 0.97 {
					dup = true
					break
				}
			}
		}
		if dup {
			continue
		}
		kept = append(kept, sc)
		out = append(out, *sc.mem)
		if len(out) == opts.K {
			break
		}
	}

	var ids []int64
	for _, m := range out {
		ids = append(ids, m.ID)
	}
	s.bumpUsage(ids)

	return out, degraded, nil
}

// Get returns one memory by uid, including its supersession chain.
func (s *Store) Get(uid string) (*Memory, error) { return s.getByUID(uid) }

// Backfill embeds rows missing a vector for the current embedding model.
// Returns how many rows were embedded.
func (s *Store) Backfill(ctx context.Context, reembed bool) (int, error) {
	if s.embedder == nil {
		return 0, fmt.Errorf("no embedding provider configured")
	}
	where := "embedding IS NULL OR embedding_model IS NULL"
	if !reembed {
		where += " OR embedding_model != ?"
	} else {
		where = "1=1"
	}
	query := fmt.Sprintf(`SELECT id, title, content FROM memories WHERE archived_at IS NULL AND (%s)`, where)
	var rows *sql.Rows
	var err error
	if !reembed {
		rows, err = s.db.Query(query, s.embedder.Model())
	} else {
		rows, err = s.db.Query(query)
	}
	if err != nil {
		return 0, err
	}

	type item struct {
		id   int64
		text string
	}
	var items []item
	for rows.Next() {
		var it item
		var title, content string
		if rows.Scan(&it.id, &title, &content) == nil {
			it.text = title + "\n" + content
			items = append(items, it)
		}
	}
	rows.Close()

	count := 0
	for start := 0; start < len(items); start += 64 {
		end := min(start+64, len(items))
		texts := make([]string, 0, end-start)
		for _, it := range items[start:end] {
			texts = append(texts, it.text)
		}
		vecs, err := s.embedder.Embed(ctx, texts, embed.InputDocument)
		if err != nil {
			return count, err
		}
		for i, vec := range vecs {
			if _, err := s.db.Exec(`UPDATE memories SET embedding=?, embedding_model=? WHERE id=?`,
				encodeVec(vec), s.embedder.Model(), items[start+i].id); err != nil {
				return count, err
			}
			count++
		}
	}
	return count, nil
}

// GC archives superseded rows, long-unused rows, and rows scoped to dead tasks.
func (s *Store) GC(deadTaskSeqs []int) (int, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(`
UPDATE memories SET archived_at=? WHERE archived_at IS NULL AND (
  superseded_by IS NOT NULL
  OR (use_count = 0 AND created_at < ?)
)`, now, time.Now().AddDate(0, 0, -180).UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()

	for _, seq := range deadTaskSeqs {
		res, err := s.db.Exec(`UPDATE memories SET archived_at=? WHERE archived_at IS NULL AND scope=?`,
			now, fmt.Sprintf("task:%d", seq))
		if err == nil {
			nn, _ := res.RowsAffected()
			n += nn
		}
	}
	return int(n), nil
}

// Count returns active (non-archived, non-superseded) rows per scope.
func (s *Store) Count() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT scope, COUNT(*) FROM memories WHERE archived_at IS NULL AND superseded_by IS NULL GROUP BY scope`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var scope string
		var n int
		if rows.Scan(&scope, &n) == nil {
			out[scope] = n
		}
	}
	return out, nil
}

// --- internals ---

func (s *Store) insert(in RememberInput, vec []float32) (string, error) {
	uid := ulid.Make().String()
	tags, _ := json.Marshal(in.Tags)
	var embBlob any
	var embModel any
	if vec != nil {
		embBlob = encodeVec(vec)
		embModel = s.embedder.Model()
	}
	_, err := s.db.Exec(`
INSERT INTO memories (uid, kind, scope, title, content, tags, source, embedding, embedding_model, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uid, in.Kind, in.Scope, in.Title, in.Content, string(tags), in.Source, embBlob, embModel,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return "", fmt.Errorf("insert memory: %w", err)
	}
	return uid, nil
}

func (s *Store) markSuperseded(oldUID, newUID string) error {
	_, err := s.db.Exec(`
UPDATE memories SET superseded_by = (SELECT id FROM memories WHERE uid=?)
WHERE uid=?`, newUID, oldUID)
	return err
}

func (s *Store) bumpUsage(ids []int64) {
	if len(ids) == 0 {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range ids {
		s.db.Exec(`UPDATE memories SET use_count=use_count+1, last_used_at=? WHERE id=?`, now, id)
	}
}

func (s *Store) getByUID(uid string) (*Memory, error) {
	row := s.db.QueryRow(`SELECT `+cols+` FROM memories WHERE uid=?`, uid)
	m, err := scanMemory(row)
	if err != nil {
		return nil, fmt.Errorf("memory %s: %w", uid, err)
	}
	return m, nil
}

func (s *Store) exactTitleMatch(title, scope string) (string, bool) {
	var uid string
	err := s.db.QueryRow(`
SELECT uid FROM memories
WHERE archived_at IS NULL AND superseded_by IS NULL AND title=? AND scope IN (?, 'global')
LIMIT 1`, title, scope).Scan(&uid)
	return uid, err == nil
}

func (s *Store) ftsCandidates(query string, scopes []string, opts SearchOptions, limit int) ([]Memory, error) {
	match := ftsQuery(query)
	if match == "" {
		return nil, nil
	}
	q := `
SELECT ` + colsPrefixed("m") + `
FROM memories_fts JOIN memories m ON m.id = memories_fts.rowid
WHERE memories_fts MATCH ?` + scopeClause("m", scopes) + kindClause("m", opts.Kinds) + archiveClause("m", opts.IncludeArchived) + `
ORDER BY memories_fts.rank LIMIT ?`
	rows, err := s.db.Query(q, match, limit)
	if err != nil {
		return nil, fmt.Errorf("fts query: %w", err)
	}
	defer rows.Close()
	var out []Memory
	for rows.Next() {
		if m, err := scanMemory(rows); err == nil {
			out = append(out, *m)
		}
	}
	return out, nil
}

func (s *Store) vectorCandidates(qvec []float32, scopes []string, opts SearchOptions, limit int) []Memory {
	q := `SELECT ` + cols + ` FROM memories WHERE embedding IS NOT NULL AND embedding_model=?` +
		scopeClause("", scopes) + kindClause("", opts.Kinds) + archiveClause("", opts.IncludeArchived)
	rows, err := s.db.Query(q, s.embedder.Model())
	if err != nil {
		return nil
	}
	defer rows.Close()

	var cands []Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil || m.embedding == nil {
			continue
		}
		m.Score = float64(dot(qvec, m.embedding))
		if m.Score >= 0.55 {
			cands = append(cands, *m)
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].Score > cands[j].Score })
	if len(cands) > limit {
		cands = cands[:limit]
	}
	return cands
}

// nearest is the dedupe-gate lookup: pure vector similarity, no FTS.
func (s *Store) nearest(vec []float32, scopes []string, k int) []Memory {
	cands := s.vectorCandidates(vec, unionGlobal(scopes), SearchOptions{}, k)
	return cands
}

const cols = `id, uid, kind, scope, title, content, tags, source, embedding, embedding_model, created_at, last_used_at, use_count, superseded_by, archived_at`

func colsPrefixed(p string) string {
	parts := strings.Split(cols, ", ")
	for i, c := range parts {
		parts[i] = p + "." + c
	}
	return strings.Join(parts, ", ")
}

type scannable interface{ Scan(...any) error }

func scanMemory(row scannable) (*Memory, error) {
	var m Memory
	var tags string
	var emb []byte
	var embModel, lastUsed, archived sql.NullString
	var superseded sql.NullInt64
	var created string
	if err := row.Scan(&m.ID, &m.UID, &m.Kind, &m.Scope, &m.Title, &m.Content, &tags, &m.Source,
		&emb, &embModel, &created, &lastUsed, &m.UseCount, &superseded, &archived); err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(tags), &m.Tags)
	m.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if lastUsed.Valid {
		t, _ := time.Parse(time.RFC3339, lastUsed.String)
		m.LastUsedAt = &t
	}
	if superseded.Valid {
		v := superseded.Int64
		m.SupersededBy = &v
	}
	if archived.Valid {
		t, _ := time.Parse(time.RFC3339, archived.String)
		m.ArchivedAt = &t
	}
	if len(emb) > 0 {
		m.embedding = decodeVec(emb)
		m.embeddingModel = embModel.String
	}
	return &m, nil
}

func scopeClause(prefix string, scopes []string) string {
	if len(scopes) == 0 {
		return ""
	}
	col := "scope"
	if prefix != "" {
		col = prefix + ".scope"
	}
	quoted := make([]string, len(scopes))
	for i, s := range scopes {
		quoted[i] = "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	return fmt.Sprintf(" AND %s IN (%s)", col, strings.Join(quoted, ","))
}

func kindClause(prefix string, kinds []string) string {
	if len(kinds) == 0 {
		return ""
	}
	col := "kind"
	if prefix != "" {
		col = prefix + ".kind"
	}
	var quoted []string
	for _, k := range kinds {
		if validKind(k) {
			quoted = append(quoted, "'"+k+"'")
		}
	}
	if len(quoted) == 0 {
		return ""
	}
	return fmt.Sprintf(" AND %s IN (%s)", col, strings.Join(quoted, ","))
}

func archiveClause(prefix string, includeArchived bool) string {
	p := ""
	if prefix != "" {
		p = prefix + "."
	}
	if includeArchived {
		return ""
	}
	return fmt.Sprintf(" AND %sarchived_at IS NULL AND %ssuperseded_by IS NULL", p, p)
}

// ftsQuery sanitizes free text into an OR-of-terms MATCH expression; FTS5
// treats bare punctuation as syntax, so terms are quoted.
func ftsQuery(query string) string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	var terms []string
	for _, f := range fields {
		if len(f) < 2 || stopwords[f] {
			continue
		}
		terms = append(terms, `"`+f+`"`)
	}
	return strings.Join(terms, " OR ")
}

var stopwords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "for": true,
	"to": true, "of": true, "in": true, "on": true, "is": true, "it": true,
	"with": true, "how": true, "what": true, "why": true, "when": true,
	"do": true, "does": true, "this": true, "that": true, "we": true,
}

func unionGlobal(scopes []string) []string {
	out := append([]string{}, scopes...)
	if slices.Contains(out, "global") {
		return out
	}
	return append(out, "global")
}

func validKind(k string) bool {
	return slices.Contains(Kinds, k)
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	for _, sep := range []string{". ", "\n"} {
		if i := strings.Index(s, sep); i > 0 && i < 80 {
			return s[:i+1]
		}
	}
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}

func encodeVec(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(x))
	}
	return buf
}

func decodeVec(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

func dot(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var sum float32
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}
