package mqttadmin

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

// Opt-in: point these variables only at a disposable EMQX 5.8 broker.
// This test creates credentials and a built-in authorization source.
func TestEMQXLifecycle(t *testing.T) {
	base := os.Getenv("ORBIT_TEST_EMQX_URL")
	if base == "" {
		t.Skip("requires a disposable EMQX broker")
	}
	addr := os.Getenv("ORBIT_TEST_EMQX_MQTT_ADDR")
	c := New(base, os.Getenv("ORBIT_TEST_EMQX_KEY"), os.Getenv("ORBIT_TEST_EMQX_SECRET"), "core")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	prefix := fmt.Sprintf("qa-%d", time.Now().UnixNano())
	legacy := prefix + "-legacy"
	if err := c.request(ctx, "POST", usersPath, map[string]any{"user_id": legacy, "password": "test-only-legacy-password", "is_superuser": false}, nil); err != nil {
		t.Fatal(PublicError(err))
	}
	t.Cleanup(func() { _, _ = c.Revoke(context.Background(), legacy) })
	credentials := map[string]Result{}
	for _, kind := range []string{"node", "agent"} {
		name := prefix + "-" + kind
		result, err := c.Create(ctx, CreateInput{name, kind, name})
		if err != nil {
			t.Fatal(kind, PublicError(err))
		}
		credentials[kind] = result
		t.Cleanup(func() { _, _ = c.Revoke(context.Background(), name) })
	}
	// Enabling managed ACLs must not change existing users' file-based permissions.
	old := mqttLogin(t, addr, legacy, "test-only-legacy-password", "legacy", true)
	defer old.Close()
	mqttSubscribe(t, old, "legacy/unrelated/topic", true)
	node := credentials["node"]
	n := mqttLogin(t, addr, node.Username, node.Password, "node-one", true)
	defer n.Close()
	n2 := mqttLogin(t, addr, node.Username, node.Password, "node-two", true)
	defer n2.Close()
	root := "orbit/v1/nodes/" + node.Username
	mqttSubscribe(t, n, root+"/view", true)
	mqttSubscribe(t, n, root+"/navigation", true)
	mqttSubscribe(t, n, "orbit/v1/nodes/someone-else/view", false)
	mqttSubscribe(t, n, "orbit/#", false)
	mqttPublish(t, n, root+"/state", true)
	mqttPublish(t, n, root+"/view", false)
	mqttPublish(t, n, "orbit/v1/agents/other/commands", false)
	mqttPublish(t, n, "$SYS/test", false)
	agent := credentials["agent"]
	a := mqttLogin(t, addr, agent.Username, agent.Password, "agent", true)
	defer a.Close()
	for _, topic := range []string{"state", "observations/usage", "observations/codex", "shortcuts", "results"} {
		mqttPublish(t, a, "orbit/v1/agents/"+agent.Username+"/"+topic, true)
	}
	mqttSubscribe(t, a, "orbit/v1/agents/"+agent.Username+"/commands", true)
	mqttPublish(t, a, root+"/view", false)
	// A chosen Client ID can bypass username ACLs in EMQX. Refuse provisioning
	// while such entries exist, instead of advertising ineffective restrictions.
	clientRulesPath := "/authorization/sources/built_in_database/rules/clients"
	if err := c.request(ctx, "POST", clientRulesPath, []map[string]any{{"clientid": "qa-client-rule", "rules": []rule{{"allow", "all", "#"}}}}, nil); err != nil {
		t.Fatal(PublicError(err))
	}
	t.Cleanup(func() { _ = c.request(context.Background(), "DELETE", clientRulesPath+"/qa-client-rule", nil, nil) })
	bypass := mqttLogin(t, addr, node.Username, node.Password, "qa-client-rule", true)
	mqttPublish(t, bypass, "unrelated/topic", true)
	bypass.Close()
	if _, err := c.Create(ctx, CreateInput{prefix + "-blocked", "node", prefix + "-blocked"}); err == nil || PublicError(err).Status != 422 {
		t.Fatal("Client-ID rules must block scoped provisioning")
	}
	if err := c.request(ctx, "DELETE", clientRulesPath+"/qa-client-rule", nil, nil); err != nil {
		t.Fatal(PublicError(err))
	}
	rotated, err := c.Rotate(ctx, node.Username)
	if err != nil || rotated.Warning != "" {
		t.Fatalf("rotate did not complete: %v", err)
	}
	mqttDisconnected(t, n)
	mqttDisconnected(t, n2)
	rejected := mqttLogin(t, addr, node.Username, node.Password, "old-password", false)
	rejected.Close()
	current := mqttLogin(t, addr, node.Username, rotated.Password, "new-password", true)
	defer current.Close()
	result, err := c.Revoke(ctx, node.Username)
	if err != nil || result.Warning != "" {
		t.Fatalf("revoke did not complete: %v", err)
	}
	mqttDisconnected(t, current)
	rejected = mqttLogin(t, addr, node.Username, rotated.Password, "revoked", false)
	rejected.Close()
	result, err = c.Revoke(ctx, node.Username)
	if err != nil || result.Warning != "" {
		t.Fatal("revoke retry must be idempotent")
	}
}

