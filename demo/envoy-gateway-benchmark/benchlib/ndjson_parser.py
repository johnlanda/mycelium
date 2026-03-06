"""Parse Claude NDJSON stream output into efficiency and behavioral metrics."""

from __future__ import annotations

import json
from pathlib import Path

from .models import TrialBehavioral, TrialEfficiency


def parse_ndjson(
    path: Path,
) -> tuple[TrialEfficiency, TrialBehavioral, str]:
    """Parse a Claude NDJSON stream file.

    Returns (efficiency, behavioral, response_text).
    """
    lines = path.read_text().strip().splitlines()

    result_event = None
    tool_uses: list[dict] = []

    for raw in lines:
        try:
            event = json.loads(raw)
        except json.JSONDecodeError:
            continue

        etype = event.get("type")

        if etype == "result":
            result_event = event
        elif etype == "assistant":
            msg = event.get("message", {})
            for block in msg.get("content", []):
                if block.get("type") == "tool_use":
                    tool_uses.append(block)

    # Extract efficiency from result event
    efficiency = TrialEfficiency()
    response_text = ""
    if result_event:
        efficiency.total_cost_usd = result_event.get("total_cost_usd", 0.0)
        efficiency.duration_ms = result_event.get("duration_ms", 0)
        efficiency.num_turns = result_event.get("num_turns", 0)

        usage = result_event.get("usage", {})
        efficiency.input_tokens = (
            usage.get("input_tokens", 0)
            + usage.get("cache_read_input_tokens", 0)
            + usage.get("cache_creation_input_tokens", 0)
        )
        efficiency.output_tokens = usage.get("output_tokens", 0)

        response_text = result_event.get("result", "")

    # Extract behavioral metrics from tool uses
    mcp_calls = [t for t in tool_uses if t.get("name", "").startswith("mcp__")]
    non_mcp_calls = [t for t in tool_uses if not t.get("name", "").startswith("mcp__")]
    web_calls = [
        t
        for t in tool_uses
        if t.get("name") in ("WebSearch", "WebFetch")
    ]
    mcp_tools_used = sorted(set(t.get("name", "") for t in mcp_calls))
    total = len(tool_uses)

    behavioral = TrialBehavioral(
        mcp_tool_calls=len(mcp_calls),
        mcp_tools_used=mcp_tools_used,
        non_mcp_tool_calls=len(non_mcp_calls),
        total_tool_calls=total,
        mcp_engagement_rate=len(mcp_calls) / total if total > 0 else 0.0,
        permission_denials=0,
        web_search_calls=len(web_calls),
    )

    return efficiency, behavioral, response_text
