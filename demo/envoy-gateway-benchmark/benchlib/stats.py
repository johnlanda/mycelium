"""Statistical analysis: Wilcoxon, Cliff's delta, Hodges-Lehmann, Holm-Bonferroni."""

from __future__ import annotations

from itertools import combinations

import numpy as np
from scipy import stats


def wilcoxon_test(
    x: list[float], y: list[float]
) -> tuple[float, float]:
    """Wilcoxon signed-rank test on paired samples.

    Returns (W_statistic, p_value).
    Falls back to (nan, 1.0) if test cannot be performed.
    """
    diffs = [a - b for a, b in zip(x, y)]
    # Remove zero differences (ties)
    nonzero = [d for d in diffs if d != 0]
    if len(nonzero) < 1:
        return float("nan"), 1.0
    try:
        result = stats.wilcoxon(nonzero)
        return float(result.statistic), float(result.pvalue)
    except ValueError:
        return float("nan"), 1.0


def cliffs_delta(x: list[float], y: list[float]) -> tuple[float, str]:
    """Compute Cliff's delta effect size.

    Returns (delta, interpretation).
    Interpretation thresholds per Romano et al. (2006):
      |d| < 0.147: negligible
      |d| < 0.330: small
      |d| < 0.474: medium
      else:        large
    """
    n_x, n_y = len(x), len(y)
    if n_x == 0 or n_y == 0:
        return 0.0, "negligible"

    more = sum(1 for xi in x for yi in y if xi > yi)
    less = sum(1 for xi in x for yi in y if xi < yi)
    delta = (more - less) / (n_x * n_y)

    abs_d = abs(delta)
    if abs_d < 0.147:
        interp = "negligible"
    elif abs_d < 0.330:
        interp = "small"
    elif abs_d < 0.474:
        interp = "medium"
    else:
        interp = "large"

    return delta, interp


def hodges_lehmann(
    x: list[float], y: list[float]
) -> tuple[float, tuple[float, float]]:
    """Hodges-Lehmann estimator for median difference with 95% CI.

    Returns (median_diff, (ci_low, ci_high)).
    """
    diffs = [a - b for a, b in zip(x, y)]
    n = len(diffs)
    if n == 0:
        return 0.0, (0.0, 0.0)

    # Walsh averages: (d_i + d_j) / 2 for all i <= j
    walsh = []
    for i in range(n):
        for j in range(i, n):
            walsh.append((diffs[i] + diffs[j]) / 2.0)

    walsh.sort()
    median_diff = float(np.median(walsh))

    # 95% CI from quantiles of Walsh averages
    if len(walsh) >= 2:
        ci_low = float(np.percentile(walsh, 2.5))
        ci_high = float(np.percentile(walsh, 97.5))
    else:
        ci_low = ci_high = median_diff

    return median_diff, (ci_low, ci_high)


def holm_bonferroni(
    p_values: list[tuple[str, float]], alpha: float = 0.05
) -> list[tuple[str, float, float, bool]]:
    """Apply Holm-Bonferroni correction to multiple p-values.

    Input: list of (metric_name, p_value)
    Returns: list of (metric_name, raw_p, corrected_p, significant)

    Corrected p-values are adjusted such that comparing to alpha gives
    the same result as the step-down procedure.
    """
    k = len(p_values)
    if k == 0:
        return []

    # Sort by p-value ascending
    sorted_pv = sorted(p_values, key=lambda x: x[1])

    results = []
    max_corrected = 0.0
    for i, (name, p) in enumerate(sorted_pv):
        corrected = p * (k - i)
        # Enforce monotonicity
        corrected = max(corrected, max_corrected)
        corrected = min(corrected, 1.0)
        max_corrected = corrected
        results.append((name, p, corrected, corrected <= alpha))

    return results


def descriptive_stats(values: list[float]) -> dict:
    """Compute descriptive statistics."""
    if not values:
        return {"median": 0.0, "iqr": [0.0, 0.0], "mean": 0.0, "std": 0.0, "n": 0}

    arr = np.array(values)
    return {
        "median": float(np.median(arr)),
        "iqr": [float(np.percentile(arr, 25)), float(np.percentile(arr, 75))],
        "mean": float(np.mean(arr)),
        "std": float(np.std(arr, ddof=1)) if len(arr) > 1 else 0.0,
        "n": len(arr),
    }
