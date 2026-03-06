"""Execute a single benchmark trial via claude -p subprocess."""

from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import time
from datetime import datetime, timezone
from pathlib import Path

from .models import (
    BenchConfig,
    ScheduledTrial,
    TrialArtifacts,
    TrialMeta,
    TrialResult,
)
from .ndjson_parser import parse_ndjson
from .rubric_loader import load_task_prompt


_MCP_GUIDANCE_PREFIX = (
    "You have access to a Mycelium MCP server with semantic search over "
    "Envoy Gateway v1.3.0 documentation and Go API types. Before answering, "
    "use the `search` and `search_code` tools to look up exact type definitions, "
    "field names, and configuration details from the indexed source. Verify your "
    "response against the indexed documentation rather than relying solely on "
    "your training knowledge.\n\n"
)


def run_trial(
    trial: ScheduledTrial,
    config: BenchConfig,
    bench_dir: Path,
    results_dir: Path,
    run_id: str,
    tasks_dir: Path,
) -> TrialResult:
    """Execute a single trial and return the result.

    - Without-MCP: run in a temp dir with no .mcp.json
    - With-MCP: run in bench_dir with --mcp-config .mcp.json
    """
    task_prompt = load_task_prompt(trial.task_id, tasks_dir)

    # For with-mcp trials, prepend guidance to encourage tool usage.
    if trial.condition == "with-mcp":
        task_prompt = _MCP_GUIDANCE_PREFIX + task_prompt

    # Build output paths
    task_dir = results_dir / trial.task_id
    task_dir.mkdir(parents=True, exist_ok=True)
    suffix = f"{trial.condition}-{trial.repetition}"
    ndjson_path = task_dir / f"{suffix}.ndjson"
    response_path = task_dir / f"{suffix}.md"
    tools_path = task_dir / f"{suffix}-tools.json"
    transcript_path = task_dir / f"{suffix}-transcript.md"

    # Build command
    cmd = [
        "claude",
        "-p",
        task_prompt,
        "--output-format",
        "stream-json",
        "--verbose",
        "--dangerously-skip-permissions",
        "--model",
        config.model,
    ]

    if trial.condition == "with-mcp":
        mcp_config = bench_dir / ".mcp.json"
        cmd.extend(["--mcp-config", str(mcp_config)])
        cwd = str(bench_dir)
    else:
        # Use a temp dir with no MCP config
        cwd = None  # set below

    # Build env: unset CLAUDECODE and ANTHROPIC_API_KEY
    # Add mycelium project root to PATH so mctl is available to MCP server
    env = os.environ.copy()
    env.pop("CLAUDECODE", None)
    env.pop("ANTHROPIC_API_KEY", None)
    mycelium_root = str(bench_dir.parent.parent)
    env["PATH"] = mycelium_root + os.pathsep + env.get("PATH", "")

    timestamp_start = datetime.now(timezone.utc).isoformat()

    # Execute with retries
    ndjson_content = _execute_with_retries(
        cmd, cwd, env, config, trial, bench_dir
    )

    timestamp_end = datetime.now(timezone.utc).isoformat()

    # Write NDJSON
    ndjson_path.write_text(ndjson_content)

    # Parse NDJSON
    efficiency, behavioral, response_text = parse_ndjson(ndjson_path)

    # Write response markdown
    response_path.write_text(response_text)

    # Write tools JSON (extract from NDJSON)
    tools_data = _extract_tools(ndjson_content)
    tools_path.write_text(json.dumps(tools_data, indent=2))

    # Write transcript
    transcript = _generate_transcript(ndjson_content)
    transcript_path.write_text(transcript)

    # Relative artifact paths
    rel_prefix = f"{trial.task_id}/{suffix}"
    artifacts = TrialArtifacts(
        ndjson=f"{rel_prefix}.ndjson",
        response=f"{rel_prefix}.md",
        tools=f"{rel_prefix}-tools.json",
        transcript=f"{rel_prefix}-transcript.md",
    )

    meta = TrialMeta(
        run_id=run_id,
        task_id=trial.task_id,
        condition=trial.condition,
        repetition=trial.repetition,
        model=config.model,
        timestamp_start=timestamp_start,
        timestamp_end=timestamp_end,
        trial_order=trial.trial_order,
        condition_order=trial.condition_order,
    )

    return TrialResult(
        meta=meta,
        efficiency=efficiency,
        behavioral=behavioral,
        artifacts=artifacts,
    )


