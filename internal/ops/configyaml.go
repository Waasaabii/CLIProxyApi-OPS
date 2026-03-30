package ops

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

func (m *Manager) syncConfigFile(cfg DeployConfig) error {
	if _, err := os.Stat(cfg.ConfigFile); err == nil {
		return mergeConfigFile(cfg)
	}
	return writeInitialConfigFile(cfg)
}

func writeInitialConfigFile(cfg DeployConfig) error {
	root := &yaml.Node{Kind: yaml.DocumentNode}
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	root.Content = []*yaml.Node{mapping}

	setMapScalar(mapping, "port", strconv.Itoa(cfg.ContainerPort), "!!int")
	remote := ensureMapValue(mapping, "remote-management")
	setMapScalar(remote, "allow-remote", boolToYAML(cfg.AllowRemoteManagement), "!!bool")
	setMapScalar(remote, "secret-key", cfg.ManagementSecret, "!!str")
	setMapScalar(remote, "disable-control-panel", boolToYAML(cfg.DisableControlPanel), "!!bool")
	setMapScalar(mapping, "auth-dir", cfg.AuthDir, "!!str")
	setMapScalar(mapping, "debug", boolToYAML(cfg.Debug), "!!bool")
	setMapScalar(mapping, "logging-to-file", "false", "!!bool")
	setMapScalar(mapping, "usage-statistics-enabled", boolToYAML(cfg.UsageStatisticsEnabled), "!!bool")
	setMapScalar(mapping, "request-retry", strconv.Itoa(cfg.RequestRetry), "!!int")

	quota := ensureMapValue(mapping, "quota-exceeded")
	setMapScalar(quota, "switch-project", "true", "!!bool")
	setMapScalar(quota, "switch-preview-model", "true", "!!bool")

	setFirstSequenceItem(mapping, "api-keys", cfg.APIKey)

	data, err := marshalYAMLNode(root)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}
	return writeFileAtomic(cfg.ConfigFile, data, 0o644)
}

func mergeConfigFile(cfg DeployConfig) error {
	data, err := os.ReadFile(cfg.ConfigFile)
	if err != nil {
		return err
	}
	var root yaml.Node
	if err = yaml.Unmarshal(data, &root); err != nil {
		return err
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return fmt.Errorf("无效的 config.yaml 结构")
	}
	mapping := root.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return fmt.Errorf("config.yaml 根节点必须是 mapping")
	}

	setMapScalar(mapping, "port", strconv.Itoa(cfg.ContainerPort), "!!int")
	remote := ensureMapValue(mapping, "remote-management")
	setMapScalar(remote, "allow-remote", boolToYAML(cfg.AllowRemoteManagement), "!!bool")
	setMapScalar(remote, "secret-key", cfg.ManagementSecret, "!!str")
	setMapScalar(remote, "disable-control-panel", boolToYAML(cfg.DisableControlPanel), "!!bool")
	setMapScalar(mapping, "auth-dir", cfg.AuthDir, "!!str")
	setMapScalar(mapping, "debug", boolToYAML(cfg.Debug), "!!bool")
	setMapScalar(mapping, "usage-statistics-enabled", boolToYAML(cfg.UsageStatisticsEnabled), "!!bool")
	setMapScalar(mapping, "request-retry", strconv.Itoa(cfg.RequestRetry), "!!int")
	setFirstSequenceItem(mapping, "api-keys", cfg.APIKey)

	rendered, err := marshalYAMLNode(&root)
	if err != nil {
		return err
	}
	return writeFileAtomic(cfg.ConfigFile, rendered, 0o644)
}

func ensureMapValue(parent *yaml.Node, key string) *yaml.Node {
	value := getOrCreateMapValue(parent, key)
	if value.Kind != yaml.MappingNode {
		value.Kind = yaml.MappingNode
		value.Tag = "!!map"
		value.Content = nil
	}
	return value
}

func setMapScalar(parent *yaml.Node, key, value, tag string) {
	node := getOrCreateMapValue(parent, key)
	node.Kind = yaml.ScalarNode
	node.Tag = tag
	node.Value = value
	node.Style = 0
}

func setFirstSequenceItem(parent *yaml.Node, key, value string) {
	node := getOrCreateMapValue(parent, key)
	if node.Kind != yaml.SequenceNode {
		node.Kind = yaml.SequenceNode
		node.Tag = "!!seq"
		node.Content = nil
	}
	entry := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
	if len(node.Content) == 0 {
		node.Content = []*yaml.Node{entry}
		return
	}
	node.Content[0] = entry
}

func getOrCreateMapValue(mapNode *yaml.Node, key string) *yaml.Node {
	if mapNode.Kind != yaml.MappingNode {
		mapNode.Kind = yaml.MappingNode
		mapNode.Tag = "!!map"
		mapNode.Content = nil
	}
	for i := 0; i+1 < len(mapNode.Content); i += 2 {
		k := mapNode.Content[i]
		if k.Value == key {
			return mapNode.Content[i+1]
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valueNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: ""}
	mapNode.Content = append(mapNode.Content, keyNode, valueNode)
	return valueNode
}

func boolToYAML(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
