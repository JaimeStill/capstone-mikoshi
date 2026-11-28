package data_test

import (
	"context"
	"slices"
	"testing"

	"github.com/JaimeStill/capstone-mikoshi/data"
)

func TestSeeder_DeclaresEmptyAlone(t *testing.T) {
	if states := (data.Seeder{}).States(); !slices.Equal(states, []string{"empty"}) {
		t.Errorf("States = %v; want [empty]", states)
	}
}

func TestSeeder_SeedWritesNothing(t *testing.T) {
	var s data.Seeder
	if err := s.Verify(context.Background()); err != nil {
		t.Fatalf("Verify = %v", err)
	}
	n, err := s.Seed(context.Background(), data.StateEmpty)
	if err != nil || n == nil || len(n) != 0 {
		t.Errorf("Seed = %v, %v; want no rows counted", n, err)
	}
}
