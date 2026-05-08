#!/bin/sh
set -eu

usage() {
  printf 'usage: %s --runtime-dir <dir> --frameworks-dir <dir>\n' "$0" >&2
  exit 2
}

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
RUNTIME_DIR=""
FRAMEWORKS_DIR=""

while [ $# -gt 0 ]; do
  case "$1" in
    --runtime-dir)
      [ -n "${2-}" ] || usage
      RUNTIME_DIR="$2"
      shift 2
      ;;
    --frameworks-dir)
      [ -n "${2-}" ] || usage
      FRAMEWORKS_DIR="$2"
      shift 2
      ;;
    *)
      usage
      ;;
  esac
done

[ -n "$RUNTIME_DIR" ] || usage
[ -n "$FRAMEWORKS_DIR" ] || usage

FUNASR_SOURCE_DIR="${TALKA_FUNASR_SOURCE_DIR:-$ROOT_DIR/.sisyphus/FunASR}"
if [ ! -f "$FUNASR_SOURCE_DIR/runtime/websocket/CMakeLists.txt" ]; then
  MAIN_WORKTREE="$(git -C "$ROOT_DIR" worktree list --porcelain 2>/dev/null | awk '/^worktree /{print substr($0,10); exit}')"
  if [ -n "$MAIN_WORKTREE" ] && [ -f "$MAIN_WORKTREE/.sisyphus/FunASR/runtime/websocket/CMakeLists.txt" ]; then
    FUNASR_SOURCE_DIR="$MAIN_WORKTREE/.sisyphus/FunASR"
  fi
fi
if [ ! -f "$FUNASR_SOURCE_DIR/runtime/websocket/CMakeLists.txt" ]; then
  FUNASR_SOURCE_DIR="$ROOT_DIR/build/cache/FunASR"
  if [ ! -f "$FUNASR_SOURCE_DIR/runtime/websocket/CMakeLists.txt" ]; then
    rm -rf "$FUNASR_SOURCE_DIR"
    mkdir -p "$(dirname "$FUNASR_SOURCE_DIR")"
    git clone --depth 1 https://github.com/alibaba-damo-academy/FunASR.git "$FUNASR_SOURCE_DIR"
  fi
fi

if ! command -v brew >/dev/null 2>&1; then
  printf 'error: Homebrew is required to build the embedded FunASR runtime\n' >&2
  exit 1
fi

HOMEBREW_PREFIX="$(brew --prefix)"
if ! brew list --versions onnxruntime >/dev/null 2>&1; then
  brew install onnxruntime
fi

ONNXRUNTIME_DIR="$(brew --prefix onnxruntime)"
FFMPEG_DIR="$(brew --prefix ffmpeg)"
OPENSSL_DIR="$(brew --prefix openssl@3)"
NLOHMANN_JSON_DIR="$(brew --prefix nlohmann-json)"
GFLAGS_DIR="$(brew --prefix gflags)"
BUILD_ROOT="$ROOT_DIR/build/funasr-runtime"
SOURCE_COPY_DIR="$BUILD_ROOT/src"
BUILD_DIR="$BUILD_ROOT/build"
DEPS_DIR="$BUILD_ROOT/deps"

rm -rf "$SOURCE_COPY_DIR" "$BUILD_DIR" "$DEPS_DIR"
mkdir -p "$SOURCE_COPY_DIR" "$BUILD_DIR" "$DEPS_DIR" "$RUNTIME_DIR" "$FRAMEWORKS_DIR"
rsync -a --delete "$FUNASR_SOURCE_DIR/" "$SOURCE_COPY_DIR/"

mkdir -p "$SOURCE_COPY_DIR/runtime/websocket/third_party/websocket"
WEBSOCKETPP_CACHE="$(find "$HOME/Library/Caches/Homebrew/downloads" -name '*websocketpp--*.tar.gz' | head -1)"
if [ -z "$WEBSOCKETPP_CACHE" ]; then
  brew fetch websocketpp
  WEBSOCKETPP_CACHE="$(find "$HOME/Library/Caches/Homebrew/downloads" -name '*websocketpp--*.tar.gz' | head -1)"
fi
if [ -z "$WEBSOCKETPP_CACHE" ]; then
  printf 'error: unable to locate websocketpp bottle cache\n' >&2
  exit 1
