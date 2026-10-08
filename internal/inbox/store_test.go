package inbox

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"sync"
	"testing"
)

var ctx = context.Background()

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func create() Operation {
	return Operation{ID: uuid.NewString(), ItemID: uuid.NewString(), Type: "create", Kind: "todo", Body: "sample"}
}
func apply(t *testing.T, s *Store, node string, op Operation) Item {
	t.Helper()
	i, err := s.Apply(ctx, node, op)
	if err != nil {
		t.Fatal(err)
	}
	return i
}
func code(t *testing.T, err error, want string) {
	t.Helper()
	var f *Fault
	if !errors.As(err, &f) || f.Code != want {
		t.Fatalf("got %v want %s", err, want)
	}
}
func TestReceiptSurvivesRestartAndPrecedesRevisionCheck(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	op := create()
	original := apply(t, s, "a", op)
	edit := Operation{ID: uuid.NewString(), ItemID: original.ID, Type: "update", Body: "edited", ExpectedRevision: 1}
	apply(t, s, "b", edit)
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := apply(t, s, "a", op); got != original {
		t.Fatal("retry did not return original receipt")
	}
	op.Body = "different"
	_, err = s.Apply(ctx, "a", op)
	code(t, err, "operation_id_reused")
	generation, _, _ := s.Watermark(ctx)
	page, err := s.Changes(ctx, generation, 0, 100)
	if err != nil || len(page.Changes) != 2 {
		t.Fatalf("duplicate change: %+v %v", page, err)
	}
}
func TestConcurrentWritersConflictAndDeleteSync(t *testing.T) {
	s := newStore(t)
	item := apply(t, s, "a", create())
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, node := range []string{"a", "b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Apply(ctx, node, Operation{ID: uuid.NewString(), ItemID: item.ID, Type: "update", ExpectedRevision: 1, Body: node})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			code(t, err, "conflict")
		}
	}
	if successes != 1 {
		t.Fatal(successes)
	}
	deleted := apply(t, s, "b", Operation{ID: uuid.NewString(), ItemID: item.ID, Type: "delete", ExpectedRevision: 2})
	if deleted.DeletedAt == "" {
		t.Fatal("no tombstone")
	}
	generation, _, _ := s.Watermark(ctx)
	page, err := s.Changes(ctx, generation, 2, 1)
	if err != nil || len(page.Changes) != 1 || page.Changes[0].Item.DeletedAt == "" {
		t.Fatal(page, err)
	}
}
func TestSnapshotBoundaryAcrossEditsDeletesAndCreates(t *testing.T) {
	s := newStore(t)
	for range 4 {
		apply(t, s, "a", create())
	}
	first, err := s.Snapshot(ctx, "", nil, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	all, err := s.Snapshot(ctx, "", nil, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	apply(t, s, "b", Operation{ID: uuid.NewString(), ItemID: all.Items[2].ID, Type: "delete", ExpectedRevision: 1})
	apply(t, s, "b", Operation{ID: uuid.NewString(), ItemID: all.Items[3].ID, Type: "update", ExpectedRevision: 1, Body: "new"})
	apply(t, s, "b", create())
	items := first.Items
	for first.HasMore {
		first, err = s.Snapshot(ctx, first.Generation, &first.Cursor, first.NextID, 1)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, first.Items...)
	}
	if len(items) != 4 {
		t.Fatal(len(items))
	}
	for _, item := range items {
		if item.Revision != 1 || item.DeletedAt != "" {
			t.Fatal("snapshot moved", item)
		}
	}
	delta, err := s.Changes(ctx, first.Generation, first.Cursor, 100)
	if err != nil || len(delta.Changes) != 3 {
		t.Fatal(delta, err)
	}
	if err = s.ResetGeneration(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = s.Changes(ctx, first.Generation, first.Cursor, 100)
	code(t, err, "reset_required")
}
func TestWriteFailureRollsBackItemAndReceipt(t *testing.T) {
	s := newStore(t)
	_, err := s.db.Exec(`CREATE TRIGGER reject_change BEFORE INSERT ON changes BEGIN SELECT RAISE(FAIL,'injected'); END`)
	if err != nil {
		t.Fatal(err)
	}
	op := create()
	if _, err = s.Apply(ctx, "a", op); err == nil {
		t.Fatal("expected injected failure")
	}
	for _, table := range []string{"items", "changes", "receipts"} {
		var n int
		if err = s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
}
func TestCompletedRetryDoesNotToggle(t *testing.T) {
	s := newStore(t)
	item := apply(t, s, "a", create())
	value := true
	op := Operation{ID: uuid.NewString(), ItemID: item.ID, Type: "set_completed", ExpectedRevision: 1, Completed: &value}
	done := apply(t, s, "a", op)
	if !done.Completed || apply(t, s, "a", op) != done {
		t.Fatal("not idempotent")
	}
}

func TestKindChangesPreserveContentAndSync(t *testing.T) {
	s := newStore(t)
	original := apply(t, s, "a", create())
	completed := true
	done := apply(t, s, "a", Operation{ID: uuid.NewString(), ItemID: original.ID, Type: "set_completed", ExpectedRevision: 1, Completed: &completed})
	op := Operation{ID: uuid.NewString(), ItemID: done.ID, Type: "set_kind", ExpectedRevision: done.Revision, Kind: "text"}
	text := apply(t, s, "a", op)
	if text.Kind != "text" || text.Completed || text.Body != original.Body || text.CreatedAt != original.CreatedAt || text.CreatedBy != original.CreatedBy {
		t.Fatal("conversion lost content or completion state", text)
	}
	if apply(t, s, "a", op) != text {
		t.Fatal("retry changed the conversion")
	}
	op.ID = uuid.NewString()
	_, err := s.Apply(ctx, "b", op)
	code(t, err, "conflict")
	op.ExpectedRevision = text.Revision
	op.Kind = "todo"
	todo := apply(t, s, "b", op)
	if todo.Kind != "todo" || todo.Completed || todo.Body != original.Body {
		t.Fatal("conversion back to todo failed", todo)
	}
	generation, _, _ := s.Watermark(ctx)
	page, err := s.Changes(ctx, generation, 2, 100)
	if err != nil || len(page.Changes) != 2 || page.Changes[0].Item != text || page.Changes[1].Item != todo {
		t.Fatal("conversion not synchronized", page, err)
	}
}

func TestKindChangeRejectsImagesAndUnrelatedFields(t *testing.T) {
	s := newStore(t)
	item := apply(t, s, "a", create())
	completed := true
	for _, fields := range []Operation{
		{Kind: ""}, {Kind: "image"}, {Kind: "unknown"},
		{Kind: "text", Body: "overwrite"}, {Kind: "text", Completed: &completed},
		{Kind: "text", AttachmentID: uuid.NewString()},
	} {
		fields.ID, fields.ItemID, fields.Type, fields.ExpectedRevision = uuid.NewString(), item.ID, "set_kind", item.Revision
		_, err := s.Apply(ctx, "a", fields)
		code(t, err, "invalid_fields")
	}
	attachment := uuid.NewString()
	if err := s.AddAttachment(ctx, "a", Attachment{ID: attachment, MIME: "image/png", Size: 10, Width: 1, Height: 1}); err != nil {
		t.Fatal(err)
	}
	image := create()
	image.Kind, image.AttachmentID = "image", attachment
	stored := apply(t, s, "a", image)
	_, err := s.Apply(ctx, "a", Operation{ID: uuid.NewString(), ItemID: stored.ID, Type: "set_kind", ExpectedRevision: stored.Revision, Kind: "todo"})
	code(t, err, "invalid_fields")
}

func TestAttachmentCleanupKeepsLiveReferences(t *testing.T) {
	s := newStore(t)
	live, orphan := uuid.NewString(), uuid.NewString()
	for _, id := range []string{live, orphan} {
		if err := s.AddAttachment(ctx, "a", Attachment{ID: id, MIME: "image/png", Size: 10, Width: 1, Height: 1}); err != nil {
			t.Fatal(err)
		}
	}
	op := create()
	op.Kind = "image"
	op.AttachmentID = live
	apply(t, s, "a", op)
	if _, err := s.db.Exec(`UPDATE attachments SET created_at=0`); err != nil {
		t.Fatal(err)
	}
	removed, err := s.ExpiredAttachments(ctx)
	if err != nil || len(removed) != 1 || removed[0] != orphan {
		t.Fatal(removed, err)
	}
	if !s.HasAttachment(ctx, live) || s.HasAttachment(ctx, orphan) {
		t.Fatal("wrong attachment removed")
	}
	if _, err = s.Attachment(ctx, "b", live, false); err != nil {
		t.Fatal("shared attachment inaccessible", err)
	}
	apply(t, s, "a", Operation{ID: uuid.NewString(), ItemID: op.ItemID, Type: "delete", ExpectedRevision: 1})
	removed, err = s.ExpiredAttachments(ctx)
	if err != nil || len(removed) != 0 {
		t.Fatal(removed, err)
	}
	if _, err := s.Attachment(ctx, "b", live, false); err == nil {
		t.Fatal("deleted attachment available to other devices")
	}
	if _, err := s.Attachment(ctx, "@console", live, true); err != nil {
		t.Fatal("deleted attachment unavailable to console", err)
	}
}

func TestSoftDeleteRestorePreservesItemAndPreventsStaleWrites(t *testing.T) {
	s := newStore(t)
	original := apply(t, s, "a", create())
	completed := true
	original = apply(t, s, "a", Operation{ID: uuid.NewString(), ItemID: original.ID, Type: "set_completed", ExpectedRevision: original.Revision, Completed: &completed})
	deleted := apply(t, s, "a", Operation{ID: uuid.NewString(), ItemID: original.ID, Type: "delete", ExpectedRevision: original.Revision})
	active, err := s.ListItems(ctx, "", "", "", false)
	if err != nil || len(active) != 0 {
		t.Fatal(active, err)
	}
	trash, err := s.ListItems(ctx, "", "todo", "sample", true)
	if err != nil || len(trash) != 1 || trash[0] != deleted {
		t.Fatal(trash, err)
	}
	op := Operation{ID: uuid.NewString(), ItemID: deleted.ID, Type: "restore", ExpectedRevision: deleted.Revision}
	bad := op
	bad.Body = "overwrite"
	_, err = s.Apply(ctx, "@console", bad)
	code(t, err, "invalid_fields")
	restored := apply(t, s, "@console", op)
	if restored.DeletedAt != "" || restored.Revision != deleted.Revision+1 || restored.Body != original.Body || !restored.Completed || restored.Kind != original.Kind || restored.CreatedBy != original.CreatedBy || restored.CreatedAt != original.CreatedAt {
		t.Fatal("restore lost item data", restored)
	}
	if got := apply(t, s, "@console", op); got != restored {
		t.Fatal("restore retry is not idempotent", got)
	}
	trash, err = s.ListItems(ctx, "", "", "", true)
	if err != nil || len(trash) != 0 {
		t.Fatal(trash, err)
	}
	_, err = s.Apply(ctx, "a", Operation{ID: uuid.NewString(), ItemID: restored.ID, Type: "update", ExpectedRevision: deleted.Revision, Body: "stale"})
	code(t, err, "conflict")
	deletedAgain := apply(t, s, "a", Operation{ID: uuid.NewString(), ItemID: restored.ID, Type: "delete", ExpectedRevision: restored.Revision})
	apply(t, s, "@console", op) // An old retry must not undo a later deletion.
	page, err := s.Snapshot(ctx, "", nil, "", 100)
	if err != nil || len(page.Items) != 1 || page.Items[0] != deletedAgain {
		t.Fatal(page, err)
	}
	changes, err := s.Changes(ctx, page.Generation, deleted.Revision, 100)
	if err != nil || len(changes.Changes) != 2 || changes.Changes[0].Item != restored {
		t.Fatal("restoration missing from sync", changes, err)
	}
}
