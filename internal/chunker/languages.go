package chunker

import (
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// languageConfig holds a tree-sitter grammar and the AST node types that
// represent chunk boundaries for that language.
type languageConfig struct {
	lang           *tree_sitter.Language
	chunkNodeTypes map[string]bool
}

// registry maps file extensions to their language configuration.
var registry map[string]languageConfig

func init() {
	registry = map[string]languageConfig{
		".go": {
			lang: tree_sitter.NewLanguage(tree_sitter_go.Language()),
			chunkNodeTypes: setOf(
				"function_declaration",
				"method_declaration",
				"type_declaration",
				"const_declaration",
				"var_declaration",
			),
		},
		".py": {
			lang: tree_sitter.NewLanguage(tree_sitter_python.Language()),
			chunkNodeTypes: setOf(
				"function_definition",
				"class_definition",
				"decorated_definition",
			),
		},
		".ts": {
			lang: tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript()),
			chunkNodeTypes: tsNodeTypes(),
		},
		".tsx": {
			lang: tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTSX()),
			chunkNodeTypes: tsNodeTypes(),
		},
		".js": {
			lang: tree_sitter.NewLanguage(tree_sitter_javascript.Language()),
			chunkNodeTypes: jsNodeTypes(),
		},
		".jsx": {
			lang: tree_sitter.NewLanguage(tree_sitter_javascript.Language()),
			chunkNodeTypes: jsNodeTypes(),
		},
		".java": {
			lang: tree_sitter.NewLanguage(tree_sitter_java.Language()),
			chunkNodeTypes: setOf(
				"class_declaration",
				"interface_declaration",
				"enum_declaration",
			),
		},
		".rs": {
			lang: tree_sitter.NewLanguage(tree_sitter_rust.Language()),
			chunkNodeTypes: setOf(
				"function_item",
				"struct_item",
				"enum_item",
				"trait_item",
				"impl_item",
				"const_item",
				"static_item",
				"type_item",
			),
		},
	}
}

func tsNodeTypes() map[string]bool {
	return setOf(
		"function_declaration",
		"class_declaration",
		"interface_declaration",
		"type_alias_declaration",
		"enum_declaration",
		"export_statement",
		"lexical_declaration",
	)
}

func jsNodeTypes() map[string]bool {
	return setOf(
		"function_declaration",
		"class_declaration",
		"export_statement",
		"lexical_declaration",
		"variable_declaration",
	)
}

func setOf(kinds ...string) map[string]bool {
	m := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		m[k] = true
	}
	return m
}

// languageForExt returns the language configuration for a file extension.
func languageForExt(ext string) (languageConfig, bool) {
	cfg, ok := registry[ext]
	return cfg, ok
}
