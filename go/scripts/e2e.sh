#!/usr/bin/env bash
#
# Copyright (c) 2026 - for information on the respective copyright owner
# see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
#
# SPDX-License-Identifier: Apache-2.0
#
# End-to-end test of the Go CLI against a running two-party Carbyne Stack (the deployment of the
# carbynestack repository's e2e/E2E-DEPLOY-GUIDE.md), after "cs login":
#   configure show, castor telemetry, thymus policies, every amphora command, three Millionaires'
#   games through ephemeral, and (with JAVA_CLI_JAR) secrets exchanged with the Java CLI.
# Commands run one after the other: every CLI call refreshes the tokens, and two calls at the same
# time would use the same refresh token (Hydra then revokes the login).
#
# Environment:
#   CS_GO         Go CLI binary                      (default: $WORK/cli/cs-go)
#   CLI_CONFIG    CLI configuration                  (default: $WORK/cli/config)
#   CLI_TOKENS    token file written by "cs login"   (default: $WORK/cli/tokens.json)
#   PROGRAM       Millionaires' program              (default: billionaires.mpc of the carbynestack repo)
#   JAVA_CLI_JAR  Java CLI jar for the interoperability checks (optional; needs JAVA_TOOL_OPTIONS
#                 with the trust store of the clusters)
#   WORK          working directory                  (default: $HOME/build-tmp)
set -u
WORK=${WORK:-$HOME/build-tmp}
CS_GO=${CS_GO:-$WORK/cli/cs-go}
CLI_CONFIG=${CLI_CONFIG:-$WORK/cli/config}
CLI_TOKENS=${CLI_TOKENS:-$WORK/cli/tokens.json}
PROGRAM=${PROGRAM:-$HOME/workspace/Go_migration/go_versions/carbynestack/e2e/billionaires.mpc}
JAVA_CLI_JAR=${JAVA_CLI_JAR:-}
CS=("$CS_GO" --config-file "$CLI_CONFIG" --access-token-file "$CLI_TOKENS")
JCS=(java -jar "$JAVA_CLI_JAR" --config-file "$CLI_CONFIG" --access-token-file "$CLI_TOKENS")
JNOISE='Picked up\|StatusLogger'
pass=0 fail=0

check() { # name condition-result details
  if [ "$2" = 0 ]; then pass=$((pass + 1)); echo "PASS  $1"; else fail=$((fail + 1)); echo "FAIL  $1: $3"; fi
}
ok() { [ "$1" = "$2" ]; echo $?; }
has() { case "$1" in *"$2"*) echo 0 ;; *) echo 1 ;; esac; }

# configuration
out=$("${CS[@]}" configure show); rc=$?
check "configure show" "$(ok "$rc$(has "$out" '"providers" : [ {')" "00")" "rc=$rc"

# castor telemetry of both providers
for p in 1 2; do
  out=$("${CS[@]}" castor get-telemetry "$p"); rc=$?
  check "castor get-telemetry $p" "$(ok "$rc$(has "$out" 'inputmask_gfp')$(has "$out" 'interval: ')" "000")" "rc=$rc $out"
done

# thymus
out=$("${CS[@]}" thymus list-policies); rc=$?
check "thymus list-policies" "$(ok "$rc" 0)" "rc=$rc $out"
policy=$(printf '%s\n' "$out" | grep ':' | head -n 1)
if [ -n "$policy" ]; then
  out=$("${CS[@]}" thymus get-policy "$policy"); rc=$?
  check "thymus get-policy $policy" "$(ok "$rc$(has "$out" 'package')" "00")" "rc=$rc $out"
fi

