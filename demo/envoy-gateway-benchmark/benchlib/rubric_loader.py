"""Load and validate rubric YAML files and reference answers."""

from __future__ import annotations

from pathlib import Path

import yaml

from .models import Rubric


def load_rubric(rubric_path: Path) -> Rubric:
    """Load a rubric YAML file and return a validated Rubric model."""
    with open(rubric_path) as f:
        data = yaml.safe_load(f)
    return Rubric(**data)


def load_reference(rubric: Rubric, tasks_dir: Path) -> str:
    """Load the reference answer for a rubric."""
    if not rubric.reference_answer_file:
        return ""
    ref_path = tasks_dir / Path(rubric.reference_answer_file).name
    if not ref_path.exists():
        return ""
    return ref_path.read_text()


def load_task_prompt(task_id: str, tasks_dir: Path) -> str:
    """Load the task prompt markdown file."""
    prompt_path = tasks_dir / f"{task_id}.md"
    if not prompt_path.exists():
        raise FileNotFoundError(f"Task prompt not found: {prompt_path}")
    return prompt_path.read_text().strip()


def load_all_rubrics(tasks_dir: Path, task_ids: list[str]) -> dict[str, Rubric]:
    """Load all rubrics for the given task IDs."""
    rubrics = {}
    for task_id in task_ids:
        rubric_path = tasks_dir / f"{task_id}.rubric.yaml"
        if rubric_path.exists():
            rubrics[task_id] = load_rubric(rubric_path)
    return rubrics
