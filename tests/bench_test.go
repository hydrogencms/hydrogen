package tests

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/content"
)

func benchSchema() string {
	var sb strings.Builder
	sb.WriteString("types:\n  doc:\n    name: \"{f0}\"\n    fields:\n")
	for i := range 20 {
		switch {
		case i < 8:
			fmt.Fprintf(&sb, "      f%d: { type: string, translatable: true, max: 200 }\n", i)
		case i < 12:
			fmt.Fprintf(&sb, "      f%d: { type: number, index: true }\n", i)
		case i < 16:
			fmt.Fprintf(&sb, "      f%d: { type: text }\n", i)
		case i < 18:
			fmt.Fprintf(&sb, "      f%d: { type: boolean }\n", i)
		default:
			fmt.Fprintf(&sb, "      f%d: { type: list, of: { type: string }, index: true }\n", i)
		}
	}
	return sb.String()
}

func benchFields(n int) map[string]any {
	f := map[string]any{}
	for i := range 20 {
		switch {
		case i < 8:
			f[fmt.Sprintf("f%d", i)] = fmt.Sprintf("value %d of round %d", i, n)
		case i < 12:
			f[fmt.Sprintf("f%d", i)] = float64(n + i)
		case i < 16:
			f[fmt.Sprintf("f%d", i)] = strings.Repeat("lorem ipsum ", 10)
		case i < 18:
			f[fmt.Sprintf("f%d", i)] = n%2 == 0
		default:
			f[fmt.Sprintf("f%d", i)] = []any{"a", "b", fmt.Sprint(n)}
		}
	}
	return f
}

func benchEnv(b *testing.B) *env {
	clk := clock.NewManual(time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC))
	return newEnv(b, openSQLite(b, clk), clk, envConfig{schema: benchSchema()})
}

func BenchmarkUpdate(b *testing.B) {
	e := benchEnv(b)
	v, err := e.svc.Create(ctx, content.CreateInput{Type: "doc", Fields: benchFields(0)})
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := range b.N {
		if _, err := e.svc.Update(ctx, v.ID, content.UpdateInput{Fields: benchFields(i + 1)}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPublish(b *testing.B) {
	e := benchEnv(b)
	v, err := e.svc.Create(ctx, content.CreateInput{Type: "doc", Fields: benchFields(0)})
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := range b.N {
		b.StopTimer()
		if _, err := e.svc.Update(ctx, v.ID, content.UpdateInput{Fields: benchFields(i + 1)}); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if _, err := e.svc.Publish(ctx, v.ID, content.PublishOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}
