# Deploying Acta

A tick-through checklist for a fresh public deployment. The narrative version
(and the Cloudflare follow-up) live in the README's "Self-hosting" section; this
is the operational sequence. Secrets are generated **on the box** and never
printed — don't paste them anywhere.

## 0. Prerequisites (before touching the box)

- [ ] A Linux host (Ubuntu 24.04 LTS). **1 GB RAM is plenty** — the box runs a
      prebuilt image from GHCR and never compiles code.
- [ ] The deploy SSH key added to the host's `root` (Vultr "SSH Keys" at create
      time, or appended to `/root/.ssh/authorized_keys`):
      `ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMb2gZNNhXxhjfSYdKGE72K12q/d1uVQSQocnZPkPiKn claude-code`
- [ ] A real domain, with a DNS `A`/`AAAA` record pointed at the host's IP.
      Keep it **DNS-only / unproxied** for now so Caddy can complete the ACME
      (Let's Encrypt) challenge.
- [ ] Confirm the host is reachable: `ssh -i ~/.ssh/id_claude root@<HOST_IP> true`

## 1. Install Docker (on the box)

```sh
ssh -i ~/.ssh/id_claude root@<HOST_IP>
curl -fsSL https://get.docker.com | sh              # docker + compose plugin
docker compose version                              # sanity check
```

## 2. Get the deploy files (on the box)

The box only needs the deploy bundle — compose file, Caddyfile, and the `.env`
template. No source, no build.

```sh
mkdir -p /opt/acta && cd /opt/acta
curl -fsSL https://github.com/peios/acta/releases/latest/download/acta-deploy.tar.gz \
  | tar xz --strip-components=1
```

## 3. Create `.env` with generated secrets (on the box)

Run this on the box. It writes secrets straight into `.env` — nothing is echoed.

```sh
cd /opt/acta
umask 077
{
  echo "ACTA_DOMAIN=<your-domain>"
  echo "ACTA_RP_NAME=Acta"
  echo "POSTGRES_PASSWORD=$(openssl rand -base64 32)"
  echo "ACTA_BOOTSTRAP_USERNAME=admin"
  echo "ACTA_BOOTSTRAP_PASSWORD=$(openssl rand -base64 24)"
} > .env
```

The bootstrap admin password is random and will be **discarded** — step 6 sets a
real one without ever needing to read it.

## 4. Launch

```sh
docker compose -f docker-compose.prod.yml up -d
```

This pulls the image, runs migrations, creates the admin account, and Caddy
requests the certificate on the first HTTPS hit.

## 5. Verify

```sh
docker compose -f docker-compose.prod.yml ps           # all healthy/running
curl -fsS https://<your-domain>/healthz                # -> ok   (valid TLS)
curl -fsS https://<your-domain>/readyz                 # -> ok   (DB reachable)
docker compose -f docker-compose.prod.yml logs caddy | grep -i certificate
```

If the cert didn't issue: check the DNS record resolves to this host and that
ports 80/443 are open (step 7 — but 80 must be reachable for the ACME challenge).

## 6. Set the real admin password

Interactive prompt — the password is typed on the box, never in argv or history:

```sh
docker compose -f docker-compose.prod.yml exec app /acta-server setpassword -username admin
```

Then sign in at `https://<your-domain>` and confirm it works. (This same command
is the recovery path for a future lockout; it also revokes existing sessions.)

## 7. Firewall

```sh
ufw allow OpenSSH
ufw allow 80/tcp
ufw allow 443/tcp
ufw enable
```

Postgres (5432) and the app (8080) are never published — Caddy reaches the app
over the compose network, so nothing else needs opening.

## 8. Backups

A nightly dump (test a restore at least once):

```sh
# /etc/cron.daily/acta-backup  (chmod +x)
cd /opt/acta && docker compose -f docker-compose.prod.yml exec -T db \
  pg_dump -U acta acta | gzip > /var/backups/acta-$(date +%F).sql.gz
```

Restore check: `gunzip -c <dump> | docker compose -f docker-compose.prod.yml exec -T db psql -U acta acta`.

## 9. Monitoring

- [ ] Point an uptime check at `https://<your-domain>/healthz`.

## Updating later

Pull the newest image (or pin `ACTA_VERSION` in `.env`) and recreate:

```sh
ssh -i ~/.ssh/id_claude root@<HOST_IP> \
  'cd /opt/acta && docker compose -f docker-compose.prod.yml pull && \
   docker compose -f docker-compose.prod.yml up -d'
```

If the compose file or Caddyfile changed, re-fetch the deploy bundle (step 2)
first — it won't touch your `.env`.

## Putting Cloudflare in front

See the README's "Putting Cloudflare in front" — enable the orange cloud, set
SSL/TLS to **Full (strict)**, remove `header_up -CF-Connecting-IP` from the
`Caddyfile`, and add Cloudflare's ranges to `ACTA_TRUSTED_PROXIES`.
