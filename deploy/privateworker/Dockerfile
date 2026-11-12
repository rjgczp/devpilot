FROM golang:1.24.3@sha256:81bf5927dc91aefb42e2bc3a5abdbe9bb3bae8ba8b107e2a4cf43ce3402534c6 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/privateworker/ cmd/privateworker/
COPY internal/ internal/
RUN CGO_ENABLED=0 go build -trimpath -tags=privateworker -o /privateworker ./cmd/privateworker
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /privateworker /privateworker
USER 65534:65534
ENTRYPOINT ["/privateworker"]
