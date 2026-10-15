package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	orbitv1 "orbit/gen/go/orbit/v1"
)

// HTTPRunner uses the same device token as the inbox gateway. It never registers
// a second MQTT product; Core remains the authority for identity and projections.
type HTTPRunner struct {
	origin, token string
	store         *Store
	client        *http.Client
	logger        *zap.Logger
}

func NewHTTPRunner(origin, token string, store *Store, logger *zap.Logger) *HTTPRunner {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &HTTPRunner{origin: strings.TrimRight(origin, "/"), token: token, store: store, logger: logger,
		client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (r *HTTPRunner) request(ctx context.Context, method, path string, body []byte, result any) error {
	req, err := http.NewRequestWithContext(ctx, method, r.origin+path, bytes.NewReader(body))
	if err != nil {
		return errors.New("invalid Core request")
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.client.Do(req)
	if err != nil {
		// Do not include upstream URLs or credentials in logs/errors.
		return errors.New("Core connection unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Core request rejected (HTTP %d)", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxViewPayload+1))
	if err != nil || len(data) > maxViewPayload {
		return errors.New("invalid Core response size")
	}
	return json.Unmarshal(data, result)
}

func (r *HTTPRunner) refresh(ctx context.Context) error {
	var status struct {
		NodeID string          `json:"node_id"`
		View   json.RawMessage `json:"view"`
	}
	if err := r.request(ctx, http.MethodGet, "/api/v1/status", nil, &status); err != nil {
		return err
	}
	if status.NodeID == "" {
		return errors.New("Core response has no device identity")
	}
	now := time.Now().UTC()
	view := &orbitv1.DeviceView{}
	if len(status.View) == 0 || string(status.View) == "null" {
		// Removing a route must clear the previously displayed sources/sessions.
		view = &orbitv1.DeviceView{NodeId: status.NodeID, CoreEpoch: "unconfigured", SessionsEnabled: proto.Bool(false),
			Metadata: &orbitv1.Metadata{Revision: uint64(now.UnixNano()), ProducedAt: timestamppb.New(now)}}
	} else if err := protojson.Unmarshal(status.View, view); err != nil {
		return errors.New("invalid Core device view")
	}
	if view.NodeId != status.NodeID || view.Metadata == nil || view.Metadata.Revision == 0 || view.CoreEpoch == "" {
		return errors.New("Core device view identity or metadata is invalid")
	}
	return r.store.Update(view, now)
}

func (r *HTTPRunner) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	failed := false
	for {
		err := r.refresh(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil && !failed {
			r.logger.Warn("web status unavailable", zap.Error(err))
		} else if err == nil && failed {
			r.logger.Info("web status connection restored")
		}
		failed = err != nil
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *HTTPRunner) OpenCodexSession(ctx context.Context, sessionID string, viewRevision uint64) (string, error) {
	snapshot, err := r.store.Snapshot()
	if err != nil || snapshot == nil || viewRevision == 0 || viewRevision > snapshot.Revision {
		return "", errors.New("current device view is unavailable")
	}
	body, _ := json.Marshal(struct {
		CoreEpoch    string `json:"core_epoch"`
		ViewRevision uint64 `json:"view_revision,string"`
	}{snapshot.CoreEpoch, viewRevision})
	var result struct {
		IntentID string `json:"intent_id"`
	}
	if err := r.request(ctx, http.MethodPost, "/api/v1/sessions/"+url.PathEscape(sessionID)+"/open", body, &result); err != nil {
		return "", err
	}
	if result.IntentID == "" {
		return "", errors.New("Core returned no session action receipt")
	}
	return result.IntentID, nil
}
