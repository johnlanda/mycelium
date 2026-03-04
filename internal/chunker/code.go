package chunker

import (
	"path/filepath"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// CodeChunker splits code files along AST boundaries using tree-sitter.
// For unsupported languages or parse errors it falls back to the LineChunker.
type CodeChunker struct {
	Options     Options
	lineChunker *LineChunker
}

// NewCodeChunker returns a code chunker with the given options.
func NewCodeChunker(opts Options) *CodeChunker {
	d := DefaultOptions()
	if opts.TargetSize == 0 {
		opts.TargetSize = d.TargetSize
	}
	if opts.MinSize == 0 {
		opts.MinSize = d.MinSize
	}
	if opts.MaxSize == 0 {
		opts.MaxSize = d.MaxSize
	}
	return &CodeChunker{
		Options:     opts,
		lineChunker: NewLineChunker(opts),
	}
}

// astNode is an intermediate representation of a top-level code construct.
type astNode struct {
	text       string
	breadcrumb string
}

// Chunk implements the Chunker interface for code content.
func (cc *CodeChunker) Chunk(content []byte, metadata ChunkMetadata) ([]Chunk, error) {
	text := string(content)
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}

	ext := strings.ToLower(filepath.Ext(metadata.Path))
	cfg, ok := languageForExt(ext)
	if !ok {
		return cc.lineChunker.Chunk(content, metadata)
	}

	parser := tree_sitter.NewParser()
	defer parser.Close()

	if err := parser.SetLanguage(cfg.lang); err != nil {
		return cc.lineChunker.Chunk(content, metadata)
	}

	tree := parser.Parse(content, nil)
	if tree == nil {
		return cc.lineChunker.Chunk(content, metadata)
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		return cc.lineChunker.Chunk(content, metadata)
	}

	nodes, preamble := cc.extractNodes(root, content, cfg, metadata.Path)
	if len(nodes) == 0 {
		// No boundary nodes found; fall back to line chunker.
		return cc.lineChunker.Chunk(content, metadata)
	}

	grouped := cc.groupNodes(nodes, preamble)

	chunks := make([]Chunk, len(grouped))
	for i, g := range grouped {
		chunks[i] = Chunk{
			Text:          g.text,
			Breadcrumb:    g.breadcrumb,
			ChunkType:     ChunkTypeCode,
			ChunkIndex:    i,
			Path:          metadata.Path,
			Source:        metadata.Source,
			SourceVersion: metadata.SourceVersion,
		}
	}
	return chunks, nil
}

// extractNodes walks the root's direct children and separates boundary nodes
// from preamble text (imports, package declarations, comments, etc.).
func (cc *CodeChunker) extractNodes(root *tree_sitter.Node, source []byte, cfg languageConfig, path string) ([]astNode, string) {
	cursor := root.Walk()
	defer cursor.Close()

	var nodes []astNode
	var preamble strings.Builder

	for _, child := range root.Children(cursor) {
		kind := child.Kind()
		text := child.Utf8Text(source)

		if cfg.chunkNodeTypes[kind] {
			bc := buildBreadcrumb(&child, source, path)
			nodes = append(nodes, astNode{text: text, breadcrumb: bc})
		} else {
			// Non-boundary text (imports, package, comments, etc.)
			if preamble.Len() > 0 {
				preamble.WriteString("\n\n")
			}
			preamble.WriteString(text)
		}
	}

	return nodes, preamble.String()
}

// groupNodes merges small adjacent AST nodes toward TargetSize.
// A non-empty preamble is prepended to the first chunk.
func (cc *CodeChunker) groupNodes(nodes []astNode, preamble string) []astNode {
	var result []astNode
	var curText strings.Builder
	var curBreadcrumbs []string

	flush := func() {
		if curText.Len() == 0 {
			return
		}
		result = append(result, astNode{
			text:       curText.String(),
			breadcrumb: strings.Join(curBreadcrumbs, " | "),
		})
		curText.Reset()
		curBreadcrumbs = nil
	}

	// If there's preamble text, start the buffer with it.
	if strings.TrimSpace(preamble) != "" {
		curText.WriteString(preamble)
	}

	for _, node := range nodes {
		nodeTokens := estimateTokens(node.text)

		// If adding this node would exceed TargetSize and the buffer is non-empty, flush first.
		if curText.Len() > 0 {
			candidate := curText.String() + "\n\n" + node.text
			if estimateTokens(candidate) > cc.Options.TargetSize {
				flush()
			}
		}

		// If the node alone exceeds MaxSize, flush any buffer and emit as-is.
		if nodeTokens > cc.Options.MaxSize {
			flush()
			result = append(result, astNode{
				text:       node.text,
				breadcrumb: node.breadcrumb,
			})
			continue
		}

		if curText.Len() > 0 {
			curText.WriteString("\n\n")
		}
		curText.WriteString(node.text)
		curBreadcrumbs = append(curBreadcrumbs, node.breadcrumb)
	}

	flush()
	return result
}

