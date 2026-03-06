# Benchmark Methodology: Mycelium MCP vs Baseline

## 1. Overview

This document specifies a statistically rigorous methodology for measuring whether Mycelium's MCP server improves AI coding agent performance on domain-specific tasks.

**Research question:** Does providing an AI coding agent with semantic search over pre-indexed documentation (via Mycelium's MCP server) produce higher-quality, more efficient responses compared to the agent operating without that tool?

**Independent variable:** MCP availability (with-mcp vs without-mcp)
**Dependent variables:** Response quality, cost, token usage, latency, MCP engagement

**Domain:** Envoy Gateway v1.3.0 — a complex CNCF project with custom CRDs, non-obvious configuration semantics, and specific Go type definitions that the model may not have fully internalized from training data.

---

## 2. Current Approach

### 2.1 How `run-benchmark.sh` Works Today

The current harness (349 lines of bash) runs 5 Envoy Gateway tasks sequentially, each in two conditions:

1. **Without MCP:** Executes `claude -p` in a clean temp directory with no `.mcp.json`, so the agent has no access to Mycelium tools.
2. **With MCP:** Executes `claude -p` in the benchmark directory with `--mcp-config .mcp.json`, providing access to `mctl` search tools.

Each task runs once per condition (n=1). The script collects:
- Token counts (input/output)
- Cost (USD)
- Wall-clock duration
- MCP tool call count
- Permission denial count

Output per task: NDJSON stream, summary JSON, tool call log, human-readable transcript, and final response markdown.

### 2.2 Current Task Suite

| ID | Category | Description |
|----|----------|-------------|
| 01-discovery | Knowledge recall | SecurityPolicy authentication methods, API key config details |
| 02-codegen | Code generation | Backend, BackendTrafficPolicy YAML manifests |
| 03-config | Configuration | ClientTrafficPolicy with HTTP/3, XFCC, keepalive, early headers |
| 04-debugging | Reasoning | SecurityPolicy authorization rule evaluation bug |
| 05-extproc | Mixed | EnvoyExtensionPolicy ext_proc YAML + explain body processing modes |

### 2.3 Observed Limitations

Analysis of 4 benchmark runs (20260304-191826, 20260304-192754, 20260304-200114, 20260305-105223) reveals critical issues:

#### Low MCP Engagement

The agent frequently ignores the available MCP tools even in the with-mcp condition:

| Run | Tasks with MCP calls > 0 | Total MCP calls | Tasks with 0 MCP calls |
|-----|--------------------------|-----------------|------------------------|
| 20260304-200114 | 2/5 (discovery: 6, config: 16) | 22 | codegen, debugging, extproc |
| 20260305-105223 | 1/5 (codegen: 7) | 7 | discovery, config, debugging, extproc |
| 20260304-191826 | Not tracked in summary format | — | — |
| 20260304-192754 | Not tracked in summary format | — | — |

Across the two runs with MCP tracking, only 3 out of 10 with-mcp trials actually engaged the MCP tools. The agent appears to rely on parametric knowledge for most tasks, calling the MCP tools only when it "decides" it needs external help — which is non-deterministic.

#### Massive Cost Variance

Cost for the same task under the same condition varies wildly across runs:

| Task | Condition | Run 1 ($) | Run 2 ($) | Run 3 ($) | Run 4 ($) | Range |
|------|-----------|-----------|-----------|-----------|-----------|-------|
| 01-discovery | with-mcp | 0.21 | 0.26 | 0.53 | 0.79 | 0.21–0.79 (276%) |
| 01-discovery | without-mcp | 0.65 | 0.28 | 0.32 | 0.67 | 0.28–0.67 (139%) |
| 03-config | with-mcp | 0.66 | 0.66 | 0.65 | 0.74 | 0.65–0.74 (14%) |
| 04-debugging | with-mcp | 0.66 | 0.06 | 0.06 | 0.50 | 0.06–0.66 (1000%) |
| 04-debugging | without-mcp | 0.21 | 0.01 | 0.08 | 0.08 | 0.01–0.21 (2000%) |

This variance comes from non-deterministic agent behavior: different numbers of tool calls, different search strategies, different amounts of reasoning, and prompt caching effects.

#### No Quality Measurement

The current harness captures only efficiency metrics. There is no evaluation of whether the agent's response is actually **correct**. An agent that hallucinates a plausible-looking YAML manifest in 2 seconds for $0.01 would score better than one that carefully searches documentation and produces a correct manifest in 30 seconds for $0.50.

#### n=1 Per Condition

Each task runs exactly once per condition per benchmark invocation. With the variance shown above, a single observation is meaningless for statistical comparison.

#### Ordering Effects

Tasks always run in the same order (01 through 05), and the without-mcp condition always runs before with-mcp. Any warm-up effects, rate limiting, or cache priming systematically bias results.

#### Permission Denials as Confound

The without-mcp condition shows higher permission denials (11 in run 200114 vs 3 with-mcp), suggesting the agent attempts tool calls that get blocked, altering its behavior in ways unrelated to MCP availability.

---

## 3. Experimental Design

### 3.1 Design Type: Paired Within-Subjects

Each task is measured under both conditions (with-mcp, without-mcp), making each task its own control. This eliminates between-task variance from the comparison — if task 03-config is inherently harder or more expensive, that difficulty appears in both conditions equally.

**Pairing unit:** (task, repetition). For each task, we run `n` repetitions per condition and compare paired observations.

### 3.2 Sample Size

**Minimum: n=5 repetitions per task per condition.**

Justification: With the large effect sizes observed in existing data (e.g., 6x cost difference for 03-config in run 200114), a paired Wilcoxon signed-rank test achieves ~80% power at α=0.05 with n=5–6 paired observations for large effects (|Cliff's δ| > 0.47). For detecting medium effects, n=10 is preferred.

Recommended progression:
- **Pilot:** n=3 (estimate variance, validate harness)
- **Standard:** n=5 (sufficient for large effects)
- **High-confidence:** n=10 (sufficient for medium effects)

### 3.3 Randomization

To eliminate ordering effects:

1. **Condition order randomization:** For each (task, repetition), randomly assign whether with-mcp or without-mcp runs first. Use a coin flip per trial.
2. **Task order randomization:** Shuffle the task execution order within each repetition block.
3. **Cool-down period:** Insert a 30-second pause between trials to avoid rate-limiting artifacts.

Implementation: generate a randomized trial schedule before execution begins. Record the schedule in the results for reproducibility.

### 3.4 Warm-Up

The first trial of each benchmark run is discarded. This absorbs:
- MCP server cold-start latency
- Ollama embedding model loading
- Any transient API warm-up effects

### 3.5 Model Pinning

Pin the exact model version in all trials using `--model` flag. Record the model ID in results. If the model changes between runs, results are not comparable.

---

## 4. Metrics

### 4.1 Tier 1: Efficiency (automatically collected)

| Metric | Source | Unit |
|--------|--------|------|
| `total_cost_usd` | Claude API result | USD |
| `input_tokens` | Claude API result (includes cache) | count |
| `output_tokens` | Claude API result | count |
| `duration_ms` | Claude API result | milliseconds |
| `num_turns` | Claude API result | count |

### 4.2 Tier 2: Behavioral (automatically collected)

| Metric | Source | Unit |
|--------|--------|------|
| `mcp_tool_calls` | NDJSON tool_use events with `mcp__` prefix | count |
| `mcp_tools_used` | Unique MCP tool names | set |
| `non_mcp_tool_calls` | NDJSON tool_use events without `mcp__` prefix | count |
| `permission_denials` | Claude API result | count |
| `web_search_calls` | Tool calls to WebSearch/WebFetch | count |

**MCP engagement rate** = `mcp_tool_calls / total_tool_calls` for with-mcp trials. Trials where the agent has MCP available but engagement rate = 0 are flagged — see [Guard Rails](#8-guard-rails).

### 4.3 Tier 3: Quality (requires evaluation)

| Metric | Method | Scale |
|--------|--------|-------|
| `fact_score` | Automated string matching against required facts | 0.0–1.0 |
| `structure_score` | YAML/JSON validation + schema checks | 0.0–1.0 |
| `judge_score` | LLM-as-judge against reference answer | 1–5 |
| `composite_quality` | Weighted: 0.3 × fact + 0.3 × structure + 0.4 × judge | 0.0–1.0 |

These are defined in detail in [Section 6: Quality Evaluation](#6-quality-evaluation).

---

## 5. Task Design

### 5.1 Task Categories

Tasks should span these categories to test different agent capabilities:

| Category | Tests | Example |
|----------|-------|---------|
| **Knowledge recall** | Factual accuracy on specific API details | "What auth methods does SecurityPolicy support?" |
| **Code generation** | Correct YAML/Go with exact field names and versions | "Write a BackendTrafficPolicy manifest" |
| **Configuration** | Multi-constraint config assembly | "Create a ClientTrafficPolicy with HTTP/3, XFCC, keepalive" |
| **Debugging/Reasoning** | Understanding runtime semantics | "Why is this authorization rule allowing traffic?" |
| **Mixed** | Generation + explanation | "Write an EnvoyExtensionPolicy and explain body modes" |

### 5.2 Task Rubric Format

Each task should have a companion rubric file (`tasks/01-discovery.rubric.yaml`) specifying expected content:

```yaml
task_id: "01-discovery"
category: "knowledge_recall"
version: 1

# Tier 1: Required facts — exact strings or patterns that must appear
required_facts:
  - pattern: "OIDC|OpenID Connect"
    description: "Mentions OIDC authentication"
    weight: 1
  - pattern: "JWT"
    description: "Mentions JWT authentication"
    weight: 1
  - pattern: "BasicAuth"
    description: "Mentions basic authentication"
    weight: 1
  - pattern: "APIKey|ApiKey"
    description: "Mentions API key authentication"
    weight: 2  # Higher weight — task specifically asks about this
  - pattern: "ExtAuth"
    description: "Mentions external authentication"
    weight: 1
  - pattern: "SecurityPolicySpec"
    description: "References the correct Go type"
    weight: 1

# Tier 2: Structural validation
structure_checks:
  - type: "contains_code_block"
    description: "Response includes code blocks for type definitions"
  - type: "no_yaml_errors"
    description: "Any YAML blocks are syntactically valid"
    applies_to: "yaml"

# Tier 3: LLM judge criteria
judge_criteria: |
  Evaluate the response for accuracy about Envoy Gateway v1.3.0
  SecurityPolicy authentication methods. Check:
  1. Are all authentication methods listed correctly?
  2. Are Go type names accurate (not hallucinated)?
  3. Is the API key configuration explained with correct field names
     (extractFrom, headers, queries, cookies)?
  4. Is the explanation of secret storage for API keys correct?
  Score 1-5 where:
    1 = Major factual errors or missing most methods
    2 = Some methods listed but significant errors in details
    3 = Most methods listed, minor errors in field names
    4 = All methods listed, mostly correct field names
    5 = Comprehensive and accurate, matches source documentation

# Reference answer (for LLM judge comparison)
reference_answer_file: "tasks/01-discovery.reference.md"
```

### 5.3 Task Requirements

Each task must:
1. Have a clear, unambiguous prompt (the existing task .md files)
2. Have a rubric file with all three evaluation tiers
3. Have a reference answer written by a human with access to source documentation
4. Be self-contained — no dependency on other tasks or external state
5. Require domain-specific knowledge that benefits from documentation access

---

## 6. Quality Evaluation

### 6.1 Tier 1: Fact Score (Automated String Matching)

For each `required_facts` entry in the rubric, check whether the response matches the pattern (case-insensitive regex). Compute a weighted score:

```
fact_score = sum(weight_i * match_i) / sum(weight_i)
```

This catches gross omissions and hallucinated alternatives. It is fast, deterministic, and reproducible.

**Limitation:** Cannot assess correctness of surrounding context. A response that mentions "JWT" in a wrong context still gets credit. This is why we need Tier 3.

### 6.2 Tier 2: Structure Score (Validation)

For tasks that require structured output (YAML manifests, JSON, Go code):

| Check | Method |
|-------|--------|
| YAML syntax valid | `yaml.Unmarshal` or `yq` |
| Correct `apiVersion` | String match against expected value |
| Correct `kind` | String match against expected value |
| Required fields present | JSONPath/yq queries |
| No unknown fields | Schema validation (optional, requires CRD schema) |

Score: proportion of checks that pass.

### 6.3 Tier 3: LLM-as-Judge (Semantic Evaluation)

Use a separate LLM call (not the same model being benchmarked) to evaluate response quality against the reference answer and judge criteria.

**Judge prompt template:**

```
You are evaluating an AI assistant's response about Envoy Gateway v1.3.0.

## Task
{task_prompt}

## Reference Answer
{reference_answer}

## Evaluation Criteria
{judge_criteria}

## Response to Evaluate
{agent_response}

Score this response from 1 to 5 according to the criteria above.
Respond with ONLY a JSON object:
{"score": <1-5>, "reasoning": "<brief explanation>"}
```

**Judge configuration:**
- Model: Use a different model family or a pinned snapshot (e.g., if benchmarking Claude Sonnet, judge with GPT-4o or vice versa). This avoids self-evaluation bias.
- Temperature: 0 for reproducibility.
- Multiple judges: For high-stakes evaluations, use 3 independent judge calls and take the median score.

### 6.4 Composite Quality Score

```
composite = 0.3 * fact_score + 0.3 * structure_score + 0.4 * judge_score_normalized
```

Where `judge_score_normalized = (judge_score - 1) / 4` to map 1–5 to 0.0–1.0.

Weights emphasize the judge score because it captures semantic correctness that automated checks miss. The 0.3/0.3/0.4 split ensures automated checks still provide a floor — a hallucinated response can't score above ~0.4 composite even with a generous judge.

---

## 7. Statistical Analysis

### 7.1 Primary Test: Wilcoxon Signed-Rank

For each metric, compare paired observations (with-mcp vs without-mcp for the same task and repetition) using the **Wilcoxon signed-rank test**.

**Why not a paired t-test?**
- The t-test assumes normally distributed differences. With n=5–10 and the heavy-tailed distributions we observe (cost varying by 10x), normality is implausible and cannot be tested reliably.
- Wilcoxon is non-parametric: it ranks the differences and tests whether positive and negative ranks are balanced. No distributional assumptions.
- At n=5 paired observations, the minimum achievable p-value for Wilcoxon is 0.0625 (all 5 pairs in the same direction). At n=6, it's 0.03125, which clears α=0.05.
- **Implication:** n=5 can demonstrate a consistent direction but cannot reach p<0.05. Use n≥6 for formal significance testing.

### 7.2 Effect Size: Cliff's Delta

Report **Cliff's δ** (Cliff's delta) as the non-parametric effect size measure:

```
δ = (#{x_i > y_i} - #{x_i < y_i}) / (n_x * n_y)
```

For paired data with equal n, this simplifies to counting how often with-mcp beats without-mcp minus how often it loses, divided by n².

**Interpretation thresholds** (following Romano et al., 2006):
| |δ| | Interpretation |
|------|----------------|
| < 0.147 | Negligible |
| 0.147–0.330 | Small |
| 0.330–0.474 | Medium |
| > 0.474 | Large |

### 7.3 Confidence Intervals

Report the Hodges-Lehmann estimator for the median difference with its associated 95% confidence interval. This is the natural CI companion to the Wilcoxon test.

### 7.4 Multiple Comparison Correction

When testing multiple metrics (cost, quality, latency, etc.), apply the **Holm-Bonferroni correction** to control the family-wise error rate.

**Why Holm-Bonferroni over Bonferroni?**
- Holm-Bonferroni is uniformly more powerful (rejects at least as many hypotheses) while maintaining the same FWER control.
- With k metrics, sort p-values p₁ ≤ p₂ ≤ ... ≤ pₖ and reject pᵢ while pᵢ ≤ α/(k-i+1).

### 7.5 Reporting Format

For each metric, report:
```
Metric: total_cost_usd
  With MCP:    median=$0.53, IQR=[$0.26, $0.79]
  Without MCP: median=$0.32, IQR=[$0.28, $0.67]
  Difference:  median_diff=$0.21, 95% CI=[-$0.10, $0.52]
  Wilcoxon:    W=3, p=0.125, Cliff's δ=0.40 (medium)
  Direction:   with-mcp costs MORE (3 of 5 pairs)
```

---

## 8. Guard Rails

### 8.1 MCP Engagement Check

**Problem:** The agent may not use MCP tools even when they're available, rendering the comparison meaningless for that trial.

**Mitigation:**
- After each with-mcp trial, check `mcp_tool_calls`. If 0, flag the trial.
- In the task prompt, add a system-level instruction: _"You have access to a Mycelium MCP server with semantic search over Envoy Gateway documentation. Use the `search` and `search_code` tools to verify your answers against the indexed documentation before responding."_
- Track **MCP engagement rate** across all with-mcp trials. If <50% of trials engage MCP, the task prompts need revision — the tasks are not sufficiently difficult to motivate tool use.
- Do **not** discard zero-engagement trials. Instead, report them separately and analyze the engagement rate as a dependent variable.

### 8.2 Model Version Pinning

Record the exact model ID (e.g., `claude-sonnet-4-5-20250929`) from the result JSON. If a model update occurs mid-benchmark, discard the run and restart.

### 8.3 Warm-Up Trials

The first repetition of each benchmark run is executed but excluded from analysis. This absorbs:
- MCP server initialization and embedding model loading
- API connection warm-up
- Prompt caching initialization

### 8.4 Permission Denial Handling

Run with `--dangerously-skip-permissions` (as the current script does) to eliminate permission denials as a confound. If this flag is unavailable in future versions, use an allowlist that permits all tools the agent might use.

Monitor permission denials and abort if any occur — they indicate the harness configuration is broken.

### 8.5 Rate Limiting and Throttling

- Insert a 30-second cool-down between trials.
- Monitor for HTTP 429 responses in stderr. If detected, exponentially back off (60s, 120s, 240s) before retrying.
- Record any retry events in the trial metadata.

### 8.6 Environment Consistency

- Run on the same machine for all trials in a benchmark run.
- Close all other applications that may compete for network bandwidth.
- Record system info: OS version, available memory, network type.

---

## 9. Results Schema

### 9.1 Per-Trial JSON

Each trial produces a file at `results/{run_id}/{task_id}/{condition}-{rep}.json`:

```json
{
  "meta": {
    "run_id": "20260310-143022",
    "task_id": "01-discovery",
    "condition": "with-mcp",
    "repetition": 2,
    "model": "claude-sonnet-4-5-20250929",
    "timestamp_start": "2026-03-10T14:32:15Z",
    "timestamp_end": "2026-03-10T14:33:02Z",
    "trial_order": 7,
    "condition_order": "mcp-first"
  },
  "efficiency": {
    "total_cost_usd": 0.527,
    "input_tokens": 276404,
    "output_tokens": 2133,
    "duration_ms": 111400,
    "num_turns": 9
  },
  "behavioral": {
    "mcp_tool_calls": 6,
    "mcp_tools_used": ["mcp__mycelium__search", "mcp__mycelium__search_code"],
    "non_mcp_tool_calls": 3,
    "total_tool_calls": 9,
    "mcp_engagement_rate": 0.667,
    "permission_denials": 0,
    "web_search_calls": 0
  },
  "quality": {
    "fact_score": 0.85,
    "structure_score": 1.0,
    "judge_score": 4,
    "judge_reasoning": "All authentication methods listed correctly...",
    "composite_quality": 0.855
  },
  "artifacts": {
    "ndjson": "01-discovery/with-mcp-2.ndjson",
    "response": "01-discovery/with-mcp-2.md",
    "tools": "01-discovery/with-mcp-2-tools.json",
    "transcript": "01-discovery/with-mcp-2-transcript.md"
  }
}
```

### 9.2 Aggregated Run Report

Each complete benchmark run produces `results/{run_id}/report.json`:

```json
{
  "run_id": "20260310-143022",
  "config": {
    "model": "claude-sonnet-4-5-20250929",
    "repetitions": 5,
    "tasks": ["01-discovery", "02-codegen", "03-config", "04-debugging", "05-extproc"],
    "warm_up_trials": 1,
    "cool_down_seconds": 30
  },
  "summary": {
    "total_trials": 50,
    "excluded_trials": 2,
    "mcp_engagement_rate": 0.72,
    "total_cost_usd": 28.50
  },
  "per_metric": {
    "total_cost_usd": {
      "with_mcp": {"median": 0.42, "iqr": [0.21, 0.68]},
      "without_mcp": {"median": 0.35, "iqr": [0.18, 0.55]},
      "wilcoxon_W": 18,
      "wilcoxon_p": 0.043,
      "p_corrected": 0.172,
      "cliffs_delta": 0.36,
      "effect_interpretation": "medium",
      "median_difference": 0.07,
      "ci_95": [-0.02, 0.15]
    }
  },
  "per_task": {
    "01-discovery": {
      "quality_with_mcp": {"median": 0.85, "iqr": [0.80, 0.92]},
      "quality_without_mcp": {"median": 0.62, "iqr": [0.55, 0.70]}
    }
  }
}
```

---

## 10. Cost Estimation

### 10.1 Per-Trial Cost

Based on existing runs, median per-trial cost is approximately:

| Task | With MCP (median) | Without MCP (median) |
|------|-------------------|---------------------|
| 01-discovery | $0.39 | $0.49 |
| 02-codegen | $0.35 | $0.12 |
| 03-config | $0.66 | $0.17 |
| 04-debugging | $0.28 | $0.09 |
| 05-extproc | $0.07 | $0.07 |
| **Per-task average** | **~$0.35** | **~$0.19** |

### 10.2 Full Run Cost

```
Per condition per rep:  5 tasks × $0.27 avg = $1.35
Both conditions:        $1.35 × 2 = $2.70 per rep
Standard run (n=5):     $2.70 × 5 = $13.50 + 1 warm-up rep = ~$16
High-confidence (n=10): $2.70 × 10 = $27.00 + 1 warm-up rep = ~$30
Quality evaluation:     ~$0.05 per judge call × 50 trials = $2.50
```

**Estimated total for a standard run: ~$18–20 USD**
**Estimated total for a high-confidence run: ~$32–35 USD**

These estimates assume Claude Max subscription pricing. API pricing would be significantly higher.

### 10.3 Time Estimate

Median per-trial duration is ~50 seconds. With 30-second cool-downs:

```
Standard run (n=5):     (5+1) reps × 5 tasks × 2 conditions × (50s + 30s) = 80 min
High-confidence (n=10): (10+1) reps × 5 tasks × 2 conditions × (50s + 30s) = ~145 min
```

---

## 11. Implementation Roadmap

### Phase 1: Task Rubrics (prerequisite)

Create rubric files for all 5 tasks with required_facts, structure_checks, and judge_criteria. Write reference answers by manually consulting Envoy Gateway v1.3.0 source code and documentation.

**Deliverables:**
- `tasks/01-discovery.rubric.yaml` through `tasks/05-extproc.rubric.yaml`
- `tasks/01-discovery.reference.md` through `tasks/05-extproc.reference.md`

### Phase 2: Benchmark Runner Rewrite

Replace `run-benchmark.sh` with a Go-based runner or a structured Python script that implements:

1. **Trial scheduler** — generates randomized (task, condition, rep) execution order
2. **Execution engine** — runs `claude -p` with correct flags per condition, with retry/backoff
3. **Result collector** — parses NDJSON into per-trial JSON (Section 9.1 schema)
4. **MCP engagement monitor** — flags zero-engagement trials in real-time

**Deliverables:**
- `bench.py` (or `cmd/bench/main.go`) — main runner
- `config.yaml` — benchmark configuration (n, model, cool-down, etc.)

### Phase 3: Quality Evaluator

Build an evaluation pipeline that runs after all trials complete:

1. **Fact scorer** — regex matching against rubric `required_facts`
2. **Structure scorer** — YAML/JSON validation per rubric `structure_checks`
3. **Judge scorer** — LLM-as-judge calls per rubric `judge_criteria`
4. **Composite calculator** — weighted combination

**Deliverables:**
- `evaluate.py` — quality evaluation script
- Per-trial quality scores appended to trial JSON

### Phase 4: Statistical Analysis

Build an analysis script that:

1. Loads all per-trial JSON files for a run
2. Computes descriptive statistics (median, IQR) per metric per condition
3. Runs Wilcoxon signed-rank tests with Holm-Bonferroni correction
4. Computes Cliff's delta and Hodges-Lehmann CIs
5. Generates `report.json` and a human-readable summary table

**Deliverables:**
- `analyze.py` — statistical analysis script
- `report.json` — machine-readable results
- `report.md` — human-readable summary with tables

### Phase 5: Task Prompt Engineering

Based on Phase 2–4 results, revise task prompts to improve MCP engagement:

- Add explicit tool-use instructions to task prompts
- Consider adding tasks that more clearly benefit from documentation lookup (e.g., version-specific breaking changes, recently added features)
- Drop tasks that show floor/ceiling effects (both conditions always perfect or always terrible)

### Dependency Graph

```
Phase 1 (rubrics)
    ↓
Phase 2 (runner) ──→ Phase 5 (prompt tuning)
    ↓                    ↑
Phase 3 (evaluator) ─────┘
    ↓
Phase 4 (analysis)
```

Phases 1 and 2 can proceed in parallel. Phase 3 requires rubrics from Phase 1. Phase 4 requires output from Phases 2 and 3. Phase 5 is iterative based on Phase 4 findings.
