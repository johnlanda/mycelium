"""Tier 1: Weighted regex matching against required facts."""

from __future__ import annotations

import re

from .models import Rubric


def score_facts(response: str, rubric: Rubric) -> float:
    """Compute fact score as weighted proportion of matching required facts.

    fact_score = sum(w_i * match_i) / sum(w_i)
    """
    if not rubric.required_facts:
        return 1.0

    total_weight = 0.0
    matched_weight = 0.0

    for fact in rubric.required_facts:
        total_weight += fact.weight
        if re.search(fact.pattern, response, re.IGNORECASE):
            matched_weight += fact.weight

    return matched_weight / total_weight if total_weight > 0 else 0.0
