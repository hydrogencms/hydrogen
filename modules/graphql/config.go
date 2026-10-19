package graphql

import (
	"errors"
	"fmt"

	"github.com/hydrogencms/hydrogen/api/delivery"
)

// Defaults of modules.graphql.
const (
	DefaultMaxDepth      = 10
	DefaultMaxFields     = 500
	DefaultMaxQueryBytes = 16384
	DefaultMaxCalls      = 50
	DefaultMaxNodes      = 20000
	DefaultMaxItems      = 5000
)

// Config is modules.graphql.
type Config struct {
	// MaxDepth is the deepest field nesting a query may have, fragments expanded (default 10).
	MaxDepth int `yaml:"max_depth"`
	// MaxFields is how many fields a query may select once fragments are expanded (default 500).
	MaxFields int `yaml:"max_fields"`
	// MaxQueryBytes is the longest query text, in bytes (default 16384, at most the POST body limit).
	MaxQueryBytes int `yaml:"max_query_bytes"`
	// Introspection serves __schema, __type and the SDL endpoint (default true).
	Introspection bool `yaml:"introspection"`
	// MaxCalls bounds the internal delivery requests one GraphQL request makes (default 50).
	MaxCalls int `yaml:"max_calls"`
	// MaxNodes bounds the objects one response may hold: items, groups and list elements (default 20000).
	MaxNodes int `yaml:"max_nodes"`
	// MaxItems bounds the items one request asks the delivery API for: page size times include depth plus one,
	// summed over its calls (default 5000).
	MaxItems int `yaml:"max_items"`
}

func defaultConfig() Config {
	return Config{MaxDepth: DefaultMaxDepth, MaxFields: DefaultMaxFields, MaxQueryBytes: DefaultMaxQueryBytes,
		Introspection: true, MaxCalls: DefaultMaxCalls, MaxNodes: DefaultMaxNodes, MaxItems: DefaultMaxItems}
}

func (c *Config) validate() error {
	var errs []error
	positive := func(name string, v int) {
		if v < 1 {
			errs = append(errs, fmt.Errorf("modules.graphql.%s must be at least 1 (got %d)", name, v))
		}
	}
	positive("max_depth", c.MaxDepth)
	positive("max_fields", c.MaxFields)
	positive("max_query_bytes", c.MaxQueryBytes)
	positive("max_calls", c.MaxCalls)
	positive("max_nodes", c.MaxNodes)
	positive("max_items", c.MaxItems)
	if c.MaxQueryBytes > delivery.MaxBodyBytes {
		errs = append(errs, fmt.Errorf("modules.graphql.max_query_bytes must be at most %d, the largest POST body (got %d)",
			delivery.MaxBodyBytes, c.MaxQueryBytes))
	}
	return errors.Join(errs...)
}
