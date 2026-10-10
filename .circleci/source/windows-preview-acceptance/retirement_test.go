package main

import (
	"errors"
	"testing"
	"time"
)

func TestRetirementRequiresObservedEmptyJob(t *testing.T) {
	queries := 0
	err := waitRetiredInventory(func() ([]uint32, error) {
		queries++
		if queries < 3 {
			return []uint32{17}, nil
		}
		return nil, nil
	}, map[uint32]bool{17: true}, time.Second)
	if err != nil || queries != 3 {
		t.Fatal("retirement did not wait for exact empty inventory")
	}
}

func TestRetirementRejectsUnprovedMembersImmediately(t *testing.T) {
	for _, ids := range [][]uint32{{18}, {17, 18}, {0}, {17, 17}} {
		queries := 0
		err := waitRetiredInventory(func() ([]uint32, error) { queries++; return ids, nil }, map[uint32]bool{17: true}, time.Second)
		if err == nil || queries != 1 {
			t.Fatal("unproved member was tolerated")
		}
	}
}

func TestRetirementPreservesQueryFailure(t *testing.T) {
	want := errors.New("synthetic query failure")
	queries := 0
	err := waitRetiredInventory(func() ([]uint32, error) { queries++; return nil, want }, nil, time.Second)
	if !errors.Is(err, want) || queries != 1 {
		t.Fatal("query failure was retried or hidden")
	}
}

func TestRetirementDeadlineCannotPassOccupiedJob(t *testing.T) {
	queries := 0
	err := waitRetiredInventory(func() ([]uint32, error) { queries++; return []uint32{17}, nil }, map[uint32]bool{17: true}, 0)
	if err == nil || queries != 1 {
		t.Fatal("deadline accepted an occupied job")
	}
	if waitRetiredInventory(func() ([]uint32, error) { return nil, nil }, nil, 0) != nil {
		t.Fatal("already empty job rejected")
	}
}
