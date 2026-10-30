// Package mqttadmin manages EMQX 5 built-in username authentication and per-user ACLs.
package mqttadmin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

const usersPath = "/authentication/password_based:built_in_database/users"
const rulesPath = "/authorization/sources/built_in_database/rules/users"

type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

type brokerError struct {
	status int
	code   string
}

func (e *brokerError) Error() string { return "MQTT 管理请求失败" }
func isStatus(err error, status int) bool {
	var e *brokerError
	return errors.As(err, &e) && e.status == status
}

// PublicError never includes upstream response bodies, URLs, credentials or transport errors.
func PublicError(err error) *Error {
	var public *Error
	if errors.As(err, &public) {
		return public
	}
	var upstream *brokerError
	if errors.As(err, &upstream) {
		switch {
		case upstream.status == 401 || upstream.status == 403:
			return &Error{502, "MQTT 管理凭据无效或权限不足，请检查配置中的 API Key 和 Secret"}
		case upstream.status == 409 || upstream.code == "ALREADY_EXISTS":
			return &Error{409, "用户名或权限记录已存在，请刷新后重试"}
		case upstream.status == 404:
			return &Error{404, "MQTT 用户或管理资源不存在，请刷新后重试"}
		}
	}
	return &Error{502, "无法完成 MQTT 管理请求，请检查服务连接后重试"}
}

type Client struct {
	base, key, secret, protected string
	http                         *http.Client
}

