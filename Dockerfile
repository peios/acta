# The programs pekit built on the host, `tools/export` writes them to bin/:
# a pekit build runs each step in a sandbox of its own, which a container
# build cannot make.
FROM scratch AS build
COPY bin/acta-server bin/acta bin/acta-backup bin/acta-update /out/

FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171 AS app
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl gosu && rm -rf /var/lib/apt/lists/* && \
    groupadd -g 10001 acta && useradd -u 10001 -g acta -M -d /nonexistent acta
COPY --from=build /out/acta-server /usr/local/bin/acta-server
COPY deploy/production/app-entrypoint.sh /usr/local/bin/acta-entrypoint
ENTRYPOINT ["/usr/local/bin/acta-entrypoint"]
CMD ["/usr/local/bin/acta-server", "-listen", ":8081"]

FROM postgres:17@sha256:e38411452a464af89e5adadb8d223bf53b898d47d6ef918b2d58c08707350449 AS database
RUN apt-get update && apt-get install -y --no-install-recommends pgbackrest age python3 && rm -rf /var/lib/apt/lists/*
COPY deploy/production/db-entrypoint.sh /usr/local/bin/acta-db-entrypoint
COPY deploy/production/init-db.sh /docker-entrypoint-initdb.d/10-acta.sh
ENTRYPOINT ["/usr/local/bin/acta-db-entrypoint"]
CMD ["postgres"]

FROM database AS backup
COPY --from=build /out/acta-backup /out/acta-server /usr/local/bin/
COPY deploy/production/backup-entrypoint.py /usr/local/bin/acta-backup-entrypoint
ENTRYPOINT ["/usr/local/bin/acta-backup-entrypoint"]
CMD ["serve"]

FROM docker:27-cli@sha256:851f91d241214e7c6db86513b270d58776379aacc5eb9c4a87e5b47115e3065c AS updater
COPY --from=build /out/acta-update /usr/local/bin/acta-update
ENTRYPOINT ["/usr/local/bin/acta-update"]
CMD ["serve"]
