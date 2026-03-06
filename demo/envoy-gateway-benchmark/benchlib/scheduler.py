"""Generate a randomized trial schedule for benchmark execution."""

from __future__ import annotations

import random

from .models import BenchConfig, ScheduledTrial


def generate_schedule(config: BenchConfig) -> list[ScheduledTrial]:
    """Generate a deterministic randomized trial schedule.

    Logic:
    - Warm-up block first (rep=0, all tasks, both conditions, shuffled).
    - Then n repetition blocks. Within each (task, rep) pair, randomly assign
      which condition runs first.
    - Deterministic given seed.
    """
    seed = config.seed if config.seed is not None else random.randint(0, 2**31)
    rng = random.Random(seed)

    trials: list[ScheduledTrial] = []
    order = 0

    # Warm-up block (rep=0)
    warmup_pairs: list[tuple[str, str]] = []
    for task_id in config.tasks:
        for cond in ["with-mcp", "without-mcp"]:
            warmup_pairs.append((task_id, cond))
    rng.shuffle(warmup_pairs)
    for task_id, cond in warmup_pairs:
        trials.append(
            ScheduledTrial(
                trial_order=order,
                task_id=task_id,
                condition=cond,
                repetition=0,
                is_warmup=True,
                condition_order="warmup",
            )
        )
        order += 1

    # Main repetition blocks
    for rep in range(1, config.repetitions + 1):
        # Shuffle task order within this rep
        task_order = list(config.tasks)
        rng.shuffle(task_order)

        for task_id in task_order:
            # Randomly assign condition order for this (task, rep) pair
            mcp_first = rng.random() < 0.5
            if mcp_first:
                conditions = ["with-mcp", "without-mcp"]
                cond_label = "mcp-first"
            else:
                conditions = ["without-mcp", "with-mcp"]
                cond_label = "baseline-first"

            for cond in conditions:
                trials.append(
                    ScheduledTrial(
                        trial_order=order,
                        task_id=task_id,
                        condition=cond,
                        repetition=rep,
                        is_warmup=False,
                        condition_order=cond_label,
                    )
                )
                order += 1

    return trials
