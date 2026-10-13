#!/bin/bash
# Shared helpers, sourced by run-*.sh and backup.sh. Not executable on its own.

# load_env <file> [KEY...]
# Reads KEY=VALUE lines (no shell evaluation, so `$`, `&`, `;` in a value are literal).
# Blank lines and #-comments are skipped; one pair of surrounding quotes is stripped.
# With KEY arguments only those keys are exported; otherwise all of them.
load_env() {
  local file=$1 line key val want k keep
  shift
  want=("$@")
  [ -r "$file" ] || { echo "cannot read env file $file" >&2; return 1; }
  while IFS= read -r line || [ -n "$line" ]; do
    case $line in '' | '#'*) continue ;; esac
    key=${line%%=*}
    val=${line#*=}
    [[ $key =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || continue
    if [[ $val =~ ^\"(.*)\"$ ]] || [[ $val =~ ^\'(.*)\'$ ]]; then val=${BASH_REMATCH[1]}; fi
    if [ ${#want[@]} -gt 0 ]; then
      keep=0
      for k in "${want[@]}"; do [ "$k" = "$key" ] && keep=1; done
      [ $keep = 1 ] || continue
    fi
    export "$key=$val"
  done <"$file"
}

# pg_env_from_url <url>
# Splits postgres://USER:PASS@[HOST[:PORT]]/DB?host=/socket/dir&... into PGUSER,
# PGPASSWORD, PGDATABASE, PGHOST, PGPORT so that psql/pg_dump get the password through
# the environment instead of argv (argv is visible to every local user via `ps`).
# The password must be URL-safe (install.sh generates hex).
pg_env_from_url() {
  local url=$1 q
  local re='^postgres(ql)?://([^:@/]+):([^@]*)@([^/:?]*)(:([0-9]+))?/([^?]*)(\?(.*))?$'
  [[ $url =~ $re ]] || { echo "DATABASE_URL is not in postgres://user:pass@host/db form" >&2; return 1; }
  export PGUSER=${BASH_REMATCH[2]} PGPASSWORD=${BASH_REMATCH[3]} PGDATABASE=${BASH_REMATCH[7]}
  [ -n "${BASH_REMATCH[4]}" ] && export PGHOST=${BASH_REMATCH[4]}
  [ -n "${BASH_REMATCH[6]}" ] && export PGPORT=${BASH_REMATCH[6]}
  q=${BASH_REMATCH[9]}
  case "&$q" in *"&host="*) q=${q#*host=}; export PGHOST=${q%%&*} ;; esac
  return 0
}
