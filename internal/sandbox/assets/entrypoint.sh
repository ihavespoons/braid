#!/usr/bin/env bash
# braid sandbox entrypoint.
#
# Environment variables:
#   BRAID_NETWORK_RESTRICTED  "1" to apply iptables egress restrictions
#   BRAID_ALLOWED_HOSTS       comma-separated hostnames to allow (restricted mode)
#   BRAID_UID / BRAID_GID     host user ids to remap the braid user to
set -euo pipefail

apply_iptables_rules() {
  # Fail open if we don't have CAP_NET_ADMIN — better to let the
  # container run (and be isolated via the default bridge network)
  # than to crash. Users who require strict isolation should run
  # `braid doctor` to catch this misconfiguration early.
  if ! iptables -L >/dev/null 2>&1; then
    echo "[braid] warning: iptables unavailable (missing CAP_NET_ADMIN); continuing with default network"
    return 0
  fi

  # Allow loopback and established connections.
  iptables -A OUTPUT -o lo -j ACCEPT
  iptables -A OUTPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT

  # DNS — required for any hostname resolution.
  iptables -A OUTPUT -p udp --dport 53 -j ACCEPT
  iptables -A OUTPUT -p tcp --dport 53 -j ACCEPT

  # Resolve each allowed host to IPs and allow HTTPS (443) to those IPs.
  if [ -n "${BRAID_ALLOWED_HOSTS:-}" ]; then
    IFS=',' read -ra HOSTS <<< "${BRAID_ALLOWED_HOSTS}"
    for host in "${HOSTS[@]}"; do
      host="$(echo "$host" | tr -d '[:space:]')"
      [ -z "$host" ] && continue
      for ip in $(getent ahostsv4 "$host" 2>/dev/null | awk '{print $1}' | sort -u); do
        iptables -A OUTPUT -d "$ip" -p tcp --dport 443 -j ACCEPT
      done
    done
  fi

  # Default deny on OUTPUT.
  iptables -P OUTPUT DROP
}

if [ "${BRAID_NETWORK_RESTRICTED:-0}" = "1" ]; then
  apply_iptables_rules
fi

# Remap the `braid` user to match host UID/GID so bind-mounted files
# don't end up owned by the container's default 1000.
if [ -n "${BRAID_UID:-}" ]; then
  current_uid=$(id -u braid)
  if [ "${current_uid}" != "${BRAID_UID}" ]; then
    usermod -u "${BRAID_UID}" braid >/dev/null 2>&1 || true
  fi
  if [ -n "${BRAID_GID:-}" ]; then
    current_gid=$(id -g braid)
    if [ "${current_gid}" != "${BRAID_GID}" ]; then
      groupmod -g "${BRAID_GID}" braid >/dev/null 2>&1 || true
    fi
  fi
  # Ensure home dir is readable/writable by the remapped user.
  chown "${BRAID_UID}:${BRAID_GID:-${BRAID_UID}}" /home/braid 2>/dev/null || true
fi

# If we're PID 1 as root, drop to the braid user; otherwise exec as-is.
if [ "$(id -u)" = "0" ]; then
  exec gosu braid "$@"
fi
exec "$@"
