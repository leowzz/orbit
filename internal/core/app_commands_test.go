package core

import (
	"context"
	"google.golang.org/protobuf/proto"
	orbitv1 "orbit/gen/go/orbit/v1"
	"testing"
	"time"
)

func TestAppSessionCommandRoutingAndBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 15, 4, 0, 0, 0, time.UTC)
	engine := newCodexCommandEngine(t, now)
	route := Route{NodeID: "phone", Profile: appProfile, Inputs: []RouteInput{{AgentID: "agent-a", ObservationType: orbitv1.ObservationType_OBSERVATION_TYPE_CODEX}}}
	// HTTP nodes deliberately have no MQTT NodeState.
	engine.config.Routes = append(engine.config.Routes, route)
	view := engine.AppView(now, "phone")
	if view == nil {
		t.Fatal("missing app view")
	}
	transport := &fakeTransport{}
	runner, _ := NewRunner(engine, transport, nil, func() time.Time { return now })
	id, err := runner.OpenAppCodexSession(context.Background(), "phone", testCodexSessionID, view.CoreEpoch, view.Metadata.Revision)
	if err != nil || id == "" {
		t.Fatal(id, err)
	}
	if len(transport.messages) != 1 || transport.messages[0].Topic != "orbit/v1/agents/agent-a/commands" || transport.messages[0].Retain {
		t.Fatal(transport.messages)
	}
	var command orbitv1.Command
	if err := proto.Unmarshal(transport.messages[0].Payload, &command); err != nil {
		t.Fatal(err)
	}
	if command.IntentRef.RequesterId != "phone" || command.GetOpenCodexSession().SessionId != testCodexSessionID {
		t.Fatal(&command)
	}
	for _, tc := range []struct {
		node, session, epoch string
		revision             uint64
	}{
		{"other", testCodexSessionID, view.CoreEpoch, view.Metadata.Revision},
		{"web-a", testCodexSessionID, view.CoreEpoch, 1},
		{"phone", "missing", view.CoreEpoch, view.Metadata.Revision},
		{"phone", testCodexSessionID, "previous-core", view.Metadata.Revision},
		{"phone", testCodexSessionID, view.CoreEpoch, view.Metadata.Revision + 1},
		{"phone", testCodexSessionID, view.CoreEpoch, 0},
	} {
		if _, err := runner.OpenAppCodexSession(context.Background(), tc.node, tc.session, tc.epoch, tc.revision); err == nil {
			t.Fatalf("accepted invalid request: %+v", tc)
		}
	}
	disabled := false
	engine.config.Routes[1].SessionsEnabled = &disabled
	if _, err := runner.OpenAppCodexSession(context.Background(), "phone", testCodexSessionID, view.CoreEpoch, view.Metadata.Revision); err == nil {
		t.Fatal("accepted hidden sessions")
	}
	engine.config.Routes[1].SessionsEnabled = nil
	now = now.Add(2 * time.Minute)
	if _, err := runner.OpenAppCodexSession(context.Background(), "phone", testCodexSessionID, view.CoreEpoch, view.Metadata.Revision); err == nil {
		t.Fatal("accepted stale sessions")
	}
	if len(transport.messages) != 1 {
		t.Fatal("published a rejected command")
	}
}
