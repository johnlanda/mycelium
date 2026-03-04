package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/filters"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/graphql"
	"github.com/weaviate/weaviate/entities/models"
)

const className = "MyceliumChunk"

// WeaviateStore implements Store backed by a Weaviate instance.
type WeaviateStore struct {
	client *weaviate.Client
}

// NewWeaviateStore connects to a Weaviate instance at host (e.g. "localhost:8080")
// and ensures the MyceliumChunk class exists.
func NewWeaviateStore(ctx context.Context, host string) (*WeaviateStore, error) {
	if host == "" {
		host = "localhost:8080"
	}

	cfg := weaviate.Config{
		Host:   host,
		Scheme: "http",
	}
	client, err := weaviate.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("create weaviate client: %w", err)
	}

	ws := &WeaviateStore{client: client}
	if err := ws.ensureClass(ctx); err != nil {
		return nil, fmt.Errorf("ensure schema class: %w", err)
	}
	return ws, nil
}

func (ws *WeaviateStore) ensureClass(ctx context.Context) error {
	exists, err := ws.client.Schema().ClassExistenceChecker().
		WithClassName(className).Do(ctx)
	if err != nil {
		return fmt.Errorf("check class existence: %w", err)
	}
	if exists {
		return nil
	}

	class := &models.Class{
		Class:      className,
		Vectorizer: "none",
		Properties: []*models.Property{
			{Name: "text", DataType: []string{"text"}},
			{Name: "breadcrumb", DataType: []string{"text"}},
			{Name: "chunkType", DataType: []string{"text"}},
			{Name: "chunkIndex", DataType: []string{"int"}},
			{Name: "path", DataType: []string{"text"}},
			{Name: "source", DataType: []string{"text"}},
			{Name: "sourceVersion", DataType: []string{"text"}},
			{Name: "storeKey", DataType: []string{"text"}},
			{Name: "language", DataType: []string{"text"}},
		},
	}

	if err := ws.client.Schema().ClassCreator().WithClass(class).Do(ctx); err != nil {
		return fmt.Errorf("create class: %w", err)
	}
	return nil
}

// Upsert deletes any existing chunks for storeKey, then batch-inserts the new chunks.
func (ws *WeaviateStore) Upsert(ctx context.Context, storeKey string, chunks []StoredChunk) error {
	if err := ws.Delete(ctx, storeKey); err != nil {
		return fmt.Errorf("upsert delete existing: %w", err)
	}

	if len(chunks) == 0 {
		return nil
	}

	batcher := ws.client.Batch().ObjectsBatcher()
	for _, c := range chunks {
		obj := &models.Object{
			Class: className,
			Properties: map[string]interface{}{
				"text":          c.Text,
				"breadcrumb":    c.Breadcrumb,
				"chunkType":     c.ChunkType,
				"chunkIndex":    c.ChunkIndex,
				"path":          c.Path,
				"source":        c.Source,
				"sourceVersion": c.SourceVersion,
				"storeKey":      storeKey,
				"language":      c.Language,
			},
			Vector: c.Vector,
		}
		batcher.WithObjects(obj)
	}

	resp, err := batcher.Do(ctx)
	if err != nil {
		return fmt.Errorf("batch insert: %w", err)
	}
	for _, r := range resp {
		if r.Result != nil && r.Result.Errors != nil {
			msgs := make([]string, len(r.Result.Errors.Error))
			for i, e := range r.Result.Errors.Error {
				msgs[i] = e.Message
			}
			if len(msgs) > 0 {
				return fmt.Errorf("batch insert errors: %s", strings.Join(msgs, "; "))
			}
		}
	}
	return nil
}

