package profiles

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "profiles.toml")
	store, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load (missing file): %v", err)
	}
	if len(store.Profiles) != 0 {
		t.Fatalf("expected 0 profiles, got %d", len(store.Profiles))
	}

	now := time.Now().Truncate(time.Second)
	for _, p := range []Profile{
		{Name: "beam-lab", Endpoint: "192.168.1.11:50051", LastUsed: now.Add(-time.Hour)},
		{Name: "host00", Endpoint: "127.0.0.1:50051", LastUsed: now},
		{Name: "prod", Endpoint: "10.0.0.5:50051"}, // zero LastUsed
	} {
		if err := store.Add(p); err != nil {
			t.Fatalf("Add %s: %v", p.Name, err)
		}
	}

	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reload, err := Load(tmp)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := len(reload.Profiles); got != 3 {
		t.Fatalf("reload: got %d profiles, want 3", got)
	}
	if reload.Profiles[1].Endpoint != "127.0.0.1:50051" {
		t.Errorf("endpoint round-trip: %q", reload.Profiles[1].Endpoint)
	}
	if !reload.Profiles[1].LastUsed.Equal(now) {
		t.Errorf("LastUsed round-trip: got %v want %v",
			reload.Profiles[1].LastUsed, now)
	}
}

func TestDuplicateAddRejected(t *testing.T) {
	store := &Store{path: filepath.Join(t.TempDir(), "p.toml")}
	if err := store.Add(Profile{Name: "x", Endpoint: "h:1"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(Profile{Name: "x", Endpoint: "other:2"}); err != ErrDuplicate {
		t.Errorf("expected ErrDuplicate, got %v", err)
	}
}

func TestDeleteAndRename(t *testing.T) {
	store := &Store{path: filepath.Join(t.TempDir(), "p.toml")}
	_ = store.Add(Profile{Name: "old", Endpoint: "h:1"})
	if err := store.Rename("old", "new"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Find("new"); err != nil {
		t.Errorf("renamed profile missing: %v", err)
	}
	if err := store.Delete("new"); err != nil {
		t.Errorf("Delete: %v", err)
	}
	if err := store.Delete("new"); err != ErrNotFound {
		t.Errorf("Delete missing: %v", err)
	}
}

func TestSortByRecency(t *testing.T) {
	now := time.Now()
	store := &Store{
		Profiles: []Profile{
			{Name: "older", LastUsed: now.Add(-2 * time.Hour)},
			{Name: "never"},
			{Name: "newest", LastUsed: now},
			{Name: "also-never"},
			{Name: "middle", LastUsed: now.Add(-time.Hour)},
		},
	}
	store.SortByRecency()
	want := []string{"newest", "middle", "older", "also-never", "never"}
	for i, w := range want {
		if store.Profiles[i].Name != w {
			t.Errorf("position %d: got %s, want %s", i, store.Profiles[i].Name, w)
		}
	}
}

func TestValidateName(t *testing.T) {
	for _, tc := range []struct {
		in  string
		err bool
	}{
		{"foo", false},
		{"beam-lab_01", false},
		{"", true},
		{" leading", true},
		{"trailing ", true},
		{"with\nnewline", true},
	} {
		err := ValidateName(tc.in)
		if (err != nil) != tc.err {
			t.Errorf("ValidateName(%q) = %v, wantErr=%v", tc.in, err, tc.err)
		}
	}
}
