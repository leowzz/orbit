package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMQTTAdminConfiguration(t *testing.T) {
	for _, cfg := range []MQTTAdminConfig{{}, {URL: "http://emqx:18083"}, {URL: "https://broker.example/api/v5/", APIKey: "key", APISecret: "secret"}} {
		if err := cfg.validate(); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(cfg)
		if string(b) != "{}" {
			t.Fatal("admin credentials serialized")
		}
	}
	for _, cfg := range []MQTTAdminConfig{
		{URL: "http://user:secret@emqx:18083", APIKey: "key", APISecret: "secret"},
		{URL: "http://emqx:18083?key=secret"}, {URL: "http://emqx:18083/other"},
		{URL: "mqtt://emqx:1883"}, {APIKey: "key", APISecret: "secret"},
		{URL: "http://emqx:18083", APIKey: "key"},
		{URL: "http://emqx:18083", APIKey: "key:secret", APISecret: "secret"},
	} {
		if err := cfg.validate(); err == nil || strings.Contains(err.Error(), "key=secret") {
			t.Fatal("invalid configuration accepted or echoed")
		}
	}
}
