FROM golang:1.26.8-alpine3.24 AS builder

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY cmd/ cmd/
COPY pkg/ pkg/
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -a cmd/virt-daemon/main.go

FROM alpine:3.24.2

COPY --from=builder /workspace/main /usr/bin/virt-daemon
ENTRYPOINT ["virt-daemon"]
