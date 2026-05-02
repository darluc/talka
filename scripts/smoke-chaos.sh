#!/bin/sh
set -eu

asr_output="$(./scripts/smoke-real-asr.sh --fixture fixtures/audio/zh-short.wav --crash-midstream)"
printf '%s\n' "$asr_output"
printf '%s\n' "$asr_output" | grep -q '^EXPECTED_ERROR asr_runtime_unavailable '
printf 'CHAOS_OK case=asr_crash\n'

ollama_output="$(./scripts/smoke-fake-e2e.sh --full-session --llm-timeout)"
printf '%s\n' "$ollama_output"
printf '%s\n' "$ollama_output" | grep -q '^CLEANUP_STATUS fallback_timeout$'
printf '%s\n' "$ollama_output" | grep -q '^INSERT_OK target=fake status=inserted$'
printf 'CHAOS_OK case=ollama_timeout\n'

if network_output="$(./scripts/qa-physical-ios.sh --scenario network-interruption 2>&1)"; then
  printf '%s\n' "$network_output" >&2
  printf 'expected physical network-interruption QA to block without real device evidence\n' >&2
  exit 1
else
  network_status="$?"
fi
printf '%s\n' "$network_output"
test "$network_status" -ne 0
printf '%s\n' "$network_output" | grep -q 'marker=backpressure_recovery'
printf '%s\n' "$network_output" | grep -q '^QA_RESULT status=blocked scenario=network-interruption '
printf 'CHAOS_OK case=network_interruption_automated status=covered marker=backpressure_recovery\n'
printf 'CHAOS_BLOCKED case=network_interruption_physical status=blocked reason=physical_agent_required\n'

paste_output="$(./scripts/qa-paste-matrix.sh --simulate-permission-denied)"
printf '%s\n' "$paste_output"
printf '%s\n' "$paste_output" | grep -q '^EXPECTED_ERROR accessibility_missing$'
printf '%s\n' "$paste_output" | grep -q '^QA_RECOVERY action="open_accessibility_guidance"'
printf 'CHAOS_OK case=paste_rejection\n'
