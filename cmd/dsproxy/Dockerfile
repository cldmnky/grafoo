# Build the UI
FROM --platform=$BUILDPLATFORM node:20 AS ui-builder
WORKDIR /app
COPY cmd/dsproxy/ui/package.json cmd/dsproxy/ui/package-lock.json ./
RUN npm ci
COPY cmd/dsproxy/ui/ .
RUN npm run build

# Build the binary
FROM --platform=$BUILDPLATFORM golang:1.25 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY api/ api/
COPY internal/ internal/

# Copy UI assets to the expected location for embedding
COPY --from=ui-builder /app/dist cmd/dsproxy/ui/dist

# Build
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o dsproxy ./cmd/dsproxy

# Final image
FROM alpine:latest
RUN apk add --no-cache nftables

WORKDIR /
COPY --from=builder /workspace/dsproxy .
COPY --from=builder /workspace/cmd/dsproxy/authz/model.conf /etc/dsproxy/policy/model.conf
COPY --from=builder /workspace/cmd/dsproxy/authz/policy.csv /etc/dsproxy/policy/policy.csv

USER 0:0

ENTRYPOINT ["/dsproxy"]
