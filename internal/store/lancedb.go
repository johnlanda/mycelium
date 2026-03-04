package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/apache/arrow/go/v17/arrow"
	"github.com/apache/arrow/go/v17/arrow/array"
	"github.com/apache/arrow/go/v17/arrow/memory"
	"github.com/lancedb/lancedb-go/pkg/contracts"
	"github.com/lancedb/lancedb-go/pkg/lancedb"
)

const tableName = "chunks"

// LanceDBStore implements Store backed by an embedded LanceDB database.
type LanceDBStore struct {
	conn       contracts.IConnection
	table      contracts.ITable
	dimensions int
}

// DefaultStorePath returns the default LanceDB store directory,
// respecting MYCELIUM_STORE_DIR if set.
func DefaultStorePath() string {
	if p := os.Getenv("MYCELIUM_STORE_DIR"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".mycelium", "store")
}

// NewLanceDBStore opens or creates a LanceDB database at dbPath.
// If dimensions > 0 and the chunks table doesn't exist, it is created.
// If dimensions == 0 and the table doesn't exist, an error is returned.
func NewLanceDBStore(ctx context.Context, dbPath string, dimensions int) (*LanceDBStore, error) {
	if err := os.MkdirAll(dbPath, 0o755); err != nil {
		return nil, fmt.Errorf("create store directory: %w", err)
	}

	conn, err := lancedb.Connect(ctx, dbPath, nil)
	if err != nil {
		return nil, fmt.Errorf("connect lancedb: %w", err)
	}

	ls := &LanceDBStore{
		conn:       conn,
		dimensions: dimensions,
	}

	// Check if the table already exists.
	tables, err := conn.TableNames(ctx)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("list tables: %w", err)
	}
	var tableExists bool
	for _, t := range tables {
		if t == tableName {
			tableExists = true
			break
		}
	}

	if tableExists {
		table, openErr := conn.OpenTable(ctx, tableName)
		if openErr != nil {
			conn.Close()
			return nil, fmt.Errorf("open table: %w", openErr)
		}
		ls.table = table
		return ls, nil
	}

	// Table doesn't exist — create if we know dimensions.
	if dimensions <= 0 {
		conn.Close()
		return nil, fmt.Errorf("store table %q does not exist and dimensions not specified", tableName)
	}

	schema, err := lancedb.NewSchemaBuilder().
		AddStringField("text", false).
		AddStringField("breadcrumb", false).
		AddStringField("chunk_type", false).
		AddInt32Field("chunk_index", false).
		AddStringField("path", false).
		AddStringField("source", false).
		AddStringField("source_version", false).
		AddStringField("store_key", false).
		AddStringField("language", false).
		AddVectorField("vector", dimensions, contracts.VectorDataTypeFloat32, false).
		Build()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("build schema: %w", err)
	}

	table, err := conn.CreateTable(ctx, tableName, schema)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("create table: %w", err)
	}
	ls.table = table
	return ls, nil
}

// Upsert deletes any existing chunks for storeKey, then batch-inserts the new chunks.
func (ls *LanceDBStore) Upsert(ctx context.Context, storeKey string, chunks []StoredChunk) error {
	if err := ls.Delete(ctx, storeKey); err != nil {
		return fmt.Errorf("upsert delete existing: %w", err)
	}

	if len(chunks) == 0 {
		return nil
	}

	record, err := ls.buildRecord(chunks, storeKey)
	if err != nil {
		return fmt.Errorf("build arrow record: %w", err)
	}
	defer record.Release()

	if err := ls.table.Add(ctx, record, nil); err != nil {
		return fmt.Errorf("add records: %w", err)
	}
	return nil
}

// Search performs a vector similarity search with optional filters.
func (ls *LanceDBStore) Search(ctx context.Context, query []float32, opts SearchOpts) ([]SearchResult, error) {
	topK := opts.TopK
	if topK <= 0 {
		topK = 10
	}

	count, err := ls.table.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count rows: %w", err)
	}
	if count == 0 {
		return nil, nil
	}

	filter := buildSQLFilter(opts)

	var rows []map[string]interface{}
	if filter != "" {
		rows, err = ls.table.VectorSearchWithFilter(ctx, "vector", query, topK, filter)
	} else {
		rows, err = ls.table.VectorSearch(ctx, "vector", query, topK)
	}
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	return parseSearchRows(rows), nil
}

