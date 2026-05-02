#!/bin/sh
set -eu

assert_blocked_physical_scenario() {
  scenario="$1"
  evidence_file="$2"
  shift 2

  rm -f "$evidence_file"

  if output="$(./scripts/qa-physical-ios.sh --scenario "$scenario" "$@" 2>&1)"; then
    printf 'expected %s to exit nonzero when physical evidence is unavailable\n' "$scenario" >&2
    exit 1
  fi

  printf '%s\n' "$output" | grep -q '^QA_RESULT status=blocked '
  if printf '%s\n' "$output" | grep -q 'manual-check-needed\|^QA_PASS\|^QA_RESULT status=pass'; then
    printf 'expected %s output to avoid manual success markers\n' "$scenario" >&2
    exit 1
  fi

  test -f "$evidence_file"
  grep -q '^QA_RESULT status=blocked ' "$evidence_file"
  if grep -q 'manual-check-needed\|^QA_PASS\|^QA_RESULT status=pass' "$evidence_file"; then
    printf 'expected %s evidence to avoid manual success markers\n' "$scenario" >&2
    exit 1
  fi
}

assert_blocked_physical_scenario local-network-denied .sisyphus/evidence/task-10-local-network-denied.log
assert_blocked_physical_scenario audio-stream .sisyphus/evidence/task-11-audio-stream.log --duration 5s
assert_blocked_physical_scenario network-interruption .sisyphus/evidence/task-11-network-error.log
assert_blocked_physical_scenario full-e2e .sisyphus/evidence/task-12-physical-full-e2e-fake-20260429.log --asr fake --target-app Notes
assert_blocked_physical_scenario full-e2e .sisyphus/evidence/task-12-physical-full-e2e-real-20260429.log --asr real --target-app Notes
assert_blocked_physical_scenario forget-device .sisyphus/evidence/task-13-forget-device.log
assert_blocked_physical_scenario restart-reconnect .sisyphus/evidence/task-14-restart-reconnect.log
assert_blocked_physical_scenario permissions-denied .sisyphus/evidence/task-14-permissions-denied.log

if chaos_output="$(./scripts/smoke-chaos.sh 2>&1)"; then
  :
else
  printf '%s\n' "$chaos_output" >&2
  printf 'expected smoke-chaos to pass while reporting physical network interruption as blocked\n' >&2
  exit 1
fi

printf '%s\n' "$chaos_output" | grep -q '^QA_RESULT status=blocked scenario=network-interruption '
printf '%s\n' "$chaos_output" | grep -q '^CHAOS_OK case=network_interruption_automated '
printf '%s\n' "$chaos_output" | grep -q '^CHAOS_BLOCKED case=network_interruption_physical '
printf '%s\n' "$chaos_output" | grep -q '^CHAOS_OK case=paste_rejection$'

printf 'QA_PHYSICAL_IOS_TEST_OK scenarios=local-network-denied,audio-stream,network-interruption,full-e2e-fake,full-e2e-real,forget-device,restart-reconnect,permissions-denied\n'
