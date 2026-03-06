"""Tier 3: LLM-as-judge via claude -p subprocess."""

from __future__ import annotations

import json
import os
import re
import subprocess
import statistics
from pathlib import Path
from typing import Optional

from .models import BenchConfig


JUDGE_PROMPT_TEMPLATE = """You are evaluating an AI assistant's response about Envoy Gateway v1.3.0.

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
{{"score": <1-5>, "reasoning": "<brief explanation>"}}"""


def score_judge(
    response: str,
    task_prompt: str,
    reference_answer: str,
    judge_criteria: str,
    config: BenchConfig,
) -> tuple[Optional[int], Optional[str]]:
    """Run LLM-as-judge scoring.

    Returns (score, reasoning) or (None, None) on failure.
    """
    prompt = JUDGE_PROMPT_TEMPLATE.format(
        task_prompt=task_prompt,
        reference_answer=reference_answer,
        judge_criteria=judge_criteria,
        agent_response=response,
    )

    scores = []
    reasonings = []

    for _ in range(config.judge_num_calls):
        score, reasoning = _single_judge_call(prompt, config)
        if score is not None:
            scores.append(score)
            reasonings.append(reasoning or "")

    if not scores:
        return None, None

    # Take median if multiple calls
    if len(scores) == 1:
        return scores[0], reasonings[0]

    median_score = int(statistics.median(scores))
    # Return reasoning from the call closest to median
    closest_idx = min(range(len(scores)), key=lambda i: abs(scores[i] - median_score))
    return median_score, reasonings[closest_idx]


def _single_judge_call(
    prompt: str, config: BenchConfig
) -> tuple[Optional[int], Optional[str]]:
    """Execute a single judge call via claude -p."""
    cmd = [
        "claude",
        "-p",
        prompt,
        "--output-format",
        "text",
    ]
    if config.judge_model:
        cmd.extend(["--model", config.judge_model])

    env = os.environ.copy()
    env.pop("CLAUDECODE", None)
    env.pop("ANTHROPIC_API_KEY", None)

    try:
        result = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            timeout=120,
            env=env,
        )
        if result.returncode != 0:
            return None, None

        return _parse_judge_response(result.stdout.strip())
    except (subprocess.TimeoutExpired, Exception):
        return None, None


def _parse_judge_response(text: str) -> tuple[Optional[int], Optional[str]]:
    """Parse JSON response from judge, stripping markdown fences if present."""
    # Strip markdown code fences
    text = re.sub(r"^```(?:json)?\s*\n?", "", text.strip())
    text = re.sub(r"\n?```\s*$", "", text.strip())

    try:
        data = json.loads(text)
        score = int(data.get("score", 0))
        reasoning = data.get("reasoning", "")
        if 1 <= score <= 5:
            return score, reasoning
        return None, None
    except (json.JSONDecodeError, ValueError, TypeError):
        return None, None
