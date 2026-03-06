"""Generate report.json and report.md from analysis results."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any


def generate_report_json(
    run_id: str,
    config: dict,
    summary: dict,
    per_metric: dict,
    per_task: dict,
    output_path: Path,
) -> None:
    """Write report.json matching METHODOLOGY.md Section 9.2 schema."""
    report = {
        "run_id": run_id,
        "config": config,
        "summary": summary,
        "per_metric": per_metric,
        "per_task": per_task,
    }
    output_path.write_text(json.dumps(report, indent=2))


def generate_report_md(
    run_id: str,
    config: dict,
    summary: dict,
    per_metric: dict,
    per_task: dict,
    output_path: Path,
) -> None:
    """Write human-readable report.md."""
    lines = [
        f"# Benchmark Report: {run_id}",
        "",
        "## Configuration",
        "",
        f"- **Model:** {config.get('model', 'unknown')}",
        f"- **Repetitions:** {config.get('repetitions', 0)}",
        f"- **Tasks:** {', '.join(config.get('tasks', []))}",
        f"- **Warm-up reps:** {config.get('warm_up_reps', 0)}",
        f"- **Cool-down:** {config.get('cool_down_seconds', 0)}s",
        "",
        "## Summary",
        "",
        f"- **Total trials:** {summary.get('total_trials', 0)}",
        f"- **Excluded (warmup):** {summary.get('excluded_trials', 0)}",
        f"- **MCP engagement rate:** {summary.get('mcp_engagement_rate', 0):.1%}",
        f"- **Total cost:** ${summary.get('total_cost_usd', 0):.2f}",
        "",
        "## Per-Metric Comparison",
        "",
    ]

    # Metric comparison table
    lines.append(
        "| Metric | With MCP (median) | Without MCP (median) "
        "| Cliff's d | Effect | p (corrected) | Sig? |"
    )
    lines.append("|--------|-------------------|----------------------|-----------|--------|---------------|------|")

    for metric_name, data in per_metric.items():
        wm = data.get("with_mcp", {})
        wom = data.get("without_mcp", {})
        lines.append(
            f"| {metric_name} "
            f"| {_fmt_val(wm.get('median', 0), metric_name)} "
            f"| {_fmt_val(wom.get('median', 0), metric_name)} "
            f"| {data.get('cliffs_delta', 0):.3f} "
            f"| {data.get('effect_interpretation', 'n/a')} "
            f"| {data.get('p_corrected', 1.0):.4f} "
            f"| {'*' if data.get('p_corrected', 1.0) <= 0.05 else ''} |"
        )

    lines.extend(["", "## MCP Engagement", ""])

    # MCP engagement per task
    if per_task:
        lines.append("| Task | With-MCP Engagement Rate | With-MCP Quality (median) | Without-MCP Quality (median) |")
        lines.append("|------|-------------------------|---------------------------|------------------------------|")
        for task_id, task_data in sorted(per_task.items()):
            eng = task_data.get("mcp_engagement_rate", 0)
            qw = task_data.get("quality_with_mcp", {}).get("median", 0)
            qwo = task_data.get("quality_without_mcp", {}).get("median", 0)
            lines.append(
                f"| {task_id} | {eng:.1%} | {qw:.3f} | {qwo:.3f} |"
            )

    lines.extend(["", "## Per-Task Quality Breakdown", ""])

    for task_id, task_data in sorted(per_task.items()):
        lines.append(f"### {task_id}")
        lines.append("")
        qwm = task_data.get("quality_with_mcp", {})
        qwom = task_data.get("quality_without_mcp", {})
        lines.append(f"- With MCP: median={qwm.get('median', 0):.3f}, IQR=[{qwm.get('iqr', [0, 0])[0]:.3f}, {qwm.get('iqr', [0, 0])[1]:.3f}]")
        lines.append(f"- Without MCP: median={qwom.get('median', 0):.3f}, IQR=[{qwom.get('iqr', [0, 0])[0]:.3f}, {qwom.get('iqr', [0, 0])[1]:.3f}]")
        lines.append("")

    output_path.write_text("\n".join(lines))


def _fmt_val(val: float, metric_name: str) -> str:
    """Format a metric value for display."""
    if "cost" in metric_name:
        return f"${val:.3f}"
    elif "tokens" in metric_name:
        return f"{val:,.0f}"
    elif "duration" in metric_name or "ms" in metric_name:
        return f"{val:,.0f}ms"
    elif "rate" in metric_name or "score" in metric_name or "quality" in metric_name:
        return f"{val:.3f}"
    else:
        return f"{val:.3f}"
