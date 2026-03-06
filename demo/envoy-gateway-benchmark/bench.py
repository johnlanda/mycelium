#!/usr/bin/env python3
"""Benchmark runner entry point (Phase 2).

Usage:
    python bench.py [--config config.yaml] [--dry-run] [--resume RUN_ID]
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

import click
import yaml

from benchlib.models import BenchConfig, TrialResult
from benchlib.runner import run_trial
from benchlib.scheduler import generate_schedule


BENCH_DIR = Path(__file__).parent.resolve()
TASKS_DIR = BENCH_DIR / "tasks"
# mctl lives in the mycelium project root (two levels up from demo/envoy-gateway-benchmark/)
MCTL_BIN = BENCH_DIR.parent.parent / "mctl"


def _find_binary(name: str) -> str:
    """Find a binary on PATH or at known project locations."""
    if name == "mctl" and MCTL_BIN.exists():
        return str(MCTL_BIN)
    found = shutil.which(name)
    if found:
        return found
    return ""


def _clean_env() -> dict:
    """Return a clean env with CLAUDECODE and ANTHROPIC_API_KEY removed."""
    env = os.environ.copy()
    env.pop("CLAUDECODE", None)
    env.pop("ANTHROPIC_API_KEY", None)
    # Add mycelium project root to PATH for mctl
    mycelium_root = str(BENCH_DIR.parent.parent)
    env["PATH"] = mycelium_root + os.pathsep + env.get("PATH", "")
    return env


def _preflight_checks() -> None:
    """Verify required tools are available."""
    for cmd in ["claude", "mctl", "ollama"]:
        if not _find_binary(cmd):
            click.echo(f"ERROR: '{cmd}' not found on PATH.", err=True)
            sys.exit(1)

    # Check claude auth (must unset CLAUDECODE to avoid nested-session error)
    env = _clean_env()
    result = subprocess.run(
        [_find_binary("claude"), "auth", "status"],
        capture_output=True,
        text=True,
        env=env,
    )
    if '"loggedIn": true' not in result.stdout:
        click.echo("ERROR: Not logged in to Claude. Run: claude auth login", err=True)
        sys.exit(1)


def _load_config(config_path: Path) -> BenchConfig:
    with open(config_path) as f:
        data = yaml.safe_load(f)
    return BenchConfig(**data)


def _load_completed_trials(results_dir: Path) -> set[str]:
    """Find already-completed trial keys for resume support."""
    completed = set()
    if not results_dir.exists():
        return completed
    for task_dir in results_dir.iterdir():
        if not task_dir.is_dir():
            continue
        for json_file in task_dir.glob("*.json"):
            # Parse trial key from filename: {condition}-{rep}.json
            stem = json_file.stem
            if "-tools" in stem or "-transcript" in stem:
                continue
            task_id = task_dir.name
            completed.add(f"{task_id}/{stem}")
    return completed


@click.command()
@click.option("--config", "config_path", default="config.yaml", type=click.Path(exists=True))
@click.option("--dry-run", is_flag=True, help="Print schedule and exit")
@click.option("--resume", "resume_run_id", default=None, help="Resume a previous run")
def main(config_path: str, dry_run: bool, resume_run_id: str | None) -> None:
    config = _load_config(Path(config_path))

    if not dry_run:
        _preflight_checks()

    # Generate or resume run
    if resume_run_id:
        run_id = resume_run_id
    else:
        run_id = datetime.now().strftime("%Y%m%d-%H%M%S")

    results_dir = BENCH_DIR / "results" / run_id

    # Generate schedule
    schedule = generate_schedule(config)

    if dry_run:
        click.echo(f"Run ID: {run_id}")
        click.echo(f"Total trials: {len(schedule)}")
        click.echo(f"Warmup trials: {sum(1 for t in schedule if t.is_warmup)}")
        click.echo(f"Data trials: {sum(1 for t in schedule if not t.is_warmup)}")
        click.echo(f"\nSchedule:")
        for t in schedule:
            warmup = " [WARMUP]" if t.is_warmup else ""
            click.echo(
                f"  #{t.trial_order:3d}  {t.task_id:15s}  {t.condition:12s}  "
                f"rep={t.repetition}  order={t.condition_order}{warmup}"
            )
        return

    # Sync store
    mctl = _find_binary("mctl")
    click.echo("==> Syncing mycelium store (mctl up)...")
    subprocess.run([mctl, "up"], cwd=str(BENCH_DIR), env=_clean_env(), check=True)
    click.echo("    Store synced.")

    # Save schedule and config
    results_dir.mkdir(parents=True, exist_ok=True)
    schedule_path = results_dir / "schedule.json"
    schedule_path.write_text(
        json.dumps([t.model_dump() for t in schedule], indent=2)
    )
    config_save_path = results_dir / "config.yaml"
    config_save_path.write_text(Path(config_path).read_text())

    # Load completed trials for resume
    completed = _load_completed_trials(results_dir) if resume_run_id else set()
    if completed:
        click.echo(f"==> Resuming: {len(completed)} trials already complete")

    # Execute trials
    total = len(schedule)
    mcp_engagement_warnings = []

    for i, trial in enumerate(schedule):
        trial_key = f"{trial.task_id}/{trial.condition}-{trial.repetition}"

        if trial_key in completed:
            click.echo(f"[{i + 1}/{total}] SKIP (done) {trial_key}")
            continue

        warmup_tag = " [WARMUP]" if trial.is_warmup else ""
        click.echo(
            f"[{i + 1}/{total}] Running {trial.task_id} "
            f"{trial.condition} rep={trial.repetition}{warmup_tag}..."
        )

        try:
            result = run_trial(
                trial=trial,
                config=config,
                bench_dir=BENCH_DIR,
                results_dir=results_dir,
                run_id=run_id,
                tasks_dir=TASKS_DIR,
            )

            # Save per-trial JSON
            trial_json_path = (
                results_dir / trial.task_id / f"{trial.condition}-{trial.repetition}.json"
            )
            trial_json_path.write_text(
                json.dumps(result.model_dump(), indent=2)
            )

            # Print summary
            e = result.efficiency
            b = result.behavioral
            click.echo(
                f"         cost=${e.total_cost_usd:.3f}  "
                f"tokens={e.input_tokens + e.output_tokens}  "
                f"duration={e.duration_ms}ms  "
                f"mcp_calls={b.mcp_tool_calls}  "
                f"turns={e.num_turns}"
            )

            # MCP engagement warning
            if trial.condition == "with-mcp" and b.mcp_tool_calls == 0 and not trial.is_warmup:
                mcp_engagement_warnings.append(trial_key)
                click.echo("         WARNING: Zero MCP engagement!")

        except Exception as exc:
            click.echo(f"         FAILED: {exc}", err=True)

        # Cool-down between trials
        if i < total - 1:
            click.echo(f"         Cooling down {config.cool_down_seconds}s...")
            time.sleep(config.cool_down_seconds)

    # Print summary
    click.echo("\n" + "=" * 60)
    click.echo(f"Benchmark complete: {run_id}")
    click.echo(f"Results: {results_dir}")

    if mcp_engagement_warnings:
        click.echo(
            f"\nWARNING: {len(mcp_engagement_warnings)} with-mcp trials had zero MCP engagement:"
        )
        for w in mcp_engagement_warnings:
            click.echo(f"  - {w}")


if __name__ == "__main__":
    main()
