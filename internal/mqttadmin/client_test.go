package mqttadmin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, secret, ok := r.BasicAuth()
		if !ok || key != "private-key" || secret != "private-secret" {
			t.Error("missing API authentication")
			w.WriteHeader(401)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return New(server.URL, "private-key", "private-secret", "core")
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func authChain(w http.ResponseWriter) {
	writeJSON(w, []map[string]any{{"id": "password_based:built_in_database", "enable": true, "user_id_type": "username"}})
}

func TestProvisionNodeRetriesWithoutChangingScopeOrPassword(t *testing.T) {
	const username = "orbit-app-test"
	secret := strings.Repeat("x", 43)
	var policy userRules
	exists, superuser, wrongPolicy := false, false, false
	writes := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v5")
		switch {
		case path == "/authentication":
			authChain(w)
		case path == "/authorization/sources":
			writeJSON(w, map[string]any{"sources": []map[string]any{{"type": "built_in_database", "enable": true}}})
		case path == "/authorization/sources/built_in_database/rules/clients":
			writeJSON(w, map[string]any{"data": []any{}})
		case path == usersPath+"/"+username && r.Method == "GET":
			if exists {
				writeJSON(w, brokerUser{Username: username, Superuser: superuser})
			} else {
				w.WriteHeader(404)
			}
		case path == rulesPath+"/"+username && r.Method == "GET":
			if policy.Username == "" {
				w.WriteHeader(404)
			} else if wrongPolicy {
				writeJSON(w, userRules{username, rulesFor("node", "other")})
			} else {
				writeJSON(w, policy)
			}
		case path == rulesPath && r.Method == "POST":
			var policies []userRules
			_ = json.NewDecoder(r.Body).Decode(&policies)
			policy = policies[0]
			writes++
			w.WriteHeader(204)
		case path == rulesPath+"/"+username && r.Method == "PUT":
			_ = json.NewDecoder(r.Body).Decode(&policy)
			writes++
			w.WriteHeader(204)
		case (path == usersPath && r.Method == "POST") || (path == usersPath+"/"+username && r.Method == "PUT"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["password"] != secret || body["is_superuser"] != false || !reflect.DeepEqual(policy.Rules, rulesFor("audio-node", "phone")) {
				t.Error("credential or ACL changed")
			}
			exists = true
			writes++
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, path)
			w.WriteHeader(500)
		}
	})
	for i := 0; i < 2; i++ {
		if err := c.ProvisionNode(context.Background(), username, "phone", secret); err != nil {
			t.Fatal(err)
		}
	}
	if writes != 3 {
		t.Fatal("expected policy then credential and idempotent password update")
	}
	policy.Rules = rulesFor("node", "phone")
	if err := c.ProvisionNode(context.Background(), username, "phone", secret); err != nil {
		t.Fatalf("legacy Android ACL upgrade failed: %v", err)
	}
	if writes != 5 || !reflect.DeepEqual(policy.Rules, rulesFor("audio-node", "phone")) {
		t.Fatal("legacy policy was not upgraded before credentials")
	}
	for _, privileged := range []bool{true, false} {
		superuser, wrongPolicy = privileged, !privileged
		if err := c.ProvisionNode(context.Background(), username, "phone", secret); err == nil {
			t.Fatal("unsafe account adopted")
		}
		if writes != 5 {
			t.Fatal("unsafe account mutated")
		}
	}
}

