#!/bin/sh
set -eu

apps="Notes,Safari,VSCode,WeChatIfInstalled"
text='Talka 测试文本。'
simulate_permission_denied="false"
simulate_success="false"
simulate_user_changed="false"
hard_failure="0"

usage() {
  printf 'usage: %s [--apps Notes,Safari,VSCode,WeChatIfInstalled] [--text "Talka 测试文本。"] [--simulate-permission-denied] [--simulate-success] [--simulate-user-changed]\n' "$0" >&2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --apps)
      shift
      apps="${1:-}"
      ;;
    --text)
      shift
      text="${1:-}"
      ;;
    --simulate-permission-denied)
      simulate_permission_denied="true"
      ;;
    --simulate-success)
      simulate_success="true"
      ;;
    --simulate-user-changed)
      simulate_user_changed="true"
      ;;
    *)
      usage
      exit 2
      ;;
  esac
  shift
done

run_probe() {
  mode="$1"
  label="$2"
  expected_code="${3:-}"
  printf 'QA_SCRIPT scenario=%s mode=%s expected_error=%s\n' "$label" "$mode" "$expected_code"
  if [ "$expected_code" != "" ]; then
    go run ./internal/inject/qapaste --mode "$mode" --app-label "$label" --text "$text" --expect-error-code "$expected_code"
    return
  fi

  if output="$(go run ./internal/inject/qapaste --mode "$mode" --app-label "$label" --text "$text" 2>&1)"; then
    printf '%s\n' "$output"
    return 0
  fi

  printf '%s\n' "$output"
  if is_permission_denied_text "$output" || is_accessibility_missing_text "$output"; then
    printf 'QA_APP app=%s outcome=%s stage=%s\n' "$label" "accessibility_missing" "paste"
    return 2
  fi

  printf 'QA_APP app=%s outcome=%s stage=%s\n' "$label" "failed" "paste"
  return 1
}

if [ "$simulate_permission_denied" = "true" ]; then
  run_probe simulate-permission-denied simulated-permission-denied accessibility_missing
  exit 0
fi

if [ "$simulate_success" = "true" ]; then
  run_probe simulate-success simulated-success
  exit 0
fi

if [ "$simulate_user_changed" = "true" ]; then
  run_probe simulate-user-changed simulated-user-changed
  exit 0
fi

prepare_notes() {
  open -a Notes >/dev/null 2>&1 || return 1
  osascript <<'APPLESCRIPT' >/dev/null
tell application "Notes"
  activate
  if not (exists folder "Notes") then
    return
  end if
  make new note at folder "Notes" with properties {name:"Talka QA", body:""}
end tell
APPLESCRIPT
}

prepare_safari() {
  html_path="$1"
  cat >"$html_path" <<'EOF'
<!DOCTYPE html>
<html lang="en">
  <body>
    <textarea autofocus rows="8" cols="60"></textarea>
  </body>
</html>
EOF
  open -a Safari "$html_path"
}

prepare_vscode() {
  file_path="$1"
  : >"$file_path"
  open -a "Visual Studio Code" "$file_path"
}

app_installed() {
  app_name="$1"
  open -Ra "$app_name"
}

is_permission_denied_text() {
  case "$1" in
    *"Not authorized to send Apple events"*|*"not authorized to send Apple events"*|*"not authorised to send Apple events"*|*"-1743"*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

is_accessibility_missing_text() {
  case "$1" in
    *"EXPECTED_ERROR accessibility_missing"*|*"accessibility_missing"*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

report_marker() {
  app_label="$1"
  outcome="$2"
  stage="$3"
  detail="${4:-}"

  if [ "$detail" != "" ]; then
    printf 'QA_APP app=%s outcome=%s stage=%s detail=%s\n' "$app_label" "$outcome" "$stage" "$detail"
    return
  fi

  printf 'QA_APP app=%s outcome=%s stage=%s\n' "$app_label" "$outcome" "$stage"
}

prepare_with_permission_marker() {
  app_label="$1"
  prepare_kind="$2"
  shift 2

  if output="$("$@" 2>&1)"; then
    return 0
  fi

  if is_permission_denied_text "$output"; then
    report_marker "$app_label" "accessibility_missing" "$prepare_kind" "$output"
    return 2
  fi

  report_marker "$app_label" "prepare_failed" "$prepare_kind" "$output"
  return 1
}

work_dir="$(mktemp -d)"
cleanup() {
  rm -rf "$work_dir"
}
trap cleanup EXIT INT TERM

OLD_IFS="$IFS"
IFS=','
set -- $apps
IFS="$OLD_IFS"

for app in "$@"; do
  case "$app" in
    Notes)
      if ! app_installed Notes; then
        report_marker "$app" "missing" "detect"
        hard_failure="1"
        continue
      fi
      status=0
      prepare_with_permission_marker "$app" prepare prepare_notes || status="$?"
      if [ "$status" -ne 0 ]; then
        if [ "$status" -eq 2 ]; then
          continue
        fi
        hard_failure="1"
        continue
      fi
      sleep 1
      status=0
      run_probe real "$app" || status="$?"
      if [ "$status" -ne 0 ]; then
        if [ "$status" -ne 2 ]; then
          hard_failure="1"
        fi
      fi
      ;;
    Safari)
      if ! app_installed Safari; then
        report_marker "$app" "missing" "detect"
        hard_failure="1"
        continue
      fi
      status=0
      prepare_with_permission_marker "$app" prepare prepare_safari "$work_dir/safari-qa.html" || status="$?"
      if [ "$status" -ne 0 ]; then
        hard_failure="1"
        continue
      fi
      sleep 1
      status=0
      run_probe real "$app" || status="$?"
      if [ "$status" -ne 0 ]; then
        if [ "$status" -ne 2 ]; then
          hard_failure="1"
        fi
      fi
      ;;
    VSCode)
      if ! app_installed "Visual Studio Code"; then
        report_marker "$app" "missing" "detect"
        hard_failure="1"
        continue
      fi
      status=0
      prepare_with_permission_marker "$app" prepare prepare_vscode "$work_dir/talka-qa.txt" || status="$?"
      if [ "$status" -ne 0 ]; then
        hard_failure="1"
        continue
      fi
      sleep 1
      status=0
      run_probe real "$app" || status="$?"
      if [ "$status" -ne 0 ]; then
        if [ "$status" -ne 2 ]; then
          hard_failure="1"
        fi
      fi
      ;;
    WeChatIfInstalled)
      if app_installed WeChat; then
        if ! open -a WeChat >/dev/null 2>&1; then
          report_marker "WeChat" "prepare_failed" "prepare"
          hard_failure="1"
          continue
        fi
        sleep 1
        status=0
        run_probe real WeChat || status="$?"
        if [ "$status" -ne 0 ]; then
          if [ "$status" -ne 2 ]; then
            hard_failure="1"
          fi
        fi
      else
        report_marker "$app" "skipped_not_installed" "detect"
      fi
      ;;
    *)
      report_marker "$app" "unsupported" "detect"
      exit 2
      ;;
  esac
done

if [ "$hard_failure" != "0" ]; then
  exit 1
fi