def _execute_with_retries(
    cmd: list[str],
    cwd: str | None,
    env: dict,
    config: BenchConfig,
    trial: ScheduledTrial,
    bench_dir: Path,
) -> str:
    """Execute command with exponential backoff on failure."""
    for attempt in range(config.max_retries + 1):
        try:
            if trial.condition == "without-mcp":
                with tempfile.TemporaryDirectory() as tmpdir:
                    result = subprocess.run(
                        cmd,
                        cwd=tmpdir,
                        env=env,
                        capture_output=True,
                        text=True,
                        timeout=600,
                    )
            else:
                result = subprocess.run(
                    cmd,
                    cwd=cwd,
                    env=env,
                    capture_output=True,
                    text=True,
                    timeout=600,
                )

            # Check for rate limiting in stderr
            if result.returncode != 0 and "429" in result.stderr:
                raise RuntimeError(f"Rate limited: {result.stderr[:200]}")

            if result.returncode != 0 and attempt < config.max_retries:
                raise RuntimeError(
                    f"Exit code {result.returncode}: {result.stderr[:200]}"
                )

            return result.stdout

        except (RuntimeError, subprocess.TimeoutExpired) as e:
            if attempt >= config.max_retries:
                print(
                    f"  FAILED after {config.max_retries + 1} attempts: {e}",
                    file=sys.stderr,
                )
                raise
            wait = config.retry_backoff_base * (2**attempt)
            print(
                f"  Retry {attempt + 1}/{config.max_retries} in {wait}s: {e}",
                file=sys.stderr,
            )
            time.sleep(wait)

    return ""  # unreachable


def _extract_tools(ndjson_content: str) -> list[dict]:
    """Extract tool call details from NDJSON content."""
    tool_uses = {}  # id -> {tool, input}
    tool_results = {}  # tool_use_id -> output

    for raw in ndjson_content.strip().splitlines():
        try:
            event = json.loads(raw)
        except json.JSONDecodeError:
            continue

        if event.get("type") == "assistant":
            for block in event.get("message", {}).get("content", []):
                if block.get("type") == "tool_use":
                    tool_uses[block["id"]] = {
                        "tool": block["name"],
                        "input": block.get("input", {}),
                    }
        elif event.get("type") == "user":
            for block in event.get("message", {}).get("content", []):
                if block.get("type") == "tool_result":
                    content = block.get("content", "")
                    if isinstance(content, list):
                        content = "\n".join(
                            b.get("text", "") for b in content
                        )
                    elif not isinstance(content, str):
                        content = json.dumps(content)
                    tool_results[block.get("tool_use_id", "")] = content

    result = []
    for tid, info in tool_uses.items():
        output = tool_results.get(tid, "")
        result.append(
            {
                "tool": info["tool"],
                "input": info["input"],
                "output_length": len(output),
                "output_preview": output[:500] + "..." if len(output) > 500 else output,
            }
        )
    return result


def _generate_transcript(ndjson_content: str) -> str:
    """Generate a human-readable transcript from NDJSON."""
    lines_out = ["# Conversation Transcript\n"]

    for raw in ndjson_content.strip().splitlines():
        try:
            event = json.loads(raw)
        except json.JSONDecodeError:
            continue

        if event.get("type") == "assistant":
            msg = event.get("message", {})
            for block in msg.get("content", []):
                if block.get("type") == "text":
                    text = block.get("text", "").strip()
                    if text:
                        lines_out.append(f"## Assistant\n\n{text}\n")
                elif block.get("type") == "tool_use":
                    name = block.get("name", "")
                    lines_out.append(f"**Tool call:** `{name}`\n")
        elif event.get("type") == "result":
            result_text = event.get("result", "")
            if result_text:
                lines_out.append(f"## Final Response\n\n{result_text}\n")

    return "\n".join(lines_out)
