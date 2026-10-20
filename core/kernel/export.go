package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"

	"github.com/hydrogencms/hydrogen/core/cachetag"
	"github.com/hydrogencms/hydrogen/core/exportfmt"
	"github.com/hydrogencms/hydrogen/core/store"
)

// ExportOptions configure App.Export.
type ExportOptions struct {
	// Version is the Hydrogen version recorded in the manifest.
	Version       string
	PublishedOnly bool
	Progress      io.Writer
	// Layout of the item records: exportfmt.LayoutNDJSON (default) or exportfmt.LayoutItems.
	Layout string
}

// ImportOptions configure App.Import.
type ImportOptions struct {
	AllowSchemaChange bool
	Progress          io.Writer
}

var exporterName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Exporter registers a module's exporter under name (usually the module's own): it adds
// modules/<name>.ndjson to exports and restores it on import. Names are unique.
func (r *Registry) Exporter(name string, e exportfmt.Exporter) error {
	if e == nil {
		return fmt.Errorf("module %s: nil exporter", r.current)
	}
	if !exporterName.MatchString(name) {
		return fmt.Errorf("module %s: exporter name %q: use lowercase letters, digits and dashes", r.current, name)
	}
	if i := slices.IndexFunc(r.exporters, func(x exportfmt.NamedExporter) bool { return x.Name == name }); i >= 0 {
		return fmt.Errorf("module %s: exporter %q is already registered", r.current, name)
	}
	r.exporters = append(r.exporters, exportfmt.NamedExporter{Name: name, Exporter: e})
	return nil
}

// Exporters lists the registered module exporters, by module name.
func (a *App) Exporters() []exportfmt.NamedExporter { return exportfmt.SortedModules(a.reg.exporters) }

// Export writes the content database and every module's data to dir (see package exportfmt).
func (a *App) Export(ctx context.Context, dir string, o ExportOptions) (*exportfmt.Manifest, error) {
	s := a.services
	compiled, err := s.Schema.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return exportfmt.Export(ctx, exportfmt.ExportOptions{DB: s.DB, Dir: dir, Clock: s.Clock, Version: o.Version,
		SchemaHash: s.Schema.Hash(), Schema: compiled, Modules: a.Exporters(), PublishedOnly: o.PublishedOnly, Progress: o.Progress, Files: mediaFiles(s.Media), Layout: o.Layout})
}

// CheckImport runs the checks of Import that write nothing (checksums, schema, modules, media storage).
func (a *App) CheckImport(dir string, o ImportOptions) error {
	s := a.services
	return exportfmt.Check(exportfmt.ImportOptions{DB: s.DB, Content: s.Content, Dir: dir,
		AllowSchemaChange: o.AllowSchemaChange, Modules: a.Exporters(), Files: mediaFiles(s.Media)})
}

// Import restores an export into the empty content database (see exportfmt.Import).
func (a *App) Import(ctx context.Context, dir string, o ImportOptions) (*exportfmt.ImportResult, error) {
	s := a.services
	res, err := exportfmt.Import(ctx, exportfmt.ImportOptions{DB: s.DB, Content: s.Content, Outbox: s.Outbox, Clock: s.Clock, Dir: dir,
		AllowSchemaChange: o.AllowSchemaChange, Modules: a.Exporters(), Progress: o.Progress, Files: mediaFiles(s.Media)})
	if err != nil {
		return res, err
	}
	// The import loaded everything at once; refresh the planner's statistics (adapters without it skip this).
	if err := store.Analyze(ctx, s.DB); err != nil {
		return res, fmt.Errorf("import: analyze: %w", err)
	}
	return res, nil
}

// importedMapper purges every cache after an import.
func importedMapper(topic string, _ json.RawMessage) []string {
	if topic == exportfmt.ImportedTopic {
		return []string{cachetag.All}
	}
	return nil
}
