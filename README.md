# Acta

Acta is a self-hosted project tool for humans and agents. It brings tasks,
discussion, documents, releases, and provider conversations together. A Go
server serves the compiled Svelte frontend and stores application data in
PostgreSQL.

Start with the [Acta overview](learn/site/acta.product/1--getting-started.topic/100--overview.md).
The documentation includes user guides, administration and recovery procedures,
and an [implementation manual](learn/site/acta.product/8--technical-reference.book/1--introduction/1--overview.md).

## Development

On an x86_64 Linux host with the [build prerequisites](learn/site/acta.product/7--development.topic/100--running-locally.md), run:

```sh
./bazel run //:db
./bazel run //:run
```

Open [the development server](http://localhost:8081). For an empty installation,
use the setup code printed in the protected server terminal. See
[builds and checks](learn/site/acta.product/7--development.topic/200--checking.md)
for the test and export targets.

## Deployment and documentation

Follow [Deploying Acta](learn/site/acta.product/5--operations.topic/100--deployment.md)
and [Backup and Recovery](learn/site/acta.product/5--operations.topic/400--backups.md)
for an operated installation.

`learn/site` is a standalone Trail tree, ready to merge into Peios Learn:

```sh
trail lint learn/site
trail build learn/site --strict
```

Use Trail with attachment support. The public tree contains only maintained
product documentation and its declared downloads. Historical source pages and
review reports are preserved in [the repository archive](docs/archive/README.md).
Repository collaboration instructions remain in [AGENTS.md](AGENTS.md).
