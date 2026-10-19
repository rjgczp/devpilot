FROM postgres:18@sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336
RUN apt-get update && apt-get install -y --no-install-recommends iproute2 iptables strace tcpdump python3 openssl util-linux && rm -rf /var/lib/apt/lists/*
ENTRYPOINT ["/bin/sh"]