fi
tar -xzf "$WEBSOCKETPP_CACHE" -C "$DEPS_DIR"
WEBSOCKETPP_INCLUDE_DIR="$(find "$DEPS_DIR" -path '*/include/websocketpp' -type d | head -1)"
if [ -z "$WEBSOCKETPP_INCLUDE_DIR" ]; then
  printf 'error: websocketpp headers not found after extracting %s\n' "$WEBSOCKETPP_CACHE" >&2
  exit 1
fi
rsync -a "$WEBSOCKETPP_INCLUDE_DIR" "$SOURCE_COPY_DIR/runtime/websocket/third_party/websocket/"
mkdir -p "$SOURCE_COPY_DIR/runtime/websocket/third_party/asio/asio/include"

mkdir -p "$SOURCE_COPY_DIR/runtime/websocket/third_party/json/include"
rsync -a "$NLOHMANN_JSON_DIR/include/nlohmann" "$SOURCE_COPY_DIR/runtime/websocket/third_party/json/include/"
if [ -f "$NLOHMANN_JSON_DIR/share/doc/nlohmann-json/ChangeLog.md" ]; then
  cp -f "$NLOHMANN_JSON_DIR/share/doc/nlohmann-json/ChangeLog.md" "$SOURCE_COPY_DIR/runtime/websocket/third_party/json/ChangeLog.md"
else
  : >"$SOURCE_COPY_DIR/runtime/websocket/third_party/json/ChangeLog.md"
fi

WEBSOCKET_ROOT_CMAKE="$SOURCE_COPY_DIR/runtime/websocket/CMakeLists.txt"
WEBSOCKET_CMAKE="$SOURCE_COPY_DIR/runtime/websocket/bin/CMakeLists.txt"
YAML_CPP_CMAKE="$SOURCE_COPY_DIR/runtime/onnxruntime/third_party/yaml-cpp/CMakeLists.txt"
OPENFST_BI_TABLE="$SOURCE_COPY_DIR/runtime/onnxruntime/third_party/openfst/src/include/fst/bi-table.h"
OPENFST_CMAKE="$SOURCE_COPY_DIR/runtime/onnxruntime/third_party/openfst/CMakeLists.txt"
KALDI_FBANK_CMAKE="$SOURCE_COPY_DIR/runtime/onnxruntime/third_party/kaldi-native-fbank/CMakeLists.txt"
ONNXRUNTIME_CMAKE="$SOURCE_COPY_DIR/runtime/onnxruntime/CMakeLists.txt"
LIMONP_STRING_UTIL="$SOURCE_COPY_DIR/runtime/onnxruntime/third_party/jieba/include/limonp/StringUtil.hpp"
OFFLINE_STREAM_HEADER="$SOURCE_COPY_DIR/runtime/onnxruntime/include/offline-stream.h"
TPASS_STREAM_HEADER="$SOURCE_COPY_DIR/runtime/onnxruntime/include/tpass-stream.h"
for standard_file in "$WEBSOCKET_ROOT_CMAKE" "$ONNXRUNTIME_CMAKE" "$KALDI_FBANK_CMAKE"; do
  if [ -f "$standard_file" ]; then
    perl -0pi -e 's/set\(CMAKE_CXX_STANDARD 14 CACHE STRING "The C\+\+ version to be used\."\)/set(CMAKE_CXX_STANDARD 17 CACHE STRING "The C++ version to be used.")/' "$standard_file"
  fi
done
if [ -f "$OPENFST_CMAKE" ]; then
  perl -0pi -e 's/set\(CMAKE_CXX_STANDARD 11\)/set(CMAKE_CXX_STANDARD 17)/' "$OPENFST_CMAKE"
fi
perl -0pi -e 's/^target_link_options\(funasr-wss-server PRIVATE "-Wl,--no-as-needed"\)\n//m' "$WEBSOCKET_CMAKE"
perl -0pi -e 's/^target_link_options\(funasr-wss-server-2pass PRIVATE "-Wl,--no-as-needed"\)\n//m' "$WEBSOCKET_CMAKE"
for header in "$SOURCE_COPY_DIR/runtime/websocket/bin/websocket-server.h" "$SOURCE_COPY_DIR/runtime/websocket/bin/websocket-server-2pass.h"; do
  perl -0pi -e 's/#define ASIO_STANDALONE 1  \/\/ not boost\n/#include <boost\/asio.hpp>\nnamespace asio = boost::asio;\n/s' "$header"
  perl -0pi -e 's/#include "asio\.hpp"\n//g' "$header"
