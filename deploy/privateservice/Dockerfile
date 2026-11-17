FROM golang:1.24.3-bookworm@sha256:29d97266c1d341b7424e2f5085440b74654ae0b61ecdba206bc12d6264844e21 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir /out && \
 CGO_ENABLED=0 go build -buildvcs=false -tags=privateworker -trimpath -o /out/privateworker ./cmd/privateworker && \
 CGO_ENABLED=0 go build -buildvcs=false -tags=privateworker -trimpath -o /out/storagereporter ./cmd/storagereporter && \
 CGO_ENABLED=0 go build -buildvcs=false -tags=privateworker,privateprobe -trimpath -o /out/privatesupervisor ./cmd/privatesupervisor && \
 CGO_ENABLED=0 go build -buildvcs=false -tags=privateworker,privateprobe -trimpath -o /out/privateapproval ./cmd/privateapproval && \
 CGO_ENABLED=0 go build -buildvcs=false -tags=privateprobe -trimpath -o /out/privateprobe ./cmd/privateprobe && \
 CGO_ENABLED=0 go build -buildvcs=false -tags=privateprobe -trimpath -o /out/privatenetwork ./cmd/privatenetwork
FROM postgres:18@sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336
RUN apt-get update && apt-get install -y --no-install-recommends iproute2 nftables conntrack strace python3 openssl && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/ /toolbin/
COPY runtime/private/control_child.py /runtime/control_child.py
# No test binary, fixture coordinator, secret, database provisioning or Gate.
ENTRYPOINT ["/toolbin/privatesupervisor"]