// Search performs a nearVector query with optional filters, returning up to opts.TopK results.
func (ws *WeaviateStore) Search(ctx context.Context, query []float32, opts SearchOpts) ([]SearchResult, error) {
	topK := opts.TopK
	if topK <= 0 {
		topK = 10
	}

	nearVector := (&graphql.NearVectorArgumentBuilder{}).
		WithVector(query)

	fields := []graphql.Field{
		{Name: "text"},
		{Name: "breadcrumb"},
		{Name: "chunkType"},
		{Name: "chunkIndex"},
		{Name: "path"},
		{Name: "source"},
		{Name: "sourceVersion"},
		{Name: "storeKey"},
		{Name: "language"},
		{Name: "_additional", Fields: []graphql.Field{
			{Name: "distance"},
		}},
	}

	builder := ws.client.GraphQL().Get().
		WithClassName(className).
		WithFields(fields...).
		WithNearVector(nearVector).
		WithLimit(topK)

	where := ws.buildWhereFilter(opts)
	if where != nil {
		builder = builder.WithWhere(where)
	}

	resp, err := builder.Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	return ws.parseSearchResults(resp)
}

func (ws *WeaviateStore) buildWhereFilter(opts SearchOpts) *filters.WhereBuilder {
	var clauses []*filters.WhereBuilder

	if opts.Source != "" {
		clauses = append(clauses, filters.Where().
			WithPath([]string{"source"}).
			WithOperator(filters.Equal).
			WithValueText(opts.Source))
	}
	if opts.ChunkType != "" {
		clauses = append(clauses, filters.Where().
			WithPath([]string{"chunkType"}).
			WithOperator(filters.Equal).
			WithValueText(opts.ChunkType))
	}
	if opts.Language != "" {
		clauses = append(clauses, filters.Where().
			WithPath([]string{"language"}).
			WithOperator(filters.Equal).
			WithValueText(opts.Language))
	}

	switch len(clauses) {
	case 0:
		return nil
	case 1:
		return clauses[0]
	default:
		return filters.Where().
			WithOperator(filters.And).
			WithOperands(clauses)
	}
}

func (ws *WeaviateStore) parseSearchResults(resp *models.GraphQLResponse) ([]SearchResult, error) {
	if resp == nil || resp.Data == nil {
		return nil, nil
	}

	getData, ok := resp.Data["Get"]
	if !ok {
		return nil, nil
	}
	getMap, ok := getData.(map[string]interface{})
	if !ok {
		return nil, nil
	}
	classData, ok := getMap[className]
	if !ok {
		return nil, nil
	}
	items, ok := classData.([]interface{})
	if !ok {
		return nil, nil
	}

	results := make([]SearchResult, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		chunk := StoredChunk{
			Text:          stringVal(m, "text"),
			Breadcrumb:    stringVal(m, "breadcrumb"),
			ChunkType:     stringVal(m, "chunkType"),
			ChunkIndex:    intVal(m, "chunkIndex"),
			Path:          stringVal(m, "path"),
			Source:        stringVal(m, "source"),
			SourceVersion: stringVal(m, "sourceVersion"),
			StoreKey:      stringVal(m, "storeKey"),
			Language:      stringVal(m, "language"),
		}

		var distance float32
		if additional, ok := m["_additional"].(map[string]interface{}); ok {
			if d, ok := additional["distance"]; ok {
				distance = float32Val(d)
			}
		}

		results = append(results, SearchResult{
			Chunk: chunk,
			Score: 1 - distance,
		})
	}
	return results, nil
}

// Delete removes all objects with the given storeKey.
func (ws *WeaviateStore) Delete(ctx context.Context, storeKey string) error {
	where := filters.Where().
		WithPath([]string{"storeKey"}).
		WithOperator(filters.Equal).
		WithValueText(storeKey)

	_, err := ws.client.Batch().ObjectsBatchDeleter().
		WithClassName(className).
		WithWhere(where).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("batch delete: %w", err)
	}
	return nil
}

