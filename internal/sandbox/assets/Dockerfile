# Dockerfile for braid's sandbox image.
# Installs:
#   - node (for Claude Code CLI)
#   - @anthropic-ai/claude-code (the claude binary)
#   - iptables (for network restriction)
#   - gosu (for dropping privileges from root → braid user)
#   - git, ca-certificates, dnsutils (host lookups)
#
# The entrypoint applies iptables rules when BRAID_NETWORK_RESTRICTED=1
# then execs the given command as the "braid" user, remapped to the
# host's UID/GID via BRAID_UID/BRAID_GID so bind-mounted files get the
# right ownership.
FROM node:20-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      iptables \
      ca-certificates \
      dnsutils \
      gosu \
      git \
      curl \
 && rm -rf /var/lib/apt/lists/*

RUN npm install -g @anthropic-ai/claude-code

# node:20-slim ships a `node` user at UID/GID 1000; remove it so we can
# claim 1000 for `braid` (the entrypoint later remaps to the host UID).
RUN userdel -r node 2>/dev/null || true \
 && groupdel node 2>/dev/null || true \
 && useradd -m -u 1000 -s /bin/bash braid

COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["bash"]
