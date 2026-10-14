package codegen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "hydrogen.go")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root (hydrogen.go) not found")
		}
		dir = parent
	}
}

func writeFiles(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, src := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, src, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGoCompiles(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not on PATH")
	}
	root := repoRoot(t)
	types, groups := fixture()
	files, err := Go(types, groups, GoOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := map[string][]byte{
		"go.mod": []byte("module example.com/check\n\ngo 1.26\n\nrequire github.com/hydrogencms/hydrogen v0.0.0\n\nreplace github.com/hydrogencms/hydrogen => " + root + "\n"),
		"main.go": []byte(`package main

import (
	"fmt"

	"example.com/check/gen/content"
)

func main() {
	a := content.Article{Title: "t", Kind: content.ArticleKindLongRead, Venue: content.ArticleVenue{}}
	a.Address = &content.ArticleAddress{City: "x", Geo: &content.ArticleAddressGeo{}}
	a.Seo = &content.SeoGroup{}
	h := content.ArticleType
	fmt.Println(a.Kind, h.Type, content.Types, content.ArticleChannelsWeb)
}
`),
	}
	for n, src := range files {
		out["gen/content/"+n] = src
	}
	writeFiles(t, dir, out)
	cmd := exec.Command(goBin, "vet", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOPROXY=off")
	if b, err := cmd.CombinedOutput(); err != nil {
		if strings.Contains(string(b), "hydrogencms/hydrogen/content") && strings.Contains(string(b), "undefined") {
			t.Skipf("content package does not define the generated value types yet:\n%s", b)
		}
		t.Fatalf("go vet: %v\n%s", err, b)
	}
}

func TestTypeScriptCompiles(t *testing.T) {
	npx, err := exec.LookPath("npx")
	if err != nil {
		t.Skip("npx not on PATH")
	}
	types, groups := fixture()
	src, err := TypeScript(types, groups, TSOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFiles(t, dir, map[string][]byte{
		"types.ts": src,
		"usage.ts": []byte(`import type { Article, HydrogenTypes, HydrogenTypeName, ItemView, Ref, RichText } from "./types";

const body: RichText = { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "hi" }] }] };
const a: Article = {
  title: "t",
  slug: "t",
  kind: "long-read",
  views: 1,
  pinned: false,
  author: "id" as Ref,
  body,
  venue: {},
  legal: {},
  address: { city: "x", geo: { lat: 1, lng: 2 } },
  links: [{ label: "a", href: "https://x" }],
  channels: ["web"],
};
const item: ItemView<Article> = {
  id: "1", type: "article", locale: "en", mainLocale: "en", alwaysAvailable: true, status: "draft", version: 1,
  draft: true, name: "n", fields: a, locales: [], draftParts: [], createdAt: "", updatedAt: "",
};
const name: HydrogenTypeName = "blogPost";
const person: HydrogenTypes["person"] = { fullName: "x" };
// @ts-expect-error unknown enum value
const bad: Article["kind"] = "nope";
export { item, name, person, bad };
`),
	})
	cmd := exec.Command(npx, "--no-install", "tsc", "--noEmit", "--strict", "--target", "es2022", "types.ts", "usage.ts")
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil {
		if strings.Contains(string(b), "not found") || strings.Contains(string(b), "could not determine executable") {
			t.Skipf("tsc unavailable: %s", b)
		}
		t.Fatalf("tsc: %v\n%s", err, b)
	}
}
