#!/usr/bin/env python3
"""Quality evaluation entry point (Phase 3).

Usage:
    python evaluate.py results/{run_id}/ [--tasks-dir tasks/] [--force]
"""

from __future__ import annotations

import json
from pathlib import Path

import click
import yaml

from benchlib.fact_scorer import score_facts
from benchlib.judge_scorer import score_judge
from benchlib.models import BenchConfig, TrialResult
from benchlib.rubric_loader import load_all_rubrics, load_reference, load_task_prompt
from benchlib.structure_scorer import score_structure


BENCH_DIR = Path(__file__).parent.resolve()


@click.command()
@click.argument("results_dir", type=click.Path(exists=True))
@click.option("--tasks-dir", default="tasks/", type=click.Path(exists=True))
@click.option("--force", is_flag=True, help="Re-evaluate even if quality scores exist")
@click.option("--skip-judge", is_flag=True, help="Skip LLM judge scoring (faster)")
def main(results_dir: str, tasks_dir: str, force: bool, skip_judge: bool) -> None:
    results_path = Path(results_dir).resolve()
    tasks_path = Path(tasks_dir).resolve()

    # Load config from results dir
    config_path = results_path / "config.yaml"
    if config_path.exists():
        with open(config_path) as f:
            config = BenchConfig(**yaml.safe_load(f))
    else:
        config = BenchConfig()

    # Discover task IDs from result directories
    task_ids = sorted(
        d.name
        for d in results_path.iterdir()
        if d.is_dir() and not d.name.startswith(".")
    )

    # Load rubrics
    rubrics = load_all_rubrics(tasks_path, task_ids)
    click.echo(f"Loaded {len(rubrics)} rubrics for tasks: {', '.join(rubrics.keys())}")

    # Find all trial JSON files
    trial_files = []
    for task_id in task_ids:
        task_dir = results_path / task_id
        for json_file in sorted(task_dir.glob("*.json")):
            stem = json_file.stem
            if "-tools" in stem or "-transcript" in stem:
                continue
            trial_files.append(json_file)

    click.echo(f"Found {len(trial_files)} trial files")

    evaluated = 0
    skipped = 0

    for trial_file in trial_files:
        # Load trial
        trial_data = json.loads(trial_file.read_text())
        trial = TrialResult(**trial_data)

        # Skip warmup (rep=0)
        if trial.meta.repetition == 0:
            skipped += 1
            continue

        # Skip if already evaluated (unless --force)
        if trial.quality.fact_score is not None and not force:
            click.echo(f"  SKIP (scored) {trial_file.stem}")
            skipped += 1
            continue

        task_id = trial.meta.task_id
        rubric = rubrics.get(task_id)
        if not rubric:
            click.echo(f"  SKIP (no rubric) {task_id}/{trial_file.stem}")
            skipped += 1
            continue

        # Load response text
        response_path = results_path / trial.artifacts.response
        if not response_path.exists():
            click.echo(f"  SKIP (no response) {trial_file.stem}")
            skipped += 1
            continue
        response_text = response_path.read_text()

        click.echo(f"  Evaluating {task_id}/{trial_file.stem}...")

        # Tier 1: Fact score
        fact = score_facts(response_text, rubric)

        # Tier 2: Structure score
        structure = score_structure(response_text, rubric)

        # Tier 3: Judge score
        judge_score = None
        judge_reasoning = None
        if not skip_judge and rubric.judge_criteria:
            try:
                task_prompt = load_task_prompt(task_id, tasks_path)
                reference = load_reference(rubric, tasks_path)
                judge_score, judge_reasoning = score_judge(
                    response=response_text,
                    task_prompt=task_prompt,
                    reference_answer=reference,
                    judge_criteria=rubric.judge_criteria,
                    config=config,
                )
            except Exception as e:
                click.echo(f"    Judge error: {e}")

        # Composite quality
        w = config.quality_weights
        composite = w.fact * fact + w.structure * structure
        if judge_score is not None:
            judge_normalized = (judge_score - 1) / 4.0
            composite += w.judge * judge_normalized
        else:
            # Redistribute judge weight to fact and structure
            composite = composite / (w.fact + w.structure) if (w.fact + w.structure) > 0 else 0.0

        # Update trial quality
        trial.quality.fact_score = round(fact, 4)
        trial.quality.structure_score = round(structure, 4)
        trial.quality.judge_score = judge_score
        trial.quality.judge_reasoning = judge_reasoning
        trial.quality.composite_quality = round(composite, 4)

        # Write back
        trial_file.write_text(json.dumps(trial.model_dump(), indent=2))

        click.echo(
            f"    fact={fact:.3f}  structure={structure:.3f}  "
            f"judge={judge_score or 'N/A'}  composite={composite:.3f}"
        )
        evaluated += 1

    click.echo(f"\nDone: {evaluated} evaluated, {skipped} skipped")


if __name__ == "__main__":
    main()