// Delete removes all chunks with the given storeKey.
func (ls *LanceDBStore) Delete(ctx context.Context, storeKey string) error {
	count, err := ls.table.Count(ctx)
	if err != nil {
		return fmt.Errorf("count rows: %w", err)
	}
	if count == 0 {
		return nil
	}

	if err := ls.table.Delete(ctx, fmt.Sprintf("store_key = '%s'", escapeSQLString(storeKey))); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	return nil
}

// HasKey returns true if any chunks exist with the given storeKey.
func (ls *LanceDBStore) HasKey(ctx context.Context, storeKey string) (bool, error) {
	count, err := ls.table.Count(ctx)
	if err != nil {
		return false, fmt.Errorf("count rows: %w", err)
	}
	if count == 0 {
		return false, nil
	}

	one := 1
	results, err := ls.table.Select(ctx, contracts.QueryConfig{
		Columns: []string{"store_key"},
		Where:   fmt.Sprintf("store_key = '%s'", escapeSQLString(storeKey)),
		Limit:   &one,
	})
	if err != nil {
		return false, fmt.Errorf("has key: %w", err)
	}
	return len(results) > 0, nil
}

// ListSources returns information about all indexed sources.
func (ls *LanceDBStore) ListSources(ctx context.Context) ([]SourceInfo, error) {
	count, err := ls.table.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count rows: %w", err)
	}
	if count == 0 {
		return nil, nil
	}

	rows, err := ls.table.SelectWithColumns(ctx, []string{"source", "source_version", "store_key"})
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}

	type sourceKey struct {
		source        string
		sourceVersion string
		storeKey      string
	}
	counts := make(map[sourceKey]int)
	for _, row := range rows {
		sk := sourceKey{
			source:        mapString(row, "source"),
			sourceVersion: mapString(row, "source_version"),
			storeKey:      mapString(row, "store_key"),
		}
		counts[sk]++
	}

	results := make([]SourceInfo, 0, len(counts))
	for sk, c := range counts {
		results = append(results, SourceInfo{
			Source:        sk.source,
			SourceVersion: sk.sourceVersion,
			StoreKey:      sk.storeKey,
			ChunkCount:   c,
		})
	}
	return results, nil
}

// Close releases the table and connection resources.
func (ls *LanceDBStore) Close() error {
	if ls.table != nil {
		ls.table.Close()
	}
	if ls.conn != nil {
		return ls.conn.Close()
	}
	return nil
}

