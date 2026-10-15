// Run from the repository root: go run ./agents/desktop/testdata
// These fixtures use the existing Go protobuf implementation. The desktop app
// does not build or execute this generator at runtime.
package main

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	v1 "orbit/gen/go/orbit/v1"
	"os"
	"path/filepath"
	"time"
)

func main() {
	now := time.Unix(1792000000, 123000000)
	metadata := &v1.Metadata{MessageId: "fixture-message", ProducerId: "core", Revision: 7, ProducedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(20 * time.Second))}
	command := &v1.Command{Metadata: metadata, CommandId: "fixture-command", TargetAgentId: "fixture-agent", IntentRef: &v1.IntentRef{IntentId: "fixture-intent", RequesterKind: v1.RequesterKind_REQUESTER_KIND_NODE, RequesterId: "fixture-node"}, IntentProducedAt: timestamppb.New(now.Add(-time.Second)), Action: &v1.Command_OpenCodexSession{OpenCodexSession: &v1.OpenCodexSession{SessionId: "01a06712-3498-74b3-b787-532466bf8eb8"}}}
	zero := uint64(0)
	cost := int64(1234567)
	observation := &v1.Observation{Metadata: metadata, AgentEpoch: "fixture-epoch", Payload: &v1.Observation_Usage{Usage: &v1.UsageObservation{ActualCostMicros: &cost, TokenCount: &zero, Tpm: &zero, CurrencyCode: "USD", ObservedAt: timestamppb.New(now), WindowStart: timestamppb.New(now.Truncate(24 * time.Hour)), WindowEnd: timestamppb.New(now.Truncate(24 * time.Hour).Add(24 * time.Hour))}}}
	for name, message := range map[string]proto.Message{"go-command.bin": command, "go-observation.bin": observation} {
		data, err := proto.MarshalOptions{Deterministic: true}.Marshal(message)
		if err != nil {
			panic(err)
		}
		if err = os.WriteFile(filepath.Join("agents/desktop/testdata", name), data, 0644); err != nil {
			panic(err)
		}
	}
}
