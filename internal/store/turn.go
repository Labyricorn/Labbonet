package store

import (
	"context"
	"time"
)

// StartTurn commits both speakers and generation provenance atomically. Validation
// and prompt assembly happen before this, so a rejected prompt leaves no messages.
func (s *Store) StartTurn(ctx context.Context, convID, endpointID int64, model, user, userSpeaker, assistantSpeaker, params string, expectedMessages int, castRevision int64) (int64, int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	// The first statement takes the SQLite write lock before checking history.
	res, err := tx.ExecContext(ctx, `UPDATE conversation_cast SET revision=revision WHERE conversation_id=? AND revision=? AND (SELECT COUNT(*) FROM messages WHERE conversation_id=?)=? AND NOT EXISTS(SELECT 1 FROM messages WHERE conversation_id=? AND status='streaming')`, convID, castRevision, convID, expectedMessages, convID)
	if err != nil {
		return 0, 0, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return 0, 0, ErrConflict
	}
	now := formatTime(time.Now().UTC())
	if _, err = tx.ExecContext(ctx, `INSERT INTO messages(conversation_id,role,content,reasoning,status,created_at,speaker_json) VALUES(?,'user',?,'','complete',?,?)`, convID, user, now, userSpeaker); err != nil {
		return 0, 0, err
	}
	res, err = tx.ExecContext(ctx, `INSERT INTO messages(conversation_id,role,content,reasoning,status,created_at,speaker_json) VALUES(?,'assistant','','','streaming',?,?)`, convID, now, assistantSpeaker)
	if err != nil {
		return 0, 0, err
	}
	msgID, err := res.LastInsertId()
	if err != nil {
		return 0, 0, err
	}
	res, err = tx.ExecContext(ctx, `INSERT INTO generations(message_id,endpoint_id,model_requested,model_reported,params_json,started_at,finish_reason,usage_json,timings_json,error_kind) VALUES(?,?,?,'',?,?,'','{}','{}','')`, msgID, endpointID, model, params, now)
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
	_, err = tx.ExecContext(ctx, `INSERT INTO messages(conversation_id,role,content,reasoning,status,created_at,speaker_json) VALUES(?,'assistant',?,'','complete',?,?)`, id, content, formatTime(time.Now().UTC()), speaker)
	if err != nil {
		return err
	}
	return tx.Commit()
}
