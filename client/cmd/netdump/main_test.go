package main

import (
	"reflect"
	"testing"

	"fancyband/internal/proto"
)

func TestParseKeys(t *testing.T) {
	got, err := parseKeys("aé<esc><enter><^X><kp8><quit><")
	if err != nil {
		t.Fatal(err)
	}
	want := []proto.Message{
		proto.Key{Code: 'a'},
		proto.Key{Code: 'é'},
		proto.Key{Code: proto.KeyEscape},
		proto.Key{Code: proto.KeyEnter},
		proto.Key{Code: 0x18},
		proto.Key{Code: '8', Mods: proto.ModKeypad},
		proto.Quit{},
		proto.Key{Code: '<'},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
	if _, err := parseKeys("<nope>"); err == nil {
		t.Error("an unknown token parsed")
	}
}
