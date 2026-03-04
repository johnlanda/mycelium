package chunker

import (
	"strings"
	"testing"
)

func TestCodeChunker_Go(t *testing.T) {
	cc := NewCodeChunker(Options{})
	content := `package main

import "fmt"

func main() {
	fmt.Println("hello")
}

type Server struct {
	Addr string
}

func (s *Server) Start() error {
	return nil
}
`
	chunks, err := cc.Chunk([]byte(content), ChunkMetadata{
		Source:        "test/repo",
		SourceVersion: "v1.0.0",
		Path:          "main.go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}

	// Check that breadcrumbs include function/type names.
	allBreadcrumbs := joinBreadcrumbs(chunks)
	for _, want := range []string{"main", "Server"} {
		if !strings.Contains(allBreadcrumbs, want) {
			t.Errorf("breadcrumbs %q should contain %q", allBreadcrumbs, want)
		}
	}

	// All chunks should be code type.
	for i, c := range chunks {
		if c.ChunkType != ChunkTypeCode {
			t.Errorf("chunk[%d] ChunkType = %q, want %q", i, c.ChunkType, ChunkTypeCode)
		}
	}
}

func TestCodeChunker_Python(t *testing.T) {
	cc := NewCodeChunker(Options{})
	content := `import os

def greet(name):
    print(f"Hello, {name}")

class MyClass:
    def __init__(self):
        self.value = 42

    def method(self):
        return self.value
`
	chunks, err := cc.Chunk([]byte(content), ChunkMetadata{Path: "app.py"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}

	allBreadcrumbs := joinBreadcrumbs(chunks)
	for _, want := range []string{"greet", "MyClass"} {
		if !strings.Contains(allBreadcrumbs, want) {
			t.Errorf("breadcrumbs %q should contain %q", allBreadcrumbs, want)
		}
	}
}

func TestCodeChunker_TypeScript(t *testing.T) {
	cc := NewCodeChunker(Options{})
	content := `interface Config {
  port: number;
  host: string;
}

class Server {
  constructor(private config: Config) {}
  start(): void {}
}

export function createServer(config: Config): Server {
  return new Server(config);
}
`
	chunks, err := cc.Chunk([]byte(content), ChunkMetadata{Path: "server.ts"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}

	allBreadcrumbs := joinBreadcrumbs(chunks)
	for _, want := range []string{"Config", "Server"} {
		if !strings.Contains(allBreadcrumbs, want) {
			t.Errorf("breadcrumbs %q should contain %q", allBreadcrumbs, want)
		}
	}
}

func TestCodeChunker_JavaScript(t *testing.T) {
	cc := NewCodeChunker(Options{})
	content := `function add(a, b) {
  return a + b;
}

class Calculator {
  constructor() {
    this.result = 0;
  }
}

const multiply = (a, b) => a * b;
`
	chunks, err := cc.Chunk([]byte(content), ChunkMetadata{Path: "calc.js"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}

	allBreadcrumbs := joinBreadcrumbs(chunks)
	for _, want := range []string{"add", "Calculator"} {
		if !strings.Contains(allBreadcrumbs, want) {
			t.Errorf("breadcrumbs %q should contain %q", allBreadcrumbs, want)
		}
	}
}

func TestCodeChunker_Java(t *testing.T) {
	cc := NewCodeChunker(Options{})
	content := `package com.example;

public class Greeter {
    private String name;

    public Greeter(String name) {
        this.name = name;
    }

    public String greet() {
        return "Hello, " + name;
    }
}
`
	chunks, err := cc.Chunk([]byte(content), ChunkMetadata{Path: "Greeter.java"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}

	allBreadcrumbs := joinBreadcrumbs(chunks)
	if !strings.Contains(allBreadcrumbs, "Greeter") {
		t.Errorf("breadcrumbs %q should contain %q", allBreadcrumbs, "Greeter")
	}
}

func TestCodeChunker_Rust(t *testing.T) {
	cc := NewCodeChunker(Options{})
	content := `use std::fmt;

struct Point {
    x: f64,
    y: f64,
}

impl Point {
    fn new(x: f64, y: f64) -> Self {
        Point { x, y }
    }
}

fn distance(a: &Point, b: &Point) -> f64 {
    ((a.x - b.x).powi(2) + (a.y - b.y).powi(2)).sqrt()
}
`
	chunks, err := cc.Chunk([]byte(content), ChunkMetadata{Path: "geometry.rs"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}

	allBreadcrumbs := joinBreadcrumbs(chunks)
	for _, want := range []string{"Point", "impl Point", "distance"} {
		if !strings.Contains(allBreadcrumbs, want) {
			t.Errorf("breadcrumbs %q should contain %q", allBreadcrumbs, want)
		}
	}
}

func TestCodeChunker_UnsupportedExtension(t *testing.T) {
	cc := NewCodeChunker(Options{})
	content := `#include <stdio.h>

int main() {
    printf("hello\n");
    return 0;
}
`
	chunks, err := cc.Chunk([]byte(content), ChunkMetadata{Path: "main.c"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected fallback to produce chunks")
	}
	// Fallback LineChunker uses file path as breadcrumb.
	if chunks[0].Breadcrumb != "main.c" {
		t.Errorf("expected fallback breadcrumb %q, got %q", "main.c", chunks[0].Breadcrumb)
	}
}

func TestCodeChunker_EmptyInput(t *testing.T) {
	cc := NewCodeChunker(Options{})
	tests := []struct {
		name    string
		content string
	}{
		{"empty string", ""},
		{"whitespace only", "   \n\n  \t  \n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks, err := cc.Chunk([]byte(tt.content), ChunkMetadata{Path: "main.go"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if chunks != nil {
				t.Fatalf("expected nil, got %d chunks", len(chunks))
			}
		})
	}
}

func TestCodeChunker_OversizedFunction(t *testing.T) {
	// Create a function with many lines to exceed MaxSize.
	cc := NewCodeChunker(Options{TargetSize: 20, MinSize: 10, MaxSize: 30})
	var lines []string
	lines = append(lines, "func bigFunc() {")
	for i := 0; i < 20; i++ {
		lines = append(lines, "\tx := doSomething(a, b, c, d, e, f, g, h, i, j)")
	}
	lines = append(lines, "}")
	bigFunc := strings.Join(lines, "\n")

	content := "package main\n\n" + bigFunc + "\n\nfunc small() {}\n"
	chunks, err := cc.Chunk([]byte(content), ChunkMetadata{Path: "big.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The oversized function should be kept whole.
	found := false
	for _, c := range chunks {
		if strings.Contains(c.Text, "bigFunc") {
			found = true
			if !strings.Contains(c.Text, "doSomething") {
				t.Error("oversized function should be kept whole, not split")
			}
		}
	}
	if !found {
		t.Error("bigFunc should appear in chunks")
	}
}

func TestCodeChunker_SmallDeclarationsMerged(t *testing.T) {
	// With a large TargetSize, small adjacent declarations should be merged.
	cc := NewCodeChunker(Options{TargetSize: 2000, MinSize: 100, MaxSize: 3000})
	content := `package main

const A = 1

const B = 2

const C = 3

var X = "hello"
`
	chunks, err := cc.Chunk([]byte(content), ChunkMetadata{Path: "consts.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 1 {
		t.Errorf("expected 1 merged chunk, got %d", len(chunks))
	}
}

func TestCodeChunker_MetadataPropagation(t *testing.T) {
	cc := NewCodeChunker(Options{})
	meta := ChunkMetadata{
		Source:        "github.com/foo/bar",
		SourceVersion: "v2.3.4",
		Path:          "pkg/util.go",
	}
	content := "package util\n\nfunc Helper() {}\n"
	chunks, err := cc.Chunk([]byte(content), meta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, c := range chunks {
		if c.Source != meta.Source {
			t.Errorf("chunk[%d] Source = %q, want %q", i, c.Source, meta.Source)
		}
		if c.SourceVersion != meta.SourceVersion {
			t.Errorf("chunk[%d] SourceVersion = %q, want %q", i, c.SourceVersion, meta.SourceVersion)
		}
		if c.Path != meta.Path {
			t.Errorf("chunk[%d] Path = %q, want %q", i, c.Path, meta.Path)
		}
		if c.ChunkType != ChunkTypeCode {
			t.Errorf("chunk[%d] ChunkType = %q, want %q", i, c.ChunkType, ChunkTypeCode)
		}
	}
}

func TestCodeChunker_SequentialChunkIndex(t *testing.T) {
	cc := NewCodeChunker(Options{TargetSize: 20, MinSize: 10, MaxSize: 50})
	content := `package main

func a() {}

func b() {}

func c() {}

func d() {}
`
	chunks, err := cc.Chunk([]byte(content), ChunkMetadata{Path: "multi.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, c := range chunks {
		if c.ChunkIndex != i {
			t.Errorf("chunk[%d] ChunkIndex = %d, want %d", i, c.ChunkIndex, i)
		}
	}
}

func TestCodeChunker_GoMethodBreadcrumb(t *testing.T) {
	cc := NewCodeChunker(Options{})
	content := `package main

func (s *Server) Start() error {
	return nil
}
`
	chunks, err := cc.Chunk([]byte(content), ChunkMetadata{Path: "server.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, c := range chunks {
		if strings.Contains(c.Breadcrumb, "Server.Start") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected breadcrumb containing 'Server.Start', got breadcrumbs: %s", joinBreadcrumbs(chunks))
	}
}

func joinBreadcrumbs(chunks []Chunk) string {
	var parts []string
	for _, c := range chunks {
		parts = append(parts, c.Breadcrumb)
	}
	return strings.Join(parts, ", ")
}
