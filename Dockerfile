# syntax=docker/dockerfile:1

FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Static binary: pgx and argon2 are pure Go, templates and migrations are
# embedded, so the result needs nothing at runtime.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /acta ./cmd/acta

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /acta /acta
EXPOSE 8080
ENTRYPOINT ["/acta"]
CMD ["serve"]
