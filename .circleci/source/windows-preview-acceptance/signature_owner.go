package main

// This policy is used only by the immutable OS-signature query. A console host
// is not an allowed member of the generic viewer/source process owner.
type signatureOwnerPolicy struct {
	root, host         uint32
	frozen, rootExited bool
}

func (s *signatureOwnerPolicy) admit(id uint32, samePath, sameHash, owned bool) error {
	if id == 0 || !owned {
		return failure("signature_member_not_owned")
	}
	if id == s.root || id == s.host {
		return nil
	}
	if s.frozen || s.rootExited || s.host != 0 {
		return failure("signature_unknown_or_late_member")
	}
	if !samePath || !sameHash {
		return failure("signature_host_identity_failed")
	}
	s.host = id
	return nil
}
func (s *signatureOwnerPolicy) finish(total, active uint32, empty, rootZero, hostZero, forced bool) error {
	if forced || s.host == 0 || total != 2 || active != 0 || !empty || !rootZero || !hostZero {
		return failure("signature_owner_retirement_failed")
	}
	return nil
}

func signatureUniqueInventory(ids []uint32) error {
	seen := map[uint32]bool{}
	for _, id := range ids {
		if id == 0 || seen[id] {
			return failure("signature_duplicate_member")
		}
		seen[id] = true
	}
	return nil
}
func signatureExactInitialSet(ids []uint32, root, host uint32) bool {
	return root != 0 && host != 0 && root != host && len(ids) == 2 && signatureUniqueInventory(ids) == nil && ((ids[0] == root && ids[1] == host) || (ids[1] == root && ids[0] == host))
}

const signatureStartupMarker = "signature_owner_ready_v1"

func (s *signatureOwnerPolicy) freezeStartup(marker []byte, total, active uint32) error {
	if string(marker) != signatureStartupMarker+"\r\n" && string(marker) != signatureStartupMarker+"\n" {
		return failure("signature_startup_marker_invalid")
	}
	if s.frozen || s.rootExited || s.host == 0 || total != 2 || active != 2 {
		return failure("signature_startup_graph_incomplete")
	}
	s.frozen = true
	return nil
}
