package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", FileName)
	s, err := Load(path)
	if err != nil || s.LastCharacter != "" {
		t.Fatalf("Load of a missing file = %+v, %v; want the defaults", s, err)
	}

	s.LastCharacter = "Bob"
	s.SetWarned("Bob")
	s.SetWarned("Bob")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastCharacter != "Bob" || !got.Warned("Bob") || got.Warned("Ann") || len(got.BorgWarned) != 1 {
		t.Errorf("Load after Save = %+v", got)
	}
	if _, err := os.Stat(path + ".new"); err == nil {
		t.Error("Save left its temporary file behind")
	}
}

func TestLoadBadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	os.WriteFile(path, []byte("{not json"), 0o644)
	if _, err := Load(path); err == nil {
		t.Error("Load of a broken file gave no error")
	}
}
