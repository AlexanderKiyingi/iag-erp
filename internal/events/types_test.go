package events

import "testing"

func TestTopicForEvent(t *testing.T) {
	cases := []string{
		TypeEmployeeCreated,
		TypeLeaveApproved,
		TypeProductionOrderUpdated,
	}
	for _, et := range cases {
		if got := TopicForEvent(et); got != TopicOperations {
			t.Fatalf("TopicForEvent(%q) = %q, want %q", et, got, TopicOperations)
		}
	}
}