func TestCreateInstallsRestrictivePolicyBeforeCredentialsAndRetriesOrphanPolicy(t *testing.T) {
	for _, kind := range []string{"node", "audio-node", "agent", "worker"} {
		t.Run(kind, func(t *testing.T) {
			var policy userRules
			var order []string
			source := false
			attempt := 0
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				path := strings.TrimPrefix(r.URL.Path, "/api/v5")
				switch {
				case path == "/authentication":
					authChain(w)
				case path == usersPath+"/new":
					w.WriteHeader(404)
				case path == "/clients":
					writeJSON(w, map[string]any{"data": []any{}})
				case path == "/authorization/sources" && r.Method == "GET":
					sources := []map[string]any{{"type": "file", "enable": true}}
					if source {
						sources = append([]map[string]any{{"type": "built_in_database", "enable": true}}, sources...)
					}
					writeJSON(w, map[string]any{"sources": sources})
				case path == "/authorization/sources" && r.Method == "POST":
					source = true
					order = append(order, "source")
					w.WriteHeader(204)
				case path == "/authorization/sources/built_in_database/rules/clients":
					writeJSON(w, map[string]any{"data": []any{}})
				case path == rulesPath+"/new" && r.Method == "GET":
					if policy.Username == "" {
						w.WriteHeader(404)
					} else {
						writeJSON(w, policy)
					}
				case path == rulesPath && r.Method == "POST":
					var rows []userRules
					_ = json.NewDecoder(r.Body).Decode(&rows)
					policy = rows[0]
					order = append(order, "policy")
					w.WriteHeader(204)
				case path == usersPath && r.Method == "POST":
					var user map[string]any
					_ = json.NewDecoder(r.Body).Decode(&user)
					if user["is_superuser"] != false || len(user["password"].(string)) < 40 || policy.Username != "new" {
						t.Error("unsafe credential provisioning")
					}
					order = append(order, "credential")
					attempt++
					if attempt == 1 {
						w.WriteHeader(503)
					} else {
						w.WriteHeader(201)
					}
				default:
					t.Errorf("unexpected %s %s", r.Method, path)
					w.WriteHeader(500)
				}
			})
			input := CreateInput{"new", kind, "device-one"}
			if _, err := client.Create(context.Background(), input); err == nil {
				t.Fatal("lost creation response must not claim success")
			}
			result, err := client.Create(context.Background(), input)
			if err != nil || result.Password == "" {
				t.Fatalf("retry failed: %v", err)
			}
			if !reflect.DeepEqual(order, []string{"source", "policy", "credential", "credential"}) {
				t.Fatal(order)
			}
			for _, rule := range policy.Rules {
				if kind == "worker" && rule.Permission == "allow" && rule.Action == "subscribe" && rule.Topic == "orbit/v1/audio/jobs" {
					continue
				}
				if rule.Permission == "allow" && !strings.Contains(rule.Topic, "/device-one/") {
					t.Fatal("scope escaped")
				}
			}
			if last := policy.Rules[len(policy.Rules)-1]; last != (rule{"deny", "all", "#"}) {
				t.Fatal("missing deny fallback")
			}
		})
	}
}

func TestRefusesDuplicateUsersAndUnsafeAuthorizationOrder(t *testing.T) {
	for _, duplicate := range []bool{true, false} {
		t.Run(fmt.Sprint(duplicate), func(t *testing.T) {
			writes := 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					writes++
					w.WriteHeader(500)
					return
				}
				switch strings.TrimPrefix(r.URL.Path, "/api/v5") {
				case "/authentication":
					authChain(w)
				case "/clients":
					writeJSON(w, map[string]any{"data": []any{}})
				case usersPath + "/existing":
					if duplicate {
						writeJSON(w, brokerUser{Username: "existing"})
					} else {
						w.WriteHeader(404)
					}
				case "/authorization/sources":
					writeJSON(w, map[string]any{"sources": []map[string]any{{"type": "file", "enable": true}, {"type": "built_in_database", "enable": true}}})
				default:
					w.WriteHeader(500)
				}
			})
			_, err := c.Create(context.Background(), CreateInput{"existing", "node", "phone"})
			if err == nil || writes != 0 {
				t.Fatal("unsafe broker mutation")
			}
		})
	}
}

