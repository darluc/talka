#!/bin/sh
set -eu

scenario=""
duration="5s"
asr="fake"
target_app="Notes"
simulate_ollama_timeout="false"
evidence_file=""

usage() {
  printf 'usage: %s --scenario local-network-denied|audio-stream|network-interruption|full-e2e|forget-device|restart-reconnect|permissions-denied [--duration 5s] [--asr fake|real] [--target-app Notes] [--simulate-ollama-timeout]\n' "$0" >&2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --scenario)
      shift
      scenario="${1:-}"
      ;;
    --duration)
      shift
      duration="${1:-}"
      ;;
    --asr)
      shift
      asr="${1:-}"
      ;;
    --target-app)
      shift
      target_app="${1:-}"
      ;;
    --simulate-ollama-timeout)
      simulate_ollama_timeout="true"
      ;;
    *)
      usage
      exit 2
      ;;
  esac
  shift
done

if [ "$scenario" = "" ]; then
  usage
  exit 2
fi

case "$scenario" in
  local-network-denied)
    evidence_file=".sisyphus/evidence/task-10-local-network-denied.log"
    ;;
  audio-stream)
    evidence_file=".sisyphus/evidence/task-11-audio-stream.log"
    ;;
  network-interruption)
    evidence_file=".sisyphus/evidence/task-11-network-error.log"
    ;;
  full-e2e)
    evidence_file=".sisyphus/evidence/task-12-physical-full-e2e-${asr}-20260429.log"
    ;;
  forget-device)
    evidence_file=".sisyphus/evidence/task-13-forget-device.log"
    ;;
  restart-reconnect)
    evidence_file=".sisyphus/evidence/task-14-restart-reconnect.log"
    ;;
  permissions-denied)
    evidence_file=".sisyphus/evidence/task-14-permissions-denied.log"
    ;;
esac

if [ "$evidence_file" != "" ]; then
  mkdir -p "$(dirname "$evidence_file")"
  : >"$evidence_file"
fi

emit() {
  printf "$@"
  if [ "$evidence_file" != "" ]; then
    printf "$@" >>"$evidence_file"
  fi
}

