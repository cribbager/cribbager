# syntax=docker/dockerfile:1

# ---- build: compile a static binary (pure stdlib, no module downloads) ----
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
# CGO off → a fully static binary that runs on the minimal runtime image below.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cribbager-server ./cmd/cribbager-server

# ---- runtime: binary + the raw (no-build) web client ----
FROM alpine:3.20
# Run as a non-root user (defense in depth). The binary listens on 8080, a
# non-privileged port, so no extra capabilities are needed; the COPY'd files are
# world-readable, so the app user can read the binary and static assets.
RUN adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/cribbager-server /app/cribbager-server
COPY web/public /app/web/public
ENV ADDR=":8080" \
    WEB="/app/web/public"
USER app
EXPOSE 8080
CMD ["/app/cribbager-server"]
