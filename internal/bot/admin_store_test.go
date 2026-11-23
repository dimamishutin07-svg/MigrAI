package bot

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestAdminStoreLifecycle(t *testing.T) {
	dsn := os.Getenv("MIGRAI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated test database")
	}
	ctx := context.Background()
	s, e := OpenStore(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	id := int64(7123456789012)
	defer s.DeleteUser(ctx, id)
	if _, e = s.Get(ctx, id, "tester"); e != nil {
		t.Fatal(e)
	}
	m := &Message{}
	m.From.ID = id
	m.From.FirstName = "Тест"
	m.Chat.ID = id
	if e = s.RecordIncoming(ctx, m, "Здравствуйте"); e != nil {
		t.Fatal(e)
	}
	if e = s.SaveSurvey(ctx, id, map[string]string{"countries": "Испания"}, "goal"); e != nil {
		t.Fatal(e)
	}
	if e = s.RecordSent(ctx, id, "Ответ"); e != nil {
		t.Fatal(e)
	}
	if e = s.RecordFeedback(ctx, id, "rating", "Полезно 👍", false); e != nil {
		t.Fatal(e)
	}
	items, n, e := s.AdminUsers(ctx, "id:7123456789012", "survey", 0, 50)
	if e != nil || n != 1 || len(items) != 1 {
		t.Fatalf("list %d %v", n, e)
	}
	var item map[string]any
	json.Unmarshal(items[0], &item)
	if item["id"] != "7123456789012" || item["feedback"] != "Полезно 👍" {
		t.Fatal(item)
	}
	events, e := s.AdminEvents(ctx, "7123456789012", 0)
	if e != nil || len(events) != 3 {
		t.Fatalf("events %d %v", len(events), e)
	}
	s.db.Exec(ctx, `UPDATE conversation_events SET created_at=now()-interval '91 days' WHERE telegram_id=$1`, id)
	if e = s.PurgeContext(ctx); e != nil {
		t.Fatal(e)
	}
	events, e = s.AdminEvents(ctx, "7123456789012", 0)
	if e != nil || len(events) != 0 {
		t.Fatal("retention")
	}
	items, n, e = s.AdminUsers(ctx, "id:7123456789012", "", 0, 50)
	if e != nil || n != 1 {
		t.Fatal("lost profile")
	}
	if e = s.DeleteUser(ctx, id); e != nil {
		t.Fatal(e)
	}
	items, n, e = s.AdminUsers(ctx, "id:7123456789012", "", 0, 50)
	if e != nil || n != 0 {
		t.Fatal("deletion")
	}
}

func TestHistoryImportIsIdempotent(t *testing.T) {
	dsn := os.Getenv("MIGRAI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated test database")
	}
	ctx := context.Background()
	s, err := OpenStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id := int64(7123456789013)
	defer s.DeleteUser(ctx, id)
	if _, err = s.Get(ctx, id, "history_test"); err != nil {
		t.Fatal(err)
	}
	if err = s.AddMessage(ctx, id, "user", "Legacy message"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(ctx, `DELETE FROM app_migrations WHERE name='admin_history_v1'`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		reopened, e := OpenStore(ctx, dsn)
		if e != nil {
			t.Fatal(e)
		}
		reopened.Close()
	}
	events, err := s.AdminEvents(ctx, "7123456789013", 0)
	if err != nil || len(events) != 1 {
		t.Fatalf("duplicated import: %d %v", len(events), err)
	}
	if _, err = s.db.Exec(ctx, `UPDATE messages SET created_at=now()-interval '91 days' WHERE telegram_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err = s.PurgeContext(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	s.db.QueryRow(ctx, `SELECT count(*) FROM messages WHERE telegram_id=$1`, id).Scan(&count)
	if count != 0 {
		t.Fatal("old raw context retained")
	}
}
