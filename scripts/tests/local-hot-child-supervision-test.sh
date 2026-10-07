#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Local hot child supervision test failed: %s\n' "$1" >&2; exit 1; }
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
helper="$repository_root/tools/dev/run-go-hot-child.sh"
air_binary=${1:-/home/s/.local/state/kodex-dev/cache/go-tools/air-v1.67.4}
[[ -x "$air_binary" ]] || fail 'pinned Air executable is unavailable'
"$air_binary" -v 2>/dev/null | grep -Fq v1.67.4 || fail 'Air version is not pinned'
for command_name in awk cc readlink sleep; do
  command -v "$command_name" >/dev/null || fail 'fixture dependency is unavailable'
done
fixture=$(mktemp -d /tmp/kodex-hot-child-test.XXXXXX)
air_pid=''
cleanup() {
  if [[ -n "$air_pid" ]]; then
    kill -TERM "$air_pid" 2>/dev/null || true
    wait "$air_pid" 2>/dev/null || true
  fi
  rm -rf -- "$fixture"
}
trap cleanup EXIT

cat >"$fixture/child.c" <<'EOF'
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>
static volatile sig_atomic_t stopped;
static void stop(int signal_number) { stopped = signal_number; }
int main(int argc, char **argv) {
  if (argc != 5 || strcmp(argv[3], "first-argument") || strcmp(argv[4], "last-argument")) return 81;
  signal(SIGINT, stop); signal(SIGTERM, stop); alarm(30);
  char mode[16] = {0};
  FILE *input = fopen(argv[1], "r");
  if (!input || !fgets(mode, sizeof(mode), input)) return 82;
  fclose(input);
  FILE *events = fopen(argv[2], "a");
  if (!events) return 83;
  fprintf(events, "%ld\n", (long)getpid()); fclose(events);
  if (!strncmp(mode, "fail", 4)) return 17;
  if (!strncmp(mode, "clean", 5)) return 0;
  while (!stopped) pause();
  return 0;
}
EOF
cat >"$fixture/launch.sh" <<'EOF'
#!/usr/bin/env sh
set -eu
fixture=$1
helper=$2
air=$3
started=$(awk '{ sub(/^.*\) /, ""); print $20 }' "/proc/$$/stat")
cat >"$fixture/air.toml" <<CONFIG
root = "$fixture"
tmp_dir = "$fixture/build"
[build]
cmd = "cc -O0 -o $fixture/app $fixture/child.c"
entrypoint = ["$helper", "$$", "$started", "$air", "$fixture/app", "$fixture/mode.state", "$fixture/events", "first-argument", "last-argument"]
include_ext = ["txt"]
delay = 10
poll = true
poll_interval = 50
stop_on_error = true
send_interrupt = true
kill_delay = "500ms"
rerun = false
[misc]
clean_on_exit = true
CONFIG
exec "$air" -c "$fixture/air.toml"
EOF

process_alive() {
  local state
  [[ -r "/proc/$1/stat" ]] || return 1
  state=$(awk '{ sub(/^.*\) /, ""); print $1 }' "/proc/$1/stat" 2>/dev/null) || return 1
  [[ "$state" != Z ]]
}
await_launches() {
  local wanted=$1
  for ((attempt=0; attempt<100; attempt++)); do
    if [[ -f "$fixture/events" ]] && [[ $(wc -l <"$fixture/events") -ge "$wanted" ]]; then return; fi
    process_alive "$air_pid" || fail 'Air exited before fixture launch'
    sleep 0.05
  done
  fail 'fixture launch deadline exceeded'
}
await_exit() {
  for ((attempt=0; attempt<120; attempt++)); do
    process_alive "$air_pid" || { wait "$air_pid" || true; air_pid=''; return; }
    sleep 0.05
  done
  fail 'Air termination deadline exceeded'
}
assert_no_children() {
  local child_pid
  while IFS= read -r child_pid; do
    [[ "$child_pid" =~ ^[0-9]+$ ]] || fail 'fixture child identity is invalid'
    process_alive "$child_pid" && fail 'fixture child was orphaned'
  done <"$fixture/events"
  return 0
}
launch() {
  printf '%s\n' "$1" >"$fixture/mode.state"
  : >"$fixture/events"
  sh "$fixture/launch.sh" "$fixture" "$helper" "$air_binary" >"$fixture/air.log" 2>&1 &
  air_pid=$!
}

launch fail
await_launches 1
await_exit
assert_no_children
grep -Fq 'Kodex development child exited unexpectedly' "$fixture/air.log" || fail 'unexpected failure did not terminate supervisor'

launch clean
await_launches 1
sleep 0.2
process_alive "$air_pid" || fail 'clean child exit terminated Air'
assert_no_children
kill -TERM "$air_pid"
await_exit

launch sleep
await_launches 1
printf 'source reload\n' >"$fixture/reload.txt"
await_launches 2
first_child=$(head -n 1 "$fixture/events")
process_alive "$first_child" && fail 'source reload left the previous child running'
process_alive "$air_pid" || fail 'normal source reload terminated Air'
kill -TERM "$air_pid"
await_exit
assert_no_children
if grep -Fq 'Kodex development child exited unexpectedly' "$fixture/air.log"; then
  fail 'normal source reload or shutdown was classified as failure'
fi

# Неверный tuple закрыто отклоняется до запуска child и не сигналит caller.
started=$(awk '{ sub(/^.*\) /, ""); print $20 }' "/proc/$$/stat")
shell_executable=$(readlink -f "/proc/$$/exe")
for wrong in generation executable; do
  generation=$started
  executable=$shell_executable
  [[ "$wrong" != generation ]] || generation=$((started + 1))
  [[ "$wrong" != executable ]] || executable=/bin/sleep
  if "$helper" "$$" "$generation" "$executable" /bin/true >/dev/null 2>&1; then
    fail 'mismatched supervisor identity was accepted'
  fi
done
printf 'Local hot child supervision passed: failure, clean exit, reload, shutdown, exact supervisor identity and no orphans\n'
