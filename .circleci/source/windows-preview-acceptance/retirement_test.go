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

func TestRetirementRetriesIncompleteWithoutAcceptingEmpty(t *testing.T) {
	for _, first := range [][]uint32{nil, {17}} {
		queries := 0
		err := waitRetiredInventory(func() ([]uint32, error) {
			queries++
			if queries == 1 {
				return first, errIncompleteInventory
			}
			return nil, nil
		}, map[uint32]bool{17: true}, time.Second)
		if err != nil || queries != 2 {
			t.Fatal("incomplete inventory accepted or not retried")
		}
	}
}
func TestRetirementIncompleteDeadlineCannotPass(t *testing.T) {
	queries := 0
	err := waitRetiredInventory(func() ([]uint32, error) { queries++; return nil, errIncompleteInventory }, map[uint32]bool{17: true}, 0)
	if err == nil || queries != 1 {
		t.Fatal("incomplete empty inventory counted as cleanup")
	}
}
func TestRetirementIncompleteUnknownMembersFailImmediately(t *testing.T) {
	for _, ids := range [][]uint32{{18}, {0}, {17, 17}} {
		queries := 0
		err := waitRetiredInventory(func() ([]uint32, error) { queries++; return ids, errIncompleteInventory }, map[uint32]bool{17: true}, time.Second)
		if err == nil || queries != 1 {
			t.Fatal("incomplete query concealed an unproved member")
		}
	}
}