done
if [ -f "$YAML_CPP_CMAKE" ]; then
  perl -0pi -e 's/cmake_minimum_required\(VERSION 2\.6\)/cmake_minimum_required(VERSION 3.5)/' "$YAML_CPP_CMAKE"
  perl -0pi -e 's/if\(POLICY CMP0012\)\n\s*cmake_policy\(SET CMP0012 OLD\)\nendif\(\)\n//s' "$YAML_CPP_CMAKE"
  perl -0pi -e 's/if\(POLICY CMP0015\)\n\s*cmake_policy\(SET CMP0015 OLD\)\nendif\(\)\n//s' "$YAML_CPP_CMAKE"
  perl -0pi -e 's/-std=c\+\+11/-std=c++17/' "$YAML_CPP_CMAKE"
fi
if [ -f "$OPENFST_BI_TABLE" ]; then
  perl -0pi -e 's/new S\(table\.s_\)/new S(*table.selector_)/' "$OPENFST_BI_TABLE"
fi
if [ -f "$LIMONP_STRING_UTIL" ]; then
  perl -0pi -e 's/std::not1\(std::ptr_fun<unsigned, bool>\(IsSpace\)\)/[](unsigned char ch) { return !IsSpace(ch); }/g' "$LIMONP_STRING_UTIL"
  perl -0pi -e 's/std::not1\(std::bind2nd\(std::equal_to<char>\(\), x\)\)/[x](char c) { return c != x; }/g' "$LIMONP_STRING_UTIL"
fi
for stream_header in "$OFFLINE_STREAM_HEADER" "$TPASS_STREAM_HEADER"; do
  if [ -f "$stream_header" ]; then
    perl -0pi -e 's/#if !defined\(__APPLE__\)\n#include "itn-model\.h"\n(?:#include "com-define\.h"\n)?#endif/#if !defined(__APPLE__)\n#include "itn-model.h"\n#endif\n#include "com-define.h"\n/s' "$stream_header"
  fi
done

cmake \
  -S "$SOURCE_COPY_DIR/runtime/websocket" \
  -B "$BUILD_DIR" \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_CXX_STANDARD=17 \
  -DCMAKE_POLICY_VERSION_MINIMUM=3.5 \
  -DENABLE_PORTAUDIO=OFF \
  -DENABLE_WEBSOCKET=ON \
  -DONNXRUNTIME_DIR="$ONNXRUNTIME_DIR" \
  -DFFMPEG_DIR="$FFMPEG_DIR" \
  -DOPENSSL_ROOT_DIR="$OPENSSL_DIR"

cmake --build "$BUILD_DIR" --target funasr-wss-server-2pass -j "${TALKA_BUILD_JOBS:-$(sysctl -n hw.ncpu)}"

RUNTIME_BINARY="$BUILD_DIR/bin/funasr-wss-server-2pass"
if [ ! -x "$RUNTIME_BINARY" ]; then
  printf 'error: missing built runtime binary at %s\n' "$RUNTIME_BINARY" >&2
  exit 1
fi

	cp -f "$RUNTIME_BINARY" "$RUNTIME_DIR/funasr-wss-server-2pass"
	chmod +x "$RUNTIME_DIR/funasr-wss-server-2pass"

