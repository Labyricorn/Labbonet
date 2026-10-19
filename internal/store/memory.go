package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Labyricorn/Labbonet/internal/memory"
)

// ErrInvalidMemory reports a rejected memory setting or summary edit.
var ErrInvalidMemory = errors.New("invalid memory request")

const (
	maxSummaryBytes   = 64 << 10
	maxEmbeddingRows  = 20000
	embeddingPrune    = 2000
	retrievalPerConv  = 50
	maxEmbeddingBatch = 256
)

// GetMemorySettings returns the stored settings merged over the defaults, so settings
// added by a later version take their default instead of a zero value.
func (s *Store) GetMemorySettings(ctx context.Context) (memory.Settings, error) {
	return readMemorySettings(ctx, s.db)
}

func readMemorySettings(ctx context.Context, q dbq) (memory.Settings, error) {
	var raw string
	var rev int64
	if err := q.QueryRowContext(ctx, `SELECT revision,data_json FROM memory_settings WHERE id=1`).Scan(&rev, &raw); err != nil {
		return memory.Settings{}, err
	}
	out := memory.Defaults()
	if raw != "" && raw != "{}" {
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return memory.Settings{}, fmt.Errorf("read memory settings: %w", err)
		}
	}
	out.Revision = rev
	return out, nil
}

