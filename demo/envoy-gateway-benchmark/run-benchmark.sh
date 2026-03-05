#!/usr/bin/env bash
set -euo pipefail

# Allow running from within a Claude Code session
unset CLAUDECODE 2>/dev/null || true

# Force Claude Max subscription auth (avoid API key billing)
unset ANTHROPIC_API_KEY 2>/dev/null || true

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# ─── Preflight checks ────────────────────────────────────────────────────────

check_cmd() {
  if ! command -v "$1" &>/dev/null; then
    echo "ERROR: '$1' is not installed or not on PATH." >&2
    exit 1
  fi
}

echo "==> Preflight checks"
check_cmd claude
check_cmd mctl
check_cmd ollama
check_cmd jq

# Verify logged-in subscription (Claude Max)
if ! claude auth status 2>&1 | grep -q '"loggedIn": true'; then
  echo "ERROR: Not logged in to Claude. Run: claude auth login" >&2
  exit 1
fi

# Check that the embedding model is pulled
if ! ollama list 2>/dev/null | grep -q "qwen3-embedding"; then
  echo "ERROR: ollama model 'qwen3-embedding' not found. Run: ollama pull qwen3-embedding" >&2
  exit 1
fi

echo "    All checks passed."

# ─── Ensure store is synced ───────────────────────────────────────────────────

echo "==> Syncing mycelium store (mctl up)..."
mctl up
echo "    Store synced."

# ─── Prepare results directory ────────────────────────────────────────────────

TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
RESULTS_DIR="results/${TIMESTAMP}"
mkdir -p "$RESULTS_DIR"

echo "==> Results will be saved to: ${RESULTS_DIR}"

# ─── NDJSON parsing helpers ───────────────────────────────────────────────────