case "$scenario" in
  local-network-denied)
    emit 'QA_SCENARIO name=local-network-denied platform=physical-ios manual_required=false\n'
    emit 'QA_PRECONDITION detail="Requires a real iPhone build with Talka freshly installed or Local Network permission reset, plus the Mac-side Talka _talka._tcp service available on the same Wi-Fi network."\n'
    emit 'QA_STEP number=1 action="Launch Talka on the iPhone and verify no local-network prompt appears on launch."\n'
    emit 'QA_EXPECT number=1 marker=no_prompt_on_launch detail="Bonjour browsing must not start until the user taps Discover Macs."\n'
    emit 'QA_STEP number=2 action="Tap Discover Macs once, deny the Local Network prompt, and observe the recovery state."\n'
    emit 'QA_EXPECT number=2 marker=prompt_after_explicit_discover detail="iOS should present the Local Network permission prompt only after the explicit Discover Macs action."\n'
    emit 'QA_EXPECT number=3 marker=denied_state detail="The app should show the Local Network Denied state and a retry/settings hint."\n'
    emit 'QA_BLOCKER code=physical_local_network_agent_required dependency="real iPhone local-network permission prompt plus Mac Bonjour service" detail="No physical prompt timing, denial action, denied-state UI, screenshot, or screen recording evidence was collected by this script invocation."\n'
    emit 'QA_EVIDENCE file="%s" physical_collected=false required="no_prompt_on_launch,prompt_after_explicit_discover,denied_state_screen"\n' "$evidence_file"
    emit 'QA_RESULT status=blocked scenario=local-network-denied reason=physical_local_network_agent_required next="Run a real physical-device QA agent with Local Network permission reset; only emit pass after collected evidence proves prompt timing and denied-state recovery."\n'
    exit 1
    ;;
  audio-stream)
    emit 'QA_SCENARIO name=audio-stream platform=physical-ios manual_required=false duration=%s\n' "$duration"
    emit 'QA_PRECONDITION detail="Requires an agent-executed run on physical iPhone ZVVZ paired with the Mac-side Talka service, microphone permission prompt observation, live encrypted audio_start/audio_frame/audio_stop receipt, and diagnostic WAV reconstruction from that physical transport."\n'
    emit 'QA_AUTOMATED marker=debug_wav_reconstruction command="go test ./internal/session ./internal/app/smokefakee2e -run '\''TestDiagnosticAudioCaptureReconstructsWAVFromEncryptedFrames|TestRunWritesDebugWAVFromEncryptedFullSession'\''" evidence="host-only regression coverage; not physical iPhone evidence"\n'
    emit 'QA_BLOCKER code=physical_agent_required dependency="connected physical iPhone/Mac interaction" detail="No real physical microphone start/stop, encrypted iPhone-to-Mac frame receipt, or physical diagnostic WAV evidence was collected by this script invocation."\n'
    emit 'QA_EVIDENCE file="%s" physical_collected=false required="microphone_prompt_after_start,audio_start_frame_stop,debug_wav_valid"\n' "$evidence_file"
    emit 'QA_RESULT status=blocked scenario=audio-stream reason=physical_agent_required next="Run a real physical-device QA agent on iPhone ZVVZ with Mac diagnostic capture enabled; only emit pass after collected evidence proves microphone prompt, encrypted frames, audio_stop, and valid physical debug WAV."\n'
    exit 1
    ;;
  network-interruption)
    emit 'QA_SCENARIO name=network-interruption platform=physical-ios manual_required=false\n'
    emit 'QA_PRECONDITION detail="Requires an agent-executed run on physical iPhone ZVVZ paired with the Mac-side Talka service while Wi-Fi or the service is interrupted during active recording."\n'
    emit 'QA_AUTOMATED marker=backpressure_recovery command="xcodebuild test -workspace apps/Talka.xcworkspace -scheme TalkaIOS -destination '\''id=00008110-0014190C2132801E'\''" evidence="physical XCTest can cover bounded queue logic, but this script invocation did not perform live Wi-Fi/service interruption during microphone capture"\n'
    emit 'QA_BLOCKER code=physical_agent_required dependency="connected physical iPhone/Mac interaction" detail="No real recording session was started and interrupted on the physical Wi-Fi/service path by this script invocation."\n'
    emit 'QA_EVIDENCE file="%s" physical_collected=false required="recording_started,recording_stopped_safely,no_unbounded_queue,retry_visible"\n' "$evidence_file"
    emit 'QA_RESULT status=blocked scenario=network-interruption reason=physical_agent_required next="Run a real physical-device QA agent that starts recording, interrupts Wi-Fi or the Mac service, observes capture stop/cancel, verifies bounded queue logs, then restores retry/reconnect."\n'
    exit 1
    ;;
  full-e2e)
    emit 'QA_SCENARIO name=full-e2e platform=physical-ios manual_required=false asr=%s target_app=%s simulate_ollama_timeout=%s\n' "$asr" "$target_app" "$simulate_ollama_timeout"
    emit 'QA_PRECONDITION detail="Use a real iPhone paired with the Mac-side Talka service and keep %s focused in an editable text field."\n' "$target_app"
    if [ "$asr" = "fake" ]; then
      emit 'QA_PRECONDITION detail="Start Mac Talka with fake ASR/fake cleanup providers; ./scripts/smoke-fake-e2e.sh --full-session is host-only evidence and cannot satisfy physical QA."\n'
    else
      emit 'QA_PRECONDITION detail="Start Mac Talka with real FunASR assets/runtime and Ollama available with the configured model."\n'
    fi
    if [ "$simulate_ollama_timeout" = "true" ]; then
      emit 'QA_PRECONDITION detail="Configure the Mac-side cleanup provider to simulate or force an Ollama timeout."\n'
    fi
    emit 'QA_STEP number=1 action="Pair or reconnect the iPhone, then start microphone recording and speak the fixture phrase."\n'
    emit 'QA_EXPECT number=1 marker=iphone_to_mac_audio detail="Mac logs should show encrypted audio_start, audio_frame, and audio_stop from the iPhone."\n'
    emit 'QA_STEP number=2 action="Stop recording and wait for ASR final, cleanup, and insertion."\n'
    if [ "$simulate_ollama_timeout" = "true" ]; then
      emit 'QA_EXPECT number=2 marker=ollama_timeout_fallback detail="Raw ASR final remains recoverable/insertable and cleanup status records ollama_timeout or fallback_timeout."\n'
    else
      emit 'QA_EXPECT number=2 marker=final_text_inserted detail="Only final optimized text should appear in %s; partial ASR text must not be inserted."\n' "$target_app"
    fi
    emit 'QA_EXPECT number=3 marker=recovery_text_preserved detail="On insertion failure, the final/raw text remains recoverable for manual copy or retry."\n'
    emit 'QA_STEP number=3 action="Save iPhone screen recording, Mac session logs, ASR final, cleanup final, and paste/insertion receipt."\n'
    emit 'QA_AUTOMATED marker=host_full_session command="./scripts/smoke-fake-e2e.sh --full-session" evidence="host-only regression coverage; not physical iPhone/Mac/Notes evidence"\n'
    if [ "$asr" = "real" ]; then
      emit 'QA_BLOCKER code=physical_full_e2e_agent_required dependency="connected physical iPhone plus Mac Talka service plus real FunASR/Ollama plus focused %s" detail="No live physical iPhone microphone, encrypted Mac session logs, real ASR/Ollama final, or %s paste receipt was collected by this script invocation."\n' "$target_app" "$target_app"
    else
      emit 'QA_BLOCKER code=physical_full_e2e_agent_required dependency="connected physical iPhone plus Mac Talka service plus fake ASR/cleanup providers plus focused %s" detail="No live physical iPhone microphone, encrypted Mac session logs, fake ASR/cleanup final, or %s paste receipt was collected by this script invocation."\n' "$target_app" "$target_app"
    fi
    emit 'QA_EVIDENCE file="%s" physical_collected=false required="iphone_to_mac_audio,asr_final,cleanup_final,paste_result,recovery_text_preserved"\n' "$evidence_file"
    emit 'QA_RESULT status=blocked scenario=full-e2e asr=%s reason=physical_full_e2e_agent_required next="Run a real physical iPhone-to-Mac QA agent with %s focused; only emit pass after collected evidence proves encrypted audio receipt, ASR final, cleanup final, paste result, and recovery text preservation."\n' "$asr" "$target_app"
    exit 1
    ;;
  forget-device)
    emit 'QA_SCENARIO name=forget-device platform=physical-ios manual_required=false\n'
    emit 'QA_PRECONDITION detail="Requires a real iPhone already paired with the Mac-side Talka service, with reconnect currently working without a new PIN before the forget action."\n'
    emit 'QA_STEP number=1 action="Open macOS Talka Settings, find the paired iPhone under Devices, and click Forget."\n'
    emit 'QA_EXPECT number=1 marker=mac_forget_ack detail="Mac removes the device from the trusted-device list and reports a forget acknowledgement."\n'
    emit 'QA_STEP number=2 action="On iPhone, tap Forget Mac or clear the selected paired Mac, then attempt reconnect."\n'
    emit 'QA_EXPECT number=2 marker=ios_reconnect_blocked detail="The iPhone must not silently reconnect with the old identity; it should require discovery/pairing with a fresh PIN."\n'
    emit 'QA_STEP number=3 action="Start a new pairing flow and record fresh pairing/session evidence without logging the PIN value."\n'
    emit 'QA_EXPECT number=3 marker=fresh_pairing_required detail="A new PIN is required and the old identity cannot resume silently."\n'
    emit 'QA_BLOCKER code=physical_forget_device_agent_required dependency="paired physical iPhone plus Mac device-management UI plus reconnect attempt" detail="No real Mac forget acknowledgement, iOS old-identity reconnect rejection, or fresh-pairing evidence was collected by this script invocation."\n'
    emit 'QA_EVIDENCE file="%s" physical_collected=false required="mac_forget_ack,ios_reconnect_blocked,fresh_pairing_required"\n' "$evidence_file"
    emit 'QA_RESULT status=blocked scenario=forget-device reason=physical_forget_device_agent_required next="Run a real physical-device QA agent with a paired iPhone and Mac Settings; only emit pass after collected evidence proves forget acknowledgement, reconnect blocking, and fresh pairing requirement."\n'
    exit 1
    ;;
  restart-reconnect)
    emit 'QA_SCENARIO name=restart-reconnect platform=physical-ios manual_required=false\n'
    emit 'QA_PRECONDITION detail="Requires a real paired iPhone and Mac-side Talka service, followed by an observed Mac service restart, iOS app relaunch, reconnect without a new PIN, resumed recording, fresh session-key logs, and replay rejection evidence."\n'
    emit 'QA_STEP number=1 action="Stop and restart the Mac-side Talka service, then relaunch the iOS app."\n'
    emit 'QA_EXPECT number=1 marker=persistent_pairing_loaded detail="Both sides should load persisted pairing identity without asking for a new PIN."\n'
    emit 'QA_STEP number=2 action="Tap reconnect on iPhone and start a short recording session."\n'
    emit 'QA_EXPECT number=2 marker=fresh_key_exchange detail="Mac logs should show a fresh resumed session/key exchange after restart, not reuse old sequence state."\n'
    emit 'QA_STEP number=3 action="Attempt or inspect replay of an old encrypted frame/sequence if diagnostic tooling is enabled."\n'
    emit 'QA_EXPECT number=3 marker=old_seq_rejected detail="Old sequence replay must be rejected and current reconnect remains usable."\n'
    emit 'QA_BLOCKER code=physical_restart_reconnect_agent_required dependency="paired physical iPhone plus Mac service restart/reconnect observation" detail="No real physical iPhone/Mac restart, reconnect, fresh session-key, recording, or old-sequence replay evidence was collected by this script invocation."\n'
    emit 'QA_EVIDENCE file="%s" physical_collected=false required="persistent_pairing_loaded,fresh_key_exchange,old_seq_rejected,reconnect_recording_usable"\n' "$evidence_file"
    emit 'QA_RESULT status=blocked scenario=restart-reconnect reason=physical_restart_reconnect_agent_required next="Run a real physical-device QA agent with a paired iPhone and Mac service restart; only emit pass after collected evidence proves persisted trust, fresh resumed session keys, replay rejection, and usable reconnect recording."\n'
    exit 1
    ;;
  permissions-denied)
    emit 'QA_SCENARIO name=permissions-denied platform=physical-ios manual_required=false\n'
    emit 'QA_PRECONDITION detail="Requires a real iPhone build with Microphone and Local Network permissions reset or denied, plus a Mac-side insertion attempt with Accessibility permission unavailable."\n'
    emit 'QA_STEP number=1 action="Tap Start Microphone and deny or verify denied Microphone permission."\n'
    emit 'QA_EXPECT number=1 marker=microphone_denied_recovery detail="iOS should stop recording and show an actionable microphone permission recovery message."\n'
    emit 'QA_STEP number=2 action="Tap Discover Macs and deny or verify denied Local Network permission."\n'
    emit 'QA_EXPECT number=2 marker=local_network_denied_recovery detail="iOS should show the Local Network denied state and settings/retry guidance."\n'
    emit 'QA_STEP number=3 action="Attempt insertion on the Mac with Accessibility permission missing."\n'
    emit 'QA_EXPECT number=3 marker=accessibility_denied_recovery detail="Mac should surface Accessibility guidance and preserve recoverable final text."\n'
    emit 'QA_BLOCKER code=physical_permissions_denied_agent_required dependency="physical iPhone permission prompts plus Mac Accessibility-denied insertion path" detail="No real physical microphone denial, local-network denial, Accessibility-denied insertion, recovery UI, or preserved-text evidence was collected by this script invocation."\n'
    emit 'QA_EVIDENCE file="%s" physical_collected=false required="microphone_denied_recovery,local_network_denied_recovery,accessibility_denied_recovery,recoverable_final_text"\n' "$evidence_file"
    emit 'QA_RESULT status=blocked scenario=permissions-denied reason=physical_permissions_denied_agent_required next="Run a real physical-device QA agent with permission states reset/denied and Mac Accessibility unavailable; only emit pass after collected evidence proves all denial recovery messages and preserved final text."\n'
    exit 1
    ;;
  *)
    printf 'unknown scenario: %s\n' "$scenario" >&2
    usage
    exit 2
    ;;
esac