func TestProtectedAccountAndInvalidIDsNeverReachBroker(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected broker request") })
	for _, name := range []string{"core", "../other", "user/other"} {
		if _, err := c.Revoke(context.Background(), name); err == nil {
			t.Fatal("accepted revoke", name)
		}
		if _, err := c.Rotate(context.Background(), name); err == nil {
			t.Fatal("accepted reset", name)
		}
		if _, err := c.Disconnect(context.Background(), name); err == nil {
			t.Fatal("accepted disconnect", name)
		}
	}
	for _, id := range []string{"#", "+", "x/other", "${username}", ""} {
		if _, err := c.Create(context.Background(), CreateInput{"new", "node", id}); err == nil {
			t.Fatal("accepted unsafe ID")
		}
	}
}

func TestRotateKeepsPasswordOnPartialSuccessAndPreservesSuperuser(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/api/v5") {
		case "/authentication":
			authChain(w)
		case usersPath + "/old":
			if r.Method == "GET" {
				writeJSON(w, brokerUser{"old", true})
				return
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["is_superuser"] != true {
				t.Error("reset changed privileges")
			}
			writeJSON(w, brokerUser{"old", true})
		case "/clients":
			w.WriteHeader(503)
		default:
			t.Fatal("unexpected path")
		}
	})
	result, err := c.Rotate(context.Background(), "old")
	if err != nil || result.Password == "" || result.Warning == "" {
		t.Fatal("partial result lost")
	}
}

func TestRevokeDeletesCredentialBeforeDisconnectAndKeepsACLOnFailure(t *testing.T) {
	var order []string
	failed := true
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v5")
		switch {
		case path == "/authentication":
			authChain(w)
		case path == usersPath+"/old":
			order = append(order, "credential")
			w.WriteHeader(404)
		case path == "/clients":
			if r.URL.Query().Get("username") != "old" {
				t.Error("missing exact username filter")
			}
			if r.URL.Query().Get("page") == "1" {
				writeJSON(w, map[string]any{"data": []connection{{"a", "old"}, {"other", "someone-else"}}, "meta": map[string]any{"hasnext": true}})
			} else {
				writeJSON(w, map[string]any{"data": []connection{{"b", "old"}}, "meta": map[string]any{"hasnext": false}})
			}
		case path == "/clients/a" || path == "/clients/b":
			order = append(order, path)
			if failed && path == "/clients/a" {
				w.WriteHeader(503)
			} else {
				w.WriteHeader(204)
			}
		case path == rulesPath+"/old":
			order = append(order, "acl")
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected request %s", path)
			w.WriteHeader(500)
		}
	})
	result, err := c.Revoke(context.Background(), "old")
	if err != nil || result.Warning == "" || !reflect.DeepEqual(order, []string{"credential", "/clients/a", "/clients/b"}) {
		t.Fatal("unsafe partial revoke", order)
	}
	failed = false
	order = nil
	result, err = c.Revoke(context.Background(), "old")
	if err != nil || result.Warning != "" || !reflect.DeepEqual(order, []string{"credential", "/clients/a", "/clients/b", "acl"}) {
		t.Fatal("retry not complete", order)
	}
}

func TestListShowsOrphanConnectionsAndProtectsCore(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/api/v5") {
		case "/authentication":
			authChain(w)
		case usersPath:
			writeJSON(w, map[string]any{"data": []brokerUser{{"core", false}}})
		case "/clients":
			writeJSON(w, map[string]any{"data": []connection{{"orphan-1", "gone"}, {"core-1", "core"}}})
		default:
			w.WriteHeader(500)
		}
	})
	users, err := c.List(context.Background())
	if err != nil || len(users) != 2 || !users[0].Protected || !users[1].Revoked || len(users[1].Clients) != 1 {
		t.Fatal("lost pending revoke state")
	}
}

func TestBrokerCredentialsNeverEscapeOnErrorOrRedirect(t *testing.T) {
	for _, status := range []int{401, 403, 500, 302} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			followed := false
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { followed = true }))
			defer target.Close()
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", target.URL)
				w.WriteHeader(status)
				_, _ = w.Write([]byte("private-key private-secret"))
			})
			_, err := c.List(context.Background())
			e := PublicError(err)
			if followed || strings.Contains(e.Message, "private-") || e.Status == 401 {
				t.Fatal("credential or auth-boundary leak")
			}
		})
	}
}