// HasKey returns true if any objects exist with the given storeKey.
func (ws *WeaviateStore) HasKey(ctx context.Context, storeKey string) (bool, error) {
	where := filters.Where().
		WithPath([]string{"storeKey"}).
		WithOperator(filters.Equal).
		WithValueText(storeKey)

	resp, err := ws.client.GraphQL().Aggregate().
		WithClassName(className).
		WithWhere(where).
		WithFields(graphql.Field{
			Name: "meta", Fields: []graphql.Field{{Name: "count"}},
		}).
		Do(ctx)
	if err != nil {
		return false, fmt.Errorf("has key aggregate: %w", err)
	}

	count, err := ws.parseAggregateCount(resp)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (ws *WeaviateStore) parseAggregateCount(resp *models.GraphQLResponse) (int, error) {
	if resp == nil || resp.Data == nil {
		return 0, nil
	}
	aggData, ok := resp.Data["Aggregate"]
	if !ok {
		return 0, nil
	}
	aggMap, ok := aggData.(map[string]interface{})
	if !ok {
		return 0, nil
	}
	classData, ok := aggMap[className]
	if !ok {
		return 0, nil
	}
	items, ok := classData.([]interface{})
	if !ok || len(items) == 0 {
		return 0, nil
	}
	item, ok := items[0].(map[string]interface{})
	if !ok {
		return 0, nil
	}
	meta, ok := item["meta"].(map[string]interface{})
	if !ok {
		return 0, nil
	}
	count, ok := meta["count"]
	if !ok {
		return 0, nil
	}
	return int(float64Val(count)), nil
}

// ListSources returns information about all indexed sources.
func (ws *WeaviateStore) ListSources(ctx context.Context) ([]SourceInfo, error) {
	// Fetch all objects' source, sourceVersion, storeKey fields using cursor pagination.
	type sourceKey struct {
		source        string
		sourceVersion string
		storeKey      string
	}
	counts := make(map[sourceKey]int)

	const pageSize = 100
	var afterID string

	for {
		builder := ws.client.GraphQL().Get().
			WithClassName(className).
			WithFields(
				graphql.Field{Name: "source"},
				graphql.Field{Name: "sourceVersion"},
				graphql.Field{Name: "storeKey"},
				graphql.Field{Name: "_additional", Fields: []graphql.Field{{Name: "id"}}},
			).
			WithLimit(pageSize)

		if afterID != "" {
			builder = builder.WithAfter(afterID)
		}

		resp, err := builder.Do(ctx)
		if err != nil {
			return nil, fmt.Errorf("list sources query: %w", err)
		}

		items := ws.extractGetItems(resp)
		if len(items) == 0 {
			break
		}

		for _, item := range items {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			sk := sourceKey{
				source:        stringVal(m, "source"),
				sourceVersion: stringVal(m, "sourceVersion"),
				storeKey:      stringVal(m, "storeKey"),
			}
			counts[sk]++

			if additional, ok := m["_additional"].(map[string]interface{}); ok {
				if id, ok := additional["id"].(string); ok {
					afterID = id
				}
			}
		}

		if len(items) < pageSize {
			break
		}
	}

	results := make([]SourceInfo, 0, len(counts))
	for sk, count := range counts {
		results = append(results, SourceInfo{
			Source:        sk.source,
			SourceVersion: sk.sourceVersion,
			StoreKey:      sk.storeKey,
			ChunkCount:   count,
		})
	}
	return results, nil
}

func (ws *WeaviateStore) extractGetItems(resp *models.GraphQLResponse) []interface{} {
	if resp == nil || resp.Data == nil {
		return nil
	}
	getData, ok := resp.Data["Get"]
	if !ok {
		return nil
	}
	getMap, ok := getData.(map[string]interface{})
	if !ok {
		return nil
	}
	classData, ok := getMap[className]
	if !ok {
		return nil
	}
	items, ok := classData.([]interface{})
	if !ok {
		return nil
	}
	return items
}

// Close is a no-op for the Weaviate HTTP client but satisfies the Store interface.
func (ws *WeaviateStore) Close() error {
	return nil
}

// Helper functions for safe type assertions from GraphQL response maps.

func stringVal(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func intVal(m map[string]interface{}, key string) int {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

func float32Val(v interface{}) float32 {
	switch n := v.(type) {
	case float64:
		return float32(n)
	case float32:
		return n
	default:
		return 0
	}
}

func float64Val(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	default:
		return 0
	}
}
