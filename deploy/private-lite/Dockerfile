FROM golang:1.24.3-bookworm@sha256:29d97266c1d341b7424e2f5085440b74654ae0b61ecdba206bc12d6264844e21 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir /out && \
 CGO_ENABLED=0 go build -buildvcs=false -tags=privateprobe -ldflags '-X main.executionProfile=private-lite' -trimpath -o /out/liteprobe ./cmd/privateprobe && \
 CGO_ENABLED=0 go build -buildvcs=false -tags=privateprobe -ldflags '-X main.executionProfile=private-lite' -trimpath -o /out/litenetwork ./cmd/privatenetwork && \
 CGO_ENABLED=0 go build -buildvcs=false -tags=privateworker,privateprobe -ldflags '-X main.executionProfile=private-lite' -trimpath -o /out/liteapproval ./cmd/privateapproval
FROM devpilot-private-runtime
COPY --from=build /out/ /toolbin/
