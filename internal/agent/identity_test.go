package agent

import "testing"

func TestDeriveAgentIDIsStableAndDoesNotExposePlatformUUID(t *testing.T) {
	t.Parallel()
	platformUUID := "AABBCCDD-0011-2233-4455-66778899AABB"
	first := deriveAgentID(platformUUID)
	second := deriveAgentID("  aabbccdd-0011-2233-4455-66778899aabb ")
	if first != second {
		t.Fatalf("derived IDs differ: %q != %q", first, second)
	}
	if first == platformUUID || len(first) != 36 {
		t.Fatalf("derived ID has unsafe format: %q", first)
	}
}
