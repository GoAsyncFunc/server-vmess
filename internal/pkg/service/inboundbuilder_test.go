package service

import (
	"encoding/json"
	"os"
	"testing"

	api "github.com/GoAsyncFunc/uniproxy/pkg"
	"github.com/xtls/xray-core/infra/conf"
)

func TestInboundBuilder_VMess_TCP(t *testing.T) {
	certFile, keyFile := generateTestCert(t)
	defer os.Remove(certFile)
	defer os.Remove(keyFile)

	config := &Config{
		Cert: &CertConfig{
			CertFile: certFile,
			KeyFile:  keyFile,
		},
	}
	nodeInfo := &api.NodeInfo{
		VMess: &api.VMessNode{
			CommonNode: api.CommonNode{
				ServerPort: 10086,
			},
			Network: "tcp",
		},
	}

	inboundConfig, err := InboundBuilder(config, nodeInfo)
	if err != nil {
		t.Fatalf("InboundBuilder failed: %v", err)
	}

	if inboundConfig.ReceiverSettings == nil {
		t.Errorf("Expected ReceiverSettings to be non-nil")
	}
	if inboundConfig.ProxySettings == nil {
		t.Errorf("Expected ProxySettings to be non-nil")
	}
}

func TestInboundBuilder_VMess_WS(t *testing.T) {
	certFile, keyFile := generateTestCert(t)
	defer os.Remove(certFile)
	defer os.Remove(keyFile)

	config := &Config{
		Cert: &CertConfig{
			CertFile: certFile,
			KeyFile:  keyFile,
		},
	}
	wsSettings := conf.WebSocketConfig{
		Path: "/ws",
	}
	wsBytes, _ := json.Marshal(wsSettings)

	nodeInfo := &api.NodeInfo{
		VMess: &api.VMessNode{
			CommonNode: api.CommonNode{
				ServerPort: 443,
			},
			Network:         "ws",
			NetworkSettings: json.RawMessage(wsBytes),
			Tls:             1,
		},
	}

	inboundConfig, err := InboundBuilder(config, nodeInfo)
	if err != nil {
		t.Fatalf("InboundBuilder failed: %v", err)
	}

	if inboundConfig.ReceiverSettings == nil {
		t.Errorf("Expected ReceiverSettings to be non-nil")
	}
	if inboundConfig.ProxySettings == nil {
		t.Errorf("Expected ProxySettings to be non-nil")
	}
}
