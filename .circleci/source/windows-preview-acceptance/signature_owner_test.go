package main

import "testing"

func TestSignatureOnlyOwnerRejectsIdentityAndLateMembers(t *testing.T) {
	for _, c := range []struct{ path, hash, owned bool }{{false, true, true}, {true, false, true}, {true, true, false}} {
		s := signatureOwnerPolicy{root: 10}
		if s.admit(20, c.path, c.hash, c.owned) == nil || s.host != 0 {
			t.Fatal("invalid host admitted")
		}
	}
	for _, frozen := range []bool{false, true} {
		s := signatureOwnerPolicy{root: 10, frozen: frozen, rootExited: !frozen}
		if s.admit(20, true, true, true) == nil {
			t.Fatal("late host admitted")
		}
	}
	s := signatureOwnerPolicy{root: 10}
	if s.admit(20, true, true, true) != nil {
		t.Fatal("exact host rejected")
	}
	if s.admit(30, true, true, true) == nil {
		t.Fatal("third member admitted")
	}
	if s.finish(2, 0, true, true, true, false) != nil {
		t.Fatal("natural exact retirement rejected")
	}
	for _, c := range []struct {
		total, active                     uint32
		empty, rootZero, hostZero, forced bool
	}{
		{3, 0, true, true, true, false}, {2, 1, true, true, true, false}, {2, 0, false, true, true, false},
		{2, 0, true, false, true, false}, {2, 0, true, true, false, false}, {2, 0, true, true, true, true},
	} {
		if s.finish(c.total, c.active, c.empty, c.rootZero, c.hostZero, c.forced) == nil {
			t.Fatal("uncertain retirement accepted")
		}
	}
}

func TestSignatureOnlyOwnerRequiresExactSuspendedSet(t *testing.T) {
	for _, ids := range [][]uint32{nil, {20, 20}, {10, 10}, {10, 30}, {10, 20, 30}, {0, 20}} {
		if signatureExactInitialSet(ids, 10, 20) {
			t.Fatal("inexact suspended set accepted")
		}
	}
	for _, ids := range [][]uint32{{10, 20}, {20, 10}} {
		if !signatureExactInitialSet(ids, 10, 20) {
			t.Fatal("exact suspended set rejected")
		}
	}
}