resolve_dep_path() {
  dep="$1"
  case "$dep" in
    @rpath/*)
      base="$(basename "$dep")"
      for dir in \
        "$BUILD_DIR/src" \
        "$BUILD_DIR/yaml-cpp" \
        "$BUILD_DIR/glog" \
        "$BUILD_DIR/openfst/src/lib" \
        "$HOMEBREW_PREFIX/lib" \
        "$HOMEBREW_PREFIX/opt/abseil/lib" \
        "$HOMEBREW_PREFIX/opt/protobuf/lib" \
        "$HOMEBREW_PREFIX/opt/onnx/lib" \
        "$HOMEBREW_PREFIX/opt/re2/lib" \
        "$ONNXRUNTIME_DIR/lib" \
        "$OPENSSL_DIR/lib" \
        "$GFLAGS_DIR/lib" \
        "$FFMPEG_DIR/lib"
      do
        if [ -f "$dir/$base" ]; then
          printf '%s\n' "$dir/$base"
          return 0
        fi
      done
      found="$(find "$BUILD_DIR" -name "$base" -type f | head -1)"
      if [ -n "$found" ]; then
        printf '%s\n' "$found"
        return 0
      fi
      return 1
      ;;
    *)
      perl -MCwd=realpath -e 'print realpath($ARGV[0])' "$dep"
      ;;
  esac
}

should_skip_dep() {
  case "$1" in
    /System/*|/usr/lib/*|"@loader_path"*|"@executable_path"*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

copy_dependency() {
  dep="$1"
  should_skip_dep "$dep" && return 0

  real_dep="$(resolve_dep_path "$dep")" || {
    printf 'error: unable to resolve dependency %s\n' "$dep" >&2
    exit 1
  }
  requested_base="$(basename "$dep")"
  base="$(basename "$real_dep")"
  dest="$FRAMEWORKS_DIR/$base"
  if [ ! -f "$dest" ]; then
    cp -fL "$real_dep" "$dest"
    chmod u+w "$dest"
    patch_library "$dest"
  fi
  if [ "$requested_base" != "$base" ] && [ ! -e "$FRAMEWORKS_DIR/$requested_base" ]; then
    ln -s "$base" "$FRAMEWORKS_DIR/$requested_base"
  fi
}

patch_binary() {
  target="$1"
  otool -L "$target" | tail -n +2 | awk '{print $1}' | while IFS= read -r dep; do
    [ -n "$dep" ] || continue
    should_skip_dep "$dep" && continue
    real_dep="$(resolve_dep_path "$dep")" || {
      printf 'error: unable to resolve dependency %s referenced by %s\n' "$dep" "$target" >&2
      exit 1
    }
    base="$(basename "$real_dep")"
    copy_dependency "$dep"
    install_name_tool -change "$dep" "@executable_path/../Frameworks/$base" "$target"
  done
}

patch_library() {
  target="$1"
  base="$(basename "$target")"
  install_name_tool -id "@loader_path/$base" "$target"
  otool -L "$target" | tail -n +2 | awk '{print $1}' | while IFS= read -r dep; do
    [ -n "$dep" ] || continue
    should_skip_dep "$dep" && continue
    real_dep="$(resolve_dep_path "$dep")" || {
      printf 'error: unable to resolve dependency %s referenced by %s\n' "$dep" "$target" >&2
      exit 1
    }
    dep_base="$(basename "$real_dep")"
    copy_dependency "$dep"
    install_name_tool -change "$dep" "@loader_path/$dep_base" "$target"
  done
}

relink_binary_to_bundle() {
  target="$1"
  otool -L "$target" | tail -n +2 | awk '{print $1}' | while IFS= read -r dep; do
    [ -n "$dep" ] || continue
    should_skip_dep "$dep" && continue
    base="$(basename "$dep")"
    if [ -f "$FRAMEWORKS_DIR/$base" ]; then
      install_name_tool -change "$dep" "@executable_path/../Frameworks/$base" "$target"
    fi
  done
}

relink_library_to_bundle() {
  target="$1"
  otool -L "$target" | tail -n +2 | awk '{print $1}' | while IFS= read -r dep; do
    [ -n "$dep" ] || continue
    should_skip_dep "$dep" && continue
    base="$(basename "$dep")"
    if [ -f "$FRAMEWORKS_DIR/$base" ]; then
      install_name_tool -change "$dep" "@loader_path/$base" "$target"
    fi
  done
}

copy_dependency_closure() {
  previous_count=-1
  while :; do
    current_count="$(find "$FRAMEWORKS_DIR" -maxdepth 1 -type f | wc -l | tr -d ' ')"
    if [ "$current_count" = "$previous_count" ]; then
      break
    fi
    previous_count="$current_count"

	for target in "$RUNTIME_DIR/funasr-wss-server-2pass" "$FRAMEWORKS_DIR"/*; do
      [ -f "$target" ] || continue
      otool -L "$target" | tail -n +2 | awk '{print $1}' | while IFS= read -r dep; do
        [ -n "$dep" ] || continue
        should_skip_dep "$dep" && continue
        base="$(basename "$dep")"
        if [ ! -f "$FRAMEWORKS_DIR/$base" ]; then
          copy_dependency "$dep"
        fi
      done
    done
  done
}

patch_binary "$RUNTIME_DIR/funasr-wss-server-2pass"
copy_dependency_closure
find "$FRAMEWORKS_DIR" -maxdepth 1 -type f | while IFS= read -r library; do
	relink_library_to_bundle "$library"
done
relink_binary_to_bundle "$RUNTIME_DIR/funasr-wss-server-2pass"

printf 'RUNTIME=%s\n' "$RUNTIME_DIR/funasr-wss-server-2pass"