// SaveMemorySettings validates and saves settings if Revision still matches. Chosen
// models must be in the cached inventory with the right type: embeddings for the
// embedding model, a chat model for compression.
func (s *Store) SaveMemorySettings(ctx context.Context, in memory.Settings) (memory.Settings, error) {
	if err := in.Validate(); err != nil {
		return in, fmt.Errorf("%w: %v", ErrInvalidMemory, err)
	}
	if err := s.checkModel(ctx, in.CompressionEndpointID, in.CompressionModel, false); err != nil {
		return in, err
	}
	if err := s.checkModel(ctx, in.EmbeddingEndpointID, in.EmbeddingModel, true); err != nil {
		return in, err
	}
	rev := in.Revision
	in.Revision = 0
	raw, err := json.Marshal(in)
	if err != nil {
		return in, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE memory_settings SET data_json=?,revision=revision+1 WHERE id=1 AND revision=?`, string(raw), rev)
	if err != nil {
		return in, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return in, ErrConflict
	}
	return s.GetMemorySettings(ctx)
}

func (s *Store) checkModel(ctx context.Context, endpointID *int64, modelID string, wantEmbedding bool) error {
	if endpointID == nil {
		return nil
	}
	models, err := s.ListModels(ctx, *endpointID)
	if err != nil {
		return err
	}
	for _, m := range models {
		if m.ModelID != modelID {
			continue
		}
		if (m.Type == "embeddings") != wantEmbedding {
			if wantEmbedding {
				return fmt.Errorf("%w: %s is not an embedding model", ErrInvalidMemory, modelID)
			}
			return fmt.Errorf("%w: %s is an embedding model and cannot write summaries", ErrInvalidMemory, modelID)
		}
		return nil
	}
	return fmt.Errorf("%w: model %q is not in that endpoint's inventory; refresh its models first", ErrInvalidMemory, modelID)
}

// Summary is one revision of a conversation's running summary.
type Summary struct {
	ID               int64     `json:"id"`
	ConversationID   int64     `json:"conversation_id"`
	Seq              int64     `json:"seq"`
	Text             string    `json:"text"`
	ThroughMessageID *int64    `json:"through_message_id"`
	AnchorMessageID  *int64    `json:"anchor_message_id"`
	ArchivedCount    int       `json:"archived_count"`
	Source           string    `json:"source"` // auto, manual or failed
	EndpointID       *int64    `json:"endpoint_id,omitempty"`
	Model            string    `json:"model,omitempty"`
	Note             string    `json:"note,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

const summaryColumns = `id,conversation_id,seq,text,through_message_id,anchor_message_id,archived_count,source,endpoint_id,model,note,created_at`

func scanSummary(row rowScanner) (*Summary, error) {
	var m Summary
	var at string
	if err := row.Scan(&m.ID, &m.ConversationID, &m.Seq, &m.Text, &m.ThroughMessageID, &m.AnchorMessageID, &m.ArchivedCount, &m.Source, &m.EndpointID, &m.Model, &m.Note, &at); err != nil {
		return nil, err
	}
	t, err := parseTime(at)
	if err != nil {
		return nil, err
	}
	m.CreatedAt = t
	return &m, nil
}

// LatestSummarySeq is the highest revision number of the conversation (0 = none); callers
// pass it back to AddSummary so a stale writer cannot overwrite a newer edit.
func (s *Store) LatestSummarySeq(ctx context.Context, convID int64) (int64, error) {
	var seq sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(seq) FROM conversation_summaries WHERE conversation_id=?`, convID).Scan(&seq); err != nil {
		return 0, err
	}
	return seq.Int64, nil
}

// AddSummary appends a revision. expectedSeq must equal the conversation's latest
// revision at the time the caller read it. Messages are never modified: a summary only
// changes what the prompt builder includes.
func (s *Store) AddSummary(ctx context.Context, in Summary, expectedSeq int64) (*Summary, error) {
	if len(in.Text) > maxSummaryBytes || len(in.Note) > 2048 {
		return nil, fmt.Errorf("%w: summary text exceeds 64 KiB", ErrInvalidMemory)
	}
	switch in.Source {
	case "auto", "manual", "failed":
	default:
		return nil, ErrInvalidMemory
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Take the write lock before reading the sequence.
	if _, err = tx.ExecContext(ctx, `UPDATE conversations SET revision=revision WHERE id=?`, in.ConversationID); err != nil {
		return nil, err
	}
	var seq sql.NullInt64
	if err = tx.QueryRowContext(ctx, `SELECT MAX(seq) FROM conversation_summaries WHERE conversation_id=?`, in.ConversationID).Scan(&seq); err != nil {
		return nil, err
	}
	if seq.Int64 != expectedSeq {
		return nil, ErrConflict
	}
	for _, id := range []*int64{in.ThroughMessageID, in.AnchorMessageID} {
		if id == nil {
			continue
		}
		var n int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE id=? AND conversation_id=?`, *id, in.ConversationID).Scan(&n); err != nil {
			return nil, err
		}
		if n != 1 {
			return nil, fmt.Errorf("%w: message does not belong to this conversation", ErrInvalidMemory)
		}
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO conversation_summaries(conversation_id,seq,text,through_message_id,anchor_message_id,archived_count,source,endpoint_id,model,note,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		in.ConversationID, seq.Int64+1, in.Text, in.ThroughMessageID, in.AnchorMessageID, in.ArchivedCount, in.Source, in.EndpointID, in.Model, in.Note, formatTime(time.Now().UTC()))
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	out, err := scanSummary(tx.QueryRowContext(ctx, `SELECT `+summaryColumns+` FROM conversation_summaries WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

// ListSummaries returns revisions newest first.
func (s *Store) ListSummaries(ctx context.Context, convID int64, limit int) ([]Summary, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+summaryColumns+` FROM conversation_summaries WHERE conversation_id=? ORDER BY seq DESC LIMIT ?`, convID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Summary{}
	for rows.Next() {
		m, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// EffectiveSummary picks the newest non-failed revision whose anchor lies on the given
// branch (onPath maps message id to its index on the path). A summary written on another
// branch is therefore never applied here, and the messages it archived there stay
// verbatim on this branch.
func (s *Store) EffectiveSummary(ctx context.Context, convID int64, onPath map[int64]int) (*Summary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+summaryColumns+` FROM conversation_summaries WHERE conversation_id=? AND source<>'failed' ORDER BY seq DESC`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		if m.AnchorMessageID != nil {
			if _, ok := onPath[*m.AnchorMessageID]; !ok {
				continue
			}
		}
		if m.ThroughMessageID != nil {
			if _, ok := onPath[*m.ThroughMessageID]; !ok {
				continue
			}
		}
		return m, nil
	}
	return nil, rows.Err()
}

// GetEmbeddings returns cached vectors for the given content hashes under one exact
// model identity. Vectors that fail validation are treated as absent.
func (s *Store) GetEmbeddings(ctx context.Context, endpointID int64, modelID string, hashes []string) (map[string][]float32, error) {
	out := map[string][]float32{}
	for _, h := range hashes {
		var dim int
		var blob []byte
		err := s.db.QueryRowContext(ctx, `SELECT dim,vec FROM embedding_cache WHERE hash=? AND endpoint_id=? AND model_id=?`, h, endpointID, modelID).Scan(&dim, &blob)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		if v, err := memory.DecodeVector(blob, dim); err == nil {
			out[h] = v
		}
	}
	return out, nil
}

// PutEmbeddings stores vectors and trims the oldest entries beyond the cache limit.
func (s *Store) PutEmbeddings(ctx context.Context, endpointID int64, modelID string, vectors map[string][]float32) error {
	if len(vectors) > maxEmbeddingBatch {
		return ErrInvalidMemory
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := formatTime(time.Now().UTC())
	for h, v := range vectors {
		if !memory.ValidVector(v) {
			return ErrInvalidMemory
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO embedding_cache(hash,endpoint_id,model_id,dim,vec,created_at) VALUES(?,?,?,?,?,?) ON CONFLICT(hash,endpoint_id,model_id) DO UPDATE SET dim=excluded.dim,vec=excluded.vec,created_at=excluded.created_at`, h, endpointID, modelID, len(v), memory.EncodeVector(v), now); err != nil {
			return err
		}
	}
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM embedding_cache`).Scan(&n); err != nil {
		return err
	}
	if n > maxEmbeddingRows {
		if _, err = tx.ExecContext(ctx, `DELETE FROM embedding_cache WHERE rowid IN (SELECT rowid FROM embedding_cache ORDER BY created_at LIMIT ?)`, embeddingPrune); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// EmbeddingCacheStats counts cached vectors, optionally per model.
type EmbeddingCacheStat struct {
	EndpointID int64  `json:"endpoint_id"`
	ModelID    string `json:"model_id"`
	Vectors    int    `json:"vectors"`
}

func (s *Store) EmbeddingCacheStats(ctx context.Context) ([]EmbeddingCacheStat, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT endpoint_id,model_id,COUNT(*) FROM embedding_cache GROUP BY endpoint_id,model_id ORDER BY endpoint_id,model_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EmbeddingCacheStat{}
	for rows.Next() {
		var st EmbeddingCacheStat
		if err := rows.Scan(&st.EndpointID, &st.ModelID, &st.Vectors); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// ClearEmbeddingCache removes every cached vector; they are recomputed on demand.
func (s *Store) ClearEmbeddingCache(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM embedding_cache`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RetrievalRecord is one stored telemetry row (the JSON is a memory.Record).
type RetrievalRecord struct {
	ID             int64           `json:"id"`
	ConversationID int64           `json:"conversation_id"`
	GenerationID   *int64          `json:"generation_id"`
	Record         json.RawMessage `json:"record"`
	CreatedAt      time.Time       `json:"created_at"`
}

// AddRetrievalRecord stores one record and keeps only the newest few per conversation.
func (s *Store) AddRetrievalRecord(ctx context.Context, convID int64, generationID *int64, record any) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO retrieval_records(conversation_id,generation_id,record_json,created_at) VALUES(?,?,?,?)`, convID, generationID, string(raw), formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM retrieval_records WHERE conversation_id=? AND id NOT IN (SELECT id FROM retrieval_records WHERE conversation_id=? ORDER BY id DESC LIMIT ?)`, convID, convID, retrievalPerConv); err != nil {
		return err
	}
	return tx.Commit()
}