// Minimal MQTT 5 wire helpers let the test assert broker reason codes directly.
func mqttString(s string) []byte {
	return append([]byte{byte(len(s) >> 8), byte(len(s))}, []byte(s)...)
}
func mqttWrite(t *testing.T, c net.Conn, header byte, body []byte) {
	t.Helper()
	packet := []byte{header}
	for n := len(body); ; {
		b := byte(n % 128)
		n /= 128
		if n > 0 {
			b |= 128
		}
		packet = append(packet, b)
		if n == 0 {
			break
		}
	}
	packet = append(packet, body...)
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write(packet); err != nil {
		t.Fatal(err)
	}
}
func mqttRead(c net.Conn) (byte, []byte, error) {
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	head := make([]byte, 1)
	if _, err := io.ReadFull(c, head); err != nil {
		return 0, nil, err
	}
	length, mult := 0, 1
	for i := 0; i < 4; i++ {
		b := make([]byte, 1)
		if _, err := io.ReadFull(c, b); err != nil {
			return 0, nil, err
		}
		length += int(b[0]&127) * mult
		if b[0]&128 == 0 {
			break
		}
		mult *= 128
	}
	if length > 65536 {
		return 0, nil, fmt.Errorf("packet too large")
	}
	body := make([]byte, length)
	_, err := io.ReadFull(c, body)
	return head[0], body, err
}
func mqttLogin(t *testing.T, addr, username, secret, id string, allowed bool) net.Conn {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	body := append(mqttString("MQTT"), 5, 0xc2, 0, 60, 0)
	for _, v := range []string{id, username, secret} {
		body = append(body, mqttString(v)...)
	}
	mqttWrite(t, c, 0x10, body)
	header, reply, err := mqttRead(c)
	if err != nil || header != 0x20 || len(reply) < 3 || (reply[1] == 0) != allowed {
		c.Close()
		t.Fatalf("MQTT login allowed=%v got packet=%x err=%v", allowed, header, err)
	}
	return c
}
func mqttSubscribe(t *testing.T, c net.Conn, topic string, allowed bool) {
	t.Helper()
	body := append([]byte{0, 1, 0}, mqttString(topic)...)
	body = append(body, 1)
	mqttWrite(t, c, 0x82, body)
	h, b, err := mqttRead(c)
	if err != nil || h != 0x90 || len(b) < 4 || (b[len(b)-1] < 128) != allowed {
		t.Fatalf("subscribe %s allowed=%v packet=%x err=%v", topic, allowed, b, err)
	}
}
func mqttPublish(t *testing.T, c net.Conn, topic string, allowed bool) {
	t.Helper()
	body := append(mqttString(topic), 0, 2, 0, 'x')
	mqttWrite(t, c, 0x32, body)
	h, b, err := mqttRead(c)
	code := byte(0)
	if len(b) > 2 {
		code = b[2]
	}
	if err != nil || h != 0x40 || len(b) < 2 || binary.BigEndian.Uint16(b[:2]) != 2 || (code < 128) != allowed {
		t.Fatalf("publish %s allowed=%v packet=%x err=%v", topic, allowed, b, err)
	}
}
func mqttDisconnected(t *testing.T, c net.Conn) {
	t.Helper()
	h, _, err := mqttRead(c)
	if err == nil && h != 0xe0 {
		t.Fatal("old MQTT connection remained active")
	}
	if e, ok := err.(net.Error); ok && e.Timeout() {
		t.Fatal("old MQTT connection was not disconnected")
	}
}
