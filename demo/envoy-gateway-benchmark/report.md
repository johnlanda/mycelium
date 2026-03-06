# Benchmark Report: 20260305-152403

## Configuration

- **Model:** claude-sonnet-4-5-20250929
- **Repetitions:** 5
- **Tasks:** 01-discovery, 02-codegen, 03-config, 04-debugging, 05-extproc
- **Warm-up reps:** 1
- **Cool-down:** 30s

## Summary

- **Total trials:** 50
- **Excluded (warmup):** 0
- **MCP engagement rate:** 100.0%
- **Total cost:** $13.18

## Per-Metric Comparison

| Metric | With MCP (median) | Without MCP (median) | Cliff's d | Effect | p (corrected) | Sig? |
|--------|-------------------|----------------------|-----------|--------|---------------|------|
| total_cost_usd | $0.304 | $0.132 | 0.584 | large | 0.1020 |  |
| input_tokens | 172,488 | 63,311 | 0.728 | large | 0.0034 | * |
| output_tokens | 1,532 | 1,243 | 0.366 | medium | 1.0000 |  |
| duration_ms | 36,488ms | 26,729ms | 0.178 | small | 1.0000 |  |
| num_turns | 7.000 | 2.000 | 0.675 | large | 0.0135 | * |
| mcp_tool_calls | 6.000 | 0.000 | 1.000 | large | 0.0001 | * |
| total_tool_calls | 6.000 | 1.000 | 0.355 | medium | 1.0000 |  |
| composite_quality | 0.970 | 0.514 | 0.822 | large | 0.0002 | * |
| fact_score | 1.000 | 0.840 | 0.797 | large | 0.0005 | * |
| structure_score | 1.000 | 0.286 | 0.584 | large | 0.0034 | * |

## MCP Engagement

| Task | With-MCP Engagement Rate | With-MCP Quality (median) | Without-MCP Quality (median) |
|------|-------------------------|---------------------------|------------------------------|
| 01-discovery | 100.0% | 0.970 | 0.890 |
| 02-codegen | 97.1% | 0.900 | 0.391 |
| 03-config | 100.0% | 1.000 | 0.399 |
| 04-debugging | 100.0% | 1.000 | 0.752 |
| 05-extproc | 100.0% | 0.782 | 0.514 |

## Per-Task Quality Breakdown

### 01-discovery

- With MCP: median=0.970, IQR=[0.890, 1.000]
- Without MCP: median=0.890, IQR=[0.870, 0.890]

### 02-codegen

- With MCP: median=0.900, IQR=[0.900, 1.000]
- Without MCP: median=0.391, IQR=[0.391, 0.391]

### 03-config

- With MCP: median=1.000, IQR=[1.000, 1.000]
- Without MCP: median=0.399, IQR=[0.355, 0.399]

### 04-debugging

- With MCP: median=1.000, IQR=[1.000, 1.000]
- Without MCP: median=0.752, IQR=[0.752, 0.876]

### 05-extproc

- With MCP: median=0.782, IQR=[0.782, 0.782]
- Without MCP: median=0.514, IQR=[0.460, 0.520]
