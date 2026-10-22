# Builds the API, the sync worker and the probe into one small image.
# Run as: docker build -f deploy/go.Dockerfile .
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sync-worker ./cmd/sync-worker \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/probe ./cmd/probe

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/ /app/
COPY config/scope.json /app/config/scope.json
USER nonroot:nonroot
# No secrets are baked in; they are injected at runtime.
ENTRYPOINT ["/app/api", "-env-file", "/dev/null"]