# amphora: every command on one secret
id=$("${CS[@]}" amphora create-secret 42 7 -t accessPolicy=carbynestack.def -t e2e=go -t n=5 | tail -n 1); rc=$?
check "amphora create-secret" "$(ok "$rc$( [[ $id =~ ^[0-9a-f-]{36}$ ]]; echo $?)" "00")" "rc=$rc id=$id"
out=$("${CS[@]}" amphora get-secret "$id"); rc=$?
check "amphora get-secret" "$(ok "$rc$(has "$out" '[42, 7]')$(has "$out" "$(printf '\te2e -> go')")" "000")" "rc=$rc $out"
out=$("${CS[@]}" amphora get-tags "$id"); rc=$?
check "amphora get-tags" "$(ok "$rc$(has "$out" "$(printf '\tn -> 5')")" "00")" "rc=$rc $out"
out=$("${CS[@]}" amphora get-tag -i "$id" e2e); rc=$?
check "amphora get-tag" "$(ok "$rc$out" "0$(printf '\te2e -> go')")" "rc=$rc $out"
out=$("${CS[@]}" amphora update-tag -i "$id" e2e=updated); rc=$?
check "amphora update-tag" "$(ok "$rc$out" "0Tag updated")" "rc=$rc $out"
out=$("${CS[@]}" amphora create-tag -i "$id" extra=1); rc=$?
check "amphora create-tag" "$(ok "$rc$out" "0Tag created")" "rc=$rc $out"
out=$("${CS[@]}" amphora get-secrets -l -f e2e:updated); rc=$?
check "amphora get-secrets -f" "$(ok "$rc$(has "$out" "$id")" "00")" "rc=$rc $out"
out=$("${CS[@]}" amphora get-secrets -f extra:1 -s n:ASC); rc=$?
check "amphora get-secrets -f -s" "$(ok "$rc$(has "$out" "$(printf '\textra -> 1')")" "00")" "rc=$rc $out"
out=$("${CS[@]}" amphora delete-tag -i "$id" extra); rc=$?
check "amphora delete-tag" "$(ok "$rc$out" "0Tag deleted")" "rc=$rc $out"
# PUT /tags fails in the Java Amphora service (Hibernate ObjectDeletedException), and the Go service
# keeps that by default (COMPAT_JAVA_TAG_REPLACE_BUG): the Java CLI gets the same HTTP 500 and exit 3.
out=$("${CS[@]}" amphora overwrite-tags -i "$id" accessPolicy=carbynestack.def e2e=overwritten); rc=$?
check "amphora overwrite-tags (service answers 500 like the Java stack, exit 3)" \
  "$(ok "$rc$(has "$out" 'ObjectDeletedException')" "30")" "rc=$rc $out"
out=$("${CS[@]}" amphora get-tags "$id"); rc=$?
check "amphora tags unchanged after the failed overwrite" "$(ok "$rc$(has "$out" "$(printf '\te2e -> updated')")" "00")" "rc=$rc $out"
out=$(printf '%s\n' "$id" | "${CS[@]}" amphora delete-secrets); rc=$?
check "amphora delete-secrets (ids on stdin)" "$(ok "$rc$(has "$out" 'All Secrets deleted')" "00")" "rc=$rc $out"
"${CS[@]}" amphora get-secret "$id" >/dev/null 2>&1; rc=$?
check "amphora get-secret of a deleted secret fails (exit 3)" "$(ok "$rc" 3)" "rc=$rc"

# Millionaires' games through ephemeral
game() { # first second expected
  local a b r v took start
  a=$(printf '%s\n' "$1" | "${CS[@]}" amphora create-secret -t accessPolicy=carbynestack.def -t authorizedPrograms=ephemeral-generic | tail -n 1)
  b=$("${CS[@]}" amphora create-secret "$2" -t accessPolicy=carbynestack.def -t authorizedPrograms=ephemeral-generic | tail -n 1)
  start=$(date +%s)
  r=$("${CS[@]}" ephemeral execute -i "$a" -i "$b" ephemeral-generic.default < "$PROGRAM" | tail -n 1 | tr -d '[]')
  took=$(( $(date +%s) - start ))
  v=$("${CS[@]}" amphora get-secret "$r" | head -n 1 | tr -d '[]')
  check "millionaires $1 < $2 -> '$v' (${took}s)" "$(ok "$v" "$3")" "expected '$3', result $r"
  "${CS[@]}" amphora delete-secrets "$a" "$b" "$r" >/dev/null
}
game 177 151 0
game 177 179 1
game 151 151 0

# Interoperability with the Java CLI (same configuration and token file)
if [ -n "$JAVA_CLI_JAR" ]; then
  id=$("${CS[@]}" amphora create-secret 4711 -t accessPolicy=carbynestack.def -t from=go | tail -n 1)
  out=$("${JCS[@]}" amphora get-secret "$id" 2>&1 | grep -v "$JNOISE" | head -n 1)
  check "secret written by Go, read by Java" "$(ok "$out" "[4711]")" "$out"
  jid=$("${JCS[@]}" amphora create-secret 815 -t accessPolicy=carbynestack.def -t from=java 2>/dev/null | grep -v "$JNOISE" | tail -n 1)
  out=$("${CS[@]}" amphora get-secret "$jid" | head -n 1)
  check "secret written by Java, read by Go" "$(ok "$out" "[815]")" "$out"
  "${CS[@]}" amphora delete-secrets "$id" "$jid" >/dev/null
  out=$("${CS[@]}" castor get-telemetry 1); jout=$("${JCS[@]}" castor get-telemetry 1 2>/dev/null | grep -v "$JNOISE")
  check "castor telemetry printed like the Java CLI" "$(ok "$(printf '%s' "$out" | sed 's/[0-9]\+/N/g')" "$(printf '%s' "$jout" | sed 's/[0-9]\+/N/g')")" "$out // $jout"
fi

echo "passed: $pass, failed: $fail"
[ "$fail" = 0 ]
