package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// JobName is the scheduled garbage collection of unreferenced uploads.
const JobName = "media.gc"

// JobSchedule runs the collection once a day.
const JobSchedule = "@daily"

// GCResult counts what a collection removed.
type GCResult struct {
	Uploads int // records (and their bytes) of unreferenced uploads
	Orphans int // stored bytes that had no record
}

const gcPage = 500

// GC removes uploads that no item version or draft references and that were last touched more than GCAge
// ago, then stored bytes without a record that are as old. The files an item holds are found by reading the
// versions and drafts of every item in the database, whatever its type (a field removed or renamed in the
// schema still pins the files of the versions that had it), trashed items included; a purge that removes an
// item leaves its files for the next run. Writing an item that attaches a file touches the upload in the
// write's transaction, so a file attached while a run is in progress is kept.
//
// What the run cannot see: a file that lives only in unsaved collaborative state or in a relayed preview
// draft for longer than GCAge. Those windows are hours, GCAge is at least a day.
//
// Removing the bytes of a record that was just deleted is serialised with storing the same file (in this
// process) and re-checks the record first; an upload of such a file stores its bytes again if they are gone.
func (s *Service) GC(ctx context.Context) (GCResult, error) {
	var res GCResult
	cutoff := s.o.Clock.Now().Add(-s.o.GCAge)
	var refs map[string]struct{}
	after := ""
	for {
		var stale []store.Upload
		if err := s.o.DB.View(ctx, func(tx store.Tx) error {
			var err error
			stale, err = tx.Uploads().ListStale(ctx, cutoff, after, gcPage)
			return err
		}); err != nil {
			return res, err
		}
		if len(stale) == 0 {
			break
		}
		if refs == nil { // nothing stale, nothing to scan
			var err error
			if refs, err = s.referenced(ctx); err != nil {
				return res, err
			}
		}
		after = stale[len(stale)-1].SHA
		var gone []string
		if err := s.o.DB.Tx(ctx, func(tx store.Tx) error {
			gone = gone[:0]
			for _, u := range stale {
				if _, used := refs[u.SHA]; used {
					continue
				}
				ok, err := tx.Uploads().DeleteStale(ctx, u.SHA, cutoff)
				if err != nil {
					return err
				}
				if ok {
					gone = append(gone, u.SHA)
				}
			}
			if len(gone) == 0 {
				return nil
			}
			if err := s.o.Audit.Write(ctx, tx, audit.Entry{Actor: audit.System, Action: "media.gc", Data: map[string]any{"count": len(gone), "files": gone}}); err != nil {
				return err
			}
			_, err := s.o.Outbox.Publish(ctx, tx, "media.deleted", map[string]any{"files": gone})
			return err
		}); err != nil {
			return res, err
		}
		for _, sha := range gone {
			if _, err := s.dropBytes(ctx, sha); err != nil {
				return res, fmt.Errorf("media: delete %s: %w", sha, err)
			}
			res.Uploads++
		}
	}

	var err error
	res.Orphans, err = s.collectOrphans(ctx, cutoff)
	if res.Uploads+res.Orphans > 0 {
		s.o.Log.Info("media: unreferenced files removed", "uploads", res.Uploads, "orphans", res.Orphans)
	}
	return res, err
}

// dropBytes deletes the bytes of a file that has no record. Under the file's lock it looks at the record
// once more, so bytes an upload has just recorded stay; it reports whether it deleted.
func (s *Service) dropBytes(ctx context.Context, sha string) (bool, error) {
	unlock := s.lock(sha)
	defer unlock()
	if _, err := s.record(ctx, sha); err == nil {
		return false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return false, err
	}
	return true, s.o.Storage.Delete(ctx, sha)
}

// collectOrphans deletes stored files that have no upload record and are older than cutoff: leftovers of an
// upload that stored its bytes and then failed, or of an interrupted collection.
func (s *Service) collectOrphans(ctx context.Context, cutoff time.Time) (int, error) {
	n, after := 0, ""
	for {
		entries, err := s.o.Storage.List(ctx, "", after, gcPage)
		if err != nil {
			return n, err
		}
		if len(entries) == 0 {
			return n, nil
		}
		after = entries[len(entries)-1].Key
		for _, e := range entries {
			if !IsSha(e.Key) || e.ModTime.IsZero() || !e.ModTime.Before(cutoff) {
				continue
			}
			dropped, err := s.dropBytes(ctx, e.Key)
			if err != nil {
				return n, err
			}
			if dropped {
				n++
			}
		}
	}
}

// collect adds every string of doc that is a SHA-256 to refs. Reading all strings, wherever the value sits
// (a file field in a group, a list or a block's props), keeps the collection independent of the schema; a text
// that merely looks like a hash keeps its file a little longer, which is harmless.
func collect(refs map[string]struct{}, doc []byte) {
	if len(doc) == 0 {
		return
	}
	dec := json.NewDecoder(bytes.NewReader(doc))
	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		if str, ok := tok.(string); ok && IsSha(str) {
			refs[str] = struct{}{}
		}
	}
}

// referenced collects the SHAs held by every version and draft of every item of every type present in the
// database, trashed items included.
func (s *Service) referenced(ctx context.Context) (map[string]struct{}, error) {
	var types []string
	if err := s.o.DB.View(ctx, func(tx store.Tx) error {
		ex, err := store.ExportOf(tx)
		if err != nil {
			return err
		}
		types, err = ex.ItemTypes(ctx)
		return err
	}); err != nil {
		return nil, err
	}
	refs := map[string]struct{}{}
	for _, typ := range types {
		after := ids.Nil
		for {
			var batch []ids.UUID
			err := store.ViewConsistent(ctx, s.o.DB, func(tx store.Tx) error {
				var err error
				if batch, err = tx.Content().ItemIDsOfType(ctx, typ, after, gcPage); err != nil || len(batch) == 0 {
					return err
				}
				ex, err := store.ExportOf(tx)
				if err != nil {
					return err
				}
				versions, err := ex.VersionsOf(ctx, batch, false)
				if err != nil {
					return err
				}
				for _, v := range versions {
					collect(refs, v.Shared)
					for _, l := range v.Locales {
						collect(refs, l.Data)
					}
				}
				parts, err := ex.DraftPartsFor(ctx, batch)
				if err != nil {
					return err
				}
				for _, p := range parts {
					collect(refs, p.Data)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
			if len(batch) == 0 {
				break
			}
			after = batch[len(batch)-1]
		}
	}
	return refs, nil
}