func New(base, key, secret, protected string) *Client {
	base = strings.TrimRight(base, "/")
	if !strings.HasSuffix(base, "/api/v5") {
		base += "/api/v5"
	}
	return &Client{base: base, key: key, secret: secret, protected: protected,
		http: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *Client) request(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	r, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	r.SetBasicAuth(c.key, c.secret)
	r.Header.Set("Accept", "application/json")
	if input != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(r)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	b, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
	if err != nil || len(b) > 2*1024*1024 {
		return errors.New("invalid broker response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var detail struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(b, &detail)
		return &brokerError{response.StatusCode, detail.Code}
	}
	if output != nil {
		return json.Unmarshal(b, output)
	}
	return nil
}

type User struct {
	Username  string   `json:"username"`
	Superuser bool     `json:"is_superuser"`
	Protected bool     `json:"protected"`
	Revoked   bool     `json:"revoked"`
	Clients   []string `json:"clients"`
}
type brokerUser struct {
	Username  string `json:"user_id"`
	Superuser bool   `json:"is_superuser"`
}
type connection struct {
	ID       string `json:"clientid"`
	Username string `json:"username"`
}

func pageAll[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	result := []T{}
	for page := 1; page <= 100; page++ {
		separator := "?"
		if strings.Contains(path, "?") {
			separator = "&"
		}
		var response struct {
			Data []T `json:"data"`
			Meta struct {
				HasNext bool `json:"hasnext"`
				Count   int  `json:"count"`
			} `json:"meta"`
		}
		if err := c.request(ctx, "GET", fmt.Sprintf("%s%spage=%d&limit=100", path, separator, page), nil, &response); err != nil {
			return nil, err
		}
		result = append(result, response.Data...)
		if !response.Meta.HasNext && (response.Meta.Count <= len(result) || len(response.Data) < 100) {
			return result, nil
		}
		if len(response.Data) == 0 {
			return nil, errors.New("invalid broker pagination")
		}
	}
	return nil, &Error{502, "MQTT 账号或连接过多，未能完整读取，请在 EMQX 管理台处理"}
}

func (c *Client) checkAuthentication(ctx context.Context) error {
	var chain []struct {
		ID         string `json:"id"`
		Enable     bool   `json:"enable"`
		UserIDType string `json:"user_id_type"`
	}
	if err := c.request(ctx, "GET", "/authentication", nil, &chain); err != nil {
		return err
	}
	enabled := 0
	for _, a := range chain {
		if !a.Enable {
			continue
		}
		enabled++
		if a.ID != "password_based:built_in_database" || a.UserIDType != "username" {
			return &Error{422, "仅支持使用用户名的单一内置数据库认证，请先检查 EMQX 认证配置"}
		}
	}
	if enabled != 1 {
		return &Error{422, "请先在 EMQX 启用用户名内置数据库认证"}
	}
	return nil
}

func (c *Client) List(ctx context.Context) ([]User, error) {
	if err := c.checkAuthentication(ctx); err != nil {
		return nil, err
	}
	users, err := pageAll[brokerUser](ctx, c, usersPath)
	if err != nil {
		return nil, err
	}
	clients, err := pageAll[connection](ctx, c, "/clients")
	if err != nil {
		return nil, err
	}
	byName := map[string]*User{}
	for _, u := range users {
		byName[u.Username] = &User{Username: u.Username, Superuser: u.Superuser, Protected: u.Username == c.protected, Clients: []string{}}
	}
	for _, client := range clients {
		if client.Username == "" {
			continue
		}
		u := byName[client.Username]
		if u == nil {
			u = &User{Username: client.Username, Protected: client.Username == c.protected, Revoked: true, Clients: []string{}}
			byName[client.Username] = u
		}
		u.Clients = append(u.Clients, client.ID)
	}
	result := make([]User, 0, len(byName))
	for _, u := range byName {
		sort.Strings(u.Clients)
		result = append(result, *u)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Username < result[j].Username })
	return result, nil
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{0,127}$`)
var participantPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func (c *Client) mutable(username string) error {
	if !usernamePattern.MatchString(username) {
		return &Error{400, "用户名需为 1–128 位字母、数字或 _ . @ -，并以字母或数字开头"}
	}
	if username == c.protected {
		return &Error{403, "此账号用于当前 Core 连接，不能在此修改或吊销"}
	}
	return nil
}

type CreateInput struct {
	Username      string `json:"username"`
	Kind          string `json:"kind"`
	ParticipantID string `json:"participant_id"`
}
type Result struct {
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	Warning  string `json:"warning,omitempty"`
}
type rule struct {
	Permission string `json:"permission"`
	Action     string `json:"action"`
	Topic      string `json:"topic"`
}
type userRules struct {
	Username string `json:"username"`
	Rules    []rule `json:"rules"`
}

func rulesFor(kind, id string) []rule {
	root := "orbit/v1/nodes/" + id
	if kind == "agent" || kind == "worker" {
		root = "orbit/v1/agents/" + id
	}
	if kind == "windows-agent" {
		root = "orbit/v1/agents/" + id
		return []rule{
			{"allow", "publish", root + "/state"},
			{"allow", "publish", root + "/observations/+"},
			{"allow", "publish", root + "/results"},
			{"allow", "publish", root + "/shortcuts"},
			{"allow", "subscribe", root + "/commands"},
			{"allow", "publish", root + "/audio/state"},
			{"allow", "publish", root + "/audio/results"},
			{"allow", "subscribe", root + "/audio/acks"},
			{"allow", "subscribe", "orbit/v1/audio/jobs"},
			{"deny", "all", "$SYS/#"},
			{"deny", "all", "#"},
		}
	}
	if kind == "worker" {
		return []rule{
			{"allow", "publish", root + "/audio/state"},
			{"allow", "publish", root + "/audio/results"},
			{"allow", "subscribe", root + "/audio/acks"},
			{"allow", "subscribe", "orbit/v1/audio/jobs"},
			{"deny", "all", "$SYS/#"},
			{"deny", "all", "#"},
		}
	}
	rules := []rule{{"allow", "publish", root + "/state"}}
	if kind == "agent" {
		rules = append(rules, rule{"allow", "publish", root + "/observations/+"}, rule{"allow", "publish", root + "/results"}, rule{"allow", "publish", root + "/shortcuts"}, rule{"allow", "subscribe", root + "/commands"})
	} else {
		rules = append(rules, rule{"allow", "publish", root + "/intents"}, rule{"allow", "subscribe", root + "/view"}, rule{"allow", "subscribe", root + "/navigation"})
		if kind == "audio-node" {
			rules = append(rules, rule{"allow", "publish", root + "/audio/segments"}, rule{"allow", "subscribe", root + "/audio/acks"})
		}
	}
	return append(rules, rule{"deny", "all", "$SYS/#"}, rule{"deny", "all", "#"})
}

// Only a missing source is added. Existing source priorities/settings are never overwritten.
func (c *Client) ensureAuthorization(ctx context.Context) error {
	type source struct {
		Type   string `json:"type"`
		Enable bool   `json:"enable"`
	}
	var data struct {
		Sources []source `json:"sources"`
	}
	if err := c.request(ctx, "GET", "/authorization/sources", nil, &data); err != nil {
		return err
	}
	found := false
	for i, s := range data.Sources {
		if s.Type == "built_in_database" {
			found = true
			if !s.Enable || i != 0 {
				return &Error{422, "请先将 EMQX 的内置数据库授权启用并放到授权列表首位"}
			}
		}
	}
	if found {
		return c.checkClientRules(ctx)
	}
	if err := c.request(ctx, "POST", "/authorization/sources", map[string]any{"type": "built_in_database", "enable": true}, nil); err != nil {
		return err
	}
	// Verify EMQX's prepend behavior before installing any user or granting login.
	if err := c.request(ctx, "GET", "/authorization/sources", nil, &data); err != nil {
		return err
	}
	if len(data.Sources) == 0 || data.Sources[0].Type != "built_in_database" || !data.Sources[0].Enable {
		return &Error{422, "内置数据库授权未位于首位，请在 EMQX 调整后重试"}
	}
	return c.checkClientRules(ctx)
}

// EMQX evaluates Client-ID rules before username rules. A caller can choose its
// own Client ID, so those rules must not be able to bypass a newly scoped user.
func (c *Client) checkClientRules(ctx context.Context) error {
	var response struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := c.request(ctx, "GET", "/authorization/sources/built_in_database/rules/clients?limit=1", nil, &response); err != nil {
		return err
	}
	if len(response.Data) > 0 {
		return &Error{422, "EMQX 存在优先于用户名的 Client ID 权限规则，请先改为按用户名授权，再创建限权账号"}
	}
	return nil
}

func password() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b), err
}

func (c *Client) Create(ctx context.Context, input CreateInput) (Result, error) {
	if err := c.mutable(input.Username); err != nil {
		return Result{}, err
	}
	if (input.Kind != "node" && input.Kind != "audio-node" && input.Kind != "agent" && input.Kind != "worker") || !participantPattern.MatchString(input.ParticipantID) {
		return Result{}, &Error{400, "请选择连接类型，并填写有效 ID（小写字母、数字、下划线或短横线，最多 64 位）"}
	}
	if err := c.checkAuthentication(ctx); err != nil {
		return Result{}, err
	}
	err := c.request(ctx, "GET", usersPath+"/"+url.PathEscape(input.Username), nil, &brokerUser{})
	if err == nil {
		return Result{}, &Error{409, "用户名已存在，请换一个用户名"}
	}
	if !isStatus(err, 404) {
		return Result{}, err
	}
	connections, err := pageAll[connection](ctx, c, "/clients?username="+url.QueryEscape(input.Username))
	if err != nil {
		return Result{}, err
	}
	for _, connection := range connections {
		if connection.Username == input.Username {
			return Result{}, &Error{409, "此用户名仍有连接，请先吊销并清理连接，再创建账号"}
		}
	}
	if err = c.ensureAuthorization(ctx); err != nil {
		return Result{}, err
	}
	policy := userRules{input.Username, rulesFor(input.Kind, input.ParticipantID)}
	var existing userRules
	err = c.request(ctx, "GET", rulesPath+"/"+url.PathEscape(input.Username), nil, &existing)
	if err == nil && !slices.Equal(existing.Rules, policy.Rules) {
		return Result{}, &Error{409, "此用户名已有其他权限规则，请换一个用户名或在 EMQX 检查"}
	}
	if isStatus(err, 404) {
		err = c.request(ctx, "POST", rulesPath, []userRules{policy}, nil)
	}
	if err != nil {
		return Result{}, err
	}
	secret, err := password()
	if err != nil {
		return Result{}, err
	}
	// Install restrictive rules BEFORE the credential, so a new user is never unrestricted.
	if err = c.request(ctx, "POST", usersPath, map[string]any{"user_id": input.Username, "password": secret, "is_superuser": false}, nil); err != nil {
		return Result{}, &Error{502, "权限已保留，但未能确认账号是否创建。请刷新；若账号已出现，请重置密码，否则可重试创建"}
	}
	return Result{Username: input.Username, Password: secret}, nil
}

// provisionDevice creates or updates a Core-owned account. Existing policies are
// only migrated when they are one of the explicitly supported legacy policies.
func (c *Client) provisionDevice(ctx context.Context, username, nodeID, secret, kind string, legacyPolicies ...[]rule) error {
	if err := c.mutable(username); err != nil {
		return err
	}
	if !strings.HasPrefix(username, "orbit-app-") || !participantPattern.MatchString(nodeID) || len(secret) < 40 {
		return &Error{400, "设备 MQTT 认证信息无效"}
	}
	if err := c.checkAuthentication(ctx); err != nil {
		return err
	}
	if err := c.ensureAuthorization(ctx); err != nil {
		return err
	}
	var user brokerUser
	err := c.request(ctx, "GET", usersPath+"/"+url.PathEscape(username), nil, &user)
	exists := err == nil
	if err != nil && !isStatus(err, 404) {
		return err
	}
	if exists && user.Superuser {
		return &Error{409, "设备 MQTT 用户权限不匹配"}
	}
	policy := userRules{username, rulesFor(kind, nodeID)}
	var current userRules
	err = c.request(ctx, "GET", rulesPath+"/"+url.PathEscape(username), nil, &current)
	if err == nil && !slices.Equal(current.Rules, policy.Rules) {
		adoptable := false
		for _, legacy := range legacyPolicies {
			if slices.Equal(current.Rules, legacy) {
				adoptable = true
				break
			}
		}
		if !adoptable {
			return &Error{409, "设备 MQTT 权限规则不匹配"}
		}
		err = c.request(ctx, "PUT", rulesPath+"/"+url.PathEscape(username), policy, nil)
	}
	if isStatus(err, 404) {
		if exists {
			return &Error{409, "设备 MQTT 权限规则缺失"}
		}
		err = c.request(ctx, "POST", rulesPath, []userRules{policy}, nil)
	}
	if err != nil {
		return err
	}
	if exists {
		return c.request(ctx, "PUT", usersPath+"/"+url.PathEscape(username), map[string]any{"password": secret, "is_superuser": false}, nil)
	}
	return c.request(ctx, "POST", usersPath, map[string]any{"user_id": username, "password": secret, "is_superuser": false}, nil)
}

// ProvisionNode provisions the Android App's Core-owned account.
func (c *Client) ProvisionNode(ctx context.Context, username, nodeID, secret string) error {
	return c.provisionDevice(ctx, username, nodeID, secret, "audio-node", rulesFor("node", nodeID))
}

// ProvisionAgent provisions the Windows Agent account. The Agent and its local
// ASR Worker use the same MQTT credentials while retaining separate client IDs.
func (c *Client) ProvisionAgent(ctx context.Context, username, nodeID, secret string) error {
	return c.provisionDevice(
		ctx,
		username,
		nodeID,
		secret,
		"windows-agent",
		rulesFor("audio-node", nodeID),
		rulesFor("agent", nodeID),
	)
}

func (c *Client) disconnect(ctx context.Context, username string) error {
	clients, err := pageAll[connection](ctx, c, "/clients?username="+url.QueryEscape(username))
	if err != nil {
		return err
	}
	var failed error
	for _, client := range clients {
		if client.Username != username {
			continue
		}
		err = c.request(ctx, "DELETE", "/clients/"+url.PathEscape(client.ID), nil, nil)
		if err != nil && !isStatus(err, 404) {
			failed = err
		}
	}
	return failed
}

func (c *Client) Disconnect(ctx context.Context, username string) (Result, error) {
	if err := c.mutable(username); err != nil {
		return Result{}, err
	}
	if err := c.disconnect(ctx, username); err != nil {
		return Result{}, err
	}
	return Result{Username: username}, nil
}

func (c *Client) Rotate(ctx context.Context, username string) (Result, error) {
	if err := c.mutable(username); err != nil {
		return Result{}, err
	}
	if err := c.checkAuthentication(ctx); err != nil {
		return Result{}, err
	}
	var user brokerUser
	if err := c.request(ctx, "GET", usersPath+"/"+url.PathEscape(username), nil, &user); err != nil {
		return Result{}, err
	}
	secret, err := password()
	if err != nil {
		return Result{}, err
	}
	if err = c.request(ctx, "PUT", usersPath+"/"+url.PathEscape(username), map[string]any{"password": secret, "is_superuser": user.Superuser}, nil); err != nil {
		return Result{}, &Error{502, "未能确认密码是否重置，请重新执行重置以获取可用密码"}
	}
	result := Result{Username: username, Password: secret}
	if err = c.disconnect(ctx, username); err != nil {
		result.Warning = "密码已重置，但未能断开全部旧连接，请重试断开连接"
	}
	return result, nil
}

func (c *Client) Revoke(ctx context.Context, username string) (Result, error) {
	if err := c.mutable(username); err != nil {
		return Result{}, err
	}
	if err := c.checkAuthentication(ctx); err != nil {
		return Result{}, err
	}
	err := c.request(ctx, "DELETE", usersPath+"/"+url.PathEscape(username), nil, nil)
	if err != nil && !isStatus(err, 404) {
		return Result{}, err
	}
	result := Result{Username: username}
	if err = c.disconnect(ctx, username); err != nil {
		result.Warning = "凭据已删除，但未能断开全部已有连接。权限规则已保留，请重试吊销"
		return result, nil
	}
	err = c.request(ctx, "DELETE", rulesPath+"/"+url.PathEscape(username), nil, nil)
	if err != nil && !isStatus(err, 404) {
		result.Warning = "凭据已删除、已有连接已断开，但权限规则未清理，请重试吊销"
	}
	return result, nil
}