# Extract summary metrics from an NDJSON stream-json file.
# Produces a JSON object with key metrics.
extract_summary() {
  local ndjson_file="$1"

  # Get the result line
  local result_json
  result_json=$(jq -s '[.[] | select(.type == "result")] | last' < "$ndjson_file")

  # Count MCP tool calls from assistant messages
  local mcp_calls
  mcp_calls=$(jq -s '[.[] | select(.type == "assistant") | .message.content[]? | select(.type == "tool_use") | select(.name | startswith("mcp__"))] | length' < "$ndjson_file")

  # List unique MCP tools used
  local mcp_tools
  mcp_tools=$(jq -s '[.[] | select(.type == "assistant") | .message.content[]? | select(.type == "tool_use") | select(.name | startswith("mcp__")) | .name] | unique' < "$ndjson_file")

  # Count permission denials (from result)
  local denials
  denials=$(echo "$result_json" | jq '[.permission_denials // [] | .[]?] | length' 2>/dev/null || echo "0")

  # Build summary JSON
  jq -n \
    --argjson result "$result_json" \
    --argjson mcp_calls "$mcp_calls" \
    --argjson mcp_tools "$mcp_tools" \
    --argjson denials "$denials" \
    '{
      num_turns: ($result.num_turns // 0),
      duration_ms: ($result.duration_ms // 0),
      total_cost_usd: ($result.total_cost_usd // 0),
      input_tokens: (($result.usage.input_tokens // 0) + ($result.usage.cache_read_input_tokens // 0) + ($result.usage.cache_creation_input_tokens // 0)),
      output_tokens: ($result.usage.output_tokens // 0),
      mcp_tool_calls: $mcp_calls,
      mcp_tools_used: $mcp_tools,
      permission_denials: $denials,
      model: ($result.model // "unknown"),
      session_id: ($result.session_id // "")
    }'
}

# Extract tool call details from an NDJSON stream-json file.
# Produces a JSON array of {tool, input, output_length, output_preview}.
extract_tools() {
  local ndjson_file="$1"

  # Collect tool_use blocks with their IDs
  local tool_uses
  tool_uses=$(jq -s '[.[] | select(.type == "assistant") | .message.content[]? | select(.type == "tool_use") | {id: .id, tool: .name, input: .input}]' < "$ndjson_file")

  # Collect tool results keyed by tool_use_id (tool results come as "user" messages with tool_result content blocks)
  local tool_results
  tool_results=$(jq -s '
    [.[] | select(.type == "user") | .message.content[]? | select(.type == "tool_result") |
      {id: .tool_use_id, output: (if .content | type == "array" then (.content | map(.text // "") | join("\n")) elif .content | type == "string" then .content else (.content | tostring) end)}
    ]' < "$ndjson_file")

  # Join tool uses with their results
  jq -n \
    --argjson uses "$tool_uses" \
    --argjson results "$tool_results" \
    '[
      $uses[] |
      . as $use |
      ($results | map(select(.id == $use.id)) | first // {output: ""}) as $res |
      {
        tool: $use.tool,
        input: $use.input,
        output_length: ($res.output | length),
        output_preview: (if ($res.output | length) > 500 then ($res.output[:500] + "...") else $res.output end)
      }
    ]'
}

# Generate a human-readable transcript from an NDJSON stream-json file.
generate_transcript() {
  local ndjson_file="$1"
  local output_file="$2"
  local turn_num=0

  : > "$output_file"
  echo "# Conversation Transcript" >> "$output_file"
  echo "" >> "$output_file"

  # Build an associative array of tool results keyed by tool_use_id
  # Tool results come as "user" messages with tool_result content blocks
  declare -A tool_result_map
  while IFS= read -r line; do
    local tr_id tr_output
    tr_id=$(echo "$line" | jq -r '.id // ""')
    tr_output=$(echo "$line" | jq -r '.output // ""')
    if [[ -n "$tr_id" ]]; then
      tool_result_map["$tr_id"]="$tr_output"
    fi
  done < <(jq -c '
    select(.type == "user") | .message.content[]? | select(.type == "tool_result") |
    {id: .tool_use_id, output: (if .content | type == "array" then (.content | map(.text // "") | join("\n")) elif .content | type == "string" then .content else (.content | tostring) end)}
  ' < "$ndjson_file")

  # Iterate through assistant messages in order
  while IFS= read -r event; do
    local etype
    etype=$(echo "$event" | jq -r '.type')

    if [[ "$etype" == "assistant" ]]; then
      turn_num=$((turn_num + 1))
      echo "## Turn ${turn_num} (assistant)" >> "$output_file"
      echo "" >> "$output_file"

      # Process each content block
      while IFS= read -r block; do
        local btype
        btype=$(echo "$block" | jq -r '.type')

        if [[ "$btype" == "text" ]]; then
          echo "$block" | jq -r '.text // ""' >> "$output_file"
          echo "" >> "$output_file"
        elif [[ "$btype" == "tool_use" ]]; then
          local tname tinput tid
          tname=$(echo "$block" | jq -r '.name')
          tinput=$(echo "$block" | jq -c '.input')
          tid=$(echo "$block" | jq -r '.id')

          echo "### Tool Call: ${tname}" >> "$output_file"
          echo "**Input:** \`${tinput}\`" >> "$output_file"

          # Look up result
          local tresult="${tool_result_map[$tid]:-}"
          if [[ -n "$tresult" ]]; then
            local tlen=${#tresult}
            local tpreview
            if (( tlen > 500 )); then
              tpreview="${tresult:0:500}..."
            else
              tpreview="$tresult"
            fi
            echo "**Output (${tlen} chars):** ${tpreview}" >> "$output_file"
          fi
          echo "" >> "$output_file"
        fi
      done < <(echo "$event" | jq -c '.message.content[]? // empty')
    fi
  done < <(jq -c 'select(.type == "assistant" or .type == "tool")' < "$ndjson_file")

  echo "" >> "$output_file"
  echo "---" >> "$output_file"
  echo "_Generated from stream-json NDJSON output._" >> "$output_file"
}

# ─── Run benchmark tasks ─────────────────────────────────────────────────────

TASKS=(
  "01-discovery"
  "02-codegen"
  "03-config"
  "04-debugging"
  "05-extproc"
)

# Collect summary data
declare -a SUMMARY_LINES

for task in "${TASKS[@]}"; do
  task_file="tasks/${task}.md"
  task_dir="${RESULTS_DIR}/${task}"
  mkdir -p "$task_dir"

  prompt="$(cat "$task_file")"
  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  Task: ${task}"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

  # ── Without MCP (clean baseline — no .mcp.json in CWD) ──────────────────────
  echo "  [without-mcp] Running..."
  without_start="$(date +%s)"

  # Use a clean temp directory so Claude cannot discover any .mcp.json
  tmpdir=$(mktemp -d)
  cp "$task_file" "$tmpdir/task.md"
  # Resolve task_dir to absolute path before cd-ing into tmpdir
  abs_task_dir="$(cd "$task_dir" && pwd)"
  (cd "$tmpdir" && claude -p "$(cat task.md)" --output-format stream-json --verbose \
    --dangerously-skip-permissions \
    > "$abs_task_dir/without-mcp.ndjson" 2>"$abs_task_dir/without-mcp.stderr") || true
  rm -rf "$tmpdir"

  without_end="$(date +%s)"
  without_elapsed=$(( without_end - without_start ))

  # Parse NDJSON: summary
  extract_summary "$task_dir/without-mcp.ndjson" > "$task_dir/without-mcp-summary.json" 2>/dev/null || \
    echo '{"error": "failed to extract summary"}' > "$task_dir/without-mcp-summary.json"

  # Parse NDJSON: tool calls
  extract_tools "$task_dir/without-mcp.ndjson" > "$task_dir/without-mcp-tools.json" 2>/dev/null || \
    echo '[]' > "$task_dir/without-mcp-tools.json"

  # Parse NDJSON: transcript
  generate_transcript "$task_dir/without-mcp.ndjson" "$task_dir/without-mcp-transcript.md" 2>/dev/null || true

  # Extract final response text from result event
  jq -s -r '[.[] | select(.type == "result")] | last | .result // "ERROR: could not extract response"' \
    "$task_dir/without-mcp.ndjson" > "$task_dir/without-mcp.md" 2>/dev/null || \
    echo "ERROR: failed to extract response" > "$task_dir/without-mcp.md"

  # Read metrics from summary
  without_input=$(jq -r '.input_tokens // "N/A"' "$task_dir/without-mcp-summary.json" 2>/dev/null || echo "N/A")
  without_output=$(jq -r '.output_tokens // "N/A"' "$task_dir/without-mcp-summary.json" 2>/dev/null || echo "N/A")
  without_cost_raw=$(jq -r '.total_cost_usd // "N/A"' "$task_dir/without-mcp-summary.json" 2>/dev/null || echo "N/A")
  without_cost=$(printf '%.2f' "$without_cost_raw" 2>/dev/null || echo "$without_cost_raw")
  without_mcp_calls=$(jq -r '.mcp_tool_calls // 0' "$task_dir/without-mcp-summary.json" 2>/dev/null || echo "0")
  without_denials=$(jq -r '.permission_denials // 0' "$task_dir/without-mcp-summary.json" 2>/dev/null || echo "0")

  echo "  [without-mcp] Done (${without_elapsed}s, in=${without_input}, out=${without_output}, cost=\$${without_cost}, mcp=${without_mcp_calls}, deny=${without_denials})"

  # ── With MCP ────────────────────────────────────────────────────────────────
  echo "  [with-mcp]    Running..."
  with_start="$(date +%s)"

  claude -p "$prompt" --output-format stream-json --verbose \
    --dangerously-skip-permissions --mcp-config .mcp.json \
    > "$task_dir/with-mcp.ndjson" 2>"$task_dir/with-mcp.stderr" || true

  with_end="$(date +%s)"
  with_elapsed=$(( with_end - with_start ))

  # Parse NDJSON: summary
  extract_summary "$task_dir/with-mcp.ndjson" > "$task_dir/with-mcp-summary.json" 2>/dev/null || \
    echo '{"error": "failed to extract summary"}' > "$task_dir/with-mcp-summary.json"

  # Parse NDJSON: tool calls
  extract_tools "$task_dir/with-mcp.ndjson" > "$task_dir/with-mcp-tools.json" 2>/dev/null || \
    echo '[]' > "$task_dir/with-mcp-tools.json"

  # Parse NDJSON: transcript
  generate_transcript "$task_dir/with-mcp.ndjson" "$task_dir/with-mcp-transcript.md" 2>/dev/null || true

  # Extract final response text from result event
  jq -s -r '[.[] | select(.type == "result")] | last | .result // "ERROR: could not extract response"' \
    "$task_dir/with-mcp.ndjson" > "$task_dir/with-mcp.md" 2>/dev/null || \
    echo "ERROR: failed to extract response" > "$task_dir/with-mcp.md"

  # Read metrics from summary
  with_input=$(jq -r '.input_tokens // "N/A"' "$task_dir/with-mcp-summary.json" 2>/dev/null || echo "N/A")
  with_output=$(jq -r '.output_tokens // "N/A"' "$task_dir/with-mcp-summary.json" 2>/dev/null || echo "N/A")
  with_cost_raw=$(jq -r '.total_cost_usd // "N/A"' "$task_dir/with-mcp-summary.json" 2>/dev/null || echo "N/A")
  with_cost=$(printf '%.2f' "$with_cost_raw" 2>/dev/null || echo "$with_cost_raw")
  with_mcp_calls=$(jq -r '.mcp_tool_calls // 0' "$task_dir/with-mcp-summary.json" 2>/dev/null || echo "0")
  with_denials=$(jq -r '.permission_denials // 0' "$task_dir/with-mcp-summary.json" 2>/dev/null || echo "0")

  echo "  [with-mcp]    Done (${with_elapsed}s, in=${with_input}, out=${with_output}, cost=\$${with_cost}, mcp=${with_mcp_calls}, deny=${with_denials})"

  # Store summary line (enhanced with MCP + deny columns)
  SUMMARY_LINES+=("$(printf "%-14s │ %6s │ %5s │ %5ss │ %6s │ %3s │ %4s │ %6s │ %5s │ %5ss │ %6s │ %3s │ %4s" \
    "$task" "$without_input" "$without_output" "$without_elapsed" "\$${without_cost}" "$without_mcp_calls" "$without_denials" \
    "$with_input" "$with_output" "$with_elapsed" "\$${with_cost}" "$with_mcp_calls" "$with_denials")")
done

# ─── Print summary table ─────────────────────────────────────────────────────

echo ""
echo ""

HEADER_W="WITHOUT MCP"
HEADER_M="WITH MCP"

echo "╔═══════════════════════════════════════════════════════════════════════════════════════════════════════════════════╗"
echo "║                                        BENCHMARK RESULTS SUMMARY                                                ║"
echo "╠═══════════════════════════════════════════════════════════════════════════════════════════════════════════════════╣"
printf "║ %-14s │ %-43s │ %-43s ║\n" "" "$HEADER_W" "$HEADER_M"
printf "║ %-14s │ %6s │ %5s │ %5s │ %7s │ %3s │ %4s │ %6s │ %5s │ %5s │ %7s │ %3s │ %4s ║\n" \
  "Task" "In Tok" "Out" "Time" "Cost" "MCP" "Deny" "In Tok" "Out" "Time" "Cost" "MCP" "Deny"
echo "╟────────────────┼────────┼───────┼───────┼─────────┼─────┼──────┼────────┼───────┼───────┼─────────┼─────┼──────╢"

for line in "${SUMMARY_LINES[@]}"; do
  echo "║ ${line} ║"
done

echo "╚═══════════════════════════════════════════════════════════════════════════════════════════════════════════════════╝"
echo ""
echo "Raw results: ${RESULTS_DIR}/"
echo ""
echo "Per-task outputs:"
echo "  Summary:    ${RESULTS_DIR}/<task>/<mode>-summary.json"
echo "  Tool calls: ${RESULTS_DIR}/<task>/<mode>-tools.json"
echo "  Transcript: ${RESULTS_DIR}/<task>/<mode>-transcript.md"
echo "  Response:   ${RESULTS_DIR}/<task>/<mode>.md"
echo ""
echo "Compare outputs:  diff ${RESULTS_DIR}/<task>/without-mcp.md ${RESULTS_DIR}/<task>/with-mcp.md"
echo "Compare tools:    diff ${RESULTS_DIR}/<task>/without-mcp-tools.json ${RESULTS_DIR}/<task>/with-mcp-tools.json"