// buildBreadcrumb extracts a human-readable name from an AST node.
func buildBreadcrumb(node *tree_sitter.Node, source []byte, fallback string) string {
	kind := node.Kind()

	switch kind {
	// Go
	case "function_declaration":
		if name := node.ChildByFieldName("name"); name != nil {
			return name.Utf8Text(source)
		}
	case "method_declaration":
		return goMethodBreadcrumb(node, source)
	case "type_declaration":
		return goTypeBreadcrumb(node, source)
	case "const_declaration":
		return "const"
	case "var_declaration":
		return "var"

	// Python
	case "function_definition":
		if name := node.ChildByFieldName("name"); name != nil {
			return name.Utf8Text(source)
		}
	case "class_definition":
		if name := node.ChildByFieldName("name"); name != nil {
			return name.Utf8Text(source)
		}
	case "decorated_definition":
		// Recurse into the inner definition.
		if def := node.ChildByFieldName("definition"); def != nil {
			return buildBreadcrumb(def, source, fallback)
		}

	// TypeScript / JavaScript
	case "class_declaration":
		if name := node.ChildByFieldName("name"); name != nil {
			return name.Utf8Text(source)
		}
	case "interface_declaration":
		if name := node.ChildByFieldName("name"); name != nil {
			return name.Utf8Text(source)
		}
	case "type_alias_declaration":
		if name := node.ChildByFieldName("name"); name != nil {
			return name.Utf8Text(source)
		}
	case "enum_declaration":
		if name := node.ChildByFieldName("name"); name != nil {
			return name.Utf8Text(source)
		}
	case "export_statement":
		return exportBreadcrumb(node, source, fallback)
	case "lexical_declaration", "variable_declaration":
		return varDeclBreadcrumb(node, source, fallback)

	// Java
	// class_declaration, interface_declaration, enum_declaration handled above

	// Rust
	case "function_item":
		if name := node.ChildByFieldName("name"); name != nil {
			return name.Utf8Text(source)
		}
	case "struct_item":
		if name := node.ChildByFieldName("name"); name != nil {
			return name.Utf8Text(source)
		}
	case "enum_item":
		if name := node.ChildByFieldName("name"); name != nil {
			return name.Utf8Text(source)
		}
	case "trait_item":
		if name := node.ChildByFieldName("name"); name != nil {
			return "trait " + name.Utf8Text(source)
		}
	case "impl_item":
		return rustImplBreadcrumb(node, source)
	case "const_item":
		if name := node.ChildByFieldName("name"); name != nil {
			return name.Utf8Text(source)
		}
	case "static_item":
		if name := node.ChildByFieldName("name"); name != nil {
			return name.Utf8Text(source)
		}
	case "type_item":
		if name := node.ChildByFieldName("name"); name != nil {
			return name.Utf8Text(source)
		}
	}

	return fallback
}

// goMethodBreadcrumb builds "ReceiverType.MethodName" for Go methods.
func goMethodBreadcrumb(node *tree_sitter.Node, source []byte) string {
	methodName := ""
	if name := node.ChildByFieldName("name"); name != nil {
		methodName = name.Utf8Text(source)
	}

	receiverType := ""
	if params := node.ChildByFieldName("receiver"); params != nil {
		// Walk the parameter list to find the type identifier.
		cursor := params.Walk()
		defer cursor.Close()
		for _, child := range params.NamedChildren(cursor) {
			if child.Kind() == "parameter_declaration" {
				// The type is the last named child (could be pointer_type or type_identifier).
				typeCursor := child.Walk()
				defer typeCursor.Close()
				for _, tc := range child.NamedChildren(typeCursor) {
					if tc.Kind() == "type_identifier" {
						receiverType = tc.Utf8Text(source)
					} else if tc.Kind() == "pointer_type" {
						// Extract type from *Type.
						inner := tc.ChildByFieldName("type")
						if inner == nil {
							// Fallback: use named child.
							if tc.NamedChildCount() > 0 {
								inner = tc.NamedChild(0)
							}
						}
						if inner != nil {
							receiverType = inner.Utf8Text(source)
						}
					}
				}
			}
		}
	}

	if receiverType != "" && methodName != "" {
		return receiverType + "." + methodName
	}
	if methodName != "" {
		return methodName
	}
	return ""
}

// goTypeBreadcrumb extracts the type name from a Go type declaration.
func goTypeBreadcrumb(node *tree_sitter.Node, source []byte) string {
	// type_declaration > type_spec > type_identifier
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		if child.Kind() == "type_spec" {
			if name := child.ChildByFieldName("name"); name != nil {
				return name.Utf8Text(source)
			}
		}
	}
	return "type"
}

// exportBreadcrumb recurses into the exported declaration.
func exportBreadcrumb(node *tree_sitter.Node, source []byte, fallback string) string {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		kind := child.Kind()
		switch kind {
		case "function_declaration", "class_declaration", "interface_declaration",
			"type_alias_declaration", "enum_declaration", "lexical_declaration",
			"variable_declaration":
			return buildBreadcrumb(&child, source, fallback)
		}
	}
	return "export"
}

// varDeclBreadcrumb extracts the first variable name from a declaration.
func varDeclBreadcrumb(node *tree_sitter.Node, source []byte, fallback string) string {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		if child.Kind() == "variable_declarator" {
			if name := child.ChildByFieldName("name"); name != nil {
				return name.Utf8Text(source)
			}
		}
	}
	return fallback
}

// rustImplBreadcrumb builds "impl TypeName" or "impl Trait for TypeName".
func rustImplBreadcrumb(node *tree_sitter.Node, source []byte) string {
	if typeName := node.ChildByFieldName("type"); typeName != nil {
		name := typeName.Utf8Text(source)
		if trait := node.ChildByFieldName("trait"); trait != nil {
			return "impl " + trait.Utf8Text(source) + " for " + name
		}
		return "impl " + name
	}
	return "impl"
}
