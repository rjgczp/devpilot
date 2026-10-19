# The image supplies Linux syscall/firewall tools only. Go binaries are built
# using the repository toolchain before running it; this does not select Go.
FROM postgres:18@sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336
RUN apt-get update && apt-get install -y --no-install-recommends strace iptables python3 && rm -rf /var/lib/apt/lists/*
ENTRYPOINT ["/bin/sh"]
