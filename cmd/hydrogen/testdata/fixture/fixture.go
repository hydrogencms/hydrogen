// Package fixture is a third-party module for the CLI tests.
package fixture

import "github.com/hydrogencms/hydrogen/core/kernel"

type module struct{}

func Module() kernel.Module { return module{} }

func (module) Name() string { return "fixture" }

func (module) Register(r *kernel.Registry) error { return nil }
