# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Static binary: pgx and argon2 are pure Go, templates and migrations are
# embedded, so the result needs nothing at runtime. Cross-compiling for
# $TARGETOS/$TARGETARCH (cheap with CGO off) keeps multi-arch image builds off
# QEMU. Under plain `docker compose build` these default to the host platform.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /acta-server ./cmd/acta-server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /acta-server /acta-server
EXPOSE 8080
ENTRYPOINT ["/acta-server"]
CMD ["serve"]
