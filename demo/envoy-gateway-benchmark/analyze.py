#!/usr/bin/env python3
"""Statistical analysis entry point (Phase 4).

Usage:
    python analyze.py results/{run_id}/ [--alpha 0.05]
"""

from __future__ import annotations

import json
from collections import defaultdict
from pathlib import Path

import click
import yaml

from benchlib.models import BenchConfig, TrialResult
from benchlib.report import generate_report_json, generate_report_md
from benchlib.stats import (
    cliffs_delta,
    descriptive_stats,
    hodges_lehmann,
    holm_bonferroni,
    wilcoxon_test,
)


@click.command()
@click.argument("results_dir", type=click.Path(exists=True))
@click.option("--alpha", default=0.05, type=float, help="Significance level")
def main(results_dir: str, alpha: float) -> None:
    results_path = Path(results_dir).resolve()

    # Load config
    config_path = results_path / "config.yaml"
    if config_path.exists():
        with open(config_path) as f:
            config_data = yaml.safe_load(f)
            config = BenchConfig(**config_data)
    else:
        config_data = {}
        config = BenchConfig()

    # Load all non-warmup trial JSONs
    trials: list[TrialResult] = []
    for task_dir in sorted(results_path.iterdir()):
        if not task_dir.is_dir() or task_dir.name.startswith("."):
            continue
        for json_file in sorted(task_dir.glob("*.json")):
            if "-tools" in json_file.stem or "-transcript" in json_file.stem:
                continue
            data = json.loads(json_file.read_text())
            trial = TrialResult(**data)
            if trial.meta.repetition > 0:  # skip warmup
                trials.append(trial)

    click.echo(f"Loaded {len(trials)} non-warmup trials")

    # Pair by (task_id, repetition)
    pairs: dict[tuple[str, int], dict[str, TrialResult]] = defaultdict(dict)
    for t in trials:
        pairs[(t.meta.task_id, t.meta.repetition)][t.meta.condition] = t

    # Build complete pairs only
    complete_pairs = {
        k: v for k, v in pairs.items() if "with-mcp" in v and "without-mcp" in v
    }
    click.echo(f"Complete pairs: {len(complete_pairs)}")

    if not complete_pairs:
        click.echo("No complete pairs found. Nothing to analyze.")
        return

    # Define metrics to analyze
    metrics = {
        "total_cost_usd": lambda t: t.efficiency.total_cost_usd,
        "input_tokens": lambda t: float(t.efficiency.input_tokens),
        "output_tokens": lambda t: float(t.efficiency.output_tokens),
        "duration_ms": lambda t: float(t.efficiency.duration_ms),
        "num_turns": lambda t: float(t.efficiency.num_turns),
        "mcp_tool_calls": lambda t: float(t.behavioral.mcp_tool_calls),
        "total_tool_calls": lambda t: float(t.behavioral.total_tool_calls),
    }

    # Add quality metrics if available
    has_quality = any(
        t.quality.composite_quality is not None
        for t in trials
    )
    if has_quality:
        metrics["composite_quality"] = lambda t: t.quality.composite_quality or 0.0
        metrics["fact_score"] = lambda t: t.quality.fact_score or 0.0
        metrics["structure_score"] = lambda t: t.quality.structure_score or 0.0

    # Analyze each metric
    per_metric = {}
    p_values_for_correction = []

    for metric_name, extractor in metrics.items():
        with_mcp_vals = []
        without_mcp_vals = []

        for (task_id, rep), pair in sorted(complete_pairs.items()):
            with_mcp_vals.append(extractor(pair["with-mcp"]))
            without_mcp_vals.append(extractor(pair["without-mcp"]))

        wm_stats = descriptive_stats(with_mcp_vals)
        wom_stats = descriptive_stats(without_mcp_vals)

        w_stat, p_val = wilcoxon_test(with_mcp_vals, without_mcp_vals)
        delta, effect_interp = cliffs_delta(with_mcp_vals, without_mcp_vals)
        med_diff, (ci_low, ci_high) = hodges_lehmann(with_mcp_vals, without_mcp_vals)

        per_metric[metric_name] = {
            "with_mcp": wm_stats,
            "without_mcp": wom_stats,
            "wilcoxon_W": round(w_stat, 4) if w_stat == w_stat else None,
            "wilcoxon_p": round(p_val, 6),
            "cliffs_delta": round(delta, 4),
            "effect_interpretation": effect_interp,
            "median_difference": round(med_diff, 4),
            "ci_95": [round(ci_low, 4), round(ci_high, 4)],
        }

        p_values_for_correction.append((metric_name, p_val))

    # Holm-Bonferroni correction
    corrections = holm_bonferroni(p_values_for_correction, alpha)
    for name, raw_p, corrected_p, sig in corrections:
        per_metric[name]["p_corrected"] = round(corrected_p, 6)
        per_metric[name]["significant"] = sig

    # Per-task analysis
    per_task: dict[str, dict] = {}
    task_ids = sorted(set(k[0] for k in complete_pairs.keys()))

    for task_id in task_ids:
        task_pairs = {
            k: v for k, v in complete_pairs.items() if k[0] == task_id
        }

        # MCP engagement rate for with-mcp trials
        mcp_engagements = [
            v["with-mcp"].behavioral.mcp_engagement_rate
            for v in task_pairs.values()
        ]
        avg_engagement = sum(mcp_engagements) / len(mcp_engagements) if mcp_engagements else 0.0

        task_info: dict = {"mcp_engagement_rate": round(avg_engagement, 4)}

        if has_quality:
            q_with = [v["with-mcp"].quality.composite_quality or 0.0 for v in task_pairs.values()]
            q_without = [v["without-mcp"].quality.composite_quality or 0.0 for v in task_pairs.values()]
            task_info["quality_with_mcp"] = descriptive_stats(q_with)
            task_info["quality_without_mcp"] = descriptive_stats(q_without)
        else:
            task_info["quality_with_mcp"] = descriptive_stats([])
            task_info["quality_without_mcp"] = descriptive_stats([])

        per_task[task_id] = task_info

    # Summary
    all_costs = [t.efficiency.total_cost_usd for t in trials]
    mcp_trials = [t for t in trials if t.meta.condition == "with-mcp"]
    mcp_engaged = [t for t in mcp_trials if t.behavioral.mcp_tool_calls > 0]

    summary = {
        "total_trials": len(trials),
        "excluded_trials": sum(1 for _ in pairs if _ not in complete_pairs),
        "mcp_engagement_rate": len(mcp_engaged) / len(mcp_trials) if mcp_trials else 0.0,
        "total_cost_usd": round(sum(all_costs), 2),
    }

    # Generate reports
    config_report = {
        "model": config.model,
        "repetitions": config.repetitions,
        "tasks": config.tasks,
        "warm_up_reps": config.warm_up_reps,
        "cool_down_seconds": config.cool_down_seconds,
    }

    run_id = results_path.name
    report_json_path = results_path / "report.json"
    report_md_path = results_path / "report.md"

    generate_report_json(run_id, config_report, summary, per_metric, per_task, report_json_path)
    generate_report_md(run_id, config_report, summary, per_metric, per_task, report_md_path)

    click.echo(f"\nReports written:")
    click.echo(f"  {report_json_path}")
    click.echo(f"  {report_md_path}")

    # Print summary table
    click.echo(f"\n{'=' * 70}")
    click.echo(f"Analysis: {run_id} (alpha={alpha})")
    click.echo(f"{'=' * 70}")
    click.echo(f"{'Metric':<22} {'With MCP':>12} {'Without MCP':>14} {'Cliff d':>9} {'p(corr)':>10} {'Sig':>4}")
    click.echo("-" * 70)

    for name, data in per_metric.items():
        wm_med = data["with_mcp"]["median"]
        wom_med = data["without_mcp"]["median"]
        cd = data["cliffs_delta"]
        pc = data.get("p_corrected", data["wilcoxon_p"])
        sig = "*" if data.get("significant", False) else ""
        click.echo(f"{name:<22} {wm_med:>12.3f} {wom_med:>14.3f} {cd:>9.3f} {pc:>10.4f} {sig:>4}")


if __name__ == "__main__":
    main()
