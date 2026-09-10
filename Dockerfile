# Build locally until the release channel has been isolated from old Acta.
FROM node:22-bookworm-slim@sha256:83f487e0a63425e5b4d146fb5e5be574bcbe1b7b843d3ebafdd95eaf7767a7e5 AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
COPY internal/tasks/properties.json /src/internal/tasks/properties.json
RUN npm run build

FROM golang:1.26-bookworm@sha256:9fdc884aacc3bec89b20ffc69f4bb369c78210e3e4f600387b5128b12c199f81 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/web/build ./web/build
ENV CGO_ENABLED=0 GOMAXPROCS=4
RUN go build -p 2 -trimpath -o /out/acta2-server ./cmd/acta2 && \
    go build -p 2 -trimpath -o /out/acta2-backup ./cmd/acta2-backup

FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171 AS app
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl gosu && rm -rf /var/lib/apt/lists/* && \
    groupadd -g 10001 acta && useradd -u 10001 -g acta -M -d /nonexistent acta
COPY --from=build /out/acta2-server /usr/local/bin/acta2-server
COPY deploy/production/app-entrypoint.sh /usr/local/bin/acta-entrypoint
ENTRYPOINT ["/usr/local/bin/acta-entrypoint"]
CMD ["/usr/local/bin/acta2-server", "-listen", ":8081"]

FROM postgres:17@sha256:e38411452a464af89e5adadb8d223bf53b898d47d6ef918b2d58c08707350449 AS database
RUN apt-get update && apt-get install -y --no-install-recommends pgbackrest age python3 && rm -rf /var/lib/apt/lists/*
COPY deploy/production/db-entrypoint.sh /usr/local/bin/acta-db-entrypoint
COPY deploy/production/init-db.sh /docker-entrypoint-initdb.d/10-acta.sh
ENTRYPOINT ["/usr/local/bin/acta-db-entrypoint"]
CMD ["postgres"]

FROM database AS backup
COPY --from=build /out/acta2-backup /out/acta2-server /usr/local/bin/
COPY deploy/production/backup-entrypoint.py /usr/local/bin/acta-backup-entrypoint
ENTRYPOINT ["/usr/local/bin/acta-backup-entrypoint"]
CMD ["serve"]
