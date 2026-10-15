package kernel

import (
	"errors"
	"fmt"

	"github.com/hydrogencms/hydrogen/channel"
	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/schema"
)

// compileChannels builds the channel set from the "channels" config section with the options and checks the
// modules registered.
func compileChannels(cfg *config.Config, defaultLocale string, s *schema.Schema, reg *Registry) (*channel.Set, error) {
	if err := reg.checkChannelOptions(); err != nil {
		return nil, err
	}
	n, err := cfg.Node("channels")
	if err != nil {
		return nil, err
	}
	o := channel.CompileOptions{Schema: s, DefaultLocale: defaultLocale}
	for _, op := range reg.chanOpts {
		o.Options = append(o.Options, op.value)
	}
	for _, c := range reg.chanChecks {
		o.Checks = append(o.Checks, moduleCheck(c.module, c.value))
	}
	set, err := channel.Compile(n, o)
	if err != nil {
		return nil, fmt.Errorf("channels:\n%w", err)
	}
	return set, nil
}

// checkChannelOptions names both modules when two register the same option.
func (r *Registry) checkChannelOptions() error {
	var errs []error
	seen := map[string]string{}
	for _, op := range r.chanOpts {
		if prev, dup := seen[op.value.Name]; dup {
			errs = append(errs, fmt.Errorf("channel option %q registered by both %s and %s", op.value.Name, prev, op.module))
			continue
		}
		seen[op.value.Name] = op.module
	}
	errs = append(errs, r.checkPublicOptions())
	return errors.Join(errs...)
}

// moduleCheck prefixes each problem a module's check reports with the module's name.
func moduleCheck(module string, chk channel.Check) channel.Check {
	return func(s *channel.Set) error {
		err := chk(s)
		if err == nil {
			return nil
		}
		var list []error
		if j, ok := err.(interface{ Unwrap() []error }); ok { //nolint:errorlint // only a join is flattened
			list = j.Unwrap()
		} else {
			list = []error{err}
		}
		out := make([]error, len(list))
		for i, e := range list {
			out[i] = fmt.Errorf("module %s: %w", module, e)
		}
		return errors.Join(out...)
	}
}
