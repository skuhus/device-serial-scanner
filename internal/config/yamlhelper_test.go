package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// yamlScalar builds the node a YAML decoder would hand to UnmarshalYAML.
func yamlScalar(t *testing.T, value string) *yaml.Node {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(value), &node); err != nil {
		t.Fatalf("build yaml node: %v", err)
	}
	return node.Content[0]
}