// ListRetrievalRecords returns the newest records first.
func (s *Store) ListRetrievalRecords(ctx context.Context, convID int64, limit int) ([]RetrievalRecord, error) {
	if limit <= 0 || limit > retrievalPerConv {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,conversation_id,generation_id,record_json,created_at FROM retrieval_records WHERE conversation_id=? ORDER BY id DESC LIMIT ?`, convID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RetrievalRecord{}
	for rows.Next() {
		var r RetrievalRecord
		var raw, at string
		if err := rows.Scan(&r.ID, &r.ConversationID, &r.GenerationID, &raw, &at); err != nil {
			return nil, err
		}
		r.Record = json.RawMessage(raw)
		if r.CreatedAt, err = parseTime(at); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ManualAnchor picks the message a hand-edited or cleared summary is scoped to, independently of
// how far the summary archives. It is the user message the visible reply answers, so rerolling
// the last reply (a new sibling under that user message) keeps the edit, while a branch that
// does not contain that user message does not see it. With no reply yet it is the leaf.
func ManualAnchor(path []PathMessage) *int64 {
	if len(path) == 0 {
		return nil
	}
	i := len(path) - 1
	if path[i].Role == "assistant" && i > 0 && path[i-1].Role == "user" {
		i--
	}
	id := path[i].ID
	return &id
}
