package service

import (
	"encoding/json"
	"fmt"

	api "github.com/GoAsyncFunc/uniproxy/pkg"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
)

// InboundBuilder builds Inbound config for VMess.
func InboundBuilder(config *Config, nodeInfo *api.NodeInfo) (*core.InboundHandlerConfig, error) {
	if nodeInfo.VMess == nil {
		return nil, fmt.Errorf("node info missing VMess config")
	}
	vmessInfo := nodeInfo.VMess

	inboundDetourConfig := &conf.InboundDetourConfig{}

	// Port
	portList := &conf.PortList{
		Range: []conf.PortRange{{From: uint32(vmessInfo.ServerPort), To: uint32(vmessInfo.ServerPort)}},
	}
	inboundDetourConfig.PortList = portList

	// Tag
	inboundDetourConfig.Tag = fmt.Sprintf("vmess_%d", vmessInfo.ServerPort)

	// Protocol
	inboundDetourConfig.Protocol = "vmess"

	// Settings
	// VMess needs a VMessServerConfig deserialized into Settings
	// Since we populate users dynamically, we start with an empty config
	type VMessSettings struct {
		Clients []json.RawMessage `json:"clients"`
	}
	settings := VMessSettings{
		Clients: []json.RawMessage{},
	}
	settingsBytes, _ := json.Marshal(settings)
	settingsJSON := json.RawMessage(settingsBytes)
	inboundDetourConfig.Settings = &settingsJSON

	// Stream Settings
	streamSetting, err := buildStreamConfig(vmessInfo, config)
	if err != nil {
		return nil, err
	}
	inboundDetourConfig.StreamSetting = streamSetting

	return inboundDetourConfig.Build()
}

func buildStreamConfig(vmessInfo *api.VMessNode, config *Config) (*conf.StreamConfig, error) {
	streamSetting := new(conf.StreamConfig)
	transportProtocol := conf.TransportProtocol(vmessInfo.Network)
	streamSetting.Network = &transportProtocol

	// Network Settings
	if len(vmessInfo.NetworkSettings) > 0 {
		var err error
		switch transportProtocol {
		case "tcp":
			err = buildTCPConfig(streamSetting, vmessInfo)
		case "ws":
			err = buildWSConfig(streamSetting, vmessInfo)
		case "grpc":
			err = buildGRPCConfig(streamSetting, vmessInfo)
		case "splithttp", "xhttp":
			err = buildXHTTPConfig(streamSetting, vmessInfo)
		}
		if err != nil {
			return nil, err
		}
	}

	// Security (TLS)
	tlsSettings := new(conf.TLSConfig)
	switch vmessInfo.Tls {
	case 1: // TLS
		streamSetting.Security = "tls"
		tlsSettings.Certs = []*conf.TLSCertConfig{
			{
				CertFile: config.Cert.CertFile,
				KeyFile:  config.Cert.KeyFile,
			},
		}
		streamSetting.TLSSettings = tlsSettings
	case 0: // None
		streamSetting.Security = "none"
	}

	return streamSetting, nil
}

func buildTCPConfig(streamSetting *conf.StreamConfig, vmessInfo *api.VMessNode) error {
	tcpConfig := new(conf.TCPConfig)
	if err := json.Unmarshal(vmessInfo.NetworkSettings, tcpConfig); err == nil {
		streamSetting.TCPSettings = tcpConfig
	}
	return nil
}

func buildWSConfig(streamSetting *conf.StreamConfig, vmessInfo *api.VMessNode) error {
	wsConfig := new(conf.WebSocketConfig)
	if err := json.Unmarshal(vmessInfo.NetworkSettings, wsConfig); err != nil {
		return fmt.Errorf("unmarshal ws config error: %w", err)
	}
	streamSetting.WSSettings = wsConfig
	return nil
}

func buildGRPCConfig(streamSetting *conf.StreamConfig, vmessInfo *api.VMessNode) error {
	grpcConfig := new(conf.GRPCConfig)
	if err := json.Unmarshal(vmessInfo.NetworkSettings, grpcConfig); err != nil {
		return fmt.Errorf("unmarshal grpc config error: %w", err)
	}
	streamSetting.GRPCSettings = grpcConfig
	return nil
}

func buildXHTTPConfig(streamSetting *conf.StreamConfig, vmessInfo *api.VMessNode) error {
	splitHTTPConfig := new(conf.SplitHTTPConfig)
	if err := json.Unmarshal(vmessInfo.NetworkSettings, splitHTTPConfig); err != nil {
		return fmt.Errorf("unmarshal splithttp config error: %w", err)
	}
	streamSetting.SplitHTTPSettings = splitHTTPConfig
	return nil
}