// buildRecord creates an Arrow record from the given chunks for batch insertion.
func (ls *LanceDBStore) buildRecord(chunks []StoredChunk, storeKey string) (arrow.Record, error) {
	alloc := memory.NewGoAllocator()
	n := len(chunks)

	textB := array.NewStringBuilder(alloc)
	defer textB.Release()
	breadcrumbB := array.NewStringBuilder(alloc)
	defer breadcrumbB.Release()
	chunkTypeB := array.NewStringBuilder(alloc)
	defer chunkTypeB.Release()
	chunkIndexB := array.NewInt32Builder(alloc)
	defer chunkIndexB.Release()
	pathB := array.NewStringBuilder(alloc)
	defer pathB.Release()
	sourceB := array.NewStringBuilder(alloc)
	defer sourceB.Release()
	sourceVersionB := array.NewStringBuilder(alloc)
	defer sourceVersionB.Release()
	storeKeyB := array.NewStringBuilder(alloc)
	defer storeKeyB.Release()
	languageB := array.NewStringBuilder(alloc)
	defer languageB.Release()

	vecB := array.NewFixedSizeListBuilder(alloc, int32(ls.dimensions), arrow.PrimitiveTypes.Float32)
	defer vecB.Release()
	vecValB := vecB.ValueBuilder().(*array.Float32Builder)

	for _, c := range chunks {
		textB.Append(c.Text)
		breadcrumbB.Append(c.Breadcrumb)
		chunkTypeB.Append(c.ChunkType)
		chunkIndexB.Append(int32(c.ChunkIndex))
		pathB.Append(c.Path)
		sourceB.Append(c.Source)
		sourceVersionB.Append(c.SourceVersion)
		storeKeyB.Append(storeKey)
		languageB.Append(c.Language)

		vecB.Append(true)
		for _, v := range c.Vector {
			vecValB.Append(v)
		}
		// Pad if vector is shorter than expected dimensions.
		for j := len(c.Vector); j < ls.dimensions; j++ {
			vecValB.Append(0)
		}
	}

	vecType := arrow.FixedSizeListOf(int32(ls.dimensions), arrow.PrimitiveTypes.Float32)
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "text", Type: arrow.BinaryTypes.String},
		{Name: "breadcrumb", Type: arrow.BinaryTypes.String},
		{Name: "chunk_type", Type: arrow.BinaryTypes.String},
		{Name: "chunk_index", Type: arrow.PrimitiveTypes.Int32},
		{Name: "path", Type: arrow.BinaryTypes.String},
		{Name: "source", Type: arrow.BinaryTypes.String},
		{Name: "source_version", Type: arrow.BinaryTypes.String},
		{Name: "store_key", Type: arrow.BinaryTypes.String},
		{Name: "language", Type: arrow.BinaryTypes.String},
		{Name: "vector", Type: vecType},
	}, nil)

	cols := []arrow.Array{
		textB.NewArray(),
		breadcrumbB.NewArray(),
		chunkTypeB.NewArray(),
		chunkIndexB.NewArray(),
		pathB.NewArray(),
		sourceB.NewArray(),
		sourceVersionB.NewArray(),
		storeKeyB.NewArray(),
		languageB.NewArray(),
		vecB.NewArray(),
	}
	defer func() {
		for _, c := range cols {
			c.Release()
		}
	}()

	return array.NewRecord(schema, cols, int64(n)), nil
}

// buildSQLFilter constructs a SQL WHERE clause from search options.
func buildSQLFilter(opts SearchOpts) string {
	var conditions []string
	if opts.Source != "" {
		conditions = append(conditions, fmt.Sprintf("source = '%s'", escapeSQLString(opts.Source)))
	}
	if opts.ChunkType != "" {
		conditions = append(conditions, fmt.Sprintf("chunk_type = '%s'", escapeSQLString(opts.ChunkType)))
	}
	if opts.Language != "" {
		conditions = append(conditions, fmt.Sprintf("language = '%s'", escapeSQLString(opts.Language)))
	}
	return strings.Join(conditions, " AND ")
}

func escapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// parseSearchRows converts LanceDB search result maps into SearchResults.
func parseSearchRows(rows []map[string]interface{}) []SearchResult {
	results := make([]SearchResult, 0, len(rows))
	for _, m := range rows {
		chunk := StoredChunk{
			Text:          mapString(m, "text"),
			Breadcrumb:    mapString(m, "breadcrumb"),
			ChunkType:     mapString(m, "chunk_type"),
			ChunkIndex:    mapInt(m, "chunk_index"),
			Path:          mapString(m, "path"),
			Source:        mapString(m, "source"),
			SourceVersion: mapString(m, "source_version"),
			StoreKey:      mapString(m, "store_key"),
			Language:      mapString(m, "language"),
		}

		var distance float32
		if d, ok := m["_distance"]; ok {
			distance = toFloat32(d)
		}

		results = append(results, SearchResult{
			Chunk: chunk,
			Score: 1 - distance,
		})
	}
	return results
}

func mapString(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

func mapInt(m map[string]interface{}, key string) int {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	default:
		return 0
	}
}

func toFloat32(v interface{}) float32 {
	switch n := v.(type) {
	case float32:
		return n
	case float64:
		return float32(n)
	default:
		return 0
	}
}
