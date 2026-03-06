"""Pydantic data models for benchmark configuration, rubrics, and trial results."""

from __future__ import annotations

from typing import Optional

from pydantic import BaseModel, Field


# ── Benchmark configuration ──────────────────────────────────────────────────


class QualityWeights(BaseModel):
    fact: float = 0.3
    structure: float = 0.3
    judge: float = 0.4


class BenchConfig(BaseModel):
    repetitions: int = 5
    model: str = "claude-sonnet-4-5-20250929"
    cool_down_seconds: int = 30
    warm_up_reps: int = 1
    tasks: list[str] = Field(
        default_factory=lambda: [
            "01-discovery",
            "02-codegen",
            "03-config",
            "04-debugging",
            "05-extproc",
        ]
    )
    max_retries: int = 3
    retry_backoff_base: int = 60
    judge_model: Optional[str] = None
    judge_num_calls: int = 1
    seed: Optional[int] = None
    quality_weights: QualityWeights = Field(default_factory=QualityWeights)


# ── Rubric schema ────────────────────────────────────────────────────────────


class RequiredFact(BaseModel):
    pattern: str
    description: str
    weight: float = 1.0


class StructureCheck(BaseModel):
    type: str  # contains_code_block, no_yaml_errors, api_version, kind, required_field
    description: str
    applies_to: Optional[str] = None  # e.g. "yaml"
    expected: Optional[str] = None  # expected value for api_version/kind/required_field checks
    field: Optional[str] = None  # field path for required_field checks


class Rubric(BaseModel):
    task_id: str
    category: str
    version: int = 1
    required_facts: list[RequiredFact] = Field(default_factory=list)
    structure_checks: list[StructureCheck] = Field(default_factory=list)
    judge_criteria: str = ""
    reference_answer_file: str = ""


# ── Trial result schema (matches METHODOLOGY.md Section 9.1) ────────────────


class TrialMeta(BaseModel):
    run_id: str
    task_id: str
    condition: str  # "with-mcp" or "without-mcp"
    repetition: int
    model: str
    timestamp_start: str
    timestamp_end: str
    trial_order: int
    condition_order: str  # "mcp-first" or "baseline-first"


class TrialEfficiency(BaseModel):
    total_cost_usd: float = 0.0
    input_tokens: int = 0
    output_tokens: int = 0
    duration_ms: int = 0
    num_turns: int = 0


class TrialBehavioral(BaseModel):
    mcp_tool_calls: int = 0
    mcp_tools_used: list[str] = Field(default_factory=list)
    non_mcp_tool_calls: int = 0
    total_tool_calls: int = 0
    mcp_engagement_rate: float = 0.0
    permission_denials: int = 0
    web_search_calls: int = 0


class TrialQuality(BaseModel):
    fact_score: Optional[float] = None
    structure_score: Optional[float] = None
    judge_score: Optional[int] = None
    judge_reasoning: Optional[str] = None
    composite_quality: Optional[float] = None


class TrialArtifacts(BaseModel):
    ndjson: str = ""
    response: str = ""
    tools: str = ""
    transcript: str = ""


class TrialResult(BaseModel):
    meta: TrialMeta
    efficiency: TrialEfficiency = Field(default_factory=TrialEfficiency)
    behavioral: TrialBehavioral = Field(default_factory=TrialBehavioral)
    quality: TrialQuality = Field(default_factory=TrialQuality)
    artifacts: TrialArtifacts = Field(default_factory=TrialArtifacts)


# ── Scheduler output ────────────────────────────────────────────────────────


class ScheduledTrial(BaseModel):
    trial_order: int
    task_id: str
    condition: str  # "with-mcp" or "without-mcp"
    repetition: int
    is_warmup: bool = False
    condition_order: str = ""  # "mcp-first" or "baseline-first"
