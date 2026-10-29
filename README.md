# Acta

Acta is a self-hosted project tool for humans and agents. It brings tasks,
discussion, documents, and releases together. A Go
server serves the compiled Svelte frontend and stores application data in
PostgreSQL.

Start with the [Acta overview](docs/learn/site/2--developer-tools.shelf/acta.product/1--getting-started.topic/100--overview.md).
The documentation includes user guides, administration and recovery procedures,
and an [implementation manual](docs/learn/site/2--developer-tools.shelf/acta.product/8--technical-reference.book/1--introduction/1--overview.md).

## Development

On an x86_64 Linux host with the [build prerequisites](docs/learn/site/2--developer-tools.shelf/acta.product/7--development.topic/100--running-locally.md), run:

```sh
tools/db
tools/run
```

Open [the development server](http://localhost:8081). For an empty installation,
use the setup code printed in the protected server terminal. See
[builds and checks](docs/learn/site/2--developer-tools.shelf/acta.product/7--development.topic/200--checking.md)
for the build, test, and export commands.

## Deployment and documentation

Follow [Deploying Acta](docs/learn/site/2--developer-tools.shelf/acta.product/5--operations.shelf/100--deployment.topic/100--deployment.md)
and [Backup and Recovery](docs/learn/site/2--developer-tools.shelf/acta.product/5--operations.shelf/200--backups-and-recovery.topic/100--backups.md)
for an operated installation.

`docs/learn/site` is a standalone Trail tree, ready to merge into Peios Learn:

```sh
trail lint docs/learn/site
trail build docs/learn/site --strict
```

Use Trail with attachment support. The public tree contains only maintained
product documentation and its declared downloads. Historical source pages and
review reports remain available in Git history.
Repository collaboration instructions remain in [AGENTS.md](AGENTS.md).
