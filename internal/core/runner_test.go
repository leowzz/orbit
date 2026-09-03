package core

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"
	"orbit/internal/mqtt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/protobuf/proto"
)

type fakeTransport struct {
	messages []mqtt.Message
}

func (transport *fakeTransport) Publish(_ context.Context, message mqtt.Message) error {
	transport.messages = append(transport.messages, message)
	return nil
}

func (*fakeTransport) Subscribe(context.Context, string, mqtt.Handler) error { return nil }

func TestRunnerRoutesObservationToRetainedView(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	engine := newTestEngine(t)
	transport := &fakeTransport{}
	var logs bytes.Buffer
	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(&logs),
		zapcore.DebugLevel,
	))
	runner, err := NewRunner(engine, transport, logger)
	if err != nil {
		t.Fatal(err)
	}
	runner.now = func() time.Time { return now }

	agentPayload, _ := proto.Marshal(testAgentState(now))
	if err := runner.handleAgentState(context.Background(), mqtt.Message{Topic: "orbit/v1/agents/agent-a/state", Payload: agentPayload}); err != nil {
		t.Fatal(err)
	}
	nodePayload, _ := proto.Marshal(testNodeState(now))
	if err := runner.handleNodeState(context.Background(), mqtt.Message{Topic: "orbit/v1/nodes/node-a/state", Payload: nodePayload}); err != nil {
		t.Fatal(err)
	}
	observationPayload, _ := proto.Marshal(testObservation(now))
	if err := runner.handleObservation(context.Background(), mqtt.Message{Topic: "orbit/v1/agents/agent-a/observations/usage", Payload: observationPayload}); err != nil {
		t.Fatal(err)
	}
	if len(transport.messages) != 1 || !transport.messages[0].Retain || transport.messages[0].Topic != "orbit/v1/nodes/node-a/view" {
		t.Fatalf("unexpected publishes: %+v", transport.messages)
	}
	var view orbitv1.DeviceView
	if err := proto.Unmarshal(transport.messages[0].Payload, &view); err != nil {
		t.Fatal(err)
	}
	if view.NodeId != "node-a" || view.Primary.Text != "$12.35" {
		t.Fatalf("unexpected view: %+v", &view)
	}
	for _, want := range []string{"agent state accepted", "node state accepted", "usage observation accepted", `"views":1`, "device view published", `"primary":"$12.35"`} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("debug log missing %q: %s", want, logs.String())
		}
	}
}

func TestRunnerRejectsTopicPayloadIdentityMismatch(t *testing.T) {
	t.Parallel()
	engine := newTestEngine(t)
	runner, err := NewRunner(engine, &fakeTransport{}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := proto.Marshal(testAgentState(time.Now()))
	if err := runner.handleAgentState(context.Background(), mqtt.Message{Topic: "orbit/v1/agents/other/state", Payload: payload}); err == nil {
		t.Fatal("accepted topic/payload identity mismatch")
	}
}

func TestUnmarshalInboundRejectsOversizedPayload(t *testing.T) {
	t.Parallel()
	if err := unmarshalInbound(make([]byte, maxInboundPayload+1), &orbitv1.Observation{}); err == nil {
		t.Fatal("accepted oversized payload")
	}
}
