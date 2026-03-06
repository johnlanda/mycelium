"""Tier 2: YAML validation, apiVersion/kind checks, required field checks."""

from __future__ import annotations

import re

import yaml

from .models import Rubric


def score_structure(response: str, rubric: Rubric) -> float:
    """Compute structure score as proportion of checks passing."""
    if not rubric.structure_checks:
        return 1.0

    passed = 0
    total = len(rubric.structure_checks)

    # Extract code blocks from response
    code_blocks = _extract_code_blocks(response)
    yaml_blocks = _extract_yaml_blocks(response)

    for check in rubric.structure_checks:
        if _run_check(check.type, check, code_blocks, yaml_blocks, response):
            passed += 1

    return passed / total if total > 0 else 0.0


def _extract_code_blocks(text: str) -> list[str]:
    """Extract all fenced code blocks from markdown."""
    pattern = r"```(?:\w*)\n(.*?)```"
    return re.findall(pattern, text, re.DOTALL)


def _extract_yaml_blocks(text: str) -> list[str]:
    """Extract YAML code blocks from markdown."""
    pattern = r"```(?:ya?ml)\n(.*?)```"
    return re.findall(pattern, text, re.DOTALL)


def _run_check(
    check_type: str,
    check,
    code_blocks: list[str],
    yaml_blocks: list[str],
    full_response: str,
) -> bool:
    """Run a single structure check."""
    if check_type == "contains_code_block":
        return len(code_blocks) > 0

    elif check_type == "no_yaml_errors":
        if not yaml_blocks:
            # No YAML blocks to validate — treat as pass if applies_to yaml
            return True
        for block in yaml_blocks:
            try:
                # Handle multi-document YAML
                list(yaml.safe_load_all(block))
            except yaml.YAMLError:
                return False
        return True

    elif check_type == "api_version":
        expected = check.expected or ""
        for block in yaml_blocks:
            try:
                for doc in yaml.safe_load_all(block):
                    if isinstance(doc, dict) and doc.get("apiVersion") == expected:
                        return True
            except yaml.YAMLError:
                continue
        return False

    elif check_type == "kind":
        expected = check.expected or ""
        for block in yaml_blocks:
            try:
                for doc in yaml.safe_load_all(block):
                    if isinstance(doc, dict) and doc.get("kind") == expected:
                        return True
            except yaml.YAMLError:
                continue
        return False

    elif check_type == "required_field":
        field_path = check.field or ""
        for block in yaml_blocks:
            try:
                for doc in yaml.safe_load_all(block):
                    if isinstance(doc, dict) and _has_field(doc, field_path):
                        return True
            except yaml.YAMLError:
                continue
        return False

    return False


def _has_field(doc: dict, field_path: str) -> bool:
    """Check if a nested field path exists in a dict."""
    parts = field_path.split(".")
    current = doc
    for part in parts:
        if isinstance(current, dict) and part in current:
            current = current[part]
        else:
            return False
    return True
