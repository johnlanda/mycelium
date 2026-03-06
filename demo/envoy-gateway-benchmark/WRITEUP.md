# Does RAG Make AI Coding Agents Better? A Controlled Benchmark

## TL;DR

We measured whether giving an AI coding agent access to semantic search over indexed documentation (via [Mycelium](https://github.com/johnlanda/mycelium)'s MCP server) improves its answers on domain-specific tasks. Across 50 paired trials on 5 Envoy Gateway tasks, **agents with RAG access scored 0.970 median composite quality vs 0.514 without** (p=0.0002). MCP tool engagement was 100%. The quality improvement is statistically significant with a large effect size, at a modest cost increase of ~$0.17/trial.

---

## 1. Experiment Setup

### 1.1 Research Question

> Does providing an AI coding agent with semantic search over pre-indexed documentation (via an MCP server) produce higher-quality responses compared to the agent operating without that tool?

### 1.2 System Under Test

**Mycelium** (`mctl`) is a CLI tool and MCP server that gives AI coding agents reproducible, version-pinned dependency context. It reads a project manifest, resolves dependencies, and maintains a local vector store of indexed documentation and source code. An MCP server exposes semantic search over that store via three tools:

- **`search`** — Semantic search over documentation and code chunks
- **`search_code`** — Semantic search scoped to source code (Go types, structs, function signatures)
- **`list_sources`** — List all indexed sources with versions and chunk counts

For this benchmark, the store was loaded with **Envoy Gateway v1.3.0** documentation and Go API type definitions from `api/v1alpha1`.

### 1.3 Experimental Design

**Design:** Paired within-subjects. Each of 5 tasks was run under both conditions (with-MCP and without-MCP), with each task serving as its own control.

**Model:** `claude-sonnet-4-5-20250929` (pinned for all trials)

**Repetitions:** 5 per task per condition (50 data trials total)

**Warm-up:** 1 additional repetition per task per condition (10 trials, excluded from analysis)

**Randomization:**
- Condition order randomized per (task, repetition) pair — sometimes with-MCP ran first, sometimes without
- Task order shuffled within each repetition block
- 30-second cool-down between trials

**Without-MCP condition:** Agent runs in a clean temp directory with no `.mcp.json` — no access to Mycelium tools. The agent relies entirely on its training knowledge.

**With-MCP condition:** Agent runs with `--mcp-config .mcp.json`, providing access to the three Mycelium search tools. The prompt includes guidance encouraging tool use for verification.

### 1.4 Task Suite

Five tasks spanning different categories of domain-specific work:

| ID | Category | Task Description |
|----|----------|-----------------|
| 01-discovery | Knowledge recall | List SecurityPolicy authentication methods with Go type names; explain API key configuration details |
| 02-codegen | Code generation | Write Backend, BackendTrafficPolicy YAML manifests with circuit breaking and retry logic |
| 03-config | Configuration | Create a ClientTrafficPolicy with HTTP/3, XFCC, keepalive, early headers — 7+ specific field requirements |
| 04-debugging | Reasoning | Diagnose why an authorization rule allows unexpected traffic; explain first-match semantics |
| 05-extproc | Mixed | Write an EnvoyExtensionPolicy YAML for ext_proc and explain body processing modes |

These tasks were chosen because they require **precise, version-specific knowledge** — exact CRD field names, Go type definitions, enum values, and configuration semantics that a model may not have fully internalized from training data.

### 1.5 Quality Scoring

Each response was evaluated on three tiers, combined into a composite score:

| Tier | Method | Weight | Scale |
|------|--------|--------|-------|
| **Fact score** | Automated regex matching against required facts from rubric | 0.3 | 0.0–1.0 |
| **Structure score** | YAML/JSON validation, correct `apiVersion`/`kind`, required fields present | 0.3 | 0.0–1.0 |
| **Judge score** | LLM-as-judge evaluation against reference answer | 0.4 | 1–5 (normalized to 0.0–1.0) |

**Composite quality** = 0.3 x fact + 0.3 x structure + 0.4 x (judge - 1) / 4

Reference answers were written by a human consulting the Envoy Gateway v1.3.0 source code directly.

---

## 2. Results

### 2.1 Configuration

- **Run ID:** 20260305-152403
- **Model:** claude-sonnet-4-5-20250929
- **Repetitions:** 5
- **Total trials:** 50 (excluding 10 warm-up)
- **MCP engagement rate:** 100.0%
- **Total cost:** $13.18

### 2.2 Overall Comparison

| Metric | With MCP (median) | Without MCP (median) | Cliff's d | Effect Size | p (corrected) | Significant? |
|--------|-------------------|----------------------|-----------|-------------|---------------|:------------:|
| **composite_quality** | **0.970** | **0.514** | **0.822** | **large** | **0.0002** | **yes** |
| **fact_score** | **1.000** | **0.840** | **0.797** | **large** | **0.0005** | **yes** |
| **structure_score** | **1.000** | **0.286** | **0.584** | **large** | **0.0034** | **yes** |
| mcp_tool_calls | 6.0 | 0.0 | 1.000 | large | 0.0001 | yes |
| num_turns | 7.0 | 2.0 | 0.675 | large | 0.0135 | yes |
| input_tokens | 172,488 | 63,311 | 0.728 | large | 0.0034 | yes |
| total_cost_usd | $0.304 | $0.132 | 0.584 | large | 0.1020 | no |
| output_tokens | 1,532 | 1,243 | 0.366 | medium | 1.0000 | no |
| duration_ms | 36,488 | 26,729 | 0.178 | small | 1.0000 | no |
| total_tool_calls | 6.0 | 1.0 | 0.355 | medium | 1.0000 | no |

### 2.3 Per-Task Quality Breakdown

| Task | MCP Engagement | With MCP (median) | Without MCP (median) | Quality Delta |
|------|:--------------:|:-----------------:|:--------------------:|:-------------:|
| 01-discovery | 100% | 0.970 | 0.890 | +9.0% |
| 02-codegen | 97.1% | 0.900 | 0.391 | +130% |
| 03-config | 100% | 1.000 | 0.399 | +151% |
| 04-debugging | 100% | 1.000 | 0.752 | +33.0% |
| 05-extproc | 100% | 0.782 | 0.514 | +52.1% |

### 2.4 Per-Task Quality Detail

**01-discovery** (Knowledge Recall)
- With MCP: median=0.970, IQR=[0.890, 1.000]
- Without MCP: median=0.890, IQR=[0.870, 0.890]
- The agent's training knowledge covers the broad strokes of SecurityPolicy auth methods well. MCP provides a small but consistent lift by grounding exact Go type names and field details.

**02-codegen** (Code Generation)
- With MCP: median=0.900, IQR=[0.900, 1.000]
- Without MCP: median=0.391, IQR=[0.391, 0.391]
- The largest relative improvement. Without MCP, the agent consistently produces YAML with incorrect field names, wrong `apiVersion` strings, or missing required fields. The structure score collapses (median 0.143 without vs 1.000 with) because hallucinated CRD fields fail validation.

**03-config** (Configuration)
- With MCP: median=1.000, IQR=[1.000, 1.000]
- Without MCP: median=0.399, IQR=[0.355, 0.399]
- Near-perfect scores with MCP — the agent looks up each configuration field and gets them right. Without MCP, it confuses field names (e.g., inventing plausible-sounding but incorrect YAML keys) and the structure score drops to 0.143.

**04-debugging** (Reasoning)
- With MCP: median=1.000, IQR=[1.000, 1.000]
- Without MCP: median=0.752, IQR=[0.752, 0.876]
- The agent can reason about authorization rule semantics from training knowledge alone, but MCP lets it verify the exact rule evaluation model. Without MCP, it occasionally gets edge cases wrong.

**05-extproc** (Mixed Generation + Explanation)
- With MCP: median=0.782, IQR=[0.782, 0.782]
- Without MCP: median=0.514, IQR=[0.460, 0.520]
- ExtProc is a niche Envoy feature. Without MCP, the agent's knowledge of the CRD schema is incomplete, leading to missing or incorrect fields. Even with MCP, this task doesn't reach perfect scores — the body processing mode explanation is nuanced enough that the agent doesn't always capture all three modes perfectly.

---

## 3. Analysis

### 3.1 The Core Finding

Providing the agent with semantic search over indexed documentation produces **dramatically better answers** on tasks requiring precise, domain-specific knowledge. The composite quality improvement (median 0.970 vs 0.514) is both practically meaningful and statistically significant.

The improvement is not uniform across tasks — it scales with how much **precise recall** a task demands:

- **High-precision tasks** (02-codegen, 03-config) see 130–151% quality improvement. These tasks require exact CRD field names, correct `apiVersion` strings, and valid enum values. The model's training knowledge gets the general shape right but hallucinates specific details. RAG eliminates this failure mode.

- **Reasoning tasks** (04-debugging) see a 33% improvement. The core reasoning is within the model's capability, but verifying against source documentation catches edge cases and ensures the explanation matches the actual implementation.

- **Knowledge recall tasks** (01-discovery) see a modest 9% improvement. The model already knows the broad facts; RAG adds precision on exact type names and field details.

### 3.2 The Cost Tradeoff

With-MCP trials use ~2.7x more input tokens (172k vs 63k median) because the agent makes ~6 search calls per trial, each returning documentation chunks. This translates to a cost increase from $0.13 to $0.30 per trial — though notably this difference is **not statistically significant** after multiple comparison correction (p=0.102).

The cost increase is driven entirely by input tokens (search results being fed back to the model). Output token counts are statistically indistinguishable (1,532 vs 1,243, p=1.0) — the agent produces similarly-sized answers either way.

Duration is also not significantly different (36.5s vs 26.7s, p=1.0). While with-MCP trials take slightly longer in median, the without-MCP condition has high variance (some trials take 100s+ as the agent goes through multiple reasoning turns without grounding).

### 3.3 MCP Engagement

This run achieved 100% MCP engagement — every with-MCP trial used the search tools at least once (median 6 calls). This is a critical validation: the agent consistently chose to use the available tools rather than relying on training knowledge alone.

---

## 4. How to Read the Statistical Tests

This section explains what each statistical measure means and how to interpret the results table.

### 4.1 Wilcoxon Signed-Rank Test (p-values)

**What it is:** A non-parametric hypothesis test for paired data. For each (task, repetition) pair, we compute the difference between with-MCP and without-MCP scores. The test asks: "Are these differences systematically positive or negative, or could they be random?"

**Why not a t-test?** The t-test assumes differences follow a normal distribution. With n=25 pairs and heavy-tailed distributions (cost can vary by 10x between trials), normality is implausible. The Wilcoxon test makes no distributional assumptions — it works on ranks, not raw values.

**How to interpret:**
- **p < 0.05** means the observed difference would occur less than 5% of the time by chance alone. We call this "statistically significant."
- **p = 0.0002** (composite_quality) means there is a 0.02% probability of seeing this large a difference if MCP truly had no effect. This is very strong evidence.
- **p = 1.0** (output_tokens) means the differences are indistinguishable from random noise. MCP has no detectable effect on output length.

### 4.2 Holm-Bonferroni Correction (p_corrected)

**The problem:** We test 10 metrics simultaneously. If each test has a 5% false-positive rate, we'd expect ~0.5 false positives across all tests by chance alone.

**The solution:** The Holm-Bonferroni correction adjusts p-values upward to account for multiple testing. It's more powerful than the simpler Bonferroni correction (which would just multiply all p-values by 10) — it applies progressively less severe corrections as you move through the sorted p-values.

**How to interpret:**
- The "Significant?" column uses the **corrected** p-values. Only metrics that remain below 0.05 after correction are marked significant.
- `total_cost_usd` has a raw p=0.026 but corrected p=0.102 — it would be significant in isolation but doesn't survive the multiple testing correction. This means we can't rule out that the cost difference is a chance fluctuation.

### 4.3 Cliff's Delta (Effect Size)

**What it is:** Cliff's delta (d) measures *how often* with-MCP beats without-MCP in pairwise comparisons. It ranges from -1.0 to +1.0.

**Formula:** For each with-MCP observation and each without-MCP observation, count wins minus losses, divided by total comparisons.

**Interpretation thresholds** (Romano et al., 2006):

| |d| Range | Interpretation | Meaning |
|------------|----------------|---------|
| < 0.147 | Negligible | Conditions are practically equivalent |
| 0.147–0.330 | Small | Detectable but minor difference |
| 0.330–0.474 | Medium | Meaningful difference in practice |
| > 0.474 | Large | Substantial, practically important difference |

**In our results:**
- **d = 0.822** (composite_quality): In 91% of pairwise comparisons, with-MCP scored higher. This is a large, practically significant effect.
- **d = 1.000** (mcp_tool_calls): With-MCP always has more MCP calls — a perfect separation, as expected by design.
- **d = 0.178** (duration_ms): Small effect. MCP trials are slightly longer, but the difference is minor and not practically important.

### 4.4 Why Effect Size Matters More Than p-Values

A p-value tells you whether an effect *exists*. Cliff's delta tells you whether it *matters*. With enough data, even trivially small differences become statistically significant. Conversely, practically important differences can fail to reach significance with small samples.

In this benchmark:
- **composite_quality** is both significant (p=0.0002) and has a large effect (d=0.822). This is the strongest kind of evidence — the effect is real *and* substantial.
- **total_cost_usd** has a large effect (d=0.584) but is not significant (p=0.102). The direction is clear (MCP costs more), but we'd need more trials to confirm it's not a fluke. The effect size tells us it's probably real; the p-value tells us we can't be confident yet.
- **duration_ms** has a small effect (d=0.178) and is not significant (p=1.0). Even with more data, this difference is unlikely to matter in practice.

### 4.5 Confidence Intervals

The report includes 95% confidence intervals for median differences (Hodges-Lehmann estimator). These tell you the plausible range for the true difference:

- **composite_quality:** median difference = 0.320, 95% CI = [0.051, 0.622]. We're 95% confident the true quality improvement is between 5.1 and 62.2 percentage points. The entire CI is above zero, consistent with the significant p-value.
- **total_cost_usd:** median difference = $0.129, 95% CI = [-$0.239, $0.300]. The CI includes zero, meaning the true cost difference could go either way — again consistent with the non-significant p-value.

---

## 5. Limitations

- **Single model:** Results are for Claude Sonnet 4.5 only. Other models may show different engagement patterns or quality tradeoffs.
- **Single domain:** Envoy Gateway is a complex CNCF project with extensive CRDs. The benefit of RAG may be smaller for better-known domains (e.g., React, Django) where model training data is more comprehensive.
- **n=5 per task:** While sufficient to detect the large effects observed here, per-task comparisons lack the power to detect smaller differences. The per-task quality breakdowns should be interpreted as directional, not definitive.
- **LLM-as-judge subjectivity:** The judge score (40% of composite) depends on the judge model's interpretation of the rubric criteria. Using a different judge model or different criteria could shift absolute scores, though relative comparisons should be stable.
- **Task prompt includes MCP guidance:** With-MCP trials receive a prompt prefix encouraging tool use. This is necessary to achieve engagement (without it, engagement was 32%) but means the comparison is "agent with RAG tools and guidance to use them" vs "agent alone" — not a pure tool-availability comparison.
