package store

import (
	"context"
	"database/sql"
	"time"
)

// Turn describes one generation to start. Exactly one of two shapes is valid:
//
//   - send: User != nil. A user message and an assistant message are appended below
//     ExpectedLeaf, which must still be the tip of the visible branch.
//   - regenerate: User == nil and ParentID names an existing user message. A new
//     assistant variant is added below it; every existing continuation is kept.
type Turn struct {
	ConversationID   int64
	EndpointID       int64
	Model            string
	User             *string
	ExpectedLeaf     *int64
	ParentID         *int64
	UserSpeaker      string
	AssistantSpeaker string
	Params           string
	CastRevision     int64
	// ExpectedRevision, when nonzero, must equal the conversation's tree revision.
	ExpectedRevision int64
}

// StartTurn commits the new messages and generation provenance atomically. Validation
// and prompt assembly happen before this, so a rejected prompt leaves no messages.
func (s *Store) StartTurn(ctx context.Context, t Turn) (int64, int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	// The first statement takes the SQLite write lock before checking history.
	res, err := tx.ExecContext(ctx, `UPDATE conversation_cast SET revision=revision WHERE conversation_id=? AND revision=? AND NOT EXISTS(SELECT 1 FROM messages WHERE conversation_id=? AND status='streaming')`, t.ConversationID, t.CastRevision, t.ConversationID)
	if err != nil {
		return 0, 0, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return 0, 0, ErrConflict
	}
	var parent *int64
	var originID *int64
	origin := "original"
	if t.User != nil {
		res, err = tx.ExecContext(ctx, `UPDATE conversations SET revision=revision WHERE id=? AND active_leaf_id IS ? AND (?=0 OR revision=?)`, t.ConversationID, t.ExpectedLeaf, t.ExpectedRevision, t.ExpectedRevision)
		if err != nil {
			return 0, 0, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return 0, 0, ErrConflict
		}
		parent = t.ExpectedLeaf
		userID, err := insertMessage(ctx, tx, t.ConversationID, parent, "user", *t.User, "", "complete", t.UserSpeaker, "original", nil)
		if err != nil {
			return 0, 0, err
		}
		if parent != nil {
			if _, err = tx.ExecContext(ctx, `UPDATE messages SET selected_child_id=? WHERE id=?`, userID, *parent); err != nil {
				return 0, 0, err
			}
		}
		parent = &userID
	} else {
		if t.ParentID == nil {
			return 0, 0, ErrConflict
		}
		var role string
		var selected sql.NullInt64
		err = tx.QueryRowContext(ctx, `SELECT m.role, m.selected_child_id FROM messages m JOIN conversations c ON c.id=m.conversation_id WHERE m.id=? AND m.conversation_id=? AND (?=0 OR c.revision=?)`, *t.ParentID, t.ConversationID, t.ExpectedRevision, t.ExpectedRevision).Scan(&role, &selected)
		if err == sql.ErrNoRows || (err == nil && role != "user") {
			return 0, 0, ErrConflict
		}
		if err != nil {
			return 0, 0, err
		}
		parent = t.ParentID
		// Only a reply that sits beside an existing one is a reroll; the first reply to an
		// edited message is an ordinary reply.
		if selected.Valid {
			origin = "reroll"
			originID = &selected.Int64
		}
	}
	msgID, err := insertMessage(ctx, tx, t.ConversationID, parent, "assistant", "", "", "streaming", t.AssistantSpeaker, origin, originID)
	if err != nil {
		return 0, 0, err
	}
	if err = setLeaf(ctx, tx, t.ConversationID, parent, msgID); err != nil {
		return 0, 0, err
	}
	now := formatTime(time.Now().UTC())
	res, err = tx.ExecContext(ctx, `INSERT INTO generations(message_id,endpoint_id,model_requested,model_reported,params_json,started_at,finish_reason,usage_json,timings_json,error_kind) VALUES(?,?,?,'',?,?,'','{}','{}','')`, msgID, t.EndpointID, t.Model, t.Params, now)
	if err != nil {
		return 0, 0, err
	}
	genID, err := res.LastInsertId()
	if err != nil {
		return 0, 0, err
	}
	return msgID, genID, tx.Commit()
}

func (s *Store) AddGreeting(ctx context.Context, id, revision int64, content, speaker string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// This write also serializes two simultaneous greeting requests.
	res, err := tx.ExecContext(ctx, `UPDATE conversation_cast SET revision=revision+1 WHERE conversation_id=? AND revision=? AND NOT EXISTS(SELECT 1 FROM messages WHERE conversation_id=?)`, id, revision, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	msgID, err := insertMessage(ctx, tx, id, nil, "assistant", content, "", "complete", speaker, "original", nil)
	if err != nil {
		return err
	}
	if err = setLeaf(ctx, tx, id, nil, msgID); err != nil {
		return err
	}
	return tx.Commit()
}
